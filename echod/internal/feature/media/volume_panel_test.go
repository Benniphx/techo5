//go:build panel

package media

import "testing"

// A press of the tablet's buttons while a ring has Android raised moves the music by the press, not
// to the ring's level; with nothing raised Android's level is the music's.
func TestAPressDuringARingMovesTheMusicByThePress(t *testing.T) {
	for _, c := range []struct {
		music, reported, told int
		raised                bool
		want                  int
	}{
		{3, 24, 25, false, 24}, // no ring: Android's level is the music's
		{3, 4, 3, false, 4},
		{3, 24, 25, true, 2}, // a press down under a ring at 25
		{3, 26, 25, true, 4}, // and up
		{3, 25, 25, true, 3}, // Android repeating the ring's level is no press
		{0, 20, 25, true, 0},
		{28, 30, 29, true, 29},
	} {
		if got := fromAndroid(c.music, c.reported, c.told, c.raised); got != c.want {
			t.Errorf("music at %d, Android told %d (raised %v) and reporting %d: music to %d, want %d",
				c.music, c.told, c.raised, c.reported, got, c.want)
		}
	}
}
