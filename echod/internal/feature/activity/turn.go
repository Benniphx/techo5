package activity

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// TurnEvent is what Home Assistant's bus calls a finished turn: one per voice request, for an
// automation or the logbook to see what was asked and how it went. Under the esphome domain because
// that is the only one Home Assistant accepts from a device; docs/actions.md has the fields.
const TurnEvent = "esphome.techo5_voice"

// Version is bumped when a field changes meaning, so a reader that does not know a version can ignore
// it rather than guess.
const Version = "1"

// Ending is how a turn ended, which is not always visible from what it left behind: a turn that timed
// out and one that was answered can both have a transcript and no reply.
type Ending string

const (
	Answered Ending = "answered"
	NoSpeech Ending = "no_speech" // nothing said, or only what speech-to-text writes for noise
	Canceled Ending = "canceled"  // stopped, interrupted, cut off, or answered by another device
	Failed   Ending = "error"     // the pipeline failed or gave up; the event's error says which
)

// Trigger is what opened a turn.
type Trigger string

const (
	WakeWord      Trigger = "wake_word"
	Button        Trigger = "button"
	FollowUp      Trigger = "follow_up"
	HomeAssistant Trigger = "home_assistant" // an announcement that asked for an answer
)

// textLimit is the longest transcript or reply an event carries. Longer than a state, since a reply
// is often a paragraph, and far short of what the wire allows: every event goes into the recorder.
const textLimit = 1000

// queued is how many finished turns may wait for a Home Assistant that has stopped reading. Turns are
// seconds apart, so a full queue is a connection that is gone, and the newest is dropped.
const queued = 8

// Turn is one turn as it happens. Each phase stamps when it began and nothing is worked out until the
// turn closes, so the arithmetic lives in one place instead of at every boundary.
type Turn struct {
	mu sync.Mutex

	id   string
	slot int
	word string
	by   Trigger

	heard string
	reply string

	// failure is what went wrong, for a turn that ends in an error.
	failure string

	// audio is seconds of recording kept for this turn, zero when none was.
	audio float64

	// When each phase began, zero for one the turn never reached, and when it ended.
	listening time.Time
	thinking  time.Time
	replying  time.Time
	ended     time.Time
}

// Begin starts a turn. The id lives as long as the device holds the turn's audio, which is what asks
// for it later. word is the slot's wake word, which chose the pipeline whatever opened the turn.
func (l *Log) Begin(slot int, word string, by Trigger) *Turn {
	return &Turn{id: newID(), slot: slot, word: word, by: by}
}

// Listening says the device is taking audio.
func (t *Turn) Listening() {
	if t == nil {
		return
	}
	t.at(&t.listening)
}

// Heard says Home Assistant has enough audio and is working on an answer.
func (t *Turn) Heard(text string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	t.heard = text
	t.mu.Unlock()

	t.at(&t.thinking)
}

// Replying says an answer has arrived and is being spoken.
func (t *Turn) Replying(text string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	t.reply = text
	t.mu.Unlock()

	t.at(&t.replying)
}

// Fails says what went wrong, for the turn about to end in an error: Home Assistant's error code, or
// timeout.
func (t *Turn) Fails(code string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.failure = code
}

// Ends closes the turn and reports it. Only the first call counts: several things can notice a turn is
// over, and the one that noticed first is the one that timed it.
//
// The event is sent from a goroutine of its own. Ending is on the voice loop, and a write to a
// connection that has died waits out its timeout; the next wake must not.
func (t *Turn) Ends(how Ending) {
	if t == nil {
		return
	}

	t.mu.Lock()
	if !t.ended.IsZero() {
		t.mu.Unlock()
		return
	}
	t.ended = time.Now()
	fields := t.fields(how)
	t.mu.Unlock()

	fields["device"] = config.Get().Device.Name
	post(component.Event{Name: TurnEvent, Data: fields})
}

var (
	outbox     = make(chan component.Event, queued)
	sendOnce   sync.Once
	sendEvents = func(e component.Event) { component.Fire.Emit(e) }
)

// post hands a finished turn to the sender and returns at once.
func post(e component.Event) {
	sendOnce.Do(func() {
		safe.Go("turn events", func() {
			for e := range outbox {
				sendEvents(e)
			}
		})
	})
	select {
	case outbox <- e:
	default:
		slog.Warn("turn: Home Assistant isn't taking events; dropped one", "id", e.Data["id"])
	}
}

// Records says how long a recording was kept for this turn, which is what decides whether anything
// offers to play it. Zero for a turn nothing was kept for.
func (t *Turn) Records(seconds float64) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.audio = seconds
}

// ID is what asks the device for this turn's audio.
func (t *Turn) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

// at stamps a phase, unless the turn has closed or that phase has already begun — Home Assistant can
// say the same thing twice, and the first time is when it happened.
func (t *Turn) at(when *time.Time) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ended.IsZero() && when.IsZero() {
		*when = time.Now()
	}
}

// fields is the event's data. Everything is a string: the wire carries no other kind, so numbers go
// as decimal and a reader turns them back.
func (t *Turn) fields(how Ending) map[string]string {
	out := map[string]string{
		"version": Version,
		"id":      t.id,
		"slot":    strconv.Itoa(t.slot),
		"trigger": string(t.by),
		"ended":   string(how),
	}

	if t.word != "" {
		out["wake_word"] = component.Fit(t.word)
	}
	if t.heard != "" {
		out["heard"] = fit(t.heard)
	}
	if t.reply != "" {
		out["reply"] = fit(t.reply)
	}
	if how == Failed && t.failure != "" {
		out["error"] = component.Fit(t.failure)
	}
	if t.audio > 0 {
		out["audio_seconds"] = strconv.FormatFloat(t.audio, 'f', 1, 64)
	}

	// A phase runs until the next one the turn reached, or until it ended. One it never reached has no
	// duration rather than a zero, so a turn that failed while thinking reports no reply time.
	marks := []struct {
		key string
		at  time.Time
	}{
		{"listen_ms", t.listening},
		{"think_ms", t.thinking},
		{"reply_ms", t.replying},
	}

	for i, mark := range marks {
		if mark.at.IsZero() {
			continue
		}

		until := t.ended
		for _, next := range marks[i+1:] {
			if !next.at.IsZero() {
				until = next.at
				break
			}
		}
		if ms := until.Sub(mark.at).Milliseconds(); ms > 0 {
			out[mark.key] = strconv.FormatInt(ms, 10)
		}
	}
	return out
}

// fit shortens a transcript or a reply to textLimit characters.
func fit(text string) string { return component.FitTo(text, textLimit) }

// The id outlives its recording, in every logbook entry the recorder keeps, so it has to stay unique
// across all of them.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
