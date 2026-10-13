package component

import (
	"slices"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"
)

// Options keeps the options of a select whose list changes while the device runs (the players to
// follow, the weather sources, the dashboards). Home Assistant is told a select's options once, as it
// connects, and the library reads them from the select then, with no lock of its own: so a new list
// goes into the select only between one spell of serving and the next (BetweenServes), and until then
// the device's own lists, which read Get, already offer it.
type Options struct {
	sel *esphome.Select

	mu  sync.Mutex
	now []string
	due bool // now is not in the select yet
}

// NewOptions keeps sel's options from now on; nothing else is to write sel.Options.
func NewOptions(sel *esphome.Select) *Options {
	o := &Options{sel: sel, now: sel.Options}
	BetweenServes.Listen(func(struct{}) { o.apply() })
	return o
}

// Get is the options offered now. The slice is shared: it is not to be changed.
func (o *Options) Get() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.now
}

// Set offers opts from the next connection on, and says whether they differ from those offered now.
// opts is kept as it is, so it is not to be changed afterward.
func (o *Options) Set(opts []string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if slices.Equal(opts, o.now) {
		return false
	}
	o.now, o.due = opts, true
	return true
}

// Restore puts opts in the select at once: only before anything serves (Restorer.Restore).
func (o *Options) Restore(opts []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.now, o.due = opts, false
	o.sel.Options = opts
}

// apply puts the options offered now in the select, between one spell of serving and the next.
func (o *Options) apply() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.due {
		o.sel.Options, o.due = o.now, false
	}
}
