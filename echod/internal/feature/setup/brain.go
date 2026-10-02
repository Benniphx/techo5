package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/wakeword"
)

// listeningSection is how long the device listens again after an answer, and how many times in a row:
// Home Assistant's two settings for the first wake word, here for a device that has none.
func listeningSection(w http.ResponseWriter, token string) {
	fmt.Fprint(w, `<fieldset><legend>Listening after an answer</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "listening", "sound")
	fmt.Fprintf(w, `<label for="fu">Keep listening for (seconds, 0 for only when asked a question)</label>
	 <input id="fu" name="followup" type="number" min="0" max="30" value="%d">
	 <label for="fus">Times in a row (0 for no limit)</label>
	 <input id="fus" name="followups" type="number" min="0" max="10" value="%d">
	 <p class="note">After an answer the device listens again without the wake word, for this long and this
	  many times. A question the assistant asks is always listened for.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`,
		int(wakeword.FollowUp(0).Seconds()), wakeword.FollowUps(0))
}

func saveListening(r *http.Request) string {
	fu, err1 := strconv.Atoi(strings.TrimSpace(r.PostFormValue("followup")))
	fus, err2 := strconv.Atoi(strings.TrimSpace(r.PostFormValue("followups")))
	if err1 != nil || err2 != nil || fu < 0 || fu > 30 || fus < 0 || fus > 10 {
		return "listening is 0 to 30 seconds, and 0 to 10 times in a row"
	}
	wakeword.Get().SetFollowUp(0, fu)
	wakeword.Get().SetFollowUps(0, fus)
	return ""
}

// brainSection is where the voice answers come from: Home Assistant's Assist pipeline, or speech and a
// chat model reached directly (config.Brain). The key is never shown: it is written, or left alone.
func brainSection(w http.ResponseWriter, token string) {
	b := config.Get().Brain
	fmt.Fprint(w, `<fieldset><legend>Voice assistant</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "brain", "sound")
	fmt.Fprintf(w, `<label for="mode">Answered by</label>
	 <select id="mode" name="mode">
	  <option value=""%s>Home Assistant (its Assist pipeline)</option>
	  <option value="direct"%s>Speech and a chat model, directly</option>
	  <option value="realtime"%s>OpenAI Realtime, directly (cloud)</option>
	 </select>
	 <fieldset id="direct-brain"><legend>Direct speech and chat servers</legend>
	 <p class="note">Directly, the device needs no Home Assistant: what is said goes to a speech-to-text
	  server, the words to a chat model that can set timers and alarms, play the radio and call other
	  devices, and the answer to a text-to-speech server. The servers are Wyoming ones, as Home Assistant
	  uses (faster-whisper, Piper), and any chat model with an OpenAI-style endpoint (llama.cpp, Ollama).</p>
	 <label for="stt">Speech to text (host:port)</label>
	 <input id="stt" name="stt" value="%s" placeholder="192.168.1.20:10300" autocomplete="off">
	 <label for="tts">Text to speech (host:port)</label>
	 <input id="tts" name="tts" value="%s" placeholder="192.168.1.20:10200" autocomplete="off">
	 <label for="voice">Voice</label>
	 <input id="voice" name="voice" value="%s" placeholder="en_US-lessac-medium" autocomplete="off">
	 <label for="llm">Chat model endpoint</label>
	 <input id="llm" name="llm" value="%s" placeholder="http://192.168.1.20:8080/v1" autocomplete="off">
	 <label for="model">Model</label>
	 <input id="model" name="model" value="%s" placeholder="only if the server has several" autocomplete="off">
	 <label for="search">Web search (a SearXNG server)</label>
	 <input id="search" name="search" value="%s" placeholder="http://192.168.1.20:8888" autocomplete="off">
	 <p class="note">To look up what the chat model cannot know: games, news, opening hours. SearXNG needs
	  its JSON format turned on (search.formats in its settings.yml). Empty: no looking things up.</p>
	 </fieldset>
	 <fieldset id="realtime-brain"><legend>Native Realtime</legend>
	 <p class="note">Microphone audio and enabled tool results go directly to OpenAI over TLS. An OpenAI
	  API account and key are required; audio usage incurs account costs. Only a local wake connects and sends up to 500 ms of preceding microphone audio,
	  sessions are bounded, and canceling closes the connection. There is no separate service.
	  Realtime language is a two-letter code such as en or de; empty means English.
	  The key stays in owner-only device state and is never shown again. Protect the local setup connection
	  when entering it. Clear the direct server fields when using a browser without JavaScript.</p>
	 <label for="realtime_model">Realtime model</label>
	 <input id="realtime_model" name="realtime_model" value="%s" placeholder="gpt-realtime-2.1" maxlength="128" autocomplete="off">
	 <label for="realtime_voice">Realtime voice</label>
	 <input id="realtime_voice" name="realtime_voice" value="%s" placeholder="marin" maxlength="32" autocomplete="off">
	 <label for="realtime_tools">Tools shared with the cloud assistant</label>
	 <select id="realtime_tools" name="realtime_tools">
	  <option value=""%s>None</option>
	  <option value="read"%s>Read device information</option>
	 </select>
	 <p class="note">Tools use TECHO5's existing capabilities; this does not provide arbitrary Home Assistant
	  entity access. Tool results may include timer and alarm labels, station names and calendar event titles.
	  Read mode exposes the device's weather, calendar, timers, alarms and station list.
	  Calendar reads may refresh the existing cache. Device mutations, remote playback, intercom and screen
	  controls are excluded in this draft. Choose a scope explicitly.</p></fieldset>
	 <label for="language">Language</label>
	 <input id="language" name="language" value="%s" placeholder="en" maxlength="8" autocomplete="off">
	 <label for="key">API key (replace it when changing providers)</label>
	 <input id="key" name="key" type="password" value="" placeholder="%s" autocomplete="off">
	 <p><label><input type="checkbox" name="nokey" value="yes" style="width:auto"> Remove the key</label></p>
	 <label for="prompt">Anything the assistant should know</label>
	 <textarea id="prompt" name="prompt" rows="3" maxlength="2000">%s</textarea>
	 <p><button type="submit">Save</button></p></form>
	 <script>(()=>{const mode=document.getElementById("mode");
	  const update=()=>{document.getElementById("direct-brain").disabled=mode.value==="realtime";
	   document.getElementById("realtime-brain").disabled=mode.value!=="realtime";};
	  mode.addEventListener("change",update);update();})();</script></fieldset>`,
		selected(b.Mode == config.BrainHomeAssistant), selected(b.Mode == config.BrainDirect), selected(b.Mode == config.BrainRealtime),
		html.EscapeString(b.STT), html.EscapeString(b.TTS), html.EscapeString(b.Voice),
		html.EscapeString(b.LLM), html.EscapeString(b.Model), html.EscapeString(b.Search),
		html.EscapeString(b.RealtimeModel), html.EscapeString(b.RealtimeVoice),
		selected(b.RealtimeTools == ""), selected(b.RealtimeTools == "read"),
		html.EscapeString(b.Language), keyHint(b.Key != ""),
		html.EscapeString(b.Prompt))
}

