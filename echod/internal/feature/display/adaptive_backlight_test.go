//go:build !dot && !spot

package display

import (
	"slices"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
)

// Adaptive backlight is offered only while the GPU draws the panel: on the cores there is nothing to
// draw the pixels brighter with.
func TestAdaptiveBacklightOnlyWhereTheGPUDraws(t *testing.T) {
	defer func(f func() bool) { liftOffered = f }(liftOffered)
	d := Get()
	for _, gpu := range []bool{false, true} {
		liftOffered = func() bool { return gpu }
		has := slices.Contains(d.Entities(), esphome.Entity(d.liftSw))
		if has != gpu {
			t.Errorf("GPU drawing %v: Adaptive backlight offered %v", gpu, has)
		}
	}
}

// A GPU that takes the panel after Home Assistant was given the entity list asks for one reconnect, so
// Adaptive backlight shows up without a restart; once it is listed, nothing more is asked.
func TestAdaptiveBacklightListedWhenTheGPUComesLate(t *testing.T) {
	defer func(f func() bool) { liftOffered = f }(liftOffered)
	d := Get()
	asked := 0
	defer component.Reconnect.Listen(func(struct{}) { asked++ })()

	liftOffered = func() bool { return false }
	d.Entities()
	d.offerLiftLate()
	if asked != 0 {
		t.Fatalf("asked for a reconnect with no GPU: %d", asked)
	}
	liftOffered = func() bool { return true }
	d.offerLiftLate()
	d.offerLiftLate()
	if asked != 1 {
		t.Fatalf("the GPU came late: %d reconnects asked, want 1", asked)
	}
	d.Entities()
	d.offerLiftLate()
	if asked != 1 {
		t.Errorf("asked again after the switch was listed: %d", asked)
	}
}
