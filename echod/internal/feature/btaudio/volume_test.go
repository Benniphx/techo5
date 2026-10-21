//go:build linux

package btaudio

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/bluealsa"
)

// fakeAlsa records what a stream's volume mode set.
type fakeAlsa struct {
	soft   []bool
	levels []int
}

func (a *fakeAlsa) SetSoftVolume(_ bluealsa.PCM, on bool) error {
	a.soft = append(a.soft, on)
	return nil
}

func (a *fakeAlsa) SetLevel(_ bluealsa.PCM, level int) error {
	a.levels = append(a.levels, level)
	return nil
}

func volumeFeature(t *testing.T) *Feature {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	was := volumeNow
	volumeNow = func() int { return 12 }
	t.Cleanup(func() { volumeNow = was })
	f := &Feature{levels: map[string]int{}, ours: map[string]time.Time{}, shared: map[string]bool{},
		setsVolume: newSetsVolumeSwitch()}
	f.setsVolume.OnCommand = func(on bool) { _ = f.SetSetsVolume(on) }
	return f
}

// phonePCM is a phone's stream at level on both channels.
func phonePCM(level int) bluealsa.PCM {
	return bluealsa.PCM{Path: "/org/bluealsa/hci0/dev_X/a2dpsrc/source", Volume: uint16(level)<<8 | uint16(level)}
}

// Bluetooth sets the volume is on by default, so nothing changes on an update, and off with its switch,
// saved as the speaker's (#245).
func TestTheBluetoothVolumeSetting(t *testing.T) {
	f := volumeFeature(t)
	if !bluetoothSetsVolume() || !f.SetsVolume() {
		t.Fatal("off on a new device")
	}
	f.Restore(config.Get())
	if !f.setsVolume.Get() {
		t.Error("the switch is off on a new device")
	}
	f.setsVolume.OnCommand(false)
	if bluetoothSetsVolume() || f.setsVolume.Get() || !config.Get().Speaker.NoBluetoothVolume {
		t.Error("still on after the switch went off")
	}
	f.setsVolume.OnCommand(true)
	if !bluetoothSetsVolume() || !f.setsVolume.Get() || config.Get().Speaker.NoBluetoothVolume {
		t.Error("still off after the switch went on")
	}
}

// With the setting off a phone's level never sets the device's volume, however it moved; on, it does
// as sliderMove says.
func TestSliderSetsOnlyWhenShared(t *testing.T) {
	for _, c := range []struct {
		level, last int
		known, ours bool
	}{
		{127, 60, true, false}, {60, 127, true, false}, {levelFor(20), levelFor(8), true, true}, {80, 0, false, false},
	} {
		if sliderSets(false, c.level, c.last, c.known, c.ours) {
			t.Errorf("%+v: set the volume with the setting off", c)
		}
		if got, want := sliderSets(true, c.level, c.last, c.known, c.ours), sliderMove(c.level, c.last, c.known, c.ours); got != want {
			t.Errorf("%+v: %v with the setting on, want %v", c, got, want)
		}
	}
}

// A stream's volume mode is set again when it was never set, or was set the other way.
func TestModeStale(t *testing.T) {
	for name, c := range map[string]struct {
		was, applied, shared, want bool
	}{
		"never set, on":      {false, false, true, true},
		"never set, off":     {false, false, false, true},
		"set on, still on":   {true, true, true, false},
		"set off, still off": {false, true, false, false},
		"set on, turned off": {true, true, false, true},
		"set off, turned on": {false, true, true, true},
	} {
		if got := modeStale(c.was, c.applied, c.shared); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// Shared, bluez-alsa leaves the audio unscaled and the phone is handed the device's level; not shared,
// bluez-alsa scales the audio by the phone's own level and nothing is handed to the phone.
func TestVolumeMode(t *testing.T) {
	f := volumeFeature(t)
	p := phonePCM(127)
	path := string(p.Path)

	al := &fakeAlsa{}
	f.volumeMode(al, p, true, 12)
	if len(al.soft) != 1 || al.soft[0] || len(al.levels) != 1 || al.levels[0] != levelFor(12) {
		t.Errorf("shared: soft volume %v, levels %v", al.soft, al.levels)
	}
	if f.levels[path] != levelFor(12) || !f.shared[path] || f.ours[path].IsZero() {
		t.Errorf("shared: level %d, shared %v, ours %v", f.levels[path], f.shared[path], f.ours[path])
	}

	al = &fakeAlsa{}
	f.volumeMode(al, p, false, 12)
	if len(al.soft) != 1 || !al.soft[0] || len(al.levels) != 0 {
		t.Errorf("own: soft volume %v, levels %v", al.soft, al.levels)
	}
	if _, ours := f.ours[path]; f.levels[path] != 127 || f.shared[path] || ours {
		t.Errorf("own: level %d, shared %v, ours %v", f.levels[path], f.shared[path], ours)
	}
}

// The switch turned off while a phone plays: the next look puts the stream over to its own volume and
// hands the phone nothing, and a phone then reporting full level leaves the device's volume alone (it
// would have reached media.Set, which this test has none of). Turned on again, the next look hands
// the phone the device's level.
func TestALookFollowsTheSwitch(t *testing.T) {
	f := volumeFeature(t)
	p := phonePCM(levelFor(12))
	al := &fakeAlsa{}
	f.volumeMode(al, p, true, 12)

	if err := config.Set().Speaker().NoBluetoothVolume(true); err != nil {
		t.Fatal(err)
	}
	al = &fakeAlsa{}
	f.phoneSlider(al, p)
	if len(al.soft) != 1 || !al.soft[0] || len(al.levels) != 0 {
		t.Fatalf("turned off: soft volume %v, levels %v", al.soft, al.levels)
	}
	f.phoneSlider(al, phonePCM(127))
	f.phoneSlider(al, phonePCM(40))
	if len(al.soft) != 1 || len(al.levels) != 0 {
		t.Errorf("off, a look set more: soft volume %v, levels %v", al.soft, al.levels)
	}
	if f.levels[string(p.Path)] != 40 {
		t.Errorf("the phone's level not noted: %d", f.levels[string(p.Path)])
	}

	if err := config.Set().Speaker().NoBluetoothVolume(false); err != nil {
		t.Fatal(err)
	}
	f.phoneSlider(al, phonePCM(40))
	if len(al.soft) != 2 || al.soft[1] || len(al.levels) != 1 || al.levels[0] != levelFor(12) {
		t.Errorf("turned on: soft volume %v, levels %v", al.soft, al.levels)
	}
}
