package alarm

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/led"
)

// Waking to light. For the minutes before an alarm, the device lights the room: on a screen the
// whole panel becomes a sunrise (feature/display), and on a device with a light ring and no screen
// the ring comes up instead — the same curve, the same colors, on the only lamp a Dot has.
//
// The alarm itself is untouched. This is the light before it.

// SunriseFloor is where the ramp starts, as a fraction of full: enough to see by in a dark room and
// not enough to wake anybody by itself.
const SunriseFloor = 0.02

// sunriseEvery is how often the ring is repainted while the light comes up. Slow enough to be
// nothing, often enough that the change is not a series of steps.
const sunriseEvery = 2 * time.Second

// SunriseProgress is how far into the light we are: 0 outside it, rising to 1 as an alarm's time
// arrives. Each alarm has its own window (config.Alarms.SunriseFor), so one with none does not light
// the room for another's sake, and where two windows meet the further along wins. A reminder never
// lights the room.
//
// Once an alarm with a wake light has rung, the light stays at full (#161) while:
//   - it rings;
//   - it is snoozed, until the snooze rings and is stopped;
//   - it was stopped while the next alarm's window was already open, until that alarm rings.
//
// The next alarm there is the soonest one with a wake light of its own whose window is already open
// at the moment the ring ends, so two alarms whose ramps overlap or touch make one light. It used to
// fall back to wherever the second alarm's ramp was: with alarms at 8:00 and 8:30 and half an hour of
// light each, from full at 8:00 to the first glow of the 8:30 one. A snooze bridges the same way: the
// light holds through it, and the rule is asked again when its ring is stopped. A stop with no such
// alarm ahead ends the light. Stopped or rung out are the same here; only a snooze is not an end.
func (a *Alarms) SunriseProgress(now time.Time) float64 {
	return a.lightNow(a.sources(now), now)
}

// lightNow is SunriseProgress over these sources.
func (a *Alarms) lightNow(sources []source, now time.Time) float64 {
	a.mu.Lock()
	hold := wakeHold{ringing: a.ringing != nil && a.ringing.lit, bridge: a.bridge}
	a.mu.Unlock()
	return wakeLight(sources, hold, now)
}

// wakeHold is what holds the light at full outside the ramps, besides a lit snooze, which is a source.
type wakeHold struct {
	ringing bool // an alarm with a wake light is sounding
	bridge  bridge
}

// bridge is the alarm the light is held for after a ring ended inside its window.
type bridge struct {
	key string
	at  time.Time
}

// bridgeGrace is how long past its time a bridge still holds: the alarm it waits for rings a moment
// after its time, not at it, and the light must not drop for that moment.
const bridgeGrace = time.Minute

// holds is whether the alarm b waits for is still coming: still armed, still at that time. One turned
// off, deleted or moved lets the light go.
func (b bridge) holds(sources []source, now time.Time) bool {
	if b.key == "" || now.After(b.at.Add(bridgeGrace)) {
		return false
	}
	for _, s := range sources {
		if s.key != b.key || s.sunrise <= 0 {
			continue
		}
		if at, ok := s.next(b.at); ok && at.Equal(b.at) {
			return true
		}
	}
	return false
}

// wakeLight is SunriseProgress for these sources and that hold.
func wakeLight(sources []source, hold wakeHold, now time.Time) float64 {
	if hold.ringing || hold.bridge.holds(sources, now) {
		return 1
	}
	// A lit snooze holds until it is gone from the list, which is when it rings (or is canceled or
	// missed), not when its time comes: the moment between the two must not drop the light.
	for _, s := range sources {
		if s.lit {
			return 1
		}
	}
	return sunriseAt(sources, now)
}

// bridgeFrom is the alarm the light is held for when a lit ring ends at now: the soonest one with a
// wake light whose window is already open. None is no bridge, and the light ends.
func bridgeFrom(sources []source, now time.Time) bridge {
	var b bridge
	for _, s := range sources {
		if s.sunrise <= 0 {
			continue
		}
		at, ok := s.next(now)
		if !ok || at.Sub(now) > time.Duration(s.sunrise)*time.Minute {
			continue
		}
		if b.key == "" || at.Before(b.at) {
			b = bridge{key: s.key, at: at}
		}
	}
	return b
}

