package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

// shortWaits makes the waits between tries short enough to test.
func shortWaits(t *testing.T) {
	t.Helper()
	oldWaits := resumeWaits
	t.Cleanup(func() { resumeWaits = oldWaits })
	resumeWaits = [resumeTries]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
}

var errReset = errors.New("read tcp: connection reset by peer")

// The case that started this: a station Home Assistant's proxy cuts off after half an hour is fetched
// again, every time it happens.
func TestALiveStationThatDropsIsFetchedAgain(t *testing.T) {
	shortWaits(t)
	tries := 0
	for drop := range 5 {
		if !resume(context.Background(), true, errReset, 30*time.Minute, &tries) {
			t.Fatalf("drop %d after half an hour of playing was not fetched again", drop+1)
		}
	}
	// Ended cleanly is no different for radio: a station has no end.
	if !resume(context.Background(), true, nil, time.Hour, &tries) {
		t.Fatal("a station whose stream ended cleanly was not fetched again")
	}
}

// A song or a podcast is never fetched again: it would start over from its beginning.
func TestWhatIsNotRadioIsNotFetchedAgain(t *testing.T) {
	shortWaits(t)
	tries := 0
	if resume(context.Background(), false, errReset, time.Hour, &tries) {
		t.Fatal("a track that is not live radio was fetched again")
	}
	if resume(context.Background(), false, nil, time.Minute, &tries) {
		t.Fatal("a track that is not live radio and ended cleanly was fetched again")
	}
}

// Stopped or replaced is somebody's doing: never undone.
func TestAStoppedStationStaysStopped(t *testing.T) {
	shortWaits(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tries := 0
	if resume(ctx, true, errReset, time.Hour, &tries) {
		t.Fatal("a stopped track was fetched again")
	}
}

// A stream the device cannot decode is handed on (library.go), not fetched again.
func TestAnUndecodableStreamIsNotFetchedAgain(t *testing.T) {
	shortWaits(t)
	tries := 0
	if resume(context.Background(), true, unplayable{format: "not audio"}, time.Hour, &tries) {
		t.Fatal("an undecodable stream was fetched again")
	}
}

// A station that is gone fails at once every time: after a few quick tries the track ends, as before.
func TestAStationThatIsGoneIsGivenUpAfterAFewTries(t *testing.T) {
	shortWaits(t)
	tries := 0
	n := 0
	for resume(context.Background(), true, errReset, time.Second, &tries) {
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
	shortWaits(t)
	tries := 0
	for range resumeTries - 1 {
		resume(context.Background(), true, errReset, time.Second, &tries)
	}
	if !resume(context.Background(), true, errReset, resumeFresh, &tries) || tries != 1 {
		t.Fatalf("after a try that played %v, tries is %d, want the count started over at 1", resumeFresh, tries)
	}
}
