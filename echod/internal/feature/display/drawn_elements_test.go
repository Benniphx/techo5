//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
)

// buttonsPage is a page drawn as one picture, as #138 asked for: twelve buttons, four across and
// three down, at the Show 5's own size, and an element over each, centered on it in percent the way
// Home Assistant places them, each going to a view of its own. A state icon and a state label sit
// over two of them.
func buttonsPage() (dashboard.Drawn, []image.Rectangle) {
	img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	draw.Draw(img, img.Rect, image.NewUniform(color.RGBA{24, 28, 40, 255}), image.Point{}, draw.Src)
	var els []dashboard.Element
	var buttons []image.Rectangle
	for i := range 12 {
		col, row := i%4, i/4
		// Centers at 12.5%, 37.5%, ... across and 1/6, 1/2, 5/6 down; each 22% wide and 28% high.
		x, y := (float64(col)+0.5)/4, (float64(row)+0.5)/3
		w, h := 0.22, 0.28
		b := image.Rect(int((x-w/2)*showWide+0.5), int((y-h/2)*showHigh+0.5), int((x+w/2)*showWide+0.5), int((y+h/2)*showHigh+0.5))
		hue := color.RGBA{uint8(60 + 15*i), uint8(200 - 12*i), uint8(90 + 10*row*4), 255}
		draw.Draw(img, b, image.NewUniform(hue), image.Point{}, draw.Src)
		buttons = append(buttons, b)
		els = append(els, dashboard.Element{X: x, Y: y, W: w, H: h, ShiftX: -0.5, ShiftY: -0.5,
			Tap: &dashboard.Action{View: fmt.Sprintf("panel/button-%d", i)}})
	}
	els = append(els,
		dashboard.Element{X: 0.125, Y: 1.0 / 6, ShiftX: -0.5, ShiftY: -0.5, Icon: "lightbulb", On: true, Entity: "light.desk"},
		dashboard.Element{X: 0.375, Y: 1.0 / 6, ShiftX: -0.5, ShiftY: -0.5, Label: "21.5 °C", Entity: "sensor.temp"},
	)
	return dashboard.Drawn{Sections: []dashboard.Section{{Blocks: []dashboard.Block{
		{Picture: &dashboard.Picture{Image: img, Elements: els}},
	}}}}, buttons
}

// actionAt is the action of the place to tap under p, as a tap finds it: the first one kept there.
func actionAt(tiles []dashTile, p image.Point) *dashboard.Action {
	for _, t := range tiles {
		if p.In(t.r) {
			return t.action
		}
	}
	return nil
}

