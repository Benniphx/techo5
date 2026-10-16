package setup

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// An alarm added or edited on the page can be light only (#164), and the box shows what was saved.
func TestAnAlarmCanBeLightOnlyFromThePage(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	add := url.Values{"kind": {"alarm"}, "at": {"06:45"}, "label": {"Morning"}, "alarm_sunrise": {"0"}, "light_only": {"yes"}}
	if p := addFromPage(form(add)); p != "" {
		t.Fatal(p)
	}
	list := config.Get().Alarms.List
	if len(list) != 1 || !list[0].LightOnly {
		t.Fatalf("added %+v", list)
	}
	w := httptest.NewRecorder()
	alarmDrawer(w, "tok", list[0])
	if !strings.Contains(w.Body.String(), `name="light_only" value="yes" style="width:auto" checked`) ||
		!strings.Contains(w.Body.String(), "light only") {
		t.Error("the alarm's drawer does not show it as light only")
	}

	edit := url.Values{"id": {list[0].ID}, "act": {"save"}, "at": {"06:45"}, "label": {"Morning"}, "alarm_sunrise": {"0"}}
	if p := editFromPage(form(edit)); p != "" {
		t.Fatal(p)
	}
	if config.Get().Alarms.List[0].LightOnly {
		t.Error("unticking the box left the alarm light only")
	}
}
