//go:build !dot

package dashboard

import (
	"encoding/json"
	"image"
	"math"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// The card from #138, with a few more elements: each is placed in fractions of the picture, centered
// on its point as Home Assistant centers it unless its transform says otherwise, with its own tap.
// One placed in pixels, a custom one and one the device does not know are left out, and the card is
// still drawn; a conditional one is there only while its condition holds.
func TestPictureElementsAreReadAsHomeAssistantPlacesThem(t *testing.T) {
	var cards []any
	if err := json.Unmarshal([]byte(`[{
	  "type": "picture-elements",
	  "image": "/local/alfred/page_musique.png",
	  "title": "Music",
	  "elements": [
	    {"type": "icon", "icon": "mdi:spotify", "style": {"left": "17%", "top": "25%", "width": "30%", "height": "45%"},
	     "tap_action": {"action": "navigate", "navigation_path": "/alfred-show/spotify"}},
	    {"type": "icon", "icon": "mdi:radio", "style": {"left": "50%", "top": "25%", "width": "30%", "height": "45%"},
	     "tap_action": {"action": "perform-action", "perform_action": "script.radio"}},
	    {"type": "image", "entity": "light.desk", "image": "/local/clear.png", "style": {"left": "0", "top": "50%", "transform": "none"},
	     "tap_action": {"action": "toggle"}},
	    {"type": "state-icon", "entity": "light.desk", "style": {"left": "80%", "top": "80%", "transform": "translate(-25%, 10%) scale(2)"}},
	    {"type": "state-label", "entity": "sensor.temp", "prefix": "In ", "style": {"left": "90%", "top": "10%"}},
	    {"type": "service-button", "title": "Lights out", "action": "light.turn_off", "data": {"entity_id": "all"}, "style": {"left": "50%", "top": "90%"}},
	    {"type": "icon", "icon": "mdi:cog", "style": {"left": "100px", "top": "20px"}, "tap_action": {"action": "navigate", "navigation_path": "/x"}},
	    {"type": "custom:sparkle-element", "style": {"left": "10%", "top": "10%"}},
	    {"type": "conditional", "conditions": [{"entity": "light.desk", "state": "on"}], "elements": [
	      {"type": "icon", "icon": "mdi:lamp", "style": {"left": "5%", "top": "5%"}, "tap_action": {"action": "navigate", "navigation_path": "/lamp"}}
	    ]}
	  ]}]`), &cards); err != nil {
		t.Fatal(err)
	}
	c := &compiler{seen: map[string]bool{}, need: needs{graphs: map[string]int{}}}
	nodes := c.cards(cards)
	if len(c.need.pictures) != 1 || c.need.pictures[0].url != "/local/alfred/page_musique.png" || !c.need.pictures[0].wide {
		t.Fatalf("pictures %+v", c.need.pictures)
	}
	if !c.seen["light.desk"] || !c.seen["sensor.temp"] {
		t.Errorf("following %v, want the state icon's and the state label's entities", c.need.entities)
	}
	lk := look{states: map[string]hass.LiveEntity{
		"light.desk":  {ID: "light.desk", State: "off", Attrs: map[string]any{}},
		"sensor.temp": {ID: "sensor.temp", State: "21.46", Attrs: map[string]any{"unit_of_measurement": "°C"}},
	}, history: map[string][]point{}, pictures: map[string]image.Image{}}
	blocks := group(nodes).blocks(lk)
	if len(blocks) != 1 || blocks[0].Picture == nil {
		t.Fatalf("blocks %+v", blocks)
	}
	p := blocks[0].Picture
	if p.Name != "" {
		t.Errorf("name %q: no band across a page of buttons", p.Name)
	}
	els := p.Elements
	if len(els) != 6 {
		t.Fatalf("%d elements, want 6: %+v", len(els), els)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if e := els[0]; !near(e.X, 0.17) || !near(e.Y, 0.25) || !near(e.W, 0.30) || !near(e.H, 0.45) ||
		e.ShiftX != -0.5 || e.ShiftY != -0.5 || e.Tap == nil || e.Tap.View != "alfred-show/spotify" || e.Icon != "" {
		t.Errorf("spotify %+v", e)
	}
	if e := els[1]; e.Tap == nil || e.Tap.Service != "script.radio" {
		t.Errorf("radio %+v", e.Tap)
	}
	if e := els[2]; e.X != 0 || e.ShiftX != 0 || e.ShiftY != 0 || e.Tap == nil || e.Tap.Service != "homeassistant.toggle" || e.Tap.Entity != "light.desk" {
		t.Errorf("image %+v", e)
	}
	if e := els[3]; !near(e.ShiftX, -0.25) || !near(e.ShiftY, 0.10) || e.Icon == "" || e.On || e.Tap != nil {
		t.Errorf("state icon %+v", e)
	}
	if e := els[4]; e.Label != "In 21.5 °C" {
		t.Errorf("state label %q", e.Label)
	}
	if e := els[5]; e.Label != "Lights out" || e.Tap == nil || e.Tap.Service != "light.turn_off" || e.Tap.Data["entity_id"] != "all" {
		t.Errorf("service button %+v", e)
	}

	lk.states["light.desk"] = hass.LiveEntity{ID: "light.desk", State: "on", Attrs: map[string]any{}}
	els = group(nodes).blocks(lk)[0].Picture.Elements
	if len(els) != 7 || !els[3].On || els[6].Tap == nil || els[6].Tap.View != "lamp" {
		t.Errorf("light on: %+v", els)
	}
}

// A grid of pictures with one picture-elements card among them is not a gallery, whose frames would
// cut its picture and move its elements off what they are on.
func TestPictureElementsAreNotCutToAGallery(t *testing.T) {
	var cards []any
	if err := json.Unmarshal([]byte(`[{"type": "grid", "columns": 2, "cards": [
	  {"type": "picture", "image": "/local/a.png"},
	  {"type": "picture-elements", "image": "/local/b.png", "elements": [{"type": "icon", "style": {"left": "50%", "top": "50%"}}]}
	]}]`), &cards); err != nil {
		t.Fatal(err)
	}
	c := &compiler{seen: map[string]bool{}, need: needs{graphs: map[string]int{}}}
	blocks := group(c.cards(cards)).blocks(look{pictures: map[string]image.Image{}})
	if len(blocks) != 2 || blocks[0].Picture == nil || blocks[1].Picture == nil || len(blocks[1].Picture.Elements) != 1 {
		t.Errorf("blocks %+v", blocks)
	}
}

// A length is read in percent, or a bare zero; pixels and the rest are not.
func TestPercent(t *testing.T) {
	for in, want := range map[any]float64{"17%": 0.17, " 50 %": 0.5, "0": 0, 0.0: 0, "-10%": -0.1, "100%": 1} {
		if got, ok := percent(in); !ok || math.Abs(got-want) > 1e-9 {
			t.Errorf("percent(%v) = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []any{"20px", "calc(50% - 10px)", 12.0, nil, "x%"} {
		if _, ok := percent(in); ok {
			t.Errorf("percent(%v) read", in)
		}
	}
}

// A picture that may fill the screen is kept as wide as it; any other, as wide as a card.
func TestAWidePictureIsKeptWide(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 1920, 960))
	if b := shrink(big, wanted{wide: true}.width()).Bounds(); b.Dx() != screenWidth || b.Dy() != 960*screenWidth/1920 {
		t.Errorf("wide: %v", b)
	}
	if b := shrink(big, wanted{}.width()).Bounds(); b.Dx() != pictureWidth {
		t.Errorf("card: %v", b)
	}
}
