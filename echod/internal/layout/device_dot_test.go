//go:build dot

package layout

import "testing"

// idme's product id decides, as read with the NUL and newline it can end in; Fire OS's product name
// stands in where idme says nothing we know; anything else is run as a Dot and reported.
func TestDotBoard(t *testing.T) {
	cases := []struct {
		typeID, product string
		want            string
		known           bool
	}{
		{"A7WXQPH584YP\x00", "", boardRadar, true},
		{"A7WXQPH584YP\n", "", boardRadar, true},
		{"A3S5BH2HU6VAYF", "", boardBiscuit, true},
		{"A3S5BH2HU6VAYF\x00\n", "radar_puffin", boardBiscuit, true},
		{"", "radar_puffin\n", boardRadar, true},
		{"", "biscuit_puffin\n", boardBiscuit, true},
		{"AXXXXXXXXXXX", "", boardBiscuit, false},
		{"", "rook\n", boardBiscuit, false},
		{"", "", boardBiscuit, false},
		// Only the whole id counts: one that merely contains a known one is not it.
		{"A7WXQPH584YPX", "", boardBiscuit, false},
	}
	for _, c := range cases {
		got, known := dotBoard(c.typeID, c.product)
		if got != c.want || known != c.known {
			t.Errorf("dotBoard(%q, %q) = %q, %v; want %q, %v", c.typeID, c.product, got, known, c.want, c.known)
		}
	}
}

// Each board reports its own model and default name; an unknown one gets the Dot's.
func TestBoardInfo(t *testing.T) {
	cases := []struct{ board, wantBoard, model, name string }{
		{boardRadar, "radar", "Echo 2nd gen (radar)", "Echo"},
		{boardBiscuit, "biscuit", "Echo Dot 2 (biscuit)", "Echo Dot"},
		{"", "biscuit", "Echo Dot 2 (biscuit)", "Echo Dot"},
	}
	for _, c := range cases {
		b, m, n := boardInfo(c.board)
		if b != c.wantBoard || m != c.model || n != c.name {
			t.Errorf("boardInfo(%q) = %q, %q, %q; want %q, %q, %q", c.board, b, m, n, c.wantBoard, c.model, c.name)
		}
	}
}
