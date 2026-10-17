package setup

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/announce"
	"github.com/HuskerMinion/techo5/echod/internal/feature/share"
)

// Shared settings, on the General tab (feature/share): whether this device takes part, and for each
// setting that can be shared, the devices it is kept in step with and whether this device keeps its own.
// Below it, a copy of chosen settings to chosen devices, once. Both have routes of their own, for a form
// with a box for every setting and every device.

func init() {
	routes["/setup/share"] = (*Feature).saveShare
	routes["/setup/share-copy"] = (*Feature).copyShare
}

// shareBody is the most a sharing form may say: a box for each setting and each device.
const shareBody = 128 << 10

// shareDevices is the other devices to offer: those on the network now, and any a setting is linked with
// that is not, so a link to a device switched off can still be undone.
func shareDevices() []string {
	me := config.Get().Device.Name
	var out []string
	add := func(n string) {
		if n != "" && !strings.EqualFold(n, me) && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			out = append(out, n)
		}
	}
	for _, p := range announce.Peers() {
		add(p.Name)
	}
	for _, devices := range config.Get().Share.Links {
		for _, d := range devices {
			add(d)
		}
	}
	sort.Strings(out)
	return out
}

func shareSection(w http.ResponseWriter, token string) {
	c := config.Get()
	s := c.Share
	fmt.Fprintf(w, `<fieldset><legend>Shared settings</legend>
	 <form method="post" action="/setup/share"><input type="hidden" name="token" value="%s">
	 <label><input type="checkbox" name="on" value="1"%s> Share settings with the other devices</label>
	 <p class="note">Off on every device until it is turned on here or with the Settings sharing switch in Home
	  Assistant; a device with it off takes nothing. Link a setting to other devices and a change to it on any of
	  them reaches the rest, the latest change winning. Keep mine: nothing another device sends changes it. What
	  opens the device to the network, uses its microphones or camera, holds a password or says where its voice
	  goes is never shared. Every device needs the same house word (Connections).</p>`,
		html.EscapeString(token), checked(s.On))
	if c.Home.HouseWord == "" {
		fmt.Fprint(w, `<p class="note"><strong>This device has no house word set</strong>, so it can neither send nor take
		 shared settings.</p>`)
	}
	devices := shareDevices()
	if s.On {
		if len(devices) == 0 {
			fmt.Fprint(w, `<p class="note">No other device is on the network now.</p>`)
		}
		for _, sec := range share.Sections() {
			fmt.Fprintf(w, `<details><summary>%s</summary><div style="overflow-x:auto"><table class="share"><tr><th>Setting</th><th>Keep mine</th>`,
				html.EscapeString(sec.Name))
			for _, d := range devices {
				fmt.Fprintf(w, `<th>%s</th>`, html.EscapeString(d))
			}
			fmt.Fprint(w, `<th>Last from</th></tr>`)
			for _, st := range sec.Settings {
				fmt.Fprintf(w, `<tr><td>%s<input type="hidden" name="id" value="%s"></td>
				 <td><input type="checkbox" name="keep_%s" value="1"%s aria-label="Keep mine: %s"></td>`,
					html.EscapeString(st.Name), html.EscapeString(st.ID), html.EscapeString(st.ID),
					checked(s.Kept(st.ID)), html.EscapeString(st.Name))
				for _, d := range devices {
					fmt.Fprintf(w, `<td><input type="checkbox" name="link_%s" value="%s"%s aria-label="%s with %s"></td>`,
						html.EscapeString(st.ID), html.EscapeString(d), checked(s.LinkedWith(st.ID, d)),
						html.EscapeString(st.Name), html.EscapeString(d))
				}
				from := ""
				if f, ok := s.From[st.ID]; ok {
					from = f.Device + ", " + f.At.Local().Format("Jan 2 15:04")
				}
				fmt.Fprintf(w, `<td>%s</td></tr>`, html.EscapeString(from))
			}
			fmt.Fprint(w, `</table></div></details>`)
		}
	}
	fmt.Fprint(w, `<p><button type="submit">Save</button></p></form>`)

	if s.On && len(devices) > 0 {
		fmt.Fprintf(w, `<h4>Copy once</h4><form method="post" action="/setup/share-copy">
		 <input type="hidden" name="token" value="%s">
		 <p class="note">This device's value for each setting ticked goes to each device ticked, once, with no
		  link. A device that keeps a setting its own, or has sharing off, says so.</p><p>To:`,
			html.EscapeString(token))
		for _, d := range devices {
			fmt.Fprintf(w, ` <label><input type="checkbox" name="to" value="%s"> %s</label>`, html.EscapeString(d), html.EscapeString(d))
		}
		fmt.Fprint(w, `</p>`)
		for _, sec := range share.Sections() {
			fmt.Fprintf(w, `<details><summary>%s</summary>`, html.EscapeString(sec.Name))
			for _, st := range sec.Settings {
				fmt.Fprintf(w, `<label><input type="checkbox" name="copy" value="%s"> %s</label><br>`,
					html.EscapeString(st.ID), html.EscapeString(st.Name))
			}
			fmt.Fprint(w, `</details>`)
		}
		fmt.Fprint(w, `<p><button type="submit">Copy</button></p></form>`)
	}
	fmt.Fprint(w, `</fieldset>`)
}

