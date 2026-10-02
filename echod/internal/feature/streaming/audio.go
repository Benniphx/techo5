package streaming

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// What the receivers send, as they are told to send it: 16-bit stereo at 44.1 kHz, interleaved.
const (
	audioRate     = 44100
	audioChannels = 2
)

// setAside is how long a receiver must be quiet, after something else took the speaker from it, before
// what it sends next counts as asked for again: an app still playing to a speaker that was given
// something else does not take it straight back, as on any speaker; pausing and playing again does.
var setAside = 2 * time.Second

// play is what plays a receiver's audio; media.Get().PlayReceived, a variable for the tests.
var play = func(name string, src media.PCMSource, rate, channels int) {
	media.Get().PlayReceived(name, src, rate, channels)
}

// pump plays what a receiver writes to r as the track named name: a track starts whenever audio arrives
// and none of the receiver's own is playing, and runs until the receiver goes quiet or something else
// takes the speaker. r stays open across tracks and across the program's restarts. gain, when there is
// one, scales every sample by what it says at the time (Spotify's, which undoes librespot's own volume).
func pump(ctx context.Context, name string, r *os.File, gain func() float64) {
	buf := make([]byte, 16384)
	for ctx.Err() == nil {
		_ = r.SetReadDeadline(time.Time{})
		n, err := r.Read(buf)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("streaming: reading a receiver's audio failed", "from", name, "err", err)
			}
			return
		}
		if n == 0 {
			continue
		}
		src := &pipeSource{f: r, pending: append([]byte(nil), buf[:n]...), gain: gain, done: make(chan struct{})}
		slog.Info("streaming: audio arrived", "from", name)
		play(name, src, audioRate, audioChannels)
		select {
		case <-ctx.Done():
			src.Close()
			return
		case <-src.done:
		}
		// A track that ended because the receiver went quiet is simply over: what it sends next is a new
		// one. One that was taken from it (something else played, or it was paused here, which no phone
		// hears of) leaves the receiver still sending: that is set aside until it has been quiet a
		// moment, which a pause on the phone gives.
		if !src.wentQuiet() {
			drain(ctx, r, buf)
		}
	}
}

// drain reads and drops what r sends until it has sent nothing for setAside. It reads no faster than
// the audio would play: librespot writes as fast as it is read, and drained flat out it would race
// through the listener's queue while the speaker plays something else.
func drain(ctx context.Context, r *os.File, buf []byte) {
	const bytesPerSecond = audioRate * audioChannels * 2
	start, read := time.Now(), int64(0)
	for ctx.Err() == nil {
		switch ahead := time.Duration(read)*time.Second/bytesPerSecond - time.Since(start); {
		case ahead > 0:
			select {
			case <-ctx.Done():
				return
			case <-time.After(ahead):
			}
		case ahead < -time.Second:
			// Behind (the sender stalled): paced from now, rather than read flat out to catch up.
			start, read = time.Now(), 0
		}
		_ = r.SetReadDeadline(time.Now().Add(setAside))
		n, err := r.Read(buf)
		read += int64(n)
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) && ctx.Err() == nil {
				slog.Warn("streaming: reading a receiver's audio failed", "err", err)
			}
			return
		}
	}
}

// pipeSource is one track's view of a receiver's pipe: what was read before it started, then the pipe
// itself. Closing it ends the track's reads without closing the pipe, which the next track reads on.
type pipeSource struct {
	f       *os.File
	mu      sync.Mutex
	pending []byte
	closed  bool
	quiet   bool // the track's last read found the receiver quiet
	gain    func() float64
	once    sync.Once
	done    chan struct{}
}

func (s *pipeSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, os.ErrClosed
	}
	// A lone byte kept back by scale is half a sample: it leads the next read from the pipe rather
	// than being handed out alone, which scale would only keep back again.
	if len(s.pending) > 1 || (len(s.pending) == 1 && s.gain == nil) {
		n := copy(p, s.pending)
		s.pending = s.pending[n:]
		n = s.scale(p, n)
		s.mu.Unlock()
		return n, nil
	}
	lead := 0
	if len(s.pending) == 1 && len(p) > 1 {
		p[0], s.pending, lead = s.pending[0], nil, 1
	}
	s.mu.Unlock()
	n, err := s.f.Read(p[lead:])
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		// Over while the read waited: what it took belongs to whatever reads the pipe next, and is
		// dropped rather than queued to a track that has ended.
		return 0, os.ErrClosed
	case errors.Is(err, os.ErrDeadlineExceeded):
		s.quiet = true
	}
	return s.scale(p, lead+n), err
}

// scale applies the gain to the n bytes read into p and says how many to hand on. Samples are two
// bytes, and a pipe may end a read between them, so an odd byte waits in pending for its other half
// whatever the gain is: one read out of step would put every sample after it out of step. Called with
// mu held.
func (s *pipeSource) scale(p []byte, n int) int {
	if s.gain == nil || n <= 0 {
		return n
	}
	if n%2 == 1 {
		n--
		s.pending = append([]byte{p[n]}, s.pending...)
	}
	g := s.gain()
	if g == 1 {
		return n
	}
	for i := 0; i < n; i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(p[i:]))) * g
		v = math.Max(math.Min(math.Round(v), math.MaxInt16), math.MinInt16)
		binary.LittleEndian.PutUint16(p[i:], uint16(int16(v)))
	}
	return n
}

// wentQuiet is whether the track ended on the receiver going quiet, not by being closed.
func (s *pipeSource) wentQuiet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quiet
}

func (s *pipeSource) SetReadDeadline(t time.Time) error { return s.f.SetReadDeadline(t) }

func (s *pipeSource) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		// A read waiting on the pipe is let go now, rather than whenever the receiver next writes.
		_ = s.f.SetReadDeadline(time.Now())
		close(s.done)
	})
	return nil
}
