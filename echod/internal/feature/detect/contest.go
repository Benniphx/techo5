package detect

import (
	"log/slog"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/nearest"
)

// contestWindow is how far apart a wake word and a command word can cross and still be one thing said.
// The engine reports a crossing the moment a model's score passes its cutoff, and two models hearing the
// same words cross at their own moments in it: a frame or two apart for words that sound alike, and
// within Hold of each other when one keeps rising after the other has fired. It is also how long a wake
// word waits for a command word while music plays, so it is kept short.
const contestWindow = Hold + 100*time.Millisecond

// contest settles a wake word and a command word that both crossed on one thing said (#265). "Next song"
// can cross a wake word over its cutoff, and the wake word opening a turn took the music's quiet and
// left the command ignored. The one that crossed by more over its own cutoff wins. Not the higher score:
// every word has a cutoff of its own, and a wake word heard at 0.78 against a playing cutoff of 0.77 is
// a poor catch next to a command word at 0.78 against 0.60 (Engine.Margin).
//
// A wake word is held for contestWindow when command words are listening, so that a command word that
// crosses just after it can still win before anything is heard or shown. A command word that crossed first
// has already acted, so only the wake word is judged against it then. With no command word loaded, and
// with nothing crossing near, nothing changes.
type contest struct {
	mu sync.Mutex

	// wake is a wake word waiting out the window, and cmdAt and cmdMargin the latest command word that
	// went ahead and acted.
	wake      *heldWake
	cmdAt     time.Time
	cmdMargin float64

	// now and after are the clock, nil for the real one; tests stand in.
	now   func() time.Time
	after func(time.Duration, func())
}

type heldWake struct{ margin float64 }

func (c *contest) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *contest) later(d time.Duration, f func()) {
	if c.after != nil {
		c.after(d, f)
		return
	}
	time.AfterFunc(d, f)
}

// wakeHeard is a wake word with margin over its cutoff. open starts the turn (held, if it waited): at once unless command words
// are listening (hold), and then after the window, unless a command word beat it by then.
func (c *contest) wakeHeard(margin float64, hold bool, open func(held bool)) {
	c.mu.Lock()
	if !c.cmdAt.IsZero() && c.clock().Sub(c.cmdAt) < contestWindow && c.cmdMargin > margin {
		c.mu.Unlock()
		slog.Info("wake word ignored: the command word was heard more surely", "margin", margin, "theirs", c.cmdMargin)
		return
	}
	if !hold {
		c.mu.Unlock()
		open(false)
		return
	}
	w := &heldWake{margin: margin}
	c.wake = w
	c.mu.Unlock()
	c.later(contestWindow, func() {
		c.mu.Lock()
		mine := c.wake == w
		if mine {
			c.wake = nil
		}
		c.mu.Unlock()
		if mine {
			open(true)
		}
	})
}

// commandCrossed is a command word with margin over its cutoff, and whether it goes on to act. It does
// not if a wake word waiting out the window crossed by as much or more; otherwise it ends that wake word's
// wait, so no turn opens.
func (c *contest) commandCrossed(margin float64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w := c.wake; w != nil {
		if w.margin >= margin {
			return false
		}
		c.wake = nil
		slog.Info("command word heard more surely than the wake word, no turn opened", "margin", margin, "theirs", w.margin)
	}
	c.cmdAt, c.cmdMargin = c.clock(), margin
	return true
}

// wakeHeard is a wake word the engine heard in slot (routeTurn).
func (d *Detect) wakeHeard(slot int) {
	at := time.Now()
	d.contest.wakeHeard(d.engine.Margin(slot), d.commandsListening() > 0, func(held bool) {
		if held {
			nearest.Get().WokeAt(slot, at)
			return
		}
		nearest.Get().Woke(slot)
	})
}
