package assistant

import (
	"testing"
	"time"
)

// Durations go to the model as they are said, not as Go writes them: "2m0s" came back from it as a
// timer's label.
func TestWordsSaysADuration(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{2 * time.Minute, "2 minutes"},
		{time.Minute + 30*time.Second, "1 minute 30 seconds"},
		{time.Hour + 5*time.Minute + 9*time.Second, "1 hour 5 minutes"},
		{40 * time.Second, "40 seconds"},
		{0, "0 seconds"},
	} {
		if got := words(c.d); got != c.want {
			t.Errorf("words(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// What is spoken is plain: no markdown, no typographic punctuation for the voice to trip on.
func TestSpokenIsPlain(t *testing.T) {
	if got := spoken("**It’s** eight thirty‑two."); got != "It's eight thirty-two." {
		t.Errorf("spoken = %q", got)
	}
}
