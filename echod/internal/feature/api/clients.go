package api

import (
	"net"
	"sync"
	"sync/atomic"
)

// clients counts the connections open on the server now, for what wants to know whether anything it
// sends Home Assistant can arrive: an action called with no connection open goes nowhere, and says
// nothing (call). Counted where the connections are accepted, since the library keeps its own list to
// itself; one still shaking hands counts too, which is a moment, not a Home Assistant that is away.
type countListener struct {
	net.Listener
	n *atomic.Int32
}

func (l countListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.n.Add(1)
	return &countConn{Conn: c, n: l.n}, nil
}

// countConn takes itself off the count on its first Close; the library may close a connection twice.
type countConn struct {
	net.Conn
	n    *atomic.Int32
	once sync.Once
}

func (c *countConn) Close() error {
	c.once.Do(func() { c.n.Add(-1) })
	return c.Conn.Close()
}
