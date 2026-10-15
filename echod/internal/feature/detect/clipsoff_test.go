package detect

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// clipsOffUnit is a Detect with only Keep wake clips: a recorder that saves into a list, the switch,
// the off record in a temporary folder, and a state save that fails while *fail is true.
func clipsOffUnit(t *testing.T, on bool) (*Detect, *bool, *[]bool) {
	t.Helper()
	oldPath, oldSave, oldEvery := clipsOffPath, saveKeepClips, clipsRetryEvery
	t.Cleanup(func() { clipsOffPath, saveKeepClips, clipsRetryEvery = oldPath, oldSave, oldEvery })
	clipsOffPath = filepath.Join(t.TempDir(), "keep-clips-off")
	clipsRetryEvery = time.Hour // the background retry never fires in a test; retryClipsOffOnce is called by hand
	fail := new(bool)
	var saves []bool
	saveKeepClips = func(on bool) error {
		if *fail {
			return errors.New("disk full")
		}
		saves = append(saves, on)
		return nil
	}
	c, _ := testClips(t, on)
	d := &Detect{clips: c}
	d.keepClips = newKeepClipsEntity(d)
	return d, fail, &saves
}

// The redditor's case: turned off, the save fails. Clips stop now, and the off is on record so the
// next start does not turn them back on.
func TestAnOffThatCannotBeSavedHoldsPastARestart(t *testing.T) {
	d, fail, saves := clipsOffUnit(t, true)
	*fail = true
	d.keepClips.OnCommand(false)
	if d.clips.on.Load() {
		t.Fatal("clips are still kept after Keep wake clips was turned off")
	}
	if !clipsOffMarked() {
		t.Fatal("the off was not recorded, so the next start would read the old on")
	}
	// The next start, with the state still saying on.
	if d.keepClipsAtStart(true) {
		t.Fatal("the next start turned Keep wake clips back on")
	}
	// Once a save holds, the record goes.
	*fail = false
	if !d.retryClipsOffOnce() {
		t.Fatal("a save that works still left something to do")
	}
	if clipsOffMarked() {
		t.Fatal("the off record stayed after the state took the off")
	}
	if len(*saves) != 1 || (*saves)[0] {
		t.Fatalf("saves %v, want one off", *saves)
	}
}

// At a start with an off on record, the off is saved at once when it can be.
func TestAStartSavesTheRecordedOff(t *testing.T) {
	d, _, saves := clipsOffUnit(t, false)
	if err := markClipsOff(); err != nil {
		t.Fatal(err)
	}
	if d.keepClipsAtStart(true) {
		t.Fatal("started with clips on over a recorded off")
	}
	if clipsOffMarked() || len(*saves) != 1 || (*saves)[0] {
		t.Fatalf("record left %v, saves %v: want the off saved and the record gone", clipsOffMarked(), *saves)
	}
}

// Turned on again and saved, that is the setting: the old off record must not win at the next start,
// and the retry stops.
func TestTurningOnAgainAndSavedClearsTheRecord(t *testing.T) {
	d, fail, _ := clipsOffUnit(t, true)
	*fail = true
	d.keepClips.OnCommand(false)
	*fail = false
	d.keepClips.OnCommand(true)
	if clipsOffMarked() {
		t.Fatal("a saved on left the old off record behind")
	}
	if !d.keepClipsAtStart(true) {
		t.Fatal("the next start ignored the saved on")
	}
	if !d.retryClipsOffOnce() {
		t.Fatal("the retry would go on saving off while clips are kept again")
	}
}

// Nothing on record: the saved setting is used as it is, either way.
func TestWithoutARecordTheSavedSettingStands(t *testing.T) {
	d, _, saves := clipsOffUnit(t, false)
	if !d.keepClipsAtStart(true) || d.keepClipsAtStart(false) {
		t.Fatal("the saved setting was not used")
	}
	if len(*saves) != 0 {
		t.Fatalf("saved %v with nothing to save", *saves)
	}
}

// An on that cannot be saved stays off, as before: nothing is kept on a promise the state never took.
func TestAnOnThatCannotBeSavedStaysOff(t *testing.T) {
	d, fail, _ := clipsOffUnit(t, false)
	*fail = true
	d.keepClips.OnCommand(true)
	if d.clips.on.Load() {
		t.Fatal("clips are kept although turning them on was not saved")
	}
}
