//go:build !dot

package display

import (
	"image"
	"image/color"
	"math"
	"time"
)

// The Glow clock style's colors: a few soft blobs drifting slowly over the ground, worked out on a
// coarse grid (glowCell panel pixels to a point) and stretched smooth over the screen, so a frame
// costs little more than the stretch.

// glowCell is how many panel pixels apart the field's points are.
const glowCell = 8

// glowBlobs are the blobs' paths: each goes round a Lissajous figure over the screen, slowly enough
// (minutes a turn) that a frame a second looks like a drift.
var glowBlobs = [...]struct {
	periodX, periodY float64 // seconds
	phase            float64
	size             float64 // radius, as a part of the screen's height
	color            int     // which of the colors
}{
	{97, 131, 0.0, 0.42, 0},
	{151, 89, 1.7, 0.36, 1},
	{173, 113, 3.1, 0.40, 2},
	{211, 157, 4.4, 0.30, 0},
}

// glowField is the field at time now, w by h points, in ground with the three colors mixed in.
func glowField(w, h int, now time.Time, ground color.RGBA, colors [3]color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	t := float64(now.UnixMilli()) / 1000
	type blob struct{ x, y, r2 float64 }
	var blobs [len(glowBlobs)]blob
	fw, fh := float64(w), float64(h)
	for i, b := range glowBlobs {
		x := 0.5 + 0.38*math.Sin(2*math.Pi*t/b.periodX+b.phase)
		y := 0.5 + 0.36*math.Sin(2*math.Pi*t/b.periodY+b.phase*1.3)
		r := b.size * fh
		blobs[i] = blob{x * fw, y * fh, r * r}
	}
	for py := range h {
		for px := range w {
			var weight [3]float64
			total := 0.0
			for i, b := range blobs {
				dx, dy := float64(px)-b.x, float64(py)-b.y
				v := b.r2 / (dx*dx + dy*dy + b.r2*0.25)
				weight[glowBlobs[i].color] += v
				total += v
			}
			// How much of the colors shows: none far from every blob, up to most of it in one.
			k := 0.62 * smoothstep(0.35, 1.6, total)
			var c [3]float64
			for i, col := range colors {
				share := weight[i] / total
				c[0] += share * float64(col.R)
				c[1] += share * float64(col.G)
				c[2] += share * float64(col.B)
			}
			o := img.PixOffset(px, py)
			img.Pix[o+0] = uint8(float64(ground.R)*(1-k) + c[0]*k)
			img.Pix[o+1] = uint8(float64(ground.G)*(1-k) + c[1]*k)
			img.Pix[o+2] = uint8(float64(ground.B)*(1-k) + c[2]*k)
			img.Pix[o+3] = 255
		}
	}
	return img
}

// smoothstep is 0 below a, 1 above b, and an easing curve between.
func smoothstep(a, b, x float64) float64 {
	t := math.Min(math.Max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}
