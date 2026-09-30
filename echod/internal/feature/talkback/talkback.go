// Package talkback sends the device's microphones to a camera's own speaker: Talk on the camera page,
// for answering the door from the kitchen. It goes straight to the camera, over the backchannel of its
// RTSP stream (ONVIF Profile T, lib/onvifback), so it needs no Home Assistant, go2rtc or Frigate in
// between: a camera on a Reolink recorder set up here is found by itself, any other is given its
// RTSP address on the setup page (config.TalkBack).
//
// The microphones go after echo cancellation, halved to 8 kHz and in G.711, which is what cameras
// with a speaker take. The camera's own sound can keep playing on the device meanwhile: the echo
// canceller takes it out of what is sent back.
//
// Talk is a toggle, not press-to-talk: a conversation at the door is not something to hold a finger
// on the glass through. A talk ends on a second tap, the view closing or turning to another camera,
// the Talk through cameras switch going off, the microphones being muted, the camera hanging up, or
// two minutes passing. While it lasts the view does not time out, and the wake word is not listened
// for, as in a call.
package talkback

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/mute"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/lib/halfrate"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
	"github.com/HuskerMinion/techo5/echod/internal/lib/onvifback"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

const (
	// maxTalk is the longest a talk runs: long enough for any conversation at a door, and a limit on
	// a microphone left sending to the yard by mistake.
	maxTalk = 2 * time.Minute

	// openWait bounds the setting up. A camera behind a hub can take eight seconds to offer its
	// backchannel (lib/onvifback asks up to three times).
	openWait = 20 * time.Second

	// holdView is how far ahead the view is kept up while a talk runs, renewed every check.
	holdView = 15 * time.Second

	// failShown is how long a talk that could not start or broke off says why on the camera page.
	failShown = 6 * time.Second
)

// check is how often a running talk looks at what ends it besides the camera: the view, the switch,
// the mute and the time. A variable so that a test need not wait a second to see it.
var check = time.Second

// Phase is where a talk is.
type Phase int

const (
	Idle    Phase = iota
	Opening       // asking the camera for its backchannel
	Talking
)

// State is what the camera page shows for its Talk control.
type State struct {
	Entity string // the camera being talked to, or the one whose talk failed
	Phase  Phase
	Left   time.Duration // of the two minutes, while talking
	Error  string        // why the last talk to Entity failed, for a few seconds after
}

type Feature struct {
	mu     sync.Mutex
	state  State
	until  time.Time // the two minutes' end
	failAt time.Time
	cancel context.CancelFunc
	done   chan struct{} // closed when the running talk's goroutine is gone

	// Changed fires when a talk starts, ends or fails; listeners must not block.
	Changed hook.Hook[struct{}]
}

var shared = &Feature{}

func Get() *Feature { return shared }

// Address is where a camera is talked to and the login for it: the one given on the setup page, or
// the Reolink recorder's. False for a camera that has neither, which gets no Talk.
func Address(entity string) (addr, user, pass string, ok bool) {
	c := config.Get().TalkBack
	if a := c.Cameras[entity]; a != "" {
		return a, c.User, c.Pass, true
	}
	return home.ReolinkRTSP(entity)
}

// Offered is whether the camera page shows Talk for entity: the switch is on and the camera has an
// address to talk to.
func Offered(entity string) bool {
	if !Here || !config.Get().Security.TalkBack || entity == "" || entity == home.LocalCamera {
		return false
	}
	_, _, _, ok := Address(entity)
	return ok
}

// State is read by the screen each frame.
func (f *Feature) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state
	if st.Phase == Talking {
		st.Left = max(time.Until(f.until), 0)
	}
	if st.Error != "" && time.Since(f.failAt) > failShown {
		st.Error = ""
	}
	return st
}

// Busy is whether a talk has the microphones, for the wake word to leave them alone.
func (f *Feature) Busy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state.Phase != Idle
}

// Toggle is the Talk control: it starts a talk to entity, or ends the one running to it.
func (f *Feature) Toggle(entity string) {
	f.mu.Lock()
	running := f.state.Phase != Idle && f.state.Entity == entity
	f.mu.Unlock()
	if running {
		f.Stop()
		return
	}
	f.Start(entity)
}

