package home

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// An event's words are kept to a size the screen can draw: the title and the place to 200 runes, the
// description to 2,000, whole runes, and a short one as it is.
func TestEventWordsAreCapped(t *testing.T) {
	long := strings.Repeat("é", 5000)
	e := capEvent(hass.Event{Summary: long, Location: long, Description: long})
	if n := utf8.RuneCountInString(e.Summary); n != titleRunes || !utf8.ValidString(e.Summary) {
		t.Errorf("title kept %d runes", n)
	}
	if n := utf8.RuneCountInString(e.Location); n != titleRunes {
		t.Errorf("place kept %d runes", n)
	}
	if n := utf8.RuneCountInString(e.Description); n != descriptionRunes {
		t.Errorf("description kept %d runes", n)
	}
	if e := capEvent(hass.Event{Summary: "Dentist"}); e.Summary != "Dentist" {
		t.Errorf("a short title became %q", e.Summary)
	}
}

// A month keeps at most monthEventsKept events, the earliest, and one that has fewer keeps them all.
func TestAMonthKeepsSoManyEvents(t *testing.T) {
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	var events []hass.Event
	for i := range monthEventsKept + 500 {
		events = append(events, hass.Event{Summary: "Every minute", Start: first.Add(time.Duration(i) * time.Minute)})
	}
	got := capMonth("2026-10", events)
	if len(got) != monthEventsKept || cap(got) != monthEventsKept {
		t.Fatalf("kept %d (room for %d); want %d", len(got), cap(got), monthEventsKept)
	}
	if !got[len(got)-1].Start.Equal(events[monthEventsKept-1].Start) {
		t.Error("not the earliest kept")
	}
	if got := capMonth("2026-11", events[:10]); len(got) != 10 {
		t.Errorf("a short month kept %d of 10", len(got))
	}
}
