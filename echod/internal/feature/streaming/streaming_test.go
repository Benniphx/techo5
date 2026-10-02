package streaming

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

func item(typ, code, data string) string {
	return fmt.Sprintf("<item><type>%s</type><code>%s</code><length>%d</length>\n<data encoding=\"base64\">\n%s</data></item>\n",
		hex.EncodeToString([]byte(typ)), hex.EncodeToString([]byte(code)), len(data), base64.StdEncoding.EncodeToString([]byte(data)))
}

// AirPlay's metadata names the song field by field, and the end of a session clears it; what the decoder
// cannot take on the way is passed over.
func TestAirPlayMetadata(t *testing.T) {
	stream := item("ssnc", "mdst", "") + item("core", "minm", "Take It Easy") + `<?xml version="2.0"?>` + "</junk>" +
		item("core", "asar", "Eagles") + item("core", "asal", "Eagles") + item("ssnc", "mden", "") + item("ssnc", "pend", "")
	var got [][3]string
	followAirPlayMetadata(strings.NewReader(stream), func(title, artist, album string) {
		got = append(got, [3]string{title, artist, album})
	})
	if len(got) != 4 || got[2] != [3]string{"Take It Easy", "Eagles", "Eagles"} || got[3] != [3]string{} {
		t.Errorf("told %v", got)
	}
}

// librespot's event lines name a new song and its cover, and a stop clears it; anything else is let
// by, a line far too long included, without losing what comes after it.
func TestSpotifyEvents(t *testing.T) {
	lines := "track_changed\tHotel California\tEagles, Don Henley\tHotel California\thttps://i.scdn.co/image/a\t\n" +
		"half a line\n" +
		"track_changed\t" + strings.Repeat("x", 40<<10) + "\ta\tb\t\t\n" +
		"track_changed\tCaf\xc3\tEagles\tB\t\t\n" + // cut inside a character
		"stopped\t\t\t\t\t\n"
	var got [][4]string
	followSpotifyEvents(strings.NewReader(lines), spotifyEvents{
		track: func(title, artist, album, cover string) {
			got = append(got, [4]string{title, artist, album, cover})
		},
		now: time.Now,
	})
	if len(got) != 3 || got[0] != [4]string{"Hotel California", "Eagles, Don Henley", "Hotel California", "https://i.scdn.co/image/a"} ||
		got[1] != [4]string{"Caf", "Eagles", "B", ""} || got[2] != [4]string{} {
		t.Errorf("told %v", got)
	}
}

