package realtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// gemini is Google's Gemini Live API.
type gemini struct{}

const (
	geminiBase  = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
	geminiModel = "gemini-2.5-flash-native-audio-preview-09-2025"
	geminiVoice = "Puck"
	geminiRate  = 24000
)

func (g gemini) address(c Config) (string, http.Header, error) {
	addr, keyOK, err := endpoint(c, geminiBase)
	if err != nil {
		return "", nil, err
	}
	u, err := url.Parse(addr)
	if err != nil {
		return "", nil, err
	}
	h := http.Header{}
	switch {
	case c.Key != "" && keyOK:
		// In a header, not the address, so it is in no log line that quotes the address.
		h.Set("x-goog-api-key", c.Key)
	case c.Endpoint == "" && c.Key == "":
		return "", nil, ErrNoKey
	}
	return u.String(), h, nil
}

func (g gemini) setup(c Config) []any {
	model := c.Model
	if model == "" {
		model = geminiModel
	}
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	voice := c.Voice
	if voice == "" {
		voice = geminiVoice
	}
	var decls []map[string]any
	for _, f := range c.Functions {
		d := map[string]any{"name": f.Name, "description": f.Description}
		if f.Parameters != nil {
			// As JSON Schema, which Home Assistant's tools are written in: "parameters" takes only a part
			// of OpenAPI's.
			d["parametersJsonSchema"] = f.Parameters
		}
		decls = append(decls, d)
	}
	setup := map[string]any{
		"model": model,
		"generationConfig": map[string]any{
			"responseModalities": []string{"AUDIO"},
			"speechConfig": map[string]any{
				"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": voice}},
			},
		},
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": c.Instructions}}},
		// The device says when somebody has finished.
		"realtimeInputConfig":      map[string]any{"automaticActivityDetection": map[string]any{"disabled": true}},
		"inputAudioTranscription":  map[string]any{},
		"outputAudioTranscription": map[string]any{},
	}
	if len(decls) > 0 {
		setup["tools"] = []any{map[string]any{"functionDeclarations": decls}}
	}
	return []any{
		map[string]any{"setup": setup},
		map[string]any{"realtimeInput": map[string]any{"activityStart": map[string]any{}}},
	}
}

func (g gemini) audio(pcm16k []int16) any {
	return map[string]any{"realtimeInput": map[string]any{
		"audio": map[string]any{"data": pcmBase64(pcm16k), "mimeType": "audio/pcm;rate=16000"},
	}}
}

func (g gemini) endOfSpeech() []any {
	return []any{map[string]any{"realtimeInput": map[string]any{"activityEnd": map[string]any{}}}}
}

// goOn is nothing: Gemini goes on by itself once a call is answered.
func (g gemini) goOn() []any { return nil }

// ready is the setupComplete that answers the setup: until then nothing else is sent.
func (g gemini) ready(msg []byte) bool {
	var m struct {
		SetupComplete *struct{} `json:"setupComplete"`
	}
	return json.Unmarshal(msg, &m) == nil && m.SetupComplete != nil
}

func (g gemini) readyFirst() bool { return false }

func (g gemini) toolResult(callID, name, output string) []any {
	return []any{map[string]any{"toolResponse": map[string]any{"functionResponses": []any{
		map[string]any{"id": callID, "name": name, "response": map[string]any{"result": output}},
	}}}}
}

func (g gemini) read(msg []byte, emit func(Event)) {
	var m struct {
		ServerContent *struct {
			ModelTurn *struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"modelTurn"`
			TurnComplete        bool                   `json:"turnComplete"`
			Interrupted         bool                   `json:"interrupted"`
			InputTranscription  *struct{ Text string } `json:"inputTranscription"`
			OutputTranscription *struct{ Text string } `json:"outputTranscription"`
		} `json:"serverContent"`
		ToolCall *struct {
			FunctionCalls []struct {
				ID   string          `json:"id"`
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			} `json:"functionCalls"`
		} `json:"toolCall"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(msg, &m) != nil {
		return
	}
	if m.Error != nil {
		emit(Event{Kind: Failed, Err: errors.New("realtime: " + m.Error.Message)})
		return
	}
	if sc := m.ServerContent; sc != nil {
		if sc.InputTranscription != nil && sc.InputTranscription.Text != "" {
			emit(Event{Kind: Heard, Text: sc.InputTranscription.Text})
		}
		if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" {
			emit(Event{Kind: Saying, Text: sc.OutputTranscription.Text})
		}
		if sc.ModelTurn != nil {
			for _, p := range sc.ModelTurn.Parts {
				if p.InlineData == nil || !strings.HasPrefix(p.InlineData.MimeType, "audio/pcm") {
					continue
				}
				if pcm := pcmFrom(p.InlineData.Data); len(pcm) > 0 {
					emit(Event{Kind: Audio, PCM: pcm, Rate: rateOf(p.InlineData.MimeType)})
				}
			}
		}
		if sc.Interrupted {
			emit(Event{Kind: Interrupt})
		}
		if sc.TurnComplete {
			emit(Event{Kind: Done})
		}
	}
	if tc := m.ToolCall; tc != nil {
		for _, f := range tc.FunctionCalls {
			args := string(f.Args)
			if args == "" || args == "null" {
				args = "{}"
			}
			emit(Event{Kind: Call, CallID: f.ID, Name: f.Name, Args: args})
		}
	}
}

// rateOf is the rate an audio type says, as in audio/pcm;rate=24000; Gemini's own when it says none.
func rateOf(mime string) int {
	if i := strings.Index(mime, "rate="); i >= 0 {
		if r, err := strconv.Atoi(mime[i+5:]); err == nil && r > 0 && r <= 48000 {
			return r
		}
	}
	return geminiRate
}
