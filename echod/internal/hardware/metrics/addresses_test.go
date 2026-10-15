package metrics

import "testing"

func TestUSBGadgetLeftOut(t *testing.T) {
	for name, want := range map[string]bool{
		"rndis0": true, "usb0": true, "usb1": true, "br0": true,
		"wlan0": false, "eth0": false, "wg0": false, "lo": false,
	} {
		if got := usbGadget(name); got != want {
			t.Errorf("usbGadget(%q) = %v, want %v", name, got, want)
		}
	}
}
