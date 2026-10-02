package streaming

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// spotifyEvent is the program librespot runs on each player event: it writes one line, tab-separated,
// to a pipe the daemon reads: the event, the song, its artists and album, its cover (the first librespot
// lists, which is the largest) and the volume the app set. Tabs and newlines in what it writes are made
// spaces, so a song's name cannot end the line early, and each field is cut short. The pipe is opened
// for reading as well as writing (1<>), which never waits: a pipe nobody reads any more, as when the
// receivers are stopped, cannot leave the program stuck.
const spotifyEvent = `#!/bin/sh
case "$PLAYER_EVENT" in
track_changed|stopped|session_connected|session_disconnected|volume_changed) ;;
*) exit 0 ;;
esac
clean() { printf '%s' "$1" | tr '\t\n' '  ' | cut -c1-300; }
artists() { printf '%s' "$ARTISTS" | awk 'NR > 1 { printf ", " } { printf "%s", $0 }' | tr '\t' ' ' | cut -c1-300; }
cover() { printf '%s' "$COVERS" | awk 'NR == 1' | tr -d '\t\r' | cut -c1-300; }
printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$PLAYER_EVENT" "$(clean "$NAME")" "$(artists)" "$(clean "$ALBUM")" "$(cover)" "$(clean "$VOLUME")" 1<>"$TECHO5_SPOTIFY_EVENTS"
`

// The Spotify app's volume is the device's volume. librespot scales what it sends by the app's slider
// whatever it is told (its "fixed" control in 0.8 still scales), so it is told to scale linearly, which
// the pump undoes exactly (spotifyGain), and the slider's level is set as the device's own. The other
// way round cannot be done: librespot takes no volume from outside, so the device's buttons do not move
// the app's slider.
//
// librespot also says the volume as a phone connects, which is only what it was left at: that one sets
// nothing, or picking the device in the app would jump the room to wherever the app last was.

// spotifyFull is librespot's top volume.
const spotifyFull = 65535

// spotifyVolume is the app's last volume, 0..spotifyFull. librespot says it as a phone connects,
// before any audio, so the full it starts at here is never what a song is undone by.
var spotifyVolume atomic.Int32

func init() { spotifyVolume.Store(spotifyFull) }

// spotifyGain undoes librespot's own scaling, so what reaches the speaker is the song at the level the
// device's volume sets. Silence is left alone: there is nothing to undo.
func spotifyGain() float64 {
	v := spotifyVolume.Load()
	if v <= 0 || v >= spotifyFull {
		return 1
	}
	return spotifyFull / float64(v)
}

// spotifyStep is the device's volume step for the app's level.
func spotifyStep(v int) int { return (v*media.VolumeSteps + spotifyFull/2) / spotifyFull }

// connectQuiet is how long after a phone connects a volume is only librespot saying where it was.
const connectQuiet = 3 * time.Second

// spotifyPort is where librespot listens for the Spotify app handing it a login: fixed, so a firewall
// can let it through (the Dot's does).
const spotifyPort = "4070"

// spotifyCache is where the login a phone hands over is kept, so the device is still the account's
// after a restart: on userdata beside the device's own state, not in it (that is root's alone, and
// librespot runs as the receivers' user), readable by that user alone. Turning Spotify Connect off
// forgets it (forgetSpotify).
func spotifyCache() string { return filepath.Join(filepath.Dir(layout.StateDir), "techo5-spotify") }

// forgetSpotify removes the kept login: a device switched off from Spotify Connect, or given away, is
// no longer the account's.
func forgetSpotify() {
	if _, err := os.Stat(spotifyCache()); err != nil {
		return
	}
	if err := os.RemoveAll(spotifyCache()); err != nil {
		slog.Warn("streaming: forgetting the Spotify login failed", "err", err)
		return
	}
	slog.Info("streaming: Spotify login forgotten")
}

