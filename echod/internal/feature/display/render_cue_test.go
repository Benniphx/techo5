//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/announce"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// A cue turn draws the page it was on and the light across its top; without the cue the same
// frame has no light, and with captions the words darken the foot.
func TestCueOverThePage(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	size := image.Pt(960, 480)
	frame := func(s scene) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		newRenderer(img).draw(s)
		return img
	}
	plain := frame(scene{now: at, phase: "idle"})
	cued := frame(scene{now: at, phase: "idle", cue: "replying", since: at})
	if c := cued.RGBAAt(size.X/2, 1); c != amber {
		t.Errorf("top edge while replying is %v, want the accent %v", c, amber)
	}
	if c := plain.RGBAAt(size.X/2, 1); c == amber {
		t.Errorf("top edge with no turn is the accent")
	}
	// Below the band the page is the idle one, untouched.
	differ := 0
	for y := 40; y < size.Y-80; y += 7 {
		for x := 0; x < size.X; x += 7 {
			if plain.RGBAAt(x, y) != cued.RGBAAt(x, y) {
				differ++
			}
		}
	}
	if differ != 0 {
		t.Errorf("%d sampled pixels of the page changed under the cue", differ)
	}

	// Captions: the shaded strip at the foot, darker than the page there.
	capd := frame(scene{now: at, phase: "idle", cue: "replying", since: at, reply: "It is sunny and 72 degrees."})
	lum := func(c color.RGBA) int { return int(c.R) + int(c.G) + int(c.B) }
	foot := image.Pt(size.X-4, size.Y-6)
	if lum(capd.RGBAAt(foot.X, foot.Y)) >= lum(plain.RGBAAt(foot.X, foot.Y)) && lum(plain.RGBAAt(foot.X, foot.Y)) > 0 {
		t.Errorf("foot under the captions %v is not darker than the page's %v", capd.RGBAAt(foot.X, foot.Y), plain.RGBAAt(foot.X, foot.Y))
	}
	// Lingering: the words stay, the light is off.
	if c := frame(scene{now: at, phase: "idle", cue: "lingering", since: at}).RGBAAt(size.X/2, 1); c == amber {
		t.Errorf("light still on once the answer is done")
	}
}

// The deck's cached frame is not reused under a cue, or the light would never be drawn over it.
func TestCueRedrawsTheDeck(t *testing.T) {
	s := scene{showDeck: true, phase: "idle"}
	if deckFrameKey(s, false, false) == "" {
		t.Fatal("a plain deck frame has no key")
	}
	s.cue = "listening"
	if k := deckFrameKey(s, false, false); k != "" {
		t.Errorf("deck frame under a cue has key %q, want none", k)
	}
}

// With CUE_PREVIEW set, the cue over each page it goes over is written there to look at: the
// clock, a bright photo, the dashboard, the music, the night clock and the LCARS skin, each listening
// and with the answer's captions.
func TestCuePreviews(t *testing.T) {
	dir := os.Getenv("CUE_PREVIEW")
	if dir == "" {
		t.Skip("set CUE_PREVIEW")
	}
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	sky := home.Weather{Condition: "partlycloudy", Temp: "72°"}
	heard, reply := "What's the weather tomorrow?", "Tomorrow will be sunny, with a high of 74 and a low of 51. "+
		"There is a light wind from the south in the afternoon, and no rain is expected anywhere nearby until the weekend, when it turns cooler."
	pages := map[string]scene{
		"clock":  {weather: sky},
		"photos": {slideshowScreensaver: testPhoto("snow", showWide, showHigh)},
		"sky":    {slideshowScreensaver: testPhoto("sky", showWide, showHigh)},
		"dash":   {showDash: true, dashMode: config.DashboardDrawn, drawn: fourControls()},
		"music": {nowPlaying: true, playing: true, weather: sky,
			radio: home.Radio{Now: "KXYZ 101.1", Title: "Take It Easy", Artist: "Eagles"}},
		"night":  {redClock: true},
		"muted":  {weather: sky, muted: true},
		"setup":  {weather: sky, setupAsking: true},
		"onebig": {weather: sky},
	}
	write := func(name string, img *image.RGBA) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	draw := func(name string, s scene, cue string, words bool) {
		s.now, s.since, s.phase, s.cue = at, at.Add(-400*time.Millisecond), "idle", cue
		if words {
			s.heard, s.reply = heard, reply
			if name == "onebig" {
				s.reply = "Supercalifragilisticexpialidocious-supercalifragilisticexpialidocious-and-then-some-more-words"
			}
		}
		img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
		newRenderer(img).draw(s)
		suffix := "-" + cue
		if words {
			suffix += "-captions"
		}
		write("cue-"+name+suffix, img)
	}
	for name, s := range pages {
		draw(name, s, "listening", false)
		draw(name, s, "thinking", false)
		draw(name, s, "replying", true)
	}
	old := skinPreview
	skinPreview = skinLCARS
	applyTheme(current())
	t.Cleanup(func() { skinPreview = old; applyTheme(current()) })
	draw("lcars", scene{weather: sky}, "listening", false)
	draw("lcars", scene{weather: sky}, "replying", true)
	skinPreview = skinHAL
	applyTheme(current())
	draw("hal", scene{weather: sky}, "listening", false)
	skinPreview = old
	applyTheme(current())

	// The corner timer and the alert badge, at the top right.
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Screen().TimerStyle(timerCorner); err != nil {
		t.Fatal(err)
	}
	running := []timer.Countdown{{Name: "Pasta", Left: 4*time.Minute + 32*time.Second, Total: 10 * time.Minute, Active: true}}
	draw("corner-timer", scene{weather: sky, timers: running}, "listening", false)
	wind := home.Alert{ID: "a", Event: "Wind Advisory", Severity: "Moderate", Color: home.AlertColor("Wind Advisory", ""), Here: true, Ends: at.Add(time.Hour)}
	draw("alert", scene{weather: sky, alerts: home.AlertView{Here: []home.Alert{wind}}}, "listening", false)
}

