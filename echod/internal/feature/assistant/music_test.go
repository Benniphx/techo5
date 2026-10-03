package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/musicassistant"
)

func item(uri, name, kind string) musicassistant.Item {
	return musicassistant.Item{URI: uri, Name: name, MediaType: kind}
}

// What was said by name wins, artists first; then the kind asked for; then an artist over a track.
func TestChooseMusic(t *testing.T) {
	r := musicassistant.Results{
		Artists: []musicassistant.Item{item("a:1", "Eagles", "artist")},
		Tracks:  []musicassistant.Item{item("t:1", "Hotel California", "track"), item("t:2", "Eagles Fly", "track")},
		Albums:  []musicassistant.Item{item("b:1", "Hotel California", "album")},
	}
	for _, c := range []struct{ query, kind, want string }{
		{"eagles", "", "a:1"},
		{"Hotel California", "", "t:1"},      // a track and an album share the name: the track, before albums
		{"Hotel California", "album", "b:1"}, // unless the album was asked for
		{"something else", "", "a:1"},        // nothing by name: the first artist
		{"something else", "track", "t:1"},
	} {
		got, ok := chooseMusic(r, c.query, c.kind)
		if !ok || got.URI != c.want {
			t.Errorf("%q as %q chose %q, want %q", c.query, c.kind, got.URI, c.want)
		}
	}
	if _, ok := chooseMusic(musicassistant.Results{}, "x", ""); ok {
		t.Error("nothing found chose something")
	}
}

// The service searched first: the one named aloud when the server has it, else the setting's, else
// none; and a server that will not list its services still gets the common names and the setting.
func TestMusicSource(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	forbid := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if forbid {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`[{"instance_id":"ytmusic--a1","domain":"ytmusic","name":"YouTube Music","available":true},
			{"instance_id":"spotify--b2","domain":"spotify","name":"Spotify","available":true}]`))
	}))
	defer srv.Close()
	c := musicassistant.Client{URL: srv.URL, Token: "tok"}
	ctx := context.Background()

	check := func(label, said, wantID, wantNamed string) {
		t.Helper()
		from, named := musicSource(ctx, c, said)
		if from.InstanceID != wantID || named != wantNamed {
			t.Errorf("%s: %q gave %q (named %q), want %q (named %q)", label, said, from.InstanceID, named, wantID, wantNamed)
		}
	}
	check("nothing named, no setting", "", "", "")
	check("named", "YouTube Music", "ytmusic--a1", "YouTube Music")
	check("named, not on this server", "Tidal", "", "Tidal")

	if err := config.Set().MusicAssistant().SetSource("spotify--b2"); err != nil {
		t.Fatal(err)
	}
	check("the setting", "", "spotify--b2", "")
	check("named over the setting", "youtube music", "ytmusic--a1", "youtube music")

	forbid = true
	check("not listed: the setting as it is", "", "spotify--b2", "")
	check("not listed: a common name", "YouTube Music", "ytmusic", "YouTube Music")
	check("not listed: an unknown name", "my mixtapes", "", "my mixtapes")
}
