package media

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hajimehoshi/go-mp3"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// A stream the device fetches itself - a station kept on it, one the voice assistant found, one typed
// on the setup page - arrives as the station sends it, not converted by Home Assistant first. Most
// internet radio is MP3, which is decoded here; AAC and the rest are refused by name, rather than the
// player reporting a station it cannot make a sound from.

// pcmSource is body as the speaker's own samples: a WAV (what Home Assistant's conversion sends)
// after its header, or an MP3 decoded and brought to the speaker's rate.
func pcmSource(body *bufio.Reader, contentType string) (io.Reader, error) {
	head, err := body.Peek(4)
	if err != nil {
		return nil, fmt.Errorf("reading the stream: %w", err)
	}
	switch {
	case string(head) == "RIFF":
		if err := header(body); err != nil {
			return nil, err
		}
		return body, nil
	case streamIsMP3(head, contentType):
		return newMP3Samples(body)
	}
	ct := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	if ct == "" {
		ct = "an unknown format"
	}
	return nil, fmt.Errorf("the stream is %s, which this device cannot play", ct)
}

// streamIsMP3 is an MP3 by its first bytes (an ID3 tag, or a frame's sync word) or by what the server calls it.
// AAC's own frames (ADTS) start with the same sync bits; what tells them apart is the layer, which an
// MPEG audio frame never leaves at zero and ADTS always does. A server that says AAC is believed.
func streamIsMP3(head []byte, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "aac") {
		return false
	}
	sync := head[0] == 0xFF && head[1]&0xE0 == 0xE0 && (head[1]>>1)&0x03 != 0
	return string(head[:3]) == "ID3" || sync || strings.HasPrefix(ct, "audio/mpeg") || strings.HasPrefix(ct, "audio/mp3")
}

// mp3Samples reads an MP3 stream as 16-bit stereo at speaker.Rate: what the WAV path hands on.
type mp3Samples struct {
	d   *mp3.Decoder
	rs  resampler
	in  []byte
	out []byte
	err error
}

func newMP3Samples(r io.Reader) (*mp3Samples, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("mp3: %w", err)
	}
	if d.SampleRate() <= 0 {
		return nil, errors.New("mp3: no sample rate")
	}
	// go-mp3 always gives 16-bit stereo; only the rate can differ from the speaker's.
	return &mp3Samples{d: d, rs: resampler{from: d.SampleRate(), to: speaker.Rate}, in: make([]byte, 4608)}, nil
}

func (m *mp3Samples) Read(p []byte) (int, error) {
	for len(m.out) == 0 {
		if m.err != nil {
			return 0, m.err
		}
		n, err := m.d.Read(m.in)
		n -= n % 4
		if n > 0 {
			samples := make([]int16, n/2)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(m.in[i*2:]))
			}
			samples = m.rs.run(samples)
			m.out = make([]byte, len(samples)*2)
			for i, s := range samples {
				binary.LittleEndian.PutUint16(m.out[i*2:], uint16(s))
			}
		}
		if err != nil {
			m.err = err
		}
	}
	n := copy(p, m.out)
	m.out = m.out[n:]
	return n, nil
}

// resampler brings interleaved stereo from one rate to another as it streams past, by straight lines
// between neighboring samples: a station's 44.1 kHz to the speaker's 48. The last frame of each chunk
// is carried into the next, so the joins are as smooth as the middles.
type resampler struct {
	from, to int
	pos      float64 // where the next output falls, in input frames from the start of the chunk
	prev     [2]int16
	primed   bool
}

func (r *resampler) run(in []int16) []int16 {
	if r.from == r.to || r.from <= 0 || r.to <= 0 {
		return in
	}
	n := len(in) / 2
	if n == 0 {
		return nil
	}
	at := func(i, ch int) float64 {
		if i < 0 {
			return float64(r.prev[ch])
		}
		return float64(in[i*2+ch])
	}
	if !r.primed {
		r.prev = [2]int16{in[0], in[1]}
		r.primed = true
	}
	step := float64(r.from) / float64(r.to)
	out := make([]int16, 0, int(float64(n)/step)*2+4)
	// Frame -1 is the last of the previous chunk, so the first output can fall between it and frame 0.
	for r.pos < float64(n-1) {
		i := int(r.pos+1) - 1 // floor, for pos down to -1
		f := r.pos - float64(i)
		for ch := range 2 {
			a, b := at(i, ch), at(i+1, ch)
			out = append(out, int16(a+(b-a)*f))
		}
		r.pos += step
	}
	r.pos -= float64(n)
	r.prev = [2]int16{in[(n-1)*2], in[(n-1)*2+1]}
	return out
}
