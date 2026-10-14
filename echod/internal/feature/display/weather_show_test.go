//go:build !dot && !spot

package display

import (
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
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
