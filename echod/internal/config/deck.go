package config

import "slices"

// Deck is TECHO5 Deck: pages of buttons on the Show's screen that act straight on OBS (and, later, on a
// computer through the deck agent), opened by a swipe up from the bottom edge of the clock.
type Deck struct {
	// OBS is the OBS Studio the deck's OBS buttons act on.
	OBS DeckOBS `json:"obs,omitempty"`

	// Cols and Rows are the grid every page is laid out in; zero is the default for the screen.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// Pages are the deck's pages, the first one opening first. A page's Buttons are its squares
	// row by row; a square past the end of the list, or with no action, is empty.
	Pages []DeckPage `json:"pages,omitempty"`
}

// DeckOBS is where OBS's WebSocket server is (host or host:port; the port is 4455 unless given) and
// its password. The password stays on the device: the setup page only says whether one is set.
type DeckOBS struct {
	Addr     string `json:"addr,omitempty"`
	Password string `json:"password,omitempty"`
}

// DeckPage is one page of the deck.
type DeckPage struct {
	Buttons []DeckButton `json:"buttons,omitempty"`
}

// DeckButton is one square: what it says and shows, and what a press does.
type DeckButton struct {
	Label string `json:"label,omitempty"`
	// Icon is a Material Design icon name without its "mdi:" ("microphone-off"); empty is the
	// action's own.
	Icon string `json:"icon,omitempty"`
	// Color is one of DeckColors; empty is the action's own.
	Color string `json:"color,omitempty"`

	Action DeckAction `json:"action,omitempty"`
	// Scene is the scene an obs_scene button switches to, and the scene an obs_source button's
	// source is in; Source that source; Input the input an obs_mute button mutes.
	Scene  string `json:"scene,omitempty"`
	Source string `json:"source,omitempty"`
	Input  string `json:"input,omitempty"`
}

// DeckAction is what a deck button does.
type DeckAction string

const (
	DeckNone      DeckAction = ""
	DeckOBSScene  DeckAction = "obs_scene"  // switch to Scene
	DeckOBSStream DeckAction = "obs_stream" // start or stop streaming
	DeckOBSRecord DeckAction = "obs_record" // start or stop recording
	DeckOBSMute   DeckAction = "obs_mute"   // mute or unmute Input
	DeckOBSSource DeckAction = "obs_source" // show or hide Source in Scene
)

// DeckActions is every action, in the order a list shows them.
func DeckActions() []DeckAction {
	return []DeckAction{DeckNone, DeckOBSScene, DeckOBSStream, DeckOBSRecord, DeckOBSMute, DeckOBSSource}
}

// Label is how a list names an action.
func (a DeckAction) Label() string {
	switch a {
	case DeckOBSScene:
		return "OBS: switch scene"
	case DeckOBSStream:
		return "OBS: start/stop streaming"
	case DeckOBSRecord:
		return "OBS: start/stop recording"
	case DeckOBSMute:
		return "OBS: mute/unmute"
	case DeckOBSSource:
		return "OBS: show/hide a source"
	}
	return "Empty"
}

// DeckColors are the colors a button can be.
var DeckColors = []string{"", "blue", "green", "red", "orange", "purple", "gray"}

// The grid a deck can have, and what it is when nobody chose.
const (
	DeckColsMin, DeckColsMax, DeckColsDefault = 2, 6, 4
	DeckRowsMin, DeckRowsMax, DeckRowsDefault = 1, 4, 3
	DeckPagesMax                              = 10
	deckTextMax                               = 256
)

// Grid is the deck's columns and rows, the defaults filled in and both kept in range.
func (d Deck) Grid() (cols, rows int) {
	cols, rows = d.Cols, d.Rows
	if cols == 0 {
		cols = DeckColsDefault
	}
	if rows == 0 {
		rows = DeckRowsDefault
	}
	return min(max(cols, DeckColsMin), DeckColsMax), min(max(rows, DeckRowsMin), DeckRowsMax)
}

