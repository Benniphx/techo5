//go:build !dot

package dashboard

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg" // camera snapshots
	_ "image/png"  // pictures from /local
	"log/slog"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// Pictures on a drawn dashboard: a camera's latest snapshot, or a picture card's image. A camera is
// fetched again every few seconds while the page is up; an image, now and then.

const (
	cameraEvery  = 5 * time.Second
	pictureEvery = 10 * time.Minute
)

// wanted is one picture a dashboard shows: a camera entity, or an image's address.
type wanted struct {
	camera string
	url    string
	// wide is a picture kept as wide as the screen rather than a card: a picture-elements card's,
	// which may be the whole page.
	wide bool
}

// key is the picture as it is kept: the same image kept at a card's width and at the screen's is
// two pictures, so each is drawn from one of its own size.
func (w wanted) key() string {
	k := w.url
	if w.camera != "" {
		k = w.camera
	}
	if w.wide {
		k += " wide"
	}
	return k
}

// shot is a picture as it stands once fetched: decoded, or why it is not.
type shot struct {
	img image.Image
	// size is its own, read from its header: known for a picture too large to decode too, whose
	// elements are still placed on a frame of its shape.
	size     image.Point
	tooLarge bool
	failed   bool // it could not be fetched or read
}

// fetchPicture gets one picture, decoded.
func fetchPicture(w wanted) (shot, error) {
	var b []byte
	var err error
	if w.camera != "" {
		// Asked for at the size it is kept: a camera whose integration scales its snapshot then sends
		// a small one. Many ignore the width and send their own size, which pictureMost then bounds.
		b, err = hass.Get().Fetch("/api/camera_proxy/" + w.camera + "?width=" + strconv.Itoa(w.width()))
	} else {
		u := w.url
		if strings.HasPrefix(u, mediaSource) {
			// An image picked in the editor's media browser: Home Assistant says where it is, signed
			// for a short while, so it is asked again each time.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			var r hass.Resolved
			r, err = hass.Get().ResolveMedia(ctx, u)
			cancel()
			if err != nil {
				return shot{failed: true}, err
			}
			u = r.URL
		}
		if strings.HasPrefix(u, "/") {
			b, err = hass.Get().Fetch(u)
		} else {
			b, err = hass.Get().FetchURL(u)
		}
	}
	if err != nil {
		return shot{failed: true}, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return shot{failed: true}, err
	}
	size := image.Pt(cfg.Width, cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > w.most() {
		return shot{size: size, tooLarge: true}, errTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return shot{failed: true}, err
	}
	return shot{img: shrink(img, w.width()), size: size}, nil
}

// mediaSource begins an image picked in Home Assistant's media browser, which it resolves.
const mediaSource = "media-source://"

// most is the most pixels the picture may have to be decoded at all.
func (w wanted) most() int {
	if w.wide {
		return wideMost
	}
	return pictureMost
}

// width is as wide as the picture is kept.
func (w wanted) width() int {
	if w.wide {
		return screenWidth
	}
	return pictureWidth
}

// errTooLarge is a picture with more pixels than it may have (most), which is said on its card rather
// than left loading.
var errTooLarge = errors.New("dashboard: a picture too large to show")

const (
	// pictureMost is the most pixels a picture may have to be decoded at all: 1080p, 8 MB decoded,
	// which the device can spare for a moment. A camera's comes already scaled (fetchPicture).
	pictureMost = 1920 * 1080
	// wideMost is the most for a picture kept as wide as the screen, which may be a whole page drawn
	// as one picture, exported at a print's size: 4096 by 4096, 64 MB decoded at the very most, and
	// shrunk to the screen's width at once. Go's decoders can't scale as they decode, so this is the
	// whole picture for that moment, one picture at a time.
	wideMost = 4096 * 4096
	// pictureWidth is as wide as a picture is kept: wider than a card on any of the screens, so it
	// is drawn by scaling down a little rather than decoded and scaled from full size every frame.
	pictureWidth = 640
)

// shrink is a picture at most most wide.
func shrink(img image.Image, most int) image.Image {
	b := img.Bounds()
	if b.Dx() <= most {
		return img
	}
	h := b.Dy() * most / b.Dx()
	out := image.NewRGBA(image.Rect(0, 0, most, max(h, 1)))
	xdraw.ApproxBiLinear.Scale(out, out.Rect, img, b, xdraw.Src, nil)
	return out
}

// keepPictures fetches the pictures until ctx ends, handing each one to got as it arrives, and a
// picture too large to show, or one that would not load, to got as that.
func keepPictures(ctx context.Context, list []wanted, got func(key string, s shot)) {
	if len(list) == 0 {
		return
	}
	next := map[string]time.Time{}
	for {
		for _, w := range list {
			if time.Now().Before(next[w.key()]) {
				continue
			}
			every := pictureEvery
			if w.camera != "" {
				every = cameraEvery
			}
			next[w.key()] = time.Now().Add(every)
			sh, err := fetchPicture(w)
			if err != nil && !errors.Is(err, errTooLarge) {
				slog.Debug("dashboard picture", "which", w.key(), "err", err)
			}
			got(w.key(), sh)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
