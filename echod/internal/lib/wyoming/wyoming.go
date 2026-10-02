// Package wyoming speaks the Wyoming protocol that Home Assistant's speech servers use, enough to
// have one transcribe (faster-whisper) and one synthesize (Piper) without Home Assistant between.
//
// An event is a line of JSON - its type, and how long the data and the payload after it are - then
// the data (more JSON) and the payload (raw audio). See github.com/OHF-Voice/wyoming.
package wyoming

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Format is raw PCM: samples a second, bytes a sample, channels.
type Format struct {
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

// Mic is what the device's microphone gives: 16 kHz, 16-bit, mono.
var Mic = Format{Rate: 16000, Width: 2, Channels: 1}

const dialTimeout = 5 * time.Second

// maxBody bounds what one event may carry, so a server that is not a Wyoming server cannot have the
// device allocate whatever a stray length says.
const maxBody = 8 << 20

type header struct {
	Type          string          `json:"type"`
	Data          json.RawMessage `json:"data,omitempty"`
	DataLength    int             `json:"data_length,omitempty"`
	PayloadLength int             `json:"payload_length,omitempty"`
}

type conn struct {
	c    net.Conn
	r    *bufio.Reader
	stop func() bool
}

func dial(ctx context.Context, addr string) (*conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Anything in flight stops when the turn does.
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	return &conn{c: c, r: bufio.NewReader(c), stop: stop}, nil
}

func (c *conn) close() {
	c.stop()
	_ = c.c.Close()
}

func (c *conn) send(typ string, data any, payload []byte) error {
	var body []byte
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		body = b
	}
	h, _ := json.Marshal(header{Type: typ, DataLength: len(body), PayloadLength: len(payload)})
	buf := make([]byte, 0, len(h)+1+len(body)+len(payload))
	buf = append(append(append(append(buf, h...), '\n'), body...), payload...)
	_, err := c.c.Write(buf)
	return err
}

func (c *conn) recv() (string, map[string]any, []byte, error) {
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return "", nil, nil, err
	}
	var h header
	if err := json.Unmarshal(line, &h); err != nil {
		return "", nil, nil, fmt.Errorf("wyoming: not an event: %w", err)
	}
	if h.DataLength < 0 || h.PayloadLength < 0 || h.DataLength > maxBody || h.PayloadLength > maxBody {
		return "", nil, nil, errors.New("wyoming: event too large")
	}
	data := map[string]any{}
	if len(h.Data) > 0 {
		_ = json.Unmarshal(h.Data, &data)
	}
	if h.DataLength > 0 {
		b := make([]byte, h.DataLength)
		if _, err := io.ReadFull(c.r, b); err != nil {
			return "", nil, nil, err
		}
		_ = json.Unmarshal(b, &data)
	}
	var payload []byte
	if h.PayloadLength > 0 {
		payload = make([]byte, h.PayloadLength)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return "", nil, nil, err
		}
	}
	return h.Type, data, payload, nil
}

// Transcriber is one utterance on its way to a speech-to-text server: audio goes in as it is heard,
// and Finish says it is over and waits for the text.
type Transcriber struct {
	c *conn
}

// Transcribe opens an utterance in language ("" for the server's own) of audio in the Mic format.
func Transcribe(ctx context.Context, addr, language string) (*Transcriber, error) {
	c, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	req := map[string]any{}
	if language != "" {
		req["language"] = language
	}
	if err := c.send("transcribe", req, nil); err != nil {
		c.close()
		return nil, err
	}
	if err := c.send("audio-start", Mic, nil); err != nil {
		c.close()
		return nil, err
	}
	return &Transcriber{c: c}, nil
}

// Audio sends one chunk of 16-bit little-endian samples.
func (t *Transcriber) Audio(pcm []byte) error { return t.c.send("audio-chunk", Mic, pcm) }

