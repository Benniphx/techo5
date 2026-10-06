//go:build !dot && !spot

package setup

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The Video section: both switches off on a new device, saved as ticked, and the allowed addresses
// counted and forgotten, never listed.
func TestTheVideoSection(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	page := func() string {
		w := httptest.NewRecorder()
		videoSection(w, "tok")
		return w.Body.String()
	}
	if p := page(); strings.Contains(p, "checked") || !strings.Contains(p, `name="what" value="video"`) {
		t.Errorf("a new device's section: %s", p)
	}
	if p := saveVideo(deckPost(url.Values{"on": {"yes"}, "dlna": {"yes"}})); p != "" {
		t.Fatal(p)
	}
	if c := config.Get().Video; !c.On || !c.DLNA {
		t.Errorf("saved %+v", c)
	}
	_ = config.Set().Video().Allow("192.168.1.30")
	_ = config.Set().Video().Allow("192.168.1.31")
	p := page()
	if strings.Count(p, "checked") != 2 || !strings.Contains(p, "Forget the 2 addresses") || strings.Contains(p, "192.168.1.30") {
		t.Errorf("with two allowed: %s", p)
	}
	if p := saveVideo(deckPost(url.Values{"on": {"yes"}, "forget": {"yes"}})); p != "" {
		t.Fatal(p)
	}
	if c := config.Get().Video; !c.On || c.DLNA || len(c.Allowed) != 0 {
		t.Errorf("after unticking DLNA and forgetting: %+v", c)
	}
}
