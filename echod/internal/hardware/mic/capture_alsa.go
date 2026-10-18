//go:build !panel

package mic

import "github.com/HuskerMinion/techo5/echod/internal/config"

// hasMixer is whether the microphones' converters have gain controls in an ALSA mixer here.
const hasMixer = true

// afterOpen points the converters at their microphones and sets the gain, once the device is held.
func afterOpen() {
	routeInputs()
	applyGain(config.Get().Microphone.Gain)
}
