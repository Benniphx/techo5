// Package share keeps settings in step between the devices of the house, setting by setting, with the
// devices each one is chosen for, or copies settings to other devices once.
//
//   - A linked setting is kept in step with the devices it is linked to: a change on any of them, from the
//     screen, Home Assistant or the setup page, reaches the others. There is no leader: the latest change
//     wins, and each device knows where its value came from.
//   - A copy sends settings to other devices once, with no link: from the setup page, or Home Assistant's
//     copy_settings action.
//   - Any device can keep any setting its own: nothing another device sends changes it, and the sender is
//     told it was kept.
//
// Off until it is turned on, on each device: one that has not been turned on takes nothing. Only the
// settings on the list (catalog.go) travel, never what opens a door, uses the microphones or the camera,
// holds a secret or says where the device's voice and words go. Messages are signed with the house word
// and bound to the two devices (feature/announce's share.go). What a device takes is logged, with whom it
// came from.
package share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/announce"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

func init() {
	component.Register(component.Network, Get(), component.Order(40))
}

// watchEvery is how often the linked settings are looked at for a change made here.
const watchEvery = time.Second

// Outcomes, as a device answers for each setting sent to it.
const (
	Applied     = "applied"       // taken
	Kept        = "kept"          // kept its own
	NotLinked   = "not linked"    // a linked change for a setting this device has not linked with the sender
	NotHere     = "not here"      // this device has no such setting
	NotShared   = "not shareable" // not on the list
	Older       = "older"         // this device has a later change
	BadValue    = "bad value"     // not a value the setting takes
	NotTaking   = "not taking"    // sharing is off on this device
	Unreachable = "unreachable"   // the device could not be reached
)

// message is what travels between two devices.
type message struct {
	Kind  string `json:"kind"` // set, copy, link
	Items []item `json:"items,omitempty"`
	Links []link `json:"links,omitempty"`
}

type item struct {
	ID    string    `json:"id"`
	Value string    `json:"value"`
	At    time.Time `json:"at"`
}

// link is a setting's whole group, the sender included: the receiver links the setting with the rest, or
// unlinks it when it is not in the group.
type link struct {
	ID    string   `json:"id"`
	Group []string `json:"group"`
}

type reply struct {
	Results map[string]string `json:"results"`
}

type Feature struct {
	sw *esphome.Switch

	// send carries a message to a device; replaced in tests.
	send func(ctx context.Context, to string, body []byte) ([]byte, error)
	// me is this device's name; now the clock; both replaced in tests.
	me  func() string
	now func() time.Time

	wake chan struct{}

	mu   sync.Mutex
	last map[string]string // the linked settings' values as last seen or taken, so a change is seen once
}

var (
	once   sync.Once
	shared *Feature
)

func Get() *Feature {
	once.Do(func() {
		f := &Feature{
			send: announce.SendShare,
			me:   func() string { return config.Get().Device.Name },
			now:  time.Now,
			wake: make(chan struct{}, 1),
			last: map[string]string{},
		}
		f.sw = &esphome.Switch{
			Base: esphome.Base{ObjectID: "settings_sharing", Name: "Settings sharing", Icon: "mdi:sync",
				Category: esphome.CategoryConfig},
			OnCommand: f.SetOn,
		}
		announce.ShareOpen = func() bool { return config.Get().Share.On }
		announce.ShareReceive = f.receive
		shared = f
	})
	return shared
}

func (f *Feature) Name() string { return "settings sharing" }

func (f *Feature) Entities() []esphome.Entity { return []esphome.Entity{f.sw} }

func (f *Feature) Restore(c config.Config) { f.sw.Set(c.Share.On) }

// SetOn is the switch.
func (f *Feature) SetOn(on bool) {
	if err := config.Set().Share().On(on); err != nil {
		slog.Error("saving a setting failed", "setting", "settings sharing", "err", err)
		f.sw.Set(!on)
		return
	}
	f.sw.Set(on)
	slog.Info("setting changed", "setting", "settings sharing", "using", on)
	f.poke()
}

