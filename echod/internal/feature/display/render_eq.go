//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"golang.org/x/image/font"

	"github.com/HuskerMinion/techo5/echod/internal/lib/spectrum"
)

// The equalizer turn screen: a wall of LED segments moving with the voice being heard while it
// listens and with the answer while it speaks, a slow wave while it thinks, and the words beneath.
// Unlit segments stay faintly visible, as on a real panel, lit ones glow a little, and the bottom
// rows are reflected under the baseline.

const (
	eqBands = 32
	eqSegs  = 18

	// eqFrame is the frame time while the bars are moving: smooth enough to read as motion, and only
	// for the few seconds of a turn.
	eqFrame = 66 * time.Millisecond
)

// equalizerOn is whether turns are drawn as the equalizer, on a device that has it.
func equalizerOn() bool { return hasEqualizer && turnStyles[turnStyleIndex()].value != "" }

// waveOn is whether the equalizer is drawn as the wave rather than the bars.
func waveOn() bool { return turnStyles[turnStyleIndex()].value == "wave" }

// turnStyleSelect is the Home Assistant setting.
func turnStyleSelect(wake func()) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_turn_style",
			Name:     "Turn screen",
			Icon:     "mdi:equalizer",
			Category: esphome.CategoryConfig,
		},
		Options: turnStyleOptions(),
	}
	s.OnCommand = func(v string) {
		for i, t := range turnStyles {
			if t.label == v {
				setTurnStyle(s, i)
				wake()
				return
			}
		}
	}
	return s
}

// eqView is what the equalizer draws: a bar and a peak per band, 0 to 1.
type eqView struct {
	level, peak []float64
	night       bool
	quiet       bool // every bar has fallen: nothing left to animate
	wave        bool // drawn as the wave rather than the bars
}

var (
	eqGround = color.RGBA{8, 10, 16, 255}
	eqGreen  = color.RGBA{57, 211, 83, 255}
	eqYellow = color.RGBA{245, 213, 71, 255}
	eqRed    = color.RGBA{255, 77, 77, 255}
	eqDeep   = color.RGBA{110, 10, 10, 255}
	eqEmber  = color.RGBA{255, 60, 40, 255}
)