// A view of one picture-elements card fills the screen with its picture, whole: on the Show 5 the
// picture is the screen's own size and every button is a tap where it is drawn, the state icon and
// label without a tap of their own not hiding the buttons under them; on the Show 8, a wider shape
// than the screen, it is as wide as the screen with the page's color above and below it, and the
// buttons are still where they are drawn. With SHOW_PREVIEW set, each is written there to look at.
func TestAPictureElementsPageFillsTheScreen(t *testing.T) {
	previewFont(t)
	dir := os.Getenv("SHOW_PREVIEW")
	at := time.Date(2026, 10, 14, 9, 30, 0, 0, time.Local)
	page, buttons := buttonsPage()
	for _, panel := range []struct {
		name       string
		wide, high int
		pic        image.Rectangle // where the picture should be drawn
	}{
		{"", showWide, showHigh, image.Rect(0, 0, showWide, showHigh)},
		{"-show8", show8Wide, show8High, image.Rect(0, 80, show8Wide, 720)},
	} {
		img := image.NewRGBA(image.Rect(0, 0, panel.wide, panel.high))
		r := newRenderer(img)
		r.draw(scene{now: at, phase: "idle", showDash: true, dashMode: config.DashboardDrawn, drawn: page})
		tiles, content := r.dash()
		if len(tiles) != 12 {
			t.Fatalf("%s: %d places to tap, want 12", panel.name, len(tiles))
		}
		if content != panel.high {
			t.Errorf("%s: scrolls to %d, want the screen's %d: a page that does not scroll", panel.name, content, panel.high)
		}
		sx := float64(panel.pic.Dx()) / showWide
		sy := float64(panel.pic.Dy()) / showHigh
		for i, b := range buttons {
			// The button as drawn on this screen, a pixel in from its edges for the scaling's rounding.
			on := image.Rect(panel.pic.Min.X+int(float64(b.Min.X)*sx)+2, panel.pic.Min.Y+int(float64(b.Min.Y)*sy)+2,
				panel.pic.Min.X+int(float64(b.Max.X)*sx)-2, panel.pic.Min.Y+int(float64(b.Max.Y)*sy)-2)
			want := fmt.Sprintf("panel/button-%d", i)
			for _, p := range []image.Point{on.Min, {on.Max.X - 1, on.Max.Y - 1}, {(on.Min.X + on.Max.X) / 2, (on.Min.Y + on.Max.Y) / 2}} {
				if a := actionAt(tiles, p); a == nil || a.View != want {
					t.Errorf("%s: a tap at %v on button %d does %+v, want %s", panel.name, p, i, a, want)
				}
			}
			// And between the buttons, nothing.
			gap := image.Pt(on.Max.X+int(4*sx)+2, on.Max.Y+int(4*sy)+2)
			if i%4 != 3 && i/4 != 2 {
				if a := actionAt(tiles, gap); a != nil {
					t.Errorf("%s: a tap between the buttons at %v does %+v", panel.name, gap, a)
				}
			}
		}
		if panel.pic.Min.Y > 0 {
			if c := img.RGBAAt(panel.wide/2, panel.pic.Min.Y/2); c == (color.RGBA{24, 28, 40, 255}) {
				t.Errorf("%s: the picture was stretched over the page's color above it", panel.name)
			}
			if c := img.RGBAAt(panel.wide/2, panel.pic.Min.Y+panel.pic.Dy()/2); c != (color.RGBA{24, 28, 40, 255}) {
				t.Errorf("%s: the picture is not where it should be: %v at its middle", panel.name, c)
			}
		}
		if dir != "" {
			f, err := os.Create(filepath.Join(dir, "dash-picture-elements"+panel.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
	}
}

// A picture-elements card among other cards is drawn whole across its column, and its elements are
// where they are on it; before its picture has arrived, nothing on it is tapped.
func TestAPictureElementsCardInAColumn(t *testing.T) {
	page, _ := buttonsPage()
	page.Sections[0].Blocks = append([]dashboard.Block{{Heading: "Music"}}, page.Sections[0].Blocks...)
	img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	r := newRenderer(img)
	r.draw(scene{now: time.Now(), phase: "idle", showDash: true, dashMode: config.DashboardDrawn, drawn: page})
	tiles, _ := r.dash()
	if len(tiles) != 12 {
		t.Fatalf("%d places to tap, want 12", len(tiles))
	}
	// The first button's box is on the left of the first column, under the heading.
	if b := tiles[11].r; b.Min.X < r.s(dashSide) || b.Min.Y < r.s(headingH) || b.Max.X > showWide/2 {
		t.Errorf("first button at %v", b)
	}
	page.Sections[0].Blocks[1].Picture.Image = nil
	r.draw(scene{now: time.Now(), phase: "idle", showDash: true, dashMode: config.DashboardDrawn, drawn: page})
	if tiles, _ := r.dash(); len(tiles) != 0 {
		t.Errorf("%d places to tap on a picture not yet arrived", len(tiles))
	}
}

// An element is its size in percent of the picture, moved by its shift: centered on its point by
// Home Assistant's default, its top left there with transform none; with no size, natural pixels.
func TestElementBox(t *testing.T) {
	pic := image.Rect(100, 50, 500, 250) // 400 by 200
	for _, c := range []struct {
		e    dashboard.Element
		want image.Rectangle
	}{
		{dashboard.Element{X: 0.5, Y: 0.5, W: 0.25, H: 0.5, ShiftX: -0.5, ShiftY: -0.5}, image.Rect(250, 100, 350, 200)},
		{dashboard.Element{X: 0.5, Y: 0.5, W: 0.25, H: 0.5}, image.Rect(300, 150, 400, 250)},
		{dashboard.Element{X: 0.17, Y: 0.25, ShiftX: -0.5, ShiftY: -0.5}, image.Rect(144, 76, 192, 124)},
		{dashboard.Element{X: 0, Y: 0, W: 0.1, H: 0.1, ShiftX: -0.25, ShiftY: 0.1}, image.Rect(90, 52, 130, 72)},
	} {
		if got := elementBox(c.e, pic, 48); got != c.want {
			t.Errorf("elementBox(%+v) = %v, want %v", c.e, got, c.want)
		}
	}
}

// A picture fitted to a frame is as large as fits with its own proportions, centered.
func TestAPictureFitsItsFrame(t *testing.T) {
	frame := image.Rect(0, 0, 1280, 800)
	for _, c := range []struct{ src, want image.Rectangle }{
		{image.Rect(0, 0, 960, 480), image.Rect(0, 80, 1280, 720)},
		{image.Rect(0, 0, 400, 400), image.Rect(240, 0, 1040, 800)},
		{image.Rect(0, 0, 640, 400), image.Rect(0, 0, 1280, 800)},
		{image.Rect(0, 0, 0, 0), frame},
	} {
		if got := fit(c.src, frame); got != c.want {
			t.Errorf("fit(%v) = %v, want %v", c.src, got, c.want)
		}
	}
}
