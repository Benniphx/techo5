package voice

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestNativeRealtimeAdmissionKeepsStockAndBusyOwners(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	v := &Voice{realtime: &nativeRealtime{}}
	if v.nativeRealtimeStart() {
		t.Fatal("stock wake/button dispatch was intercepted")
	}
	// An existing native session owns wake/button input even after its mode
	// changes. A nil stock turn catches accidental fallthrough or restarting.
	v.realtime.busy.Store(true)
	if !v.nativeRealtimeStart() {
		t.Fatal("existing Realtime session lost input ownership")
	}
}
