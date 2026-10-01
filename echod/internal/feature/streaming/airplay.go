package streaming

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// runAirPlayReceiver runs shairport-sync under the device's name until ctx ends, playing what it sends
// and showing what it says is playing.
func runAirPlayReceiver(ctx context.Context, name string) {
	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("streaming: AirPlay's audio pipe failed", "err", err)
		return
	}
	defer r.Close()
	defer w.Close()
	meta := filepath.Join(runDir, "airplay-metadata")
	_ = os.Remove(meta)
	if err := syscall.Mkfifo(meta, 0o600); err != nil {
		slog.Warn("streaming: AirPlay's metadata pipe failed; no song names", "err", err)
	} else {
		safe.Go("airplay metadata", func() { readAirPlayMetadata(ctx, meta) })
	}
	conf := filepath.Join(runDir, "shairport-sync.conf")
	if err := os.WriteFile(conf, []byte(shairportConf(name, meta)), 0o644); err != nil {
		slog.Error("streaming: writing shairport-sync's configuration failed", "err", err)
		return
	}
	safe.Go("airplay audio", func() { pump(ctx, home.AirPlayName, r) })
	supervise(ctx, "shairport-sync", shairportPath, []string{"-c", conf}, w)
}

// shairportConf is shairport-sync's configuration: the name phones show, the audio to standard output
// as 16-bit stereo at 44.1 kHz, and what is playing to the metadata pipe. Its volume stays its own: the
// phone's slider scales what it sends, and the device's volume is applied on top as for anything else.
func shairportConf(name, metaPipe string) string {
	return fmt.Sprintf(`general = {
	name = %s;
	output_backend = "stdout";
	mdns_backend = "avahi";
	interpolation = "soxr";
};
sessioncontrol = {
	session_timeout = 60;
};
metadata = {
	enabled = "yes";
	include_cover_art = "no";
	pipe_name = %s;
	pipe_timeout = 5000;
};
`, confString(name), confString(metaPipe))
}

// confString is s as a libconfig string: quoted, with quotes and backslashes escaped and anything
// unprintable left out, since a device's name is whatever somebody typed.
func confString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// metaItem is one of shairport-sync's metadata items: a type and a code, each four characters written
// as hex, and data in base64.
type metaItem struct {
	Type string `xml:"type"`
	Code string `xml:"code"`
	Data string `xml:"data"`
}

// readAirPlayMetadata follows the metadata pipe, telling the media player the song as it changes. The
// pipe is opened for writing as well as reading, so it neither waits for shairport-sync nor sees an
// end each time shairport-sync restarts.
func readAirPlayMetadata(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("streaming: opening AirPlay's metadata pipe failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	defer f.Close()
	followAirPlayMetadata(f, func(title, artist, album string) {
		media.Get().SetReceivedTrack(home.AirPlayName, title, artist, album)
	})
}

// followAirPlayMetadata reads items from r until it ends, calling told with the song whenever a field of
// it changes, and with nothing when the session ends.
func followAirPlayMetadata(r io.Reader, told func(title, artist, album string)) {
	dec := xml.NewDecoder(bufio.NewReaderSize(r, 64<<10))
	dec.Strict = false
	var title, artist, album string
	for {
		var it metaItem
		if err := dec.Decode(&it); err != nil {
			return
		}
		typ, code := fourCC(it.Type), fourCC(it.Code)
		data := ""
		if d, err := base64.StdEncoding.DecodeString(strings.TrimSpace(it.Data)); err == nil && len(d) <= 1024 {
			data = string(d)
		}
		switch {
		case typ == "core" && code == "minm":
			title = data
		case typ == "core" && code == "asar":
			artist = data
		case typ == "core" && code == "asal":
			album = data
		case typ == "ssnc" && (code == "pend" || code == "disc"):
			title, artist, album = "", "", ""
		default:
			continue
		}
		told(title, artist, album)
	}
}

// fourCC is a four-character code written as eight hex digits.
func fourCC(h string) string {
	b, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil || len(b) != 4 {
		return ""
	}
	return string(b)
}
