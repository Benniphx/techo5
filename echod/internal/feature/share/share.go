// Package share keeps settings in step between the devices of the house, setting by setting, with the
// devices each one is chosen for, or copies settings to other devices once.
//
//   - A linked setting is kept in step with the devices it is linked to: a change on any of them, from the
//     screen, Home Assistant or the setup page, reaches the others, passed on from one to the next. There is
//     no leader. Each change is numbered one past the highest the device making it had seen, and the
//     highest number wins (the device's name breaks a tie), so the order does not rest on any device's clock.
//   - A link is made on each device, never by another: ticking a device for a setting here links it here and
//     asks that device, which links back only when somebody accepts there. A device can always take itself
//     out of a link.
//   - A copy sends settings to other devices once, with no link: from the setup page, or Home Assistant's
//     copy_settings action. Any device of the house can copy to a device that has sharing on; a copy landing
//     on a setting linked there is passed on to its links, as a change made there would be.
//   - Any device can keep any setting its own: nothing another device sends changes it, and the sender is
//     told it was kept.
//
// Off until it is turned on, on each device: one that has not been turned on takes nothing. Only the
// settings on the list (catalog.go) travel. Messages are signed with the house word and bound to the two
// devices (feature/announce's share.go). What a device takes, and from whom, is logged.
package share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

const (
	// watchEvery is how often the linked settings are looked at for a change made here.
	watchEvery = time.Second

	// sendWithin is how long one message to one device may take.
	sendWithin = 10 * time.Second

	// maxJump is the furthest a change's number may run ahead of the one this device has: changes made
	// elsewhere while it was away number a handful, and a number far past that would end every change
	// made here after it.
	maxJump = 1 << 20
)

// Outcomes, as a device answers for each setting sent to it.
const (
	Applied     = "applied"       // taken
	Same        = "same"          // already so
	Kept        = "kept"          // kept its own
	NotLinked   = "not linked"    // a linked change from a device this one has not linked the setting with
	NotHere     = "not here"      // this device has no such setting
	NotShared   = "not shareable" // not on the list
	Older       = "older"         // this device has a later change
	BadValue    = "bad value"     // not a value the setting takes, or a number out of order
	NotTaking   = "not taking"    // sharing is off on that device
	Unreachable = "unreachable"   // the device could not be reached
	Linked      = "linked"        // a link in place both ways
	Asked       = "asked"         // a link request waiting to be accepted there
	Full        = "full"          // the setting is linked with as many devices as it may be
)

// message is what travels between two devices.
type message struct {
	Kind  string `json:"kind"` // set, copy, link
	Items []item `json:"items,omitempty"`
	// Link, for a link message: request, accept or remove, for the setting ID.
	Link string `json:"link,omitempty"`
	ID   string `json:"id,omitempty"`
}