func checked(b bool) string {
	if b {
		return " checked"
	}
	return ""
}

// shareForm checks a sharing form came from this page, behind the press on the device.
func (f *Feature) shareForm(w http.ResponseWriter, r *http.Request) bool {
	token, in := f.session(r)
	if !in {
		http.Error(w, "not let in", http.StatusForbidden)
		return false
	}
	if r.Method != http.MethodPost {
		http.Error(w, "post the form", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, shareBody)
	if err := r.ParseForm(); err != nil || r.PostFormValue("token") != token {
		http.Error(w, "that did not come from this page", http.StatusForbidden)
		return false
	}
	f.used()
	return true
}

// saveShare saves whether this device shares, what it keeps, and its links: each setting whose devices
// changed is linked anew, which tells the devices in its group and any taken out of it.
func (f *Feature) saveShare(w http.ResponseWriter, r *http.Request) {
	if !f.shareForm(w, r) {
		return
	}
	on := r.PostFormValue("on") == "1"
	if err := config.Set().Share().On(on); err != nil {
		http.Redirect(w, r, back("general", "problem", "could not be saved: "+err.Error()), http.StatusSeeOther)
		return
	}
	share.Get().Restore(config.Get())
	var problems []string
	if on {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		for _, id := range r.PostForm["id"] {
			if !share.Shareable(id) {
				continue
			}
			if err := config.Set().Share().Keep(id, r.PostFormValue("keep_"+id) == "1"); err != nil {
				problems = append(problems, err.Error())
			}
			want := r.PostForm["link_"+id]
			if sameDevices(want, config.Get().Share.Linked(id)) {
				continue
			}
			results, err := share.Get().Link(ctx, id, want)
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			for d, outcome := range results {
				if outcome != share.Applied {
					problems = append(problems, fmt.Sprintf("%s did not take the link for %s (%s)", d, id, orUnreachable(outcome)))
				}
			}
		}
	}
	slog.Info("setup page: shared settings saved", "on", on)
	to := back("general", "saved", "1")
	if len(problems) > 0 {
		to = back("general", "problem", strings.Join(problems, "; "))
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func orUnreachable(outcome string) string {
	if outcome == "" {
		return share.Unreachable
	}
	return outcome
}

// sameDevices is whether two lists name the same devices, however cased and ordered.
func sameDevices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.ContainsFunc(b, func(y string) bool { return strings.EqualFold(x, y) }) {
			return false
		}
	}
	return true
}

// copyShare copies the settings ticked to the devices ticked, once, and shows what each made of them.
func (f *Feature) copyShare(w http.ResponseWriter, r *http.Request) {
	if !f.shareForm(w, r) {
		return
	}
	if !config.Get().Share.On {
		http.Redirect(w, r, back("general", "problem", "turn settings sharing on first"), http.StatusSeeOther)
		return
	}
	ids, devices := r.PostForm["copy"], r.PostForm["to"]
	if len(ids) == 0 || len(devices) == 0 {
		http.Redirect(w, r, back("general", "problem", "tick the settings to copy and the devices to copy them to"), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	results := share.Get().Copy(ctx, ids, devices)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	head(w)
	fmt.Fprintf(w, `<div class="wrap"><h1>%s</h1><p class="sub">Settings copied</p><fieldset><legend>What each device took</legend>`,
		html.EscapeString(deviceName()))
	sort.Strings(devices)
	for _, d := range devices {
		r, ok := results[d]
		fmt.Fprintf(w, `<h4>%s</h4><ul>`, html.EscapeString(d))
		if !ok {
			fmt.Fprint(w, `<li>Nothing sent: it is this device.</li>`)
		}
		for _, id := range ids {
			if !share.Shareable(id) {
				continue
			}
			fmt.Fprintf(w, `<li>%s: %s</li>`, html.EscapeString(id), html.EscapeString(orUnreachable(r[id])))
		}
		fmt.Fprint(w, `</ul>`)
	}
	fmt.Fprint(w, `<p><a href="/setup?tab=general">Back to the setup page</a></p></fieldset></div>`)
}
