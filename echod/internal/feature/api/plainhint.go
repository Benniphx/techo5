package api

import (
	"net"
	"sync"
	"time"
)

// Home Assistant first tries a device without encryption, and learns that it needs a key from what
// the device says back: a frame that starts with Noise's indicator byte (aioesphomeapi raises
// RequiresEncryptionAPIError on it, and Home Assistant asks for the key). The ESPHome library hangs
// up on a plaintext hello without a word, so Home Assistant saw only the connection close and told
// people to put an api section in their YAML. Added by address, without discovery saying it is
// encrypted, a device could not be added at all (techo5-spot#3).
//
// plainHint answers a plaintext hello the way ESPHome's own firmware does, with an explicit
// handshake rejection, before the library sees the hello and closes the connection as it did.

// Frame indicators: plaintext ESPHome API, and Noise.
const (
	plaintextIndicator = 0x00
	noiseIndicator     = 0x01
)

// plainReject is ESPHome's explicit handshake rejection: a Noise frame (indicator, 16-bit length)
// whose payload is a failure byte and the reason.
var plainReject = func() []byte {
	reason := "Bad indicator byte"
	payload := append([]byte{0x01}, reason...)
	return append([]byte{noiseIndicator, byte(len(payload) >> 8), byte(len(payload))}, payload...)
}()

// hintListener hands out connections that answer a plaintext hello with plainReject.
type hintListener struct{ net.Listener }

func (l hintListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &hintConn{Conn: c}, nil
}

// hintConn looks at the first byte the client sends, once.
type hintConn struct {
	net.Conn
	once sync.Once
}

func (c *hintConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.once.Do(func() {
		if n > 0 && p[0] == plaintextIndicator {
			// Best effort, and short: the library is about to close the connection either way.
			_ = c.Conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Conn.Write(plainReject)
			_ = c.Conn.SetWriteDeadline(time.Time{})
		}
	})
	return n, err
}
