package home

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// coverServer serves a test picture over https, failing the first n requests for fail, and holding
// each request for hold first.
func coverServer(t *testing.T, fail int32, hold time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	pic := testJPEG(t, 300, 300)
	var asked atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := asked.Add(1)
		select {
		case <-time.After(hold):
		case <-r.Context().Done():
			return
		}
		if r.URL.Path == "/fail" && n <= fail {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(pic)
	}))
	was := artClient
	artClient = srv.Client()
	t.Cleanup(func() {
		artClient = was
		ReceivedArt("", "")
		srv.Close()
	})
	return srv, &asked
}

// waitFor polls until ok or a second has passed.
func waitFor(ok func() bool) bool {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return true
		}
	}
	return ok()
}

// A song's cover is fetched and shown for that receiver only; a new song drops the old picture at once.
func TestReceivedArtShowsTheSongsCover(t *testing.T) {
	if !hasScreen {
		t.Skip("no screen")
	}
	srv, _ := coverServer(t, 0, 0)
	ReceivedArt(SpotifyName, srv.URL+"/a")
	if !waitFor(func() bool { a, _ := receivedArt(SpotifyName); return a != nil }) {
		t.Fatal("the cover never came")
	}
	if a, _ := receivedArt(AirPlayName); a != nil {
		t.Error("Spotify's cover shown for AirPlay")
	}
	ReceivedArt(SpotifyName, "http://example.invalid/b") // not https: no picture
	if a, _ := receivedArt(SpotifyName); a != nil {
		t.Error("the last song's cover stayed")
	}
}

// Skipping on calls off the fetch for the song left behind, and only the newest song's cover is shown,
// even when the same song comes round again while its first fetch is still out.
func TestReceivedArtKeepsOnlyTheNewest(t *testing.T) {
	if !hasScreen {
		t.Skip("no screen")
	}
	srv, _ := coverServer(t, 0, 200*time.Millisecond)
	ReceivedArt(SpotifyName, srv.URL+"/a")
	ReceivedArt(SpotifyName, srv.URL+"/b")
	ReceivedArt(SpotifyName, srv.URL+"/a") // back to the first song
	if !waitFor(func() bool { a, _ := receivedArt(SpotifyName); return a != nil }) {
		t.Fatal("the newest cover never came")
	}
	received.mu.Lock()
	url, cancel := received.url, received.cancel
	received.mu.Unlock()
	if url != srv.URL+"/a" || cancel != nil {
		t.Errorf("shown for %q with a fetch still out: %v", url, cancel != nil)
	}
}

// A fetch that failed is tried again when the next song names the same cover, as the next track on an
// album does; one that worked is not fetched twice.
func TestReceivedArtRetriesAFailedCover(t *testing.T) {
	if !hasScreen {
		t.Skip("no screen")
	}
	srv, asked := coverServer(t, 1, 0)
	ReceivedArt(SpotifyName, srv.URL+"/fail")
	if !waitFor(func() bool {
		received.mu.Lock()
		defer received.mu.Unlock()
		return received.cancel == nil
	}) {
		t.Fatal("the first fetch never finished")
	}
	if a, _ := receivedArt(SpotifyName); a != nil {
		t.Fatal("a failed fetch showed a picture")
	}
	ReceivedArt(SpotifyName, srv.URL+"/fail") // the next track on the album
	if !waitFor(func() bool { a, _ := receivedArt(SpotifyName); return a != nil }) {
		t.Fatal("the cover was not tried again")
	}
	ReceivedArt(SpotifyName, srv.URL+"/fail") // and the one after: already shown
	time.Sleep(50 * time.Millisecond)
	if n := asked.Load(); n != 2 {
		t.Errorf("asked %d times, want 2", n)
	}
}
