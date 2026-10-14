//go:build !dot && !spot

package mic

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/HuskerMinion/techo5/echod/internal/lib/alsa"
	"github.com/HuskerMinion/techo5/echod/internal/lib/audio"
)

// The 2nd gen Echo Show 8 (athena) on Amazon's own 32-bit 4.4 kernel. Its one card, mt-snd-card,
// has the microphones and the playback reference on two capture devices, where every other board has
// one device with the loopback inside the frame:
//
//   - hw:0,17 "I2S2ADC2_Capture": a TLV320ADC5140, four microphones, ch1/ch2 on its first data line
//     and ch3/ch4 on the second. Fire OS opens it at 96 kHz in 32-bit slots, four channels (asp.cfg
//     "Mics": 4 at 96000; AFE.cfg "Mics Input SamplingRate": 96000), and so does this.
//   - hw:0,16 "I2S0AWB_Capture": the speaker's I2S stream written back, left and right. At most
//     48 kHz and 16 bits. That this device is the reference is the best reading of the card, not a
//     measurement yet: Fire OS states a 96 kHz reference, which this device cannot give.
//
// Both are brought down to 16 kHz and interleaved into the 1st gen Show 8's frame (S24_3LE, six
// channels, the microphones then the reference: see device_cronos.go), so the canceller, the
// beamformer, the leveler and everything raw downstream read athena as they read crown.
//
// Alignment. The two streams run off the same AFE clocks, so they do not drift; what can differ is
// where each starts. Both are configured and prepared first and then started back to back, reference
// first, so the reference is never behind the microphones (a canceller can only remove an echo whose
// cause it has already seen). An overrun on either starts both over the same way. The two decimators
// delay by the same time (lib/audio), so they add nothing. `techo5 echo` reports the lag that is
// left.
const (
	athenaMicDevice   = 17
	athenaMicRate     = 96000
	athenaMicChannels = 4
	athenaMicBits     = 32

	athenaRefDevice   = 16
	athenaRefRate     = 48000
	athenaRefChannels = 2
	athenaRefBits     = 16

	// 10 ms periods. The microphone ring is four of them: the card refuses more than 64 KB of ring, and
	// four channels of 32 bits at 96 kHz fill that in 43 ms.
	athenaMicPeriod  = athenaMicRate / 100
	athenaMicPeriods = 4
	athenaRefPeriod  = athenaRefRate / 100
	athenaRefPeriods = 8

	// Decimator lengths in the ratio of the rates, so both delay by the same 1 ms.
	athenaMicTaps = 192
	athenaRefTaps = 96

	// athenaMicBoostDB is what Fire OS adds after the same conversion: "Athena AFE mic input (post
	// 96KHz to 16Khz SRC) levels is 5.89dB lower than Crown" (AFE.cfg, MicBoostdB). With it the room
	// reads at crown's level, which is what the leveler and the wake word were set against.
	athenaMicBoostDB = 5.89
)

// athenaControls are the converter's initial settings, as Fire OS's audio HAL writes them at start
// (vendor /etc/audio_device_tas5805m.xml), in its order: ch1/ch2 in the left and right slots of the
// first data line, ch3/ch4 the same on the second (GPIO1 as SDOUT2), digital volume at 0 dB (201),
// and dynamic range enhancement off. The analog gain, which the same file sets to 20 dB, is the
// daemon's own setting (athenaGain). Nothing here is the speaker's: Amazon's kernel driver sets the
// amplifier up itself.
var athenaControls = []struct {
	name  string
	value uint32
}{
	{"ADC Reset A", 1},
	{"Ch1 Output A", 0},
	{"Ch1 Slot Offset A", 0},
	{"Ch2 Output A", 0},
	{"Ch2 Slot Offset A", 32},
	{"Ch3 Output A", 1},
	{"Ch3 Slot Offset A", 0},
	{"Ch4 Output A", 1},
	{"Ch4 Slot Offset A", 32},
	{"Enable Secondary Output A", 3},
	{"Ch1 Digital Capture Vol A", 201},
	{"Ch2 Digital Capture Vol A", 201},
	{"Ch3 Digital Capture Vol A", 201},
	{"Ch4 Digital Capture Vol A", 201},
	{"DRE Trigger Level A", 4},
	{"DRE Max Gain A", 11},
	{"Ch1 DRE Enable A", 0},
	{"Ch2 DRE Enable A", 0},
	{"Ch3 DRE Enable A", 0},
	{"Ch4 DRE Enable A", 0},
}

