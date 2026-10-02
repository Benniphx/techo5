package realtime

import (
	"encoding/binary"
	"math"
)

// inputResampler converts hardware PCM16 at 16 kHz to the API's 24 kHz. A
// polyphase low-pass FIR interpolates by three and decimates by two, retaining
// both history and rational phase across microphone packets.
type inputResampler struct {
	history [16]float64
	phase   int
}

var inputFilter = func() [48]float64 {
	var h [48]float64
	for i := range h {
		x := (float64(i) - 23.5) / 3
		sinc := 1.0
		if x != 0 {
			sinc = math.Sin(math.Pi*x) / (math.Pi * x)
		}
		h[i] = sinc * (.54 - .46*math.Cos(2*math.Pi*float64(i)/47))
	}
	// Normalize each interpolation phase for unity DC gain.
	for p := 0; p < 3; p++ {
		sum := 0.0
		for k := p; k < len(h); k += 3 {
			sum += h[k]
		}
		for k := p; k < len(h); k += 3 {
			h[k] /= sum
		}
	}
	return h
}()

func (r *inputResampler) run(samples []int16) []byte {
	out := make([]byte, 0, (len(samples)*3 + 1))
	for _, sample := range samples {
		copy(r.history[:], r.history[1:])
		r.history[15] = float64(sample)
		for r.phase < 3 {
			value := 0.0
			for k := range r.history {
				value += inputFilter[r.phase+k*3] * r.history[15-k]
			}
			out = binary.LittleEndian.AppendUint16(out, uint16(clampSample(value)))
			r.phase += 2
		}
		r.phase -= 3
	}
	return out
}

func clampSample(v float64) int16 {
	return int16(math.Round(math.Max(-32768, math.Min(32767, v))))
}

// Resampler doubles 24 kHz PCM to 48 kHz stereo through a stateful polyphase
// windowed-sinc FIR. Identical channels preserve the device's left-loopback AEC.
type Resampler struct{ history [12]float64 }

var outputFilter = func() [24]float64 {
	var h [24]float64
	for i := range h {
		x := (float64(i) - 11.5) / 2
		sinc := 1.0
		if x != 0 {
			sinc = math.Sin(math.Pi*x) / (math.Pi * x)
		}
		h[i] = sinc * (.54 - .46*math.Cos(2*math.Pi*float64(i)/23)) * .9
	}
	return h
}()

func (r *Resampler) Reset() { r.history = [12]float64{} }
func (r *Resampler) Run(pcm []byte) []int16 {
	out := make([]int16, 0, len(pcm)*2)
	for i := 0; i+1 < len(pcm); i += 2 {
		copy(r.history[:], r.history[1:])
		r.history[11] = float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		for p := 0; p < 2; p++ {
			value := 0.0
			for k := 0; k < 12; k++ {
				value += outputFilter[p+k*2] * r.history[11-k]
			}
			sample := clampSample(value)
			out = append(out, sample, sample)
		}
	}
	return out
}