// The app's volume is passed on; the one librespot says as a phone connects is marked as only that,
// and so is nothing after it once a moment has passed. A volume that is not one is let by.
func TestSpotifyVolumeEvents(t *testing.T) {
	now := time.Unix(1000, 0)
	lines := []string{
		"volume_changed\t\t\t\t\t32768",
		"session_connected\t\t\t\t\t",
		"volume_changed\t\t\t\t\t65535", // as the phone connected
		"volume_changed\t\t\t\t\t13107",
		"session_connected\t\t\t\t\t",
		"later",
		"volume_changed\t\t\t\t\t0", // long after that connect: the app's
		"volume_changed\t\t\t\t\t70000",
		"volume_changed\t\t\t\t\tloud",
	}
	type vol struct {
		v          int
		connecting bool
	}
	var got []vol
	r, w := io.Pipe()
	go func() {
		for _, l := range lines {
			if l == "later" {
				now = now.Add(time.Minute)
				continue
			}
			_, _ = w.Write([]byte(l + "\n"))
		}
		w.Close()
	}()
	followSpotifyEvents(r, spotifyEvents{
		track:  func(string, string, string, string) {},
		volume: func(v int, connecting bool) { got = append(got, vol{v, connecting}) },
		now:    func() time.Time { return now },
	})
	want := []vol{{32768, false}, {65535, true}, {13107, false}, {0, false}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The app's slider runs the device's volume steps end to end, and the pump undoes librespot's linear
// scaling exactly.
func TestSpotifyVolumeIsTheDevicesVolume(t *testing.T) {
	for v, step := range map[int]int{0: 0, 65535: 30, 32768: 15, 2184: 1, 1000: 0} {
		if got := spotifyStep(v); got != step {
			t.Errorf("app volume %d is step %d, want %d", v, got, step)
		}
	}
	was := spotifyVolume.Load()
	t.Cleanup(func() { spotifyVolume.Store(was) })
	for v, g := range map[int32]float64{65535: 1, 0: 1, 32768: 65535.0 / 32768, 6553: 65535.0 / 6553} {
		spotifyVolume.Store(v)
		if got := spotifyGain(); got != g {
			t.Errorf("app volume %d undone by %v, want %v", v, got, g)
		}
	}
}

// Scaled samples come out whole and in step, an odd byte from the pipe waiting for its other half, and
// a sample that would pass full scale stops there.
func TestScaledReadsStayInStep(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	in := []int16{100, -200, 300, 20000, -20000}
	raw := make([]byte, 2*len(in))
	for i, v := range in {
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(v))
	}
	// The first byte arrives with what pump read before the track; the rest splits mid-sample.
	src := &pipeSource{f: r, pending: raw[:3], gain: func() float64 { return 2 }, done: make(chan struct{})}
	go func() {
		_, _ = w.Write(raw[3:5])
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write(raw[5:])
		w.Close()
	}()
	var out []byte
	buf := make([]byte, 7)
	for len(out) < len(raw) {
		n, err := src.Read(buf)
		if n%2 != 0 {
			t.Fatalf("read handed on %d bytes, half a sample", n)
		}
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	want := []int16{200, -400, 600, 32767, -32768}
	for i, v := range want {
		if 2*i+1 >= len(out) {
			t.Fatalf("only %d bytes came out", len(out))
		}
		if got := int16(binary.LittleEndian.Uint16(out[2*i:])); got != v {
			t.Errorf("sample %d: %d, want %d", i, got, v)
		}
	}
}

// The event program writes one tab-separated line per event, the artists joined and nothing in a name
// able to break the line.
func TestTheSpotifyEventProgram(t *testing.T) {
	dir := t.TempDir()
	script, out := filepath.Join(dir, "event"), filepath.Join(dir, "events")
	if err := os.WriteFile(script, []byte(spotifyEvent), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) {
		cmd := exec.Command("/bin/sh", script)
		cmd.Env = append([]string{"TECHO5_SPOTIFY_EVENTS=" + out}, env...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
	}
	run("PLAYER_EVENT=track_changed", "NAME=One\tTwo", "ARTISTS=Eagles\nDon Henley", "ALBUM=Hotel California",
		"COVERS=https://i.scdn.co/image/big\nhttps://i.scdn.co/image/small")
	b, _ := os.ReadFile(out)
	if got := string(b); got != "track_changed\tOne Two\tEagles, Don Henley\tHotel California\thttps://i.scdn.co/image/big\t\n" {
		t.Errorf("wrote %q", got)
	}
	_ = os.Remove(out)
	run("PLAYER_EVENT=volume_changed", "VOLUME=32768")
	b, _ = os.ReadFile(out)
	if got := string(b); got != "volume_changed\t\t\t\t\t32768\n" {
		t.Errorf("wrote %q", got)
	}
	_ = os.Remove(out)
	run("PLAYER_EVENT=playing")
	if _, err := os.Stat(out); err == nil {
		t.Error("an event nothing reads wrote a line")
	}
}

// A name is a libconfig string whatever was typed for it.
func TestShairportConf(t *testing.T) {
	c := shairportConf(`Den's "Desk" \ one`+"\x01", "/run/x")
	if !strings.Contains(c, `name = "Den's \"Desk\" \\ one";`) {
		t.Errorf("conf:\n%s", c)
	}
}

// A receiver's audio plays as a track when it arrives; once that track is over (quiet, or something
// else played), what the receiver goes on sending is set aside until it pauses, and then plays again.
func TestThePumpLeavesTheSpeakerToWhatWasPlayedLast(t *testing.T) {
	was, wasAside := play, setAside
	setAside = 150 * time.Millisecond
	t.Cleanup(func() { play, setAside = was, wasAside })
	var mu sync.Mutex
	var started []media.PCMSource
	play = func(name string, src media.PCMSource, rate, channels int) {
		mu.Lock()
		started = append(started, src)
		mu.Unlock()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(started) }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pump(ctx, "AirPlay", r, nil)

	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 1 })
	// The track is taken over (something else played): the sender keeps going, and is set aside.
	mu.Lock()
	started[0].Close()
	mu.Unlock()
	for i := 0; i < 5; i++ {
		w.Write(make([]byte, 4096))
		time.Sleep(50 * time.Millisecond)
	}
	if count() != 1 {
		t.Fatal("a sender still going took the speaker straight back")
	}
	// It pauses, then plays again: that is asked for.
	time.Sleep(300 * time.Millisecond)
	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 2 })
}