// athenaGainControl is the converter's analog gain, one control per microphone, in dB.
const athenaGainControl = "Ch%d Gain A"

// athenaWrite sets one control, looked up by name alone: this card has controls that do things when
// read (a timestamp, a playback state), so nothing here walks the list. A control the unit does not
// have is said once and skipped.
func athenaWrite(m *alsa.Mixer, name string, v uint32) bool {
	c, err := m.Lookup(name)
	if err != nil {
		slog.Warn("athena: mixer control not on this unit", "control", name)
		return false
	}
	if err := m.Set(c, v); err != nil {
		slog.Error("athena: mixer write failed", "control", name, "err", err)
		return false
	}
	return true
}

// athenaGain sets the analog gain on all four microphones, held to what the control allows.
func athenaGain(m *alsa.Mixer, db int) {
	for ch := 1; ch <= athenaMicChannels; ch++ {
		name := fmt.Sprintf(athenaGainControl, ch)
		c, err := m.Lookup(name)
		if err != nil {
			slog.Warn("athena: mixer control not on this unit", "control", name)
			continue
		}
		v := int64(db)
		if c.Max > c.Min {
			v = min(max(v, c.Min), c.Max)
		}
		if err := m.Set(c, uint32(v)); err != nil {
			slog.Error("setting the microphone gain failed", "control", name, "err", err)
			return
		}
	}
	slog.Info("microphone gain", "db", db)
}

// athenaInit writes athenaControls before the converter is opened, as the HAL does.
func athenaInit() {
	m, err := alsa.OpenMixer(Card)
	if err != nil {
		slog.Error("opening the mixer failed", "err", err)
		return
	}
	defer m.Close()
	for _, c := range athenaControls {
		athenaWrite(m, c.name, c.value)
	}
}

// pairedCapture is the two devices read as one.
type pairedCapture struct {
	mics *alsa.Capture
	// ref is nil when the reference could not be opened: the frame then carries silence there, which
	// the canceller reads as nothing playing, and everything else works.
	ref *alsa.Capture

	micBuf, refBuf []byte
	micDec         [athenaMicChannels]*audio.Decimator
	refDec         [athenaRefChannels]*audio.Decimator
	boost          float32
}

func openAthena() (capture, error) {
	athenaInit()

	ref, err := alsa.OpenPrepared(Card, athenaRefDevice, alsa.Config{
		Channels:   athenaRefChannels,
		Rate:       athenaRefRate,
		Format:     alsa.FormatS16_LE,
		Bits:       athenaRefBits,
		PeriodSize: athenaRefPeriod,
		Periods:    athenaRefPeriods,
	})
	if err != nil {
		slog.Warn("athena: no playback reference, echo cancellation will have nothing to cancel",
			"device", athenaRefDevice, "err", err)
		ref = nil
	}

	mics, err := alsa.OpenPrepared(Card, athenaMicDevice, alsa.Config{
		Channels:   athenaMicChannels,
		Rate:       athenaMicRate,
		Format:     alsa.FormatS32_LE,
		Bits:       athenaMicBits,
		PeriodSize: athenaMicPeriod,
		Periods:    athenaMicPeriods,
	})
	if err != nil {
		if ref != nil {
			_ = ref.Close()
		}
		return nil, err
	}

	c := newPairedCapture(mics, ref)
	if err := c.start(); err != nil {
		_ = c.Close()
		return nil, err
	}
	slog.Info("athena: capture open", "mics", fmt.Sprintf("hw:%d,%d", Card, athenaMicDevice),
		"reference", ref != nil, "ref_device", fmt.Sprintf("hw:%d,%d", Card, athenaRefDevice))
	return c, nil
}

func newPairedCapture(mics, ref *alsa.Capture) *pairedCapture {
	c := &pairedCapture{mics: mics, ref: ref, boost: float32(math.Pow(10, athenaMicBoostDB/20))}
	for i := range c.micDec {
		c.micDec[i] = audio.NewDecimator(athenaMicRate/Rate, athenaMicTaps)
	}
	for i := range c.refDec {
		c.refDec[i] = audio.NewDecimator(athenaRefRate/Rate, athenaRefTaps)
	}
	return c
}

// start runs both streams, the reference first.
func (c *pairedCapture) start() error {
	if c.ref != nil {
		if err := c.ref.Start(); err != nil {
			return err
		}
	}
	return c.mics.Start()
}

