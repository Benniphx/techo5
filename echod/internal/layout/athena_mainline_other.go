//go:build dot || spot

package layout

// AthenaMainline is never true on a Dot or a Spot.
func AthenaMainline() bool { return false }

// Skylight is never true on a Dot or a Spot: the Skylight Calendar runs the Show build.
func Skylight() bool { return false }

// idmeFallback: the Dot and the Spot always have /proc/idme.
func idmeFallback(string) (string, bool) { return "", false }
