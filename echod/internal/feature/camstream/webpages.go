package camstream

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/web"
)

// The camera's web pages (feature/camera's /camera.jpg and /camera.mjpeg) take the stream's login, as an
// HTTP Digest, or a browser the setup page has let in: the same picture as the RTSP stream is not to be had
// for less. A device with no camera password set lets only the setup page's browsers see them. Wrong logins
// count with the stream's (guard.go).

// nonceFor is how long a page's nonce is good: long enough for a recorder that logs in once and asks
// again every few seconds, short enough that a login read off the wire is not good for long.
const nonceFor = 5 * time.Minute

var (
	webKeyOnce sync.Once
	webKey     []byte
)

// pageNonce is a nonce the device can check without remembering it: the time it was made and a MAC over
// that with a key made at start.
func pageNonce(now time.Time) string {
	webKeyOnce.Do(func() { webKey = randomBytes(32) })
	t := strconv.FormatInt(now.Unix(), 16)
	m := hmac.New(sha256.New, webKey)
	m.Write([]byte(t))
	return t + "." + hex.EncodeToString(m.Sum(nil))[:32]
}

// nonceOK is whether a nonce is one of ours and not too old.
func nonceOK(n string, now time.Time) bool {
	t, _, ok := strings.Cut(n, ".")
	if !ok {
		return false
	}
	sec, err := strconv.ParseInt(t, 16, 64)
	if err != nil {
		return false
	}
	at := time.Unix(sec, 0)
	if now.Sub(at) > nonceFor || at.After(now.Add(time.Minute)) {
		return false
	}
	return hmac.Equal([]byte(n), []byte(pageNonce(at)))
}

// PageAllowed is whether a request may see a camera page, and answers it with the login asked for when not.
func PageAllowed(w http.ResponseWriter, r *http.Request) bool {
	if web.LetIn(r) {
		return true
	}
	now := time.Now()
	if h := r.Header.Get("Authorization"); h != "" {
		if pageLogin(r, h, now) {
			return true
		}
	}
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=MD5`, realm, pageNonce(now)))
	web.Refuse(w, "401 the camera's login is needed", http.StatusUnauthorized)
	return false
}

// pageLogin checks a Digest login against the stream's user and password, counted by the stream's guard.
func pageLogin(r *http.Request, h string, now time.Time) bool {
	user, pass := config.Get().CamStream.Login()
	if pass == "" {
		return false
	}
	rest, ok := strings.CutPrefix(h, "Digest ")
	if !ok {
		return false
	}
	p := digestParams(rest)
	if p["realm"] != realm || !nonceOK(p["nonce"], now) || p["uri"] != r.URL.RequestURI() {
		return false
	}
	ok, done := Get().guard.attempt(remoteHost(r))
	if !ok {
		return false
	}
	ha1 := md5hex(user + ":" + realm + ":" + pass)
	ha2 := md5hex(r.Method + ":" + p["uri"])
	var want string
	switch p["qop"] {
	case "":
		want = md5hex(ha1 + ":" + p["nonce"] + ":" + ha2)
	case "auth":
		want = md5hex(ha1 + ":" + p["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
	}
	good := want != "" && hmac.Equal([]byte(user), []byte(p["username"])) && hmac.Equal([]byte(want), []byte(p["response"]))
	done(!good)
	return good
}

// lingerFor keeps the camera mark up a little after a page is done, so a snapshot is seen.
var lingerFor = 3 * time.Second

// PageWatching marks the camera as watched while a page sends it, lighting what the stream lights;
// call the func it gives back when done.
func PageWatching() func() {
	f := Get()
	f.mu.Lock()
	f.pages++
	first := f.pages == 1
	f.mu.Unlock()
	if first {
		f.Changed.Emit(struct{}{})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			time.AfterFunc(lingerFor, func() {
				f.mu.Lock()
				f.pages--
				last := f.pages == 0
				f.mu.Unlock()
				if last {
					f.Changed.Emit(struct{}{})
				}
			})
		})
	}
}