// restart starts both over together after either lost audio, so the two stay lined up.
func (c *pairedCapture) restart() error {
	if c.ref != nil {
		if err := c.ref.Rewind(); err != nil {
			return err
		}
	}
	if err := c.mics.Rewind(); err != nil {
		return err
	}
	for _, d := range c.micDec {
		d.Reset()
	}
	for _, d := range c.refDec {
		d.Reset()
	}
	return c.start()
}

// Read fills buf with whole 16 kHz frames in the 1st gen Show 8's layout. An overrun on either device
// starts both over and is reported as alsa.ErrOverrun, as a single device's is.
func (c *pairedCapture) Read(buf []byte) (int, error) {
	frameBytes := Channels * Bits / 8
	frames := len(buf) / frameBytes
	if frames == 0 {
		return 0, fmt.Errorf("buffer smaller than one frame (%d bytes)", frameBytes)
	}

	micBytes := frames * (athenaMicRate / Rate) * athenaMicChannels * athenaMicBits / 8
	if len(c.micBuf) < micBytes {
		c.micBuf = make([]byte, micBytes)
	}
	if err := readFull(c.mics, c.micBuf[:micBytes]); err != nil {
		return 0, c.lost(err)
	}

	refBytes := frames * (athenaRefRate / Rate) * athenaRefChannels * athenaRefBits / 8
	if c.ref != nil {
		if len(c.refBuf) < refBytes {
			c.refBuf = make([]byte, refBytes)
		}
		if err := readFull(c.ref, c.refBuf[:refBytes]); err != nil {
			return 0, c.lost(err)
		}
	}

	var ref []byte
	if c.ref != nil {
		ref = c.refBuf[:refBytes]
	}
	c.convert(buf[:frames*frameBytes], c.micBuf[:micBytes], ref)
	return frames * frameBytes, nil
}

// lost handles a read that failed: an overrun starts both over, anything else is the caller's.
func (c *pairedCapture) lost(err error) error {
	if !errors.Is(err, alsa.ErrOverrun) {
		return err
	}
	if rerr := c.restart(); rerr != nil {
		return fmt.Errorf("mic: starting the capture over: %w", rerr)
	}
	return alsa.ErrOverrun
}

// convert decimates what Read gathered, mics and ref (nil without a reference), into out. The sizes
// agree: every output frame is a whole number of input frames on each.
func (c *pairedCapture) convert(out, mics, ref []byte) {
	frameBytes := Channels * Bits / 8
	frames := len(out) / frameBytes

	f := 0
	for at := 0; at+athenaMicChannels*4 <= len(mics) && f < frames; at += athenaMicChannels * 4 {
		var ready bool
		for ch, d := range c.micDec {
			x := float32(int32(binary.LittleEndian.Uint32(mics[at+4*ch:]))) / (1 << 31)
			y, ok := d.Push(x * c.boost)
			if ok {
				putS24(out[f*frameBytes+3*ch:], y)
			}
			ready = ok
		}
		if ready {
			f++
		}
	}

	if ref == nil {
		for f := range frames {
			clear(out[f*frameBytes+3*RefFirst : f*frameBytes+3*(RefFirst+Refs)])
		}
		return
	}
	f = 0
	for at := 0; at+athenaRefChannels*2 <= len(ref) && f < frames; at += athenaRefChannels * 2 {
		var ready bool
		for ch, d := range c.refDec {
			x := float32(int16(binary.LittleEndian.Uint16(ref[at+2*ch:]))) / (1 << 15)
			y, ok := d.Push(x)
			if ok {
				putS24(out[f*frameBytes+3*(RefFirst+ch):], y)
			}
			ready = ok
		}
		if ready {
			f++
		}
	}
}

// putS24 writes a sample in [-1, 1) as S24_3LE, held to full scale.
func putS24(b []byte, v float32) {
	s := int32(math.Round(float64(v) * (1 << 23)))
	s = min(max(s, -1<<23), 1<<23-1)
	b[0], b[1], b[2] = byte(s), byte(s>>8), byte(s>>16)
}

// readFull reads until buf is full: a read the runtime's signals cut short is carried on.
func readFull(pcm *alsa.Capture, buf []byte) error {
	for got := 0; got < len(buf); {
		n, err := pcm.Read(buf[got:])
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("mic: the capture device returned nothing")
		}
		got += n
	}
	return nil
}

// Close lets both devices go.
func (c *pairedCapture) Close() error {
	var err error
	if c.ref != nil {
		err = c.ref.Close()
	}
	if merr := c.mics.Close(); merr != nil {
		err = merr
	}
	return err
}
