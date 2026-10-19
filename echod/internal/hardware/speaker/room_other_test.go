//go:build !panel

package speaker

// roomGain is the device's own volume on top of what is written: none on an Echo, whose volume is all
// in software.
func roomGain() float64 { return 1 }
