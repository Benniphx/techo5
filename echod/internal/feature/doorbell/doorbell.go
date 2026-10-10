// Package doorbell makes the device a doorbell's chime: a press of the button rings it and puts the
// door's camera up on the screen, with no Home Assistant in between.
//
// The press comes from the Reolink recorder set up here (Home.Reolink), as the ONVIF event Reolink
// calls Visitor, by pull point (lib/onvifevent). It has to come through a recorder: a Reolink battery
// doorbell on its own lists Visitor among its events and never sends it, and has no web page for the
// webhook its newer firmware is said to have, while the same doorbell on a Home Hub sends a Visitor
// within a second of every press, on its own channel, apart from its motion and person events.
//
// A doorbell rings like any doorbell: every press, whatever the hour. Quiet hours are for the
// device's own business, and somebody at the door at night is not that.
package doorbell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	"github.com/HuskerMinion/techo5/echod/internal/lib/onvifevent"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

const (
	// life is how long a subscription lasts unless renewed, and renewEvery how often it is: well
	// inside it, so one renewal lost to a busy recorder does not lose the subscription.
	life       = 5 * time.Minute
	renewEvery = 2 * time.Minute

	// pullWait is how long one ask waits for something to happen. Short enough that a changed
	// setting is noticed soon, long enough that an idle doorbell costs one request every pullWait.
	pullWait = 20 * time.Second

	// again is the least time between two rings. A press is one Visitor going true; a finger held
	// or pressing twice in a second is still one visitor.
	again = 3 * time.Second

	// retryMin and retryMax bound the wait before trying a recorder that failed again.
	retryMin = 5 * time.Second
	retryMax = time.Minute
)

// chimeLevel is the chime's loudness within its volume step: its strikes peak near full scale, since
// a struck bar is loud only for a moment and then dies away, where a beep at the bell's own 0.6
// (ring/bell.go) holds its level all through.
const chimeLevel = 0.9

// chime plays the chime at the louder of the alarm volume and the volume the device is at now: a
// doorbell is never quieter than an alarm, and turning the device up turns its doorbell up too.
func chime() {
	speaker.Sound().Interject(func(p *speaker.Player) {
		p.Bell(max(ring.Level(), p.Step()), chimeLevel, dingDong()...)
	})
}

// Status is how the doorbell is doing, for the setup page.
type Status struct {
	Camera    string    // the doorbell's camera, "" for none
	Listening bool      // a subscription is open and being asked
	Since     time.Time // when Listening or Problem last changed
	Problem   string    // why it is not listening, when it should be
	LastPress time.Time
}

// Feature is the doorbell.
type Feature struct {
	mu     sync.Mutex
	status Status
	last   time.Time // the last ring
	poke   chan struct{}

	// What a press does and the clock, as variables so a test can be the world.
	ring func()
	show func(entity string)
	now  func() time.Time
}

var shared = &Feature{
	poke: make(chan struct{}, 1),
	ring: chime,
	show: showCamera,
	now:  time.Now,
}

func init() {
	component.Register(component.Network, shared, component.Order(76))
}

func Get() *Feature { return shared }

func (f *Feature) Name() string { return "doorbell" }

// Chime plays the chime once through, as a press would, without showing the camera: for somebody
// choosing a volume, or checking what a press will sound like.
func (f *Feature) Chime() { safe.Go("doorbell chime", f.ring) }

// Status is how the doorbell is doing now.
func (f *Feature) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// Poke has the doorbell look at its settings now, after the setup page changed them.
func (f *Feature) Poke() {
	select {
	case f.poke <- struct{}{}:
	default:
	}
}

// target is what to listen to, as set: the recorder's ONVIF address and login, and the doorbell's
// channel on it. ok is false when there is no doorbell; problem says why a doorbell that is set
// cannot be listened to.
type target struct {
	camera     string
	base       string
	user, pass string
	channel    int
}

func current() (t target, ok bool, problem string) {
	c := config.Get()
	t.camera = c.Doorbell.Camera
	if t.camera == "" {
		return t, false, ""
	}
	r := c.Home.Reolink
	if r.Base == "" {
		return t, false, "no Reolink recorder is set up"
	}
	listed := false
	for _, cam := range r.Cameras {
		listed = listed || cam.Entity == t.camera
	}
	ch, err := strconv.Atoi(strings.TrimPrefix(t.camera, "reolink:"))
	if !listed || err != nil || ch < 0 || !strings.HasPrefix(t.camera, "reolink:") {
		return t, false, "the doorbell's camera is no longer on the recorder"
	}
	u, err := url.Parse(r.Base)
	if err != nil || u.Hostname() == "" {
		return t, false, "the recorder's address is not usable"
	}
	t.base = "http://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(c.Doorbell.ONVIFPort()))
	t.user, t.pass, t.channel = r.User, r.Pass, ch
	return t, true, ""
}

