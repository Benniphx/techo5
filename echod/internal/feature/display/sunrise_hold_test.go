//go:build !dot

package display

import (
	"testing"
	"time"
)

// heldLight makes the screen see the wake light held at full, as the alarm feature reports it, for
// the length of a test.
func heldLight(t *testing.T, until time.Time, snooze, ok bool) {
	t.Helper()
	wasProgress, wasHold := sunriseProgress, sunriseHold
	t.Cleanup(func() { sunriseProgress, sunriseHold = wasProgress, wasHold })
	sunriseProgress = func(time.Time) float64 {
		if ok {
			return 1
		}
		return 0.4
	}
	sunriseHold = func(time.Time) (time.Time, bool, bool) { return until, snooze, ok }
}

// Held at full after an alarm, the Wake with light row says what it waits for, not that the light is
// coming up (#161).
func TestTheRowSaysWhatHoldsTheLight(t *testing.T) {
	was := clock24.Load()
	t.Cleanup(func() { clock24.Store(was) })
	clock24.Store(false)
	at := time.Date(2026, 10, 16, 8, 30, 0, 0, time.Local)

	for _, tc := range []struct {
		name   string
		until  time.Time
		snooze bool
		ok     bool
		want   string
	}{
		{"coming up", time.Time{}, false, false, "The light is coming up now"},
		{"ringing", time.Time{}, false, true, "On while the alarm rings"},
		{"the next alarm", at, false, true, "On until the 8:30 AM alarm"},
		{"a snooze", at.Add(-21 * time.Minute), true, true, "On until the 8:09 AM snooze"},
	} {
		heldLight(t, tc.until, tc.snooze, tc.ok)
		if got := sunriseSub(); got != tc.want {
			t.Errorf("%s: the row says %q, want %q", tc.name, got, tc.want)
		}
	}
}
