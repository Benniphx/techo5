//go:build !dot

package display

import "time"

// saverIdleSince is when the screensaver's wait runs from, given when it ran from before. Anything but
// the plain idle page stops it (zero). On that page it starts at the first frame; a turn under a cue
// is somebody at the screen, so it starts again then, unless the screensaver is already up (it stays
// up under the light, as any page under a cue does); and somebody near the device, with Presence
// detection on (present), holds it at now, so the screensaver waits for the room to be empty, and
// ends when somebody comes near.
func saverIdleSince(since, now time.Time, boring, cue, present bool, wait time.Duration) time.Time {
	switch {
	case !boring:
		return time.Time{}
	case since.IsZero(), present, cue && now.Sub(since) < wait:
		return now
	}
	return since
}
