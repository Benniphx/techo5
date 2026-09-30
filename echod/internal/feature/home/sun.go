package home

import (
	"log/slog"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
	"github.com/HuskerMinion/techo5/echod/internal/lib/sun"
)

// sunKept is today's sunrise and sunset at home, worked out once a day. Finding home can ask Home
// Assistant, which a frame being drawn must never wait on, so it is worked out off the caller's
// goroutine and the last day's times stand in meanwhile: they are a minute or two out, not wrong.
var sunKept struct {
	mu        sync.Mutex
	day       string
	rise, set time.Time
	ok        bool
	asking    bool
	retry     time.Time // after a failure, when to try again
}

// SunTimes is today's sunrise and sunset at home, for the Sun clock style; ok is false until home's
// place is known, and on a day the sun does not rise or set there.
func (f *Feature) SunTimes(now time.Time) (rise, set time.Time, ok bool) {
	k := &sunKept
	day := now.Format("2006-01-02")
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.day != day && !k.asking && now.After(k.retry) {
		k.asking = true
		safe.Go("sun times", func() { sunWorkOut(day, now) })
	}
	return k.rise, k.set, k.ok
}

func sunWorkOut(day string, now time.Time) {
	k := &sunKept
	lat, lon, err := homeLocation()
	k.mu.Lock()
	defer k.mu.Unlock()
	k.asking = false
	if err != nil {
		slog.Debug("sun times wait for home's place", "err", err)
		k.retry = time.Now().Add(10 * time.Minute)
		return
	}
	k.day = day
	k.rise, k.set, k.ok = sun.Times(lat, lon, now)
}
