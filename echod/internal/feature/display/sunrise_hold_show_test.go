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