// A track that ended on its own, the sender gone quiet (paused on the phone), is simply over: what
// arrives next plays at once, with nothing set aside.
func TestAQuietEndIsNotSetAside(t *testing.T) {
	was, wasAside := play, setAside
	setAside = 5 * time.Second // long, so a drain would show
	t.Cleanup(func() { play, setAside = was, wasAside })
	var mu sync.Mutex
	started := 0
	play = func(name string, src media.PCMSource, rate, channels int) {
		mu.Lock()
		started++
		mu.Unlock()
		// As the player does: reads until the sender has been quiet a while, then closes.
		go func() {
			buf := make([]byte, 4096)
			for {
				_ = src.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				if _, err := src.Read(buf); err != nil {
					src.Close()
					return
				}
			}
		}()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return started }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pump(ctx, "AirPlay", r, nil)

	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 1 })
	time.Sleep(300 * time.Millisecond) // quiet: the track ends
	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 2 })
}

// A program that keeps stopping is started again, each time after a longer pause.
func TestAProgramIsStartedAgain(t *testing.T) {
	was, wasMost := restartFirst, restartMost
	restartFirst, restartMost = 20*time.Millisecond, 80*time.Millisecond
	t.Cleanup(func() { restartFirst, restartMost = was, wasMost })
	runs := filepath.Join(t.TempDir(), "runs")
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	supervise(ctx, program{name: "test", path: "/bin/sh", args: []string{"-c", "echo x >> " + runs + "; exit 1"}})
	b, _ := os.ReadFile(runs)
	if n := strings.Count(string(b), "x"); n < 3 {
		t.Errorf("started %d times in 600 ms", n)
	}
}

// The receivers start only once avahi says it is up, and when avahi stops they stop with it and start
// again once a new avahi is up: they register with it once, as they start.
func TestTheReceiversFollowAvahi(t *testing.T) {
	wasRun, wasUp, wasAir, wasSpot, wasFirst, wasLeft := runAvahi, avahiUp, runAirPlay, runSpotify, restartFirst, leftovers
	t.Cleanup(func() {
		runAvahi, avahiUp, runAirPlay, runSpotify, restartFirst, leftovers = wasRun, wasUp, wasAir, wasSpot, wasFirst, wasLeft
	})
	leftovers = func() {}
	restartFirst = 10 * time.Millisecond
	var mu sync.Mutex
	var up bool
	var avahis, airplays, spotifies, running int
	stopAvahi := make(chan struct{}, 1)
	runAvahi = func(ctx context.Context) {
		mu.Lock()
		avahis++
		first := avahis == 1
		mu.Unlock()
		if first {
			time.Sleep(400 * time.Millisecond) // slow to come up
		}
		mu.Lock()
		up = true
		mu.Unlock()
		select {
		case <-ctx.Done():
		case <-stopAvahi:
		}
		mu.Lock()
		up = false
		mu.Unlock()
	}
	avahiUp = func(context.Context) bool { mu.Lock(); defer mu.Unlock(); return up }
	receiver := func(count *int) func(context.Context, string) {
		return func(ctx context.Context, _ string) {
			mu.Lock()
			if !up {
				t.Error("a receiver started before avahi was up")
			}
			*count++
			running++
			mu.Unlock()
			<-ctx.Done()
			mu.Lock()
			running--
			mu.Unlock()
		}
	}
	runAirPlay, runSpotify = receiver(&airplays), receiver(&spotifies)
	get := func() (int, int, int, int) { mu.Lock(); defer mu.Unlock(); return avahis, airplays, spotifies, running }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runGroup(ctx, "Kitchen", true, true); close(done) }()
	waitUntil(t, func() bool { _, a, s, r := get(); return a == 1 && s == 1 && r == 2 })
	stopAvahi <- struct{}{}
	waitUntil(t, func() bool { n, a, s, r := get(); return n == 2 && a == 2 && s == 2 && r == 2 })
	cancel()
	<-done
	if _, _, _, r := get(); r != 0 {
		t.Errorf("%d receivers still running after the group ended", r)
	}
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
