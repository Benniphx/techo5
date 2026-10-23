//go:build dot && donut

package update

// archSuffix marks the Echo Dot 3's builds. Nothing is published for it, and a Dot 2 release carries
// only "-dot" assets, so the Dot 2's channel offers this device nothing rather than the wrong thing.
const archSuffix = "-donut"

// rootfsArch is the key this device's root filesystem would be published under.
func rootfsArch() string { return arch }
