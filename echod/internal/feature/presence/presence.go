//go:build !dot

// Package presence watches the room through the device's own camera for somebody near: for Home
// Assistant (a Presence sensor, as an occupancy sensor would be) and for the screen, which can go dark
// once nobody has been near for a while and light again as somebody comes up to it.
//
// Off until it is turned on. Nothing the camera sees is kept or sent: a couple of times a second the
// newest frame is read as a small grid of brightness (32 by 24) and compared with the last, and what
// leaves the package is only "somebody is near" or not. The mute button and the lens shutter stop it,
// as they stop the camera for everything else; while the camera cannot see, the screen is left as it
// is rather than put out for an empty room it cannot vouch for.
package presence

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

func init() {
	if camera.Available() {
		component.Register(component.Device, Get(), component.Order(83))
	}
}

const (
	gridW, gridH = 32, 24

	// hold is how long after the last movement somebody still counts as near: people stand still.
	hold = 90 * time.Second

	// startFrames are passed over after the camera starts, while its exposure finds the room.
	startFrames = 6

	// retry is how soon a camera that could not be had (muted, the shutter closed) is asked again.
	retry = 10 * time.Second
)

type Feature struct {
	sw      *esphome.Switch
	offNum  *esphome.Number
	sensNum *esphome.Number
	sensor  *esphome.BinarySensor

	// Changed fires when somebody comes near or the room has been empty long enough to say so, and
	// when the switch changes. Listeners must not block.
	Changed hook.Hook[struct{}]

	wake chan struct{}

	mu        sync.Mutex
	watching  bool // the camera is being read now
	present   bool
	lastSeen  time.Time
	watchFrom time.Time // when the current watch began: an empty room is only an empty room after hold
	det       detector
	skip      int       // frames still to pass over: the camera's first ones, after a start
	hushUntil time.Time // the device's own light changing (Hush): frames until then are not compared
}

var (
	once   sync.Once
	shared *Feature
)

func Get() *Feature {
	once.Do(func() {
		f := &Feature{wake: make(chan struct{}, 1)}
		f.sw = &esphome.Switch{
			Base:      esphome.Base{ObjectID: "presence_detection", Name: "Presence detection", Icon: "mdi:motion-sensor", Category: esphome.CategoryConfig},
			OnCommand: f.SetOn,
		}
		f.offNum = &esphome.Number{
			Base: esphome.Base{ObjectID: "screen_off_when_nobody_is_near", Name: "Screen off when nobody is near",
				Icon: "mdi:monitor-off", Category: esphome.CategoryConfig},
			Min: 0, Max: 60, Step: 1, Unit: "min",
			Mode: esphome.NumberBox,
		}
		f.offNum.OnCommand = func(v float32) { f.SetScreenOff(int(v)) }
		f.sensNum = &esphome.Number{
			Base: esphome.Base{ObjectID: "presence_sensitivity", Name: "Presence sensitivity",
				Icon: "mdi:tune-vertical", Category: esphome.CategoryConfig},
			Min: 1, Max: 100, Step: 1,
			Mode: esphome.NumberSlider,
		}
		f.sensNum.OnCommand = func(v float32) { f.SetSensitivity(int(v)) }
		f.sensor = &esphome.BinarySensor{
			Base:        esphome.Base{ObjectID: "presence", Name: "Presence", Icon: "mdi:account-eye"},
			DeviceClass: "occupancy",
		}
		shared = f
	})
	return shared
}

func (f *Feature) Name() string { return "presence" }

func (f *Feature) Entities() []esphome.Entity {
	return []esphome.Entity{f.sw, f.offNum, f.sensNum, f.sensor}
}

func (f *Feature) Restore(c config.Config) {
	f.sw.Set(c.Presence.On)
	f.offNum.Set(float32(c.Presence.PresenceScreenOff()))
	f.sensNum.Set(float32(c.Presence.PresenceSensitivity()))
	f.sensor.Set(false)
}

// On is whether the camera watches for somebody near.
func On() bool { return config.Get().Presence.On && camera.Available() }

// SetOn is the switch, from Home Assistant or the screen.
func (f *Feature) SetOn(on bool) {
	if err := config.Set().Presence().On(on); err != nil {
		slog.Error("saving a setting failed", "setting", "presence", "err", err)
		f.sw.Set(!on)
		return
	}
	f.sw.Set(on)
	slog.Info("setting changed", "setting", "presence", "using", on)
	f.poke()
	f.Changed.Emit(struct{}{})
}

// SetScreenOff is the screen's wait, in minutes; 0 leaves the screen on.
func (f *Feature) SetScreenOff(minutes int) {
	minutes = min(max(minutes, 0), 60)
	if err := config.Set().Presence().ScreenOff(minutes); err != nil {
		slog.Error("saving a setting failed", "setting", "presence screen off", "err", err)
		return
	}
	f.offNum.Set(float32(minutes))
	f.Changed.Emit(struct{}{})
}

// SetSensitivity is 1 to 100: higher sees smaller movements, lower needs more of the picture to move.
func (f *Feature) SetSensitivity(v int) {
	v = min(max(v, 1), 100)
	if err := config.Set().Presence().Sensitivity(v); err != nil {
		slog.Error("saving a setting failed", "setting", "presence sensitivity", "err", err)
		return
	}
	f.sensNum.Set(float32(v))
	f.mu.Lock()
	f.det.patchMin = patchFor(v)
	f.mu.Unlock()
}

