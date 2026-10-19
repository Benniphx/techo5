package phone

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// registerFor is how long a registration lasts before it is renewed. Short enough that a device that
// went away stops being rung soon, and long enough not to be chatty.
const registerFor = 5 * time.Minute

// line is one signed-in SIP account: the user agent, its transport, and the calls through it.
type line struct {
	acct Account
	ua   *sipgo.UserAgent
	dg   *diago.Diago
	tran string // "tls" or "udp"
	port int
}

// providerCiphers are what the TLS connection to the provider may use. VoIP.ms offers only RSA key
// exchange (TLS_RSA_WITH_AES_256_GCM_SHA384), which Go leaves out unless it is asked for by name. It
// is still encrypted and the certificate is still checked; what it lacks is forward secrecy, which the
// provider decides. The ECDHE suites come first so a provider that offers them gets them.
var providerCiphers = []uint16{
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
}

// open signs nothing in yet: it builds the user agent and starts answering what arrives on it.
// incoming is called for every call offered by the provider (fromProvider), on its own goroutine, and
// the call lasts as long as it runs.
func open(ctx context.Context, acct Account, incoming func(*diago.DialogServerSession)) (*line, error) {
	host, err := localAddr(acct.Server)
	if err != nil {
		return nil, err
	}

	l := &line{acct: acct, tran: "tls", port: 5061}
	if acct.Plain {
		l.tran, l.port = "udp", 5060
	}

	opts := []sipgo.UserAgentOption{
		// The From user is what the provider matches the account on, so it is the SIP username.
		sipgo.WithUserAgent(acct.Username),
		sipgo.WithUserAgentHostname(host),
		sipgo.WithUserAgenTLSConfig(&tls.Config{ServerName: acct.Server, CipherSuites: providerCiphers, MinVersion: tls.VersionTLS12}),
		sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerLogger(sipLogger())),
	}
	ua, err := sipgo.NewUA(opts...)
	if err != nil {
		return nil, err
	}
	l.ua = ua

	tr := diago.Transport{Transport: l.tran, BindHost: host}
	if !acct.Plain {
		// Calls arrive over the connection the registration keeps open, so nothing ever connects to
		// this listener; diago still starts one, and a TLS listener cannot start without a
		// certificate. A throwaway one, never shown to anyone.
		cert, err := throwawayCert()
		if err != nil {
			ua.Close()
			return nil, err
		}
		tr.TLSConf = &tls.Config{Certificates: []tls.Certificate{cert}}
		tr.MediaSRTP = 1 // SDES, which is what a provider offers alongside SIP over TLS
	}

	// The listener is on the home network, so anything there can send it an INVITE. An Echo's firewall
	// lets in only the provider's side of the registration; a panel has no firewall of its own, so the
	// check is made here, on every device: a call is only rung for when it came from the provider.
	offered := func(d *diago.DialogServerSession) {
		from := d.InviteRequest.MessageData.Source()
		if !fromProvider(acct.Server, from) {
			slog.Warn("phone: refused a call that did not come from the provider", "from", from)
			_ = d.Respond(sip.StatusForbidden, "Forbidden", nil)
			return
		}
		incoming(d)
	}

	quiet := slog.New(slog.NewTextHandler(slogWriter{}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	l.dg = diago.NewDiago(ua, diago.WithTransport(tr), diago.WithLogger(quiet))
	if err := l.dg.ServeBackground(ctx, offered); err != nil {
		ua.Close()
		return nil, fmt.Errorf("phone: listening: %w", err)
	}
	return l, nil
}

func (l *line) close() { l.ua.Close() }

func (l *line) uri(user string) (sip.Uri, error) {
	var u sip.Uri
	s := fmt.Sprintf("sip:%s@%s:%d", user, l.acct.Server, l.port)
	if l.tran != "udp" {
		s += ";transport=" + l.tran
	}
	err := sip.ParseUri(s, &u)
	return u, err
}

// register keeps the account signed in until ctx ends, and signs it out then. registered is called
// each time the provider accepts it.
func (l *line) register(ctx context.Context, registered func()) error {
	u, err := l.uri(l.acct.Username)
	if err != nil {
		return err
	}
	// RetryInterval is also how often diago registers again, which the firewalls rely on: a call to a
	// UDP account arrives on the registration's own conntrack entry, which lapses after three minutes.
	return l.dg.Register(ctx, u, diago.RegisterOptions{
		Username:      l.acct.Username,
		Password:      l.acct.Password,
		Expiry:        registerFor,
		RetryInterval: 30 * time.Second,
		OnRegistered:  registered,
	})
}

// dial places a call and returns once it is answered, refused, or ctx ends.
func (l *line) dial(ctx context.Context, number string) (*diago.DialogClientSession, error) {
	u, err := l.uri(number)
	if err != nil {
		return nil, err
	}
	return l.dg.Invite(ctx, u, diago.InviteOptions{
		Username:  l.acct.Username,
		Password:  l.acct.Password,
		Transport: l.tran,
	})
}

// localAddr is the address this device reaches the provider from, which is what goes into the call's
// media description. The provider sees past it (the home router's address), but it has to be one the
// device really has.
func localAddr(server string) (string, error) {
	c, err := net.Dial("udp", net.JoinHostPort(server, "5060"))
	if err != nil {
		return "", fmt.Errorf("phone: reaching %s: %w", server, err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// providerLookup bounds the lookup of the provider's addresses when a call comes in.
const providerLookup = 5 * time.Second

// fromProvider is whether a request's source, the address it arrived from (host:port), is one of the
// provider's: an address the server's name resolves to, looked up when the call arrives so a provider
// that moves is still recognized. A call over TLS arrives on the registration's own connection, and
// one over UDP from the server the registration went to.
func fromProvider(server, source string) bool {
	host, _, err := net.SplitHostPort(source)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if s := net.ParseIP(server); s != nil {
		return s.Equal(ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerLookup)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, server)
	if err != nil {
		slog.Warn("phone: looking up the provider for a call", "server", server, "err", err)
		return false
	}
	for _, a := range addrs {
		if a.IP.Equal(ip) {
			return true
		}
	}
	return false
}

func throwawayCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// slogWriter sends the library's own warnings to the daemon's log, one line each.
type slogWriter struct{}

func (slogWriter) Write(b []byte) (int, error) {
	slog.Warn("phone: sip", "said", strings.TrimSpace(string(b)))
	return len(b), nil
}
