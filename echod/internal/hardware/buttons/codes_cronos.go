//go:build !dot && !spot

package buttons

// longHoldAfter is zero: the Show's screen is where pairing and everything else is asked for.
const longHoldAfter = 0

// The Echo Show 5's three buttons, by evdev key code. Volume comes from gpio-keys; the mute button
// is its own input device, gpio-privacy-button, which the kernel reports as KEY_POWER (116). There
// is no action button.
var codes = map[uint16]Name{
	114: VolumeDown,
	115: VolumeUp,
	116: Mute,
}

// notButtons are input devices that are not buttons, by name, though they declare key codes. The Show
// 8's FocalTech lists KEY_POWER (116, the mute button's code) among its gesture keys: on the 2nd gen,
// read off the unit 2026-10-14, next to the letter and arrow gestures. A gesture read as a button would
// toggle the mute, so the touchscreen is left to the touch package.
var notButtons = map[string]bool{"fts_ts": true}
