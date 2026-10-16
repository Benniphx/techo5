//go:build !dot

package display

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The alarm editor's Light only row (#164) turns the draft light only, and saving keeps it; a silent
// alarm, Home Assistant's, has no such row.
func TestTheAlarmEditorSetsLightOnly(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{poke: make(chan struct{}, 1)}
	d.EditNewAlarm()
	card := alarmEditorCard(sheetView{}, *d.draft)
	found := false
	for _, r := range card.rows {
		found = found || r.id == "e.lightonly"
	}
	if !found {
		t.Fatal("no Light only row in the editor")
	}
	if !d.alarmRowTap("e.lightonly", partMain, -1) || !d.draft.alarm.LightOnly {
		t.Fatal("the row did not turn the draft light only")
	}
	d.actionTap("save")
	list := config.Get().Alarms.List
	if len(list) != 1 || !list[0].LightOnly {
		t.Fatalf("saved %+v", list)
	}

	silent := alarmEditorCard(sheetView{}, alarmDraft{alarm: config.Alarm{Silent: true}})
	for _, r := range silent.rows {
		if r.id == "e.lightonly" {
			t.Error("a silent alarm offered Light only")
		}
	}
}
