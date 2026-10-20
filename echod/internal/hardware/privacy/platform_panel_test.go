//go:build panel

package privacy

import "testing"

// A panel has no mute line of the daemon's own, whatever the tablet's sysfs holds: the Shows' latch
// is never looked for, and nothing seeds a cut that only an Echo's hardware would clear.
func TestAPanelHasNoMuteLine(t *testing.T) {
	if m, err := Microphone(); err == nil || m != nil {
		t.Fatalf("a panel found a mute line: %v, %v", m, err)
	}
	Seed(true)
	if SoftwareCut() {
		t.Error("a panel seeded a software cut")
	}
}
