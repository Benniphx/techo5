// Package localname looks up .local names, which a device on a TECHO5 root filesystem cannot do
// otherwise: musl's resolver and Go's own ask the DNS servers, and a home's DNS knows nothing of
// homeassistant.local. The name is asked for over multicast DNS instead, the way a phone or a laptop
// finds it, and what comes back is written into the hosts file, which every program on the device
// reads - the daemon, ffmpeg playing Home Assistant's text to speech, the media players.
package localname

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// group is where a multicast DNS question goes and its answers come back.
var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// listen opens the socket a question is asked from and its answers heard on: the multicast DNS port
// itself, shared with the device's own advertiser (Go sets SO_REUSEADDR on it). A question from a
// port of its own would be answered straight back to that port by the responder's own address, and
// the device's firewall, which lets 5353 in for exactly this, turns such an answer away. A variable
// so a test can answer on loopback.
var listen = func() (*net.UDPConn, *net.UDPAddr, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	return conn, group, err
}

// IsLocal is whether host is a .local name: one only multicast DNS can find.
func IsLocal(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.HasSuffix(h, ".local") && net.ParseIP(h) == nil
}

// Resolve asks the network for host's IPv4 address over multicast DNS. It finds a name on this
// network only: a router passes multicast DNS from another one only if it is set to.
func Resolve(ctx context.Context, host string) (net.IP, error) {
	name := dns.Fqdn(strings.ToLower(host))
	conn, to, err := listen()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	q := new(dns.Msg)
	q.SetQuestion(name, dns.TypeA)
	q.RecursionDesired = false
	out, err := q.Pack()
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Second)
	}
	// Asked twice, a second apart, as multicast loses things: the first answer ends the wait.
	if _, err := conn.WriteToUDP(out, to); err != nil {
		return nil, err
	}
	again := time.Now().Add(time.Second)
	buf := make([]byte, 9000)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = conn.SetReadDeadline(minTime(deadline, again))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && time.Now().Before(deadline) {
				if !again.IsZero() {
					_, _ = conn.WriteToUDP(out, to)
					again = time.Time{}
				}
				continue
			}
			return nil, fmt.Errorf("localname: no answer for %s", host)
		}
		var m dns.Msg
		if m.Unpack(buf[:n]) != nil || !m.Response {
			continue // the questions on the group, its own among them
		}
		for _, rr := range append(m.Answer, m.Extra...) {
			if a, ok := rr.(*dns.A); ok && strings.EqualFold(a.Hdr.Name, name) {
				return a.A, nil
			}
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

// base is what the hosts file always holds, as boot.sh writes it.
const base = "127.0.0.1\tlocalhost localhost.localdomain\n::1\t\tlocalhost localhost.localdomain\n"

// WriteHosts replaces the hosts file at path with localhost and names, each name's address beside it.
// Through a file next to it and a rename, so a program reading it never sees half of one.
func WriteHosts(path string, names map[string]net.IP) error {
	var b strings.Builder
	b.WriteString(base)
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", names[k], strings.TrimSuffix(strings.ToLower(k), "."))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hosts-")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
