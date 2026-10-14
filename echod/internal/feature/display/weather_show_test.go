//go:build !dot && !spot

package display

import (
	"fmt"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/screen"
)

// runAction runs one of the display's actions by its name, as Home Assistant would.
func runAction(t *testing.T, d *Display, name string) error {
	t.Helper()
	for _, a := range d.Actions() {
		if a.Name == name {
			_, err := a.Run(esphome.Call{})
			return err
		}
	}
	t.Fatalf("no %s action", name)
	return nil
}

// weather_show puts the forecast up and radar_show the rain map, each over the other, as the page's
// own button turns between them.
func TestWeatherShowAndRadarShow(t *testing.T) {
	d := nightTestDisplay(t)
	if err := runAction(t, d, "weather_show"); err != nil {
		t.Fatalf("weather_show: %v", err)
	}
	if !time.Now().Before(d.weatherUntil) || d.radar {
		t.Fatalf("weather_show: up until %v, radar %v", d.weatherUntil, d.radar)
	}
	if err := runAction(t, d, "radar_show"); err != nil {
		t.Fatalf("radar_show: %v", err)
	}
	if !time.Now().Before(d.weatherUntil) || !d.radar {
		t.Fatalf("radar_show: up until %v, radar %v", d.weatherUntil, d.radar)
	}
	if err := runAction(t, d, "weather_show"); err != nil || d.radar {
		t.Errorf("weather_show over the rain map: %v, radar %v", err, d.radar)
	}
}

// With the weather set to none, neither opens, and Home Assistant is told why.
func TestNoWeatherNoWeatherShow(t *testing.T) {
	d := nightTestDisplay(t)
	if err := config.Set().Home().Weather(config.WeatherOff); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"weather_show", "radar_show"} {
		if err := runAction(t, d, name); err == nil {
			t.Errorf("%s worked with no weather", name)
		}
	}
	if !d.weatherUntil.IsZero() || d.radar {
		t.Errorf("the weather page went up anyway: until %v, radar %v", d.weatherUntil, d.radar)
	}
}

// At night, weather_show and radar_show on a screen the night put out light it for the page, at the
// night light's level when one is set and at the night's half brightness when not, and the night
// leaves it lit while the page is up: the action no longer works on a screen nobody can see. Once the
// page has gone, the night has the screen back.
func TestWeatherShowLiftsTheNight(t *testing.T) {
	for _, nightLight := range []bool{true, false} {
		d := nightTestDisplay(t)
		d.view = voice.State{Phase: "idle"}
		d.light = &esphome.Light{
			Base:                esphome.Base{ObjectID: "screen", Name: "Screen", Icon: "mdi:monitor"},
			SupportedColorModes: []esphome.ColorMode{esphome.ColorModeBrightness},
		}
		h := time.Now().Hour()
		if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
			t.Fatal(err)
		}
		if err := config.Set().Screen().NightLight(nightLight); err != nil {
			t.Fatal(err)
		}
		d.touchedAt = time.Now().Add(-time.Hour)
		d.on, d.nightDark, d.ceiling = false, true, 100

		if err := runAction(t, d, "radar_show"); err != nil {
			t.Fatalf("night light %v: radar_show: %v", nightLight, err)
		}
		d.mu.Lock()
		on, dark, glow, asked, level := d.on, d.nightDark, d.nightGlow, d.askedUp(time.Now()), d.level
		d.mu.Unlock()
		if !on || dark || !asked || glow != nightLight {
			t.Fatalf("night light %v: on %v, dark %v, glowing %v, asked %v", nightLight, on, dark, glow, asked)
		}
		if !nightLight && level > screen.BacklightMax/2 {
			t.Errorf("lit at %v at night, over half of %v", level, screen.BacklightMax)
		}
		d.night(time.Now(), true, d.view)
		if d.nightDark || !d.on {
			t.Errorf("night light %v: the night put the screen out under the rain map", nightLight)
		}

		// Put away: the night has it again.
		d.mu.Lock()
		d.weatherUntil = time.Time{}
		d.mu.Unlock()
		d.night(time.Now(), true, d.view)
		if !nightLight && !d.nightDark {
			t.Errorf("the screen stayed lit after the rain map went")
		}
	}
}

// By day, and on a screen somebody brought up at night, weather_show changes nothing about the light.
func TestWeatherShowLeavesALitScreen(t *testing.T) {
	d := nightTestDisplay(t)
	h := time.Now().Hour()
	if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
		t.Fatal(err)
	}
	d.on = true
	if err := runAction(t, d, "weather_show"); err != nil {
		t.Fatal(err)
	}
	if !d.askedNight.IsZero() {
		t.Errorf("a screen already up was taken over for the night: until %v", d.askedNight)
	}
}
