//go:build !dot

package camera

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The camera is not opened until the latch has read released for a while: an open while it still holds
// the camera off is what wedges the sensor (#17).
func TestTheCameraWaitsForTheLatch(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	oldState, oldSettle := gatingState, latchSettle
	gatingState, latchSettle = state, 300*time.Millisecond
	t.Cleanup(func() { gatingState, latchSettle = oldState, oldSettle })
	if err := os.WriteFile(state, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan time.Time, 1)
	start := time.Now()
	go func() {
		if latchReleased(make(chan struct{})) {
			done <- time.Now()
		}
	}()
	time.Sleep(400 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("opened while the latch held the camera off")
	default:
	}
	if err := os.WriteFile(state, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-done:
		if at.Sub(start) < 600*time.Millisecond {
			t.Errorf("opened %v after the latch let go; wanted the settle first", at.Sub(start)-400*time.Millisecond)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("never opened after the latch let go")
	}

	// No latch on this device: no wait.
	gatingState = filepath.Join(dir, "none")
	if !latchReleased(make(chan struct{})) {
		t.Error("a device without the latch waited")
	}
}
