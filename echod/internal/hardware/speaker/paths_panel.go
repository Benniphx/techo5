//go:build panel

package speaker

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/asp"
	"github.com/HuskerMinion/techo5/echod/internal/lib/panelctl"
)

// TECHO5 Panel: the app plays what the daemon writes through an Android AudioTrack. There is no codec
// to set up, no jack to watch and no amplifier to switch: Android routes the sound. The volume is
// Android's media volume, which the app sets from the daemon's step (volumeChanged), so the tablet's
// own buttons and slider and Home Assistant all move the one level.

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

// mediaStep is the step Android's media volume was last set to, which everything played rides on.
var mediaStep atomic.Int32

func init() { mediaStep.Store(VolumeSteps) }

// gainForStep is the software gain on top of Android's media volume. The volume is the tablet's own,
// set by the app from the media step (volumeChanged), so the tablet's buttons and slider and Home
// Assistant all move one level; the media step itself therefore plays at unity. Anything played at a
// step of its own, a ring or a bell, is scaled against it on the same curve a Bluetooth sink uses (-40
// dB at the first step, unity at the top), so a quiet ring is still quieter than the music. A step
// above the media level can only reach it: Android's volume is the ceiling.
func gainForStep(_ Output, step int) float32 {
	if step <= 0 {
		return 0
	}
	m := int(mediaStep.Load())
	if m <= 0 {
		return curve(step)
	}
	return min(curve(step)/curve(m), 1)
}

func curve(step int) float32 {
	step = max(0, min(step, VolumeSteps))
	return float32(math.Pow(10, (float64(step)/VolumeSteps-1)*40/20))
}

// ringsRideTheDeviceVolume is true: see gainForStep.
const ringsRideTheDeviceVolume = true

// volumeChanged hands the step to the app, which sets Android's media volume to it.
func volumeChanged(step int) {
	mediaStep.Store(int32(step))
	panelctl.Send(fmt.Sprintf("volume %d %d", step, VolumeSteps))
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

// firstCurves is empty on a panel: Android plays the sound, with no Amazon tuning to put the volume
// in front of (paths_cronos.go).
var firstCurves = map[string][VolumeSteps + 1]float64{}

// MuteSwitch is a mute on the amplifier besides AmpSwitch: none, Android owns the amplifier.
const MuteSwitch = ""

// mayHaveTAS5805M is false: there is no amplifier of the daemon's own to find (GainAvailable), so
// the speaker gain control never shows on a panel.
const mayHaveTAS5805M = false

// tuningSet is nil: there is no vendor tuning to load (DriverTuning).
var tuningSet *asp.Set

// softRef is off: a panel keeps its own reference of what it sent the app (playback_panel.go,
// lib/loopback), which the microphone's canceller reads.
const softRef = false

// hwRate is Rate and nothing is trimmed after the volume: the app takes the stream as it comes and
// Android resamples it for the hardware.
const (
	hwRate   = Rate
	hwTrimDB = 0.0
)
