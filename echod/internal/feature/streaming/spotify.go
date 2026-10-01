package streaming

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// spotifyEvent is the program librespot runs on each player event: it writes one line, the event and
// the song, tab-separated, to a pipe the daemon reads. Tabs and newlines in what it writes are made
// spaces, so a song's name cannot end the line early.
const spotifyEvent = `#!/bin/sh
case "$PLAYER_EVENT" in
track_changed|stopped|session_disconnected) ;;
*) exit 0 ;;
esac
clean() { printf '%s' "$1" | tr '\t\n' '  '; }
artists() { printf '%s' "$ARTISTS" | awk 'NR > 1 { printf ", " } { printf "%s", $0 }' | tr '\t' ' '; }
printf '%s\t%s\t%s\t%s\n' "$PLAYER_EVENT" "$(clean "$NAME")" "$(artists)" "$(clean "$ALBUM")" > "$TECHO5_SPOTIFY_EVENTS"
`

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
	events := filepath.Join(runDir, "spotify-events")
	script := filepath.Join(runDir, "spotify-event")
	_ = os.Remove(events)
	if err := syscall.Mkfifo(events, 0o600); err != nil {
		slog.Warn("streaming: Spotify's event pipe failed; no song names", "err", err)
	} else if err := os.WriteFile(script, []byte(spotifyEvent), 0o755); err != nil {
		slog.Warn("streaming: writing Spotify's event program failed; no song names", "err", err)
	} else {
		safe.Go("spotify events", func() { readSpotifyEvents(ctx, events) })
	}
	// The login a phone hands over by Spotify Connect is kept, so the device is still the account's
	// after a restart: in the device's own state, readable by root alone.
	cache := filepath.Join(layout.StateDir, "spotify")
	_ = os.MkdirAll(cache, 0o700)
	safe.Go("spotify audio", func() { pump(ctx, home.SpotifyName, r) })
	args := []string{
		"--name", name,
		"--backend", "pipe",
		"--format", "S16",
		"--bitrate", "160",
		"--device-type", "speaker",
		"--disable-audio-cache",
		"--cache", cache,
		"--onevent", script,
		"--initial-volume", "100",
	}
	supervise(ctx, "librespot", librespotPath, args, w, "TECHO5_SPOTIFY_EVENTS="+events)
}

// readSpotifyEvents follows the event pipe, telling the media player the song. Opened for writing as
// well, so it neither waits for librespot nor sees an end between events.
func readSpotifyEvents(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("streaming: opening Spotify's event pipe failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	defer f.Close()
	followSpotifyEvents(f, func(title, artist, album string) {
		media.Get().SetReceivedTrack(home.SpotifyName, title, artist, album)
	})
}

// followSpotifyEvents reads event lines from r until it ends, calling told with the song on a new
// track, and with nothing when playing stops.
func followSpotifyEvents(r io.Reader, told func(title, artist, album string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 16<<10)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 4 {
			continue
		}
		switch f[0] {
		case "track_changed":
			told(f[1], f[2], f[3])
		case "stopped", "session_disconnected":
			told("", "", "")
		}
	}
}
