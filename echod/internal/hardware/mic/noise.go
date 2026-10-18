package mic

import (
	"math"
	"sync"
	"sync/atomic"
)

// How loud the room is, for the ambient noise sensor: the mean power of what the microphones heard over
// a window, in dBFS.
//
// It is not the level the ring follows. That one is measured against the room's own floor, so a quiet
// house and a noisy one read the same, which is what a light reacting to speech needs and the opposite
// of what a sensor about the room needs. This is against full scale, so a fan, a dishwasher or the TV
// moves it.
//
// It is taken from the mix before the noise reduction and the leveling, which would take the room out
// of it and put gain back on: what is left is the room, shifted only by the analog gain.
//
// A window the device's own playback reached is thrown away, since what the microphones heard then is
// mostly the speaker. The loopback says so, for anything that plays through the device, and the same
// tail the echo canceler keeps after the loopback goes quiet covers the room still ringing.
type noiseMeter struct {
	on atomic.Bool

	// ref and hold belong to the reader alone: the loopback of this frame, and the samples still to
	// count as playing after it went quiet.
	ref  []int16
	hold int

	// The window, between reads.
	mu     sync.Mutex
	sum    float64
	n      int
	played bool
}

// add counts one frame of the mix, with the raw capture it came from for the loopback. It does nothing
// while the meter is off, which is nearly always.
func (m *noiseMeter) add(raw []byte, frame []int16) {
	if !m.on.Load() || len(frame) == 0 {
		return
	}
	n := len(frame)
	if cap(m.ref) < n {
		m.ref = make([]int16, n)
	}
	m.ref = m.ref[:n]
	referenceInto(raw, m.ref)

	played := false
	switch {
	case playing(m.ref):
		m.hold = refHold
		played = true
	case m.hold > 0:
		m.hold -= n
		played = true
	}

	var sum float64
	for _, v := range frame {
		sum += float64(v) * float64(v)
	}

	m.mu.Lock()
	m.sum += sum
	m.n += n
	m.played = m.played || played
	m.mu.Unlock()
}

// read ends the window and starts the next. It answers false when the window has nothing to say about
// the room: nothing was heard, the device played something, or what was heard is digital silence, which
// is the microphones cut rather than a quiet room.
func (m *noiseMeter) read() (float64, bool) {
	m.mu.Lock()
	sum, n, played := m.sum, m.n, m.played
	m.sum, m.n, m.played = 0, 0, false
	m.mu.Unlock()

	if n == 0 || played {
		return 0, false
	}
	mean := sum / float64(n)
	if mean < 1 {
		return 0, false
	}
	return 10 * math.Log10(mean/(fullScale*fullScale)), true
}

// SetNoise starts or stops measuring the room for the ambient noise sensor. Either way the window starts
// again. The playback tail is not reset: it belongs to the reader, and a window that starts within a
// second of playback is one to throw away anyway.
func (s *Source) SetNoise(on bool) {
	s.noise.on.Store(on)
	s.noise.read()
}

// Noise is how loud the room was since the last time it was asked, in dBFS, and asking starts a new
// window. False when there is no reading: measuring is off, nothing was heard, the device played
// something in that time, or the microphones are cut.
func (s *Source) Noise() (float64, bool) {
	if !s.noise.on.Load() {
		return 0, false
	}
	return s.noise.read()
}
