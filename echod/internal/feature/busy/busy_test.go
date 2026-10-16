package busy

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)

func TestReasons(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Snapshot
		at   time.Time
		want []string
	}{
		{"idle", Snapshot{}, now, nil},
		{"idle with an alarm tomorrow", Snapshot{NextAlarm: now.Add(20 * time.Hour)}, now, nil},
		{"everything", Snapshot{Ring: true, Voice: true, Call: true, Sound: true, Media: true, Light: true, NextAlarm: now.Add(time.Minute)}, now,
			[]string{"ring", "voice", "call", "sound", "media", "light", "alarm"}},
		{"music", Snapshot{Media: true}, now, []string{"media"}},
		{"alarm in 30 minutes", Snapshot{NextAlarm: now.Add(AlarmAhead)}, now, []string{"alarm"}},
		{"alarm in 31 minutes", Snapshot{NextAlarm: now.Add(AlarmAhead + time.Minute)}, now, nil},
		{"alarm due a moment ago", Snapshot{NextAlarm: now.Add(-time.Second)}, now, []string{"alarm"}},
		{"clock not set", Snapshot{NextAlarm: time.Date(1970, 1, 1, 0, 10, 0, 0, time.UTC)}, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), nil},
	} {
		if got := Reasons(c.s, c.at); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMarker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "techo5", "busy")
	m := &Marker{path: p, pid: 1234}
	if err := m.Set(nil); err != nil {
		t.Fatalf("removing a marker that is not there: %v", err)
	}
	if err := m.Set([]string{"ring", "alarm"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "pid 1234\nring\nalarm\n" {
		t.Fatalf("marker says %q", b)
	}
	// Removed underneath it: written again even though nothing changed.
	os.Remove(p)
	if err := m.Set([]string{"ring", "alarm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("not written again: %v", err)
	}
	if err := m.Set([]string{"media"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "pid 1234\nmedia\n" {
		t.Fatalf("marker says %q", b)
	}
	if err := m.Set(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("marker still there when idle: %v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("a temporary file was left behind")
	}
}
