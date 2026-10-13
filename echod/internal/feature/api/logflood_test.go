package api

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A flood of closed connections keeps a few lines a minute, one warning, and a count when the minute
// is up; other lines pass untouched, and loggers made With attributes share the count.
func TestFloodGuard(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	st := &floodState{now: func() time.Time { return now }}
	log := slog.New(floodGuard{slog.NewTextHandler(&buf, nil), st})
	for i := range 50 {
		log.With("peer", "ha:"+string(rune('a'+i%26))).Info("connection closed", "err", "broken pipe")
		now = now.Add(50 * time.Millisecond)
	}
	log.Info("something else")
	if got := strings.Count(buf.String(), "msg=\"connection closed\""); got != closedSeen {
		t.Errorf("%d closed lines passed, want %d", got, closedSeen)
	}
	if strings.Count(buf.String(), "keeps reconnecting") != 1 {
		t.Errorf("want one warning: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "something else") {
		t.Error("another line was held back")
	}
	now = now.Add(time.Minute)
	log.Info("connection closed", "err", "reset")
	if !strings.Contains(buf.String(), "closed=50") || !strings.Contains(buf.String(), "last_err=\"broken pipe\"") {
		t.Errorf("no count for the minute: %s", buf.String())
	}
}
