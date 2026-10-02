package assistant

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
)

func TestRealtimeScopesCannotCallUnadvertisedTools(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	available := tools()
	for _, scope := range []string{"", "read", "device", "invalid"} {
		definitions, run, prompt := realtimeTools(scope, config.Brain{Prompt: "Speak gently."}, available, time.Now())
		names := map[string]bool{}
		for _, d := range definitions {
			names[d.Name] = true
		}
		if (scope == "" || scope == "invalid" || scope == "device") && len(definitions) != 0 {
			t.Fatal("unselected or invalid scope enabled tools")
		}
		for _, name := range []string{"weather_elsewhere", "find_radio", "play_radio", "play_music", "save_station", "web_search", "read_page", "call_device", "show_on_screen", "stop", "get_home_state", "list_callees", "start_timer", "cancel_timers", "set_alarm", "delete_alarm", "set_volume"} {
			if names[name] {
				t.Errorf("%s enabled unsafe or unsupported %s", scope, name)
			}
			if _, err := run(context.Background(), name, `{}`); err == nil {
				t.Errorf("unadvertised %s ran for %s", name, scope)
			}
		}

		for _, name := range []string{"list_timers", "list_alarms", "list_stations", "weather", "calendar"} {
			if names[name] != (scope == "read") {
				t.Errorf("unexpected %s permission for %s", name, scope)
			}
		}
		if !strings.Contains(prompt, "Speak gently.") || !strings.Contains(prompt, "only these tools are enabled") {
			t.Fatal("existing instructions or explicit scope missing")
		}
	}
}

func TestRealtimeCallsValidateBoundedArgumentsBeforeExistingRun(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	calls := 0
	available := []tool{{llm.Tool{Name: "weather", Parameters: object(map[string]any{"seconds": num("Duration"), "label": str("Label")}, "seconds")}, func(a map[string]any) (string, error) {
		calls++
		if a["seconds"].(float64) < 1 {
			return "", errors.New("seconds must be positive")
		}
		return "started", nil
	}}}
	defs, run, _ := realtimeTools("read", config.Brain{}, available, time.Now())
	for _, bad := range []string{`{`, `null`, `[]`, `{}`, `{"seconds":"2"}`, `{"seconds":2,"unexpected":true}`, `{"seconds":2,"label":4}`, `{"seconds":2,"label":"` + strings.Repeat("x", 2001) + `"}`, strings.Repeat(" ", realtimeArgumentsMax+1)} {
		if _, err := run(context.Background(), "weather", bad); err == nil {
			t.Errorf("accepted invalid arguments: %.60s", bad)
		}
	}
	if calls != 0 {
		t.Fatal("invalid arguments reached callback")
	}
	// A caller modifying the exposed schema cannot make extra arguments executable.
	defs[0].Parameters["properties"].(map[string]any)["unexpected"] = str("Do not accept")
	if _, err := run(context.Background(), "weather", `{"seconds":2,"unexpected":"bad"}`); err == nil {
		t.Fatal("advertised schema mutation widened permissions")
	}
	if out, err := run(context.Background(), "weather", `{"seconds":2}`); err != nil || out != "started" {
		t.Fatalf("valid call failed: %q %v", out, err)
	}
	if out, err := run(context.Background(), "weather", `{"seconds":0}`); err != nil || out != "error: seconds must be positive" {
		t.Fatal("existing tool semantic error was lost")
	}
	if calls != 2 {
		t.Fatal("wrong execution count")
	}
}

func TestRealtimeToolCancellationAndResultBound(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	callback := func(map[string]any) (string, error) { calls++; return strings.Repeat("x", realtimeResultMax+1), nil }
	available := []tool{{llm.Tool{Name: "weather", Parameters: object(map[string]any{})}, callback}}
	_, run, _ := realtimeTools("read", config.Brain{}, available, time.Now())
	cancel()
	if _, err := run(ctx, "weather", `{}`); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled tool ran")
	}
	if _, err := run(context.Background(), "weather", `{}`); err == nil || calls != 1 {
		t.Fatal("oversized tool result escaped")
	}
	ctx, cancel = context.WithCancel(context.Background())
	available[0].Run = func(map[string]any) (string, error) { cancel(); return "private result", nil }
	_, run, _ = realtimeTools("read", config.Brain{}, available, time.Now())
	if out, err := run(ctx, "weather", `{}`); !errors.Is(err, context.Canceled) || out != "" {
		t.Fatal("result escaped after cancellation")
	}
}

func TestRealtimeArgumentTypesAndEnumsFailClosed(t *testing.T) {
	schema := object(map[string]any{
		"count":   map[string]any{"type": "integer", "enum": []int{1, 2}},
		"enabled": map[string]any{"type": "boolean", "enum": []bool{true}},
		"level":   map[string]any{"type": "number", "enum": []any{1.5, 2}},
		"name":    map[string]any{"type": "string", "enum": []string{"one", "two"}},
	}, "count", "enabled", "level", "name")
	if err := realtimeArgs(schema, `{"count":2,"enabled":true,"level":1.5,"name":"one"}`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"count":1.5,"enabled":true,"level":1.5,"name":"one"}`,
		`{"count":3,"enabled":true,"level":1.5,"name":"one"}`,
		`{"count":2,"enabled":"true","level":1.5,"name":"one"}`,
		`{"count":2,"enabled":false,"level":1.5,"name":"one"}`,
		`{"count":2,"enabled":true,"level":3,"name":"one"}`,
		`{"count":2,"enabled":true,"level":1.5,"name":"three"}`,
	} {
		if err := realtimeArgs(schema, bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, kind := range []string{"array", "object", "unknown", ""} {
		schema := object(map[string]any{"value": map[string]any{"type": kind}})
		if err := realtimeArgs(schema, `{"value":[]}`); err == nil {
			t.Errorf("accepted unsupported type %q", kind)
		}
	}
	if err := realtimeArgs(object(map[string]any{"value": map[string]any{"type": "number", "enum": "bad"}}), `{"value":2}`); err == nil {
		t.Fatal("accepted malformed enum schema")
	}
}

func TestRealtimeReadSnapshotsDoNotCallDeviceGettersDuringExecution(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	calls := 0
	value := "initial weather"
	original := tool{llm.Tool{Name: "weather", Parameters: object(map[string]any{})}, func(map[string]any) (string, error) { calls++; return value, nil }}
	snapshot := snapshotRealtimeRead(original)
	value = "changed weather"
	_, run, _ := realtimeTools("read", config.Brain{}, []tool{snapshot}, time.Now())
	for range 3 {
		if out, err := run(context.Background(), "weather", `{}`); err != nil || (!strings.HasPrefix(out, "Snapshot at ") || !strings.HasSuffix(out, ": initial weather")) {
			t.Fatal("did not retain session snapshot")
		}
	}
	if calls != 1 {
		t.Fatal("tool worker reentered device getter")
	}
	huge := original
	huge.Run = func(map[string]any) (string, error) { return strings.Repeat("x", realtimeResultMax+1), nil }
	huge = snapshotRealtimeRead(huge)
	if out, err := huge.Run(nil); out != "" || err == nil {
		t.Fatal("oversized snapshot retained")
	}
}
