//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"github.com/HuskerMinion/techo5/echod/internal/lib/locale"
)

// The Show's Binary, World, Agenda and Glow clock styles (clock_style.go), each in the box styledClock
// gives it, and the name a swipe that turned to a style shows for a moment.

// binaryStyle is the time in lights, a column per digit: hours, minutes and seconds, each digit's
// value counted up from the bottom light (1, 2, 4, 8). The time is written small under them, with the
// date, for anybody not counting.
func (r *renderer) binaryStyle(s scene, box image.Rectangle) {
	digits := binaryDigits(s.now)
	foot := r.s(100) // the time and the date under the lights
	room := box.Dy() - foot
	pitch := math.Min(float64(room)/4, float64(box.Dx())/7.6)
	rad := pitch * 0.36
	gap := pitch * 0.8 // between the hours, the minutes and the seconds
	x := float64(box.Min.X) + (float64(box.Dx())-(6*pitch+2*gap))/2 + pitch/2
	bottom := float64(box.Min.Y+room) - pitch/2
	ghost := lerp(walnut, amber, 0.13)
	for i, d := range digits {
		for bit := range binaryBits[i] {
			c := ghost
			if d&(1<<bit) != 0 {
				c = amber
			}
			r.fxDisc(x, bottom-float64(bit)*pitch, rad, c, 1)
		}
		x += pitch
		if i == 1 || i == 3 {
			x += gap
		}
	}
	tf := r.styleFace(true, 40)
	t := clockHM(s.now) + s.now.Format(":05")
	if suffix := clockSuffix(s.now); suffix != "" {
		t += " " + suffix
	}
	r.text(tf, t, (r.w-r.width(tf, t))/2, box.Max.Y-r.s(52), cream)
	r.styleDate(s, r.w/2, box.Max.Y-r.s(8), 0)
}

// worldStyle is the time here and in up to three other places, a row each: the place, how its day
// differs from here, and its time.
func (r *renderer) worldStyle(s scene, box image.Rectangle) {
	type row struct {
		name, day string
		at        time.Time
		here      bool
	}
	rows := []row{{name: "Here", at: s.now, here: true}}
	for _, p := range s.style.places {
		rows = append(rows, row{name: p.name, day: placeDay(s.now, p.loc), at: s.now.In(p.loc)})
	}
	h := box.Dy() / len(rows)
	size := min(84, h*100/r.s(100)*62/100)
	tf := r.styleFace(true, size)
	nf := r.styleFace(true, min(40, size/2+6))
	sf := r.styleFace(false, 28)
	for i, w := range rows {
		top := box.Min.Y + i*h
		base := top + (h+capHeight(tf))/2
		if i > 0 {
			r.fxLine(float64(box.Min.X), float64(top), float64(box.Max.X), float64(top), float64(r.s(2)), ember, 1)
		}
		t, suffix := clockHM(w.at), clockSuffix(w.at)
		sw := 0
		if suffix != "" {
			sw = r.width(r.ampm, suffix) + r.s(12)
		}
		tx := box.Max.X - r.width(tf, t) - sw
		r.text(tf, t, tx, base, cream)
		if suffix != "" {
			r.text(r.ampm, suffix, box.Max.X-sw+r.s(12), base, dateColor(amber))
		}
		nc := cream
		if w.here {
			nc = amber
		}
		room := tx - box.Min.X - r.s(24)
		name := r.clipTo(nf, w.name, room)
		day := w.day
		if w.here {
			day = locale.Weekday(s.now, screenLang()) + ", " + locale.MonthDay(s.now, screenLang()) + alarmSuffix(s)
		}
		if day == "" {
			r.text(nf, name, box.Min.X, base-(capHeight(tf)-capHeight(nf))/2, nc)
			continue
		}
		mid := top + h/2
		r.text(nf, name, box.Min.X, mid-r.s(4), nc)
		r.text(sf, r.clipTo(sf, day, room), box.Min.X, mid+capHeight(sf)+r.s(8), dim)
		if w.here {
			// Here is today: its date opens the calendar, as the date does on the other styles.
			r.setDateAt(image.Rect(box.Min.X, top, box.Min.X+room, top+h))
		}
	}
}

