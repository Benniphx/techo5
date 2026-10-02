//go:build !dot

package dashboard

import (
	"testing"
	"time"
)

func TestRealtimeLeaseRetainsStreamPastCleanupAndReleasesOnce(t *testing.T) {
	f := &Feature{}
	s := &stream{f: f}
	f.stream = s
	f.streamUsed = time.Now().Add(-2 * unused)
	release := f.HoldStream()
	other := f.HoldStream()
	f.closeUnused()
	if f.stream != s || s.stopped {
		t.Fatal("long voice conversation reconnected dashboard")
	}
	release()
	release()
	if f.leases != 1 {
		t.Fatal("release not idempotent")
	}
	other()
	if f.leases != 0 || time.Since(f.streamUsed) > time.Second {
		t.Fatal("cleanup races return to dashboard")
	}
	f.closeUnused()
	if f.stream != s {
		t.Fatal("first idle frame lost connection")
	}
	f.streamUsed = time.Now().Add(-2 * unused)
	f.closeUnused()
	if f.stream != nil || !s.stopped {
		t.Fatal("expired idle stream leaked")
	}
}
func TestExplicitCloseOverridesLease(t *testing.T) {
	f := &Feature{}
	s := &stream{f: f}
	f.stream = s
	release := f.HoldStream()
	defer release()
	f.Close()
	if f.stream != nil || !s.stopped {
		t.Fatal("old server/path ignored change while leased")
	}
}
