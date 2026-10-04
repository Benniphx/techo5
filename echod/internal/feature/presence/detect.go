//go:build !dot

package presence

// detector tells movement from a still room in a stream of brightness grids. A cell has moved when it
// changed by more than cellStep beyond whatever the whole picture did (a cloud, the screen's own light,
// the camera's exposure catching up); a frame has movement when enough cells did; and somebody is near
// when two of the last three frames had movement, so one flicker is not a person. A frame whose whole
// picture jumped (a light switched on or off) is not compared at all: it starts the comparison again.
type detector struct {
	prev   []uint8
	recent [3]bool
	n      int
}

const (
	cellStep   = 18 // a cell's change, on 0-255, beyond the picture's own, that counts as movement
	jumpStep   = 28 // the whole picture's change that is a light, not a person
	movedShare = 40 // of 1000: the share of cells that have to move (4%)
)

// step takes the next grid and reports whether somebody is moving in front of the camera.
func (d *detector) step(cur []uint8) bool {
	if len(cur) == 0 {
		return false
	}
	prev := d.prev
	d.prev = append(d.prev[:0:0], cur...)
	if len(prev) != len(cur) {
		return false
	}
	var sum int
	for i := range cur {
		sum += int(cur[i]) - int(prev[i])
	}
	shift := sum / len(cur)
	if shift > jumpStep || shift < -jumpStep {
		d.recent = [3]bool{}
		return false
	}
	moved := 0
	for i := range cur {
		diff := int(cur[i]) - int(prev[i]) - shift
		if diff > cellStep || diff < -cellStep {
			moved++
		}
	}
	d.recent[d.n%3] = moved*1000 >= movedShare*len(cur)
	d.n++
	hits := 0
	for _, r := range d.recent {
		if r {
			hits++
		}
	}
	return hits >= 2
}
