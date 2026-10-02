package voice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	rtapi "github.com/HuskerMinion/techo5/echod/internal/lib/realtime"
)

func TestRealtimeFlushResetsAudioAndRejectsOverflow(t *testing.T) {
	a := &realtimeAudio{p: speaker.New()}
	if err := a.push(make([]byte, realtimeQueueSamples/2)); err != nil {
		t.Fatal(err)
	}
	if err := a.push([]byte{1, 0}); !errors.Is(err, rtapi.ErrAudioWouldBlock) {
		t.Fatal("playback backpressure ignored")
	}
	a.flush()
	if len(a.pcm) != 0 {
		t.Fatal("interrupt retained playback")
	}
	a.push([]byte{0, 100})
	a.flush()
	a.push(make([]byte, 100))
	if !slices.Equal(a.pcm, make([]int16, len(a.pcm))) {
		t.Fatal("interrupt retained resampler history")
	}
}
func TestRealtimeQueuePushAndFlushRace(t *testing.T) {
	a := &realtimeAudio{p: speaker.New()}
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		for i := 0; i < 500; i++ {
			a.push(make([]byte, 1920))
		}
	}()
	go func() {
		defer group.Done()
		for i := 0; i < 500; i++ {
			a.flush()
		}
	}()
	group.Wait()
	a.flush()
	if !a.empty() {
		t.Fatal("flush did not empty player")
	}
}
func TestPilotConfigRejectsMissingAndUnknownFields(t *testing.T) {
	for _, data := range []string{`{`, `{"enabled":true}`, `{"enabled":true,"key_file":"/tmp/t","skip_verify":true}`, `{"enabled":false} {}`, `{"enabled":true,"key_file":"/tmp/t","tools":"all"}`} {
		if _, err := decodeRealtimeConfig([]byte(data)); err == nil {
			t.Fatalf("unsafe config accepted: %s", data)
		}
	}
	c, err := decodeRealtimeConfig([]byte(`{"enabled":true,"key_file":"/tmp/t","tools":"read"}`))
	if err != nil || !c.Enabled || c.Tools != "read" {
		t.Fatal(err)
	}
}

func TestPlaybackTruncationExcludesQueuedAndHardwareTail(t *testing.T) {
	if got := playedMilliseconds(48000, 4800); got != 750 {
		t.Fatalf("truncated at %dms, want 750ms", got)
	}
	if got := playedMilliseconds(4800, 0); got != 0 {
		t.Fatal("counted hardware tail as heard")
	}
	a := &realtimeAudio{p: speaker.New(), framesSent: 48000}
	a.begin()
	if got := a.playedMS(); got != 0 {
		t.Fatal("new output retained prior item's time")
	}
	if a.p.TryPlay([]int16{1, 1}) {
		t.Fatal("unavailable speaker accepted playback")
	}
}

func TestMicQueueFailureSurvivesSocketCancellationRace(t *testing.T) {
	q := rtapi.NewQueue()
	if err := q.Push(make([]int16, rtapi.StartBufferBytes/2)); err != nil {
		t.Fatal(err)
	}
	overflow := q.Push([]int16{1})
	if overflow == nil {
		t.Fatal("expected overflow")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	// The writer/reader return context.Canceled while capture has already
	// published its cause. Both select branches are ready at this point.
	cancel(overflow)
	if got := realtimeOutcome(ctx, context.Canceled); got != overflow || errors.Is(got, context.Canceled) {
		t.Fatalf("capture failure was mistaken for local Stop: %v", got)
	}
	if got := realtimeOutcome(ctx, ctx.Err()); got != overflow {
		t.Fatalf("ctx.Done branch lost capture failure: %v", got)
	}
	cancel(context.Canceled)
	if got := realtimeOutcome(ctx, context.Canceled); got != overflow {
		t.Fatal("cleanup overwrote failure cause")
	}
	stopCtx, stop := context.WithCancelCause(context.Background())
	stop(context.Canceled)
	if !errors.Is(realtimeOutcome(stopCtx, context.Canceled), context.Canceled) {
		t.Fatal("intentional Stop became a backend failure")
	}
}

func TestRealtimeKeyRejectsPublicFilesSymlinksAndMalformedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	for _, tc := range []struct {
		value string
		mode  os.FileMode
		ok    bool
	}{
		{"test-key\n", 0600, true}, {"test-key", 0644, false}, {"", 0600, false}, {"one\ntwo", 0600, false}, {string(make([]byte, 4097)), 0600, false},
	} {
		if err := os.WriteFile(path, []byte(tc.value), tc.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, tc.mode); err != nil {
			t.Fatal(err)
		}
		_, err := readRealtimeKey(path)
		if (err == nil) != tc.ok {
			t.Fatalf("key acceptance mismatch for mode %o", tc.mode)
		}
	}
	if err := os.WriteFile(path, []byte("test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readRealtimeKey(link); err == nil {
		t.Fatal("credential symlink accepted")
	}
}

func TestRealtimeTranscriptsCoalesceByRoleAndClearOnInterrupt(t *testing.T) {
	m := newRealtimeTranscripts()
	for i := 0; i < 1000; i++ {
		m.put(rtapi.Event{Type: "transcript", Role: "assistant", Content: "older"})
	}
	m.put(rtapi.Event{Type: "transcript", Role: "user", Content: "question"})
	m.put(rtapi.Event{Type: "transcript", Role: "assistant", Content: "latest"})
	got := m.take()
	if len(got) != 2 || got[0].Content != "question" || got[1].Content != "latest" {
		t.Fatalf("latest snapshots lost: %v", got)
	}
	m.put(rtapi.Event{Type: "transcript", Role: "assistant", Content: "stale"})
	m.clear()
	if len(m.take()) != 0 {
		t.Fatal("interrupt retained stale transcript")
	}
}
