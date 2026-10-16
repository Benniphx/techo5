// Package busy keeps a marker saying the device is in the middle of something a reboot would cut off,
// for the boot script's loops that reboot on their own: the 2nd gen Show 8's kernel update, which
// reboots into a new kernel, and its trial, which reboots to use up a try (etc/techo5/boot.sh).
//
// The marker is Path, written while the device is busy and removed when it is not. It holds the
// daemon's pid on its first line, so a marker left by a daemon that died is seen to be stale, and
// then one reason a line:
//
//	ring    an alarm or a timer is sounding
//	voice   a voice request is being heard or answered
//	call    a call or a talk through a camera
//	sound   an announcement or another sound has the speaker
//	media   music or a station is playing
//	light   the wake light is coming up before an alarm, or held at full by one that rang
//	alarm   an alarm (on the device, a followed Home Assistant helper, or a snooze) is due within
//	        AlarmAhead: a reboot then could still be in progress, or fail, when it should ring
//
// The script waits while the marker is there, but not forever: what can last for hours (media, the
// light, alarms one after another) holds it off for two hours at most, and after that only ring
// does, so a unit that is always busy, or a kernel that keeps something from ever ending, still
// takes its kernel.
package busy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/feature/alarm"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/feature/talkback"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// Path is the marker.
const Path = "/run/techo5/busy"

// AlarmAhead is how long before an alarm is due the device counts as busy.
const AlarmAhead = 30 * time.Minute

// every is how often the marker is brought up to date.
const every = 5 * time.Second

func init() {
	component.Register(component.Device, Get(), component.Order(90))
}

// Snapshot is what the device is doing, as far as a reboot is concerned.
type Snapshot struct {
	Ring, Voice, Call, Sound, Media, Light bool
	// NextAlarm is when the next alarm rings; zero for none.
	NextAlarm time.Time
}

// Reasons are the marker's lines for s at now, in a fixed order; none when a reboot would cut nothing
// off. A clock not yet set says nothing about alarms.
func Reasons(s Snapshot, now time.Time) []string {
	var r []string
	add := func(on bool, why string) {
		if on {
			r = append(r, why)
		}
	}
	add(s.Ring, "ring")
	add(s.Voice, "voice")
	add(s.Call, "call")
	add(s.Sound, "sound")
	add(s.Media, "media")
	add(s.Light, "light")
	add(now.Year() >= 2020 && !s.NextAlarm.IsZero() && !s.NextAlarm.After(now.Add(AlarmAhead)), "alarm")
	return r
}

// Marker keeps the file at its path.
type Marker struct {
	path string
	pid  int
	mu   sync.Mutex
	last string
}

// Set writes the marker for these reasons, or removes it for none. The file is rewritten only when
// what it says changes, and replaced whole, so the script never reads half of one.
func (m *Marker) Set(reasons []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(reasons) == 0 {
		m.last = ""
		err := os.Remove(m.path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	body := fmt.Sprintf("pid %d\n%s\n", m.pid, strings.Join(reasons, "\n"))
	if body == m.last {
		if _, err := os.Stat(m.path); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.path); err != nil {
		os.Remove(tmp)
		return err
	}
	m.last = body
	return nil
}

// Busy is the device's marker.
type Busy struct {
	marker *Marker
	look   func(now time.Time) Snapshot
}

var (
	once   sync.Once
	shared *Busy
)

func Get() *Busy {
	once.Do(func() { shared = &Busy{marker: &Marker{path: Path, pid: os.Getpid()}, look: look} })
	return shared
}

func (b *Busy) Name() string { return "busy" }

// look asks each part of the device what it is doing.
func look(now time.Time) Snapshot {
	playing, _ := media.Get().Playing()
	s := Snapshot{
		Ring:  ring.IsSounding(),
		Voice: voice.Get().Busy(),
		Call:  phone.Get().Busy() || talkback.Get().Busy(),
		Sound: speaker.Sound().Busy(),
		Media: playing || media.Get().ExternalPlaying(),
		Light: alarm.Get().SunriseProgress(now) > 0,
	}
	if next := alarm.Get().View(now).Next; next != nil {
		s.NextAlarm = next.At
	}
	return s
}

// Run keeps the marker, on the one device whose boot script reads it. A marker from before this start
// goes first: whatever it said is over.
func (b *Busy) Run(ctx context.Context) error {
	if !layout.AthenaMainline() {
		<-ctx.Done()
		return nil
	}
	b.marker.Set(nil)
	defer b.marker.Set(nil)
	t := time.NewTicker(every)
	defer t.Stop()
	failed := ""
	for {
		now := time.Now()
		// Said once, not every few seconds while it lasts.
		err := b.marker.Set(Reasons(b.look(now), now))
		if err != nil && err.Error() != failed {
			slog.Warn("busy marker", "err", err)
		}
		failed = ""
		if err != nil {
			failed = err.Error()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
