package timezone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata" // the zones to check the rules against, on a machine without them as files too
)

// Every common zone has a rule, and the rule keeps the same clock as the zone itself, hour by hour
// across the next few years, over every daylight saving change. From now on, not before: a zone
// that changed its rules lately (British Columbia's all-year daylight time) has its old ones only in
// the database.
func TestCommonRulesMatchTheDatabase(t *testing.T) {
	if len(commonRules) != len(Common) {
		t.Errorf("%d rules for %d common zones", len(commonRules), len(Common))
	}
	start := time.Now().UTC().Truncate(time.Hour)
	for _, name := range Common {
		rule, ok := commonRules[name]
		if !ok {
			t.Errorf("%s has no rule", name)
			continue
		}
		want, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := loadRule(rule)
		if err != nil {
			t.Errorf("%s: rule %q: %v", name, rule, err)
			continue
		}
		for at := start; at.Before(start.AddDate(4, 0, 0)); at = at.Add(time.Hour) {
			_, w := at.In(want).Zone()
			_, g := at.In(got).Zone()
			if w != g {
				t.Errorf("%s at %s: rule offset %d, database %d", name, at, g, w)
				break
			}
		}
	}
}

// A unit without the zone database still takes a common zone by name: the rule goes in force and
// the name is what is saved. Any other name is refused with something that says why.
func TestACommonZoneWithoutTheDatabase(t *testing.T) {
	dir := t.TempDir()
	nameFile, linkFile, zoneinfo = filepath.Join(dir, "timezone"), filepath.Join(dir, "localtime"), filepath.Join(dir, "no-zoneinfo")
	drainRestarts()
	if HasDatabase() {
		t.Fatal("an empty directory counted as the zone database")
	}

	z := &Zone{}
	if err := z.apply("Europe/Amsterdam"); err != nil {
		t.Fatal(err)
	}
	if got := z.Current(); got != "Europe/Amsterdam" {
		t.Errorf("saved zone = %q", got)
	}
	data, err := os.ReadFile(linkFile)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocationFromTZData("file", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		when   time.Time
		offset int
	}{
		{time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), 3600},
		{time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), 2 * 3600},
	} {
		if _, offset := tc.when.In(loc).Zone(); offset != tc.offset {
			t.Errorf("%s: offset %d, want %d", tc.when, offset, tc.offset)
		}
	}
	if why := restartAsked(); why != "time zone Europe/Amsterdam" {
		t.Errorf("restart asked for %q", why)
	}

	if err := z.apply("America/Boise"); !errors.Is(err, errNoDatabase) {
		t.Errorf("a zone with no file and no rule: %v", err)
	}
	if got := z.Current(); got != "Europe/Amsterdam" {
		t.Errorf("a refused zone replaced the saved one: %q", got)
	}
}

// A rule from Home Assistant with a slash in its daylight saving part is a rule, not a name, so it is
// applied without the database.
func TestARuleWithASlashIsNoName(t *testing.T) {
	dir := t.TempDir()
	nameFile, linkFile, zoneinfo = filepath.Join(dir, "timezone"), filepath.Join(dir, "localtime"), filepath.Join(dir, "no-zoneinfo")
	drainRestarts()
	rule := "CET-1CEST,M3.5.0,M10.5.0/3"
	if err := (&Zone{}).apply(rule); err != nil {
		t.Fatal(err)
	}
	if why := restartAsked(); why != "time zone "+rule {
		t.Errorf("restart asked for %q", why)
	}
}
