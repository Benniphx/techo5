//go:build !dot && !spot

package layout

import (
	"fmt"
	"testing"
)

func fmtSscanHex(s string, b *byte) (int, error) { return fmt.Sscanf(s, "%02x", b) }

// The Skylight's MAC comes from its eMMC CID: the same CID always gives the same address, two CIDs
// give two, and every one is locally administered and unicast. Anything that is not a CID gives none.
func TestTheSkylightMACComesFromTheCID(t *testing.T) {
	a, okA := macFromCID("150100414a544434520a1b2c3d4e5f60\n")
	again, _ := macFromCID("150100414A544434520A1B2C3D4E5F60")
	b, okB := macFromCID("150100414a544434520a1b2c3d4e5f61")
	if !okA || !okB {
		t.Fatalf("a CID gave no address: %q %v, %q %v", a, okA, b, okB)
	}
	if a != again {
		t.Errorf("the same CID gave %q and %q", a, again)
	}
	if a == b {
		t.Errorf("two CIDs gave the same address %q", a)
	}
	for _, m := range []string{a, b} {
		if MAC(m) == "" {
			t.Errorf("%q is not an address", m)
		}
		var first byte
		if _, err := fmtSscanHex(m[:2], &first); err != nil || first&0x03 != 0x02 {
			t.Errorf("%q is not locally administered and unicast", m)
		}
	}
	for _, bad := range []string{"", "1234", "zz0100414a544434520a1b2c3d4e5f60", "150100414a544434520a1b2c3d4e5f6"} {
		if m, ok := macFromCID(bad); ok {
			t.Errorf("%q gave %q", bad, m)
		}
	}
}
