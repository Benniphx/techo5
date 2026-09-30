// Package onvifback talks through a camera: the ONVIF backchannel, an RTSP session in which the client
// sends audio to the camera's speaker rather than receiving the camera's media. Nearly every IP camera
// with a speaker offers it (ONVIF Profile T), Reolink's among them, reached directly with the camera's
// own login: no go2rtc, no Frigate, no cloud.
//
// The session is DESCRIBE, SETUP and PLAY, each asking for www.onvif.org/ver20/backchannel; the camera's
// SDP names the audio track it takes (sendonly, from the camera's side of it) and its codec. The audio
// goes as RTP interleaved on the same TCP connection, G.711 at 8 kHz in 20 ms packets.
package onvifback

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/g711"
)

// require is the header that asks a camera for its backchannel rather than its media.
const require = "www.onvif.org/ver20/backchannel"

// PacketSamples is how many 8 kHz samples go in a packet: 20 ms.
const PacketSamples = 160

// keepEvery is how often the session is kept alive; cameras drop one after 60 s of silence on RTSP.
const keepEvery = 25 * time.Second

// Session is a talk-back channel open to a camera.
type Session struct {
	conn net.Conn
	br   *bufio.Reader

	base, control string // the request URL, and the backchannel track's
	user, pass    string
	challenge     map[string]string // the camera's last digest challenge; nil for none yet
	basic         bool

	mu      sync.Mutex // writes to conn, and everything below
	cseq    int
	session string
	channel byte // the interleaved channel the audio goes on
	pt      byte
	alaw    bool
	ssrc    uint32
	seq     uint16
	ts      uint32
	pending []int16 // 8 kHz samples short of a whole packet

	done    chan struct{}
	stopped sync.Once
}

// Open starts talking to the camera at rawURL (rtsp://host[:port]/path), logging in as user. It fails
// with what the camera said when it offers no backchannel, or one in a codec this device cannot send.
func Open(ctx context.Context, rawURL, user, pass string) (*Session, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "rtsp" || u.Hostname() == "" {
		// Not quoted back: the address can carry a login.
		return nil, errors.New("the camera's address is not an rtsp:// one")
	}
	if u.User != nil { // a login in the address wins over the one given
		user = u.User.Username()
		pass, _ = u.User.Password()
		u.User = nil
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "554")
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("the camera did not answer: %w", err)
	}
	s := &Session{conn: conn, br: bufio.NewReader(conn), base: u.String(), user: user, pass: pass, done: make(chan struct{})}
	if err := s.start(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	go s.drain()
	go s.keepAlive()
	return s, nil
}

func (s *Session) start(ctx context.Context) error {
	if dl, ok := ctx.Deadline(); ok {
		s.conn.SetDeadline(dl)
	} else {
		s.conn.SetDeadline(time.Now().Add(20 * time.Second))
	}
	defer s.conn.SetDeadline(time.Time{})

	// A camera reached through a hub (a Reolink doorbell behind its Home Hub) can leave the talk-back
	// track out of its first answer after a quiet spell, while the hub wakes it, and put it in the next.
	// So an answer without one is asked again, twice, before it counts.
	var hdr map[string]string
	var tk track
	for try := 0; ; try++ {
		var sdp string
		var err error
		hdr, sdp, err = s.request("DESCRIBE", s.base, map[string]string{"Accept": "application/sdp", "Require": require})
		if err != nil {
			return err
		}
		tk, err = parseSDP(sdp)
		if err == nil {
			break
		}
		if try == 2 || err != errNoTalkBack {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(1500 * time.Millisecond):
		}
	}
	base := s.base
	if cb := hdr["content-base"]; cb != "" {
		base = cb
	}
	s.control = resolve(base, tk.control)
	s.pt, s.alaw = tk.pt, tk.alaw

	hdr, _, err := s.request("SETUP", s.control, map[string]string{
		"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
		"Require":   require,
	})
	if err != nil {
		return err
	}
	s.session = strings.TrimSpace(strings.SplitN(hdr["session"], ";", 2)[0])
	if s.session == "" {
		return errors.New("the camera set up the talk-back channel without a session")
	}
	s.channel = 0
	if t := hdr["transport"]; t != "" {
		for _, p := range strings.Split(t, ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "interleaved="); ok {
				if n, err := strconv.Atoi(strings.SplitN(v, "-", 2)[0]); err == nil && n >= 0 && n < 256 {
					s.channel = byte(n)
				}
			}
		}
	}
	if _, _, err := s.request("PLAY", base, map[string]string{"Session": s.session, "Require": require, "Range": "npt=0.000-"}); err != nil {
		return err
	}
	var id [4]byte
	rand.Read(id[:])
	s.ssrc = binary.BigEndian.Uint32(id[:])
	return nil
}

// track is the backchannel as the SDP describes it.
type track struct {
	control string
	pt      byte
	alaw    bool
}

