//go:build !dot && !spot

package display

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Alarms card with the wake light held after an alarm, for looking at with SHOW_PREVIEW set.
func TestHeldLightRowDraws(t *testing.T) {
	at := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	for name, hold := range map[string]struct {
		until  time.Time
		snooze bool
	}{
		"settings-alarms-light-held":    {at.Add(18 * time.Minute), false},
		"settings-alarms-light-snoozed": {at.Add(6 * time.Minute), true},
	} {
		heldLight(t, hold.until, hold.snooze, true)
		img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
		newRenderer(img).draw(scene{now: at, phase: "idle", showSheet: true, snooze: 9,
			sheet: settings{cat: catAlarms, cardScroll: 1000}})
		dir := os.Getenv("SHOW_PREVIEW")
		if dir == "" {
			continue
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// The screen turned off under the held wake light stays off for that light, and shows it again once
// it is over or an alarm rings (#161). It used to be turned straight back on.
func TestTheScreenTurnedOffUnderTheHeldLightStaysOff(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 12, 0, 0, time.Local)
	d := &Display{}

	heldLight(t, time.Time{}, false, false)
	d.turnedOff(now)
	if d.wakeLight(now) == 0 {
		t.Error("turned off while the light only comes up, the light is gone from the screen")
	}

	heldLight(t, now.Add(18*time.Minute), false, true)
	d.turnedOff(now)
	if got := d.wakeLight(now); got != 0 {
		t.Errorf("turned off under the held light, the screen still shows %.2f of it", got)
	}
	d.lightOffOver(now, false)
	if d.wakeLight(now) != 0 {
		t.Error("the screen took the light back while it was still held")
	}
	d.lightOffOver(now, true)
	if d.wakeLight(now) != 1 {
		t.Error("an alarm rang and the screen does not show the light after it")
	}

	d.turnedOff(now)
	sunriseProgress = func(time.Time) float64 { return 0 }
	d.lightOffOver(now, false)
	sunriseProgress = func(time.Time) float64 { return 1 }
	if d.wakeLight(now) != 1 {
		t.Error("the light ended and the next one stays off the screen")
	}
}