// Captions go only over a page to be looked at. Over a page that takes its taps where it drew its
// buttons, the words change nothing and leave nothing to tap, and the light is not drawn either.
func TestCueCaptionsOnlyOverPassivePages(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	reply := "Tomorrow will be sunny, with a high of 74."
	frame := func(s scene, words bool) (*image.RGBA, image.Rectangle) {
		s.now, s.since, s.phase, s.cue = at, at, "idle", "replying"
		if words {
			s.heard, s.reply = "What's the weather?", reply
		}
		img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
		r := newRenderer(img)
		r.draw(s)
		r.zmu.Lock()
		defer r.zmu.Unlock()
		return img, r.captionsAt
	}
	for name, s := range map[string]scene{
		"the setup page asking": {setupAsking: true},
		"the PIN pad":           {pin: pinView{open: true}},
		"a call":                {call: phone.State{Phase: phone.Ringing, Peer: "104"}},
		"a ring":                {ring: ringState{timer: "Pasta"}},
		"the settings":          {showSheet: true},
		"a camera":              {showCamera: true},
		"an announcement":       {showAnnouncement: true, announcement: announce.Message{From: "Kitchen", Text: "dinner is ready"}},
	} {
		plain, _ := frame(s, false)
		capd, at := frame(s, true)
		if !same(plain.Pix, capd.Pix) {
			t.Errorf("%s: the captions were drawn over it", name)
		}
		if !at.Empty() {
			t.Errorf("%s: captions left to tap at %v", name, at)
		}
		if name != "an announcement" && capd.RGBAAt(showWide/2, 1) == amber {
			t.Errorf("%s: the light was drawn over it", name)
		}
	}
	for name, s := range map[string]scene{
		"the clock":       {},
		"the photos":      {slideshowScreensaver: testPhoto("snow", showWide, showHigh)},
		"the dashboard":   {showDash: true, dashMode: config.DashboardDrawn, drawn: fourControls()},
		"the music":       {nowPlaying: true, playing: true, radio: home.Radio{Now: "KXYZ", Title: "Song"}},
		"the night clock": {redClock: true},
	} {
		plain, _ := frame(s, false)
		capd, at := frame(s, true)
		if same(plain.Pix, capd.Pix) || at.Empty() {
			t.Errorf("%s: no captions over it", name)
		}
	}
}

// Over the night clock the captions and the light are the night's red, not the day's cream and accent.
func TestCueAtNightIsRed(t *testing.T) {
	at := time.Date(2026, 10, 20, 2, 30, 0, 0, time.Local)
	img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	newRenderer(img).draw(scene{now: at, since: at, phase: "idle", cue: "replying", redClock: true, reply: "It is two thirty."})
	if c := img.RGBAAt(showWide/2, 1); c != nightRed {
		t.Errorf("light at night is %v, want %v", c, nightRed)
	}
	creamy := 0
	for y := showHigh - 80; y < showHigh; y++ {
		for x := 0; x < showWide; x++ {
			if c := img.RGBAAt(x, y); c.G > 120 && c.B > 100 {
				creamy++
			}
		}
	}
	if creamy > 0 {
		t.Errorf("%d pixels of the night's captions are not red", creamy)
	}
}

// The light, listening, has the microphone hanging from it, and under LCARS it is below the
// console's top bar rather than along the edge where the bar's own color is.
func TestCueLightPlace(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	draw := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
		newRenderer(img).draw(scene{now: at, since: at.Add(-400 * time.Millisecond), phase: "idle", cue: "listening"})
		return img
	}
	plain := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	newRenderer(plain).draw(scene{now: at, phase: "idle"})
	lit := draw()
	// The tab under the band's middle: changed from the page, below the band and its shadow.
	if y := cueBand + cueGlow + 6; plain.RGBAAt(showWide/2, y) == lit.RGBAAt(showWide/2, y) {
		t.Error("no microphone tab under the band while listening")
	}
	old := skinPreview
	skinPreview = skinLCARS
	applyTheme(current())
	t.Cleanup(func() { skinPreview = old; applyTheme(current()) })
	r := newRenderer(image.NewRGBA(image.Rect(0, 0, showWide, showHigh)))
	if top := r.cueTop(); top <= lcarsEdge+lcarsBarH {
		t.Errorf("under LCARS the band starts at %d, inside the top bar", top)
	}
}

// A tap on the captions puts them away; a tap anywhere else is left to the page under them.
func TestCaptionsTap(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{r: newRenderer(image.NewRGBA(image.Rect(0, 0, showWide, showHigh))), poke: make(chan struct{}, 1)}
	at := time.Now()
	d.r.draw(scene{now: at, since: at, phase: "idle", cue: "replying", reply: "It's noon."})
	if d.captionsTap(touch.Gesture{Kind: touch.Tap, X: showWide / 2, Y: 100}) || d.quiet {
		t.Error("a tap above the captions put them away")
	}
	if d.captionsTap(touch.Gesture{Kind: touch.Hold, X: showWide / 2, Y: showHigh - 20}) {
		t.Error("a hold on the captions was taken as a tap")
	}
	if !d.captionsTap(touch.Gesture{Kind: touch.Tap, X: showWide / 2, Y: showHigh - 20}) || !d.quiet {
		t.Error("a tap on the captions did not put them away")
	}
}