func (f *Feature) poke() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Run watches the linked settings for a change made here, while sharing is on, and sends it on.
func (f *Feature) Run(ctx context.Context) error {
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
	f.mu.Lock()
	f.last = map[string]string{}
	f.mu.Unlock()
	for {
		f.watch(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-f.wake:
		case <-tick.C:
		}
	}
}

// watch looks at every linked setting once, and sends each that changed here to its group.
func (f *Feature) watch(ctx context.Context) {
	c := config.Get().Share
	if !c.On {
		return
	}
	out := map[string][]item{}
	now := f.now()
	f.mu.Lock()
	for id, devices := range c.Links {
		e, ok := entities(id)
		if !ok || !Shareable(id) || len(devices) == 0 {
			continue
		}
		v, ok := valueOf(e)
		if !ok {
			continue
		}
		was, seen := f.last[id]
		f.last[id] = v
		if !seen || was == v {
			continue // the first look only learns the value
		}
		for _, d := range devices {
			out[d] = append(out[d], item{ID: id, Value: v, At: now})
		}
	}
	f.mu.Unlock()
	if len(out) == 0 {
		return
	}
	for id := range changedIDs(out) {
		_ = config.Set().Share().From(id, config.ShareFrom{Device: f.me(), At: now})
	}
	for d, items := range out {
		safe.Go("share", func() { f.deliver(ctx, d, message{Kind: "set", Items: items}) })
	}
}

func changedIDs(out map[string][]item) map[string]bool {
	ids := map[string]bool{}
	for _, items := range out {
		for _, it := range items {
			ids[it.ID] = true
		}
	}
	return ids
}

// deliver sends a message to one device and logs what it made of it.
func (f *Feature) deliver(ctx context.Context, to string, m message) map[string]string {
	body, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	raw, err := f.send(ctx, to, body)
	if err != nil {
		slog.Warn("share: not delivered", "to", to, "kind", m.Kind, "err", err)
		out := map[string]string{}
		for _, it := range m.Items {
			out[it.ID] = Unreachable
		}
		return out
	}
	var r reply
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	for id, outcome := range r.Results {
		if outcome != Applied {
			slog.Info("share: a device did not take a setting", "to", to, "setting", id, "outcome", outcome)
		}
	}
	return r.Results
}

// receive is a message from another device.
func (f *Feature) receive(from string, body []byte) ([]byte, error) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.New("not a message this device reads")
	}
	if strings.EqualFold(from, f.me()) {
		return nil, errors.New("a message from this device's own name")
	}
	c := config.Get().Share
	results := map[string]string{}
	if !c.On {
		for _, it := range m.Items {
			results[it.ID] = NotTaking
		}
		return json.Marshal(reply{Results: results})
	}
	switch m.Kind {
	case "set", "copy":
		for _, it := range m.Items {
			results[it.ID] = f.take(from, it, m.Kind == "set", c)
		}
	case "link":
		for _, l := range m.Links {
			results[l.ID] = f.relink(from, l)
		}
		c = config.Get().Share
		for _, it := range m.Items {
			results[it.ID] = f.take(from, it, true, c)
		}
	default:
		return nil, fmt.Errorf("not a kind of message this device reads: %q", m.Kind)
	}
	return json.Marshal(reply{Results: results})
}

// take is one setting from another device: a linked change only from a device it is linked with, and
// only one later than this device's own last change; a copy whenever it comes.
func (f *Feature) take(from string, it item, linked bool, c config.Share) string {
	if !Shareable(it.ID) {
		return NotShared
	}
	e, ok := entities(it.ID)
	if !ok {
		return NotHere
	}
	if c.Kept(it.ID) {
		slog.Info("share: kept this device's own", "setting", it.ID, "from", from)
		return Kept
	}
	if linked {
		if !c.LinkedWith(it.ID, from) {
			return NotLinked
		}
		if last, ok := c.From[it.ID]; ok && !it.At.After(last.At) {
			return Older
		}
	}
	f.mu.Lock()
	was, _ := valueOf(e)
	if was == it.Value {
		f.last[it.ID] = was
		f.mu.Unlock()
		_ = config.Set().Share().From(it.ID, config.ShareFrom{Device: from, At: it.At})
		return Applied
	}
	// What this applies is no change made here: the watch is told the new value first, so it does not
	// send it back.
	f.last[it.ID] = it.Value
	err := apply(e, it.Value)
	now, _ := valueOf(e)
	f.last[it.ID] = now
	f.mu.Unlock()
	if err != nil {
		return BadValue
	}
	_ = config.Set().Share().From(it.ID, config.ShareFrom{Device: from, At: it.At})
	slog.Info("share: setting changed by another device", "setting", it.ID, "from", from, "was", was, "now", now)
	return Applied
}

