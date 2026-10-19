package phone

import "testing"

// A call is rung for only when it came from the provider: the listener is on the home network, and on
// a panel nothing in front of it keeps anyone else's INVITE out.
func TestOnlyTheProviderRings(t *testing.T) {
	for _, c := range []struct {
		server, source string
		want           bool
	}{
		{"192.0.2.10", "192.0.2.10:5060", true},
		{"192.0.2.10", "192.0.2.10:40123", true},
		{"192.0.2.10", "[::ffff:192.0.2.10]:5061", true},
		{"192.0.2.10", "198.51.100.7:5060", false},
		{"192.0.2.10", "203.0.113.20:5060", false},
		{"2001:db8::5", "[2001:db8::5]:5061", true},
		{"2001:db8::5", "[2001:db8::6]:5061", false},
		{"localhost", "127.0.0.1:5061", true},
		{"localhost", "192.0.2.10:5061", false},
		{"192.0.2.10", "", false},
		{"192.0.2.10", "192.0.2.10", false},
		{"192.0.2.10", "provider.example:5060", false},
	} {
		if got := fromProvider(c.server, c.source); got != c.want {
			t.Errorf("a call from %q with the provider at %q: rung %v, want %v", c.source, c.server, got, c.want)
		}
	}
}
