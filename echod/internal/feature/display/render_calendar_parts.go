//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"slices"
	"strings"
	"time"

	"golang.org/x/image/font"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// The pieces the calendar's views share: the theme's washes and an event's fill, an icon set by its
// middle, the countdown chip, the Day, Week and Month switch, the calendars' chips, and a day's weather.

// calWash is the ground with a little light laid over it, a little dark on a light theme: the shade of
// a box on the page, a for how much (as white at that alpha would be over a dark ground).
func calWash(a int) color.RGBA {
	if dark() {
		return lerp(walnut, color.RGBA{255, 255, 255, 255}, float64(a)/255)
	}
	return lerp(walnut, color.RGBA{60, 40, 20, 255}, float64(a)/255*0.9)
}

// evFill is an event's box in its calendar's color c: the color at half on a dark theme, a pale tint
// of it on a light one, so the words over it read in the text color either way.
func evFill(c color.RGBA) color.RGBA {
	if dark() {
		return color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255}
	}
	return lerp(c, color.RGBA{255, 255, 255, 255}, 0.62)
}

// calFade is c faded toward the ground by t: a day that is over, a calendar turned off.
func calFade(c color.RGBA, t float64) color.RGBA { return lerp(c, walnut, t) }

// evSub is the second line's color on an event's box: the text color, a little toward the box.
func evSub(c color.RGBA) color.RGBA { return lerp(cream, evFill(c), 0.3) }

// timeInk is the start time of an event in the month, in its calendar's color: deeper on a light theme.
func timeInk(c color.RGBA) color.RGBA {
	if dark() {
		return c
	}
	return lerp(c, color.RGBA{0, 0, 0, 255}, 0.3)
}

// mdiAt draws an icon with its middle at cx, cy, size tall in the Show 5's pixels as mdiIcon takes it.
func (r *renderer) mdiAt(name string, cx, cy, size int, c color.RGBA) {
	px := r.s(size)
	r.mdiIcon(name, cx-px/2, cy-px/2, size, c)
}

// countChip draws a countdown chip with its right edge at right, around cy, in f: filled in the accent
// for the next event, a box of the page's shade for the rest. It says where its left edge is.
func (r *renderer) countChip(text string, right, cy int, filled bool, f font.Face) int {
	w := r.width(f, text) + r.s(20)
	h := r.s(26)
	if f != r.micro {
		h = r.s(30)
	}
	b := image.Rect(right-w, cy-h/2, right, cy-h/2+h)
	fill, ink := calWash(22), cream
	if filled {
		fill, ink = amber, walnut
	}
	r.roundRect(b, h/2, fill)
	r.text(f, text, b.Min.X+r.s(10), cy+capHeight(f)/2, ink)
	return b.Min.X
}

// calSegmented is the Day, Week and Month switch, with its right edge at right on the header line, sel
// the view shown; Week is there only while the week view is on. It says where the next button goes.
func (r *renderer) calSegmented(sel string, right int, week bool) int {
	labels := []string{"Day", "Week", "Month"}
	kinds := []calHitKind{calViewDay, calViewWeek, calMonthView}
	if !week {
		labels, kinds = []string{"Day", "Month"}, []calHitKind{calViewDay, calMonthView}
	}
	f, pad := r.tiny, r.s(16)
	total := 0
	for _, l := range labels {
		total += r.width(f, l) + 2*pad
	}
	b := image.Rect(right-total, r.margin-r.s(8), right, r.margin+r.s(40))
	r.roundRect(b, b.Dy()/2, calWash(18))
	x := b.Min.X
	for i, l := range labels {
		w := r.width(f, l) + 2*pad
		seg := image.Rect(x, b.Min.Y, x+w, b.Max.Y)
		ink := dim
		if l == sel {
			in := seg.Inset(r.s(4))
			r.roundRect(in, in.Dy()/2, calWash(46))
			ink = cream
		} else {
			r.calHitAdd(calHit{r: seg.Inset(-r.s(2)), kind: kinds[i]})
		}
		r.text(f, l, x+pad, b.Min.Y+r.s(33), ink)
		x += w
	}
	return b.Min.X - r.s(14)
}

