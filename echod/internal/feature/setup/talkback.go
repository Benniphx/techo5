package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/talkback"
)

// talkBackSection is where each camera is talked to (feature/talkback): an RTSP address per camera on
// the list, and the login they share. A camera on the Reolink recorder above needs nothing here. The
// password is never shown: it is written, or left alone.
func talkBackSection(w http.ResponseWriter, token string) {
	if !talkback.Here {
		return
	}
	c := config.Get()
	fmt.Fprint(w, `<fieldset><legend>Talk through cameras</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "talkback", "connections")
	state := "off: turn on Talk through cameras under Privacy &amp; Security, on the device or in Home Assistant"
	if c.Security.TalkBack {
		state = "on"
	}
	fmt.Fprintf(w, `<p class="note" style="margin-top:0">Talk on the camera page sends this device's microphones
	  to the camera's own speaker, straight over its RTSP stream, for cameras that have a speaker and ONVIF
	  two-way audio (most Reolinks do). The switch is %s.</p>`, state)

	var found []string
	for _, cam := range c.Home.Reolink.Cameras {
		found = append(found, html.EscapeString(cam.Name))
	}
	if len(found) > 0 {
		fmt.Fprintf(w, `<p class="note">From the Reolink recorder, with nothing to set: %s.</p>`, strings.Join(found, ", "))
	}

	hint := "the cameras' password"
	if c.TalkBack.Pass != "" {
		hint = "set; leave empty to keep it"
	}
	fmt.Fprintf(w, `<label for="tbuser">User</label>
	 <input id="tbuser" name="user" value="%s" placeholder="admin" autocomplete="off">
	 <label for="tbpass">Password</label>
	 <input id="tbpass" name="pass" type="password" value="" placeholder="%s" autocomplete="off">`,
		html.EscapeString(c.TalkBack.User), hint)

	cams := talkBackCameras()
	for i, cam := range cams {
		fmt.Fprintf(w, `<label for="tbaddr%d">%s</label>
		 <input type="hidden" name="entity" value="%s">
		 <input id="tbaddr%d" name="addr" value="%s" placeholder="rtsp://192.168.1.40:554/h264Preview_01_main" autocomplete="off">`,
			i, html.EscapeString(cam.Name), html.EscapeString(cam.Entity), i, html.EscapeString(c.TalkBack.Cameras[cam.Entity]))
	}
	if len(cams) == 0 {
		fmt.Fprint(w, `<p class="note">No other cameras are on the list.</p>`)
	}
	fmt.Fprint(w, `<p class="note">A camera's address is its RTSP stream: for a Reolink,
	  rtsp://<i>address</i>:554/h264Preview_01_main. Leave it empty for a camera with no speaker, and it
	  gets no Talk. Put the login in User and Password, not in the address.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`)
}

// talkBackCameras are the cameras on the list that are given an address here: not the device's own, and
// not the Reolink recorder's, which have theirs.
func talkBackCameras() []config.Camera {
	var out []config.Camera
	for _, cam := range home.Get().Cameras() {
		if cam.Entity == home.LocalCamera {
			continue
		}
		if _, _, _, ok := home.ReolinkRTSP(cam.Entity); ok {
			continue
		}
		out = append(out, cam)
	}
	return out
}

func saveTalkBack(r *http.Request) string {
	user := strings.TrimSpace(r.PostFormValue("user"))
	pass := r.PostFormValue("pass")
	if strings.ContainsAny(user+pass, "\r\n") {
		return "the user and password are one line each"
	}
	known := map[string]bool{}
	for _, cam := range talkBackCameras() {
		known[cam.Entity] = true
	}
	ents, addrs := r.PostForm["entity"], r.PostForm["addr"]
	if len(ents) != len(addrs) {
		return "the form came back incomplete; reload the page and try again"
	}
	kept := map[string]string{}
	// A camera no longer on the list keeps its address, for when it comes back.
	for e, a := range config.Get().TalkBack.Cameras {
		if !known[e] {
			kept[e] = a
		}
	}
	for i, e := range ents {
		if !known[e] {
			continue
		}
		a := strings.TrimSpace(addrs[i])
		if a == "" {
			continue
		}
		u, err := url.Parse(a)
		if err != nil || u.Scheme != "rtsp" || u.Hostname() == "" || len(a) > 512 || strings.ContainsAny(a, "\r\n ") {
			return "a camera's address should be like rtsp://192.168.1.40:554/h264Preview_01_main"
		}
		if u.User != nil {
			return "put the login in User and Password, not in the address"
		}
		kept[e] = a
	}
	var p *string
	if pass != "" {
		p = &pass
	}
	if err := config.Set().TalkBack().Login(user, p); err != nil {
		return "could not save it: " + err.Error()
	}
	if err := config.Set().TalkBack().Cameras(kept); err != nil {
		return "could not save it: " + err.Error()
	}
	slog.Info("setup page: talk back set", "cameras", len(kept), "password", p != nil)
	return ""
}
