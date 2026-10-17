package config

import (
	"maps"
	"slices"
	"strings"
	"time"
)

// Share is settings shared with the other devices of the house (feature/share): whether this device takes
// part, which of its settings are linked with which devices, the ones it keeps to itself whatever the
// others do, and where each linked setting's value last came from. All of it is this device's own: a
// settings file neither carries nor changes it (KeepOwn).
type Share struct {
	On bool `json:"on,omitempty"`

	// Links is, by setting (its entity's object ID), the other devices it is kept in step with.
	Links map[string][]string `json:"links,omitempty"`

	// Keep is the settings this device keeps its own: nothing another device sends changes them.
	Keep []string `json:"keep,omitempty"`

	// From is, by setting, the device and time its value last came from (this device's own name for a
	// change made here), so the latest change wins and each device can say where a value came from.
	From map[string]ShareFrom `json:"from,omitempty"`
}

// ShareFrom is where a setting's value came from.
type ShareFrom struct {
	Device string    `json:"device"`
	At     time.Time `json:"at"`
}

func (s Share) clone() Share {
	s.Links = maps.Clone(s.Links)
	for k, v := range s.Links {
		s.Links[k] = slices.Clone(v)
	}
	s.Keep = slices.Clone(s.Keep)
	s.From = maps.Clone(s.From)
	return s
}

// Linked is the devices a setting is kept in step with.
func (s Share) Linked(id string) []string { return s.Links[id] }

// LinkedWith is whether a setting is kept in step with a device, by its name however cased.
func (s Share) LinkedWith(id, device string) bool {
	return slices.ContainsFunc(s.Links[id], func(d string) bool { return strings.EqualFold(d, device) })
}

// Kept is whether this device keeps a setting its own.
func (s Share) Kept(id string) bool { return slices.Contains(s.Keep, id) }

type ShareWriter struct{ st *Store }

func (w ShareWriter) On(v bool) error {
	return w.st.Update(func(c *Config) { c.Share.On = v })
}

// Link sets the devices a setting is kept in step with; none unlinks it.
func (w ShareWriter) Link(id string, devices []string) error {
	return w.st.Update(func(c *Config) {
		if len(devices) == 0 {
			delete(c.Share.Links, id)
			return
		}
		if c.Share.Links == nil {
			c.Share.Links = map[string][]string{}
		}
		c.Share.Links[id] = slices.Clone(devices)
	})
}

// Keep sets whether this device keeps a setting its own.
func (w ShareWriter) Keep(id string, keep bool) error {
	return w.st.Update(func(c *Config) {
		i := slices.Index(c.Share.Keep, id)
		switch {
		case keep && i < 0:
			c.Share.Keep = append(c.Share.Keep, id)
			slices.Sort(c.Share.Keep)
		case !keep && i >= 0:
			c.Share.Keep = slices.Delete(c.Share.Keep, i, i+1)
		}
	})
}

// From records where a setting's value came from.
func (w ShareWriter) From(id string, f ShareFrom) error {
	return w.st.Update(func(c *Config) {
		if c.Share.From == nil {
			c.Share.From = map[string]ShareFrom{}
		}
		c.Share.From[id] = f
	})
}
