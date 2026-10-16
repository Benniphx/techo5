package media

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// A live radio stream picks itself back up when it drops. What a station sends never ends of its own
// accord, but the way to it can: Home Assistant's ESPHome media proxy, which voice-started radio goes
// through, was seen cutting a station off every 30 minutes ("connection reset by peer", or nothing
// arriving at all, while the proxy's ffmpeg timed out), and the music simply stopped. So a track that is
// live radio and ends without anyone stopping it is fetched again from the same url, after a short
// wait, as one track: the media player goes on reporting it as playing, and the gap is a second or so.
//
// Only radio: a song or a podcast fetched again would start over from its beginning, and a clip that ends
// cleanly would play again. Whether a track is radio is decided once, when it starts, and kept with it
// (track.live): the radio feature says so through SetLive, for the url of a station it was asked to play.
// What happens to be named as playing later (a station's name lingering after it) never makes a track
// that is playing now live.
//
// A stream that had played for resumeFresh counts as a new drop, and is always tried again; one that
// fails sooner than that counts towards resumeTries, so a station that is gone ends after a few quick
// tries rather than being knocked on forever.
const (
	resumeFresh = 20 * time.Second
	resumeTries = 3
)

// liveStall is how long a live station may send nothing before it is fetched again: on unit 2 the
// proxy went quiet for about 20 seconds before it reset the connection, which with the 30 seconds a
// track is given (stall) was all heard as silence.
const liveStall = 8 * time.Second

// resumeWaits is the wait before each try in a row.
var resumeWaits = [resumeTries]time.Duration{time.Second, 5 * time.Second, 15 * time.Second}

// liveCheck says whether a url about to play is a live station. nil until the radio feature sets it.
var liveCheck atomic.Pointer[func(url string) bool]

// SetLive is how the radio feature says whether a url about to play is a live station: one that may be
// fetched again when it drops. It is asked once per track, as the track starts.
func SetLive(f func(url string) bool) { liveCheck.Store(&f) }

// liveURL asks the radio feature about url, as its track starts.
func liveURL(url string) bool {
	f := liveCheck.Load()
	return f != nil && (*f)(url)
}

// resume says whether a track whose fetch just ended after ran (err, if it failed) is fetched again,
// and waits out the pause first. Only a live one (track.live) ever is. tries counts the quick failures
// in a row and is updated. ctx is the track's: stopped or replaced, it is never resumed.
func resume(ctx context.Context, live bool, err error, ran time.Duration, tries *int) bool {
	if !live || ctx.Err() != nil || IsUnplayable(err) {
		return false
	}
	if ran >= resumeFresh {
		*tries = 0
	}
	if *tries >= resumeTries {
		slog.Warn("media: the live stream would not come back; giving up", "tries", *tries, "err", err)
		return false
	}
	wait := resumeWaits[*tries]
	*tries++
	slog.Warn("media: the live stream dropped; fetching it again", "after", wait, "try", *tries, "ran", ran.Round(time.Second), "err", err)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(wait):
		return true
	}
}
