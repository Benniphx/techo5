//go:build panel

package privacy

import "errors"

// A panel has no mute line of its own: the tablet's microphone is Android's, and the app hands the
// daemon what it records. The Shows' latch is not looked for at all, because a tablet on the Echo Show
// 5's MediaTek chip (the 8th gen Fire HD 8) can carry nodes with the Shows' names, and they are not a
// mute the daemon may drive. Android's own privacy switch silences the app instead, which the capture
// notices (hardware/mic, flat_panel.go).
var errNoLine = errors.New("privacy: a panel has no microphone mute of its own")

func platformMute() (Mute, error) { return nil, errNoLine }

// noLED stands in for a mute light a tablet does not have.
type noLED struct{}

func (noLED) SetBright(bool) error  { return nil }
func (noLED) Bright() (bool, error) { return true, nil }

func platformLight() (LED, error) { return noLED{}, nil }

// Nothing to seed: there is no cut of the daemon's own here. See privacy.Seed.
func seedSoftwareCut(bool) {}
