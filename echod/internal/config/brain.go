package config

// Brain is where a turn's speech and answer come from when they do not come from Home Assistant: the
// direct pipeline (feature/voice/direct.go). Speech to text and text to speech are Wyoming servers
// (faster-whisper and Piper, as Home Assistant itself uses), and the answer is a chat model behind an
// OpenAI-style endpoint (llama.cpp's server, Ollama, and so on), which acts through the device's own
// abilities as tools. Realtime instead sends audio directly to OpenAI. Empty Mode is Home
// Assistant's Assist pipeline, as always.
type Brain struct {
	Mode BrainMode `json:"mode,omitempty"`

	// Realtime connects directly to OpenAI using Key. Read-only tools are opt-in: empty or read.
	RealtimeModel string `json:"realtime_model,omitempty"`
	RealtimeVoice string `json:"realtime_voice,omitempty"`
	RealtimeTools string `json:"realtime_tools,omitempty"`

	// STT and TTS are the Wyoming servers, host:port.
	STT string `json:"stt,omitempty"`
	TTS string `json:"tts,omitempty"`
	// Voice is the Piper voice, like en_US-lessac-medium; empty is the server's default.
	Voice string `json:"voice,omitempty"`
	// Language is what the speech is in, for the recognizer; empty is English.
	Language string `json:"language,omitempty"`

	// LLM is the chat endpoint's base, like http://192.168.1.20:8080/v1, and Model the model to ask
	// for (a server with one model ignores it). Key is sent as a bearer token when set; it is a
	// secret, never shown again once saved.
	LLM   string `json:"llm,omitempty"`
	Model string `json:"model,omitempty"`
	Key   string `json:"key,omitempty"`

	// Search is a SearXNG server's address, like http://192.168.1.20:8888, with its JSON format on: the
	// model looks things up there and reads the pages it finds. Empty is no looking anything up.
	Search string `json:"search,omitempty"`

	// Prompt is added to the device's own instructions to the model: a name, a tone, what the
	// household wants it to know.
	Prompt string `json:"prompt,omitempty"`
}

type BrainMode string

const (
	BrainHomeAssistant BrainMode = ""
	BrainDirect        BrainMode = "direct"
	BrainRealtime      BrainMode = "realtime"
)

// Direct is whether turns go to the direct pipeline, and it is set up enough to run one.
func (b Brain) Direct() bool {
	return b.Mode == BrainDirect && b.STT != "" && b.TTS != "" && b.LLM != ""
}

type BrainWriter struct{ st *Store }

// Set replaces everything but the key: a form that shows the key as "set" and posts nothing for it
// must not clear it. SetWithKey saves a validated form with an explicit key change.
func (w BrainWriter) Set(b Brain) error {
	return w.SetWithKey(b, nil)
}

// SetVoice changes the speaking voice alone ("" for the server's default), so a choice on the screen
// cannot undo a setup page save made at the same moment.
func (w BrainWriter) SetVoice(voice string) error {
	return w.st.Update(func(c *Config) { c.Brain.Voice = voice })
}

func (w BrainWriter) SetKey(key string) error {
	return w.st.Update(func(c *Config) { c.Brain.Key = key })
}

// Realtime is an explicit backend choice, including a misconfigured one: never
// silently send the same request to a different service.
func (b Brain) Realtime() bool { return b.Mode == BrainRealtime }

// Empty model and voice follow these defaults without changing Home Assistant's default mode.
const (
	DefaultRealtimeModel = "gpt-realtime-2.1"
	DefaultRealtimeVoice = "marin"
)

func (b Brain) RealtimeModelName() string {
	if b.RealtimeModel != "" {
		return b.RealtimeModel
	}
	return DefaultRealtimeModel
}

func (b Brain) RealtimeVoiceName() string {
	if b.RealtimeVoice != "" {
		return b.RealtimeVoice
	}
	return DefaultRealtimeVoice
}

// SetWithKey saves a validated form in one write. A nil key keeps the existing secret; a pointer to
// an empty string removes it. Callers validate everything before changing either settings or key.
func (w BrainWriter) SetWithKey(b Brain, key *string) error {
	return w.st.Update(func(c *Config) {
		saved := c.Brain.Key
		c.Brain = b
		c.Brain.Key = saved
		if key != nil {
			c.Brain.Key = *key
		}
	})
}
