//go:build panel

package mic

// TECHO5 Panel: the app records the tablet's microphone through Android and streams it to the daemon
// (capture_panel.go). Frames are laid out as the Echos' are, so everything downstream is unchanged:
// 16 kHz, S24_3LE, 2 channels, ch0 the microphone and ch1 the playback reference. The reference is
// silence until the panel's speaker feeds it, which leaves the canceller idle.
const (
	Channels      = 2
	CaptureDevice = 0

	// Mics is how many channels are microphones. Android hands an app one, already mixed.
	Mics = 1

	// maxMics is the largest Mics this build has to hold.
	maxMics = 1

	// RefFirst is the reference channel and Refs how many there are.
	RefFirst = 1
	Refs     = 1

	CenterMic = 0

	CancelOnMix = false
)

// adcs is empty: there is no codec to route or set the gain of, Android owns it.
var adcs []string

// MediaService is empty: nothing has to be stopped to get the microphone.
const MediaService = ""

const VendorBeamformer = false

func resetsOnMute() bool { return false }