type item struct {
	ID     string `json:"id"`
	Value  string `json:"value"`
	N      uint64 `json:"n"`
	Origin string `json:"origin"` // the device the change was made on
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
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Run watches the linked settings for a change made here, while sharing is on, and sends it on.
func (f *Feature) Run(ctx context.Context) error {
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
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

// watch looks at every linked setting once, and sends each that changed here to the devices it is linked
// with (those that have accepted).
func (f *Feature) watch(ctx context.Context) {
	c := config.Get().Share
	if !c.On {
		return
	}
	me := f.me()
	out := map[string][]item{}
	f.mu.Lock()
	defer f.mu.Unlock()
	from := map[string]config.ShareFrom{}
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
		it := item{ID: id, Value: v, N: c.From[id].N + 1, Origin: me}
		from[id] = config.ShareFrom{Device: me, At: f.now(), N: it.N}
		for _, d := range devices {
			if !c.WaitingFor(id, d) {
				out[d] = append(out[d], it)
			}
		}
	}
	if len(from) > 0 {
		// Under the lock, so a change taken from another device in between is not written over.
		_ = config.Set().Share().Update(func(s *config.Share) {
			for id, fr := range from {
				if s.From == nil {
					s.From = map[string]config.ShareFrom{}
				}
				s.From[id] = fr
			}
		})
	}
	for d, items := range out {
		safe.Go("share", func() { f.deliver(ctx, d, message{Kind: "set", Items: items}) })
	}
}

// deliver sends a message to one device, within sendWithin, and answers what it made of it.
func (f *Feature) deliver(ctx context.Context, to string, m message) map[string]string {
	body, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, sendWithin)
	defer cancel()
	raw, err := f.send(ctx, to, body)
	if err != nil {
		outcome := Unreachable
		if errors.Is(err, announce.ErrShareNotTaking) {
			outcome = NotTaking
		}
		slog.Warn("share: not delivered", "to", to, "kind", m.Kind, "outcome", outcome, "err", err)
		out := map[string]string{}
		for _, it := range m.Items {
			out[it.ID] = outcome
		}
		if m.ID != "" {
			out[m.ID] = outcome
		}
		return out
	}
	var r reply
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	for id, outcome := range r.Results {
		if outcome != Applied && outcome != Same && outcome != Linked {
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
	results := map[string]string{}
	if !config.Get().Share.On {
		for _, it := range m.Items {
			results[it.ID] = NotTaking
		}
		if m.ID != "" {
			results[m.ID] = NotTaking
		}
		return json.Marshal(reply{Results: results})
	}
	switch m.Kind {
	case "set", "copy":
		results = f.take(from, m.Items, m.Kind == "copy")
	case "link":
		results[m.ID] = f.linkFrom(from, m.Link, m.ID)
	default:
		return nil, fmt.Errorf("not a kind of message this device reads: %q", m.Kind)
	}
	return json.Marshal(reply{Results: results})
}

// take is settings from another device. A linked change is taken only from a device this one has linked
// the setting with, and only when it comes later in the setting's order than the one this device has; a
// copy is taken whenever it comes, as the latest change. Either way a setting kept here is left, and what
// is taken is passed on to the setting's other links. Where every setting came from is written once.
func (f *Feature) take(from string, items []item, copied bool) map[string]string {
	c := config.Get().Share
	results := map[string]string{}
	newFrom := map[string]config.ShareFrom{}
	forward := map[string][]item{}
	now := f.now()

	f.mu.Lock()
	for _, it := range items {
		outcome, fr, ok := f.takeOne(from, it, copied, c, now)
		results[it.ID] = outcome
		if !ok {
			continue
		}
		newFrom[it.ID] = fr
		if outcome == Applied {
			// Passed on, numbered as taken, to every other device this one has linked it with: the number
			// stops it going round, since a device that has it already answers it is older.
			fwd := item{ID: it.ID, Value: it.Value, N: fr.N, Origin: fr.Device}
			for _, d := range c.Links[it.ID] {
				if !strings.EqualFold(d, from) && !strings.EqualFold(d, fr.Device) && !c.WaitingFor(it.ID, d) {
					forward[d] = append(forward[d], fwd)
				}
			}
		}
	}
	if len(newFrom) > 0 {
		_ = config.Set().Share().Update(func(s *config.Share) {
			if s.From == nil {
				s.From = map[string]config.ShareFrom{}
			}
			for id, fr := range newFrom {
				s.From[id] = fr
			}
		})
	}
	f.mu.Unlock()

	for d, its := range forward {
		safe.Go("share", func() { f.deliver(context.Background(), d, message{Kind: "set", Items: its}) })
	}
	return results
}

// takeOne is one setting; with f.mu held. It answers the outcome, and where the setting now came from
// when that is to be written.
func (f *Feature) takeOne(from string, it item, copied bool, c config.Share, now time.Time) (string, config.ShareFrom, bool) {
	if !Shareable(it.ID) {
		return NotShared, config.ShareFrom{}, false
	}
	e, ok := entities(it.ID)
	if !ok {
		return NotHere, config.ShareFrom{}, false
	}
	if c.Kept(it.ID) {
		slog.Info("share: kept this device's own", "setting", it.ID, "from", from)
		return Kept, config.ShareFrom{}, false
	}
	have := c.From[it.ID]
	origin := it.Origin
	if origin == "" || copied {
		origin = from
	}
	n := it.N
	if copied {
		// A copy is the latest change, wherever the sender's own order stands.
		n = max(have.N, it.N) + 1
	} else {
		if !c.LinkedWith(it.ID, from) || c.WaitingFor(it.ID, from) {
			return NotLinked, config.ShareFrom{}, false
		}
		if n > have.N+maxJump {
			return BadValue, config.ShareFrom{}, false
		}
		if n < have.N || (n == have.N && strings.ToLower(origin) <= strings.ToLower(have.Device)) {
			return Older, config.ShareFrom{}, false
		}
	}
	fr := config.ShareFrom{Device: origin, At: now, N: n}
	was, _ := valueOf(e)
	if was == it.Value {
		f.last[it.ID] = was
		if n == have.N && strings.EqualFold(origin, have.Device) {
			return Same, config.ShareFrom{}, false // nothing new to write
		}
		return Same, fr, true
	}
	// What this applies is no change made here: the watch is told the new value first, so it does not
	// send it back.
	f.last[it.ID] = it.Value
	err := apply(e, it.Value)
	now2, _ := valueOf(e)
	f.last[it.ID] = now2
	if err != nil {
		return BadValue, config.ShareFrom{}, false
	}
	slog.Info("share: setting changed by another device", "setting", it.ID, "from", from, "origin", origin,
		"was", was, "now", now2, "copied", copied)
	return Applied, fr, true
}

// linkFrom is another device asking about a link. It can ask (kept as a request here until somebody
// accepts or declines it), accept one this device asked for, or take itself out of one: never link this
// device by itself.
func (f *Feature) linkFrom(from, op, id string) string {
	if !Shareable(id) {
		return NotShared
	}
	if _, ok := entities(id); !ok && op != "remove" {
		return NotHere
	}
	var outcome string
	err := config.Set().Share().Update(func(s *config.Share) {
		switch op {
		case "request":
			if s.LinkedWith(id, from) {
				config.SetWaiting(s, id, from, false)
				outcome = Linked
				return
			}
			config.AddRequest(s, id, from, f.now())
			outcome = Asked
		case "accept":
			// Only a link this device asked for: anything else is a request like any other.
			if s.LinkedWith(id, from) && s.WaitingFor(id, from) {
				config.SetWaiting(s, id, from, false)
				outcome = Linked
				return
			}
			if s.LinkedWith(id, from) {
				outcome = Linked
				return
			}
			outcome = NotLinked
		case "remove":
			config.RemoveLink(s, id, from)
			outcome = Applied
		default:
			outcome = BadValue
		}
	})
	if err != nil {
		return BadValue
	}
	slog.Info("share: link message", "setting", id, "from", from, "op", op, "outcome", outcome)
	if op == "accept" && outcome == Linked {
		// The link is in place both ways now: this device's value goes to the one that accepted, as the
		// latest change, so the two start out the same.
		safe.Go("share", func() { f.sendValue(context.Background(), id, from) })
	}
	return outcome
}

// sendValue sends this device's value for a setting to one linked device, as a new change.
func (f *Feature) sendValue(ctx context.Context, id, to string) {
	e, ok := entities(id)
	if !ok {
		return
	}
	f.mu.Lock()
	v, ok := valueOf(e)
	if !ok {
		f.mu.Unlock()
		return
	}
	me := f.me()
	var n uint64
	_ = config.Set().Share().Update(func(s *config.Share) {
		n = s.From[id].N + 1
		if s.From == nil {
			s.From = map[string]config.ShareFrom{}
		}
		s.From[id] = config.ShareFrom{Device: me, At: f.now(), N: n}
	})
	f.last[id] = v
	f.mu.Unlock()
	f.deliver(ctx, to, message{Kind: "set", Items: []item{{ID: id, Value: v, N: n, Origin: me}}})
}

// Link sets the devices a setting is linked with, from here. Devices added are linked here and asked; each
// links back once it is accepted there. Devices taken out are unlinked here and told. Answers, by device,
// what each made of it.
func (f *Feature) Link(ctx context.Context, id string, devices []string) (map[string]string, error) {
	if !Shareable(id) {
		return nil, fmt.Errorf("%s is not a setting that can be shared", id)
	}
	me := f.me()
	c := config.Get().Share
	var want []string
	for _, d := range devices {
		d = strings.TrimSpace(d)
		if d != "" && !strings.EqualFold(d, me) && !containsFold(want, d) {
			want = append(want, d)
		}
	}
	if len(want) > config.MaxLinked {
		return nil, fmt.Errorf("a setting is linked with %d devices at most", config.MaxLinked)
	}
	var added, removed []string
	for _, d := range want {
		if !c.LinkedWith(id, d) {
			added = append(added, d)
		}
	}
	for _, d := range c.Linked(id) {
		if !containsFold(want, d) {
			removed = append(removed, d)
		}
	}
	if err := config.Set().Share().Update(func(s *config.Share) {
		for _, d := range removed {
			config.RemoveLink(s, id, d)
		}
		for _, d := range added {
			config.AddLink(s, id, d)
			config.SetWaiting(s, id, d, true)
		}
	}); err != nil {
		return nil, err
	}

	results := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	tell := func(d, op string) {
		defer wg.Done()
		outcome := f.deliver(ctx, d, message{Kind: "link", Link: op, ID: id})[id]
		mu.Lock()
		results[d] = outcome
		mu.Unlock()
		if op == "request" && outcome == Linked {
			// It had this device linked already: settled both ways, and the two start out the same.
			_ = config.Set().Share().Update(func(s *config.Share) { config.SetWaiting(s, id, d, false) })
			f.sendValue(ctx, id, d)
		}
	}
	for _, d := range added {
		wg.Add(1)
		go tell(d, "request")
	}
	for _, d := range removed {
		wg.Add(1)
		go tell(d, "remove")
	}
	wg.Wait()
	slog.Info("share: links set here", "setting", id, "added", added, "removed", removed)
	return results, nil
}

// Accept accepts another device's request to link a setting: linked here, and the device is told, which
// sends its value as the latest change.
func (f *Feature) Accept(ctx context.Context, id, device string) (string, error) {
	if !Shareable(id) {
		return "", fmt.Errorf("%s is not a setting that can be shared", id)
	}
	ok := false
	if err := config.Set().Share().Update(func(s *config.Share) { ok = config.AddLink(s, id, device) }); err != nil {
		return "", err
	}
	if !ok {
		return Full, fmt.Errorf("%s is linked with %d devices already", id, config.MaxLinked)
	}
	return f.deliver(ctx, device, message{Kind: "link", Link: "accept", ID: id})[id], nil
}

// Decline declines another device's request to link a setting, and tells it.
func (f *Feature) Decline(ctx context.Context, id, device string) {
	_ = config.Set().Share().Update(func(s *config.Share) { config.RemoveLink(s, id, device) })
	f.deliver(ctx, device, message{Kind: "link", Link: "remove", ID: id})
}

// Copy sends settings to devices once, with no link: answers, by device, what each made of each setting.
func (f *Feature) Copy(ctx context.Context, ids, devices []string) map[string]map[string]string {
	me := f.me()
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
			items = append(items, item{ID: id, Value: v, N: config.Get().Share.From[id].N, Origin: me})
		}
	}
	out := map[string]map[string]string{}
	if len(items) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, d := range devices {
		if strings.EqualFold(d, me) {
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

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
