package media

import (
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// Home Assistant's stop with nothing of the player's own playing or paused is a stop like one here:
// what would start something again in a moment (a dropped station's put-back) is told (OnStop).
func TestHomeAssistantsStopWithNothingPlayingIsAStop(t *testing.T) {
	d := speaker.NewDriver(speaker.New())
	p := &Player{stream: NewStream(d, speaker.New(), func() {}, func(string) {}), mp: &esphome.MediaPlayer{}}
	stopped := 0
	p.OnStop.Listen(func(struct{}) { stopped++ })
	p.command(esphome.MediaCommand{HasCommand: true, Command: esphome.MediaPlayerStop})
	if stopped != 1 {
		t.Errorf("a stop from Home Assistant with nothing playing was told %d times, want once", stopped)
	}
}