func keyHint(set bool) string {
	if set {
		return "set; leave empty to keep it"
	}
	return "none"
}

// saveBrain keeps the form, refusing what could not work rather than keeping it to fail at the next
// wake word.
func saveBrain(r *http.Request) string {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	b := config.Brain{
		Mode:          config.BrainMode(v("mode")),
		RealtimeModel: v("realtime_model"),
		RealtimeVoice: v("realtime_voice"),
		RealtimeTools: v("realtime_tools"),
		STT:           v("stt"),
		TTS:           v("tts"),
		Voice:         v("voice"),
		Language:      v("language"),
		LLM:           strings.TrimRight(v("llm"), "/"),
		Model:         v("model"),
		Search:        strings.TrimRight(v("search"), "/"),
		Prompt:        strings.TrimSpace(r.PostFormValue("prompt")),
	}
	if b.Mode != config.BrainHomeAssistant && b.Mode != config.BrainDirect && b.Mode != config.BrainRealtime {
		return "that is not a way of answering this device knows"
	}
	for _, hp := range []struct{ what, v string }{{"speech to text", b.STT}, {"text to speech", b.TTS}} {
		if hp.v == "" {
			continue
		}
		if _, port, err := net.SplitHostPort(hp.v); err != nil || port == "" {
			return hp.what + " should be host:port, like 192.168.1.20:10300"
		}
	}
	if b.LLM != "" {
		u, err := url.Parse(b.LLM)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "the chat model endpoint should be an address like http://192.168.1.20:8080/v1"
		}
	}
	if b.Search != "" {
		u, err := url.Parse(b.Search)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "the search server should be an address like http://192.168.1.20:8888"
		}
	}
	if b.Mode == config.BrainDirect && (b.STT == "" || b.TTS == "" || b.LLM == "") {
		return "answering directly needs all three: speech to text, text to speech and the chat model"
	}
	if strings.ContainsAny(b.Voice+b.Language+b.Model, "\r\n") {
		return "the voice, language and model are one line each"
	}

	keyValue := v("key")
	if len(keyValue) > 4096 || strings.IndexFunc(keyValue, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
		return "a key is one line with no whitespace"
	}
	var key *string
	if r.PostFormValue("nokey") == "yes" {
		keyValue = ""
		key = &keyValue
	} else if keyValue != "" {
		key = &keyValue
	}
	previous := config.Get().Brain
	if (b.Mode == config.BrainDirect || b.Mode == config.BrainRealtime) && previous.Mode != b.Mode && previous.Key != "" && key == nil {
		return "changing assistant providers requires a replacement key, or explicitly removing the key for direct mode"
	}
	if b.Mode == config.BrainRealtime {
		if v("realtime_url") != "" || v("realtime_token_file") != "" || v("realtime_ca_file") != "" {
			return "native Realtime does not use a separate service endpoint or local credential paths"
		}
		if b.STT != "" || b.TTS != "" || b.Voice != "" || b.LLM != "" || b.Model != "" || b.Search != "" {
			return "Realtime uses its own model and voice, not direct speech, chat or search servers"
		}
		if !realtimeModel.MatchString(b.RealtimeModelName()) || !realtimeVoice.MatchString(b.RealtimeVoiceName()) {
			return "the Realtime model and voice must be valid names"
		}
		if b.Language != "" && !realtimeLanguage.MatchString(b.Language) {
			return "the Realtime language should be two lowercase letters, like en or de"
		}
		if b.RealtimeTools != "" && b.RealtimeTools != "read" {
			return "Realtime tools must be none or read; device controls are not available in this draft"
		}
		effective := previous.Key
		if key != nil {
			effective = *key
		}
		if effective == "" {
			return "Realtime requires an OpenAI API key; leave the field empty only to keep a saved key"
		}
	} else if b.RealtimeModel != "" || b.RealtimeVoice != "" || b.RealtimeTools != "" {
		return "Realtime settings require the Realtime answering mode"
	}
	if len(b.Prompt) > 2000 {
		return "the assistant instructions must be at most 2000 bytes"
	}
	if err := config.Set().Brain().SetWithKey(b, key); err != nil {
		return "could not save it: " + err.Error()
	}
	slog.Info("setup page: the voice assistant was set", "mode", b.Mode, "stt", b.STT, "tts", b.TTS, "llm", b.LLM)
	return ""
}

var (
	realtimeModel    = regexp.MustCompile(`^gpt-realtime[a-zA-Z0-9._-]{0,116}$`)
	realtimeVoice    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)
	realtimeLanguage = regexp.MustCompile(`^[a-z]{2}$`)
)
