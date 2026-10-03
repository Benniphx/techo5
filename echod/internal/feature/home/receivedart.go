package home

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// The cover a receiver this device runs gives for its song. Spotify Connect names one with each new
// track, as a link to Spotify's own picture; AirPlay sends the picture itself. A song without one gets
// the page's stand-in.

var received struct {
	mu         sync.Mutex
	from, key  string // key is the cover's URL, or a hash of the picture sent
	art, thumb *image.RGBA
	cancel     context.CancelFunc // the fetch under way, ended when another song takes its place
	fetch      int                // which fetch is the current one; each new song's is the next
}

// coverWait is how long a cover may take to arrive before the page goes on without it.
const coverWait = 15 * time.Second

// ReceivedArt takes the cover a receiver named for its new song: a URL, or "" for none. The last song's
// picture goes at once, and the new one is fetched aside and shown when it arrives, unless the song has
// moved on by then. Only https is fetched: anything else in its place is taken as no picture.
func ReceivedArt(from, u string) {
	if !strings.HasPrefix(u, "https://") {
		u = ""
	}
	receive(from, u, func(ctx context.Context) (*image.RGBA, *image.RGBA, error) { return fetchArt(ctx, u, false) })
}

// ReceivedPicture takes the cover a receiver sent for its song as the encoded picture itself, as
// AirPlay does: empty for none. It is decoded aside and shown by ReceivedArt's rules, the same picture
// again (the next song on an album) left as it is.
func ReceivedPicture(from string, b []byte) {
	key := ""
	if len(b) > 0 {
		sum := sha256.Sum256(b)
		key = "sha256:" + hex.EncodeToString(sum[:])
	}
	receive(from, key, func(context.Context) (*image.RGBA, *image.RGBA, error) {
		return layoutArt(b, false, "cover art from "+from)
	})
}

// receive puts the cover named by key ("" for none) in place of the last song's, loading it aside with
// load.
func receive(from, key string, load func(context.Context) (*image.RGBA, *image.RGBA, error)) {
	if !hasScreen {
		return
	}
	received.mu.Lock()
	// The same cover again (the next song on an album) is left as it is, unless the last fetch of it
	// failed: that gets another go.
	same := received.from == from && received.key == key && (key == "" || received.art != nil || received.cancel != nil)
	var ctx context.Context
	var fetch int
	if !same {
		received.from, received.key, received.art, received.thumb = from, key, nil, nil
		// Skipping through songs starts a fetch for each: only the newest is worth finishing.
		if received.cancel != nil {
			received.cancel()
			received.cancel = nil
		}
		received.fetch++
		fetch = received.fetch
		if key != "" {
			ctx, received.cancel = context.WithTimeout(context.Background(), coverWait)
		}
	}
	received.mu.Unlock()
	if same {
		return
	}
	Get().Changed.Emit(struct{}{})
	if key == "" {
		return
	}
	safe.Go("receiver cover", func() {
		art, thumb, err := load(ctx)
		received.mu.Lock()
		// The same song can come round again while an older fetch for it is still out, so it is the
		// fetch that is checked, not the song.
		current := received.fetch == fetch
		if current {
			if err == nil {
				received.art, received.thumb = art, thumb
			}
			received.cancel()
			received.cancel = nil
		}
		received.mu.Unlock()
		if !current {
			// A newer song's cover is on its way; this one was called off or is not wanted.
			return
		}
		if err != nil {
			// The page's own stand-in is what a song without a picture gets, and so does this one.
			slog.Info("receiver cover", "from", from, "err", err)
			return
		}
		slog.Info("receiver cover", "from", from, "shown", art != nil)
		Get().Changed.Emit(struct{}{})
	})
}

// receivedArt is the cover for the song the receiver from is playing, nil when there is none yet.
func receivedArt(from string) (art, thumb *image.RGBA) {
	received.mu.Lock()
	defer received.mu.Unlock()
	if received.from != from {
		return nil, nil
	}
	return received.art, received.thumb
}
