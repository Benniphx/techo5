package assistant

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/alarm"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
)

// tool is one thing the model may do on the device. Run gets the model's arguments and returns what
// happened, for the model to put into words.
type tool struct {
	llm.Tool
	Run func(args map[string]any) (string, error)
}

func specs(ts []tool) []llm.Tool {
	out := make([]llm.Tool, len(ts))
	for i, t := range ts {
		out[i] = t.Tool
	}
	return out
}

func object(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

func argString(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return strings.TrimSpace(v)
}

func argNumber(args map[string]any, k string) (float64, bool) {
	switch v := args[k].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// tools is what the model may do, as the device is now: the lists it reads are read when asked.
func tools() []tool {
	return []tool{
		{llm.Tool{Name: "start_timer", Description: "Start a countdown timer on this device.",
			Parameters: object(map[string]any{
				"seconds": num("How long, in seconds."),
				"label":   str("What it is for, like pasta; empty if not said."),
			}, "seconds")},
			func(a map[string]any) (string, error) {
				s, ok := argNumber(a, "seconds")
				if !ok || s < 1 || s > 24*3600 {
					return "", errors.New("seconds must be between 1 and 86400")
				}
				d := time.Duration(math.Round(s)) * time.Second
				timer.Get().Start(argString(a, "label"), d)
				return "started a timer for " + d.String(), nil
			}},

		{llm.Tool{Name: "list_timers", Description: "List the timers running on this device and how long each has left.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				list := timer.Get().List(time.Now())
				if len(list) == 0 {
					return "no timers", nil
				}
				var s []string
				for _, t := range list {
					name := t.Name
					if name == "" {
						name = "timer"
					}
					s = append(s, fmt.Sprintf("%s: %s left", name, t.Left.Round(time.Second)))
				}
				return strings.Join(s, "; "), nil
			}},

		{llm.Tool{Name: "cancel_timers", Description: "Cancel timers: the one with this label, or all of them.",
			Parameters: object(map[string]any{"label": str("The timer's label; empty cancels all.")})},
			func(a map[string]any) (string, error) {
				label := strings.ToLower(argString(a, "label"))
				n := 0
				for _, t := range timer.Get().List(time.Now()) {
					if label == "" || strings.ToLower(t.Name) == label {
						if timer.Get().Cancel(t.ID) {
							n++
						}
					}
				}
				return fmt.Sprintf("canceled %d timer(s)", n), nil
			}},

		{llm.Tool{Name: "set_alarm", Description: "Set an alarm on this device.",
			Parameters: object(map[string]any{
				"time":  str("The time of day, 24-hour, as HH:MM."),
				"days":  str("once, daily, weekdays, weekends, or days like mon,wed,fri. Default once."),
				"label": str("What it is for; empty if not said."),
			}, "time")},
			func(a map[string]any) (string, error) {
				h, m, err := clock(argString(a, "time"))
				if err != nil {
					return "", err
				}
				days, err := config.ParseDays(argString(a, "days"))
				if err != nil {
					return "", err
				}
				al, err := alarm.Get().Set(h, m, days, argString(a, "label"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("alarm set for %02d:%02d (%s)", al.Hour, al.Minute, config.DaysLabel(al.Days)), nil
			}},

		{llm.Tool{Name: "stop", Description: "Stop whatever is ringing or playing on this device: an alarm, a timer, the radio or music.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				rang := ring.End()
				media.Get().Stop()
				if rang {
					return "stopped the ringing and anything playing", nil
				}
				return "stopped anything playing", nil
			}},

		{llm.Tool{Name: "list_stations", Description: "List the radio stations this device can play.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				r := home.Get().Radio()
				if len(r.Stations) == 0 {
					return "no stations are set up", nil
				}
				return strings.Join(r.Stations, "; "), nil
			}},

		{llm.Tool{Name: "play_radio", Description: "Play a radio station. Use a name from list_stations.",
			Parameters: object(map[string]any{"station": str("The station's name.")}, "station")},
			func(a map[string]any) (string, error) {
				want := argString(a, "station")
				name, ok := station(want)
				if !ok {
					return "", fmt.Errorf("no station called %q; call list_stations", want)
				}
				home.Get().Play(name)
				return "playing " + name, nil
			}},

		{llm.Tool{Name: "set_volume", Description: fmt.Sprintf("Set the speaker volume, 0 to %d.", media.VolumeSteps),
			Parameters: object(map[string]any{"level": num(fmt.Sprintf("0 to %d.", media.VolumeSteps))}, "level")},
			func(a map[string]any) (string, error) {
				v, ok := argNumber(a, "level")
				if !ok {
					return "", errors.New("level is a number")
				}
				media.Get().Set(int(math.Round(v)))
				return fmt.Sprintf("volume is %d of %d", media.Get().Volume(), media.VolumeSteps), nil
			}},

		// Not "call": llama.cpp's grammar for a tool call has a rule by that name, and a tool called the
		// same makes the whole list of tools unusable.
		{llm.Tool{Name: "call_device", Description: "Call another device (an intercom call to a room or a family member's device). Use a name from list_callees.",
			Parameters: object(map[string]any{"who": str("Who or which room to call.")}, "who")},
			func(a map[string]any) (string, error) {
				want := strings.ToLower(argString(a, "who"))
				for _, c := range phone.Get().Callees() {
					if c.Device && strings.ToLower(c.Name) == want {
						if err := phone.Get().CallDevice(c.Name); err != nil {
							return "", err
						}
						return "calling " + c.Name, nil
					}
				}
				return "", fmt.Errorf("nobody called %q; call list_callees", want)
			}},

		{llm.Tool{Name: "list_callees", Description: "List the devices this one can call.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				var s []string
				for _, c := range phone.Get().Callees() {
					if c.Device {
						s = append(s, c.Name)
					}
				}
				if len(s) == 0 {
					return "no other devices can be called", nil
				}
				return strings.Join(s, "; "), nil
			}},

		{llm.Tool{Name: "weather", Description: "The weather here now.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				w := home.Get().Weather()
				if w.Condition == "" {
					return "", errors.New("this device has no weather source")
				}
				return fmt.Sprintf("%+v", w), nil
			}},
	}
}

// clock reads HH:MM, 24-hour.
func clock(s string) (int, int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not a time as HH:MM", s)
	}
	return t.Hour(), t.Minute(), nil
}

// station finds a station by name the way somebody says it: case aside, and a part of the name will
// do when only one station has it.
func station(want string) (string, bool) {
	w := strings.ToLower(strings.TrimSpace(want))
	if w == "" {
		return "", false
	}
	list := home.Get().Radio().Stations
	for _, s := range list {
		if strings.ToLower(s) == w {
			return s, true
		}
	}
	var found []string
	for _, s := range list {
		if strings.Contains(strings.ToLower(s), w) {
			found = append(found, s)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return "", false
}
