//go:build !dot

package dashboard

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// A picture-elements card's elements: places on its picture, each with a tap of its own, so that one
// picture drawn for the screen can be a whole page of buttons. They are placed as Home Assistant
// places them, so the same card works the same on both: an element is position: absolute in the
// picture, its style's left and top in percent, and Home Assistant's own transform,
// translate(-50%, -50%), centers it on that point (hui-picture-elements-card's .element). A state
// icon and a state label show their entity; an icon, an image, a state badge and a service button
// are only their tap here, the picture being what shows them. Any other element, a custom one or one
// placed in pixels, is left out, and the rest of the card is drawn as usual.

// element is one element as the card gives it.
type element struct {
	kind                      string
	entity                    string
	x, y, w, h                float64
	sx, sy                    float64
	icon, title               string
	attribute, prefix, suffix string
	tap                       *Action
	conds                     []cond // a conditional element's, all of them met for it to be there
}

// elementKinds are the elements the device places; anything else is left out.
var elementKinds = map[string]bool{
	"icon": true, "image": true, "state-icon": true, "state-label": true, "state-badge": true, "service-button": true,
}

// elements reads a card's elements, those in a conditional element too, behind its conditions as
// well as conds.
func (c *compiler) elements(v any, conds []cond) []element {
	list, _ := v.([]any)
	var out []element
	for _, it := range list {
		m, ok := it.(raw)
		if !ok {
			continue
		}
		kind := str(m, "type")
		if kind == "conditional" {
			inner := append(append([]cond{}, conds...), c.conditions(m["conditions"])...)
			out = append(out, c.elements(m["elements"], inner)...)
			continue
		}
		if !elementKinds[kind] {
			continue
		}
		style, _ := m["style"].(raw)
		x, okx := percent(style["left"])
		y, oky := percent(style["top"])
		if !okx || !oky {
			continue
		}
		e := element{kind: kind, entity: str(m, "entity"), x: x, y: y, icon: str(m, "icon"), title: str(m, "title"),
			attribute: str(m, "attribute"), prefix: str(m, "prefix"), suffix: str(m, "suffix"), conds: conds}
		e.w, _ = percent(style["width"])
		e.h, _ = percent(style["height"])
		e.sx, e.sy = shiftOf(style)
		if (kind == "state-icon" || kind == "state-label") && e.entity == "" {
			continue
		}
		if kind == "state-icon" || kind == "state-label" {
			c.follow(e.entity)
		}
		e.tap = pictureTap(m, e.entity)
		if kind == "service-button" {
			e.tap = serviceButton(m)
		}
		out = append(out, e)
	}
	return out
}

// serviceButton is what a service button element runs: its action and data, as the card writes them.
func serviceButton(m raw) *Action {
	service := str(m, "action")
	if service == "" {
		service = str(m, "service")
	}
	if !strings.Contains(service, ".") {
		return nil
	}
	data, _ := m["data"].(raw)
	if data == nil {
		data, _ = m["service_data"].(raw)
	}
	out := map[string]any{}
	for k, v := range data {
		out[k] = v
	}
	if target, ok := m["target"].(raw); ok {
		for k, v := range target {
			out[k] = v
		}
	}
	return &Action{Service: service, Data: out}
}

// percent is a style's length in percent, as a fraction: "17%" is 0.17. A zero needs no unit; pixels
// and anything else are not read, as there is no telling what they come to on another screen.
func percent(v any) (float64, bool) {
	s, ok := v.(string)
	if !ok {
		if f, ok := v.(float64); ok && f == 0 {
			return 0, true
		}
		return 0, false
	}
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, true
	}
	n, isPct := strings.CutSuffix(s, "%")
	if !isPct {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
	if err != nil {
		return 0, false
	}
	return f / 100, true
}

var translate = regexp.MustCompile(`translate\(\s*(-?[\d.]+)%\s*(?:,\s*(-?[\d.]+)%\s*)?\)`)

// shiftOf is how far an element's style moves it by its own size: Home Assistant's translate(-50%,
// -50%) when it gives no transform, the transform's own translate when it gives one, and nothing for
// a transform without one ("none", or a translate in pixels), which replaces Home Assistant's.
func shiftOf(style raw) (float64, float64) {
	t, ok := style["transform"].(string)
	if !ok {
		return -0.5, -0.5
	}
	m := translate.FindStringSubmatch(t)
	if m == nil {
		return 0, 0
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	y := 0.0
	if m[2] != "" {
		y, _ = strconv.ParseFloat(m[2], 64)
	}
	return x / 100, y / 100
}

// at is the element as it stands, or false while its conditions are not met.
func (e element) at(l look) (Element, bool) {
	if !all(e.conds, l) {
		return Element{}, false
	}
	out := Element{X: e.x, Y: e.y, W: e.w, H: e.h, ShiftX: e.sx, ShiftY: e.sy, Entity: e.entity, Tap: e.tap}
	switch e.kind {
	case "state-icon", "state-label":
		st, ok := l.states[e.entity]
		if !ok {
			st = hass.LiveEntity{ID: e.entity, State: "unknown", Attrs: map[string]any{}}
		}
		t := describe(st, "")
		out.On, out.Gone = t.On, t.Gone
		if e.kind == "state-icon" {
			out.Icon = orIcon(e.icon, t.Icon)
			break
		}
		v := t.Value
		if e.attribute != "" {
			v = ""
			if a, ok := st.Attrs[e.attribute]; ok && a != nil {
				v = fmt.Sprint(a)
			}
		}
		out.Label = e.prefix + v + e.suffix
	case "service-button":
		out.Label = e.title
	}
	return out, true
}
