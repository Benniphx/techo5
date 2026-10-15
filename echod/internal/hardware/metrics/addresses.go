package metrics

import (
	"net"
	"slices"
	"strings"
)

// Addresses is every address something else on the network could reach the device at. Loopback and
// link-local are left out: neither is an answer to "where is it". So is a USB gadget link (the bench
// network on the USB cable, athena's RNDIS at 172.16.42.1): it reaches only the computer at the other
// end of the cable, and advertised over mDNS it became the address Home Assistant kept for the device
// and could not connect to (unit 1, 2026-10-15).
//
// Not a Reader: this comes from the network stack rather than from a file under Root, so there is
// nothing for a test to point somewhere else.
func Addresses() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, i := range ifs {
		if usbGadget(i.Name) {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		ips = append(ips, reachable(addrs)...)
	}
	return ips
}

// usbGadget is whether an interface is a USB gadget link: the RNDIS and ECM functions (rndis0, usb0,
// usb1) and the bridge TECHO5's bench shells put them on (br0).
func usbGadget(name string) bool {
	return strings.HasPrefix(name, "rndis") || strings.HasPrefix(name, "usb") || name == "br0"
}

// WiFiAddresses is the addresses of Addresses on the Wi-Fi interface alone: the network's own, without a
// VPN's beside them.
func WiFiAddresses() []net.IP {
	i, err := net.InterfaceByName(wifiInterface)
	if err != nil {
		return nil
	}
	addrs, err := i.Addrs()
	if err != nil {
		return nil
	}
	return reachable(addrs)
}

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
