//go:build !dot

package presence

import (
	"math/rand/v2"
	"testing"
)

// room is a still scene with a little sensor noise, a block of it brighter where something stands at
// x (none for x < 0), and the whole picture lit by light.
func room(r *rand.Rand, x, light int) []uint8 {
	g := make([]uint8, gridW*gridH)
	for y := range gridH {
		for c := range gridW {
			v := 80 + (c*3+y*2)%40 + r.IntN(7) - 3 + light
			if x >= 0 && c >= x && c < x+5 && y >= 6 && y < 22 {
				v += 70
			}
			g[y*gridW+c] = uint8(min(max(v, 0), 255))
		}
	}
	return g
}

func TestAStillRoomIsEmpty(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var d detector
	for i := range 40 {
		if d.step(room(r, -1, 0)) {
			t.Fatalf("frame %d: noise taken for somebody", i)
		}
	}
	// Somebody standing still in a room is not movement either, once they have stopped.
	for i := range 10 {
		if d.step(room(r, 10, 0)) && i > 3 {
			t.Fatalf("frame %d: a still figure taken for movement", i)
		}
	}
}

func TestSomebodyWalkingIsSeen(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	var d detector
	d.step(room(r, -1, 0))
	seen := false
	for i := range 6 {
		if d.step(room(r, 2+i*4, 0)) {
			seen = true
			if i < 1 {
				t.Errorf("seen at the first moving frame; two are needed")
			}
		}
	}
	if !seen {
		t.Fatal("somebody walking across was not seen")
	}
}

// A light switched on changes every cell at once: that is not somebody, and the next frame is compared
// with the room as newly lit.
func TestALightIsNotSomebody(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	var d detector
	d.step(room(r, -1, 0))
	for i := range 6 {
		light := 0
		if i >= 1 {
			light = 60
		}
		if d.step(room(r, -1, light)) {
			t.Fatalf("frame %d: the light was taken for somebody", i)
		}
	}
	// A slow change of the whole picture (the evening coming on) is not movement either.
	for i := range 20 {
		if d.step(room(r, -1, 60-i*2)) {
			t.Fatalf("frame %d: a slow fade was taken for somebody", i)
		}
	}
}
