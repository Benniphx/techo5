package realtime

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"
)

func tone(rate, frequency int) []int16 {
	samples := make([]int16, rate)
	for i := range samples {
		samples[i] = int16(12000 * math.Sin(2*math.Pi*float64(frequency*i)/float64(rate)))
	}
	return samples
}

func pcm(samples []int16) []byte {
	result := make([]byte, 0, len(samples)*2)
	for _, sample := range samples {
		result = binary.LittleEndian.AppendUint16(result, uint16(sample))
	}
	return result
}

func TestInputResamplingToneDurationAndChunkInvariance(t *testing.T) {
	samples := tone(16000, 1000)
	var whole inputResampler
	want := whole.run(samples)
	if len(want) != 24000*2 {
		t.Fatalf("expected one second at 24kHz, got %d bytes", len(want))
	}
	for _, chunk := range []int{1, 2, 3, 37, 160, 511, 1600} {
		var streaming inputResampler
		var got []byte
		for at := 0; at < len(samples); at += chunk {
			got = append(got, streaming.run(samples[at:min(at+chunk, len(samples))])...)
		}
		if !slices.Equal(want, got) {
			t.Fatalf("rational phase or FIR history lost at chunk size %d", chunk)
		}
	}
	crossings, square := 0, 0.0
	for i := 240; i < 24000-1; i++ {
		current := int16(binary.LittleEndian.Uint16(want[i*2:]))
		next := int16(binary.LittleEndian.Uint16(want[(i+1)*2:]))
		if current <= 0 && next > 0 {
			crossings++
		}
		square += float64(current) * float64(current)
	}
	if crossings < 988 || crossings > 991 {
		t.Fatalf("1kHz tone shifted: %d crossings over 990ms", crossings)
	}
	rms := math.Sqrt(square / float64(24000-241))
	if rms < 8200 || rms > 8800 {
		t.Fatalf("unexpected interpolated signal gain: RMS %.1f", rms)
	}
}

func TestInputResamplingSuppressesImagesAndClips(t *testing.T) {
	var r inputResampler
	data := r.run(tone(16000, 1000))
	power := func(frequency int) float64 {
		re, im := 0.0, 0.0
		for i := 240; i < 24000; i++ {
			sample := float64(int16(binary.LittleEndian.Uint16(data[i*2:])))
			phase := 2 * math.Pi * float64(frequency*i) / 24000
			re += sample * math.Cos(phase)
			im += sample * math.Sin(phase)
		}
		return re*re + im*im
	}
	if image := power(7000) / power(1000); image > .0001 {
		t.Fatalf("interpolation imaging not suppressed: %.6f", image)
	}
	if clampSample(100000) != 32767 || clampSample(-100000) != -32768 {
		t.Fatal("PCM saturation wrapped instead of clipping")
	}
}

func TestOutputResamplingPreservesStereoToneAndHistory(t *testing.T) {
	samples := tone(24000, 1000)
	var whole, chunked Resampler
	want := whole.Run(pcm(samples))
	var got []int16
	for at := 0; at < len(samples); at += 37 {
		got = append(got, chunked.Run(pcm(samples[at:min(at+37, len(samples))]))...)
	}
	if len(got) != 48000*2 || !slices.Equal(got, want) {
		t.Fatal("24kHz to 48kHz duration or packet seam incorrect")
	}
	crossings := 0
	for i := 1000; i < len(got)-2; i += 2 {
		if got[i] != got[i+1] {
			t.Fatal("left-loopback and right output differ")
		}
		if got[i] <= 0 && got[i+2] > 0 {
			crossings++
		}
	}
	if crossings < 985 || crossings > 995 {
		t.Fatalf("output tone shifted: %d crossings", crossings)
	}
	chunked.Reset()
	for _, sample := range chunked.Run(make([]byte, 200)) {
		if sample != 0 {
			t.Fatal("interrupt retained old FIR tail")
		}
	}
}

func TestQueueIsBoundedOwnsSamplesAndPreservesOrder(t *testing.T) {
	q := NewQueue()
	q.Push(nil)
	select {
	case <-q.ready:
		t.Fatal("empty capture frame signalled input")
	default:
	}
	samples := make([]int16, StartBufferBytes/2)
	for i := range samples {
		samples[i] = int16(i)
	}
	want := slices.Clone(samples)
	if err := q.Push(samples); err != nil {
		t.Fatal(err)
	}
	clear(samples)
	if err := q.Push([]int16{1}); err == nil {
		t.Fatal("full startup queue silently discarded samples")
	}
	var got []int16
	for {
		frame := q.pop()
		if frame == nil {
			break
		}
		got = append(got, frame...)
	}
	if !slices.Equal(want, got) {
		t.Fatal("capture alias, overflow or queue wrap corrupted PCM")
	}
	if err := q.Push(want[:1700]); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(q.pop(), want[:1600]) || !slices.Equal(q.pop(), want[1600:1700]) {
		t.Fatal("ring wrap changed sample order")
	}
	q.Push([]int16{123})
	q.Clear()
	if q.pop() != nil {
		t.Fatal("clear retained capture frames")
	}
	for _, value := range q.samples {
		if value != 0 {
			t.Fatal("clear retained private microphone history")
		}
	}
}
