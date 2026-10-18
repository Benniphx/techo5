//go:build panel

package all

import "github.com/HuskerMinion/techo5/echod/internal/lib/panelscreen"

// No app answers in a test: the screen gives up on it at once rather than waiting for it at every
// restore of every setting (panelscreen.DialWait), which took this package past its ten minutes.
func init() { panelscreen.DialWait = 0 }

// notOnPanel is what a TECHO5 Panel leaves out of the Show's list. Nothing yet.
var notOnPanel []string

// onlyOnPanel is what a panel adds: Android can silence a tablet's microphone under the app.
var onlyOnPanel = []string{"microphone_silent"}
