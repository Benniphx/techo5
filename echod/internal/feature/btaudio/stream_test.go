//go:build linux

package btaudio

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/bluealsa"
	"github.com/godbus/dbus/v5"
)

// look is one refresh's view of the speaker's stream: the PCM wanted (nil for none) and whether a
// reopen is allowed now.
type look struct {
	want      *bluealsa.PCM
	mayReopen bool
	move      streamMove
}

// play runs looks through moveStream as refresh does, opening and closing as it says, and reports
// the first look whose move differs.
func play(t *testing.T, name string, looks []look) {
	t.Helper()
	var cur dbus.ObjectPath
	ran := false
	for i, l := range looks {
		var move streamMove
		move, ran = moveStream(l.want, cur, ran, l.mayReopen)
		if move != l.move {
			t.Fatalf("%s: look %d: move %d, want %d", name, i, move, l.move)
		}
		switch move {
		case streamOpen:
			cur = l.want.Path
		case streamClose, streamReopen:
			cur = ""
		}
	}
}

func TestStreamFromTheSpeakersOwnReconnect(t *testing.T) {
	const path = dbus.ObjectPath("/org/bluealsa/hci0/dev_00_11_22_33_44_55/a2dpsrc/sink")
	idle := &bluealsa.PCM{Path: path, Transport: "A2DP-source", Mode: "sink", Channels: 2, Rate: 44100}
	running := *idle
	running.Running = true

	// The speaker reconnects on its own: its stream appears, is opened and runs, then the speaker
	// suspends the transport. bluez-alsa stops the stream and does not take the transport again while
	// it is open, so it is closed and opened again (#157).
	play(t, "own reconnect", []look{
		{want: idle, mayReopen: true, move: streamOpen},
		{want: idle, mayReopen: true, move: streamKeep}, // the Running signal not here yet
		{want: &running, mayReopen: true, move: streamKeep},
		{want: idle, mayReopen: true, move: streamReopen},
		{want: idle, mayReopen: true, move: streamOpen},
		{want: &running, mayReopen: true, move: streamKeep},
	})

	// One connected from here runs and stays running: nothing is opened again.
	play(t, "connected from here", []look{
		{want: idle, mayReopen: true, move: streamOpen},
		{want: &running, mayReopen: true, move: streamKeep},
		{want: &running, mayReopen: true, move: streamKeep},
	})

	// A stream that stops again soon after it was opened again waits for reopenEvery, and is opened
	// again once it has passed.
	play(t, "stops again", []look{
		{want: idle, mayReopen: true, move: streamOpen},
		{want: &running, mayReopen: true, move: streamKeep},
		{want: idle, mayReopen: true, move: streamReopen},
		{want: idle, mayReopen: false, move: streamOpen},
		{want: &running, mayReopen: false, move: streamKeep},
		{want: idle, mayReopen: false, move: streamKeep},
		{want: idle, mayReopen: false, move: streamKeep},
		{want: idle, mayReopen: true, move: streamReopen},
	})

	// The speaker going: the stream is let go, and one opened that never ran is not opened again.
	play(t, "speaker goes", []look{
		{want: idle, mayReopen: true, move: streamOpen},
		{want: &running, mayReopen: true, move: streamKeep},
		{want: nil, mayReopen: true, move: streamClose},
		{want: nil, mayReopen: true, move: streamKeep},
		{want: idle, mayReopen: true, move: streamOpen},
		{want: idle, mayReopen: true, move: streamKeep},
	})

	// Another device's stream wanted: that one is opened.
	other := running
	other.Path = "/org/bluealsa/hci0/dev_66_77_88_99_AA_BB/a2dpsrc/sink"
	play(t, "another device", []look{
		{want: &running, mayReopen: true, move: streamOpen},
		{want: &running, mayReopen: true, move: streamKeep},
		{want: &other, mayReopen: true, move: streamOpen},
	})
}