// agendaStyle is the time on the left and the coming events down the right: today's and tomorrow's
// that have not ended, soonest first, as many as fit.
func (r *renderer) agendaStyle(s scene, box image.Rectangle) {
	col := box.Min.X + box.Dx()*42/100
	hm := clockHM(s.now)
	tf := r.styleFace(true, 112)
	if tw := r.width(tf, hm); tw > col-box.Min.X-r.s(40) {
		tf = r.styleFace(true, 112*(col-box.Min.X-r.s(40))/tw)
	}
	base := box.Min.Y + r.s(20) + capHeight(tf)
	r.text(tf, hm, box.Min.X, base, cream)
	if suffix := clockSuffix(s.now); suffix != "" {
		r.text(r.ampm, suffix, box.Min.X, base+r.s(48), dateColor(amber))
		base += r.s(48)
	}
	day := r.styleFace(true, 34)
	left := col - r.s(30) - box.Min.X
	r.text(day, r.clipTo(day, locale.Weekday(s.now, screenLang()), left), box.Min.X, base+r.s(64), cream)
	date := locale.MonthDay(s.now, screenLang())
	r.text(r.small, r.clipTo(r.small, date, left), box.Min.X, base+r.s(108), dateColor(dim))
	r.setDateAt(image.Rect(box.Min.X, base+r.s(30), col-r.s(30), base+r.s(118)))
	if a := alarmSuffix(s); a != "" {
		r.text(r.tiny, r.clipTo(r.tiny, strings.TrimPrefix(a, "  ·  "), left), box.Min.X, base+r.s(150), amber)
	}

	r.fxLine(float64(col-r.s(20)), float64(box.Min.Y), float64(col-r.s(20)), float64(box.Max.Y), float64(r.s(2)), ember, 1)
	head := r.styleFace(true, 20)
	y := box.Min.Y + r.s(28)
	r.text(head, "TODAY", col, y, dim)
	y += r.s(48)
	if len(s.style.next) == 0 {
		r.text(r.small, "Nothing on the calendar", col, y, dim)
		return
	}
	events := slices.Clone(s.style.next)
	slices.SortStableFunc(events, func(a, b hass.Event) int { return a.Start.Compare(b.Start) })
	today := time.Date(s.now.Year(), s.now.Month(), s.now.Day(), 0, 0, 0, 0, s.now.Location())
	tomorrow := today.AddDate(0, 0, 1)
	saidTomorrow := false
	tx := col + r.s(130)
	if !events[0].Start.Before(tomorrow) {
		r.text(r.small, "Nothing else today", col, y, dim)
		y += r.s(48)
	}
	for _, e := range events {
		later := !e.Start.Before(tomorrow)
		if later && !saidTomorrow {
			if y+r.s(56) > box.Max.Y {
				return
			}
			y += r.s(10)
			r.text(head, "TOMORROW", col, y, dim)
			y += r.s(46)
			saidTomorrow = true
		}
		if y > box.Max.Y {
			return
		}
		when := "All day"
		switch {
		case !e.AllDay && !e.Start.After(s.now):
			when = "Now"
		case !e.AllDay:
			when = clockText(e.Start)
		}
		r.text(r.tiny, when, col, y, amber)
		r.text(r.small, r.clipTo(r.small, e.Summary, box.Max.X-tx), tx, y, cream)
		y += r.s(48)
	}
}

// glowStyle is the time over colors drifting slowly across the screen, like a lava lamp. Over a photo
// the colors are left out: the photo is what is behind the clock then.
func (r *renderer) glowStyle(s scene, box image.Rectangle) {
	if s.slideshow == nil {
		field := glowField(r.w/glowCell+1, r.h/glowCell+1, s.now, walnut, [3]color.RGBA{amber, ember, lerp(dim, amber, 0.3)})
		xdraw.BiLinear.Scale(r.dst, r.dst.Rect, field, field.Bounds(), xdraw.Src, nil)
	}
	r.bigStyle(s, image.Rect(box.Min.X, box.Min.Y+r.s(24), box.Max.X, box.Max.Y-r.s(60)))
	r.styleDate(s, r.w/2, box.Max.Y-r.s(8), 0)
}

// styleNameTag is the name of the style a swipe turned to, in the middle of the clock for a moment.
func (r *renderer) styleNameTag(s scene) {
	name := s.style.named
	if name == "" {
		return
	}
	f := r.styleFace(true, 30)
	w := r.width(f, name)
	pad := r.s(22)
	hgt := capHeight(f) + 2*r.s(18)
	b := image.Rect((r.w-w)/2-pad, (r.h-hgt)/2, (r.w+w)/2+pad, (r.h+hgt)/2)
	r.roundRect(b, b.Dy()/2, shift(walnut, 34))
	r.text(f, name, (r.w-w)/2, b.Max.Y-r.s(18), cream)
}
