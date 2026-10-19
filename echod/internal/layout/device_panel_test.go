//go:build panel

package layout

import "testing"

// A panel is no Echo board: main's Show code asks these to pick athena's or the Show 8's paths, and
// on a tablet every one of them has to say no, or the daemon would go looking for hardware an app
// cannot reach.
func TestPanelIsNoShowBoard(t *testing.T) {
	for name, got := range map[string]bool{
		"Checkers":       Checkers(),
		"Crown":          Crown(),
		"Show8":          Show8(),
		"Athena":         Athena(),
		"AthenaMainline": AthenaMainline(),
	} {
		if got {
			t.Errorf("%s() = true on a panel", name)
		}
	}
	if v, ok := idmeFallback("serial"); ok || v != "" {
		t.Errorf("idmeFallback = %q, %v on a panel; want nothing", v, ok)
	}
}
