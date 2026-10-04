//go:build panel

package mic

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/loopback"
)

// micSocket is the abstract socket the app serves the microphone on. The app reads it from Android
// as 16 kHz mono S16LE and writes it here as it comes; nothing else is said on it.
const micSocket = "@techo5.mic"

// dialWait is how long the daemon waits for the app's socket before giving up on this start. The app
// starts the daemon after the socket is listening, so this only covers a slow start.
const dialWait = 10 * time.Second

// Lining the speaker's history up with the microphone. Android keeps both ends of the path to itself
// and the delay between them differs by device, route and buffer, so it is measured while something
// plays: the loudness of what was played against the loudness of what was heard, a few milliseconds
// at a time, at every lag up to maxLag. The canceller's filter reaches 64 ms (cancelTaps), so the
// reference has to arrive at most that far ahead of its echo and never after it: it is placed lead
// ahead.
const (
	envBlock    = 64              // samples per loudness block, 4 ms
	envWindow   = Rate / envBlock // blocks compared at once, one second
	maxLag      = Rate / envBlock // a second of lag, in blocks
	lead        = 2 * envBlock    // 8 ms
	minScore    = 0.5             // correlation a measurement needs to be believed
	moveAtLeast = envBlock * 2    // a smaller correction is jitter, not a different delay
	driftLimit  = Rate / 8        // 125 ms: further than this from the writer, line up again
	wantSettle  = 0.02            // how much of each read's view of the writer the average takes
)

// socketCapture turns the app's mono S16LE stream into the frames the Source expects: S24_3LE, the
// microphone in ch0 and what the speaker played, lined up, in ch1.
type socketCapture struct {
	conn net.Conn
	in   []byte
	ref  []int16

	// read is how many microphone samples have been read. The reference for microphone sample m is
	// loopback sample m+offset; lag is the delay measured into it, so the writer's position less lag
	// is where offset should sit.
	read    int64
	offset  int64
	lag     int64
	started bool
	// want is where offset should sit, averaged over about a second of reads: the writer moves a
	// period at a time and is held up by the app now and then, so one read's view of it jitters.
	want float64

	// Loudness of the microphone, one value per envBlock, since the last time offset moved.
	env   []float64
	block float64
	fill  int
	from  int64 // the microphone sample env starts at
}

// openDevice dials the app's microphone socket: on a panel the capture is the app's stream, not ALSA.
func openDevice() (capture, error) {
	deadline := time.Now().Add(dialWait)
	for {
		conn, err := net.Dial("unix", micSocket)
		if err == nil {
			return &socketCapture{conn: conn}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connecting to the app's microphone: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// afterOpen has nothing to do: Android sets the microphone up for the app.
func afterOpen() {}

// Read fills p with whole frames. It reads one mono sample for every frame p holds, so a frame is
// never split across reads.
func (c *socketCapture) Read(p []byte) (int, error) {
	const frameBytes = Channels * Bits / 8
	frames := len(p) / frameBytes
	if frames == 0 {
		return 0, errors.New("mic: buffer smaller than one frame")
	}
	if cap(c.in) < frames*2 {
		c.in = make([]byte, frames*2)
		c.ref = make([]int16, frames)
	}
	in := c.in[:frames*2]
	if _, err := io.ReadFull(c.conn, in); err != nil {
		return 0, err
	}

	c.lineUp()
	ref := c.ref[:frames]
	loopback.Played.ReadAt(ref, c.read+c.offset)

	for f := range frames {
		// A 16-bit sample is the top two bytes of a 24-bit one.
		o := f * frameBytes
		p[o], p[o+1], p[o+2] = 0, in[2*f], in[2*f+1]
		r := uint16(ref[f])
		p[o+3], p[o+4], p[o+5] = 0, byte(r), byte(r>>8)

		v := float64(int16(uint16(in[2*f]) | uint16(in[2*f+1])<<8))
		c.block += v * v
		if c.fill++; c.fill == envBlock {
			c.env = append(c.env, math.Sqrt(c.block/envBlock))
			c.block, c.fill = 0, 0
		}
	}
	c.read += int64(frames)

	if len(c.env) >= envWindow {
		c.measure()
	}
	return frames * frameBytes, nil
}

// lineUp keeps offset where the writer says it should be, on average: the writer moves a period at a
// time and stalls while the app catches up, and following every jump would tear the reference.
func (c *socketCapture) lineUp() {
	now := float64(int64(loopback.Played.Written()) - c.read - c.lag)
	if !c.started {
		c.want, c.offset, c.started = now, int64(now), true
		c.restart()
		return
	}
	c.want += (now - c.want) * wantSettle
	if drift := int64(c.want) - c.offset; abs64(drift) > driftLimit {
		slog.Info("echo reference lined up again", "drift_ms", drift*1000/Rate)
		c.offset = int64(c.want)
		c.restart()
	}
}

// measure finds the delay from the speaker's history to the microphone over the last second, and
// moves the reference to it when the answer is clear and different.
func (c *socketCapture) measure() {
	mic := c.env[len(c.env)-envWindow:]
	start := c.from + int64(len(c.env)-envWindow)*envBlock

	// The speaker's loudness from maxLag blocks before the window to its end, as the reference stood.
	span := (envWindow + maxLag) * envBlock
	played := make([]int16, span)
	loopback.Played.ReadAt(played, start+c.offset-int64(maxLag*envBlock))
	spk := make([]float64, envWindow+maxLag)
	for b := range spk {
		var e float64
		for _, v := range played[b*envBlock : (b+1)*envBlock] {
			e += float64(v) * float64(v)
		}
		spk[b] = math.Sqrt(e / envBlock)
	}

	best, score := 0, 0.0
	for lag := 0; lag <= maxLag; lag++ {
		// spk[maxLag+i-lag] is what was played lag blocks before microphone block i was heard.
		if s := correlate(mic, spk[maxLag-lag:maxLag-lag+envWindow]); s > score {
			best, score = lag, s
		}
	}
	c.restart()
	if score < minScore {
		return
	}

	// The echo comes best blocks after the reference as it stands; move the reference that much
	// later, less the lead.
	move := int64(best*envBlock) - lead
	if abs64(move) < moveAtLeast {
		return
	}
	c.lag += move
	c.offset -= move
	c.want -= float64(move)
	slog.Info("echo reference delay measured", "delay_ms", c.lag*1000/Rate, "score", math.Round(score*100)/100)
}

// restart forgets the microphone's loudness: after the reference moves, what was gathered against the
// old position measures the wrong thing.
func (c *socketCapture) restart() {
	c.env = c.env[:0]
	c.block, c.fill = 0, 0
	c.from = c.read
}

// correlate is the Pearson correlation of two equal runs, 0 where either is flat: a speaker playing
// silence, or a quiet room, says nothing about the delay.
func correlate(a, b []float64) float64 {
	n := float64(len(a))
	var sa, sb float64
	for i := range a {
		sa += a[i]
		sb += b[i]
	}
	ma, mb := sa/n, sb/n
	var ab, aa, bb float64
	for i := range a {
		x, y := a[i]-ma, b[i]-mb
		ab += x * y
		aa += x * x
		bb += y * y
	}
	// A reference this flat is silence or a steady hum: nothing to line up against.
	if aa == 0 || bb < n*100 {
		return 0
	}
	return ab / math.Sqrt(aa*bb)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (c *socketCapture) Close() error { return c.conn.Close() }
