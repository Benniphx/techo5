//go:build dot && donut

package speaker

import (
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/asp"
)

// Output is one of the device's audio outputs.
type Output string

const (
	OutputSpeaker   Output = "speaker"
	OutputHeadphone Output = "headphone"
	// OutputBoth is the speaker and the jack at once, which this board does not do (HasBoth).
	OutputBoth Output = "both"
)

// The mixer sequences below come from the device: Fire OS's mixer_paths.xml on the Echo Dot 3rd gen
// (donut), unit 1, 2026-10-23.
type kctl struct {
	name  string
	value string
	level int32
	blob  []byte
	// ifPresent writes the control only on a unit that has it.
	ifPresent bool
}

// The Dot 3 plays DL1 out of the MT8516's AFE two ways: over I2S (the AFE's O03/O04) to a TI TAS2770
// amplifier on the speaker, or through the SoC's own codec to the jack. The amplifier's level is its
// own (DAC Playback Volume, 16 of 0 to 20 from boot, 11 to 21 dBV full scale at 0.5 dB a step) and its
// digital volume (PCM Playback Volume) is at full on the speaker's path and zero on the jack's, which
// is how Fire OS keeps the speaker quiet while the jack plays. Found by echo-dot-assist (Gamer92000,
// MIT: devices/donut/README.md) and read off the unit.
var initSequence = []kctl{
	{name: "LINEOUT Ext Amp Switch", level: 0},
	{name: "HP Ext Amp Switch", level: 0},
	{name: "INT ADDA O03_O04 Switch", level: 1},
	{name: "O03 I05 Switch", level: 1},
	{name: "O04 I06 Switch", level: 1},
	{name: "O03 I07 Switch", level: 0},
	{name: "O04 I08 Switch", level: 0},
}

var pathSequence = map[Output][]kctl{
	OutputSpeaker: {
		{name: "Audio Amp Playback Volume", level: 0},
		{name: "PCM Playback Volume", level: 255},
		{name: ampLevel, level: ampUnity},
		{name: "I2S O03_O04 Switch", level: 1},
		{name: "LINEOUT Mux", value: "VOICE_AMP"},
		{name: "HPOUT Mux", value: "OPEN"},
	},
	OutputHeadphone: {
		{name: "Audio Amp Playback Volume", level: 3},
		{name: "PCM Playback Volume", level: 0},
		{name: "I2S O03_O04 Switch", level: 0},
		{name: "LINEOUT Mux", value: "OPEN"},
		{name: "HPOUT Mux", value: "AUDIO_AMP"},
	},
}

// headphoneOff takes the jack's path down when it is left.
var headphoneOff = []kctl{
	{name: "HPOUT Mux", value: "OPEN"},
}

// ampLevel is the TAS2770's level, 0.5 dB a step; ampUnity is where the board boots it and where Fire
// OS leaves it.
const (
	ampLevel = "DAC Playback Volume"
	ampUnity = 16
	ampMax   = 20
)

// gainOnPath is whether the Speaker gain setting is written with the speaker's path: here it is the
// amplifier's own level, raised from ampUnity by up to 2 dB, which is all the control has above it.
const gainOnPath = true

// withSpeakerGain is seq, out's path, with the Speaker gain in it.
func withSpeakerGain(seq []kctl, out Output, db int) []kctl {
	if out != OutputSpeaker {
		return seq
	}
	gained := slices.Clone(seq)
	for i := range gained {
		if gained[i].name == ampLevel {
			gained[i].level = ampFor(db)
		}
	}
	return gained
}

// speakerGainControls is what a change of the Speaker gain writes while the speaker plays.
func speakerGainControls(db int) []kctl {
	return []kctl{{name: ampLevel, level: ampFor(db)}}
}

// ampFor is the amplifier's level for a Speaker gain of db dB.
func ampFor(db int) int32 {
	db = max(0, min(db, config.SpeakerGainMax))
	return int32(min(ampUnity+2*db, ampMax))
}

// jackState is the kernel's headphone jack switch: 1 while something is plugged in.
const jackState = "/sys/class/switch/h2w/state"

// jackPoll is how often the jack switch is sampled.
const jackPoll = 500 * time.Millisecond

// DetectOutput picks the output to use. A missing switch means no jack detection, so assume the
// speaker.
func DetectOutput() Output {
	b, err := os.ReadFile(jackState)
	if err != nil || strings.TrimSpace(string(b)) == "0" {
		return OutputSpeaker
	}
	return OutputHeadphone
}

// VolumeSteps is the number of volume steps; the range is config's.
const VolumeSteps = config.VolumeSteps

// volumeCurves are the board's volume curves: index is the volume step, value is attenuation in dB.
// The Dot 2's for now, until the Dot 3's are read off its VolumeCurves.xml and listened to.
var volumeCurves = map[Output][VolumeSteps + 1]float64{
	OutputSpeaker: {
		-90, -33, -30, -26, -25, -23, -21, -19, -17, -16,
		-14, -13, -12, -10, -9, -8, -7, -5, -5, -4,
		-4, -4, -4, -3, -3, -3, -3, -3, -2, -1, 0,
	},
	OutputHeadphone: {
		-100, -39, -38, -36, -34, -33, -31, -29, -27, -26,
		-25, -24, -23, -21, -20, -19, -18, -17, -15, -14,
		-13, -12, -11, -9, -8, -7, -6, -4, -3, -2, 0,
	},
}

// mute is the attenuation the curves use for step 0.
const mute = -90

// gainForStep converts a volume step to a linear gain using the output's curve.
func gainForStep(out Output, step int) float32 {
	curve, ok := volumeCurves[out]
	if !ok {
		curve = volumeCurves[OutputSpeaker]
	}
	step = max(0, min(step, VolumeSteps))

	db := curve[step]
	if db <= mute {
		return 0
	}
	return float32(math.Pow(10, db/20))
}

// The playback ring.
const (
	period  = 1024
	periods = 4
)

// DRAMHold is unused here.
const DRAMHold = ""

// startFull is off: no restart storm has been seen on this board.
const startFull = false

// softRef is on: the capture uses the card's DL1_AWB_Record where it opens, and otherwise asks for
// the software reference (echoref.InUse), which this player then feeds.
const softRef = true

// openSequence is empty: the playback device opens with the card as the paths left it.
var openSequence []kctl

// MediaService is the init service that owns Android's audio HAL on Fire OS.
const MediaService = "media"

// AmpSwitch is empty: the TAS2770 has no gate of its own on this card; its digital volume carries the
// speaker's on and off (pathSequence).
const AmpSwitch = ""

// MuteSwitch is empty: no separate amplifier mute.
const MuteSwitch = ""

// mayHaveTAS5805M is false: the amplifier is a TAS2770.
const mayHaveTAS5805M = false

// OutputBoost is make-up gain on the output.
const OutputBoost = 1.0

// DriverTuning is off: the Dot 3's vendor tuning (audio-algorithms) is another set than the ones
// lib/asp reads, and it plays without it until it is.
const DriverTuning = false

// tuningSet is nil: no tuning is loaded by name here.
var tuningSet *asp.Set

// firstCurves is empty: without the tuning, the volume is volumeCurves as they are.
var firstCurves = map[string][VolumeSteps + 1]float64{}

// HasJack is whether the device has a headphone jack: the Dot 3 has one.
const HasJack = true

// HasBoth is whether the speaker and the jack can play at once: not tried, so not offered.
const HasBoth = false

// PlaybackDevice is DL1_Playback, which the card takes at Rate as it is.
const (
	PlaybackDevice = 6
	hwRate         = Rate
	hwTrimDB       = 0.0
)
