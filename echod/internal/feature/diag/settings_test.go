package diag

import (
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The newer switches are in a bundle, as on or off, and their secrets are not.
func TestTheBundleSaysWhichSwitchesAreOn(t *testing.T) {
	var c config.Config
	c.Remote.On, c.Remote.Secret = true, "remote-secret-value"
	c.CamStream.On, c.CamStream.Pass = true, "camera-password-value"
	c.Microphone.Claps = true
	got := settingsSummary(c)
	want := "switches: remote_api=true camera_streaming=true nearest=false noise_sensor=false claps=true clock_hold=false"
	if !strings.Contains(got, want) {
		t.Errorf("summary %q, want %q", got, want)
	}
	for _, secret := range []string{c.Remote.Secret, c.CamStream.Pass} {
		if strings.Contains(got, secret) {
			t.Errorf("the summary carries %q", secret)
		}
	}
}
