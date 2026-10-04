package loopback

import (
	"os"
	"sync"
	"time"
)

// Dump is a recorder for measuring the echo path offline. While FlagPath exists, the speaker writes
// what it plays and the microphone writes what it hears, each to its own file next to the flag. It
// is checked about once a second, so turning it on or off costs nothing while it is off.
type Dump struct {
	FlagPath string

	mu      sync.Mutex
	checked time.Time
	on      bool
}

// On reports whether the flag is there, looking at most once a second.
func (d *Dump) On() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.checked) > time.Second {
		_, err := os.Stat(d.FlagPath)
		d.on, d.checked = err == nil, time.Now()
	}
	return d.on
}
