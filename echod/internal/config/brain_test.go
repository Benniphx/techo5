package config

import (
	"os"
	"testing"
)

func TestRealtimeChoiceAndDefaults(t *testing.T) {
	b := load(t).Get().Brain
	if b.Realtime() || b.Direct() {
		t.Fatal("new device changed its Home Assistant default")
	}
	b.Mode = BrainRealtime
	if !b.Realtime() || b.Direct() {
		t.Fatal("Realtime must be explicit even without credentials")
	}
	if b.RealtimeModelName() != DefaultRealtimeModel || b.RealtimeVoiceName() != DefaultRealtimeVoice {
		t.Fatal("missing native defaults")
	}
	b.RealtimeModel, b.RealtimeVoice = "gpt-realtime-custom", "cedar"
	if b.RealtimeModelName() != b.RealtimeModel || b.RealtimeVoiceName() != b.RealtimeVoice {
		t.Fatal("explicit settings ignored")
	}
}

func TestBrainFormWritesKeyTogetherAndKeepsItWhenBlank(t *testing.T) {
	st := load(t)
	key := "test-private-key"
	b := Brain{Mode: BrainRealtime, RealtimeTools: "read"}
	if err := st.Set().Brain().SetWithKey(b, &key); err != nil {
		t.Fatal(err)
	}
	b.RealtimeVoice = "cedar"
	if err := st.Set().Brain().SetWithKey(b, nil); err != nil {
		t.Fatal(err)
	}
	again, err := Load(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().Brain; got.Key != key || got.RealtimeVoice != "cedar" || got.RealtimeTools != "read" {
		t.Fatal("native settings or private key did not survive reload")
	}
	info, err := os.Stat(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret state is not owner-only")
	}
	empty := ""
	if err := st.Set().Brain().SetWithKey(Brain{}, &empty); err != nil {
		t.Fatal(err)
	}
	if st.Get().Brain.Key != "" {
		t.Fatal("explicit removal did not clear key")
	}
}