// Finish ends the utterance and returns what was said.
func (t *Transcriber) Finish() (string, error) {
	defer t.c.close()
	if err := t.c.send("audio-stop", nil, nil); err != nil {
		return "", err
	}
	for {
		typ, data, _, err := t.c.recv()
		if err != nil {
			return "", err
		}
		if typ == "transcript" {
			text, _ := data["text"].(string)
			return text, nil
		}
		if typ == "error" {
			msg, _ := data["text"].(string)
			return "", fmt.Errorf("wyoming: %s", msg)
		}
	}
}

// Close gives up on an utterance.
func (t *Transcriber) Close() { t.c.close() }

// Synthesize speaks text in voice ("" for the server's default), and returns the samples and their
// format as the server sent them.
func Synthesize(ctx context.Context, addr, text, voice string) ([]int16, Format, error) {
	c, err := dial(ctx, addr)
	if err != nil {
		return nil, Format{}, err
	}
	defer c.close()
	req := map[string]any{"text": text}
	if voice != "" {
		req["voice"] = map[string]string{"name": voice}
	}
	if err := c.send("synthesize", req, nil); err != nil {
		return nil, Format{}, err
	}
	var f Format
	var pcm []byte
	for {
		typ, data, payload, err := c.recv()
		if err != nil {
			return nil, Format{}, err
		}
		switch typ {
		case "audio-start":
			f = formatOf(data)
		case "audio-chunk":
			if f.Rate == 0 {
				f = formatOf(data)
			}
			pcm = append(pcm, payload...)
			if len(pcm) > maxBody {
				return nil, Format{}, errors.New("wyoming: speech too long")
			}
		case "audio-stop":
			if f.Width != 2 || f.Channels != 1 || f.Rate <= 0 {
				return nil, Format{}, fmt.Errorf("wyoming: speech in %+v, not 16-bit mono", f)
			}
			out := make([]int16, len(pcm)/2)
			for i := range out {
				out[i] = int16(binary.LittleEndian.Uint16(pcm[2*i:]))
			}
			return out, f, nil
		case "error":
			msg, _ := data["text"].(string)
			return nil, Format{}, fmt.Errorf("wyoming: %s", msg)
		}
	}
}

// Voices asks a text-to-speech server which voices it can speak in now, and returns their names,
// like en_US-ryan-medium, for the language given ("en" or "en_US"; "" for all). A voice the server
// lists but has not downloaded is left out: asking for it would fail or stall the reply while it
// fetches.
func Voices(ctx context.Context, addr, language string) ([]string, error) {
	c, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer c.close()
	if err := c.send("describe", nil, nil); err != nil {
		return nil, err
	}
	for {
		typ, data, _, err := c.recv()
		if err != nil {
			return nil, err
		}
		if typ == "info" {
			return voicesIn(data, language), nil
		}
	}
}

// voicesIn picks the voice names out of an info event's data.
func voicesIn(data map[string]any, language string) []string {
	var names []string
	programs, _ := data["tts"].([]any)
	for _, p := range programs {
		prog, _ := p.(map[string]any)
		voices, _ := prog["voices"].([]any)
		for _, v := range voices {
			voice, _ := v.(map[string]any)
			name, _ := voice["name"].(string)
			if installed, ok := voice["installed"].(bool); name == "" || (ok && !installed) {
				continue
			}
			if language == "" || speaks(voice, name, language) {
				names = append(names, name)
			}
		}
	}
	return names
}

// speaks reports whether a voice is in language, from its languages list or else its name's prefix.
// "en" takes en_US and en_GB; "en_US" takes only en_US.
func speaks(voice map[string]any, name, language string) bool {
	match := func(l string) bool {
		l = strings.ReplaceAll(l, "-", "_")
		return strings.EqualFold(l, language) || strings.HasPrefix(strings.ToLower(l), strings.ToLower(language)+"_")
	}
	if langs, ok := voice["languages"].([]any); ok && len(langs) > 0 {
		for _, l := range langs {
			if s, _ := l.(string); match(s) {
				return true
			}
		}
		return false
	}
	lang, _, _ := strings.Cut(name, "-")
	return match(lang)
}

func formatOf(data map[string]any) Format {
	n := func(k string) int {
		v, _ := data[k].(float64)
		return int(v)
	}
	return Format{Rate: n("rate"), Width: n("width"), Channels: n("channels")}
}
