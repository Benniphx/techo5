//go:build !dot && !spot

package display

import (
	"image"
	"log/slog"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/deck"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// TECHO5 Deck's page (render_deck.go): opened by a swipe up from the bottom edge of the clock, once a
// deck is set up, or by Tap on the clock set to Deck. It stays until a swipe down puts it away.

// bottomEdge is the strip along the bottom of the clock a swipe up has to start in to open the deck,
// in the layout's sizes; above it a swipe up is the volume, as it always was. Thinner than the top
// edge's band, so the volume keeps most of the screen.
const bottomEdge = 90

// deckFlash is how long a press shows on its button: dimmed while it goes out, red when it failed.
const deckFlash = 600 * time.Millisecond

// deckPress is a press being shown on its button.
type deckPress struct {
	page, button int
	until        time.Time
	failed       bool
}

// fromBottomEdge is whether a swipe up starting at y starts in the deck's strip.
func (d *Display) fromBottomEdge(y int) bool {
	return d.r != nil && y >= d.r.h-d.r.s(bottomEdge)
}

// openDeck puts the deck up, if there is one.
func (d *Display) openDeck() bool {
	if !deck.Get().Set() {
		return false
	}
	d.mu.Lock()
	d.deckUp, d.deckPress = true, deckPress{}
	d.closeAlert()
	d.calUntil, d.weatherUntil = time.Time{}, time.Time{}
	d.sheet, d.dash, d.drawer = false, false, false
	pages := len(config.Get().Deck.Pages)
	if d.deckPage >= pages {
		d.deckPage = 0
	}
	d.mu.Unlock()
	slog.Info("deck up")
	d.wake()
	return true
}

func (d *Display) closeDeck() {
	d.mu.Lock()
	d.deckUp = false
	d.mu.Unlock()
	d.wake()
}

// deckShowing is whether the deck page is on the screen now.
func (d *Display) deckShowing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deckUp && d.view.Phase == "idle"
}

// deckScene fills in what the deck page shows, when it is up.
func (d *Display) deckScene(s *scene, now time.Time) {
	d.mu.Lock()
	up := d.deckUp && s.phase == "idle"
	page, press := d.deckPage, d.deckPress
	d.mu.Unlock()
	if !up {
		return
	}
	cfg := config.Get().Deck
	if !cfg.Set() {
		// Every button taken off it from the setup page while it was up.
		d.closeDeck()
		return
	}
	cols, rows := cfg.Grid()
	st := deck.Get().OBS()
	pcs := deck.Get().Computers()
	v := deckView{cols: cols, rows: rows, page: page, pages: max(len(cfg.Pages), 1),
		connected: st.Connected, problem: st.Problem, obsSet: cfg.OBS.Addr != ""}
	for i := range cols * rows {
		b := cfg.Button(page, i)
		lit, known := deck.Lit(b, st, pcs)
		bv := deckButtonView{empty: b.Action == config.DeckNone, label: deck.Label(b), icon: deck.Icon(b, lit),
			color: b.Color, lit: lit, known: known}
		if press.page == page && press.button == i && now.Before(press.until) {
			bv.pressed, bv.failed = !press.failed, press.failed
		}
		v.buttons = append(v.buttons, bv)
	}
	s.showDeck, s.deck = true, v
}

// deckGesture is a finger on the deck page: a tap presses a button, a swipe left or right turns the
// page, a swipe down puts the deck away. Swipes up do nothing: the swipe that opened it keeps
// reporting notches until the finger lifts.
func (d *Display) deckGesture(g touch.Gesture) {
	switch g.Kind {
	case touch.Tap:
		if d.r == nil {
			return
		}
		i, ok := d.r.deckHit(image.Pt(g.X, g.Y))
		if !ok {
			return
		}
		d.mu.Lock()
		page := d.deckPage
		d.deckPress = deckPress{page: page, button: i, until: time.Now().Add(deckFlash)}
		d.mu.Unlock()
		go func() {
			err := deck.Get().Press(page, i)
			if err != nil {
				d.mu.Lock()
				if d.deckPress.page == page && d.deckPress.button == i {
					d.deckPress.failed, d.deckPress.until = true, time.Now().Add(deckFlash)
				}
				d.mu.Unlock()
			}
			d.wake()
			// Once more after the flash, so the button is drawn plain again.
			time.AfterFunc(deckFlash+50*time.Millisecond, d.wake)
		}()
	case touch.SwipeLeft, touch.SwipeRight:
		pages := max(len(config.Get().Deck.Pages), 1)
		d.mu.Lock()
		if g.Kind == touch.SwipeLeft {
			d.deckPage = min(d.deckPage+1, pages-1)
		} else {
			d.deckPage = max(d.deckPage-1, 0)
		}
		d.mu.Unlock()
	case touch.SwipeDown:
		d.closeDeck()
	}
}

// deckChanged redraws the page when OBS's state changes under it.
func (d *Display) deckChanged(struct{}) {
	if d.deckShowing() {
		d.wake()
	}
}
