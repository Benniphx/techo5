//go:build panel

package speaker

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// roomGain is the device's own volume on top of what is written: on a panel, Android's level.
func roomGain() float64 { return float64(curve(int(deviceStep.Load()))) }

// androidHears records what the app is told, at once rather than androidLag later, for one test.
func androidHears(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	send, lag := sendToApp, androidLag
	sendToApp = func(line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
	}
	androidLag = 0
	t.Cleanup(func() { sendToApp, androidLag = send, lag })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func last(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// Muted is silent here, over a speaker Android still has on: Android is never sent zero.
func TestMuteIsKeptInSoftware(t *testing.T) {
	heard := androidHears(t)
	p := &Player{}
	p.SetVolume(0)
	if want := fmt.Sprintf("volume %d %d", deviceFloor, VolumeSteps); last(heard()) != want {
		t.Errorf("muted, Android was told %q, want %q", last(heard()), want)
	}
	if p.Volume() != 0 {
		t.Errorf("muted, the music's gain is %v", p.Volume())
	}
	p.SetVolume(12)
	if last(heard()) != fmt.Sprintf("volume 12 %d", VolumeSteps) || p.Volume() != 1 {
		t.Errorf("at step 12 Android was told %q and the music is at %v; want step 12 at unity", last(heard()), p.Volume())
	}
}

// A ring over quiet or muted music raises Android to the ring's step while it sounds, turns the music
// down by as much in software, and puts both back when it ends.
func TestARingRaisesAndroidAndPutsItBack(t *testing.T) {
	heard := androidHears(t)
	for _, music := range []int{0, 5} {
		p := &Player{}
		p.SetVolume(music)
		p.bellStep.Store(25)
		p.ringing.Store(true)
		buf := make([]byte, period*Channels*2)
		p.fill(buf)
		if want := fmt.Sprintf("volume 25 %d", VolumeSteps); !androidTold(heard, want) {
			t.Fatalf("music at %d, ringing at 25: Android was told %q, want %q", music, last(heard()), want)
		}
		if want := gainForStep(OutputSpeaker, music); p.Volume() != want || (music > 0 && want >= 1) {
			t.Errorf("music at %d under a ring at 25: gain %v, want %v, below unity", music, p.Volume(), want)
		}
		if g := p.gainFor(25); g != 1 {
			t.Errorf("a ring at 25 with Android at 25: gain %v, want 1", g)
		}

		p.Ringing(false, 25)
		p.fill(buf)
		back := max(music, deviceFloor)
		if want := fmt.Sprintf("volume %d %d", back, VolumeSteps); !androidTold(heard, want) {
			t.Fatalf("after the ring with music at %d: Android was told %q, want %q", music, last(heard()), want)
		}
		if want := gainForStep(OutputSpeaker, music); p.Volume() != want {
			t.Errorf("after the ring the music's gain is %v, want %v", p.Volume(), want)
		}
	}
}

// A ring quieter than the music leaves Android where the music has it.
func TestAQuietRingLeavesAndroidAlone(t *testing.T) {
	heard := androidHears(t)
	p := &Player{}
	p.SetVolume(20)
	before := len(heard())
	p.bellStep.Store(8)
	p.ringing.Store(true)
	p.fill(make([]byte, period*Channels*2))
	time.Sleep(20 * time.Millisecond)
	if got := heard()[before:]; len(got) != 0 {
		t.Errorf("a ring at 8 under music at 20 told Android %q", got)
	}
	p.Ringing(false, 8)
	p.fill(make([]byte, period*Channels*2))
}

// A level change made late is not sent over a newer one.
func TestALateLevelIsNotSentOverANewerOne(t *testing.T) {
	heard := androidHears(t)
	androidLag = 50 * time.Millisecond
	tellAndroid(25, androidLag)
	tellAndroid(7, 0)
	time.Sleep(100 * time.Millisecond)
	if got := last(heard()); got != fmt.Sprintf("volume 7 %d", VolumeSteps) {
		t.Errorf("Android was last told %q, want the newer step 7", got)
	}
}

// androidTold is whether the last line Android was told becomes want within a moment (a raise or a
// release goes out on a timer, even with no lag).
func androidTold(heard func() []string, want string) bool {
	for range 50 {
		if last(heard()) == want {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// ringOver starts a ring at 25 over music at 3 and waits for its raise to reach Android.
func ringOver(t *testing.T, heard func() []string) (*Player, []byte) {
	t.Helper()
	p := &Player{}
	p.SetVolume(3)
	p.bellStep.Store(25)
	p.ringing.Store(true)
	buf := make([]byte, period*Channels*2)
	p.fill(buf)
	raise := fmt.Sprintf("volume 25 %d", VolumeSteps)
	for i := 0; i < 100 && last(heard()) != raise; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if last(heard()) != raise {
		t.Fatalf("ringing at 25, Android was told %q, want %q", last(heard()), raise)
	}
	return p, buf
}

// A ring's raise reaches Android androidLag after the samples scaled for it, but the drop when it ends
// goes at once: the music is back at its own gain from that period on, and over a level still raised
// for the ring it would be played out loud.
func TestARingEndReachesAndroidAtOnce(t *testing.T) {
	heard := androidHears(t)
	androidLag = 60 * time.Millisecond
	p, buf := ringOver(t, heard)
	p.Ringing(false, 25)
	p.fill(buf)
	if want := fmt.Sprintf("volume 3 %d", VolumeSteps); last(heard()) != want {
		t.Errorf("as the ring ended Android was told %q, want %q at once", last(heard()), want)
	}
}
