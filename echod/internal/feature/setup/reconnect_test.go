package setup

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A reconnect waits for the device to be idle, and to stay idle a moment; several asked for while it
// waits are the one reconnect.
func TestTheReconnectWaitsForIdleAndIsOne(t *testing.T) {
	var busy atomic.Bool
	busy.Store(true)
	var emitted atomic.Int32
	done := make(chan struct{}, 4)
	r := &idleReconnect{
		busy:    busy.Load,
		emit:    func() { emitted.Add(1); done <- struct{}{} },
		look:    time.Millisecond,
		settle:  20 * time.Millisecond,
		patient: time.Hour,
	}
	for range 3 {
		r.ask()
	}
	time.Sleep(50 * time.Millisecond)
	if emitted.Load() != 0 {
		t.Fatal("reconnected while the device was busy")
	}
	busy.Store(false)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("never reconnected once the device was idle")
	}
	time.Sleep(50 * time.Millisecond)
	if n := emitted.Load(); n != 1 {
		t.Errorf("reconnected %d times for three asks while waiting", n)
	}

	// Asked again after that one went, there is another.
	r.ask()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a second ask was never answered")
	}
}

// A device busy on and off, never quiet for long enough, still reconnects at an idle moment once it
// has waited long enough, and never while busy.
func TestTheReconnectStopsWaitingForASettledMoment(t *testing.T) {
	var mu sync.Mutex
	looks := 0
	busyAtEmit := false
	busy := func() bool {
		mu.Lock()
		defer mu.Unlock()
		looks++
		return looks%2 == 0 // idle every other look: never settled
	}
	done := make(chan struct{})
	r := &idleReconnect{
		busy: busy,
		emit: func() {
			mu.Lock()
			busyAtEmit = looks%2 == 0
			mu.Unlock()
			close(done)
		},
		look:    time.Millisecond,
		settle:  time.Hour,
		patient: 20 * time.Millisecond,
	}
	r.ask()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("never reconnected")
	}
	if busyAtEmit {
		t.Error("reconnected on a busy look")
	}
}
