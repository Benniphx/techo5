package home

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/locale"
)

// With the sun down at home, every condition drawn with a sun by day takes its night form, drawn with
// the moon; the rest are left alone. Night is from home's own sunrise and sunset, so a device with no
// Home Assistant gets it too, and one that does not know where home is yet is left as reported.
func TestTheSkyAtNightHasNoSun(t *testing.T) {
	k := &sunKept
	k.mu.Lock()
	lat, lon, placed, looked := k.lat, k.lon, k.placed, k.looked
	zone := time.FixedZone("CDT", -5*60*60)
	noon := time.Date(2026, 9, 16, 13, 0, 0, 0, zone)
	k.lat, k.lon, k.placed = 40.0, -100.0, true // on the plains: up about 7:25, down about 7:45
	k.looked, k.day = noon.Format("2006-01-02"), ""
	k.mu.Unlock()
	t.Cleanup(func() {
		k.mu.Lock()
		k.lat, k.lon, k.placed, k.looked, k.day = lat, lon, placed, looked, ""
		k.mu.Unlock()
	})

	f := &Feature{}
	late := time.Date(2026, 9, 16, 23, 0, 0, 0, zone)
	early := time.Date(2026, 9, 16, 1, 0, 0, 0, zone)
	for _, c := range []struct {
		cond      string
		day, dark string
	}{
		{"sunny", "sunny", "clear-night"},
		{"partlycloudy", "partlycloudy", PartlyCloudyNight},
		{"clear-night", "clear-night", "clear-night"},
		{"cloudy", "cloudy", "cloudy"},
		{"rainy", "rainy", "rainy"},
		{"lightning-rainy", "lightning-rainy", "lightning-rainy"},
		{"", "", ""},
	} {
		if got := f.SkyAt(c.cond, noon); got != c.day {
			t.Errorf("%q at one in the afternoon is %q, want %q", c.cond, got, c.day)
		}
		if got := f.SkyAt(c.cond, late); got != c.dark {
			t.Errorf("%q at eleven at night is %q, want %q", c.cond, got, c.dark)
		}
		if got := f.SkyAt(c.cond, early); got != c.dark {
			t.Errorf("%q at one in the morning is %q, want %q", c.cond, got, c.dark)
		}
	}

	k.mu.Lock()
	k.placed = false
	k.mu.Unlock()
	if got := f.SkyAt("partlycloudy", late); got != "partlycloudy" {
		t.Errorf("with home's place unknown, a partly cloudy night is %q, want it as reported", got)
	}

	// The night form is said as partly cloudy is, in every language.
	if got := ConditionWords(PartlyCloudyNight); got != "Partly cloudy" {
		t.Errorf("ConditionWords(PartlyCloudyNight) = %q", got)
	}
	for _, lang := range []string{"en", "de", "fr"} {
		if got, want := locale.Sky(PartlyCloudyNight, lang), locale.Sky("partlycloudy", lang); got != want {
			t.Errorf("in %s the night form is %q, want %q", lang, got, want)
		}
	}
}
