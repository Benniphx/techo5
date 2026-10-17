package camstream

import (
	"log/slog"
	"sync"
	"time"
)

// A wrong login, over RTSP or ONVIF, is answered only after wrongDelay, and an address that gets lockAfter
// of them wrong within lockWindow is refused for lockFor: the password is all that stands between the
// network and the room, and this keeps guessing at it slow. Logins being checked count as wrong ones
// until they are known not to be, so asking many at once is no faster than asking one after another.
var wrongDelay = time.Second

const (
	lockAfter  = 10
	lockWindow = 10 * time.Minute
	lockFor    = 15 * time.Minute

	// guardKept is how many addresses are remembered before the ones with nothing against them now are let
	// go.
	guardKept = 256
)

// guard counts wrong logins by address.
type guard struct {
	mu sync.Mutex
	by map[string]*record
}

type record struct {
	fails    []time.Time
	until    time.Time
	checking int // logins being checked now
}

// refused is whether an address is locked out now.
func (g *guard) refused(from string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.by[from]
	return r != nil && time.Now().Before(r.until)
}

// attempt is a login from an address about to be checked: false when the address is locked out, or has
// as many wrong and unsettled logins as it may. Otherwise done is called with whether it was wrong, and
// waits wrongDelay when it was.
func (g *guard) attempt(from string) (ok bool, done func(wrong bool)) {
	g.mu.Lock()
	now := time.Now()
	r := g.record(from, now)
	if now.Before(r.until) || countRecent(r.fails, now)+r.checking >= lockAfter {
		g.mu.Unlock()
		return false, nil
	}
	r.checking++
	g.mu.Unlock()
	return true, func(wrong bool) {
		if wrong {
			g.failed(from)
			time.Sleep(wrongDelay)
		}
		g.mu.Lock()
		r.checking--
		g.mu.Unlock()
	}
}

// record is an address's, made when it has none; g.mu is held.
func (g *guard) record(from string, now time.Time) *record {
	if g.by == nil {
		g.by = map[string]*record{}
	}
	r := g.by[from]
	if r != nil {
		return r
	}
	if len(g.by) >= guardKept {
		for k, v := range g.by {
			if v.checking == 0 && !now.Before(v.until) && countRecent(v.fails, now) == 0 {
				delete(g.by, k)
			}
		}
	}
	if len(g.by) >= guardKept {
		// Still full: what is forgotten now is wrong logins that have not yet locked anybody out.
		for k, v := range g.by {
			if v.checking == 0 && !now.Before(v.until) {
				delete(g.by, k)
			}
		}
	}
	r = &record{}
	g.by[from] = r
	return r
}

// failed counts a wrong login, locking the address out at lockAfter.
func (g *guard) failed(from string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	r := g.record(from, now)
	r.fails = append(recent(r.fails, now), now)
	if len(r.fails) >= lockAfter && !now.Before(r.until) {
		r.until = now.Add(lockFor)
		r.fails = nil
		slog.Warn("camera streaming: an address locked out after wrong logins", "from", from, "for", lockFor)
	}
}

// countRecent is how many failures are within lockWindow of now, leaving the list as it is.
func countRecent(ts []time.Time, now time.Time) int {
	n := 0
	for _, t := range ts {
		if now.Sub(t) < lockWindow {
			n++
		}
	}
	return n
}

// recent is the failures within lockWindow of now, kept in the list's own storage: assign it back.
func recent(ts []time.Time, now time.Time) []time.Time {
	kept := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < lockWindow {
			kept = append(kept, t)
		}
	}
	return kept
}
