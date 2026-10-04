//go:build panel

package speaker

import (
	"math"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// TECHO5 Panel: the app plays what the daemon writes through an Android AudioTrack. There is no codec
// to set up, no jack to watch and no amplifier to switch: Android routes the sound. The daemon's own
// volume is applied in software, the same as on the Echos, on top of Android's media volume.

// Output is the device's audio output. A panel has the speaker only; Android moves the sound to
// headphones or Bluetooth on its own.
type Output string

const (
	OutputSpeaker   Output = "speaker"
	OutputHeadphone Output = "headphone"
	OutputBoth      Output = "both"
)

// The write loop's period, 20 ms, and how many the app's buffer is sized against.
const (
	period  = Rate / 50
	periods = 4
)

// DRAMHold is an Echo Show workaround; nothing here.
const DRAMHold = ""

// A mixer write, as the shared player applies it. A panel has no mixer, so none are made.
type kctl struct {
	name      string
	value     string
	level     int32
	blob      []byte
	ifPresent bool
}

var (
	initSequence []kctl
	pathSequence = map[Output][]kctl{}
	headphoneOff []kctl
)

const jackPoll = time.Second

// DetectOutput is always the speaker: Android routes it.
func DetectOutput() Output { return OutputSpeaker }

// VolumeSteps is the number of volume steps, config's range.
const VolumeSteps = config.VolumeSteps

// mute is the attenuation for step 0.
const mute = -90

// gainForStep is linear in dB from -40 dB at the first step to unity at the top, like a Bluetooth
// sink's: a tablet's speaker has its own amplifier and Android's volume on top.
func gainForStep(_ Output, step int) float32 {
	step = max(0, min(step, VolumeSteps))
	if step == 0 {
		return 0
	}
	db := (float64(step)/VolumeSteps - 1) * 40
	return float32(math.Pow(10, db/20))
}

// MediaService is empty: nothing has to be stopped to play.
const MediaService = ""

const AmpSwitch = ""

const OutputBoost = 1.0

// DriverTuning is off: the vendor tuning is an Echo's.
const DriverTuning = false

var (
	HasJack = false
	HasBoth = false
)
