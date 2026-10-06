package voice

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/wakeword"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/led"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// thinkingTurn is a conversation that has stopped listening and is waiting on its answer. The loop is
// not running: the test hands it events, and settle handles what the turn posts to itself (the reply's
// errand, an announcement) as the loop would.
func thinkingTurn(t *testing.T) *conversation {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	c := newConversation(&esphome.VoiceSatellite{})
	c.claim = c.leds.Claim(led.PriorityTurn)
	t.Cleanup(func() {
		c.handle(event{kind: evCancel})
		c.claim.Release()
	})
	c.think()
	return c
}

// settle handles what the turn posts to itself for a while.
func settle(c *conversation, d time.Duration) {
	end := time.After(d)
	for {
		select {
		case e := <-c.events:
			c.handle(e)
		case <-end:
			return
		}
	}
}

// heldSpeech serves a second of silence as a whole WAVE file, once release is closed: until then it is
// Home Assistant still synthesizing it.
func heldSpeech(t *testing.T) (url string, release chan struct{}) {
	t.Helper()
	release = make(chan struct{})
	pcm := make([]byte, speaker.VoiceRate*2)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(speaker.VoiceRate))
	binary.Write(&b, binary.LittleEndian, uint32(speaker.VoiceRate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(b.Bytes())
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	return srv.URL + "/reply.wav", release
}

// Canceling a reply that is still downloading is the turn being stopped, not the url failing. The
// fetch fails with the context canceled all the same, and that was taken as Home Assistant's url being
// out of reach: the wake word moved to streamed replies for good.
func TestACanceledReplyKeepsItsDelivery(t *testing.T) {
	c := thinkingTurn(t)
	url, _ := heldSpeech(t)

	c.handle(event{kind: evReplyText, text: "It's noon."})
	c.handle(event{kind: evReplyURL, url: url})
	settle(c, 100*time.Millisecond)
	c.handle(event{kind: evCancel})
	settle(c, 300*time.Millisecond)

	if got := wakeword.Delivery(0); got != config.DeliveryWhole {
		t.Fatalf("after a cancel the wake word's delivery is %q, want %q", got, config.DeliveryWhole)
	}
}