// runSpotifyReceiver runs librespot under the device's name until ctx ends, playing what it sends and
// showing what it says is playing.
func runSpotifyReceiver(ctx context.Context, name string) {
	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("streaming: Spotify's audio pipe failed", "err", err)
		return
	}
	defer r.Close()
	defer w.Close()
	cred := receiverCred()
	events := filepath.Join(runDir, "spotify-events")
	script := filepath.Join(runDir, "spotify-event")
	_ = os.Remove(events)
	if err := syscall.Mkfifo(events, 0o600); err != nil {
		slog.Warn("streaming: Spotify's event pipe failed; no song names", "err", err)
	} else if err := os.WriteFile(script, []byte(spotifyEvent), 0o755); err != nil {
		slog.Warn("streaming: writing Spotify's event program failed; no song names", "err", err)
	} else {
		handTo(cred, events)
		safe.Go("spotify events", func() { readSpotifyEvents(ctx, events) })
	}
	cache := spotifyCache()
	_ = os.MkdirAll(cache, 0o700)
	_ = os.Chmod(cache, 0o700)
	handTo(cred, cache)
	safe.Go("spotify audio", func() { pump(ctx, home.SpotifyName, r, spotifyGain) })
	supervise(ctx, program{
		name: "librespot",
		path: librespotPath,
		args: []string{
			"--name", name,
			"--backend", "pipe",
			"--format", "S16",
			"--bitrate", "160",
			"--device-type", "speaker",
			"--disable-audio-cache",
			"--cache", cache,
			"--zeroconf-port", spotifyPort,
			"--onevent", script,
			"--initial-volume", "100",
			"--volume-ctrl", "linear",
		},
		stdout: w,
		env:    []string{"TECHO5_SPOTIFY_EVENTS=" + events},
		cred:   cred,
		keep:   librespotWorthLogging,
	})
}

// librespotWorthLogging keeps librespot's warnings and errors, and what it says as it fails to start or
// crashes, and drops the rest, which names the account that logged in.
func librespotWorthLogging(line string) bool {
	for _, s := range []string{" WARN ", " ERROR ", "panicked", "error:", "Usage:"} {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

// readSpotifyEvents follows the event pipe, telling the media player the song and the page its cover,
// and taking the app's volume as the device's. Opened for writing as well, so it neither waits for
// librespot nor sees an end between events.
func readSpotifyEvents(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("streaming: opening Spotify's event pipe failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	defer f.Close()
	followSpotifyEvents(f, spotifyEvents{
		track: func(title, artist, album, cover string) {
			media.Get().SetReceivedTrack(home.SpotifyName, title, artist, album)
			home.ReceivedArt(home.SpotifyName, cover)
		},
		volume: func(v int, connecting bool) {
			spotifyVolume.Store(int32(v))
			if connecting {
				slog.Info("streaming: Spotify connected", "volume", spotifyStep(v))
				return
			}
			media.Get().Set(spotifyStep(v))
		},
		now: time.Now,
	})
}

// spotifyEvents is what the event lines are turned into.
type spotifyEvents struct {
	// track is a new song, or nothing when playing stops.
	track func(title, artist, album, cover string)
	// volume is the app's volume, 0..spotifyFull; connecting when it is only librespot saying where
	// it was as a phone connected.
	volume func(v int, connecting bool)
	now    func() time.Time
}

// followSpotifyEvents reads event lines from r until it ends. A line too long to be one of the
// program's is skipped, not taken as the end.
func followSpotifyEvents(r io.Reader, on spotifyEvents) {
	br := bufio.NewReaderSize(r, 4096)
	var connected time.Time
	for {
		line, err := readLine(br, 16<<10)
		if err != nil {
			return
		}
		f := strings.Split(line, "\t")
		if len(f) < 6 {
			continue
		}
		// The program cuts each field at a byte count, which can fall inside a character.
		for i := range f {
			f[i] = strings.ToValidUTF8(f[i], "")
		}
		switch f[0] {
		case "track_changed":
			on.track(f[1], f[2], f[3], f[4])
		case "stopped", "session_disconnected":
			on.track("", "", "", "")
		case "session_connected":
			connected = on.now()
		case "volume_changed":
			v, err := strconv.Atoi(strings.TrimSpace(f[5]))
			if err != nil || v < 0 || v > spotifyFull {
				continue
			}
			connecting := !connected.IsZero() && on.now().Sub(connected) < connectQuiet
			connected = time.Time{}
			on.volume(v, connecting)
		}
	}
}

// readLine is the next line from br without its newline, or "" for one longer than most, which is read
// through and dropped.
func readLine(br *bufio.Reader, most int) (string, error) {
	var line []byte
	over := false
	for {
		part, isPrefix, err := br.ReadLine()
		if err != nil {
			return "", err
		}
		if !over {
			line = append(line, part...)
			over = len(line) > most
		}
		if !isPrefix {
			break
		}
	}
	if over {
		return "", nil
	}
	return string(line), nil
}
