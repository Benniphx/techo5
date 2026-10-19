//go:build panel

package media

import (
	"strconv"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	"github.com/HuskerMinion/techo5/echod/internal/lib/panelctl"
)

// The tablet's own volume buttons, or anything else that moved Android's media volume, come back as
// "volume <step>" and are taken the way Home Assistant's slider is, so Home Assistant sees the change.
func init() {
	panelctl.Handle("volume", func(args []string) {
		if len(args) == 0 {
			return
		}
		step, err := strconv.Atoi(args[0])
		if err != nil {
			return
		}
		told, raised := speaker.AndroidLevel()
		if step = fromAndroid(Get().Volume(), step, told, raised); step != Get().Volume() {
			Get().Set(step)
		}
	})
}

// fromAndroid is the media step a level Android reports stands for. Android reports where its level
// is, and while a ring raised it above the music (raised, to told) that is the ring's level, so a press
// moves the music from where it was by as much as it moved Android: music at 3 under a ring at 25,
// a press down is 24 from Android and 2 for the music.
func fromAndroid(music, reported, told int, raised bool) int {
	if !raised {
		return reported
	}
	return max(0, min(music+reported-told, VolumeSteps))
}
