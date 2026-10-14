//go:build !dot

package display

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"

	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
)

// A picture drawn whole: a picture-elements card's, whose elements are places on it to tap, and a
// view's only picture, which fills the screen. The picture is drawn as large as fits with its own
// proportions, nothing of it cut off, so that a page drawn as one picture shows every button on it,
// each where its element says, and the screen's color around it where the two shapes differ.

// elementSide is the size of an element whose style gives it none: Home Assistant's icon and its
// padding, a little larger to be a finger's.
const elementSide = 48

// lonePicture is the picture of a view that is nothing else: one section, one picture.
func lonePicture(v dashboard.Drawn) (dashboard.Picture, bool) {
	if len(v.Sections) != 1 || len(v.Sections[0].Blocks) != 1 || v.Sections[0].Blocks[0].Picture == nil {
		return dashboard.Picture{}, false
	}
	return *v.Sections[0].Blocks[0].Picture, true
}

// wholePage draws a view's only picture over the whole of area, and keeps where its elements went.
// The page does not scroll.
func (r *paint) wholePage(p dashboard.Picture, area image.Rectangle, pal dashPal) {
	var zones []dashTile
	r.wholePicture(area, p, pal, &zones)
	r.setDash(zones, area.Dy())
}

// fit is the largest part of the frame dst with the proportions of a picture src, centered: the
// picture shown whole, with the frame's own color at its sides or above and below it.
func fit(src, dst image.Rectangle) image.Rectangle {
	sw, sh, dw, dh := src.Dx(), src.Dy(), dst.Dx(), dst.Dy()
	if sw <= 0 || sh <= 0 || dw <= 0 || dh <= 0 {
		return dst
	}
	if sw*dh > sh*dw {
		h := max(1, dw*sh/sw)
		y := dst.Min.Y + (dh-h)/2
		return image.Rect(dst.Min.X, y, dst.Max.X, y+h)
	}
	w := max(1, dh*sw/sh)
	x := dst.Min.X + (dw-w)/2
	return image.Rect(x, dst.Min.Y, x+w, dst.Max.Y)
}

// elementBox is where an element is on a picture drawn at pic: its size in percent of the picture,
// or natural pixels square when its style gives none, moved from its point by its shift.
func elementBox(e dashboard.Element, pic image.Rectangle, natural int) image.Rectangle {
	w, h := e.W*float64(pic.Dx()), e.H*float64(pic.Dy())
	if e.W <= 0 {
		w = float64(natural)
	}
	if e.H <= 0 {
		h = float64(natural)
	}
	x := float64(pic.Min.X) + e.X*float64(pic.Dx()) + e.ShiftX*w
	y := float64(pic.Min.Y) + e.Y*float64(pic.Dy()) + e.ShiftY*h
	return image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
}

// wholePicture draws a picture whole in frame, its elements over it, and keeps the places to tap:
// the elements, the last one first as it is the one on top, then the picture's own tap. Until the
// picture has arrived there is nothing to place them on, and nothing is kept.
func (r *paint) wholePicture(frame image.Rectangle, p dashboard.Picture, pal dashPal, tiles *[]dashTile) {
	fc := r.faces()
	if p.Image == nil || p.Image.Bounds().Empty() {
		msg := "Loading the picture…"
		if p.TooLarge {
			msg = "Too large to show here"
		}
		r.text(fc.sub, msg, frame.Min.X+(frame.Dx()-r.width(fc.sub, msg))/2, frame.Min.Y+frame.Dy()/2, pal.sub)
		return
	}
	pic := fit(p.Image.Bounds(), frame)
	xdraw.ApproxBiLinear.Scale(r.dst, pic, p.Image, p.Image.Bounds(), draw.Src, nil)
	boxes := make([]image.Rectangle, len(p.Elements))
	for i, e := range p.Elements {
		boxes[i] = elementBox(e, pic, r.s(elementSide))
		r.element(boxes[i], e, pic, pal)
	}
	for i := len(p.Elements) - 1; i >= 0; i-- {
		// Only what is on the picture: an element running off its edge is tapped on the part on it.
		if box := boxes[i].Intersect(pic); !box.Empty() {
			r.zone(tiles, box, dashboard.Tile{Tap: p.Elements[i].Tap, Entity: p.Elements[i].Entity})
		}
	}
	if p.Name != "" {
		band := image.Rect(pic.Min.X, pic.Max.Y-r.s(40), pic.Max.X, pic.Max.Y)
		draw.Draw(r.dst, band, image.NewUniform(color.RGBA{0, 0, 0, 140}), image.Point{}, draw.Over)
		r.text(fc.label, r.fit(fc.label, p.Name, pic.Dx()-2*r.s(cardPad)), pic.Min.X+r.s(cardPad), pic.Max.Y-r.s(12), color.RGBA{255, 255, 255, 255})
	}
	r.zone(tiles, pic, dashboard.Tile{Tap: p.Tap})
}

// element draws what an element shows, centered in its box: a state icon's icon, lit when its entity
// is on, or a state label's words. Anything else is the picture's to show.
func (r *paint) element(box image.Rectangle, e dashboard.Element, pic image.Rectangle, pal dashPal) {
	c := pal.text
	switch {
	case e.Gone:
		c = lerp(pal.text, pal.bg, 0.5)
	case e.On:
		c = pal.active
	}
	cx, cy := (box.Min.X+box.Max.X)/2, (box.Min.Y+box.Max.Y)/2
	switch {
	case e.Icon != "":
		const isz = 28
		r.mdiIcon(e.Icon, cx-r.s(isz)/2, cy-r.s(isz)/2, isz, c)
	case e.Label != "":
		fc := r.faces()
		text := r.fit(fc.label, e.Label, pic.Dx())
		r.text(fc.label, text, cx-r.width(fc.label, text)/2, cy+r.s(9), c)
	}
}