// Run listens for presses for as long as the device is up, starting again whenever the settings
// change or the recorder goes away.
func (f *Feature) Run(ctx context.Context) error {
	wait := retryMin
	for {
		t, ok, problem := current()
		f.setStatus(t.camera, false, problem)
		if !ok {
			if !f.sleep(ctx, 0) {
				return nil
			}
			continue
		}
		err := f.listen(ctx, t)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil { // the settings changed: look again at once
			wait = retryMin
			continue
		}
		f.setStatus(t.camera, false, explain(err, t))
		if !f.sleep(ctx, wait) {
			return nil
		}
		wait = min(wait*2, retryMax)
	}
}

// sleep waits d, or for a poke, or with d of 0 for a poke alone. False when the device is stopping.
func (f *Feature) sleep(ctx context.Context, d time.Duration) bool {
	var after <-chan time.Time
	if d > 0 {
		tm := time.NewTimer(d)
		defer tm.Stop()
		after = tm.C
	}
	select {
	case <-ctx.Done():
		return false
	case <-f.poke:
	case <-after:
	}
	return true
}

// listen holds one subscription open and rings for what comes through it. It returns nil when the
// settings no longer match t, and the error that ended it otherwise.
func (f *Feature) listen(ctx context.Context, t target) error {
	c := &onvifevent.Client{Base: t.base, User: t.user, Pass: t.pass,
		HTTP: &http.Client{Timeout: pullWait + 20*time.Second}}
	sub, err := c.Subscribe(ctx, life)
	if err != nil {
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sub.Close(cctx)
	}()
	f.setStatus(t.camera, true, "")
	slog.Info("doorbell: listening", "camera", t.camera, "channel", t.channel)
	renewed := f.now()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if f.now().Sub(renewed) >= renewEvery {
			if err := sub.Renew(ctx, life); err != nil {
				return fmt.Errorf("renewing: %w", err)
			}
			renewed = f.now()
		}
		evs, poked, err := f.pull(ctx, sub)
		for _, ev := range evs {
			if pressed(ev, t.channel) {
				f.press(t.camera)
			}
		}
		if poked {
			if now, ok, _ := current(); !ok || now != t {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
	}
}

// pull asks for what happened, giving up the ask when poked: a changed setting is acted on now, not
// at the end of a wait that may have most of pullWait to go.
func (f *Feature) pull(ctx context.Context, sub *onvifevent.Subscription) (evs []onvifevent.Event, poked bool, err error) {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		select {
		case <-f.poke:
			poked = true
			cancel()
		case <-done:
		}
	}()
	evs, err = sub.Pull(pctx, pullWait)
	close(done)
	<-gone // poked is settled, and no poke is swallowed after we stopped looking
	return evs, poked, err
}

// pressed is whether ev is the doorbell on channel being pressed: Visitor going true there. Those
// that only report the state as it stands (Initialized, as a subscription opens) are not a press.
func pressed(ev onvifevent.Event, channel int) bool {
	if !strings.HasSuffix(ev.Topic, "/Visitor") || ev.Op != "Changed" || ev.Items["State"] != "true" {
		return false
	}
	src, ok := ev.Items["Source"]
	if !ok {
		return true // a single camera names no source
	}
	n, err := strconv.Atoi(strings.TrimSpace(src))
	return err == nil && n == channel
}

// press rings, and shows the door, unless it rang a moment ago.
func (f *Feature) press(camera string) {
	f.mu.Lock()
	now := f.now()
	if !f.last.IsZero() && now.Sub(f.last) < again {
		f.mu.Unlock()
		return
	}
	f.last = now
	f.status.LastPress = now
	f.mu.Unlock()
	slog.Info("doorbell: pressed", "camera", camera)
	safe.Go("doorbell ring", f.ring)
	safe.Go("doorbell camera", func() { f.show(camera) })
}

func (f *Feature) setStatus(camera string, listening bool, problem string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.status
	if s.Camera == camera && s.Listening == listening && s.Problem == problem {
		return
	}
	if problem != "" && problem != s.Problem {
		slog.Warn("doorbell: not listening", "camera", camera, "why", problem)
	}
	f.status = Status{Camera: camera, Listening: listening, Since: f.now(), Problem: problem, LastPress: s.LastPress}
}

// explain is an error as somebody at the setup page can act on it.
func explain(err error, t target) string {
	host := strings.TrimPrefix(t.base, "http://")
	var ne net.Error
	switch {
	case errors.Is(err, onvifevent.ErrAuth):
		return "the recorder refused the login"
	case errors.As(err, &ne) && ne.Timeout():
		return "the recorder at " + host + " did not answer"
	case strings.Contains(err.Error(), "connection refused"):
		return "nothing answers ONVIF at " + host + ": turn ONVIF on for the recorder in the Reolink app, and check the port"
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
