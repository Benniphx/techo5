//go:build linux

package btaudio

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/bluez"
	"github.com/godbus/dbus/v5"
)

func TestWhatIsAPhone(t *testing.T) {
	const speaker = "00:11:22:33:44:55"
	source := []string{"0000110A-0000-1000-8000-00805F9B34FB"}
	for name, c := range map[string]struct {
		d    bluez.Device
		want bool
	}{
		"sends audio":           {bluez.Device{Paired: true, UUIDs: source}, true},
		"says it is a phone":    {bluez.Device{Paired: true, Icon: "phone"}, true},
		"not paired":            {bluez.Device{Icon: "phone", UUIDs: source}, false},
		"plays audio too":       {bluez.Device{Paired: true, UUIDs: source, AudioSink: true}, false},
		"the remembered device": {bluez.Device{Paired: true, Address: speaker, UUIDs: source}, false},
		"a headset, no sink":    {bluez.Device{Paired: true, Icon: "audio-headset", UUIDs: source}, false},
		"headphones":            {bluez.Device{Paired: true, Icon: "audio-headphones"}, false},
		"an audio card":         {bluez.Device{Paired: true, Icon: "audio-card"}, false},
		"neither":               {bluez.Device{Paired: true, Icon: "input-keyboard"}, false},
	} {
		if got := isPhone(c.d, speaker); got != c.want {
			t.Errorf("%s: isPhone = %v, want %v", name, got, c.want)
		}
	}
}

func TestWhichPhoneJoins(t *testing.T) {
	const speaker = "00:11:22:33:44:55"
	phone := func(path, addr, name string) bluez.Device {
		return bluez.Device{Path: dbus.ObjectPath(path), Address: addr, Name: name, Paired: true, Connected: true, Icon: "phone"}
	}
	a := phone("/a", "AA:00:00:00:00:01", "Phone A")
	b := phone("/b", "AA:00:00:00:00:02", "Phone B")
	off := a
	off.Connected = false
	buds := bluez.Device{Path: "/c", Address: "AA:00:00:00:00:03", Paired: true, Connected: true, Icon: "audio-headphones"}
	up := func(paths ...string) map[string]bool {
		m := map[string]bool{}
		for _, p := range paths {
			m[p] = true
		}
		return m
	}
	for name, c := range map[string]struct {
		devices    []bluez.Device
		audio, was map[string]bool
		open       bool
		joined     string
		linked     bool
		wantName   string
	}{
		"audio comes up while open":       {[]bluez.Device{a}, up("/a"), up(), true, a.Address, true, "Phone A"},
		"connected, audio not up yet":     {[]bluez.Device{a}, up(), up(), true, "", true, "Phone A"},
		"audio already up before":         {[]bluez.Device{a}, up("/a"), up("/a"), true, "", true, "Phone A"},
		"second phone joins, first stays": {[]bluez.Device{a, b}, up("/a", "/b"), up("/a"), true, b.Address, true, "Phone A"},
		"window closed":                   {[]bluez.Device{a}, up("/a"), up(), false, "", true, "Phone A"},
		"not connected":                   {[]bluez.Device{off}, up("/a"), up(), true, "", false, ""},
		"earbuds are not a phone":         {[]bluez.Device{buds}, up("/c"), up(), true, "", false, ""},
	} {
		l := lookAtPhones(c.devices, speaker, c.audio, c.was, c.open)
		if l.joined != c.joined || l.linked != c.linked || l.name != c.wantName {
			t.Errorf("%s: joined %q linked %v name %q, want %q %v %q", name, l.joined, l.linked, l.name, c.joined, c.linked, c.wantName)
		}
	}
	// What the look remembers is what the next look compares with: the audio that was up, phones only.
	l := lookAtPhones([]bluez.Device{a, buds}, speaker, up("/a", "/c"), up(), true)
	if !l.audio["/a"] || len(l.audio) != 1 {
		t.Errorf("remembered audio %v, want only /a", l.audio)
	}
}
