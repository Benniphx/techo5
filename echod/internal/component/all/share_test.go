package all

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/remote"
	"github.com/HuskerMinion/techo5/echod/internal/feature/share"
)

// Every setting that can be shared is a setting, and changing it touches nothing a shared setting must
// never touch: what is this device's own (config.KeepOwn: its doors, microphones and camera, what it is in
// the middle of), its secrets, where its voice, words or photos go, its name, or its sharing.
func TestSharedSettingsTouchNothingOfTheDevicesOwn(t *testing.T) {
	byID := map[string]esphome.Entity{}
	for _, e := range component.Default().Entities() {
		switch v := e.(type) {
		case *esphome.Switch:
			byID[v.ObjectID] = e
		case *esphome.Number:
			byID[v.ObjectID] = e
		case *esphome.Select:
			byID[v.ObjectID] = e
		}
	}
	for _, id := range share.IDs() {
		e, ok := byID[id]
		if !ok {
			if len(notOnThisDevice) == 0 || !contains(notOnThisDevice, id) {
				t.Errorf("%s is on the share list but not on this device", id)
			}
			continue
		}
		config.Use(filepath.Join(t.TempDir(), "state.json"))
		component.Default().Restore(config.Get())
		before := config.Get()

		done := make(chan struct{})
		go func() { defer close(done); change(e) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: its command did not come back", id)
		}
		after := config.Get()

		if len(config.Changed(before, after)) == 0 {
			t.Errorf("%s: changing it changed no setting, so it is no setting to share", id)
		}
		own := after
		config.KeepOwn(&own, before)
		if !reflect.DeepEqual(own, after) {
			t.Errorf("%s: changing it changes something that is the device's own", id)
		}
		sec := after
		config.KeepSecrets(&sec, before)
		if !reflect.DeepEqual(sec, after) {
			t.Errorf("%s: changing it changes a secret", id)
		}
		if c := config.ServerChanges(before, after); len(c) > 0 {
			t.Errorf("%s: changing it changes where something goes: %v", id, c)
		}
		if before.Device != after.Device || !reflect.DeepEqual(before.Share, after.Share) {
			t.Errorf("%s: changing it changes the device's name or its sharing", id)
		}
	}
	if share.Shareable("settings_sharing") {
		t.Error("sharing itself is on the list")
	}
	// What the remote API may not change, sharing may not either.
	for id, e := range byID {
		if share.Shareable(id) && !remote.Writable(e) {
			t.Errorf("%s is on the share list but read-only through the remote API", id)
		}
	}
}

// change sets an entity to a value other than the one it has, through its own command.
func change(e esphome.Entity) {
	switch v := e.(type) {
	case *esphome.Switch:
		v.OnCommand(!v.Get())
	case *esphome.Number:
		n := float64(v.Get()) + v.Step
		if n > v.Max {
			n = float64(v.Get()) - v.Step
		}
		v.OnCommand(float32(n))
	case *esphome.Select:
		for _, o := range v.Options {
			if o != v.Get() {
				v.OnCommand(o)
				return
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