// hushFor is how long the device's own change of light is given to pass.
const hushFor = 2 * time.Second

// Hush says the device itself is about to change the light in the room (the screen coming on, going
// out, changing its brightness), which the camera would otherwise take for somebody: frames are not
// compared for a moment, and the comparison starts over after it.
func Hush() {
	f := Get()
	f.mu.Lock()
	f.hushUntil = time.Now().Add(hushFor)
	f.mu.Unlock()
}

// ScreenOffAfter is how long the room must be empty before the screen goes dark: 0 when it never does
// (the setting is 0, presence is off, or the camera is not watching now).
func ScreenOffAfter() time.Duration {
	if !On() {
		return 0
	}
	f := Get()
	f.mu.Lock()
	watching := f.watching
	f.mu.Unlock()
	if !watching {
		return 0
	}
	return time.Duration(config.Get().Presence.PresenceScreenOff()) * time.Minute
}

// Present reports whether somebody is near now.
func Present() bool {
	f := Get()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watching && f.present
}

// EmptyFor is how long nobody has been near: 0 while somebody is, or while the camera is not
// watching, since then nothing is known.
func EmptyFor(now time.Time) time.Duration {
	f := Get()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.watching || f.present {
		return 0
	}
	since := f.lastSeen
	if since.Before(f.watchFrom) {
		since = f.watchFrom
	}
	return max(now.Sub(since), 0)
}

func (f *Feature) poke() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Run holds the camera while the switch is on, and lets it go when it goes off.
func (f *Feature) Run(ctx context.Context) error {
	for {
		if On() {
			f.watch(ctx)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-f.wake:
		}
	}
}

// watch reads the camera until the switch goes off or the camera is taken away (the mute, the
// shutter), and asks again after a while in the second case.
func (f *Feature) watch(ctx context.Context) {
	for On() && ctx.Err() == nil {
		release, err := camera.Get().AcquireSlow()
		if err != nil {
			if errors.Is(err, camera.ErrNeedsReboot) {
				slog.Warn("presence: the camera needs a reboot; not watching until then")
			} else {
				slog.Debug("presence: the camera is not available", "err", err)
			}
			if !f.pause(ctx, retry) {
				return
			}
			continue
		}
		f.watchFor(ctx, release)
	}
}

// watchFor takes frames until the switch goes off, the camera stops sending (held off by the mute),
// or ctx ends; then lets the camera go.
func (f *Feature) watchFor(ctx context.Context, release func()) {
	frames := make(chan *camera.Frame, 1)
	stop := camera.Get().Frames.Listen(func(fr *camera.Frame) {
		select {
		case frames <- fr:
		default: // still busy with the last one: this one is not needed
		}
	})
	defer stop()
	defer release()

	now := time.Now()
	f.mu.Lock()
	f.watching, f.watchFrom = true, now
	f.det = detector{patchMin: patchFor(config.Get().Presence.PresenceSensitivity())}
	f.skip = startFrames
	f.mu.Unlock()
	slog.Info("presence: watching")
	defer func() {
		f.mu.Lock()
		f.watching, f.present = false, false
		f.mu.Unlock()
		f.sensor.Set(false)
		f.Changed.Emit(struct{}{})
		slog.Info("presence: not watching")
	}()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	quiet := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.wake:
			if !On() {
				return
			}
		case fr := <-frames:
			g := fr.Luma(gridW, gridH)
			if g == nil {
				// A frame with nothing in it: the camera is held off by the mute. Let it go, so the
				// sensor is not kept powered for nothing, and ask again later.
				return
			}
			quiet = time.Now()
			f.take(g, fr.At)
		case now := <-tick.C:
			if now.Sub(quiet) > 15*time.Second {
				slog.Info("presence: the camera stopped sending frames")
				return
			}
			f.expire(now)
		}
	}
}

// take runs one frame through the detector.
func (f *Feature) take(g []uint8, at time.Time) {
	f.mu.Lock()
	if f.skip > 0 || at.Before(f.hushUntil) {
		// The camera's first frames, with the exposure still finding the room, or the device's own
		// light changing: not compared, and the next comparison starts from the next frame.
		if f.skip > 0 {
			f.skip--
		}
		f.det.prev = nil
		f.mu.Unlock()
		return
	}
	moved := f.det.step(g)
	was := f.present
	if moved {
		f.lastSeen, f.present = at, true
	}
	f.mu.Unlock()
	if moved && !was {
		slog.Info("presence: somebody near")
		f.sensor.Set(true)
		f.Changed.Emit(struct{}{})
	}
}

// expire says the room is empty once nothing has moved for hold.
func (f *Feature) expire(now time.Time) {
	f.mu.Lock()
	gone := f.present && now.Sub(f.lastSeen) > hold
	if gone {
		f.present = false
	}
	f.mu.Unlock()
	if gone {
		slog.Info("presence: nobody near")
		f.sensor.Set(false)
		f.Changed.Emit(struct{}{})
	}
}

func (f *Feature) pause(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-f.wake:
		return true
	case <-time.After(d):
		return true
	}
}
