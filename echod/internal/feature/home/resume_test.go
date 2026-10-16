package home

import (
	"testing"
	"time"
)

// What may be put back on and what may not. A device that restarts what somebody just stopped is
// worse than one that never restarts anything, so the cases that must do nothing are the ones worth
// holding: only the stream that is playing, only a station this device asked for, only so often.
func TestWhatIsWorthPuttingBackOn(t *testing.T) {
	for _, c := range []struct {
		what    string
		set     func(f *Feature)
		ended   string
		wantTry bool
	}{
		{
			what:    "the stream that is playing, after a station was asked for",
			set:     func(f *Feature) { f.url, f.asked, f.askedAt = "http://p/1", "KXYZ", time.Now() },
			ended:   "http://p/1",
			wantTry: true,
		},
		{
			what:  "a stream that is no longer the one playing",
			set:   func(f *Feature) { f.url, f.asked, f.askedAt = "http://p/2", "KXYZ", time.Now() },
			ended: "http://p/1",
		},
		{
			what:  "a stream nobody asked this device for",
			set:   func(f *Feature) { f.url = "http://p/1" },
			ended: "http://p/1",
		},
		{
			what: "a station asked for so long ago that this is something else",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt = "http://p/1", "KXYZ", time.Now().Add(-askedFor-time.Minute)
			},
			ended: "http://p/1",
		},
		{
			what: "a stream that has already been put back on too many times",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt = "http://p/1", "KXYZ", time.Now()
				f.resumed, f.resumedAt = resumeTries, time.Now()
			},
			ended: "http://p/1",
		},
	} {
		f := &Feature{}
		c.set(f)
		f.ended(c.ended)

		f.mu.Lock()
		tried := f.resumed > 0 && f.resumedAt.After(time.Time{})
		if c.set != nil && f.resumed == resumeTries && !c.wantTry {
			tried = false // the run it started with, not a try this call made
		}
		f.mu.Unlock()

		if tried != c.wantTry {
			t.Errorf("%s: put back = %t, want %t", c.what, tried, c.wantTry)
		}
	}
}

// The run of tries is forgotten once a stream has stayed up for a while, so an evening of listening
// is not ended by five drops spread across it.
func TestTheRunOfTriesIsForgotten(t *testing.T) {
	f := &Feature{}
	f.url, f.asked, f.askedAt = "http://p/1", "KXYZ", time.Now()
	f.resumed, f.resumedAt = resumeTries, time.Now().Add(-resumeWindow-time.Minute)

	f.ended("http://p/1")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resumed != 1 {
		t.Errorf("after the window passed the count is %d, expected a fresh first try", f.resumed)
	}
}

// Live radio is the stream that starts for a station this device asked for, decided as it starts: not
// whatever plays while a station's name lingers on the radio page.
func TestLiveIsTheStreamStartedForAStation(t *testing.T) {
	f := &Feature{}
	if f.liveURL("http://p/song") {
		t.Error("with no station asked for, a stream was taken for live radio")
	}

	f.mu.Lock()
	f.askStation("KXYZ")
	f.meta.station = "KXYZ"
	f.mu.Unlock()
	if !f.liveURL("http://p/1") {
		t.Fatal("the stream that started for a station was not taken for live radio")
	}
	// What follows the station is not it, though the page still names the station.
	if f.liveURL("http://p/clip") {
		t.Error("a clip after the station was taken for live radio")
	}
	if f.liveURL("/local/sleep/rain.mp3") {
		t.Error("a file after the station was taken for live radio")
	}
	// The station's own stream started again (put back on) still is.
	if !f.liveURL("http://p/1") {
		t.Error("the station's stream started again was not taken for live radio")
	}

	// A new station: its stream is, and the one before it no longer is.
	f.mu.Lock()
	f.askStation("KABC")
	f.mu.Unlock()
	if !f.liveURL("http://p/2") || f.liveURL("http://p/1") {
		t.Error("switching stations did not go by the new station's stream")
	}

	// A station asked for long ago whose stream never came: what starts now is something else.
	f.mu.Lock()
	f.askStation("KDEF")
	f.askedAt = time.Now().Add(-liveClaim - time.Second)
	f.mu.Unlock()
	if f.liveURL("http://p/song") {
		t.Error("a stream starting long after a station was asked for was taken for live radio")
	}
}
