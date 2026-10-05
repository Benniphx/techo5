//go:build panel

package firmware

import "github.com/HuskerMinion/techo5/echod/internal/lib/panelctl"

// askApp has the panel app look for an update: the app carries this daemon and is updated as a whole,
// from the release server it was built for, and installs what it finds by itself.
func askApp() { panelctl.Tell("update check") }
