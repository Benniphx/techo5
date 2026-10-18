package setup

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/HuskerMinion/techo5/echod/internal/feature/btaudio"
)

// bluetoothSection is the controller's Secure Connections (#183): on, as it comes, and off for a phone
// that pairs and is dropped a second or two later. On every device that offers it, the Dot too, whose
// only place for it besides Home Assistant this is. Not on athena on mainline, which has it off for good.
func bluetoothSection(w http.ResponseWriter, token string) {
	bt := btaudio.Get()
	if !bt.SecureConnectionsHere() {
		return
	}
	checked := ""
	if bt.SecureConnections() {
		checked = " checked"
	}
	fmt.Fprint(w, `<fieldset><legend>Bluetooth</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "bluetooth", "connections")
	fmt.Fprintf(w, `
	 <p><label><input type="checkbox" name="sc" value="yes" style="width:auto"%s> Secure Connections</label></p>
	 <p class="note">Leave it on unless a phone pairs with this device and is dropped a second or two later.
	  Off, such a phone pairs the older way and stays. A phone set to pair only with Secure Connections then
	  cannot pair at all.</p>
	 <p class="note">Changing it turns Bluetooth off and on again: whatever is connected drops for a moment.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`, checked)
}

func saveBluetooth(r *http.Request) string {
	bt := btaudio.Get()
	if !bt.SecureConnectionsHere() {
		return "this device has no such setting"
	}
	on := r.PostFormValue("sc") == "yes"
	if on == bt.SecureConnections() {
		return ""
	}
	if err := bt.SetSecureConnections(on); err != nil {
		return "could not save it"
	}
	slog.Info("setup page: bluetooth secure connections set", "on", on)
	return ""
}
