package voice

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/led"
)

// watcher is a conversation with Home Assistant subscribed, a clock the test moves, and the runs it
// opens at the wake word stage counted instead of sent. The loop is not running: the test hands it
// events, and flush runs what it queued for Home Assistant.
type watcher struct {
	*conversation
	t       *testing.T
	at      time.Time
	mu      sync.Mutex
	watches []string
	ready   bool
	busyMic bool
}

func newWatcher(t *testing.T, on config.OnHA) *watcher {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	set := config.Set().OnHA()
	if err := set.Stream(on.Stream); err != nil {
		t.Fatal(err)
	}
	if err := set.Confirm(on.Confirm); err != nil {
		t.Fatal(err)
	}

	w := &watcher{t: t, at: time.Now(), ready: true}
	c := newConversation(&esphome.VoiceSatellite{})
	c.claim = c.leds.Claim(led.PriorityTurn)
	c.now = func() time.Time { return w.at }
	c.busy = func() bool { return w.busyMic }
	c.ha.ready = func() bool { return w.ready }
	c.ha.watch = func(phrase string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.watches = append(w.watches, phrase)
		return nil
	}
	w.conversation = c
	t.Cleanup(func() {
		c.handle(event{kind: evCancel})
		c.dropWatch()
		c.claim.Release()
	})
	return w
}

// flush runs what was queued for Home Assistant, in order.
func (w *watcher) flush() {
	for {
		select {
		case fn := <-w.out:
			_ = fn()
		default:
			return
		}
	}
}

func (w *watcher) opened() int {
	w.flush()
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.watches)
}

func (w *watcher) later(d time.Duration) {
	w.at = w.at.Add(d)
	w.handle(event{kind: evWatch})
}

// Streaming opens a run at the wake word stage while the device is idle, and Home Assistant hearing
// the word opens the turn on that same run: no second run, and the device's own first slot stands down
// only once Home Assistant's engine has said it is listening.
func TestStreamingOpensTheTurnOnTheSameRun(t *testing.T) {
	w := newWatcher(t, config.OnHA{Stream: true})
	w.handle(event{kind: evWatch})
	if w.watch == nil || w.watch.mode != watchStream || w.opened() != 1 {
		t.Fatalf("streaming on and idle: watch %+v, runs %d", w.watch, w.opened())
	}
	if w.hears.Load() {
		t.Error("the device stood down before home assistant said it was listening")
	}
	w.handle(event{kind: evWakeStart})
	if !w.hears.Load() {
		t.Error("home assistant is listening and the device has not stood down")
	}

	w.handle(event{kind: evWakeEnd})
	if w.phase != phaseListening || w.watch != nil || !w.woke {
		t.Fatalf("after home assistant heard the word: phase %s, watch %+v, woke %v", w.phase, w.watch, w.woke)
	}
	if w.hears.Load() {
		t.Error("the device is still standing down during the turn")
	}

	// The turn ends; the next run waits for this one to close, then opens.
	w.handle(event{kind: evCancel})
	if w.watch != nil {
		t.Fatal("a run opened before the turn's had closed")
	}
	w.handle(event{kind: evRunEnd})
	if w.watch == nil || w.opened() != 2 {
		t.Fatalf("after the turn's run closed: watch %+v, runs %d", w.watch, w.opened())
	}
}

// A run that ends the ordinary way is started again; one whose pipeline has no wake word engine leaves
// the device listening for itself and is tried again later, longer each time.
func TestARunIsRestartedOrBackedOffFrom(t *testing.T) {
	w := newWatcher(t, config.OnHA{Stream: true})
	w.handle(event{kind: evWatch})
	w.handle(event{kind: evWakeStart})
	w.at = w.at.Add(time.Minute)

	w.handle(event{kind: evError, code: "wake-word-timeout"})
	w.handle(event{kind: evRunEnd})
	if w.watch == nil || w.opened() != 2 || w.fallback {
		t.Fatalf("after an ordinary end: watch %+v, runs %d, fallback %v", w.watch, w.opened(), w.fallback)
	}

	w.handle(event{kind: evError, code: "wake-engine-missing", msg: "no engine"})
	if !w.fallback || w.hears.Load() {
		t.Fatalf("no engine: fallback %v, standing down %v", w.fallback, w.hears.Load())
	}
	w.handle(event{kind: evRunEnd})
	if w.watch != nil {
		t.Fatal("a run was opened again at once after the engine was missing")
	}
	w.later(retryLong - time.Second)
	if w.watch != nil {
		t.Fatal("tried again before the wait was over")
	}
	w.later(2 * time.Second)
	if w.watch == nil || w.opened() != 3 {
		t.Fatalf("after the wait: watch %+v, runs %d", w.watch, w.opened())
	}

	// Missing again: twice as long.
	w.handle(event{kind: evError, code: "wake-engine-missing"})
	w.handle(event{kind: evRunEnd})
	w.later(retryLong + time.Second)
	if w.watch != nil {
		t.Fatal("the second wait was not longer than the first")
	}
	w.later(retryLong)
	if w.watch == nil {
		t.Fatal("not tried again after the second wait")
	}
	w.handle(event{kind: evWakeStart})
	if w.fallback || w.failures != 0 || !w.hears.Load() {
		t.Errorf("listening again: fallback %v, failures %d, standing down %v", w.fallback, w.failures, w.hears.Load())
	}
}

