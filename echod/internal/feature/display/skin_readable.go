//go:build !dot && !spot

package display

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"

	"github.com/HuskerMinion/techo5/echod/internal/lib/picscale"
)

// What the readable skins draw behind the home clock (skinBackdrops): each faint, at the edges or low on
// the screen, so the time and the words over it keep their contrast. None over a photo.

// Prairie's picture behind the home clock (skinphotos/prairie.jpg, public domain, U.S. Fish and
// Wildlife Service, Mountain-Prairie region): the Sandhills at Crescent Lake National Wildlife Refuge
// at sunrise.
//
//go:embed skinphotos/prairie.jpg
var prairieJPEG []byte

func (r *renderer) prairiePhoto(s scene) { r.skinPhoto("prairie", prairieJPEG, s) }

// skinPhotoShade is how much of the picture's light is kept: dark enough that the words over it,
// even over the sun, keep their contrast.
const skinPhotoShade = 0.42

// skinPhotoCache is the picture last drawn, scaled to the panel and shaded, so a frame only copies it.
var skinPhotoCache struct {
	sync.Mutex
	key string
	img *image.RGBA
}

// skinPhoto fills the screen with a skin's picture, cropped to cover it and shaded; not over a photo
// of the slideshow's.
func (r *renderer) skinPhoto(name string, file []byte, s scene) {
	if s.slideshow != nil {
		return
	}
	key := fmt.Sprintf("%s %dx%d", name, r.w, r.h)
	skinPhotoCache.Lock()
	defer skinPhotoCache.Unlock()
	if skinPhotoCache.key != key {
		src, err := jpeg.Decode(bytes.NewReader(file))
		if err != nil {
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, r.w, r.h))
		picscale.CatmullRom.Scale(img, img.Rect, src, cover(src.Bounds(), img.Rect), draw.Src)
		for i := 0; i < len(img.Pix); i += 4 {
			for c := 0; c < 3; c++ {
				img.Pix[i+c] = uint8(float64(img.Pix[i+c]) * skinPhotoShade)
			}
		}
		skinPhotoCache.key, skinPhotoCache.img = key, img
	}
	draw.Draw(r.dst, image.Rect(0, 0, r.w, r.h), skinPhotoCache.img, image.Point{}, draw.Src)
}

// controlFrame is a console's thin frame round the screen, with ticks at its corners and three status
// lights at the top left, green, amber and red, only the green lit.
func (r *renderer) controlFrame(s scene) {
	if s.slideshow != nil {
		return
	}
	line := rgb(0x3b434d)
	in := r.s(8)
	for x := in; x < r.w-in; x++ {
		r.blendAt(x, in, line, 1)
		r.blendAt(x, r.h-in, line, 1)
	}
	for y := in; y < r.h-in; y++ {
		r.blendAt(in, y, line, 1)
		r.blendAt(r.w-in, y, line, 1)
	}
	tick := rgb(0x8a95a3)
	n := r.s(18)
	for _, c := range [][2]int{{in, in}, {r.w - in, in}, {in, r.h - in}, {r.w - in, r.h - in}} {
		dx, dy := 1, 1
		if c[0] > r.w/2 {
			dx = -1
		}
		if c[1] > r.h/2 {
			dy = -1
		}
		for i := 0; i < n; i++ {
			for t := 0; t < r.s(2); t++ {
				r.blendAt(c[0]+dx*i, c[1]+dy*t, tick, 1)
				r.blendAt(c[0]+dx*t, c[1]+dy*i, tick, 1)
			}
		}
	}
	y := in + r.s(14)
	for i, col := range []uint32{0x52e07c, 0xffb020, 0xff4d4d} {
		c := rgb(col)
		x := in + r.s(16) + i*r.s(16)
		if i == 0 {
			r.disc(x, y, r.s(4), c)
		} else {
			r.disc(x, y, r.s(4), lerpRGB(rgb(0x111418), c, 0.3))
		}
	}
}