// Set is whether the deck has any button that does something: until then the bottom edge's swipe is
// the volume, as it always was.
func (d Deck) Set() bool {
	cols, rows := d.Grid()
	for _, p := range d.Pages {
		for i, b := range p.Buttons {
			if i < cols*rows && b.Action != DeckNone {
				return true
			}
		}
	}
	return false
}

// Button is page p's square i, or an empty one.
func (d Deck) Button(p, i int) DeckButton {
	cols, rows := d.Grid()
	if p < 0 || p >= len(d.Pages) || i < 0 || i >= cols*rows || i >= len(d.Pages[p].Buttons) {
		return DeckButton{}
	}
	return d.Pages[p].Buttons[i]
}

// clone copies the pages, so a snapshot does not share the store's slices.
func (d Deck) clone() Deck {
	d.Pages = slices.Clone(d.Pages)
	for i := range d.Pages {
		d.Pages[i].Buttons = slices.Clone(d.Pages[i].Buttons)
	}
	return d
}

// tidy keeps a button's text to a sane length, and its action and color to known ones.
func (b DeckButton) tidy() DeckButton {
	clip := func(s string) string {
		if len(s) > deckTextMax {
			return s[:deckTextMax]
		}
		return s
	}
	b.Label, b.Icon, b.Scene, b.Source, b.Input = clip(b.Label), clip(b.Icon), clip(b.Scene), clip(b.Source), clip(b.Input)
	if !slices.Contains(DeckActions(), b.Action) {
		b.Action = DeckNone
	}
	if !slices.Contains(DeckColors, b.Color) {
		b.Color = ""
	}
	return b
}

type DeckWriter struct{ st *Store }

// OBS sets where OBS is. An empty password keeps the one set before, the way the setup page's
// password fields work; Forget clears it.
func (w DeckWriter) OBS(addr, password string) error {
	return w.st.Update(func(c *Config) {
		c.Deck.OBS.Addr = addr
		if password != "" {
			c.Deck.OBS.Password = password
		}
	})
}

// ForgetOBSPassword clears OBS's password, for an OBS with authentication turned off.
func (w DeckWriter) ForgetOBSPassword() error {
	return w.st.Update(func(c *Config) { c.Deck.OBS.Password = "" })
}

// Grid sets the columns and rows, kept in range.
func (w DeckWriter) Grid(cols, rows int) error {
	return w.st.Update(func(c *Config) {
		c.Deck.Cols = min(max(cols, DeckColsMin), DeckColsMax)
		c.Deck.Rows = min(max(rows, DeckRowsMin), DeckRowsMax)
	})
}

// Button sets page p's square i, adding pages and squares up to it as needed.
func (w DeckWriter) Button(p, i int, b DeckButton) error {
	if p < 0 || p >= DeckPagesMax || i < 0 || i >= DeckColsMax*DeckRowsMax {
		return nil
	}
	return w.st.Update(func(c *Config) {
		for len(c.Deck.Pages) <= p {
			c.Deck.Pages = append(c.Deck.Pages, DeckPage{})
		}
		btns := c.Deck.Pages[p].Buttons
		for len(btns) <= i {
			btns = append(btns, DeckButton{})
		}
		btns[i] = b.tidy()
		c.Deck.Pages[p].Buttons = btns
	})
}

// AddPage adds an empty page at the end, up to DeckPagesMax.
func (w DeckWriter) AddPage() error {
	return w.st.Update(func(c *Config) {
		if len(c.Deck.Pages) < DeckPagesMax {
			c.Deck.Pages = append(c.Deck.Pages, DeckPage{})
		}
	})
}

// RemovePage takes page p out; the pages after it move up.
func (w DeckWriter) RemovePage(p int) error {
	return w.st.Update(func(c *Config) {
		if p >= 0 && p < len(c.Deck.Pages) {
			c.Deck.Pages = slices.Delete(c.Deck.Pages, p, p+1)
		}
	})
}
