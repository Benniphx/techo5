//go:build !dot

package display

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

// The screen stands apart from the room's lights unless asked otherwise (#179): its light is described
// as a configuration entity, which Home Assistant does not hand to Assist when it first sees it, and the
// setting changes that only while nothing is connected. The light keeps its object ID whichever way, so
// its entity ID in Home Assistant, and the automations that use it, stay as they were.
func TestTheScreenIgnoresRoomLightsUntilAsked(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{
		poke: make(chan struct{}, 1),
		view: voice.State{Phase: "idle"},
		light: &esphome.Light{
			Base:                esphome.Base{ObjectID: "screen", Name: "Screen", Icon: "mdi:monitor"},
			SupportedColorModes: []esphome.ColorMode{esphome.ColorModeBrightness},
		},
	}
	d.buildRoomLights()
	d.roomSw.Set(!config.Get().Screen.RoomLights)
	d.describeScreen()
	if !d.roomSw.Get() || d.light.Category != esphome.CategoryConfig {
		t.Fatalf("a new device: setting %v, light category %v; want on, and a configuration entity", d.roomSw.Get(), d.light.Category)
	}

	d.roomSw.OnCommand(false)
	if !config.Get().Screen.RoomLights || d.roomSw.Get() {
		t.Fatal("turning the setting off did not save it")
	}
	if d.light.Category != esphome.CategoryConfig {
		t.Fatal("the light's description changed while a connection could have been reading it")
	}
	component.Between.Emit(struct{}{})
	if d.light.Category != esphome.CategoryNone {
		t.Errorf("light category %v between connections with the setting off; want none, as it always was", d.light.Category)
	}
	if d.light.ObjectID != "screen" {
		t.Errorf("the light's object ID is %q; Home Assistant would take it for a new entity", d.light.ObjectID)
	}

	d.roomSw.OnCommand(true)
	component.Between.Emit(struct{}{})
	if config.Get().Screen.RoomLights || d.light.Category != esphome.CategoryConfig {
		t.Error("turning the setting back on did not put the light back among the configuration")
	}
}

// Screen power is the screen's own off and on: off puts the light out with it, and on lights it again at
// the brightness it was left at, not at some default.
func TestScreenPowerIsTheLightsOffAndOn(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{
		poke: make(chan struct{}, 1),
		view: voice.State{Phase: "idle"},
		light: &esphome.Light{
			Base:                esphome.Base{ObjectID: "screen", Name: "Screen", Icon: "mdi:monitor"},
			SupportedColorModes: []esphome.ColorMode{esphome.ColorModeBrightness},
		},
	}
	d.buildRoomLights()
	d.apply(true, 37, true)
	if !d.power.Get() {
		t.Fatal("Screen power is off with the screen lit")
	}

	d.power.OnCommand(false)
	if d.light.Get().On || d.power.Get() {
		t.Fatalf("after Screen power off: light on %v, switch %v", d.light.Get().On, d.power.Get())
	}
	d.power.OnCommand(true)
	got := d.light.Get()
	if !got.On || !d.power.Get() {
		t.Fatalf("after Screen power on: light on %v, switch %v", got.On, d.power.Get())
	}
	if pct := int(got.Brightness*100 + 0.5); pct != 37 {
		t.Errorf("the screen came back at %d%%; want the 37%% it was left at", pct)
	}
}
