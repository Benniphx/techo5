package setup

import (
	"strings"
	"testing"
)

// A short house word is warned of, not refused; a long one and none are not.
func TestAShortHouseWordIsWarnedOf(t *testing.T) {
	if !strings.Contains(shortWordNote("lantern"), "short") {
		t.Error("a short word is not warned of")
	}
	if shortWordNote("") != "" || shortWordNote("a long phrase for this house") != "" {
		t.Error("an empty or long word is warned of")
	}
}
