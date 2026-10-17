// Package realtime is a speech-to-speech session with a model: the microphone goes up as it is heard, and
// the answer comes back as speech, as it is made, with no speech-to-text or text-to-speech in between.
// OpenAI's Realtime API and xAI's (which speaks the same events) and Google's Gemini Live are spoken here.
//
// The device decides when somebody has finished speaking, as it does for every other assistant: the
// provider's own detection of speech is turned off, and EndOfSpeech asks for the answer. That keeps the
// turn, the follow-up and the stop word the device's, whoever answers.
//
// Only the functions a session is given are offered to the model, and none of a provider's own tools (a
// web search of its own, code, files) are ever turned on: what a turn may do is decided on the device.
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Providers.
const (
	OpenAI = "openai"
	XAI    = "xai"
	Gemini = "gemini"
)

// Function is a function the model may call.
type Function struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Config is a session to open.
type Config struct {
	Provider string
	Key      string
	// Endpoint, when set, is used in place of the provider's own address: a relay on the home network
	// that holds the key, say. The key is sent only to the provider's own address, or to an endpoint
	// given with one.
	Endpoint     string
	Model        string
	Voice        string
	Instructions string
	Functions    []Function
}

// Kinds of event.
const (
	Heard     = "heard"     // what was heard, transcribed (Text)
	Saying    = "saying"    // a piece of the answer's words (Text)
	Said      = "said"      // the answer's words, whole (Text)
	Audio     = "audio"     // a piece of the answer's speech (PCM, Rate)
	Call      = "call"      // the model calls a function (CallID, Name, Args)
	Done      = "done"      // the answer is finished
	Failed    = "failed"    // the session failed (Err)
	Interrupt = "interrupt" // the provider cut its answer short
)

// Event is something from the session.
type Event struct {
	Kind   string
	Text   string
	PCM    []int16
	Rate   int
	CallID string
	Name   string
	Args   string // JSON
	Err    error
}

// codec is one provider's way of saying things.
type codec interface {
	address(c Config) (string, http.Header, error)
	setup(c Config) []any
	audio(pcm16k []int16) any
	endOfSpeech() []any
	toolResult(callID, name, output string) []any
	cancel() any
	read(msg []byte, emit func(Event))
}

// Session is one open session.
type Session struct {
	conn   *websocket.Conn
	codec  codec
	events chan Event

	wmu    sync.Mutex
	closed chan struct{}
	once   sync.Once
}

const (
	// maxMessage is the most one message from a provider may be: a second of speech, encoded, is under
	// a hundred kilobytes.
	maxMessage = 4 << 20
	// writeWithin is how long one message may take to go.
	writeWithin = 10 * time.Second
	// dialWithin is how long the connection may take.
	dialWithin = 15 * time.Second
)

// ErrNoKey is a provider asked for with no key and no endpoint of its own.
var ErrNoKey = errors.New("realtime: no key")

// Dial opens a session and sends its setup.
func Dial(ctx context.Context, c Config) (*Session, error) {
	cd, err := codecFor(c.Provider)
	if err != nil {
		return nil, err
	}
	addr, header, err := cd.address(c)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{HandshakeTimeout: dialWithin, Proxy: nil}
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	conn, resp, err := d.DialContext(ctx, addr, header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("realtime: %s answered %s", c.Provider, resp.Status)
		}
		return nil, fmt.Errorf("realtime: %s: %w", c.Provider, scrub(err, c.Key))
	}
	conn.SetReadLimit(maxMessage)
	s := &Session{conn: conn, codec: cd, events: make(chan Event, 256), closed: make(chan struct{})}
	for _, m := range cd.setup(c) {
		if err := s.write(m); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	go s.readLoop()
	return s, nil
}

func codecFor(provider string) (codec, error) {
	switch provider {
	case OpenAI:
		return openAI{base: "wss://api.openai.com/v1/realtime", defaultModel: "gpt-realtime", defaultVoice: "marin"}, nil
	case XAI:
		return openAI{base: "wss://api.x.ai/v1/realtime", defaultModel: "grok-voice", defaultVoice: "Ara"}, nil
	case Gemini:
		return gemini{}, nil
	}
	return nil, fmt.Errorf("realtime: not a provider: %q", provider)
}

// endpoint is the address to use, and whether the key may go to it: the provider's own always, an
// endpoint of the user's own only when it is a secure one (wss).
func endpoint(c Config, own string) (addr string, keyOK bool, err error) {
	if c.Endpoint == "" {
		return own, true, nil
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return "", false, errors.New("realtime: the endpoint is not a ws:// or wss:// address")
	}
	return c.Endpoint, u.Scheme == "wss", nil
}

// Events is what the session says, until it closes.
func (s *Session) Events() <-chan Event { return s.events }

// Audio sends a piece of the microphone, 16 kHz.
func (s *Session) Audio(pcm16k []int16) error { return s.write(s.codec.audio(pcm16k)) }

// EndOfSpeech says the person has finished, and asks for the answer.
func (s *Session) EndOfSpeech() error {
	for _, m := range s.codec.endOfSpeech() {
		if err := s.write(m); err != nil {
			return err
		}
	}
	return nil
}

// ToolResult answers a function call, and has the model go on.
func (s *Session) ToolResult(callID, name, output string) error {
	for _, m := range s.codec.toolResult(callID, name, output) {
		if err := s.write(m); err != nil {
			return err
		}
	}
	return nil
}

// Cancel stops the answer being made.
func (s *Session) Cancel() error {
	if m := s.codec.cancel(); m != nil {
		return s.write(m)
	}
	return nil
}

// Close ends the session at once.
func (s *Session) Close() error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		err = s.conn.Close()
	})
	return err
}

func (s *Session) write(m any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	select {
	case <-s.closed:
		return errors.New("realtime: closed")
	default:
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeWithin))
	return s.conn.WriteJSON(m)
}

func (s *Session) readLoop() {
	defer close(s.events)
	emit := func(e Event) {
		select {
		case s.events <- e:
		case <-s.closed:
		}
	}
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			select {
			case <-s.closed:
			default:
				emit(Event{Kind: Failed, Err: err})
			}
			return
		}
		s.codec.read(msg, emit)
	}
}

// scrub takes a key out of an error, which can quote the address it was in.
func scrub(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "<key>"))
}

// pcmBase64 is samples as little-endian bytes, in base64.
func pcmBase64(pcm []int16) string {
	b := make([]byte, 2*len(pcm))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// pcmFrom is base64 little-endian bytes as samples.
func pcmFrom(s string) []int16 {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

// upTo24k is 16 kHz speech at 24 kHz, two samples become three, by straight lines between them.
func upTo24k(in []int16) []int16 {
	if len(in) == 0 {
		return nil
	}
	out := make([]int16, 0, len(in)*3/2+1)
	for i := 0; i+1 < len(in); i += 2 {
		a, b := int32(in[i]), int32(in[i+1])
		c := b
		if i+2 < len(in) {
			c = int32(in[i+2])
		}
		out = append(out, int16(a), int16((a+2*b)/3), int16((2*b+c)/3))
	}
	if len(in)%2 == 1 {
		out = append(out, in[len(in)-1])
	}
	return out
}
