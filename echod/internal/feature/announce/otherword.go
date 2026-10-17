package announce

import (
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"
)

// otherWordFor is how long a device that refused us for the house word is still named on the setup
// page, and how long before the log says so again.
const otherWordFor = time.Hour

// otherWord is the devices that refused what this one sent them as not this house: they have another
// house word, often the same one with other capitals. Each with when it was last seen, and when the
// log last said so.
var otherWord = struct {
	sync.Mutex
	seen, warned map[string]time.Time
}{seen: map[string]time.Time{}, warned: map[string]time.Time{}}

// refusedBy notes a device's answer to something sent it: a 403 is the device saying it has another
// house word.
func refusedBy(name string, status int) {
	if status != http.StatusForbidden {
		return
	}
	now := time.Now()
	otherWord.Lock()
	defer otherWord.Unlock()
	otherWord.seen[name] = now
	if now.Sub(otherWord.warned[name]) >= otherWordFor {
		otherWord.warned[name] = now
		slog.Warn("announce: this device has another house word; check the capitals too", "device", name)
	}
}

// OtherWords is the devices seen with another house word in the last hour, by name.
func OtherWords() []string {
	now := time.Now()
	otherWord.Lock()
	defer otherWord.Unlock()
	var out []string
	for name, at := range otherWord.seen {
		if now.Sub(at) < otherWordFor {
			out = append(out, name)
		} else {
			delete(otherWord.seen, name)
		}
	}
	slices.Sort(out)
	return out
}
