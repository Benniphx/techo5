package home

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
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
			what: "the stream that is playing, after a station was asked for",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt, f.stationURL = "http://p/1", "KXYZ", time.Now(), "http://p/1"
			},
			ended:   "http://p/1",
			wantTry: true,
		},
		{
			what: "a stream that is no longer the one playing",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt, f.stationURL = "http://p/2", "KXYZ", time.Now(), "http://p/1"
			},
			ended: "http://p/1",
		},
		{
			what: "a song played after the station, with the station still the last asked for",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt, f.stationURL = "http://p/song", "KXYZ", time.Now(), "http://p/1"
			},
			ended: "http://p/song",
		},
		{
			what:  "a stream nobody asked this device for",
			set:   func(f *Feature) { f.url = "http://p/1" },
			ended: "http://p/1",
		},
		{
			what: "a station asked for so long ago that this is something else",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt, f.stationURL = "http://p/1", "KXYZ", time.Now().Add(-askedFor-time.Minute), "http://p/1"
			},
			ended: "http://p/1",
		},
		{
			what: "a stream that has already been put back on too many times",
			set: func(f *Feature) {
				f.url, f.asked, f.askedAt, f.stationURL = "http://p/1", "KXYZ", time.Now(), "http://p/1"
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
	f.url, f.asked, f.askedAt, f.stationURL = "http://p/1", "KXYZ", time.Now(), "http://p/1"
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
	f.claimAt = time.Now().Add(-liveClaim - time.Second)
	f.mu.Unlock()
	if f.liveURL("http://p/song") {
		t.Error("a stream starting long after a station was asked for was taken for live radio")
	}
}

// A station that will not start after all (not on the list, or Home Assistant said no) leaves no claim
// behind for whatever starts next.
func TestAStationThatDidNotStartClaimsNothing(t *testing.T) {
	f := &Feature{}
	f.mu.Lock()
	f.askStation("KXYZ")
	f.mu.Unlock()
	f.unask("KXYZ")
	if f.liveURL("http://p/song") {
		t.Error("a stream after a station that did not start was taken for live radio")
	}
	// Only the station still asked for: a later ask is not undone by an earlier one failing.
	f.mu.Lock()
	f.askStation("KABC")
	f.mu.Unlock()
	f.unask("KXYZ")
	if !f.liveURL("http://p/2") {
		t.Error("an earlier station's failure undid the claim of the one asked for since")
	}
}

// A station started from Home Assistant itself, which names it in the "last station" text, has its
// stream taken for live radio as one asked for here would; but not the name of one just started here,
// arriving after its stream, and not the value that was there at start.
func TestAStationStartedFromHomeAssistantIsLive(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Radio(config.Radio{Now: "input_text.last_station"}); err != nil {
		t.Fatal(err)
	}
	now := func(v string, first bool) hastate.Update {
		return hastate.Update{Key: hastate.Key{Entity: "input_text.last_station"}, Value: v, First: first}
	}
	f := &Feature{}
	f.stationStarted(now("KXYZ", true))
	if f.liveURL("http://p/song") {
		t.Error("the value at start armed a claim")
	}
	f.stationStarted(now("unavailable", false))
	if f.liveURL("http://p/song") {
		t.Error("an unavailable text armed a claim")
	}
	f.stationStarted(now("KXYZ", false))
	if !f.liveURL("http://p/1") {
		t.Fatal("the stream of a station started from Home Assistant was not taken for live radio")
	}
	// The name arriving just after the stream it names: no second claim for the next stream.
	f.stationStarted(now("KABC", false))
	if f.liveURL("http://p/clip") {
		t.Error("a station's name arriving after its stream armed a claim for the next one")
	}
}

// A Stop in the seconds before a put-back cancels it, as does another station asked for meanwhile.
func TestAStopCancelsAPutBack(t *testing.T) {
	f := &Feature{}
	f.url, f.asked, f.askedAt, f.stationURL = "http://p/1", "KXYZ", time.Now(), "http://p/1"
	if !f.stillWanted("KXYZ", "http://p/1", f.stationGen) {
		t.Fatal("a station that dropped with nothing else going on is not to be put back")
	}
	f.mu.Lock()
	f.forgetStation()
	f.mu.Unlock()
	if f.stillWanted("KXYZ", "http://p/1", f.stationGen) {
		t.Error("a station stopped before its put-back is still put back")
	}
	f.mu.Lock()
	f.asked, f.stationURL = "KABC", "http://p/1"
	f.mu.Unlock()
	if f.stillWanted("KXYZ", "http://p/1", f.stationGen) {
		t.Error("a station replaced by another asked for is still put back")
	}
}

// A station started from Home Assistant is the one put back on when its stream ends, not one asked for
// on the device before it.
func TestAStationStartedFromHomeAssistantIsTheOnePutBack(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Radio(config.Radio{Now: "input_text.last_station"}); err != nil {
		t.Fatal(err)
	}
	f := &Feature{}
	f.mu.Lock()
	f.askStation("KABC")
	f.askedAt = time.Now().Add(-time.Hour)
	f.claiming = false
	f.mu.Unlock()
	f.stationStarted(hastate.Update{Key: hastate.Key{Entity: "input_text.last_station"}, Value: "KXYZ"})
	if !f.liveURL("http://p/2") {
		t.Fatal("the stream of a station started from Home Assistant was not taken for it")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked != "KXYZ" {
		t.Errorf("the station to put back is %q, want the one Home Assistant started", f.asked)
	}
}

// The claim made for a name in the house-wide "last station" text holds only seconds; one asked for on
// the device holds longer.
func TestTheClaimForAStationStartedFromHomeAssistantIsShort(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Radio(config.Radio{Now: "input_text.last_station"}); err != nil {
		t.Fatal(err)
	}
	started := func(f *Feature, ago time.Duration) {
		f.stationStarted(hastate.Update{Key: hastate.Key{Entity: "input_text.last_station"}, Value: "KXYZ"})
		f.mu.Lock()
		f.claimAt = time.Now().Add(-ago)
		f.mu.Unlock()
	}
	f := &Feature{}
	started(f, stationClaim-time.Second)
	if !f.liveURL("http://p/1") {
		t.Error("a stream just after Home Assistant named a station was not taken for it")
	}
	f = &Feature{}
	started(f, stationClaim+time.Second)
	if f.liveURL("http://p/1") {
		t.Error("a stream long after Home Assistant named a station was taken for it")
	}
	f = &Feature{}
	f.mu.Lock()
	f.askStation("KABC")
	f.claimAt = time.Now().Add(-stationClaim - time.Second)
	f.mu.Unlock()
	if !f.liveURL("http://p/2") {
		t.Error("a station asked for on the device lost its claim as soon as one named by Home Assistant")
	}
}

// A station the device plays itself (its own, one from the setup page, one the voice assistant found)
// is put back on when its stream ends by itself, as one Home Assistant sends is: the action button's
// way in (PlayOwn) included, which does not go through Play.
func TestAStationTheDevicePlaysItselfIsPutBack(t *testing.T) {
	played := ownPlayer(t)
	f := &Feature{}
	if !f.PlayOwn("KXYZ") {
		t.Fatal("the device's own station did not play")
	}
	if got := played(); len(got) != 1 || got[0] != "http://p/own" {
		t.Fatalf("played %v", got)
	}
	if !f.liveURL("http://p/own") {
		t.Fatal("the device's own station, started by PlayOwn, was not taken for live radio")
	}
	f.mu.Lock()
	gen := f.stationGen
	f.mu.Unlock()
	f.ended("http://p/own")
	f.mu.Lock()
	tried := f.resumed
	f.forgetStation() // the put-back on its timer is not this test's
	f.mu.Unlock()
	if tried != 1 {
		t.Fatalf("a station the device played itself was put back %d times, want once", tried)
	}
	// The put-back itself, as its timer makes it: from the same address.
	f.mu.Lock()
	f.askStation("KXYZ")
	f.playingOwn("KXYZ", "http://p/own")
	f.stationURL = "http://p/own"
	gen = f.stationGen
	f.mu.Unlock()
	f.putBack("KXYZ", "http://p/own", true, gen)
	if got := played(); len(got) != 2 || got[1] != "http://p/own" {
		t.Fatalf("the put-back played %v", got)
	}
}

// ownPlayer keeps one station on the device (KXYZ at http://p/own), records what the device is asked
// to play itself instead of playing it, and has nothing else playing.
func ownPlayer(t *testing.T) func() []string {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := SetOwnStations([]config.Station{{Name: "KXYZ", URL: "http://p/own"}}); err != nil {
		t.Fatal(err)
	}
	oldPlay, oldBusy := playURL, mediaBusy
	t.Cleanup(func() { playURL, mediaBusy = oldPlay, oldBusy })
	var mu sync.Mutex
	var played []string
	playURL = func(url string) {
		mu.Lock()
		played = append(played, url)
		mu.Unlock()
	}
	mediaBusy = func() bool { return false }
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(played)
	}
}

// A put-back is not made after a Stop (here or anywhere the player was stopped), after another station
// was asked for, or while something else plays.
func TestAPutBackGivesWay(t *testing.T) {
	played := ownPlayer(t)
	ready := func() (*Feature, uint64) {
		f := &Feature{}
		f.mu.Lock()
		f.askStation("KXYZ")
		f.playingOwn("KXYZ", "http://p/own")
		f.stationURL = "http://p/own"
		gen := f.stationGen
		f.mu.Unlock()
		return f, gen
	}
	f, gen := ready()
	f.mu.Lock()
	f.forgetStation()
	f.mu.Unlock()
	f.putBack("KXYZ", "http://p/own", true, gen)

	f, gen = ready()
	f.mu.Lock()
	f.askStation("KXYZ") // the same station asked for again meanwhile: that ask stands, not this
	f.mu.Unlock()
	f.putBack("KXYZ", "http://p/own", true, gen)

	f, gen = ready()
	mediaBusy = func() bool { return true }
	f.putBack("KXYZ", "http://p/own", true, gen)
	if got := played(); len(got) != 0 {
		t.Errorf("put back %v", got)
	}
}

// A claim asked for on the device and still waiting for its stream is not replaced by a name in the
// house's "last station" text: it keeps its time, and its station.
func TestAPendingClaimKeepsItsTimeAndName(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Radio(config.Radio{Now: "input_text.last_station"}); err != nil {
		t.Fatal(err)
	}
	f := &Feature{}
	f.mu.Lock()
	f.askStation("KABC")
	f.mu.Unlock()
	f.stationStarted(hastate.Update{Key: hastate.Key{Entity: "input_text.last_station"}, Value: "KXYZ"})
	f.mu.Lock()
	f.claimAt = time.Now().Add(-8 * time.Second)
	f.mu.Unlock()
	if !f.liveURL("http://p/1") {
		t.Fatal("a claim asked for here lost its time to a name in the house's text")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked != "KABC" {
		t.Errorf("the station is %q, renamed by the house's text", f.asked)
	}
}

// A station the voice assistant asked for that does not play leaves neither its name on what plays
// nor its address to put back.
func TestAStationThatDidNotPlayLeavesNoName(t *testing.T) {
	old := playURLChecked
	t.Cleanup(func() { playURLChecked = old })
	playURLChecked = func(string, time.Duration) error { return errors.New("nothing arrived") }
	f := &Feature{}
	if err := f.PlayStreamChecked("KXYZ", "http://p/gone", time.Second); err == nil {
		t.Fatal("a station that did not play said it did")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.urlName != "" || f.ownURL != "" {
		t.Errorf("a station that did not play left name %q, address %q", f.urlName, f.ownURL)
	}
}
