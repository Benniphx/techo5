package mute

import (
	"errors"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
)

// scriptedLine reads as each of states in turn, and then as the last one for good.
type scriptedLine struct {
	fakeLine
	states []bool
}

func (s *scriptedLine) Get() (bool, error) {
	v := s.states[0]
	if len(s.states) > 1 {
		s.states = s.states[1:]
	}
	return v, nil
}

// After a hold the switch follows the line, however the line moved: the gating driver's own set on
// a long press, a release, or a release and then the driver's set. Before, a hold that set the latch
// left the switch saying live, and the next tap set the latch again instead of letting it go (#153).
func TestFollowAfterAHold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		was     bool
		states  []bool
		settles int
		want    bool
	}{
		{"a hold the driver took as a mute is published", false, []bool{false, false, true}, 1, true},
		{"a hold that released the latch is published", true, []bool{false}, 1, false},
		{"a release and then the driver's own set are both published", true, []bool{false, false, true}, 2, true},
		{"a hold that changed nothing publishes nothing", true, []bool{true}, 0, true},
	} {
		line := &scriptedLine{states: tc.states}
		m := &Mute{sw: &esphome.Switch{}, line: line}
		m.sw.Set(tc.was)

		settles := 0
		m.follow(20*time.Millisecond, time.Millisecond, func() {
			settles++
			is, _ := line.Get()
			m.sw.Set(is)
		})

		if settles != tc.settles {
			t.Errorf("%s: settled %d times, want %d", tc.name, settles, tc.settles)
		}
		if m.sw.Get() != tc.want {
			t.Errorf("%s: switch says %v, want %v", tc.name, m.sw.Get(), tc.want)
		}
	}
}

// A tap moves the line only where nothing else has. On amazon-gating a press of half a second or
// more is a tap here and a mute in the driver; toggling after it asked for an unmute the latch
// refused, and the switch stayed live over a set latch (#153).
func TestTapMovesTheLineOnlyWhereNothingElseHas(t *testing.T) {
	for _, tc := range []struct {
		name      string
		acts      bool
		was, line bool
		want      bool
	}{
		{"a live tap on a latch nothing else moved is the daemon's", false, false, false, true},
		{"a tap the driver already took as a mute is only news", false, false, true, false},
		{"a tap where the hardware acts is only news", true, true, false, false},
	} {
		m := &Mute{sw: &esphome.Switch{}, line: &fakeLine{muted: tc.line, acts: tc.acts}}
		if got := m.mustMove(tc.was); got != tc.want {
			t.Errorf("%s: mustMove = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// oneWay refuses every request, so that nothing past the request runs.
type oneWay struct {
	fakeLine
	asked []bool
}

func (o *oneWay) Set(muted bool) error {
	o.asked = append(o.asked, muted)
	return errors.New("refused")
}

// The screen's switch asks for the other state of the line, not for a button press: on a Show a
// press is taken as already done by the hardware, so from the screen nothing moved.
func TestFlipAsksForTheOtherState(t *testing.T) {
	for _, muted := range []bool{false, true} {
		line := &oneWay{fakeLine: fakeLine{muted: muted, acts: true}}
		m := &Mute{sw: &esphome.Switch{}, line: line}
		m.sw.Set(muted)

		m.Flip()

		if len(line.asked) != 1 || line.asked[0] != !muted {
			t.Errorf("muted=%v: asked for %v, want [%v]", muted, line.asked, !muted)
		}
	}
}
