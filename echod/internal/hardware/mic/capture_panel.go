//go:build panel

package mic

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// micSocket is the abstract socket the app serves the microphone on. The app reads it from Android
// as 16 kHz mono S16LE and writes it here as it comes; nothing else is said on it.
const micSocket = "@techo5.mic"

// dialWait is how long the daemon waits for the app's socket before giving up on this start. The app
// starts the daemon after the socket is listening, so this only covers a slow start.
const dialWait = 10 * time.Second

// socketCapture turns the app's mono S16LE stream into the frames the Source expects: S24_3LE, the
// microphone in ch0 and a silent reference in ch1.
type socketCapture struct {
	conn net.Conn
	in   []byte
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
	}
	in := c.in[:frames*2]
	if _, err := io.ReadFull(c.conn, in); err != nil {
		return 0, err
	}
	for f := range frames {
		// A 16-bit sample is the top two bytes of a 24-bit one.
		o := f * frameBytes
		p[o], p[o+1], p[o+2] = 0, in[2*f], in[2*f+1]
		p[o+3], p[o+4], p[o+5] = 0, 0, 0
	}
	return frames * frameBytes, nil
}

func (c *socketCapture) Close() error { return c.conn.Close() }