func sunriseAt(sources []source, now time.Time) float64 {
	best := 0.0
	for _, s := range sources {
		if s.sunrise <= 0 {
			continue
		}
		at, ok := s.next(now)
		if !ok {
			continue
		}
		window := time.Duration(s.sunrise) * time.Minute
		left := at.Sub(now)
		if left <= 0 || left > window {
			continue
		}
		best = max(best, 1-float64(left)/float64(window))
	}
	return best
}

// endLight is a lit ring ending for good at now, stopped rather than snoozed: the light is held for
// the next alarm whose window is already open, or let go.
func (a *Alarms) endLight(sources []source, now time.Time) {
	b := bridgeFrom(sources, now)
	a.mu.Lock()
	a.bridge = b
	a.mu.Unlock()
	if b.key != "" {
		slog.Info("wake light held for the next alarm", "key", b.key, "at", b.at.Format("15:04"))
	}
}

// dropBridge lets go of the light held for the next alarm, for a stop pressed with nothing ringing:
// somebody who presses Stop wants the light off. That alarm's own ramp still comes up as it would
// have. It reports whether there was a bridge.
func (a *Alarms) dropBridge() bool {
	a.mu.Lock()
	had := a.bridge.key != ""
	a.bridge = bridge{}
	a.mu.Unlock()
	if had {
		slog.Info("wake light held for the next alarm let go")
		a.poke()
	}
	return had
}

// SunriseLevel is the fraction of full brightness the ramp asks for, eased so most of the change
// happens near the end rather than the moment it starts.
func SunriseLevel(progress float64) float64 {
	if progress <= 0 {
		return 0
	}
	return SunriseFloor + (1-SunriseFloor)*math.Pow(math.Min(progress, 1), 2)
}

// sunriseColor is the ring's color as the light comes up: the deep red of the first minutes, then
// orange, then a warm white, scaled by how far up it is.
func sunriseColor(progress float64) led.Color {
	return sunriseHue(progress).scaled(SunriseLevel(progress))
}

// hue is a color before it is dimmed, 0 to 255 a channel.
type hue struct{ r, g, b float64 }

// sunriseHue is the light's color at progress at full strength: what sunriseColor dims to the level,
// and what Home Assistant is told beside the level, for a lamp that takes the two apart.
func sunriseHue(progress float64) hue {
	g := 40 + 200*math.Min(progress*1.15, 1)
	b := 10 + 180*math.Max(progress-0.45, 0)/0.55
	return hue{r: 255, g: g, b: math.Min(b, 235)}
}

func (h hue) scaled(lvl float64) led.Color {
	scale := func(v float64) uint8 { return uint8(math.Min(math.Max(v*lvl, 0), 255)) }
	return led.Color{R: scale(h.r), G: scale(h.g), B: scale(h.b)}
}

func (h hue) hex() string {
	c := h.scaled(1)
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// sunriseRing paints the ring for the light before an alarm, and clears it when there is none. It
// reports whether the light is on, so the caller knows to come back sooner.
func (a *Alarms) sunriseRing(p float64) bool {
	if p <= 0 {
		if a.lighting {
			a.sun.Clear()
			a.lighting = false
		}
		return false
	}
	a.lighting = true
	a.sun.Paint(led.Solid(sunriseColor(p)))
	return true
}

// lightEvery is how often Home Assistant hears the wake light change while it comes up: often enough
// for a lamp in the room to follow it, not every repaint. Coming on and going off are told at once.
const lightEvery = 5 * time.Second

// lightTold is what Home Assistant was last told of the wake light; the scheduler's alone.
type lightTold struct {
	at      time.Time
	percent int
	color   string
	once    bool
}

// tellLight puts the wake light's brightness and color in Home Assistant, at most every lightEvery
// while it changes. Off is 0 and no color.
func (a *Alarms) tellLight(p float64, now time.Time) {
	percent, color := lightState(p)
	t := a.told
	if t.once && percent == t.percent && color == t.color {
		return
	}
	if t.once && (percent > 0) == (t.percent > 0) && now.Sub(t.at) < lightEvery {
		return
	}
	a.lightLevel.Set(float32(percent))
	a.lightColor.Set(color)
	a.told = lightTold{at: now, percent: percent, color: color, once: true}
}

// lightState is the wake light at p as Home Assistant has it: percent of full and a color as #rrggbb.
func lightState(p float64) (percent int, color string) {
	if p <= 0 {
		return 0, ""
	}
	return int(math.Round(SunriseLevel(p) * 100)), sunriseHue(p).hex()
}
