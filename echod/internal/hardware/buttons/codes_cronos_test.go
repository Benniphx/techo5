//go:build !dot && !spot

package buttons

import "testing"

// The mute button is KEY_POWER under Amazon's kernels and KEY_MICMUTE on the 2nd gen Show 8's
// mainline kernel; both are the mute, and neither is anything else.
func TestShowMuteCodes(t *testing.T) {
	for code, want := range map[uint16]Name{114: VolumeDown, 115: VolumeUp, 116: Mute, 248: Mute} {
		if got, ok := codes[code]; !ok || got != want {
			t.Errorf("code %d is %q (%v), want %q", code, got, ok, want)
		}
	}
	if len(codes) != 4 {
		t.Errorf("%d codes, want 4: %v", len(codes), codes)
	}
}
