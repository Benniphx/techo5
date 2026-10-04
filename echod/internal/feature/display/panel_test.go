//go:build panel

package display

import "github.com/HuskerMinion/techo5/echod/internal/lib/panelscreen"

// No app serves the screen in a test: every backlight and frame would wait out the whole dial.
func init() { panelscreen.DialWait = 0 }
