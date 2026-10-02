package media

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// As quiet hours start a loud device comes down to the night volume and remembers where it was; a quiet
// one, one with no limit, or one already turned down is left alone.
func TestNightTurnsDownOnce(t *testing.T) {
	for _, c := range []struct {
		limit, step, day, to, keep int
	}{
		{limit: 8, step: 20, day: 0, to: 8, keep: 20},
		{limit: 8, step: 5, day: 0, to: 5, keep: 0},
		{limit: 8, step: 8, day: 0, to: 8, keep: 0},
		{limit: 0, step: 20, day: 0, to: 20, keep: 0},
		{limit: 8, step: 20, day: 12, to: 20, keep: 12}, // turned up overnight after it came down
	} {
		if to, keep := turnDown(c.limit, c.step, c.day); to != c.to || keep != c.keep {
			t.Errorf("turnDown(%d, %d, %d) = %d, %d; want %d, %d", c.limit, c.step, c.day, to, keep, c.to, c.keep)
		}
	}
}

// As they end it goes back up, unless somebody chose another level in the night; either way nothing is
// remembered after.
func TestNightTurnsBackUpUnlessChanged(t *testing.T) {
	for _, c := range []struct {
		limit, step, day, to int
	}{
		{limit: 8, step: 8, day: 20, to: 20},
		{limit: 8, step: 14, day: 20, to: 14}, // somebody turned it up
		{limit: 8, step: 3, day: 20, to: 3},   // or down
		{limit: 8, step: 8, day: 0, to: 8},    // it was never turned down
	} {
		if to, keep := turnUp(c.limit, c.step, c.day); to != c.to || keep != 0 {
			t.Errorf("turnUp(%d, %d, %d) = %d, %d; want %d, 0", c.limit, c.step, c.day, to, keep, c.to)
		}
	}
}

// An alarm's volume starts from the daytime level, not the night's.
func TestDaytimeIsTheLevelBeforeTheNight(t *testing.T) {
	s := config.Speaker{Volume: 8, DayVolume: 20}
	if got := s.Daytime(); got != 20 {
		t.Errorf("turned down: daytime %d, want 20", got)
	}
	s.DayVolume = 0
	if got := s.Daytime(); got != 8 {
		t.Errorf("not turned down: daytime %d, want 8", got)
	}
}
