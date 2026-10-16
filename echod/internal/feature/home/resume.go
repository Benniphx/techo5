package home

import (
	"log/slog"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// Radio that puts itself back on when the stream drops.
//
// A station does not end. What ends is the thing carrying it: Home Assistant hands the device a URL
// from its own proxy, and when that proxy stops — five minutes in, or thirteen, with no pattern to
// it — the device plays the stream out and reports it finished, correctly and uselessly. What the
// room hears is the music stopping for no reason, and somebody has to go and start it again.
//
// So a stream that ends by itself while a station is playing is put back on. A stream that was
// stopped, paused or replaced is not: those are somebody's doing, and undoing them would be a device
// arguing with the person in front of it.

const (
	// resumeAfter is the wait before putting it back: long enough that a station which is genuinely
	// gone does not hammer Home Assistant, short enough to sound like a hiccup rather than a silence.
	resumeAfter = 3 * time.Second

	// askedFor is how long after asking for a station a dropped stream is still taken to be that
	// station's. Beyond it the device has been playing something else for hours and what dropped is
	// nobody's business of ours.
	askedFor = 12 * time.Hour

	// resumeTries is how many times in resumeWindow a station may be put back before this gives up
	// and leaves it off. A stream that drops three times in ten minutes is not going to keep playing,
	// and a device that keeps trying forever is worse than one that stops.
	resumeTries  = 3
	resumeWindow = 10 * time.Minute
)

// ended is a stream that stopped of its own accord. Whether it is worth putting back is decided
// here: only the stream that is playing now, only while a station is meant to be on.
func (f *Feature) ended(url string) {
	f.mu.Lock()
	playing, station := f.url, f.asked
	if station != "" && time.Since(f.askedAt) > askedFor {
		station = ""
	}
	// Only the station's own stream: a song or a sleep sound that played after it, even with the
	// station's name still about, is not the station, and its end is nothing to put back.
	if url != f.stationURL {
		station = ""
	}
	now := time.Now()
	if now.Sub(f.resumedAt) > resumeWindow {
		f.resumed = 0
	}
	tries := f.resumed
	own, gen := url == f.ownURL, f.stationGen
	f.mu.Unlock()

	if url != playing || station == "" {
		return
	}
	if tries >= resumeTries {
		slog.Warn("radio: the stream keeps dropping, leaving it off",
			"station", station, "tries", tries, "within", resumeWindow)
		return
	}

	f.mu.Lock()
	f.resumed, f.resumedAt = tries+1, now
	f.mu.Unlock()

	slog.Info("radio: the stream ended by itself, putting it back on",
		"station", station, "try", tries+1)
	time.AfterFunc(resumeAfter, func() { f.putBack(station, url, own, gen) })
}

// mediaBusy is whether the player has something playing or paused; a variable for the tests.
var mediaBusy = func() bool {
	playing, paused := media.Get().Playing()
	return playing || paused
}

// putBack starts station again after its stream (url) ended by itself: one this device played itself
// (own) from the same url, one Home Assistant played by asking for it again. Anything that started in
// the meantime is what somebody wanted instead (a phone, a cast, another stream), and a Stop meanwhile
// is somebody wanting nothing. gen is the station's generation when it ended (stationGen): the play is
// made only while it still is, checked again as it starts, so a Stop up to that moment wins.
func (f *Feature) putBack(station, url string, own bool, gen uint64) {
	if !f.stillWanted(station, url, gen) {
		return
	}
	if mediaBusy() {
		slog.Info("radio: something else plays now; the station is not put back on", "station", station)
		return
	}
	if own {
		f.playStream(station, url, &gen)
		return
	}
	f.play(station, &gen)
}

// stillWanted is whether station, whose stream url ended in generation gen, is still to be put back
// on: nothing else has started, and the station has not been stopped or replaced by another asked for.
func (f *Feature) stillWanted(station, url string, gen uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stationGen == gen && f.url == url && f.asked == station && f.stationURL == url
}

// forgetStation is the station over (a Stop, here or anywhere the player is stopped): nothing that
// plays after it is put back on or taken for it. Under f.mu.
func (f *Feature) forgetStation() {
	f.asked, f.claiming, f.claimName, f.stationURL = "", false, "", ""
	f.stationGen++
}

// playingOwn is a stream this device is about to play itself, for station name, through media's
// PlayURL: what plays now, as a stream Home Assistant sends is (played), so its end can be put back on
// as theirs can. Under f.mu.
func (f *Feature) playingOwn(name, url string) {
	f.url, f.urlName, f.ownURL = url, name, url
}

// liveClaim is how long after a station is asked for the next stream to start is taken to be it: the
// seconds Home Assistant takes to resolve a favorite and send its url, with room to spare. A stream
// that starts later than that, with nothing asked for since, is something else.
const liveClaim = 20 * time.Second

// stationClaim is liveClaim for a station Home Assistant names as started (stationStarted): shorter,
// since the name is the whole house's, and a station started for another room names itself too.
const stationClaim = 5 * time.Second

// askStation is a station being asked for: what the next stream to start is (liveURL). Under f.mu.
func (f *Feature) askStation(station string) {
	f.asked, f.askedAt = station, time.Now()
	f.claim(station, liveClaim)
	f.stationGen++
}

// claim takes the next stream to start within claimFor for station (liveURL). Under f.mu.
func (f *Feature) claim(station string, claimFor time.Duration) {
	f.claiming, f.claimName, f.claimAt, f.claimFor = true, station, time.Now(), claimFor
}

// unask is a station asked for that will not start after all (it is not on the list, or Home
// Assistant said no): the next stream is not taken for it. Only while it is still the one asked for.
func (f *Feature) unask(station string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked == station {
		f.claiming = false
	}
}

// stationStarted is Home Assistant's "last station" text naming a new station: one started from Home
// Assistant itself (a dashboard, an automation running the favorites script), not asked for here. The
// next stream to start is taken for it, as for one asked for here; but not when a stream was just taken
// for a station, whose name this is arriving after it.
func (f *Feature) stationStarted(u hastate.Update) {
	entity := config.Get().Home.Radio.Now
	if entity == "" || u.Entity != entity || u.Attribute != "" || u.First {
		return
	}
	if u.Value == "" || u.Value == "unknown" || u.Value == "unavailable" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.claimedAt) < liveClaim {
		return
	}
	// A claim still waiting for its stream stays as it is: one asked for here keeps its time and its
	// name, which the house's text may be giving another room's station.
	if f.claiming && time.Since(f.claimAt) < f.claimFor {
		return
	}
	f.claim(u.Value, stationClaim)
}

// liveURL says whether url, as its track starts, is a live station (media.SetLive): the first stream
// to start after a station was asked for, or that same stream again. Decided here, once, rather than
// from what the radio page names as playing, which lingers after the station: a song, a clip or a file
// of Home Assistant's that follows the radio is not radio, and fetched again it would play again.
func (f *Feature) liveURL(url string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claiming && time.Since(f.claimAt) < f.claimFor {
		f.claiming, f.stationURL, f.claimedAt = false, url, time.Now()
		// The station this is: one started from Home Assistant becomes the one put back on when its
		// stream ends (ended), rather than whatever was asked for here before it.
		f.asked, f.askedAt = f.claimName, f.claimedAt
		return true
	}
	f.claiming = false
	return url != "" && url == f.stationURL
}
