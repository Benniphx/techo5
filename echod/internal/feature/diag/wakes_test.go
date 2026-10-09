package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bundle's wake word section is every detection and near miss in the whole log, the last of them
// when there are more, and nothing else from the log around them.
func TestTheWakesAreTheLogsWakeLines(t *testing.T) {
	var log []string
	for i := range 150 {
		log = append(log, "I [1.00] playback underrun times=1")
		if i%2 == 0 {
			log = append(log, "I [2.00] wake detected slot=1 id=alexa peak=0.97 crossing=0.9 cutoff=0.85 dropped=0")
		} else {
			log = append(log, "I [3.00] wake near miss slot=1 id=alexa peak=0.6 cutoff=0.85 dropped=0")
		}
	}
	log[len(log)-1] = "I [9.99] wake detected slot=1 id=alexa peak=0.99 the last one\r"
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte(strings.Join(log, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := strings.Split(wakes(path, 100), "\n")
	if len(got) != 100 {
		t.Fatalf("%d lines, want 100", len(got))
	}
	for _, line := range got {
		if !strings.Contains(line, "wake detected") && !strings.Contains(line, "wake near miss") {
			t.Fatalf("not a wake line: %q", line)
		}
	}
	if last := got[len(got)-1]; last != "I [9.99] wake detected slot=1 id=alexa peak=0.99 the last one" {
		t.Errorf("the last line is %q", last)
	}
	if wakes(filepath.Join(t.TempDir(), "none.log"), 100) != "" {
		t.Error("a missing log gave wake lines")
	}
}
