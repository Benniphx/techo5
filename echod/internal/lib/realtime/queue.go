package realtime

import (
	"errors"
	"sync"
)

// StartBufferBytes holds 500 ms of wake history and up to five seconds of
// connection setup, at the hardware's 16 kHz mono PCM16 rate.
const StartBufferBytes = 176000

// Queue decouples microphone capture from network writes. It never silently
// drops audio: overflow ends the session through an explicit capture error.
type Queue struct {
	mu           sync.Mutex
	samples      []int16
	start, count int
	ready        chan struct{}
}

func NewQueue() *Queue {
	return &Queue{samples: make([]int16, StartBufferBytes/2), ready: make(chan struct{}, 1)}
}

func (q *Queue) Push(samples []int16) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(samples) > len(q.samples)-q.count {
		return errors.New("realtime microphone queue overflow")
	}
	if len(samples) == 0 {
		return nil
	}
	end := (q.start + q.count) % len(q.samples)
	n := copy(q.samples[end:], samples)
	copy(q.samples, samples[n:])
	q.count += len(samples)
	q.signal()
	return nil
}

func (q *Queue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// pop bounds each write to 100 ms, keeping control events responsive even
// when connection setup accumulated a full startup buffer.
func (q *Queue) pop() []int16 {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count == 0 {
		return nil
	}
	result := make([]int16, min(q.count, 1600))
	n := copy(result, q.samples[q.start:min(q.start+len(result), len(q.samples))])
	copy(result[n:], q.samples[:len(result)-n])
	clear(q.samples[q.start : q.start+n])
	clear(q.samples[:len(result)-n])
	q.start = (q.start + len(result)) % len(q.samples)
	q.count -= len(result)
	if q.count > 0 {
		q.signal()
	}
	return result
}

func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	clear(q.samples)
	q.start, q.count = 0, 0
}
