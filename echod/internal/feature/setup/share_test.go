package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func postTo(f *Feature, path string, c *http.Cookie, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.serve(w, r)
	return w
}

func TestSharingFormsNeedThePage(t *testing.T) {
	f, c := in(t)
	for _, path := range []string{"/setup/share", "/setup/share-copy"} {
		if w := postTo(f, path, nil, url.Values{"on": {"1"}}); w.Code != http.StatusForbidden {
			t.Errorf("%s without being let in: %d", path, w.Code)
		}
		if w := postTo(f, path, c, url.Values{"token": {"wrong"}, "on": {"1"}}); w.Code != http.StatusForbidden {
			t.Errorf("%s from another page: %d", path, w.Code)
		}
	}
	if config.Get().Share.On {
		t.Fatal("sharing was turned on by a form that did not come from the page")
	}
}

func TestSharingTurnsOnAndKeeps(t *testing.T) {
	f, c := in(t)
	w := postTo(f, "/setup/share", c, url.Values{"token": {c.Value}, "on": {"1"}, "id": {"bass", "sendspin"},
		"keep_bass": {"1"}, "keep_sendspin": {"1"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "saved=1") {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
	}
	s := config.Get().Share
	if !s.On || !s.Kept("bass") {
		t.Fatalf("%+v", s)
	}
	if s.Kept("sendspin") {
		t.Error("a setting that is never shared was kept, as though it could be")
	}
	if body := get(f, "/setup?tab=general", c).Body.String(); !strings.Contains(body, "Shared settings") ||
		!strings.Contains(body, `name="on" value="1" checked`) {
		t.Error("General does not show sharing on")
	}

	// Off again: the switch, nothing else lost.
	postTo(f, "/setup/share", c, url.Values{"token": {c.Value}})
	if s := config.Get().Share; s.On || !s.Kept("bass") {
		t.Fatalf("turned off: %+v", s)
	}
}

func TestCopyNeedsSharingOn(t *testing.T) {
	f, c := in(t)
	w := postTo(f, "/setup/share-copy", c, url.Values{"token": {c.Value}, "copy": {"bass"}, "to": {"Office"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "problem=") {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestSharingWithoutAHouseWordSaysSo(t *testing.T) {
	f, c := in(t)
	if body := get(f, "/setup?tab=general", c).Body.String(); !strings.Contains(body, "no house word set") {
		t.Error("no word for the house, and the page does not say it cannot share")
	}
}
