//go:build !panel

package mic

import "github.com/HuskerMinion/techo5/echod/internal/config"

// afterOpen points the converters at their microphones and sets the gain, once the device is held.
func afterOpen() {
	routeInputs()
	applyGain(config.Get().Microphone.Gain)
}
