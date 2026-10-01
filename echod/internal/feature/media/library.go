package media

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/musicassistant"
)

// A stream the device cannot decode itself (AAC, HLS and the rest: much of the radio there is) is
// handed to the music library where one is set up (config.MusicAssistant). The library converts it
// and plays it here over Sendspin, as it plays its own music, so a station plays whatever its format,
// from wherever it was started: a voice request, the radio page, the setup page.

// libraryWait is how long the library has to get a stream playing here.
var libraryWait = 20 * time.Second

// libraryPlayer is this device's player id at the library, its factory MAC; a variable for the tests.
var libraryPlayer = layout.FactoryMAC

// errNoLibrary is a device with no music library set up to hand a stream to.
var errNoLibrary = errors.New("no music library is set up on this device")

// viaLibrary has the music library play url here, and waits for it to be playing.
func viaLibrary(url string) error {
	m := config.Get().MusicAssistant
	if !m.Set() {
		return errNoLibrary
	}
	player, err := libraryPlayer()
	if err != nil || player == "" {
		return errors.New("this device does not know its own player id")
	}
	slog.Info("media: handing a stream this device cannot decode to the music library", "url", url)
	ctx, cancel := context.WithTimeout(context.Background(), libraryWait+20*time.Second)
	defer cancel()
	c := musicassistant.Client{URL: m.URL, Token: m.Token}
	return c.PlayChecked(ctx, player, url, libraryWait, time.Second)
}

// handOff is Stream.handOff: a url Play started that the device could not decode.
func handOff(url string) {
	if err := viaLibrary(url); err != nil && !errors.Is(err, errNoLibrary) {
		slog.Warn("media: the music library did not play it either", "url", url, "err", err)
	}
}
