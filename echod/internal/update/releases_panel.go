//go:build panel

package update

// releases is empty on a panel: the daemon comes inside the app, so there is nothing to fetch and a
// check finds no manifest.
const releases = ""

// A panel updates with its app and never follows the native Show firmware stream.
const nativeChannel = false
const platformReleaseKey = upstreamReleaseKey