// Start talks to entity, ending any talk already running. It returns once the old talk is gone; the
// camera is asked for its backchannel in the background.
func (f *Feature) Start(entity string) {
	if !Offered(entity) {
		return
	}
	f.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.mu.Lock()
	f.state = State{Entity: entity, Phase: Opening}
	f.cancel, f.done = cancel, done
	f.mu.Unlock()
	f.Changed.Emit(struct{}{})
	safe.Go("talk back", func() {
		defer close(done)
		err := f.talk(ctx, entity)
		f.mu.Lock()
		f.state.Phase, f.state.Left = Idle, 0
		if err != nil {
			f.state.Error, f.failAt = err.Error(), time.Now()
		}
		f.mu.Unlock()
		f.Changed.Emit(struct{}{})
	})
}

// Stop ends the talk running, if any, and waits for the camera to have been let go.
func (f *Feature) Stop() {
	f.mu.Lock()
	cancel, done := f.cancel, f.done
	f.cancel, f.done = nil, nil
	f.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// errEnded is a talk that something other than the camera ended; it is not a failure to show.
var errEnded = errors.New("ended")

func (f *Feature) talk(ctx context.Context, entity string) (err error) {
	addr, user, pass, ok := Address(entity)
	if !ok {
		return errors.New("no address for this camera")
	}
	octx, cancel := context.WithTimeout(ctx, openWait)
	s, err := open(octx, addr, user, pass)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		slog.Warn("talk back: the camera would not take it", "entity", entity, "addr", redacted(addr), "err", err)
		return err
	}
	defer s.Close()

	f.mu.Lock()
	f.state.Phase = Talking
	f.until = time.Now().Add(maxTalk)
	f.mu.Unlock()
	f.Changed.Emit(struct{}{})
	slog.Info("talk back: talking", "entity", entity, "addr", redacted(addr), "codec", s.Codec())
	start := time.Now()
	defer func() {
		why := "ended"
		if err != nil && err != errEnded {
			why = err.Error()
		}
		slog.Info("talk back: done", "entity", entity, "after", time.Since(start).Round(time.Second), "why", why)
		if err == errEnded {
			err = nil
		}
	}()

	frames, stop := mic.Get().Listen("talk back")
	defer stop()
	d := halfrate.NewDown()
	t := time.NewTicker(check)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return errEnded
		case <-s.Done():
			return errors.New("the camera hung up")
		case fr, ok := <-frames:
			if !ok {
				return errors.New("the microphones stopped")
			}
			if err := s.Write(d.Run(fr)); err != nil {
				return err
			}
		case <-t.C:
			if why := f.over(entity); why != "" {
				slog.Info("talk back: ending", "why", why)
				return errEnded
			}
			home.Get().HoldCamera(entity, holdView)
		}
	}
}

// open is onvifback.Open, and a variable so that a test can put a camera of its own behind it.
var open = func(ctx context.Context, addr, user, pass string) (session, error) {
	return onvifback.Open(ctx, addr, user, pass)
}

// session is what a talk needs of lib/onvifback's.
type session interface {
	Write([]int16) error
	Codec() string
	Done() <-chan struct{}
	Close() error
}

// over is why a running talk to entity has to end, besides the camera or a tap: empty while it may
// go on.
func (f *Feature) over(entity string) string {
	if !config.Get().Security.TalkBack {
		return "switched off"
	}
	if v, up := home.Get().Camera(); !up || v.Entity != entity {
		return "the camera page closed"
	}
	if m, err := mute.Get().Muted(); err == nil && m {
		return "the microphones were muted"
	}
	f.mu.Lock()
	late := time.Now().After(f.until)
	f.mu.Unlock()
	if late {
		return "two minutes passed"
	}
	return ""
}

// redacted is an address fit for the log: without a login it may carry.
func redacted(addr string) string {
	u, err := url.Parse(addr)
	if err != nil {
		return "(an address that does not parse)"
	}
	u.User = nil
	return u.String()
}
