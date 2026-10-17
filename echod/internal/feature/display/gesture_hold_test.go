//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// A finger held on the plain clock tells Home Assistant while the switch is on, and only then.
func TestAHoldOnTheClockIsTold(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	var told []string
	was := fireGesture
	fireGesture = func(name string) { told = append(told, name) }
	t.Cleanup(func() { fireGesture = was })

	d := &Display{}
	d.view.Phase = "idle"
	d.clockHoldScene(true)
	d.clockHeld()
	if len(told) != 0 {
		t.Errorf("told %v with the switch off", told)
	}
	if err := config.Set().Screen().ClockHold(true); err != nil {
		t.Fatal(err)
	}
	d.clockHoldScene(false) // a page over the clock
	d.clockHeld()
	if len(told) != 0 {
		t.Errorf("told %v with a page over the clock", told)
	}
	d.clockHoldScene(true)
	if !d.clockHolds {
		t.Error("the plain clock with the switch on does not ask for holds")
	}
	d.clockHeld()
	if len(told) != 1 || told[0] != "hold" {
		t.Errorf("told %v, want one hold", told)
	}
}
