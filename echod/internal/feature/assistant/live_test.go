//go:build live

package assistant

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
)

// Asks a real model, to see the tools used the way a person would use them. Needs a chat endpoint:
//
//	TECHO5_LLM=http://host:8080/v1 TECHO5_LLM_KEYFILE=path/with/llm_key=line go test -tags live ./internal/feature/assistant/
//
// The key is read from the file and never printed.
func TestLiveAssistant(t *testing.T) {
	base := os.Getenv("TECHO5_LLM")
	if base == "" {
		t.Skip("TECHO5_LLM is not set")
	}
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Brain().Set(config.Brain{Mode: config.BrainDirect, LLM: base, STT: "x:1", TTS: "x:1"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Brain().SetKey(keyFrom(t, os.Getenv("TECHO5_LLM_KEYFILE"))); err != nil {
		t.Fatal(err)
	}

	ask := func(s string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		start := time.Now()
		reply, err := Get().Think(ctx, s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		t.Logf("%q -> %q (%v)", s, reply, time.Since(start).Round(10*time.Millisecond))
		return reply
	}

	ask("set a timer for ten minutes for the pasta")
	found := false
	for _, c := range timer.Get().List(time.Now()) {
		if strings.Contains(strings.ToLower(c.Name), "pasta") && c.Total == 10*time.Minute {
			found = true
		}
	}
	if !found {
		t.Errorf("no ten-minute pasta timer after asking for one: %+v", timer.Get().List(time.Now()))
	}
	ask("how long is left on it?")
	ask("what time is it?")
	ask("set an alarm for six thirty on weekdays")
	ask("play some country music")
	ask("cancel the pasta timer")
	if n := len(timer.Get().List(time.Now())); n != 0 {
		t.Errorf("%d timer(s) left after canceling", n)
	}
}

func keyFrom(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "llm_key="); ok {
			return k
		}
	}
	return ""
}
