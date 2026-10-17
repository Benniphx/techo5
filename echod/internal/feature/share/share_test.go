package share

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// rig is this device as "Kitchen", three settings of its own, and what it sends, kept.
type rig struct {
	f      *Feature
	bass   *esphome.Number
	theme  *esphome.Select
	lyrics *esphome.Switch

	mu   sync.Mutex
	sent map[string][]message
	at   time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Share().On(true); err != nil {
		t.Fatal(err)
	}
	r := &rig{sent: map[string][]message{}, at: time.Unix(1_000_000, 0)}
	r.bass = &esphome.Number{Base: esphome.Base{ObjectID: "bass"}, Min: -6, Max: 6, Step: 1}
	r.bass.OnCommand = func(v float32) { r.bass.Set(v) }
	r.theme = &esphome.Select{Base: esphome.Base{ObjectID: "screen_theme"}, Options: []string{"Dark", "Light"}}
	r.theme.OnCommand = func(v string) { r.theme.Set(v) }
	r.theme.Set("Dark")
	r.lyrics = &esphome.Switch{Base: esphome.Base{ObjectID: "lyrics"}}
	r.lyrics.OnCommand = func(v bool) { r.lyrics.Set(v) }
	own := map[string]esphome.Entity{"bass": r.bass, "screen_theme": r.theme, "lyrics": r.lyrics}
	was := entities
	entities = func(id string) (esphome.Entity, bool) { e, ok := own[id]; return e, ok }
	t.Cleanup(func() { entities = was })

	r.f = &Feature{
		me:   func() string { return "Kitchen" },
		now:  func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.at },
		wake: make(chan struct{}, 1),
		last: map[string]string{},
	}
	r.f.send = func(_ context.Context, to string, body []byte) ([]byte, error) {
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			t.Errorf("sent something that is not a message: %v", err)
		}
		r.mu.Lock()
		r.sent[to] = append(r.sent[to], m)
		r.mu.Unlock()
		results := map[string]string{}
		for _, it := range m.Items {
			results[it.ID] = Applied
		}
		for _, l := range m.Links {
			results[l.ID] = Applied
		}
		return json.Marshal(reply{Results: results})
	}
	return r
}

func (r *rig) tick(d time.Duration) {
	r.mu.Lock()
	r.at = r.at.Add(d)
	r.mu.Unlock()
}

func (r *rig) receive(t *testing.T, from string, m message) map[string]string {
	t.Helper()
	body, _ := json.Marshal(m)
	raw, err := r.f.receive(from, body)
	if err != nil {
		t.Fatal(err)
	}
	var rep reply
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	return rep.Results
}

func (r *rig) sentTo(name string) []message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]message(nil), r.sent[name]...)
}

func TestALinkedChangeIsTakenOnlyFromALinkedDevice(t *testing.T) {
	r := newRig(t)
	_ = config.Set().Share().Link("bass", []string{"Office"})

	got := r.receive(t, "Office", message{Kind: "set", Items: []item{{ID: "bass", Value: "3", At: r.at}}})
	if got["bass"] != Applied || r.bass.Get() != 3 {
		t.Fatalf("from a linked device: %v, bass %v", got, r.bass.Get())
	}
	if from := config.Get().Share.From["bass"]; from.Device != "Office" {
		t.Errorf("where it came from: %+v", from)
	}
	got = r.receive(t, "Bedroom", message{Kind: "set", Items: []item{{ID: "bass", Value: "5", At: r.at.Add(time.Minute)}}})
	if got["bass"] != NotLinked || r.bass.Get() != 3 {
		t.Fatalf("from a device it is not linked with: %v, bass %v", got, r.bass.Get())
	}
}

func TestTheLatestChangeWins(t *testing.T) {
	r := newRig(t)
	_ = config.Set().Share().Link("bass", []string{"Office"})
	r.receive(t, "Office", message{Kind: "set", Items: []item{{ID: "bass", Value: "3", At: r.at}}})
	got := r.receive(t, "Office", message{Kind: "set", Items: []item{{ID: "bass", Value: "1", At: r.at.Add(-time.Second)}}})
	if got["bass"] != Older || r.bass.Get() != 3 {
		t.Fatalf("an older change: %v, bass %v", got, r.bass.Get())
	}
}

func TestAKeptSettingIsNotChanged(t *testing.T) {
	r := newRig(t)
	_ = config.Set().Share().Link("bass", []string{"Office"})
	_ = config.Set().Share().Keep("bass", true)
	got := r.receive(t, "Office", message{Kind: "set", Items: []item{{ID: "bass", Value: "3", At: r.at}}})
	if got["bass"] != Kept || r.bass.Get() != 0 {
		t.Fatalf("a linked change: %v, bass %v", got, r.bass.Get())
	}
	got = r.receive(t, "Office", message{Kind: "copy", Items: []item{{ID: "bass", Value: "3", At: r.at}}})
	if got["bass"] != Kept || r.bass.Get() != 0 {
		t.Fatalf("a copy: %v, bass %v", got, r.bass.Get())
	}
}

func TestACopyNeedsNoLink(t *testing.T) {
	r := newRig(t)
	got := r.receive(t, "Office", message{Kind: "copy", Items: []item{
		{ID: "screen_theme", Value: "Light", At: r.at},
		{ID: "lyrics", Value: "on", At: r.at},
	}})
	if got["screen_theme"] != Applied || r.theme.Get() != "Light" || got["lyrics"] != Applied || !r.lyrics.Get() {
		t.Fatalf("%v theme %q lyrics %v", got, r.theme.Get(), r.lyrics.Get())
	}
}