// A wake the device was not sure of is asked about; one Home Assistant does not hear in time is dropped
// without a turn, and one it hears opens the turn and is told as confirmed, with the device's score.
func TestACandidateIsConfirmedOrDropped(t *testing.T) {
	confirmed := make(chan Confirmation, 2)
	t.Cleanup(Confirmed.Listen(func(c Confirmation) { confirmed <- c }))

	w := newWatcher(t, config.OnHA{Confirm: true})
	w.handle(event{kind: evWatch})
	if w.watch != nil {
		t.Fatal("a run opened with only confirming on")
	}

	w.handle(event{kind: evCandidate, slot: 0, peak: 0.74})
	if w.watch == nil || w.watch.mode != watchConfirm || w.opened() != 1 {
		t.Fatalf("a candidate: watch %+v, runs %d", w.watch, w.opened())
	}
	if w.hears.Load() {
		t.Error("confirming stood the device down")
	}
	w.handle(event{kind: evUnconfirmed})
	if w.watch == nil || !w.watch.closing || w.phase != phaseIdle {
		t.Fatalf("not heard in time: watch %+v, phase %s", w.watch, w.phase)
	}
	w.handle(event{kind: evRunEnd})
	if w.watch != nil || w.phase != phaseIdle {
		t.Fatalf("after the run closed: watch %+v, phase %s", w.watch, w.phase)
	}
	select {
	case c := <-confirmed:
		t.Fatalf("a wake Home Assistant did not hear was told as confirmed: %+v", c)
	default:
	}

	w.handle(event{kind: evCandidate, slot: 0, peak: 0.81})
	w.handle(event{kind: evWakeEnd})
	if w.phase != phaseListening || !w.woke {
		t.Fatalf("a confirmed wake: phase %s, woke %v", w.phase, w.woke)
	}
	select {
	case c := <-confirmed:
		if c.Peak != 0.81 || c.Slot != 0 {
			t.Errorf("confirmed %+v, want slot 0 at 0.81", c)
		}
	case <-time.After(time.Second):
		t.Fatal("the confirmed wake was not told")
	}

	// Busy: a candidate during a turn is not asked about.
	w.handle(event{kind: evCandidate, slot: 0, peak: 0.8})
	if w.watch != nil {
		t.Error("a candidate during a turn was asked about")
	}
}

// With confirming off, a candidate is nothing.
func TestACandidateIsIgnoredWithConfirmingOff(t *testing.T) {
	w := newWatcher(t, config.OnHA{})
	w.handle(event{kind: evCandidate, slot: 0, peak: 0.8})
	if w.watch != nil || w.opened() != 0 {
		t.Fatalf("confirming off: watch %+v, runs %d", w.watch, w.opened())
	}
}

// The microphones cut, or taken by a call, close the stream at once and keep it closed; and what Home
// Assistant heard while they were is not a turn.
func TestAMuteOrACallStopsTheStream(t *testing.T) {
	w := newWatcher(t, config.OnHA{Stream: true})
	w.handle(event{kind: evWatch})
	w.handle(event{kind: evWakeStart})

	w.busyMic = true
	w.handle(event{kind: evWatch})
	if w.watch == nil || !w.watch.closing || w.hears.Load() {
		t.Fatalf("muted: watch %+v, standing down %v", w.watch, w.hears.Load())
	}
	w.handle(event{kind: evWakeEnd})
	if w.phase != phaseIdle {
		t.Fatalf("a wake heard as the stream was closing opened a turn: %s", w.phase)
	}
	w.handle(event{kind: evRunEnd})
	w.later(time.Minute)
	if w.watch != nil {
		t.Fatal("a run opened while muted")
	}

	w.busyMic = false
	w.later(time.Second)
	if w.watch == nil {
		t.Fatal("no run after the mute ended")
	}
}

// Home Assistant going away hands the wake word back to the device at once; and a turn asked for on the
// device while a run is open waits for that run to close rather than opening into its events.
func TestFallingBackAndATurnFromTheDevice(t *testing.T) {
	w := newWatcher(t, config.OnHA{Stream: true})
	w.handle(event{kind: evWatch})
	w.handle(event{kind: evWakeStart})

	w.ready = false
	w.handle(event{kind: evWatch})
	if w.watch != nil || w.hears.Load() {
		t.Fatalf("home assistant gone: watch %+v, standing down %v", w.watch, w.hears.Load())
	}

	w.ready = true
	w.handle(event{kind: evWatch})
	w.handle(event{kind: evWakeStart})
	asked := w.at
	w.handle(event{kind: evStart, slot: 0, woke: true, at: asked})
	if w.phase != phaseIdle || w.pending == nil || w.watch == nil || !w.watch.closing {
		t.Fatalf("a wake on the device during a run: phase %s, pending %+v, watch %+v", w.phase, w.pending, w.watch)
	}
	if w.hears.Load() {
		t.Error("still standing down while the run closes")
	}
	w.handle(event{kind: evRunEnd})
	if w.phase != phaseListening || w.watch != nil || !w.conversation.opened.Equal(asked) {
		t.Fatalf("after the run closed: phase %s, watch %+v, opened %v", w.phase, w.watch, w.conversation.opened)
	}
}
