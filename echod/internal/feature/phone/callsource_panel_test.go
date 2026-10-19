//go:build panel

package phone

import (
	"context"
	"net"
	"testing"
)

// A panel rings for a call only when it came from the provider.
func TestAPanelRefusesACallFromElsewhere(t *testing.T) {
	p := newProvider("sip.provider.example")
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}}, nil
	}
	if !p.admits("192.0.2.10:5061") {
		t.Error("a panel refused the provider's call")
	}
	if p.admits("198.51.100.7:5060") {
		t.Error("a panel rang for a call from elsewhere on the network")
	}
}