// relink takes a setting's group from another device: linked with the rest of it, or unlinked when this
// device is not in it.
func (f *Feature) relink(from string, l link) string {
	if !Shareable(l.ID) {
		return NotShared
	}
	me := f.me()
	in := slices.ContainsFunc(l.Group, func(d string) bool { return strings.EqualFold(d, me) })
	var rest []string
	if in {
		for _, d := range l.Group {
			if !strings.EqualFold(d, me) && !slices.ContainsFunc(rest, func(r string) bool { return strings.EqualFold(r, d) }) {
				rest = append(rest, d)
			}
		}
	}
	if err := config.Set().Share().Link(l.ID, rest); err != nil {
		return BadValue
	}
	slog.Info("share: links changed by another device", "setting", l.ID, "from", from, "with", rest)
	return Applied
}

// Link sets a setting's group from here: this device and the devices given. Each device in the group, and
// each that was in it and is not now, is told; this device's value goes to the group with it. Answers
// what each device made of it.
func (f *Feature) Link(ctx context.Context, id string, devices []string) (map[string]string, error) {
	if !Shareable(id) {
		return nil, fmt.Errorf("%s is not a setting that can be shared", id)
	}
	me := f.me()
	var others []string
	for _, d := range devices {
		d = strings.TrimSpace(d)
		if d != "" && !strings.EqualFold(d, me) && !slices.ContainsFunc(others, func(o string) bool { return strings.EqualFold(o, d) }) {
			others = append(others, d)
		}
	}
	was := config.Get().Share.Linked(id)
	if err := config.Set().Share().Link(id, others); err != nil {
		return nil, err
	}
	group := append([]string{me}, others...)
	var items []item
	if e, ok := entities(id); ok {
		if v, ok := valueOf(e); ok {
			at := f.now()
			items = []item{{ID: id, Value: v, At: at}}
			_ = config.Set().Share().From(id, config.ShareFrom{Device: me, At: at})
			f.mu.Lock()
			f.last[id] = v
			f.mu.Unlock()
		}
	}
	results := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	tell := func(d string, m message) {
		defer wg.Done()
		r := f.deliver(ctx, d, m)
		mu.Lock()
		results[d] = r[id]
		mu.Unlock()
	}
	for _, d := range others {
		wg.Add(1)
		go tell(d, message{Kind: "link", Links: []link{{ID: id, Group: group}}, Items: items})
	}
	for _, d := range was {
		if !slices.ContainsFunc(others, func(o string) bool { return strings.EqualFold(o, d) }) {
			wg.Add(1)
			go tell(d, message{Kind: "link", Links: []link{{ID: id, Group: nil}}})
		}
	}
	wg.Wait()
	slog.Info("share: links set here", "setting", id, "with", others)
	return results, nil
}

// Copy sends settings to devices once, with no link: answers, by device, what each made of each setting.
func (f *Feature) Copy(ctx context.Context, ids, devices []string) map[string]map[string]string {
	at := f.now()
	var items []item
	for _, id := range ids {
		if !Shareable(id) {
			continue
		}
		e, ok := entities(id)
		if !ok {
			continue
		}
		if v, ok := valueOf(e); ok {
			items = append(items, item{ID: id, Value: v, At: at})
		}
	}
	out := map[string]map[string]string{}
	if len(items) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, d := range devices {
		if strings.EqualFold(d, f.me()) {
			continue
		}
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			r := f.deliver(ctx, d, message{Kind: "copy", Items: items})
			mu.Lock()
			out[d] = r
			mu.Unlock()
		}(d)
	}
	wg.Wait()
	slog.Info("share: settings copied", "settings", len(items), "to", len(out))
	return out
}
