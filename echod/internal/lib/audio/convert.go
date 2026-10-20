package audio

import "math"

// Converter changes interleaved 16-bit audio from one rate to another by a ratio of whole numbers:
// a station's 44.1 kHz to the speaker's 48 (160/147), or a 96 kHz file down to it (1/2).
//
// It is the textbook polyphase resampler: up by to/g, a low pass, down by from/g, with only the
// output samples that are kept ever computed. Drawing straight lines between neighboring samples
// instead folds the images of everything above a few kHz back into the treble as tones that are no
// harmonic of anything: a 10 kHz tone at 44.1 kHz came out with a ghost at 13.9 kHz only 21 dB under
// it, heard as grit on cymbals and sibilants. Here those images are about 74 dB down.
//
// The filter's cutoff sits at the lower rate's Nyquist frequency, with convertTaps taps for every
// sample at the higher rate, so the transition band is the same few kHz either way.
type Converter struct {
	up, down int // to/g and from/g
	channels int
	// phases[p] is the branch for an output that falls p/up of the way past its newest input.
	phases [][]float32
	// hist is, per channel, the last k inputs twice over, newest last (as in Upsampler).
	hist [][]float32
	at   int
	// phase is where the next output falls past the newest input, in 1/up of an input sample.
	phase int

	clipped uint64
}

const (
	// convertTaps is the filter length per sample of the faster side: a transition band 5.5/48 of that
	// rate wide (about 5 kHz at 44.1 or 48 kHz), so flat to about 19.5 kHz, centered on the cutoff.
	convertTaps = 48

	// convertMostPhases bounds the table: up is 160 for 44.1 kHz and 640 for 11.025, the worst of
	// the usual rates. A rate with no small ratio to the output gets no converter (NewConverter).
	convertMostPhases = 640
)

// NewConverter makes a converter from one rate to another for channels interleaved channels, or
// returns nil when the ratio needs more than convertMostPhases phases (no common rate does).
func NewConverter(from, to, channels int) *Converter {
	if from <= 0 || to <= 0 || channels <= 0 {
		return nil
	}
	g := gcd(from, to)
	up, down := to/g, from/g
	if up > convertMostPhases {
		return nil
	}
	// Taps per phase: convertTaps per input sample when going up, scaled by the ratio going down so
	// the band stays as wide in Hz.
	k := convertTaps
	if from > to {
		k = (convertTaps*from + to - 1) / to
	}
	// The prototype runs at up*from; its cutoff is half the slower rate, and its gain up makes up for
	// the zeros the upsampling would have put between the inputs.
	h := lowpass(up*k, 0.5*float64(min(from, to))/float64(up*from), float64(up))
	c := &Converter{up: up, down: down, channels: channels}
	for p := range up {
		branch := make([]float32, k)
		for i := range branch {
			branch[i] = h[p+i*up]
		}
		c.phases = append(c.phases, branch)
	}
	for range channels {
		c.hist = append(c.hist, make([]float32, 2*k))
	}
	return c
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// Run appends the converted form of in, interleaved, to out and returns it. It keeps its history
// between calls, so a stream fed in pieces of any size comes out as one signal.
func (c *Converter) Run(in, out []int16) []int16 {
	k := len(c.phases[0])
	for f := 0; f+c.channels <= len(in); f += c.channels {
		for ch := range c.channels {
			x := float32(in[f+ch])
			c.hist[ch][c.at] = x
			c.hist[ch][c.at+k] = x
		}
		c.at++
		if c.at == k {
			c.at = 0
		}
		for ; c.phase < c.up; c.phase += c.down {
			branch := c.phases[c.phase]
			for ch := range c.channels {
				win := c.hist[ch][c.at : c.at+k]
				var y float32
				for i, coef := range branch {
					y += coef * win[k-1-i]
				}
				out = append(out, c.clip(y))
			}
		}
		c.phase -= c.up
	}
	return out
}

func (c *Converter) clip(y float32) int16 {
	switch {
	case y > math.MaxInt16:
		c.clipped++
		return math.MaxInt16
	case y < math.MinInt16:
		c.clipped++
		return math.MinInt16
	}
	return int16(math.Round(float64(y)))
}

// Clipped counts output samples that came out past full scale: a filter overshoots a transient by a
// few percent, so music mastered to full scale can.
func (c *Converter) Clipped() uint64 { return c.clipped }

// Delay is how far the output lags the input, in output samples.
func (c *Converter) Delay() int {
	return (c.up*len(c.phases[0]) - 1) / 2 / c.down
}
