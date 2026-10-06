package config

import "slices"

// Video is the Show playing videos on its screen (feature/video, docs/video.md). Everything is off on
// a new device: each switch lets something on the network put a picture and sound on it.
type Video struct {
	// On lets videos play at all: from Home Assistant's play_video action, and from DLNA when DLNA
	// is on too. Off, nothing plays, whoever asks.
	On bool `json:"on,omitempty"`

	// DLNA makes the DLNA renderer take videos as well as music, while On and the DLNA switch are on.
	DLNA bool `json:"dlna,omitempty"`

	// Allowed are the addresses whose DLNA videos the screen was told to allow: the first video from
	// any other address asks on the screen first. Home Assistant's own action never asks.
	Allowed []string `json:"allowed,omitempty"`
}

// MostAllowed bounds the remembered addresses: the oldest goes when a new one is allowed past it.
const MostAllowed = 32

// IsAllowed is whether addr's DLNA videos play without asking.
func (v Video) IsAllowed(addr string) bool { return addr != "" && slices.Contains(v.Allowed, addr) }

type VideoWriter struct{ st *Store }

func (w VideoWriter) On(v bool) error {
	return w.st.Update(func(c *Config) { c.Video.On = v })
}

func (w VideoWriter) DLNA(v bool) error {
	return w.st.Update(func(c *Config) { c.Video.DLNA = v })
}

// Allow remembers addr as one whose videos play without asking.
func (w VideoWriter) Allow(addr string) error {
	return w.st.Update(func(c *Config) {
		if addr == "" || slices.Contains(c.Video.Allowed, addr) {
			return
		}
		c.Video.Allowed = append(c.Video.Allowed, addr)
		if n := len(c.Video.Allowed); n > MostAllowed {
			c.Video.Allowed = slices.Clone(c.Video.Allowed[n-MostAllowed:])
		}
	})
}

// ForgetAllowed makes every address ask again.
func (w VideoWriter) ForgetAllowed() error {
	return w.st.Update(func(c *Config) { c.Video.Allowed = nil })
}