func TestWhatIsNeverTaken(t *testing.T) {
	r := newRig(t)
	got := r.receive(t, "Office", message{Kind: "copy", Items: []item{
		{ID: "sendspin", Value: "on"},          // opens a port: never on the list
		{ID: "screen_theme", Value: "Plaid"},   // no such option
		{ID: "bass", Value: "99"},              // past its range
		{ID: "screen_clock_style", Value: "x"}, // on the list, not on this device
	}})
	want := map[string]string{"sendspin": NotShared, "screen_theme": BadValue, "bass": BadValue, "screen_clock_style": NotHere}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: %q, want %q", id, got[id], w)
		}
	}
	if r.theme.Get() != "Dark" || r.bass.Get() != 0 {
		t.Error("a value refused was applied")
	}
	if _, err := r.f.receive("Kitchen", []byte(`{"kind":"copy"}`)); err == nil {
		t.Error("a message in this device's own name was taken")
	}
	if _, err := r.f.receive("Office", []byte(`{"kind":"reboot"}`)); err == nil {
		t.Error("an unknown kind was taken")
	}
}

func TestNothingIsTakenWithSharingOff(t *testing.T) {
	r := newRig(t)
	_ = config.Set().Share().On(false)
	got := r.receive(t, "Office", message{Kind: "copy", Items: []item{{ID: "bass", Value: "3"}}})
	if got["bass"] != NotTaking || r.bass.Get() != 0 {
		t.Fatalf("%v bass %v", got, r.bass.Get())
	}
}

// A change made here goes to the setting's group, once; one taken from another device is not sent back.
func TestAChangeHereGoesToTheGroupAndIsNotEchoed(t *testing.T) {
	r := newRig(t)
	_ = config.Set().Share().Link("bass", []string{"Office", "Bedroom"})
	ctx := context.Background()
	r.f.watch(ctx) // learns the value
	r.bass.OnCommand(2)
	r.tick(time.Second)
	r.f.watch(ctx)
	r.f.watch(ctx)
	waitFor(t, func() bool { return len(r.sentTo("Office")) == 1 && len(r.sentTo("Bedroom")) == 1 })
	m := r.sentTo("Office")[0]
	if m.Kind != "set" || len(m.Items) != 1 || m.Items[0].ID != "bass" || m.Items[0].Value != "2" {
		t.Fatalf("sent %+v", m)
	}

	r.receive(t, "Office", message{Kind: "set", Items: []item{{ID: "bass", Value: "4", At: r.at.Add(time.Minute)}}})
	r.f.watch(ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(r.sentTo("Office")) + len(r.sentTo("Bedroom")); n != 2 {
		t.Fatalf("a change taken from another device was sent on: %d messages", n)
	}
}

// Linking from here tells the group, with this device's value, and tells a device taken out of it.
func TestLinkTellsTheGroup(t *testing.T) {
	r := newRig(t)
	r.theme.Set("Light")
	if _, err := r.f.Link(context.Background(), "screen_theme", []string{"Office", "Bedroom"}); err != nil {
		t.Fatal(err)
	}
	m := r.sentTo("Bedroom")[0]
	if m.Kind != "link" || len(m.Links) != 1 || len(m.Links[0].Group) != 3 || len(m.Items) != 1 || m.Items[0].Value != "Light" {
		t.Fatalf("sent %+v", m)
	}
	if _, err := r.f.Link(context.Background(), "screen_theme", []string{"Office"}); err != nil {
		t.Fatal(err)
	}
	out := r.sentTo("Bedroom")
	if last := out[len(out)-1]; last.Kind != "link" || len(last.Links[0].Group) != 0 {
		t.Fatalf("the device taken out was told %+v", last)
	}
	if got := config.Get().Share.Linked("screen_theme"); len(got) != 1 || got[0] != "Office" {
		t.Fatalf("links here %v", got)
	}
	if _, err := r.f.Link(context.Background(), "sendspin", []string{"Office"}); err == nil {
		t.Error("a setting not on the list was linked")
	}
}

// A group from another device links this one with the rest of it, and takes the sender's value with it.
func TestALinkFromAnotherDevice(t *testing.T) {
	r := newRig(t)
	got := r.receive(t, "Office", message{Kind: "link",
		Links: []link{{ID: "bass", Group: []string{"Office", "Kitchen", "Bedroom"}}},
		Items: []item{{ID: "bass", Value: "2", At: r.at}}})
	if got["bass"] != Applied || r.bass.Get() != 2 {
		t.Fatalf("%v bass %v", got, r.bass.Get())
	}
	if l := config.Get().Share.Linked("bass"); len(l) != 2 {
		t.Fatalf("linked with %v", l)
	}
	r.receive(t, "Office", message{Kind: "link", Links: []link{{ID: "bass", Group: nil}}})
	if l := config.Get().Share.Linked("bass"); len(l) != 0 {
		t.Fatalf("still linked with %v", l)
	}
}

func TestCopy(t *testing.T) {
	r := newRig(t)
	got := r.f.Copy(context.Background(), []string{"bass", "sendspin", "screen_theme"}, []string{"Office", "Kitchen"})
	if len(got) != 1 || len(r.sentTo("Kitchen")) != 0 {
		t.Fatalf("copied to %v", got)
	}
	m := r.sentTo("Office")[0]
	if m.Kind != "copy" || len(m.Items) != 2 {
		t.Fatalf("sent %+v: sendspin is never sent", m)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 200 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("not in time")
}
