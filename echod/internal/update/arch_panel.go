//go:build panel

package update

// archSuffix marks panel binaries apart from the Echos'. None are published: the app carries the
// daemon and is updated as a whole.
const archSuffix = "-panel"
