package api

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/component"
)

// The connections open are counted as they are accepted and closed, a connection closed twice once, and
// an action called with none open tells the caller nobody took it.
func TestAnActionWithNobodyConnectedIsUnheard(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var n atomic.Int32
	cl := countListener{ln, &n}
	dial, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dial.Close()
	c, err := cl.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("%d open after one was accepted", n.Load())
	}
	_ = c.Close()
	_ = c.Close()
	if n.Load() != 0 {
		t.Fatalf("%d open after it was closed twice", n.Load())
	}

	a := &API{}
	unheard := 0
	call := component.Call{Service: "script.example", Unheard: func() { unheard++ }}
	a.call(call)
	a.srv.Store(a.serverFor(nil))
	a.call(call)
	if unheard != 2 {
		t.Errorf("%d of two actions with nobody connected, served or not, were unheard", unheard)
	}
	a.clients.Add(1)
	a.call(call)
	if unheard != 2 {
		t.Error("an action with Home Assistant connected was unheard")
	}
}
