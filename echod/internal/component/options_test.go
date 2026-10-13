package component

import (
	"slices"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
)

// A new list is offered at once to the device's own lists, and goes into the select only between one
// spell of serving and the next, where no connection is reading it.
func TestOptionsChangeBetweenServes(t *testing.T) {
	sel := &esphome.Select{Options: []string{"a"}}
	o := NewOptions(sel)
	if o.Set([]string{"a"}) {
		t.Error("the same list counted as a change")
	}
	if !o.Set([]string{"a", "b"}) {
		t.Error("a new list did not count as a change")
	}
	if !slices.Equal(o.Get(), []string{"a", "b"}) {
		t.Errorf("offered %q", o.Get())
	}
	if !slices.Equal(sel.Options, []string{"a"}) {
		t.Errorf("the select changed while serving: %q", sel.Options)
	}
	BetweenServes.Emit(struct{}{})
	if !slices.Equal(sel.Options, []string{"a", "b"}) {
		t.Errorf("the select between serves: %q", sel.Options)
	}

	o.Restore([]string{"c"})
	if !slices.Equal(sel.Options, []string{"c"}) || !slices.Equal(o.Get(), []string{"c"}) {
		t.Errorf("restored: select %q, offered %q", sel.Options, o.Get())
	}
	BetweenServes.Emit(struct{}{})
	if !slices.Equal(sel.Options, []string{"c"}) {
		t.Errorf("a restored list was changed between serves: %q", sel.Options)
	}
}
