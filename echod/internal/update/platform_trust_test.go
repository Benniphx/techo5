package update

import (
	"os"
	"testing"
)

// These public fixtures are signed by the real independent release keys. No private key is needed
// to prove that the fork rejects a genuine upstream manifest, even though its signature is valid.
func TestPlatformReleaseTrust(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		body, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	upstream := read("upstream-v0.9.30-manifest.json")
	upstreamSig := read("upstream-v0.9.30-manifest.json.sig")
	if err := verify(upstream, upstreamSig, upstreamReleaseKey); err != nil {
		t.Fatalf("invalid upstream fixture: %v", err)
	}
	if err := verify(upstream, upstreamSig, releaseKey); (err == nil) == nativeChannel {
		t.Fatalf("native=%v, upstream fixture verification: %v", nativeChannel, err)
	}
	native := read("native-trust-message.json")
	nativeSig := read("native-trust-message.json.sig")
	if err := verify(native, nativeSig, releaseKey); (err == nil) != nativeChannel {
		t.Fatalf("native=%v, fork fixture verification: %v", nativeChannel, err)
	}
	if nativeChannel {
		if err := verify(append(native, '\n'), nativeSig, releaseKey); err == nil {
			t.Fatal("changed fork-signed bytes were trusted")
		}
	}
}