// chipFace is the calendars' chips' words and height: small in the header line, larger on a row of
// their own.
func (r *renderer) chipFace(inLine bool) (font.Face, int) {
	if inLine {
		return r.micro, r.s(32)
	}
	return r.tiny, r.s(38)
}

// chipWidth is how wide one calendar's chip is.
func (r *renderer) chipWidth(name string, inLine bool) int {
	f, _ := r.chipFace(inLine)
	if inLine {
		return r.width(f, name) + r.s(42)
	}
	return r.width(f, name) + r.s(60)
}

// chipsWidth is how wide all the calendars' chips are together.
func (r *renderer) chipsWidth(v calendarView, inLine bool) int {
	w := 0
	for i, c := range v.order {
		if i > 0 {
			w += r.chipGap(inLine)
		}
		w += r.chipWidth(v.name(c), inLine)
	}
	return w
}

// chipGap is the room between two chips.
func (r *renderer) chipGap(inLine bool) int {
	if inLine {
		return r.s(6)
	}
	return r.s(8)
}

// calWrap breaks a title into lines no wider than w, as wrap does, and after a hyphen too, so
// "Parent-teacher night" can be "Parent-" and "teacher night".
func (r *renderer) calWrap(f font.Face, s string, w int) []string {
	var words []string
	for _, word := range strings.Fields(s) {
		for {
			i := strings.Index(word, "-")
			if i <= 0 || i == len(word)-1 {
				break
			}
			words = append(words, word[:i+1])
			word = word[i+1:]
		}
		words = append(words, word)
	}
	var lines []string
	line := ""
	for _, word := range words {
		try := word
		if line != "" && !strings.HasSuffix(line, "-") {
			try = line + " " + word
		} else if line != "" {
			try = line + word
		}
		if line != "" && r.width(f, try) > w {
			lines = append(lines, line)
			line = word
			continue
		}
		line = try
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// calChips draws a chip for each calendar from x, around cy: in its color while it is shown, an outline
// with a crossed-out eye while it is hidden. A tap on one turns it over.
func (r *renderer) calChips(v calendarView, x, cy int, inLine bool) {
	f, h := r.chipFace(inLine)
	dot, eye, gap := r.s(6), 16, r.s(30)
	if !inLine {
		dot, eye, gap = r.s(8), 20, r.s(36)
	}
	for _, cal := range v.order {
		name := v.name(cal)
		w := r.chipWidth(name, inLine)
		b := image.Rect(x, cy-h/2, x+w, cy-h/2+h)
		base := cy + capHeight(f)/2
		if slices.Contains(v.hidden, cal) {
			r.roundStroke(b, float64(h)/2, r.sf(3)/2, calFade(dim, 0.3))
			r.mdiAt("eye-off", b.Min.X+r.s(16), cy, eye, calFade(dim, 0.2))
			r.text(f, name, b.Min.X+gap, base, calFade(dim, 0.15))
		} else {
			c := v.color(cal)
			t := 0.28
			if !dark() {
				t = 0.35
			}
			r.roundRect(b, h/2, lerp(walnut, c, t))
			r.aaDisc(float64(b.Min.X+r.s(16)), float64(cy), float64(dot), c)
			r.text(f, name, b.Min.X+gap, base, cream)
		}
		r.calHitAdd(calHit{r: b.Inset(-r.s(3)), kind: calChip, cal: cal})
		x += w + r.chipGap(inLine)
	}
}

// dayWeather draws a day's forecast with its right edge at right, its words' baseline at base: the
// icon, size tall, then the words. It says where its left edge is, or right when there is none.
func (r *renderer) dayWeather(days []hass.Day, day time.Time, right, base int, f font.Face, size int, words bool) int {
	d, ok := dayForecast(days, day)
	if !ok {
		return right
	}
	text := highLow(d)
	if words {
		if c := conditionWords(d.Condition); c != "" {
			text += "  " + c
		}
	}
	x := right - r.width(f, text)
	r.text(f, text, x, base, dim)
	name, c := weatherGlyph(d.Condition)
	cx := x - r.s(size)/2 - r.s(6)
	r.mdiAt(name, cx, base-capHeight(f)/2, size, c)
	return cx - r.s(size)/2
}
