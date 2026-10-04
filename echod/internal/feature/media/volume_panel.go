//go:build panel

package media

import (
	"strconv"

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
		if step != Get().Volume() {
			Get().Set(step)
		}
	})
}
