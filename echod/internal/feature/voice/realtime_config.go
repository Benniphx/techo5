package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	rtapi "github.com/HuskerMinion/techo5/echod/internal/lib/realtime"
)

const realtimePilotFile = "/tmp/techo5-realtime.json"

// A pilot override is temporary; saved settings use the existing root-only state.
// Reboot removes the override along with a temporarily mounted test daemon.
type pilotConfig struct {
	Enabled  bool   `json:"enabled"`
	KeyFile  string `json:"key_file,omitempty"`
	Model    string `json:"model,omitempty"`
	Voice    string `json:"voice,omitempty"`
	Language string `json:"language,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
	Tools    string `json:"tools,omitempty"`
	key      string
}

type realtimeToolProvider func(string) ([]llm.Tool, func(context.Context, string, string) (string, error), string)

var realtimeTools realtimeToolProvider

// SetRealtimeTools binds the assistant's existing abilities at initialization,
// without making the voice package depend on the assistant feature.
func SetRealtimeTools(provider realtimeToolProvider) { realtimeTools = provider }

func realtimeChosen() bool {
	if _, err := os.Lstat(realtimePilotFile); !errors.Is(err, os.ErrNotExist) {
		c, err := readRealtimeConfig()
		return err != nil || c.Enabled
	}
	return config.Get().Brain.Realtime()
}
func readRealtimeConfig() (pilotConfig, error) {
	_, err := os.Lstat(realtimePilotFile)
	if errors.Is(err, os.ErrNotExist) {
		b := config.Get().Brain
		if b.Realtime() && (b.STT != "" || b.TTS != "" || b.Voice != "" || b.LLM != "" || b.Model != "" || b.Search != "") {
			return pilotConfig{}, errors.New("realtime configuration contains incompatible direct settings")
		}
		return pilotConfig{Enabled: b.Realtime(), Model: b.RealtimeModelName(), Voice: b.RealtimeVoiceName(), Language: b.Language, Prompt: b.Prompt, Tools: b.RealtimeTools, key: b.Key}, nil
	}
	data, err := readRealtimePrivate(realtimePilotFile, 8192)
	if err != nil {
		return pilotConfig{}, errors.New("realtime pilot configuration unavailable or unsafe")
	}
	return decodeRealtimeConfig(data)
}
func decodeRealtimeConfig(data []byte) (pilotConfig, error) {
	var c pilotConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, errors.New("invalid realtime pilot configuration")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return c, errors.New("trailing realtime pilot configuration")
	}
	if c.Enabled && c.KeyFile == "" {
		return c, errors.New("realtime pilot needs a key file")
	}
	if len(data) > 8192 || (c.Tools != "" && c.Tools != "read") {
		return c, errors.New("invalid realtime pilot tools or size")
	}
	return c, nil
}
func (c pilotConfig) options() (rtapi.Options, error) {
	if !c.Enabled {
		return rtapi.Options{}, errors.New("realtime is disabled")
	}
	key := c.key
	if c.KeyFile != "" {
		var err error
		key, err = readRealtimeKey(c.KeyFile)
		if err != nil {
			return rtapi.Options{}, err
		}
	}
	if strings.TrimSpace(key) == "" {
		return rtapi.Options{}, errors.New("realtime credential unavailable")
	}
	if c.Tools != "" && c.Tools != "read" {
		return rtapi.Options{}, errors.New("invalid realtime tool scope")
	}
	b := config.Brain{RealtimeModel: c.Model, RealtimeVoice: c.Voice}
	o := rtapi.Options{Model: b.RealtimeModelName(), Voice: b.RealtimeVoiceName(), Key: key, Language: c.Language, Instructions: c.Prompt}
	if o.Language == "" {
		o.Language = "en"
	}
	if realtimeTools != nil {
		var instructions string
		o.Tools, o.RunTool, instructions = realtimeTools(c.Tools)
		o.Instructions = instructions
		// The provider includes the saved prompt. A pilot prompt is a temporary addition.
		if c.KeyFile != "" && c.Prompt != "" {
			o.Instructions += "\n" + c.Prompt
		}
	} else if c.Tools != "" {
		return rtapi.Options{}, errors.New("realtime tools unavailable")
	}
	return o, nil
}
func readRealtimePrivate(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("realtime private file unavailable")
	}
	source := os.NewFile(uintptr(fd), "realtime-private")
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, errors.New("realtime private file unsafe")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
		return nil, errors.New("realtime private file unsafe")
	}
	data, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("realtime private file unavailable or too large")
	}
	return data, nil
}
func readRealtimeKey(path string) (string, error) {
	data, err := readRealtimePrivate(path, 4096)
	if err != nil {
		return "", errors.New("realtime credential must be an owned private small regular file")
	}
	key := strings.TrimSpace(string(data))
	if key == "" || strings.ContainsAny(key, "\r\n\t ") {
		return "", errors.New("realtime credential invalid")
	}
	return key, nil
}
