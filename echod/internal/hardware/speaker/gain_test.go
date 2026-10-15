package speaker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/alsa"
)

type testGainMixer struct {
	control alsa.Control
	missing bool
	fail    error
	writes  []uint32
}

func (m *testGainMixer) Find(name string) (alsa.Control, error) {
	if m.missing || name != gainControl {
		return alsa.Control{}, errors.New("control absent")
	}
	return m.control, nil
}

func (m *testGainMixer) Set(_ alsa.Control, v uint32) error {
	if m.fail != nil {
		return m.fail
	}
	m.writes = append(m.writes, v)
	return nil
}

func (m *testGainMixer) Close() error { return nil }

func gainTestMixer() *testGainMixer {
	return &testGainMixer{control: alsa.Control{Name: gainControl, Type: alsa.TypeInteger,
		Count: 1, Min: 0, Max: 158, Step: 1}}
}

func TestSpeakerGainWritesDBAndCanReset(t *testing.T) {
	m := gainTestMixer()
	for _, db := range []int{0, 5, 6, 0} {
		if err := writeSpeakerGain(m, db); err != nil {
			t.Fatal(err)
		}
		if got := m.writes[len(m.writes)-1]; got != uint32(110+db) {
			t.Fatalf("%d dB wrote %d", db, got)
		}
	}
	for _, db := range []int{-1, 7, 10, 158} {
		if err := writeSpeakerGain(m, db); err == nil {
			t.Fatalf("accepted %d dB", db)
		}
	}
	if len(m.writes) != 4 {
		t.Fatal("invalid gain reached hardware")
	}
}

func TestSpeakerGainRejectsUnsupportedControls(t *testing.T) {
	for _, change := range []func(*testGainMixer){
		func(m *testGainMixer) { m.missing = true },
		func(m *testGainMixer) { m.control.Type = alsa.TypeEnumerated },
		func(m *testGainMixer) { m.control.Count = 2 },
		func(m *testGainMixer) { m.control.Max = 115 },
		func(m *testGainMixer) { m.control.Min = 111 },
		func(m *testGainMixer) { m.control.Step = 2 },
	} {
		m := gainTestMixer()
		change(m)
		if err := writeSpeakerGain(m, 5); err == nil {
			t.Fatal("unsupported control accepted")
		}
		if len(m.writes) != 0 {
			t.Fatal("unsupported control written")
		}
	}
	m := gainTestMixer()
	m.fail = errors.New("write failed")
	if err := writeSpeakerGain(m, 5); !errors.Is(err, m.fail) {
		t.Fatalf("lost write failure: %v", err)
	}
}

func TestSpeakerGainIsQueuedBeforeStart(t *testing.T) {
	was := GainAvailable
	GainAvailable = func() bool { return true }
	t.Cleanup(func() { GainAvailable = was })
	p := &Player{}
	if err := p.SetSpeakerGain(5); err != nil {
		t.Fatal(err)
	}
	if p.gainDB != 5 {
		t.Fatal("gain was not remembered before the mixer opens")
	}
	if err := p.SetSpeakerGain(7); err == nil || p.gainDB != 5 {
		t.Fatal("bad gain changed the queued value")
	}
}

// A gain restore failure must reach the normal playback loop rather than fail the
// service and repeatedly restart it. Cancel at the first wait after mixer setup;
// an unopened mixer forces the same error branch as a failed amplifier write.
func TestSpeakerRunContinuesAfterGainRestoreFails(t *testing.T) {
	was := GainAvailable
	GainAvailable = func() bool { return true }
	t.Cleanup(func() { GainAvailable = was })
	for _, db := range []int{0, 5} {
		p := New()
		p.pb = &alsa.Playback{}
		p.gainDB = db
		if err := p.applySpeakerGain(); err == nil {
			t.Fatal("test did not force a gain restore failure")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := p.Run(ctx); err != nil {
			t.Fatalf("gain restore at %d dB stopped playback: %v", db, err)
		}
	}
}

// A board that cannot have the amplifier is a sure no without opening the mixer;
// one that may have it is answered by the mixer, and a mixer that does not open
// yet is not taken as a no for good.
func TestSpeakerGainLooksOnlyWhereTheAmplifierCanBe(t *testing.T) {
	opened := 0
	open := func(m *testGainMixer, err error) func() (gainMixerCloser, error) {
		return func() (gainMixerCloser, error) {
			opened++
			if err != nil {
				return nil, err
			}
			return m, nil
		}
	}
	if ok, sure := findGain(false, open(gainTestMixer(), nil)); ok || !sure || opened != 0 {
		t.Fatalf("no amplifier possible: ok %v, sure %v, opened %d", ok, sure, opened)
	}
	if ok, sure := findGain(true, open(gainTestMixer(), nil)); !ok || !sure {
		t.Fatalf("the amplifier's control: ok %v, sure %v", ok, sure)
	}
	missing := gainTestMixer()
	missing.missing = true
	if ok, sure := findGain(true, open(missing, nil)); ok || !sure {
		t.Fatalf("a card without the control: ok %v, sure %v", ok, sure)
	}
	if ok, sure := findGain(true, open(nil, errors.New("no card yet"))); ok || sure {
		t.Fatalf("a card not up yet: ok %v, sure %v", ok, sure)
	}
}

// The amplifier is looked for among the I2C devices by name; only a list read
// whole without it says no.
func TestTAS5805MIsLookedForOnTheI2CBus(t *testing.T) {
	bus := func(names ...string) string {
		dir := t.TempDir()
		for i, n := range names {
			d := filepath.Join(dir, "2-00"+string(rune('a'+i)))
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "name"), []byte(n+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	if !tas5805mListed(bus("max98396", "tas5805m")) {
		t.Error("the amplifier listed was not found")
	}
	if tas5805mListed(bus("max98396", "mt6323")) {
		t.Error("a bus without the amplifier said it may have it")
	}
	if !tas5805mListed(filepath.Join(t.TempDir(), "absent")) || !tas5805mListed(bus()) {
		t.Error("a list that could not be read said no")
	}
	unreadable := bus("max98396")
	if err := os.Mkdir(filepath.Join(unreadable, "i2c-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !tas5805mListed(unreadable) {
		t.Error("a device whose name could not be read was taken as not the amplifier")
	}
}

// GainAvailable keeps a sure answer and asks again after a card that did not open;
// on a build whose boards cannot have the amplifier it never opens the mixer.
func TestSpeakerGainAsksAgainUntilSure(t *testing.T) {
	wasDir, wasOpen := i2cDevices, openGainMixer
	t.Cleanup(func() {
		i2cDevices, openGainMixer = wasDir, wasOpen
		gainSeen.ok, gainSeen.sure = false, false
	})
	i2cDevices = filepath.Join(t.TempDir(), "absent") // unknown: the mixer says
	gainSeen.ok, gainSeen.sure = false, false
	opened := 0
	var openErr error = errors.New("no card yet")
	openGainMixer = func() (gainMixerCloser, error) {
		opened++
		if openErr != nil {
			return nil, openErr
		}
		return gainTestMixer(), nil
	}
	if !mayHaveTAS5805M {
		openErr = nil
		if GainAvailable() || opened != 0 {
			t.Fatalf("this build's boards cannot have the amplifier: opened the mixer %d times", opened)
		}
		return
	}
	if GainAvailable() {
		t.Fatal("available with no card")
	}
	openErr = nil
	if !GainAvailable() || !GainAvailable() || opened != 2 {
		t.Fatalf("the card once up: opened %d times, want 2", opened)
	}
}
