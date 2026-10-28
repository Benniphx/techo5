//go:build !dot

package display

import (
	"testing"
	"time"
)

// Somebody near holds the screensaver off and ends one that is up; without them it waits as before.
func TestSaverIdleSince(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	wait := 5 * time.Minute
	up := t0.Add(-10 * time.Minute) // the wait ran out long ago: the screensaver is up

	for _, c := range []struct {
		name                 string
		since                time.Time
		boring, cue, present bool
		want                 time.Time
	}{
		{"another page stops the wait", up, false, false, false, time.Time{}},
		{"the first idle frame starts it", time.Time{}, true, false, false, t0},
		{"an empty room keeps waiting", up, true, false, false, up},
		{"somebody near ends the screensaver", up, true, false, true, t0},
		{"somebody near holds the wait", t0.Add(-time.Minute), true, false, true, t0},
		{"a turn under a cue restarts the wait", t0.Add(-time.Minute), true, true, false, t0},
		{"a cue over the screensaver leaves it up", up, true, true, false, up},
	} {
		if got := saverIdleSince(c.since, t0, c.boring, c.cue, c.present, wait); !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