// parseSDP finds the audio track the camera receives (sendonly from its side) in G.711 at 8 kHz.
func parseSDP(sdp string) (track, error) {
	blocks := strings.Split("\n"+strings.ReplaceAll(sdp, "\r\n", "\n"), "\nm=")[1:]
	other := ""
	for _, b := range blocks {
		if !strings.HasPrefix(b, "audio") || !strings.Contains(b, "a=sendonly") {
			continue
		}
		var t track
		codecs := map[byte]string{}
		for _, line := range strings.Split(b, "\n") {
			line = strings.TrimSpace(line)
			if v, ok := strings.CutPrefix(line, "a=control:"); ok {
				t.control = v
			}
			if v, ok := strings.CutPrefix(line, "a=rtpmap:"); ok {
				f := strings.Fields(v)
				if n, err := strconv.Atoi(f[0]); err == nil && len(f) > 1 && n >= 0 && n < 128 {
					codecs[byte(n)] = strings.ToUpper(f[1])
				}
			}
		}
		// The static payload types stand for themselves when no rtpmap is given.
		fmts := strings.Fields(strings.SplitN(b, "\n", 2)[0])
		for _, f := range fmts[min(3, len(fmts)):] {
			if n, err := strconv.Atoi(f); err == nil && n >= 0 && n < 128 {
				if _, ok := codecs[byte(n)]; !ok {
					switch n {
					case 0:
						codecs[0] = "PCMU/8000"
					case 8:
						codecs[8] = "PCMA/8000"
					}
				}
			}
		}
		for _, want := range []string{"PCMU/8000", "PCMA/8000"} {
			for pt, c := range codecs {
				if strings.HasPrefix(c, want) {
					t.pt, t.alaw = pt, want == "PCMA/8000"
					if t.control == "" {
						return track{}, errors.New("the camera's talk-back track has no control address")
					}
					return t, nil
				}
			}
		}
		for _, c := range codecs {
			other = c
		}
	}
	if other != "" {
		return track{}, fmt.Errorf("the camera's talk-back channel takes %s, which this device cannot send", other)
	}
	return track{}, errNoTalkBack
}

// errNoTalkBack is a camera whose description has no track it receives audio on.
var errNoTalkBack = errors.New("the camera offers no talk-back channel")

// resolve is a track's control address against the session's base.
func resolve(base, control string) string {
	if control == "" || control == "*" {
		return base
	}
	if strings.HasPrefix(strings.ToLower(control), "rtsp://") {
		return control
	}
	b, err := url.Parse(base)
	if err != nil {
		return control
	}
	if !strings.HasSuffix(b.Path, "/") {
		b.Path += "/"
	}
	r, err := b.Parse(control)
	if err != nil {
		return control
	}
	return r.String()
}

// request sends one request and reads its answer, logging in when the camera asks. Before PLAY only:
// after it, drain reads the connection.
func (s *Session) request(method, uri string, headers map[string]string) (map[string]string, string, error) {
	for attempt := 0; ; attempt++ {
		if err := s.send(method, uri, headers); err != nil {
			return nil, "", err
		}
		code, reason, hdr, body, err := readResponse(s.br)
		if err != nil {
			return nil, "", fmt.Errorf("reading the camera's answer to %s: %w", method, err)
		}
		switch {
		case code == 200:
			return hdr, body, nil
		case code == 401 && attempt == 0 && hdr["www-authenticate"] != "":
			s.learn(hdr["www-authenticate"])
			continue
		case code == 401:
			return nil, "", errors.New("the camera refused the login")
		case method == "DESCRIBE" && (code == 551 || code == 404):
			return nil, "", errNoTalkBack
		}
		return nil, "", fmt.Errorf("the camera answered %s with %d %s", method, code, reason)
	}
}

// send writes one request, signed when the camera has asked for a login.
func (s *Session) send(method, uri string, headers map[string]string) error {
	s.cseq++
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: TECHO5\r\n", method, uri, s.cseq)
	for k, v := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if a := s.authorization(method, uri); a != "" {
		fmt.Fprintf(&b, "Authorization: %s\r\n", a)
	}
	b.WriteString("\r\n")
	_, err := io.WriteString(s.conn, b.String())
	return err
}

