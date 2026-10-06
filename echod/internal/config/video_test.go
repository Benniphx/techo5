package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

// Videos are off on a new device, and allowed addresses are kept once each, newest last, up to
// MostAllowed, and a snapshot does not share the store's list.
func TestVideoSettings(t *testing.T) {
	Use(filepath.Join(t.TempDir(), "state.json"))
	if v := Get().Video; v.On || v.DLNA || len(v.Allowed) != 0 {
		t.Fatalf("a new device has videos set: %+v", v)
	}
	for i := range MostAllowed + 3 {
		if err := Set().Video().Allow(fmt.Sprintf("192.168.1.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Set().Video().Allow("192.168.1.10"); err != nil { // already there
		t.Fatal(err)
	}
	v := Get().Video
	if len(v.Allowed) != MostAllowed || v.Allowed[0] != "192.168.1.3" || v.Allowed[MostAllowed-1] != fmt.Sprintf("192.168.1.%d", MostAllowed+2) {
		t.Errorf("allowed: %v", v.Allowed)
	}
	if !v.IsAllowed("192.168.1.10") || v.IsAllowed("192.168.1.0") || v.IsAllowed("") {
		t.Error("IsAllowed is wrong")
	}
	v.Allowed[0] = "changed"
	if Get().Video.Allowed[0] == "changed" {
		t.Error("a snapshot shares the store's list")
	}
	if err := Set().Video().ForgetAllowed(); err != nil {
		t.Fatal(err)
	}
	if len(Get().Video.Allowed) != 0 {
		t.Error("forgetting kept some")
	}
}
