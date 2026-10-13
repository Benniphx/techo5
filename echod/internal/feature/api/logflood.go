package api

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Home Assistant that drops its connection as soon as it is made (#136: it reconnected some 20 times a
// second) put a "connection closed" line in the log for every try. The log is kept short, and the
// diagnostics carry only its last lines, so within a minute or so the flood had pushed out whatever
// came before it, which is what would say why. floodGuard lets the first few through each minute,
// says once that Home Assistant keeps reconnecting, and counts the rest, saying how many when the
// minute is up.

// closedSeen is how many "connection closed" lines a minute pass through before the rest are counted.
const closedSeen = 3

// floodWindow is the minute.
const floodWindow = time.Minute

// closes is the count, shared by every server the device listens with.
var closes = &floodState{now: time.Now}

type floodState struct {
	now func() time.Time

	mu      sync.Mutex
	since   time.Time
	n       int
	lastErr string
}

// floodGuard is a handler that passes everything on but the "connection closed" lines past
// closedSeen in a minute.
type floodGuard struct {
	slog.Handler
	st *floodState
}

func (h floodGuard) Handle(ctx context.Context, r slog.Record) error {
	if r.Message != "connection closed" {
		return h.Handler.Handle(ctx, r)
	}
	errText := ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "err" {
			errText = a.Value.String()
			return false
		}
		return true
	})
	st := h.st
	st.mu.Lock()
	now := st.now()
	var summary *slog.Record
	if st.since.IsZero() || now.Sub(st.since) >= floodWindow {
		if st.n > closedSeen {
			s := slog.NewRecord(now, slog.LevelWarn, "home assistant kept reconnecting", 0)
			s.AddAttrs(slog.Int("closed", st.n), slog.Duration("within", now.Sub(st.since).Round(time.Second)),
				slog.String("last_err", st.lastErr))
			summary = &s
		}
		st.since, st.n = now, 0
	}
	st.n++
	st.lastErr = errText
	n := st.n
	st.mu.Unlock()
	if summary != nil {
		_ = h.Handler.Handle(ctx, *summary)
	}
	switch {
	case n <= closedSeen:
		return h.Handler.Handle(ctx, r)
	case n == closedSeen+1:
		w := slog.NewRecord(r.Time, slog.LevelWarn, "home assistant keeps reconnecting; further closed connections this minute are counted", 0)
		w.AddAttrs(slog.String("err", errText))
		return h.Handler.Handle(ctx, w)
	}
	return nil
}

func (h floodGuard) WithAttrs(attrs []slog.Attr) slog.Handler {
	return floodGuard{h.Handler.WithAttrs(attrs), h.st}
}

func (h floodGuard) WithGroup(name string) slog.Handler {
	return floodGuard{h.Handler.WithGroup(name), h.st}
}
