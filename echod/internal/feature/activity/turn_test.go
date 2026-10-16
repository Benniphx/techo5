package activity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/component"
)

// caught collects the events a turn fires, so a test can read what Home Assistant would have.
func caught(t *testing.T) chan component.Event {
	t.Helper()

	got := make(chan component.Event, 8)
	cancel := component.Fire.Listen(func(e component.Event) { got <- e })
	t.Cleanup(cancel)

	return got
}

// one waits for the turn's event, which goes out from a goroutine of its own, and then makes sure no
// second one follows.
func one(t *testing.T, got chan component.Event) map[string]string {
	t.Helper()

	var e component.Event
	select {
	case e = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no event fired")
	}
	if e.Name != TurnEvent {
		t.Errorf("fired %q, want %q", e.Name, TurnEvent)
	}
	select {
	case extra := <-got:
		t.Errorf("a second event fired: %v", extra.Data)
	case <-time.After(50 * time.Millisecond):
	}
	return e.Data
}

// A phase is only reported if the turn reached it, which is the difference between "spent no time
// speaking" and "never got as far as speaking".
func TestAFailedTurnReportsNoPhaseItNeverReached(t *testing.T) {
	got := caught(t)

	turn := Get().Begin(1, "hey jarvis", WakeWord)
	time.Sleep(2 * time.Millisecond)
	turn.Listening()
	time.Sleep(2 * time.Millisecond)
	turn.Heard("what time is it")
	time.Sleep(2 * time.Millisecond)
	turn.Fails("stt-stream-failed")
	turn.Ends(Failed)

	data := one(t, got)
	for _, key := range []string{"listen_ms", "think_ms"} {
		if data[key] == "" {
			t.Errorf("%s missing, and the turn reached that phase", key)
		}
	}
	if data["reply_ms"] != "" {
		t.Errorf("reply_ms is %q, and the turn never replied", data["reply_ms"])
	}
	if data["reply"] != "" {
		t.Errorf("reply is %q, and there was none", data["reply"])
	}
	if data["ended"] != "error" || data["error"] != "stt-stream-failed" {
		t.Errorf("ended %q with error %q, want error and stt-stream-failed", data["ended"], data["error"])
	}
	if data["trigger"] != "wake_word" || data["wake_word"] != "hey jarvis" {
		t.Errorf("trigger %q, wake word %q", data["trigger"], data["wake_word"])
	}
	if data["heard"] != "what time is it" {
		t.Errorf("heard is %q", data["heard"])
	}
}

func TestAWholeTurnReportsEveryPhase(t *testing.T) {
	got := caught(t)

	turn := Get().Begin(2, "alexa", Button)
	for _, step := range []func(){
		turn.Listening,
		func() { turn.Heard("play something") },
		func() { turn.Replying("playing") },
	} {
		time.Sleep(2 * time.Millisecond)
		step()
	}
	time.Sleep(2 * time.Millisecond)
	turn.Ends(Answered)

	data := one(t, got)
	for _, key := range []string{"listen_ms", "think_ms", "reply_ms"} {
		if data[key] == "" {
			t.Errorf("%s missing", key)
		}
	}
	if data["slot"] != "2" {
		t.Errorf("slot is %q, want 2", data["slot"])
	}
	if data["id"] == "" {
		t.Error("no id, so nothing could ask for the recording")
	}
	if data["version"] != Version {
		t.Errorf("version is %q, want %q", data["version"], Version)
	}
	if data["ended"] != "answered" || data["trigger"] != "button" || data["reply"] != "playing" {
		t.Errorf("ended %q, trigger %q, reply %q", data["ended"], data["trigger"], data["reply"])
	}
	if _, ok := data["error"]; ok {
		t.Errorf("an answered turn carries an error: %q", data["error"])
	}
	if _, ok := data["device"]; !ok {
		t.Error("no device field")
	}
}

// A transcript or a reply is kept short enough for the recorder, and a mark says it was cut.
func TestLongTextIsBounded(t *testing.T) {
	got := caught(t)

	long := strings.Repeat("ab", textLimit)
	turn := Get().Begin(1, "hey jarvis", FollowUp)
	turn.Heard(long)
	turn.Replying(long)
	turn.Ends(Answered)

	data := one(t, got)
	for _, key := range []string{"heard", "reply"} {
		if n := utf8.RuneCountInString(data[key]); n != textLimit || !strings.HasSuffix(data[key], "…") {
			t.Errorf("%s is %d characters, want %d ending in a mark", key, n, textLimit)
		}
	}
	if data["trigger"] != "follow_up" {
		t.Errorf("trigger is %q, want follow_up", data["trigger"])
	}
}

// Several things notice a turn is over — the run closing, the reply finishing, a timeout — and the
// event has to be the one, not one each.
func TestEndingTwiceFiresOnce(t *testing.T) {
	got := caught(t)

	turn := Get().Begin(1, "hey jarvis", WakeWord)
	turn.Ends(Answered)
	turn.Ends(Failed)

	if data := one(t, got); data["ended"] != string(Answered) {
		t.Errorf("ended is %q, want the first one", data["ended"])
	}
}

// A turn nobody started is what a cancel arriving while idle has, so every method takes a nil.
func TestNoTurnIsSafeToMark(t *testing.T) {
	got := caught(t)

	var turn *Turn
	turn.Listening()
	turn.Heard("nothing")
	turn.Replying("nothing")
	turn.Fails("timeout")
	turn.Ends(Canceled)

	if turn.ID() != "" {
		t.Errorf("id is %q, want empty", turn.ID())
	}
	select {
	case e := <-got:
		t.Errorf("fired %v, want none", e.Data)
	case <-time.After(50 * time.Millisecond):
	}
}
