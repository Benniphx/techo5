package setup

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestRealtimeFormSavesAndNeverRendersKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	config.Use(path)
	v := url.Values{"mode": {"realtime"}, "key": {"test-private-key"}, "language": {"en"}, "realtime_tools": {"read"}, "prompt": {"Speak clearly."}}
	if why := saveBrain(form(v)); why != "" {
		t.Fatal(why)
	}
	b := config.Get().Brain
	if !b.Realtime() || b.RealtimeModelName() != config.DefaultRealtimeModel || b.RealtimeVoiceName() != config.DefaultRealtimeVoice || b.RealtimeTools != "read" {
		t.Fatal("native config not saved")
	}
	v.Del("key")
	v.Set("realtime_voice", "cedar")
	if why := saveBrain(form(v)); why != "" {
		t.Fatal(why)
	}
	if config.Get().Brain.Key != "test-private-key" {
		t.Fatal("blank form cleared private key")
	}
	w := httptest.NewRecorder()
	brainSection(w, "page-token")
	page := w.Body.String()
	for _, want := range []string{`value="realtime" selected`, `name="key" type="password" value=""`, "set; leave empty to keep it", "audio usage incurs account costs", "Only a local wake connects", `name="realtime_tools"`, "cedar"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(page, "test-private-key") || strings.Contains(page, "%!") {
		t.Fatal("secret exposed or template failed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("saved secret must be owner-only")
	}
}

func TestInvalidBrainFormNeverChangesSettingsOrKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	config.Use(path)
	key := "test-existing-secret"
	original := config.Brain{Mode: config.BrainDirect, STT: "localhost:10300", TTS: "localhost:10200", LLM: "http://localhost:8080/v1", Prompt: "Keep this."}
	if err := config.Set().Brain().SetWithKey(original, &key); err != nil {
		t.Fatal(err)
	}
	before := config.Get().Brain
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []url.Values{
		{"mode": {"realtime"}, "key": {"line\nbreak"}},
		{"mode": {"realtime"}, "key": {"two words"}},
		{"mode": {"realtime"}, "nokey": {"yes"}},
		{"mode": {"realtime"}, "realtime_tools": {"anything"}, "nokey": {"yes"}},
		{"mode": {"realtime"}, "realtime_tools": {"device"}, "key": {"test-cloud-key"}},
		{"mode": {"realtime"}, "realtime_model": {"gpt-realtime?other=bad"}},
		{"mode": {"realtime"}, "realtime_model": {"unrelated-model"}},
		{"mode": {"realtime"}, "realtime_voice": {"voice\r\n" + "other"}},
		{"mode": {"realtime"}, "language": {"en_US"}},
		{"mode": {"realtime"}, "language": {"en-US"}},
		{"mode": {"realtime"}, "language": {"EN"}},
		{"mode": {"realtime"}, "realtime_voice": {strings.Repeat("a", 33)}},
		{"mode": {"realtime"}, "stt": {"localhost:10300"}},
		{"mode": {"realtime"}, "tts": {"localhost:10200"}},
		{"mode": {"realtime"}, "llm": {"http://localhost:8080/v1"}},
		{"mode": {"realtime"}, "voice": {"local-voice"}},
		{"mode": {"realtime"}, "model": {"local-model"}},
		{"mode": {"realtime"}, "search": {"http://localhost:8888"}},
		{"mode": {"realtime"}, "realtime_url": {"https://service.invalid"}},
		{"mode": {"realtime"}, "realtime_token_file": {"/private/key"}},
		{"mode": {"realtime"}, "realtime_ca_file": {"/private/ca"}},
		{"mode": {"realtime"}, "prompt": {strings.Repeat("x", 2001)}},
		{"mode": {"unknown"}, "nokey": {"yes"}},
		{"mode": {""}, "realtime_tools": {"device"}},
		{"mode": {"direct"}, "stt": {"localhost:10300"}, "tts": {"localhost:10200"}, "llm": {"http://localhost:8080/v1"}, "key": {"bad\nkey"}},
	} {
		if why := saveBrain(form(bad)); why == "" {
			t.Errorf("invalid form accepted: %v", bad)
		}
		if !reflect.DeepEqual(before, config.Get().Brain) {
			t.Fatalf("invalid form mutated settings: %v", bad)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(disk) != string(after) {
			t.Fatal("invalid form wrote state")
		}
	}
}

func TestRealtimeRequiresEffectiveKeyAndDirectStillWorks(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if saveBrain(form(url.Values{"mode": {"realtime"}})) == "" {
		t.Fatal("native cloud mode accepted no key")
	}
	direct := url.Values{"mode": {"direct"}, "stt": {"localhost:10300"}, "tts": {"localhost:10200"}, "llm": {"http://localhost:8080/v1/"}, "model": {"local-model"}}
	if why := saveBrain(form(direct)); why != "" {
		t.Fatal(why)
	}
	if b := config.Get().Brain; !b.Direct() || b.LLM != "http://localhost:8080/v1" {
		t.Fatal("direct behavior changed")
	}
	if err := config.Set().Brain().SetKey("test-key"); err != nil {
		t.Fatal(err)
	}
	if why := saveBrain(form(url.Values{"mode": {""}, "nokey": {"yes"}})); why != "" {
		t.Fatal(why)
	}
	if b := config.Get().Brain; b.Mode != config.BrainHomeAssistant || b.Key != "" {
		t.Fatal("default mode and explicit key removal failed")
	}
}

func TestProviderSwitchRequiresAnExplicitCredentialDecision(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	direct := url.Values{"mode": {"direct"}, "stt": {"localhost:10300"}, "tts": {"localhost:10200"}, "llm": {"http://localhost:8080/v1"}, "key": {"test-local-provider-key"}}
	if why := saveBrain(form(direct)); why != "" {
		t.Fatal(why)
	}
	if saveBrain(form(url.Values{"mode": {"realtime"}})) == "" {
		t.Fatal("local provider key reused in cloud mode")
	}
	if b := config.Get().Brain; b.Mode != config.BrainDirect || b.Key != "test-local-provider-key" {
		t.Fatal("rejected provider switch changed state")
	}
	if why := saveBrain(form(url.Values{"mode": {"realtime"}, "key": {"test-cloud-provider-key"}})); why != "" {
		t.Fatal(why)
	}
	direct.Del("key")
	if saveBrain(form(direct)) == "" {
		t.Fatal("cloud provider key reused in direct mode")
	}
	if b := config.Get().Brain; b.Mode != config.BrainRealtime || b.Key != "test-cloud-provider-key" {
		t.Fatal("rejected provider switch changed state")
	}
	// Home Assistant may temporarily disable either provider without losing its secret, but cannot
	// be used as an intermediate state to carry that secret to a different provider silently.
	if why := saveBrain(form(url.Values{"mode": {""}})); why != "" {
		t.Fatal(why)
	}
	if saveBrain(form(direct)) == "" {
		t.Fatal("intermediate Home Assistant mode bypassed credential decision")
	}
	direct.Set("nokey", "yes")
	if why := saveBrain(form(direct)); why != "" {
		t.Fatal(why)
	}
	if b := config.Get().Brain; !b.Direct() || b.Key != "" {
		t.Fatal("explicit keyless direct mode failed")
	}
}
