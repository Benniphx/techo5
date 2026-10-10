//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// A tap on the night light opens the dashboard when a tap on the clock does, and only then: left
// alone otherwise, as a hand in the dark is more likely a knock (#84). Once open, the dashboard is
// shown over the red night clock.
func TestATapAtNightOpensTheDashboardWhenChosen(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Dashboard().Mode(config.DashboardDrawn); err != nil {
		t.Fatal(err)
	}
	tap := touch.Gesture{Kind: touch.Tap, X: 480, Y: 240}

	d := &Display{on: true, nightGlow: true, poke: make(chan struct{}, 1)}
	d.gesture(tap)
	if d.dash {
		t.Fatal("a tap on the night light opened the dashboard with the clock's tap left as Assist")
	}

	if err := config.Set().Screen().ClockTap("dashboard"); err != nil {
		t.Fatal(err)
	}
	d.gesture(tap)
	if !d.dash || !d.dashNight || !d.nightGlow {
		t.Fatalf("with the clock's tap on Dashboard: dashboard %v (night %v), night light %v; want all", d.dash, d.dashNight, d.nightGlow)
	}

	// A dashboard opened by day, or by Home Assistant, is not the night one: it stays under the night
	// clock, and its tiles don't work in the night light.
	day := &Display{on: true, poke: make(chan struct{}, 1)}
	day.openDashboard()
	day.dashboardAsked(true)
	if day.dashNight {
		t.Error("a dashboard opened by day or by Home Assistant counts as the night one")
	}

	r := testRenderer()
	night := scene{now: time.Now(), phase: "idle", redClock: true}
	r.draw(night)
	clock := append([]uint8(nil), r.dst.Pix...)
	asked := night
	asked.showDash, asked.dashAsked, asked.dashMode = true, true, config.DashboardDrawn
	r.draw(asked)
	if same(r.dst.Pix, clock) {
		t.Error("the dashboard asked for at night is hidden under the red clock")
	}
	idle := night
	idle.showDash, idle.dashMode = true, config.DashboardDrawn // standing in for the clock, not asked for
	r.draw(idle)
	if !same(r.dst.Pix, clock) {
		t.Error("a dashboard standing in for the clock took the night clock's place")
	}
}
