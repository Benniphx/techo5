//go:build !dot && !spot

package setup

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/video"
)

// videoSection is the video player on the Screen & Photos tab: the two switches, and the addresses
// DLNA videos were allowed from, which can be forgotten.
func videoSection(w http.ResponseWriter, token string) {
	c := config.Get().Video
	checked := func(on bool) string {
		if on {
			return " checked"
		}
		return ""
	}
	fmt.Fprint(w, `<fieldset><legend>Video</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "video", "photos")
	fmt.Fprint(w, `<p class="note" style="margin-top:0">Play a video full screen, with its sound, from an
	  address on your network or the internet. Home Assistant sends one with the play_video action.
	  H.264 at 720p or less plays best.</p>`)
	if !video.Installed() {
		fmt.Fprint(w, `<p class="note"><strong>This device's software has no video player yet.</strong> An
		  update brings it.</p>`)
	}
	fmt.Fprintf(w, `
	 <p><label><input type="checkbox" name="on" value="yes" style="width:auto"%s> Videos: from Home Assistant</label></p>
	 <p><label><input type="checkbox" name="dlna" value="yes" style="width:auto"%s> DLNA video: from apps and media
	  servers too (BubbleUPnP, Jellyfin, Windows Cast to device), while DLNA is on under Sound</label></p>
	 <p class="note">Anyone on your network can send a DLNA video, so the first one from each address asks on
	  the screen first.</p>`, checked(c.On), checked(c.DLNA))
	if n := len(c.Allowed); n > 0 {
		fmt.Fprintf(w, `<p><label><input type="checkbox" name="forget" value="yes" style="width:auto"> Forget the %d %s
		 allowed to send videos, so each asks again</label></p>`, n, plural(n, "address", "addresses"))
	}
	fmt.Fprint(w, `<p><button type="submit">Save</button></p></form></fieldset>`)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func saveVideo(r *http.Request) string {
	on := func(name string) bool { return r.PostFormValue(name) == "yes" }
	if on("on") != config.Get().Video.On {
		video.SetOn(on("on"))
	}
	if on("dlna") != config.Get().Video.DLNA {
		video.SetDLNA(on("dlna"))
	}
	if on("forget") {
		if err := config.Set().Video().ForgetAllowed(); err != nil {
			return "could not forget them"
		}
	}
	c := config.Get().Video
	if c.On != on("on") || c.DLNA != on("dlna") {
		return "could not save it"
	}
	slog.Info("setup page: video set", "on", c.On, "dlna", c.DLNA, "forgot", on("forget"))
	return ""
}
