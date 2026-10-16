package wake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/feature/detect/assets"
)

// realModel is a microWakeWord model the engine loads: the stop word the daemon carries.
func realModel(t *testing.T) []byte {
	t.Helper()
	path, err := assets.Stop(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const goodManifest = `{"type":"micro","wake_word":"Hey Computer","author":"someone","model":"../../outside.tflite",
 "trained_languages":["en","en-GB"],"version":2,
 "micro":{"probability_cutoff":0.97,"sliding_window_size":5,"feature_step_size":10,"tensor_arena_size":30000}}`

// A model sent from the setup page is kept under an id made from its phrase, beside the others, and
// is offered with them at once. The name its manifest gives the model is not used for anything.
func TestAWakeWordFromTheSetupPage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "models")
	l := NewLibrary(dir)

	m, err := l.AddOwn([]byte(goodManifest), realModel(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "hey_computer" || m.Phrase != "Hey Computer" || !m.Uploaded || m.Kind != KindMicroWakeWord {
		t.Errorf("kept as %+v", m)
	}
	if m.Path != filepath.Join(dir, "hey_computer.tflite") {
		t.Errorf("the model is at %s", m.Path)
	}
	if _, err := os.Stat(filepath.Join(root, "outside.tflite")); err == nil {
		t.Error("the manifest's own model name was used as a path")
	}

	var man map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "hey_computer.json"))
	if err := json.Unmarshal(b, &man); err != nil || man["techo5_uploaded"] != true || man["model"] != "hey_computer.tflite" {
		t.Errorf("manifest written as %s (%v)", b, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("the model directory holds %d files, want the model and its manifest", len(entries))
	}

	words, _ := l.Advertise()
	if len(words) != 1 || words[0].ID != "hey_computer" || words[0].Phrase != "Hey Computer" {
		t.Errorf("advertised %+v", words)
	}

	// The same phrase again replaces it; a purge of the cache leaves it, since nothing can be
	// downloaded to put it back.
	if _, err := l.AddOwn([]byte(goodManifest), realModel(t)); err != nil {
		t.Errorf("sending it again: %v", err)
	}
	if gone, _ := l.Purge(nil); gone != 0 {
		t.Errorf("a purge deleted %d models sent from the page", gone)
	}

	if err := l.RemoveOwn("hey_computer"); err != nil {
		t.Fatal(err)
	}
	if len(l.Ours()) != 0 {
		t.Errorf("still has %+v", l.Ours())
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("left %d files behind", len(entries))
	}
}

// What the engine could not use, or that is not a microWakeWord model at all, is refused with a
// reason, and nothing is left on disk.
func TestAWakeWordThatWillNotRunIsRefused(t *testing.T) {
	model := realModel(t)
	withMicro := func(m string) string {
		return `{"type":"micro","wake_word":"Hey Computer","micro":` + m + `}`
	}
	for what, c := range map[string]struct {
		manifest string
		model    []byte
		says     string
	}{
		"not JSON":              {"wake_word: hey", model, "could not be read"},
		"no type":               {`{"wake_word":"Hey","micro":{}}`, model, `"type": "micro"`},
		"an openWakeWord type":  {`{"type":"openWakeWord","wake_word":"Hey","micro":{}}`, model, `"type": "micro"`},
		"no micro section":      {`{"type":"micro","wake_word":"Hey"}`, model, `"micro"`},
		"no phrase":             {`{"type":"micro","wake_word":"  ","micro":{}}`, model, "wake_word"},
		"a control character":   {`{"type":"micro","wake_word":"Hey\u0007there","micro":{}}`, model, "control"},
		"a long phrase":         {`{"type":"micro","wake_word":"` + strings.Repeat("a", 61) + `","micro":{}}`, model, "at most"},
		"a bad language":        {`{"type":"micro","wake_word":"Hey","trained_languages":["en; rm -rf"],"micro":{}}`, model, "language tag"},
		"a step it cannot run":  {withMicro(`{"feature_step_size":15}`), model, "feature_step_size"},
		"a window out of range": {withMicro(`{"sliding_window_size":1000}`), model, "sliding_window_size"},
		"a manifest too big":    {withMicro(`{}`) + strings.Repeat(" ", MaxOwnManifest), model, "bigger"},
		"a model too big":       {withMicro(`{}`), append(bytes.Clone(model), make([]byte, MaxOwnModel)...), "bigger"},
		"not a model":           {withMicro(`{}`), []byte("this is a note, not a model, honestly"), "not a TensorFlow Lite"},
		"a model that lies":     {withMicro(`{}`), badInputModel(), "microWakeWord"},
		"a truncated model":     {withMicro(`{}`), model[:200], ""},
	} {
		dir := filepath.Join(t.TempDir(), "models")
		l := NewLibrary(dir)
		_, err := l.AddOwn([]byte(c.manifest), c.model)
		if err == nil {
			t.Errorf("%s was kept", what)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v", what, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s left %d files behind", what, len(entries))
		}
	}
}

