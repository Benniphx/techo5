package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
)

func init() { voice.SetRealtimeTools(NewRealtimeTools) }

const (
	realtimeArgumentsMax = 8 << 10
	realtimeResultMax    = 16 << 10
)

// NewRealtimeTools takes one session's snapshot of existing device abilities and instructions. The
// cloud gets no tools unless read scope was chosen. Mutating callbacks are excluded: their existing
// filesystem writes and hardware/hook operations cannot honor session cancellation.
func NewRealtimeTools(scope string) ([]llm.Tool, func(context.Context, string, string) (string, error), string) {
	b := config.Get().Brain
	if scope != "read" {
		return realtimeTools(scope, b, nil, time.Now())
	}
	available := tools()
	// Capture these small existing results while constructing the session. Their callbacks then need
	// no filesystem access, config lock or device work inside the cancellable tool worker.
	for i := range available {
		switch available[i].Name {
		case "list_timers", "list_alarms", "list_stations", "weather":
			available[i] = snapshotRealtimeRead(available[i])
		}
	}
	return realtimeTools(scope, b, available, time.Now())
}

// snapshotRealtimeRead runs only during session initialization, never inside the tool worker.
func snapshotRealtimeRead(t tool) tool {
	at := time.Now().Format(time.RFC3339)
	output, err := t.Run(map[string]any{})
	if err == nil {
		output = "Snapshot at " + at + ": " + output
	}
	if len(output) > realtimeResultMax {
		output = ""
		err = fmt.Errorf("tool result exceeds the limit")
	}
	t.Run = func(map[string]any) (string, error) { return output, err }
	return t
}

func realtimeTools(scope string, b config.Brain, available []tool, now time.Time) ([]llm.Tool, func(context.Context, string, string) (string, error), string) {
	var selected []tool
	for _, t := range available {
		allowed := false

		if scope == "read" {
			switch t.Name {
			case "list_timers", "list_alarms", "list_stations", "weather", "calendar":
				allowed = true
			}
		}
		if allowed {
			selected = append(selected, t)
		}
	}
	names := make([]string, len(selected))
	for i, t := range selected {
		names[i] = t.Name
	}
	// Reuse the ordinary assistant's instructions, with the session's authoritative capability list.
	prompt := instructions(b, now) + "\n\nFor this Realtime session only these tools are enabled: " + strings.Join(names, ", ") + ". Do not claim any other device capability or invent a tool. If no tools are enabled, you cannot read device information or control devices. Weather and calendar results use this device's configured sources and may be incomplete or stale."
	execute := func(ctx context.Context, name, args string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if len(args) > realtimeArgumentsMax {
			return "", fmt.Errorf("tool arguments exceed the limit")
		}
		var found *tool
		for i := range selected {
			if selected[i].Name == name {
				found = &selected[i]
				break
			}
		}
		if found == nil {
			return "", fmt.Errorf("tool is not enabled for this session")
		}
		if err := realtimeArgs(found.Parameters, args); err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		call := llm.ToolCall{}
		call.Function.Name, call.Function.Arguments = name, args
		result := run(selected, call)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if len(result) > realtimeResultMax {
			return "", fmt.Errorf("tool result exceeds the limit")
		}
		return result, nil
	}
	// Parameters are copied separately so mutations to the advertised schema cannot widen execution.
	definitions := specs(selected)
	for i := range definitions {
		raw, _ := json.Marshal(definitions[i].Parameters)
		var copied map[string]any
		_ = json.Unmarshal(raw, &copied)
		definitions[i].Parameters = copied
	}
	return definitions, execute, prompt
}

// realtimeArgs checks the existing tools' small object schemas before run applies their ordinary
// semantic checks. There are no arbitrary paths, shell commands or Home Assistant entity arguments.
func realtimeArgs(schema map[string]any, raw string) error {
	args := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil || args == nil {
			return fmt.Errorf("tool arguments must be a JSON object")
		}
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, name := range required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("required tool argument is missing")
		}
	}
	for name, value := range args {
		prop, ok := props[name].(map[string]any)
		if !ok {
			return fmt.Errorf("unknown tool argument")
		}

		switch prop["type"] {
		case "string":
			text, ok := value.(string)
			if !ok || len(text) > 2000 {
				return fmt.Errorf("invalid string tool argument")
			}
		case "number":
			if _, ok := value.(float64); !ok {
				return fmt.Errorf("invalid numeric tool argument")
			}
		case "integer":
			n, ok := value.(float64)
			if !ok || math.Trunc(n) != n {
				return fmt.Errorf("invalid integer tool argument")
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("invalid boolean tool argument")
			}
		default:
			return fmt.Errorf("unsupported tool argument type")
		}
		if choices, present := prop["enum"]; present {
			choices := reflect.ValueOf(choices)
			if choices.Kind() != reflect.Slice && choices.Kind() != reflect.Array {
				return fmt.Errorf("invalid tool argument schema")
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("invalid tool argument value")
			}
			valid := false
			for i := 0; i < choices.Len(); i++ {
				choice, err := json.Marshal(choices.Index(i).Interface())
				if err == nil && string(encoded) == string(choice) {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid tool argument choice")
			}
		}

	}
	return nil
}