func mix(a, b color.RGBA, t float64) color.RGBA {
	t = min(max(t, 0), 1)
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// eqColor is segment k's color: green rising through yellow to red, or dim reds at night.
func eqColor(k int, night bool) color.RGBA {
	t := float64(k) / (eqSegs - 1)
	if night {
		return mix(eqDeep, eqEmber, t)
	}
	if t < 0.65 {
		return mix(eqGreen, eqYellow, t/0.65)
	}
	return mix(eqYellow, eqRed, (t-0.65)/0.35)
}

// eqThinking is the wave while an answer is being worked out: low, slow, and the same for every
// frame at the same moment.
func eqThinking(now time.Time, level, peak []float64) {
	t := float64(now.UnixMilli()%100000) / 1000
	for i := range level {
		x := float64(i) / float64(len(level))
		s := math.Sin(2*math.Pi*(1.5*x-0.25*t) + 0.8)
		level[i] = 0.16 + 0.12*s*s
		peak[i] = level[i] + 0.05
	}
}

// eqFor is the view for a turn's phase at now.
func eqFor(phase string, night bool, now time.Time) *eqView {
	v := &eqView{night: night}
	switch phase {
	case "listening":
		v.level, v.peak = spectrum.Mic.Bands(eqBands, now)
		v.quiet = spectrum.Mic.Quiet()
	case "thinking":
		v.level, v.peak = make([]float64, eqBands), make([]float64, eqBands)
		eqThinking(now, v.level, v.peak)
	default: // replying, and the answer lingering after it while the bars fall
		v.level, v.peak = spectrum.Speaker.Bands(eqBands, now)
		v.quiet = spectrum.Speaker.Quiet()
	}
	return v
}

// eqLine is one line of words under the bars.
type eqLine struct {
	face  font.Face
	text  string
	c     color.RGBA
	lineH int
}

// eqWords lays out what goes under the bars: what it is doing, what was heard, and the answer, the
// answer a size smaller when it is long.
func (r *renderer) eqWords(s scene, room int) []eqLine {
	accent, heardCol, replyCol := color.RGBA{120, 220, 140, 255}, color.RGBA{170, 178, 190, 255}, color.RGBA{240, 244, 250, 255}
	if s.eq.night {
		accent, heardCol, replyCol = color.RGBA{255, 90, 70, 255}, color.RGBA{150, 110, 105, 255}, color.RGBA{255, 140, 120, 255}
	}
	maxW := r.w - 2*r.margin
	var out []eqLine
	switch s.phase {
	case "listening":
		out = append(out, eqLine{r.small, "Listening…", accent, r.s(42)})
	case "thinking":
		out = append(out, eqLine{r.small, "Thinking…", accent, r.s(42)})
	}
	if s.heard != "" && s.phase != "listening" {
		for _, l := range r.wrap(r.small, "“"+s.heard+"”", maxW) {
			out = append(out, eqLine{r.small, l, heardCol, r.s(40)})
		}
		out[len(out)-1].lineH += r.s(10) // a breath between the question and the answer
	}
	if s.reply == "" || (s.phase != "replying" && s.phase != "lingering") {
		return out
	}
	used := 0
	for _, l := range out {
		used += l.lineH
	}
	face, lineH := r.body, r.s(52)
	lines := r.wrap(face, s.reply, maxW)
	if used+len(lines)*lineH > room {
		face, lineH = r.small, r.s(42)
		lines = r.wrap(face, s.reply, maxW)
	}
	for _, l := range lines {
		out = append(out, eqLine{face, l, replyCol, lineH})
	}
	return out
}

// equalizer draws the whole turn page in the equalizer style.
func (r *renderer) equalizer(s scene) {
	v := s.eq
	draw.Draw(r.dst, r.dst.Rect, image.NewUniform(eqGround), image.Point{}, draw.Src)

	top, bot, under, bottom, lines := r.eqLayout(s, 54)

	x0, x1 := r.s(40), r.w-r.s(40)
	cw := float64(x1-x0) / eqBands
	gap := max(2, int(cw*0.22))
	sh := float64(bot-top) / eqSegs
	sg := max(2, int(sh*0.28))
	halo := max(1, r.s(2))

	seg := func(c, k int) image.Rectangle {
		X0 := x0 + int(float64(c)*cw) + gap/2
		X1 := x0 + int(float64(c+1)*cw) - gap/2
		Y1 := bot - int(float64(k)*sh)
		Y0 := Y1 - int(sh) + sg
		return image.Rect(X0, Y0, X1, Y1)
	}
	fill := func(rc image.Rectangle, c color.RGBA) {
		draw.Draw(r.dst, rc, image.NewUniform(c), image.Point{}, draw.Src)
	}

	for c := range eqBands {
		lit := int(math.Round(v.level[c] * eqSegs))
		pk := int(math.Round(v.peak[c] * eqSegs))
		for k := range eqSegs {
			col := eqColor(k, v.night)
			rc := seg(c, k)
			switch {
			case k < lit:
				fill(rc.Inset(-halo), mix(eqGround, col, 0.28))
				fill(rc, col)
			case k == pk-1 && pk > lit:
				fill(rc.Inset(-halo), mix(eqGround, col, 0.22))
				fill(rc, mix(col, color.RGBA{255, 255, 255, 255}, 0.35))
			default:
				fill(rc, mix(eqGround, col, 0.10))
			}
		}
		// The reflection: the bottom rows mirrored under the baseline, fading out.
		for k, f := range []float64{0.20, 0.08} {
			col := eqColor(k, v.night)
			if k >= lit {
				f /= 3
			}
			rc := seg(c, 0).Add(image.Pt(0, int(float64(k+1)*sh)+r.s(4)))
			fill(rc, mix(eqGround, col, f))
		}
	}

	r.eqText(lines, bot+under, bottom)
}

// eqLayout splits the page between the picture and the words: the picture takes the top part (pct of
// the height) and gives way to a long answer down to a quarter of the screen. It returns the picture's
// top and bottom, the gap under it, the words' last baseline, and the words.
func (r *renderer) eqLayout(s scene, pct int) (top, bot, under, bottom int, lines []eqLine) {
	top, bottom = headerH+r.s(8), r.h-r.s(14)
	under = r.s(54)
	lines = r.eqWords(s, bottom-(r.h*28/100)-under)
	words := 0
	for _, l := range lines {
		words += l.lineH
	}
	bot = min(r.h*pct/100, bottom-under-words)
	bot = max(bot, r.h*28/100)
	return top, bot, under, bottom, lines
}

// eqText draws the words centered from y down; what still does not fit ends in an ellipsis.
func (r *renderer) eqText(lines []eqLine, y, bottom int) {
	for i, l := range lines {
		base := y + l.lineH - r.s(14)
		last := i+1 < len(lines) && base+lines[i+1].lineH > bottom+r.s(10)
		text := l.text
		if last {
			text += " …"
		}
		r.text(l.face, text, (r.w-r.width(l.face, text))/2, base, l.c)
		if last {
			return
		}
		y += l.lineH
	}
}
