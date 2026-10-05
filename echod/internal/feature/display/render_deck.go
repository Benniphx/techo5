//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/draw"
)

// deckView is what the deck page shows (deck.go fills it in).
type deckView struct {
	cols, rows  int
	page, pages int
	buttons     []deckButtonView

	// connected, problem and obsSet say how OBS is: a line at the foot of the page when it isn't
	// there, so a deck of grayed-out buttons says why.
	connected bool
	problem   string
	obsSet    bool
}

type deckButtonView struct {
	empty       bool
	label, icon string
	color       string
	lit, known  bool
	pressed     bool // a press going out
	failed      bool // a press that failed
}

// Sizes, in the layout's sizes (a Show 5 in landscape).
const (
	deckMargin = 14
	deckGap    = 12
	deckFoot   = 30 // the strip for the page dots and the OBS line
	deckRadius = 18
)

// deckColor is a button's color by name; empty is the theme's accent.
func deckColor(name string) color.RGBA {
	switch name {
	case "blue":
		return color.RGBA{0x3d, 0x8b, 0xe8, 0xff}
	case "green":
		return color.RGBA{0x3f, 0xb9, 0x6a, 0xff}
	case "red":
		return danger
	case "orange":
		return color.RGBA{0xf0, 0x8a, 0x2c, 0xff}
	case "purple":
		return color.RGBA{0x9b, 0x6b, 0xe0, 0xff}
	case "gray":
		return color.RGBA{0x9a, 0x9a, 0x9a, 0xff}
	}
	return amber
}

// deckPage draws the deck: the grid of buttons, the page dots under it, and a line about OBS when
// it isn't connected.
func (r *renderer) deckPage(s scene) {
	v := s.deck
	draw.Draw(r.dst, r.dst.Rect, image.NewUniform(walnut), image.Point{}, draw.Src)
	m, gap, foot := r.s(deckMargin), r.s(deckGap), r.s(deckFoot)
	area := image.Rect(m, m, r.w-m, r.h-foot)
	cols, rows := max(v.cols, 1), max(v.rows, 1)
	cw := (area.Dx() - (cols-1)*gap) / cols
	ch := (area.Dy() - (rows-1)*gap) / rows

	var zones []image.Rectangle
	for i, b := range v.buttons {
		c, row := i%cols, i/cols
		at := image.Pt(area.Min.X+c*(cw+gap), area.Min.Y+row*(ch+gap))
		box := image.Rectangle{Min: at, Max: at.Add(image.Pt(cw, ch))}
		zones = append(zones, box)
		r.deckButton(box, b)
	}
	r.zmu.Lock()
	r.deckZones = zones
	r.zmu.Unlock()

	// The page dots, centered in the foot, when there is more than one page.
	fy := r.h - foot/2
	if v.pages > 1 {
		dot, step := r.s(8), r.s(18)
		x := r.w/2 - (v.pages-1)*step/2
		for p := range v.pages {
			col := lerp(walnut, dim, 0.6)
			if p == v.page {
				col = cream
			}
			b := image.Rect(x-dot/2, fy-dot/2, x+dot/2, fy+dot/2)
			r.roundFill(b, float64(dot)/2, col, col)
			x += step
		}
	}

	// Why the buttons are gray, at the foot's left.
	var line string
	switch {
	case !v.obsSet:
		line = "OBS isn't set up: setup page → Screen & Photos → Deck"
	case !v.connected && v.problem != "":
		line = v.problem
	case !v.connected:
		line = "Connecting to OBS…"
	}
	if line != "" {
		fc := r.faces()
		r.text(fc.sub, r.fit(fc.sub, line, r.w/2-2*m), m, fy+r.s(7), dim)
	}
}

// deckButton draws one square: its color, brighter when lit; its icon and its words. An empty square
// is a faint outline, so the grid still reads as a grid.
func (r *renderer) deckButton(b image.Rectangle, v deckButtonView) {
	rad := r.sf(deckRadius)
	if v.empty {
		r.roundFill(b, rad, lerp(walnut, surface(3), 0.5), lerp(walnut, surface(3), 0.5))
		return
	}
	col := deckColor(v.color)
	base := surface(3)
	fill := lerp(base, col, 0.22)
	text, icon := cream, col
	switch {
	case v.failed:
		fill, text, icon = lerp(base, danger, 0.75), cream, cream
	case !v.known:
		// OBS away, or something on the button that OBS doesn't have.
		fill, text, icon = lerp(base, walnut, 0.4), dim, dim
	case v.lit:
		fill, text, icon = lerp(base, col, 0.85), walnut, walnut
	}
	if v.pressed {
		fill = lerp(fill, cream, 0.25)
	}
	r.roundFill(b, rad, fill, fill)

	// The icon in the upper part, the words under it, both scaled to the button.
	unit := min(b.Dx(), b.Dy())
	isz := min(unit*42/100, b.Dy()*45/100) * r.sDenOr1() / r.sNumOr1()
	labelSize := max(min(unit*16/100, 30), 16) * r.sDenOr1() / r.sNumOr1()
	face := r.textFace(true, labelSize)
	words := r.fit(face, v.label, b.Dx()-r.s(16))
	lh := r.s(labelSize)
	content := r.s(isz) + r.s(14) + lh
	top := b.Min.Y + (b.Dy()-content)/2
	if v.icon != "" {
		r.mdiIcon(v.icon, b.Min.X+(b.Dx()-r.s(isz))/2, top, isz, icon)
	}
	r.text(face, words, b.Min.X+(b.Dx()-r.width(face, words))/2, top+r.s(isz)+r.s(14)+lh*8/10, text)
}

// sNumOr1 and sDenOr1 are the panel's scale against the layout's, 1:1 when unset: deckButton sizes
// its icon and words from the button's own pixels, and the drawing helpers scale again.
func (r *paint) sNumOr1() int { return max(r.sNum, 1) }
func (r *paint) sDenOr1() int { return max(r.sDen, 1) }

// deckHit is the button a tap at p landed on, in the deck last drawn.
func (r *paint) deckHit(p image.Point) (int, bool) {
	r.zmu.Lock()
	defer r.zmu.Unlock()
	for i, z := range r.deckZones {
		if p.In(z) {
			return i, true
		}
	}
	return 0, false
}
