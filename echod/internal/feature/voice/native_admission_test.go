package voice

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

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

func TestNativeAndUpstreamRealtimeKeepTheirSelectedOwner(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	v := &Voice{
		vs:       &esphome.VoiceSatellite{ActiveWakeWords: []string{"test"}},
		turn:     &conversation{realtime: newRealtime(func(event) {})},
		realtime: &nativeRealtime{},
	}
	if err := config.Set().Wake(0).Realtime(true); err != nil {
		t.Fatal(err)
	}
	if v.nativeRealtimeStart() || v.CanAnswer(0) {
		t.Fatal("an unconfigured upstream slot was claimed by a voice backend")
	}
	if err := config.Set().Brain().Set(config.Brain{Mode: config.BrainRealtime}); err != nil {
		t.Fatal(err)
	}
	if !v.nativeRealtimeStart() || !v.CanAnswer(0) {
		t.Fatal("the upstream slot hid the existing native voice selection")
	}
	if err := config.Set().Brain().Set(config.Brain{}); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Realtime().Set("openai", "", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Realtime().SetKey("test-key-not-a-credential"); err != nil {
		t.Fatal(err)
	}
	if v.nativeRealtimeStart() || !v.CanAnswer(0) {
		t.Fatal("the native backend intercepted an explicitly selected upstream slot")
	}
	v.realtime.busy.Store(true)
	if !v.nativeRealtimeStart() {
		t.Fatal("a configuration change started another backend over the active native session")
	}
}

func TestNativeRealtimeRejectsSavedPageReader(t *testing.T) {
 config.Use(filepath.Join(t.TempDir(), "state.json"))
 if err := config.Set().Brain().Set(config.Brain{Mode: config.BrainRealtime, Reader: "http://localhost:8891"}); err != nil { t.Fatal(err) }
 if _, err := readRealtimeConfig(); err == nil { t.Fatal("native Realtime accepted a direct page-reader setting") }
}
