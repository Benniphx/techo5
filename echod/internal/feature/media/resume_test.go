package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

// liveFor makes what plays live radio (or not) for one test, with waits short enough to test.
func liveFor(t *testing.T, isLive bool) {
	t.Helper()
	oldLive, oldWaits := liveCheck.Load(), resumeWaits
	t.Cleanup(func() { liveCheck.Store(oldLive); resumeWaits = oldWaits })
	SetLive(func() bool { return isLive })
	resumeWaits = [resumeTries]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
}

var errReset = errors.New("read tcp: connection reset by peer")

// The case that started this: a station Home Assistant's proxy cuts off after half an hour is fetched
// again, every time it happens.
func TestALiveStationThatDropsIsFetchedAgain(t *testing.T) {
	liveFor(t, true)
	tries := 0
	for drop := range 5 {
		if !resume(context.Background(), errReset, 30*time.Minute, &tries) {
			t.Fatalf("drop %d after half an hour of playing was not fetched again", drop+1)
		}
	}
	// Ended cleanly is no different for radio: a station has no end.
	if !resume(context.Background(), nil, time.Hour, &tries) {
		t.Fatal("a station whose stream ended cleanly was not fetched again")
	}
}

// A song or a podcast is never fetched again: it would start over from its beginning.
func TestWhatIsNotRadioIsNotFetchedAgain(t *testing.T) {
	liveFor(t, false)
	tries := 0
	if resume(context.Background(), errReset, time.Hour, &tries) {
		t.Fatal("a track that is not live radio was fetched again")
	}
	liveCheck.Store(nil)
	if resume(context.Background(), errReset, time.Hour, &tries) {
		t.Fatal("with no radio feature to ask, a track was fetched again")
	}
}

// Stopped or replaced is somebody's doing: never undone.
func TestAStoppedStationStaysStopped(t *testing.T) {
	liveFor(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tries := 0
	if resume(ctx, errReset, time.Hour, &tries) {
		t.Fatal("a stopped track was fetched again")
	}
}

// A stream the device cannot decode is handed on (library.go), not fetched again.
func TestAnUndecodableStreamIsNotFetchedAgain(t *testing.T) {
	liveFor(t, true)
	tries := 0
	if resume(context.Background(), unplayable{format: "not audio"}, time.Hour, &tries) {
		t.Fatal("an undecodable stream was fetched again")
	}
}

// A station that is gone fails at once every time: after a few quick tries the track ends, as before.
func TestAStationThatIsGoneIsGivenUpAfterAFewTries(t *testing.T) {
	liveFor(t, true)
	tries := 0
	n := 0
	for resume(context.Background(), errReset, time.Second, &tries) {
		n++
		if n > 10 {
			t.Fatal("a station that fails at once was tried forever")
		}
	}
	if n != resumeTries {
		t.Fatalf("tried %d times, want %d", n, resumeTries)
	}
}

// A try that plays for a while counts as a fresh drop: the quick tries before it are forgotten.
func TestPlayingAWhileStartsTheCountOver(t *testing.T) {
	liveFor(t, true)
	tries := 0
	for range resumeTries - 1 {
		resume(context.Background(), errReset, time.Second, &tries)
	}
	if !resume(context.Background(), errReset, resumeFresh, &tries) || tries != 1 {
		t.Fatalf("after a try that played %v, tries is %d, want the count started over at 1", resumeFresh, tries)
	}
}
