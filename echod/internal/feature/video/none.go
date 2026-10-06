//go:build dot || spot

package video

// Here is whether this device plays videos: the Spot and the Dot do not (doc.go).
const Here = false

func Play(Request) (uint64, error) { return 0, ErrNotHere }
func Stop()                        {}
func StopID(uint64)                {}
func Pause()                       {}
func Resume()                      {}
func Current() State               { return State{Phase: Idle} }
func SetOn(bool)                   {}
func SetDLNA(bool)                 {}
func Listen(func())                {}
func DLNAOn() bool                 { return false }
func Installed() bool              { return false }