// The page never overwrites or removes a model it did not send: the image's own, Home Assistant's, or
// anything named by a path.
func TestOnlyWhatThePageSentIsThePagesToChange(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "okay_nabu", `{"wake_word":"Okay Nabu"}`, 100)
	l := NewLibrary(dir)

	_, err := l.AddOwn([]byte(`{"type":"micro","wake_word":"Okay, Nabu!","micro":{}}`), realModel(t))
	if err == nil || !strings.Contains(err.Error(), "did not come from this page") {
		t.Errorf("a phrase that would replace the image's model: %v", err)
	}
	for _, id := range []string{"okay_nabu", "", "..", "../okay_nabu", "x/../../okay_nabu", `..\okay_nabu`, "nothing_here"} {
		if err := l.RemoveOwn(id); err == nil {
			t.Errorf("%q was removed", id)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "okay_nabu.tflite")); err != nil {
		t.Errorf("the image's model is gone: %v", err)
	}

	// A phrase that looks like a path is only ever a name.
	m, err := l.AddOwn([]byte(`{"type":"micro","wake_word":"../../etc/passwd","micro":{}}`), realModel(t))
	if err != nil || m.ID != "etc_passwd" || filepath.Dir(m.Path) != dir {
		t.Errorf("kept as %q at %s (%v)", m.ID, m.Path, err)
	}
}

// Home Assistant's config cannot make a model it hands over look like one the page sent, which would
// keep it from the cache purge and let the page delete it.
func TestAnAdoptedModelIsNeverThePages(t *testing.T) {
	dir := t.TempDir()
	_, offer := served(t, `{"type":"micro","wake_word":"x","model":"test_model.tflite","techo5_uploaded":true,"micro":{}}`, []byte("model"))
	m, err := Adopt(context.Background(), dir, offer)
	if err != nil {
		t.Fatal(err)
	}
	if m.Uploaded {
		t.Error("an adopted model was taken as sent from the page")
	}
}

func TestThePageKeepsOnlySoMany(t *testing.T) {
	dir := t.TempDir()
	for i := range MaxOwnModels {
		put(t, dir, fmt.Sprintf("word_%d", i), `{"wake_word":"Word","techo5_uploaded":true}`, 10)
	}
	l := NewLibrary(dir)
	if _, err := l.AddOwn([]byte(goodManifest), realModel(t)); err == nil || !strings.Contains(err.Error(), "remove one first") {
		t.Errorf("one more than the most: %v", err)
	}
}

func TestOwnIDIsAPlainName(t *testing.T) {
	for phrase, want := range map[string]string{
		"Hey Jarvis":                "hey_jarvis",
		"  Okay,   Computer!  ":     "okay_computer",
		"../../etc/passwd":          "etc_passwd",
		`C:\Windows`:                "c_windows",
		"Ça va":                     "a_va",
		strings.Repeat("abcd ", 10): "abcd_abcd_abcd_abcd_abcd_abcd_abcd_abcd",
	} {
		if got := OwnID(phrase); got != want {
			t.Errorf("%q: %q, want %q", phrase, got, want)
		}
	}
	// A phrase with no Latin letters still gets a name, and always the same one.
	a, b := OwnID("안녕 컴퓨터"), OwnID("안녕 컴퓨터")
	if a != b || !strings.HasPrefix(a, "own_") || strings.ContainsAny(a, `/\.:`) {
		t.Errorf("%q, %q", a, b)
	}
}
