//go:build panel

package metrics

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// netPath is where the app writes the device's interfaces. Android 11 and later refuse apps the
// netlink requests Go lists interfaces with, so net.Interfaces and net.InterfaceAddrs come back
// empty; Java's NetworkInterface still answers, so the app asks it and writes the answer here
// whenever the network changes. One line an interface:
//
//	name index mtu flags addr/prefix ...
//
// flags is net.Flags as a number.
var netPath = layout.StateDir + "/net"

type iface struct {
	net.Interface
	addrs []net.IP
}

func readInterfaces() []iface {
	f, err := os.Open(netPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []iface
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		index, err1 := strconv.Atoi(fields[1])
		mtu, err2 := strconv.Atoi(fields[2])
		flags, err3 := strconv.ParseUint(fields[3], 10, 32)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		i := iface{Interface: net.Interface{Index: index, MTU: mtu, Name: fields[0], Flags: net.Flags(flags)}}
		for _, a := range fields[4:] {
			if ip, _, err := net.ParseCIDR(a); err == nil {
				i.addrs = append(i.addrs, ip)
			}
		}
		out = append(out, i)
	}
	return out
}

// Addresses is every address something else on the network could reach the device at, as the app
// last reported them. Loopback and link-local are left out.
func Addresses() []net.IP {
	return reported(func(string) bool { return true })
}

// WiFiAddresses is the addresses of Addresses on the Wi-Fi interface alone: the network's own, without a
// VPN's beside them. Android names it wlan0 as the Echo does; a tablet that names it otherwise has none
// here, which only means an IPv4 move goes unnoticed and is advertised like any other change (api's
// nextHeld).
func WiFiAddresses() []net.IP {
	return reported(func(name string) bool { return name == wifiInterface })
}

// reported is the reachable addresses of the interfaces that are up and whose name passes keep.
func reported(keep func(name string) bool) []net.IP {
	var ips []net.IP
	for _, i := range readInterfaces() {
		if i.Flags&net.FlagUp == 0 || !keep(i.Name) {
			continue
		}
		for _, ip := range i.addrs {
			if ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

// MulticastInterfaces is the interfaces that are up and take multicast, for the mDNS library, which
// would otherwise list them itself and find none.
func MulticastInterfaces() []net.Interface {
	var out []net.Interface
	for _, i := range readInterfaces() {
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagMulticast != 0 && i.Flags&net.FlagLoopback == 0 {
			out = append(out, i.Interface)
		}
	}
	return out
}
