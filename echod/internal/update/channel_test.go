package update

import "testing"

func TestDeviceUpdateChannels(t *testing.T) {
	channels := Channels()
	if nativeChannel {
		if len(channels) != 1 || channels[0] != Stable {
			t.Fatalf("native Show offers channels %v, want only Stable", channels)
		}
		for _, c := range []Channel{Stable, Dev} {
			if c.Label() != "native-stable" || c.URL() != "https://github.com/Benniphx/techo5/releases/latest/download/manifest.json" {
				t.Errorf("native channel %v reports %q at %q", c, c.Label(), c.URL())
			}
		}
		if releaseKey == upstreamReleaseKey {
			t.Fatal("native Show still trusts the upstream signing key")
		}
		return
	}
	if len(channels) != 2 || channels[0] != Stable || channels[1] != Dev {
		t.Fatalf("upstream device channels changed: %v", channels)
	}
	if Stable.Label() != "stable" || Dev.Label() != "dev" || releaseKey != upstreamReleaseKey {
		t.Fatal("upstream device labels or signing key changed")
	}
	if Stable.URL() != releases+"/latest/download/manifest.json" || Dev.URL() != releases+"/download/dev/manifest.json" {
		t.Fatal("upstream device channel paths changed")
	}
	if releases != "https://github.com/HuskerMinion/techo5-dot/releases" && releases != "https://github.com/HuskerMinion/techo5-spot/releases" {
		t.Fatalf("upstream device was sent to %q", releases)
	}
}
