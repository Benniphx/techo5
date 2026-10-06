//go:build !dot

package video

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The guard takes only the decoder, fetches what it asks for, hands a redirect back rather than
// following it, and refuses a connection to a refused address however it was named, for http and for
// https's CONNECT alike.
func TestTheGuardKeepsTheDecoderOffTheDevice(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "http://localhost:1/x", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "picture")
	}))
	defer ok.Close()
	saved := refused
	t.Cleanup(func() { refused = saved })
	// In the test everything is on loopback, so what the guard refuses is switched by hand: first the
	// upstream stands for the network, then for the device.
	refusing := false
	refused = func(net.IP) bool { return refusing }

	addr, err := guardProxy()
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(addr)
	client := func(proxy *url.URL) *http.Client {
		return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}

	// Without the password: refused.
	bare := *pu
	bare.User = nil
	resp, err := client(&bare).Get(ok.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Errorf("without the password: %d", resp.StatusCode)
	}

	// With it: fetched, and a redirect handed back as it is.
	resp, err = client(pu).Get(ok.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "picture" {
		t.Errorf("fetch: %d %q", resp.StatusCode, b)
	}
	resp, err = client(pu).Get(ok.URL + "/moved")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "localhost:1") {
		t.Errorf("redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// The device: refused, by http and by CONNECT.
	refusing = true
	guardTransport.CloseIdleConnections() // a connection already made was checked as it was made
	resp, err = client(pu).Get(ok.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a refused address was fetched: %d", resp.StatusCode)
	}
	conn, err := net.Dial("tcp", pu.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pass, _ := pu.User.Password()
	req, _ := http.NewRequest(http.MethodConnect, "http://"+ok.Listener.Addr().String(), nil)
	req.Host = ok.Listener.Addr().String()
	req.SetBasicAuth("video", pass)
	req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
	_ = req.Write(conn)
	cr, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	if cr.StatusCode == http.StatusOK {
		t.Error("a CONNECT to a refused address was made")
	}
}

// Turning Video off closes the proxy; the next video gets a new one, with a new password.
func TestTheGuardClosesWithVideo(t *testing.T) {
	a, err := guardProxy()
	if err != nil {
		t.Fatal(err)
	}
	closeGuard()
	u, _ := url.Parse(a)
	if c, err := net.Dial("tcp", u.Host); err == nil {
		c.Close()
		t.Error("the proxy still listens after it was closed")
	}
	b, err := guardProxy()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("the proxy came back with the same address and password")
	}
	closeGuard()
}
