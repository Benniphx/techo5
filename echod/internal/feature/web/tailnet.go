package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Over Tailscale (docs/tailscale.md), Home Assistant reaches the device on its own encrypted port, not
// this one. The camera, the screen, the setup page and the calls between devices are for the house the
// device is in: a request from away, over the tailnet, gets nothing here, whatever switch is on, but
// the setup page while Remote setup is on (tailnet_setup.go).

var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// FromTailnet reports whether a connection's remote address is a Tailscale one.
func FromTailnet(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap()
	return tailnetV4.Contains(a) || tailnetV6.Contains(a)
}

// TunName is the Tailscale interface's name, as tailscaled is started with it (feature/tailscale).
const TunName = "tailscale0"

// TailscaleUp is whether Tailscale says it is connected (feature/tailscale sets it): then a tailnet
// address is from away even if no Tailscale interface is found (a renamed one, userspace
// networking).
var TailscaleUp = func() bool { return false }

// LocalNets is this device's networks: whether it has a Tailscale interface, and the prefixes of its
// other interfaces. A variable, for tests.
var LocalNets = func() (tailscale bool, nets []netip.Prefix) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return true, nil // not known: a tailnet address is taken for one from away
	}
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		ts := ifc.Name == TunName || strings.HasPrefix(ifc.Name, "tailscale")
		for _, a := range addrs {
			pfx, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if ts {
				tailscale = true
				continue
			}
			nets = append(nets, pfx.Masked())
		}
	}
	return tailscale, nets
}

// Away is whether a remote address is from away, over the tailnet: a tailnet address, with a
// tailnet on this device to reach it by. A tailnet address is in 100.64.0.0/10, which some home
// networks are numbered in too (carrier-grade NAT, some mesh routers): one on this device's own
// networks is never away, and with no Tailscale running here nothing is.
func Away(remote string) bool {
	if !FromTailnet(remote) {
		return false
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	tailscale, nets := LocalNets()
	tailscale = tailscale || TailscaleUp()
	for _, n := range nets {
		if n.Contains(ip) {
			return false // this house's own network, numbered like a tailnet
		}
	}
	return tailscale
}

// tailnetGate lets a request from away (Away) reach nothing but the setup page, and that only while
// Remote setup is on (RemoteSetup).
func tailnetGate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Away(r.RemoteAddr) && !remoteSetupRequest(r) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