// learn takes a camera's WWW-Authenticate. Digest is preferred to Basic when it offers both, but only
// one header is kept by the reader, and cameras send Digest.
func (s *Session) learn(h string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(h), " ")
	if strings.EqualFold(scheme, "Basic") {
		s.basic, s.challenge = true, nil
		return
	}
	s.basic = false
	s.challenge = map[string]string{}
	for _, part := range splitParams(rest) {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			s.challenge[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
}

// splitParams splits a challenge's parameters on commas outside quotes.
func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (s *Session) authorization(method, uri string) string {
	if s.basic {
		return "Basic " + basic(s.user, s.pass)
	}
	c := s.challenge
	if c == nil {
		return ""
	}
	h := func(v string) string { sum := md5.Sum([]byte(v)); return hex.EncodeToString(sum[:]) }
	ha1 := h(s.user + ":" + c["realm"] + ":" + s.pass)
	ha2 := h(method + ":" + uri)
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s"`, s.user, c["realm"], c["nonce"], uri)
	if strings.Contains(c["qop"], "auth") {
		var cn [8]byte
		rand.Read(cn[:])
		cnonce, nc := hex.EncodeToString(cn[:]), fmt.Sprintf("%08x", s.cseq)
		out += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s", response="%s"`, nc, cnonce, h(ha1+":"+c["nonce"]+":"+nc+":"+cnonce+":auth:"+ha2))
	} else {
		out += fmt.Sprintf(`, response="%s"`, h(ha1+":"+c["nonce"]+":"+ha2))
	}
	if o := c["opaque"]; o != "" {
		out += fmt.Sprintf(`, opaque="%s"`, o)
	}
	if a := c["algorithm"]; a != "" {
		out += ", algorithm=" + a
	}
	return out
}

func basic(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// readResponse reads one RTSP answer: status, headers (lower-cased keys, the first of each) and body,
// skipping any interleaved packets in front of it.
func readResponse(br *bufio.Reader) (code int, reason string, hdr map[string]string, body string, err error) {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return 0, "", nil, "", err
		}
		if b[0] != '$' {
			break
		}
		if err := skipPacket(br); err != nil {
			return 0, "", nil, "", err
		}
	}
	line, err := br.ReadString('\n')
	if err != nil {
		return 0, "", nil, "", err
	}
	f := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(f) < 2 || !strings.HasPrefix(f[0], "RTSP/") {
		return 0, "", nil, "", fmt.Errorf("not an RTSP answer: %q", strings.TrimSpace(line))
	}
	code, _ = strconv.Atoi(f[1])
	if len(f) > 2 {
		reason = f[2]
	}
	hdr = map[string]string{}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			return 0, "", nil, "", err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, seen := hdr[k]; !seen {
			hdr[k] = strings.TrimSpace(v)
		}
	}
	if n, err := strconv.Atoi(hdr["content-length"]); err == nil && n > 0 {
		if n > 1<<20 {
			return 0, "", nil, "", fmt.Errorf("an answer of %d bytes", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(br, buf); err != nil {
			return 0, "", nil, "", err
		}
		body = string(buf)
	}
	return code, reason, hdr, body, nil
}

// skipPacket reads past one interleaved packet: '$', the channel, a 16-bit length, the data.
func skipPacket(br *bufio.Reader) error {
	var h [4]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return err
	}
	_, err := br.Discard(int(binary.BigEndian.Uint16(h[2:])))
	return err
}

// drain reads whatever the camera sends once the session is playing (its RTCP, answers to the keep-alive)
// so the connection never backs up, and closes the session when the camera does.
func (s *Session) drain() {
	defer s.stop()
	for {
		b, err := s.br.Peek(1)
		if err != nil {
			return
		}
		if b[0] == '$' {
			err = skipPacket(s.br)
		} else {
			_, _, _, _, err = readResponse(s.br)
		}
		if err != nil {
			return
		}
	}
}

// keepAlive tells the camera the session is still wanted: GET_PARAMETER, which every camera that
// passed PLAY answers, and whose answer drain reads.
func (s *Session) keepAlive() {
	t := time.NewTicker(keepEvery)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.mu.Lock()
			s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := s.send("GET_PARAMETER", s.base, map[string]string{"Session": s.session})
			s.mu.Unlock()
			if err != nil {
				s.stop()
				return
			}
		}
	}
}

// Write sends 8 kHz samples to the camera's speaker, in 20 ms packets; what is short of a packet waits
// for the next call.
func (s *Session) Write(samples []int16) error {
	select {
	case <-s.done:
		return errors.New("the camera closed the talk-back channel")
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, samples...)
	for len(s.pending) >= PacketSamples {
		pkt := make([]byte, 4+12+PacketSamples)
		pkt[0], pkt[1] = '$', s.channel
		binary.BigEndian.PutUint16(pkt[2:], uint16(12+PacketSamples))
		rtp := pkt[4:]
		rtp[0], rtp[1] = 0x80, s.pt
		binary.BigEndian.PutUint16(rtp[2:], s.seq)
		binary.BigEndian.PutUint32(rtp[4:], s.ts)
		binary.BigEndian.PutUint32(rtp[8:], s.ssrc)
		for i, v := range s.pending[:PacketSamples] {
			if s.alaw {
				rtp[12+i] = g711.ALaw(v)
			} else {
				rtp[12+i] = g711.ULaw(v)
			}
		}
		s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := s.conn.Write(pkt); err != nil {
			return fmt.Errorf("sending to the camera: %w", err)
		}
		s.seq++
		s.ts += PacketSamples
		s.pending = s.pending[PacketSamples:]
	}
	return nil
}

// Codec is what the camera takes: "PCMU" or "PCMA".
func (s *Session) Codec() string {
	if s.alaw {
		return "PCMA"
	}
	return "PCMU"
}

// Done is closed when the session has ended, by Close or by the camera.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) stop() { s.stopped.Do(func() { close(s.done) }) }

// Close ends the session: TEARDOWN, as a courtesy the camera may not wait for, then the connection.
func (s *Session) Close() error {
	s.mu.Lock()
	s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	s.send("TEARDOWN", s.base, map[string]string{"Session": s.session})
	s.mu.Unlock()
	s.stop()
	return s.conn.Close()
}
