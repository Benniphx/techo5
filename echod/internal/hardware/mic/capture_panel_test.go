//go:build panel

package mic

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/loopback"
)

// The speaker down holds the writer still while the microphone reads on: the reference is not lined
// up again on every read (it was, about eight times a second, each one logged).
func TestNoLineUpWhileTheSpeakerIsDown(t *testing.T) {
	c := &socketCapture{}
	loopback.Played.Write(make([]int16, 320))
	c.lineUp()
	at := c.offset
	for range 200 {
		c.read += 320
		c.lineUp()
	}
	if c.offset != at {
		t.Errorf("the reference moved from %d to %d with nothing played", at, c.offset)
	}

	// Once the writer moves again, the line-up follows it.
	loopback.Played.Write(make([]int16, 320))
	for range 50 {
		c.read += 320
		loopback.Played.Write(make([]int16, 320))
		c.lineUp()
	}
	if c.offset == at {
		t.Error("the reference never caught up with the writer once it moved again")
	}
}
