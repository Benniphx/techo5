package setup

import (
	"fmt"
	"html"
	"io"
	"net/http"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// Sounds of the owner's own (hardware/speaker/own_sounds.go): a WAVE file for any of the sounds a
// request makes, sent from the page one at a time and checked the way the player checks it before it
// is kept, so a file that would only play the stock sound in its place is refused with the reason
// instead. Nothing is converted here: the device has no converter on every model, and a page that
// says what format it needs is better than one that works on some devices only.

// soundTitles is what each sound is called on the page, and when it plays.
var soundTitles = map[string][2]string{
	"wake_word_triggered": {"Wake", "when the wake word is heard, where Wake sound is Home Assistant"},
	"failure":             {"Failure", "when a request cannot be served"},
	"canceled":            {"Canceled", "when a request is dropped"},
	"timer_finished":      {"Timer finished", "a finished timer, and the Home Assistant alarm sound"},
	"mute_switch_on":      {"Mute", "muting the microphones"},
	"mute_switch_off":     {"Unmute", "unmuting the microphones"},
}

// soundsSection lists the sounds, whether each is the device's own or one sent here, and takes a file
// for any of them.
func soundsSection(w http.ResponseWriter, token string) {
	fmt.Fprint(w, `<fieldset><legend>Sounds of your own</legend>
	 <p class="note" style="margin-top:0">A recording of your own plays in place of the device's sound. Play
	  tries what plays now, quietly.</p>`)
	for _, name := range speaker.OwnSounds() {
		title := soundTitles[name]
		has, problem := speaker.OwnState(name)
		state := "the device's own"
		switch {
		case has && problem != "":
			state = `<span class="bad">yours, but it does not play: ` + html.EscapeString(problem) + `</span>`
		case has:
			state = `<span class="ok">yours</span>`
		}
		fmt.Fprintf(w, `<p style="margin:1rem 0 0"><strong>%s</strong>, %s. Now: %s</p><div class="btns" style="margin-top:.4rem">`,
			html.EscapeString(title[0]), html.EscapeString(title[1]), state)
		fmt.Fprint(w, `<form method="post" action="/setup/save" style="margin:0">`)
		hidden(w, token, "sound-play", "sound")
		fmt.Fprintf(w, `<input type="hidden" name="sound" value="%s"><button type="submit" class="quiet">Play</button></form>`,
			html.EscapeString(name))
		fmt.Fprintf(w, `<input type="file" accept=".wav,audio/wav" class="soundfile" style="width:auto;flex:1;min-width:12rem"
		 data-sound="%s" aria-label="Send a recording for %s">`, html.EscapeString(name), html.EscapeString(title[0]))
		if has {
			fmt.Fprint(w, `<form method="post" action="/setup/save" style="margin:0">`)
			hidden(w, token, "sound-remove", "sound")
			fmt.Fprintf(w, `<input type="hidden" name="sound" value="%s"><button type="submit" class="quiet">Use the device's own</button></form>`,
				html.EscapeString(name))
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprintf(w, `<p id="soundsays" class="note">A WAVE (.wav) file, 16-bit, 48 kHz, mono or stereo, at most 10
	  seconds. Other formats are not converted here; ffmpeg makes one from anything:
	  <code>ffmpeg -i chime.mp3 -ac 1 -ar 48000 -c:a pcm_s16le chime.wav</code>. Keep the wake sound short: the
	  device listens for the request only once it has played.</p>
	 <script>
	 document.querySelectorAll('.soundfile').forEach(el => el.addEventListener('change', async (ev) => {
	   const say = document.getElementById('soundsays');
	   const f = ev.target.files[0];
	   if (!f) return;
	   say.textContent = 'Sending ' + f.name + '...';
	   const fd = new FormData(); fd.append('token', %q); fd.append('sound', ev.target.dataset.sound); fd.append('file', f, 'sound.wav');
	   try {
	     const r = await fetch('/setup/sound', {method: 'POST', body: fd});
	     say.textContent = r.ok ? f.name + ' kept.' : await r.text();
	     if (r.ok) setTimeout(() => location.reload(), 1200);
	   } catch (e) { say.textContent = 'It could not be sent.'; }
	 }));
	 </script></fieldset>`, token)
}

// sound takes one recording from the page's script: the token, then which sound it is for, then the
// file. The token is checked before anything else is read, and the file is never read past the most a
// sound can be.
func (f *Feature) sound(w http.ResponseWriter, r *http.Request) {
	token, in := f.session(r)
	if !in {
		http.Error(w, "not let in", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "post a sound", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, speaker.MaxOwnUpload+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "that was not a sound sent from this page", http.StatusBadRequest)
		return
	}
	// The form carries the session it came from, as every save does, so another site's page cannot
	// post a sound here with a browser's cookie.
	p, err := mr.NextPart()
	if err != nil || p.FormName() != "token" {
		http.Error(w, "that did not come from this page", http.StatusForbidden)
		return
	}
	got, err := io.ReadAll(io.LimitReader(p, 256))
	if err != nil || string(got) != token {
		http.Error(w, "that did not come from this page", http.StatusForbidden)
		return
	}
	// Which sound is a name from the list, looked up there; what the browser called its file is not
	// read at all.
	p, err = mr.NextPart()
	if err != nil || p.FormName() != "sound" {
		http.Error(w, "no sound was named", http.StatusBadRequest)
		return
	}
	name, err := io.ReadAll(io.LimitReader(p, 64))
	if err != nil {
		http.Error(w, "no sound was named", http.StatusBadRequest)
		return
	}
	if _, ok := soundTitles[string(name)]; !ok {
		http.Error(w, "that is not one of the sounds", http.StatusBadRequest)
		return
	}
	p, err = mr.NextPart()
	if err != nil || p.FormName() != "file" {
		http.Error(w, "no file was sent", http.StatusBadRequest)
		return
	}
	if err := speaker.SaveOwn(string(name), p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.used()
	w.WriteHeader(http.StatusNoContent)
}

func playSound(r *http.Request) string {
	name := r.PostFormValue("sound")
	if _, ok := soundTitles[name]; !ok {
		return "that is not one of the sounds"
	}
	if err := speaker.PreviewOwn(name); err != nil {
		return err.Error()
	}
	return ""
}

func removeSound(r *http.Request) string {
	name := r.PostFormValue("sound")
	if _, ok := soundTitles[name]; !ok {
		return "that is not one of the sounds"
	}
	if err := speaker.RemoveOwn(name); err != nil {
		return "could not remove it: " + err.Error()
	}
	return ""
}
