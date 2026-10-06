package media

import (
	"os"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// pipeSrc is a received track's source over an os.Pipe.
type pipeSrc struct{ *os.File }

func (p pipeSrc) SetReadDeadline(t time.Time) error { return p.File.SetReadDeadline(t) }

// How far into a received track the room has heard follows what went into the queue, less the card's
// latency, and only for the track that is loaded.
func TestHeardFollowsTheReceivedTrack(t *testing.T) {
	s := NewStream(speaker.NewDriver(speaker.New()), speaker.New(), func() {}, func(string) {})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, ok := s.Heard("Video"); ok {
		t.Fatal("heard a track that is not playing")
	}
	s.PlayPCM("Video", pipeSrc{r}, speaker.Rate, 2)
	// Half a second of stereo.
	go func() { _, _ = w.Write(make([]byte, speaker.Rate/2*4)) }()
	want := time.Second/2 - speaker.OutputLatency
	deadline := time.Now().Add(5 * time.Second)
	for {
		at, ok := s.Heard("Video")
		if ok && at == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heard %v (%v), want %v", at, ok, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := s.Heard("Bluetooth"); ok {
		t.Error("heard a track by another name")
	}
	s.Stop()
	if _, ok := s.Heard("Video"); ok {
		t.Error("heard a track that was stopped")
	}
}

// Samples that arrive while the track is held up are kept for it, not dropped: dropped, a video's
// sound would run ahead of its picture by them.
func TestAHeldTrackKeepsWhatArrives(t *testing.T) {
	s := NewStream(speaker.NewDriver(speaker.New()), speaker.New(), func() {}, func(string) {})
	tr, _ := s.start(&track{item: "Video", received: true})
	s.Pause()
	if s.offer(tr, make([]int16, 100)) {
		t.Fatal("a paused track took samples")
	}
	s.Unpause()
	if !s.offer(tr, make([]int16, 100)) || tr.queued != 100 {
		t.Fatalf("queued %d", tr.queued)
	}
	s.Stop()
	if !s.offer(tr, make([]int16, 100)) || tr.queued != 100 {
		t.Error("a track that is over took samples, or held them")
	}
}
