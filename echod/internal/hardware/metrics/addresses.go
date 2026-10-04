package metrics

import (
	"net"
	"slices"
	"strings"
)

// reachable is the addresses something else on the network could reach: loopback and link-local are
// left out, since neither is an answer to "where is it".
func reachable(addrs []net.Addr) []net.IP {
	var ips []net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if ok && ipnet.IP.IsGlobalUnicast() && !ipnet.IP.IsLinkLocalUnicast() {
			ips = append(ips, ipnet.IP)
		}
	}
	return ips
}

// AddressKey is a set of addresses as one comparable string, so a change is one comparison.
func AddressKey(ips []net.IP) string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}
