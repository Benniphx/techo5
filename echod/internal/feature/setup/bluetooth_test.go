package setup

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/btaudio"
)

// The Connections tab's Bluetooth section shows Secure Connections as it is, says what turning it off
// and changing it cost, and saves it both ways (#183).
func TestTheBluetoothSection(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	page := func() string {
		w := httptest.NewRecorder()
		bluetoothSection(w, "tok")
		return w.Body.String()
	}
	// A panel leaves the radio to Android, and offers no section at all.
	if !btaudio.Get().SecureConnectionsHere() {
		if p := page(); p != "" {
			t.Errorf("a device without the setting showed a Bluetooth section: %s", p)
		}
		return
	}
	p := page()
	if !strings.Contains(p, `name="sc" value="yes" style="width:auto" checked`) ||
		!strings.Contains(p, "drops for a moment") || !strings.Contains(p, `value="bluetooth"`) {
		t.Fatalf("on by default, with what it costs: %s", p)
	}

	if msg := saveBluetooth(form(url.Values{})); msg != "" || !config.Get().Bluetooth.SecureConnectionsOff {
		t.Fatalf("unticked: %q, off %v", msg, config.Get().Bluetooth.SecureConnectionsOff)
	}
	if strings.Contains(page(), " checked") {
		t.Error("off, the box is still ticked")
	}
	if msg := saveBluetooth(form(url.Values{"sc": {"yes"}})); msg != "" || config.Get().Bluetooth.SecureConnectionsOff {
		t.Errorf("ticked: %q, off %v", msg, config.Get().Bluetooth.SecureConnectionsOff)
	}
}
