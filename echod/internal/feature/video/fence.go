//go:build !dot

package video

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"
)

// The decoder fetches whatever it is pointed at, and follows names and redirects itself, so checking
// the address first (CheckURL) cannot keep it off the device: a name can resolve to 127.0.0.1, a
// redirect can lead to the device's own address. The kernel can. Everything the decoder's user sends
// out goes through a chain of its own that refuses loopback (127.0.0.0/8, ::1, and the device's own
// addresses, which the kernel carries over loopback too) and link-local (169.254.0.0/16, fe80::/10),
// and leaves the rest of the network as it was: the media servers are on it.
//
// The image's firewall (tools/linux/rootfs/usr/local/sbin/techo5-firewall) makes it at boot for the
// image's own decoder user; this makes sure of it for whichever user the decoder runs as, before each
// start, and the decoder is not started without it.

// fenceChain is the decoder's chain, jumped to from OUTPUT for its user.
const fenceChain = "TECHO5-VIDEO"

// fence makes sure the decoder's user is fenced; a variable for the tests, which have no iptables.
var fence = ensureFence

var fenceMu sync.Mutex

// fenceRules are the chain's rules, IPv4 and IPv6.
var fenceRules = map[string][][]string{
	"iptables-legacy": {
		{"-o", "lo", "-j", "REJECT"},
		{"-d", "127.0.0.0/8", "-j", "REJECT"},
		{"-d", "169.254.0.0/16", "-j", "REJECT"},
	},
	"ip6tables-legacy": {
		{"-o", "lo", "-j", "REJECT"},
		{"-d", "::1/128", "-j", "REJECT"},
		{"-d", "fe80::/10", "-j", "REJECT"},
	},
}

// iptables runs one iptables command; a variable for the tests.
var iptables = func(tool string, args ...string) error {
	out, err := exec.Command(tool, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %s", tool, args, err, clip(string(out), 200))
	}
	return nil
}

// ensureFence adds what is missing of the fence for uid, and says whether it is all there.
func ensureFence(uid uint32) error {
	fenceMu.Lock()
	defer fenceMu.Unlock()
	owner := []string{"-m", "owner", "--uid-owner", strconv.FormatUint(uint64(uid), 10), "-j", fenceChain}
	for _, tool := range []string{"iptables-legacy", "ip6tables-legacy"} {
		_ = iptables(tool, "-N", fenceChain) // there already, as often as not
		for _, rule := range fenceRules[tool] {
			if iptables(tool, append([]string{"-C", fenceChain}, rule...)...) == nil {
				continue
			}
			if err := iptables(tool, append([]string{"-A", fenceChain}, rule...)...); err != nil {
				return fmt.Errorf("the decoder cannot be kept off the device: %w", err)
			}
		}
		if iptables(tool, append([]string{"-C", "OUTPUT"}, owner...)...) == nil {
			continue
		}
		if err := iptables(tool, append([]string{"-I", "OUTPUT", "1"}, owner...)...); err != nil {
			return fmt.Errorf("the decoder cannot be kept off the device: %w", err)
		}
	}
	return nil
}
