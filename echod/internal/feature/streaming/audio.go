package streaming

import (
	"context"
	"errors"
	"log/slog"
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
// takes the speaker. r stays open across tracks and across the program's restarts.
func pump(ctx context.Context, name string, r *os.File) {
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
		src := &pipeSource{f: r, pending: append([]byte(nil), buf[:n]...), done: make(chan struct{})}
		slog.Info("streaming: audio arrived", "from", name)
		play(name, src, audioRate, audioChannels)
		select {
		case <-ctx.Done():
			src.Close()
			return
		case <-src.done:
		}
		// The track is over: the receiver went quiet, or something else was played. Either way, what it
		// sends now is set aside until it has been quiet a moment.
		drain(ctx, r, buf)
	}
}

// drain reads and drops what r sends until it has sent nothing for setAside.
func drain(ctx context.Context, r *os.File, buf []byte) {
	for ctx.Err() == nil {
		_ = r.SetReadDeadline(time.Now().Add(setAside))
		if _, err := r.Read(buf); err != nil {
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
	once    sync.Once
	done    chan struct{}
}

func (s *pipeSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, os.ErrClosed
	}
	if len(s.pending) > 0 {
		n := copy(p, s.pending)
		s.pending = s.pending[n:]
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()
	return s.f.Read(p)
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
