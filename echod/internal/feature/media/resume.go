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
// Only radio: a song or a podcast fetched again would start over from its beginning. Whether what plays
// is radio is the radio feature's to say (feature/home follows the station), through SetLive.
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

// liveCheck says whether what plays now is live radio. nil until the radio feature sets it.
var liveCheck atomic.Pointer[func() bool]

// SetLive is how the radio feature says whether what plays is a live station: one that may be fetched
// again when it drops.
func SetLive(f func() bool) { liveCheck.Store(&f) }

func live() bool {
	f := liveCheck.Load()
	return f != nil && (*f)()
}

// resume says whether a track whose fetch just ended after ran (err, if it failed) is fetched again,
// and waits out the pause first. tries counts the quick failures in a row and is updated. ctx is the
// track's: stopped or replaced, it is never resumed.
func resume(ctx context.Context, err error, ran time.Duration, tries *int) bool {
	if ctx.Err() != nil || IsUnplayable(err) || !live() {
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
