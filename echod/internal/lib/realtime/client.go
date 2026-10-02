// Package realtime implements a direct OpenAI Realtime GA audio session. Each
// Run owns one connection; reconnecting requires a deliberate local wake.
package realtime

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/gorilla/websocket"
)

const (
	endpoint                  = "wss://api.openai.com/v1/realtime"
	maxEventBytes             = 512 << 10
	maxToolCalls              = 32
	maxToolResultBytes        = 64 << 10
	maxSessionToolResultBytes = 256 << 10
	maxTranscriptRunes        = 1200
	maxPendingAudioBytes      = 24000 * 2 * 60
	maxPendingAudioPackets    = 4096
)

// ErrAudioWouldBlock lets the bounded hardware jitter buffer apply backpressure
// without blocking provider event handling. Run retries the same PCM later.
var ErrAudioWouldBlock = errors.New("realtime playback buffer full")

// Options contains an in-memory credential and the existing assistant tools.
// RunTool must honor its context, including cancellation during interruption.
// PlayedAudioMS reports audio actually played since the last output_start.
// PlaybackEmpty includes the local hardware tail and gates item handoffs.
type Options struct {
	Model, Voice, Language, Instructions, Key string
	Tools                                     []llm.Tool
	RunTool                                   func(context.Context, string, string) (string, error)
	PlayedAudioMS                             func() int
	PlaybackEmpty                             func() bool
}

type Event struct {
	Type, Phase, Role, Content, ItemID string
	FollowUp, Terminal                 bool
}

func (e Event) Text() string { return boundedText(e.Content) }

func boundedText(s string) string {
	if utf8.RuneCountInString(s) <= maxTranscriptRunes {
		return s
	}
	r := []rune(s)
	return string(r[len(r)-maxTranscriptRunes:])
}

func identifier(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func (o Options) sessionUpdate() (map[string]any, map[string]bool, error) {
	key := strings.TrimSpace(o.Key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") || !identifier(o.Model, 128) || !identifier(o.Voice, 32) || len(o.Instructions) > 16000 {
		return nil, nil, errors.New("invalid realtime configuration or unavailable credential")
	}
	if o.Language != "" {
		if len(o.Language) != 2 || o.Language[0] < 'a' || o.Language[0] > 'z' || o.Language[1] < 'a' || o.Language[1] > 'z' {
			return nil, nil, errors.New("realtime transcription requires an ISO 639-1 language")
		}
	}
	format := map[string]any{"type": "audio/pcm", "rate": 24000}
	input := map[string]any{"format": format, "turn_detection": map[string]any{"type": "server_vad", "interrupt_response": true, "create_response": true}}
	if o.Language != "" {
		input["transcription"] = map[string]any{"model": "gpt-4o-mini-transcribe", "language": o.Language}
	}
	session := map[string]any{
		"type": "realtime", "model": o.Model, "output_modalities": []string{"audio"}, "instructions": o.Instructions,
		"audio": map[string]any{"input": input, "output": map[string]any{"format": format, "voice": o.Voice}},
	}
	allowed := make(map[string]bool, len(o.Tools))
	if len(o.Tools) > 16 || (len(o.Tools) > 0 && o.RunTool == nil) {
		return nil, nil, errors.New("invalid realtime tool configuration")
	}
	tools := make([]map[string]any, 0, len(o.Tools))
	for _, tool := range o.Tools {
		if !identifier(tool.Name, 64) || allowed[tool.Name] || len(tool.Description) > 4096 || tool.Parameters == nil {
			return nil, nil, errors.New("invalid realtime tool definition")
		}
		allowed[tool.Name] = true
		tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
	}
	session["tools"] = tools
	session["tool_choice"] = "auto"
	update := map[string]any{"type": "session.update", "session": session}
	encoded, err := json.Marshal(update)
	if err != nil || len(encoded) > 65536 {
		return nil, nil, errors.New("realtime session configuration is too large or invalid")
	}
	return update, allowed, nil
}

// trustedDialer never uses the daemon's general HTTP/TLS transport, whose
// compatibility settings may permit unverified device certificates.
func trustedDialer() *websocket.Dialer {
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second, Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
}

// Run uses only the fixed provider endpoint; a user-configurable URL must never
// receive this provider credential. Callbacks are synchronous and must not block.
func Run(ctx context.Context, o Options, q *Queue, event func(Event) error, audio func([]byte) error) error {
	return run(ctx, o, q, event, audio, endpoint+"?model="+url.QueryEscape(o.Model), trustedDialer())
}

type wireItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
}

type wireResponse struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Output []wireItem `json:"output"`
}

type wireEvent struct {
	Type         string       `json:"type"`
	ResponseID   string       `json:"response_id"`
	ItemID       string       `json:"item_id"`
	ContentIndex int          `json:"content_index"`
	Delta        string       `json:"delta"`
	Transcript   string       `json:"transcript"`
	Response     wireResponse `json:"response"`
	Session      struct {
		Type             string   `json:"type"`
		OutputModalities []string `json:"output_modalities"`
		Audio            struct {
			Input struct {
				Format        wireFormat `json:"format"`
				TurnDetection struct {
					Type              string `json:"type"`
					InterruptResponse bool   `json:"interrupt_response"`
					CreateResponse    bool   `json:"create_response"`
				} `json:"turn_detection"`
			} `json:"input"`
			Output struct {
				Format wireFormat `json:"format"`
			} `json:"output"`
		} `json:"audio"`
	} `json:"session"`
}

type wireFormat struct {
	Type string `json:"type"`
	Rate int    `json:"rate"`
}
type command struct {
	payload any
	valid   context.Context
}
type readResult struct {
	event wireEvent
	err   error
}
type responseState struct {
	ctx                          context.Context
	cancel                       context.CancelFunc
	done, interrupted, continued bool
	followUp                     bool
	pending, calls               int
}
type toolJob struct {
	ctx                                 context.Context
	responseID, callID, name, arguments string
}
type toolResult struct{ responseID, callID, output string }

// run accepts an internal transport seam for offline TLS/WebSocket tests. The
// exported API deliberately exposes neither an endpoint nor a TLS bypass.
func run(parent context.Context, o Options, q *Queue, event func(Event) error, audio func([]byte) error, target string, dialer *websocket.Dialer) error {
	initial, allowed, err := o.sessionUpdate()
	if err != nil {
		return err
	}
	if q == nil || event == nil || audio == nil {
		return errors.New("realtime requires audio and event callbacks")
	}
	began := time.Now()
	connect, stopConnect := context.WithTimeout(parent, 5*time.Second)
	c, response, err := dialer.DialContext(connect, target, http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(o.Key)}})
	stopConnect()
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return errors.New("realtime TLS or authentication connection failed")
	}
	c.SetReadLimit(maxEventBytes)
	_ = c.SetReadDeadline(began.Add(5 * time.Second))
	ctx, cancel := context.WithCancel(parent)
	var workers sync.WaitGroup
	defer func() { cancel(); c.Close(); workers.Wait() }()
	start := make(chan struct{})
	commands := make(chan command, 32)
	incoming := make(chan readResult, 8)
	writerError := make(chan error, 1)
	jobs := make(chan toolJob, 4)
	results := make(chan toolResult, 4)
	workers.Go(func() { <-ctx.Done(); c.Close() })
	workers.Go(func() { writeLoop(ctx, c, initial, q, start, commands, writerError) })
	workers.Go(func() { readLoop(ctx, c, incoming) })
	workers.Go(func() { toolLoop(ctx, o.RunTool, jobs, results) })
	state := sessionState{
		ctx: ctx, options: o, allowed: allowed, emit: event, audio: audio, commands: commands, jobs: jobs,
		responses: make(map[string]*responseState), seenCalls: make(map[string]bool),
	}
	ready := false
	playback := time.NewTicker(10 * time.Millisecond)
	defer playback.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-writerError:
			return err
		case <-playback.C:
			if err := state.drainAudio(); err != nil {
				return err
			}
		case result := <-results:
			if err := state.toolDone(result); err != nil {
				return err
			}
		case result := <-incoming:
			if result.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return result.err
			}
			e := result.event
			if e.Type == "error" {
				return errors.New("realtime provider reported an error")
			}
			if !ready {
				if e.Type == "session.created" {
					continue
				}
				if e.Type != "session.updated" || !validSession(e) {
					return errors.New("realtime session negotiation failed")
				}
				if err := state.emitEvent(Event{Type: "ready", Phase: "listening"}); err != nil {
					return err
				}
				ready = true
				close(start)
				continue
			}
			if err := state.handle(e); err != nil {
				return err
			}
			if err := state.drainAudio(); err != nil {
				return err
			}
		}
	}
}

func validSession(e wireEvent) bool {
	s := e.Session
	v := s.Audio.Input.TurnDetection
	return s.Type == "realtime" && len(s.OutputModalities) == 1 && s.OutputModalities[0] == "audio" &&
		s.Audio.Input.Format == (wireFormat{"audio/pcm", 24000}) && s.Audio.Output.Format == (wireFormat{"audio/pcm", 24000}) &&
		v.Type == "server_vad" && v.InterruptResponse && v.CreateResponse
}

func writeLoop(ctx context.Context, c *websocket.Conn, initial any, q *Queue, start <-chan struct{}, commands <-chan command, failure chan<- error) {
	write := func(value any) bool {
		if ctx.Err() != nil {
			return false
		}
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if c.WriteJSON(value) != nil {
			select {
			case failure <- errors.New("realtime connection write failed"):
			default:
			}
			c.Close()
			return false
		}
		return true
	}
	if !write(initial) {
		return
	}
	var input <-chan struct{}
	var resampler inputResampler
	for {
		// Control gets priority over queued startup audio. All writes belong to
		// this goroutine, including tool results and interruption truncation.
		select {
		case cmd := <-commands:
			if cmd.valid.Err() == nil && !write(cmd.payload) {
				return
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-start:
			input, start = q.ready, nil
		case cmd := <-commands:
			if cmd.valid.Err() == nil && !write(cmd.payload) {
				return
			}
		case <-input:
			if samples := q.pop(); len(samples) > 0 {
				data := resampler.run(samples)
				if !write(map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(data)}) {
					return
				}
			}
		}
	}
}

func readLoop(ctx context.Context, c *websocket.Conn, incoming chan<- readResult) {
	for {
		kind, data, err := c.ReadMessage()
		result := readResult{}
		if err != nil {
			result.err = errors.New("realtime connection lost")
		} else if kind != websocket.TextMessage || json.Unmarshal(data, &result.event) != nil || result.event.Type == "" {
			result.err = errors.New("invalid realtime event")
		}
		if result.err == nil && result.event.Type == "session.updated" {
			// Read methods, including deadline changes, have one owner.
			_ = c.SetReadDeadline(time.Time{})
		}
		select {
		case incoming <- result:
		case <-ctx.Done():
			return
		}
		if result.err != nil {
			return
		}
	}
}

func toolLoop(ctx context.Context, runTool func(context.Context, string, string) (string, error), jobs <-chan toolJob, results chan<- toolResult) {
	for {
		var job toolJob
		select {
		case <-ctx.Done():
			return
		case job = <-jobs:
		}
		if job.ctx.Err() != nil {
			continue
		}
		callCtx, cancel := context.WithTimeout(job.ctx, 5*time.Second)
		output, err := runTool(callCtx, job.name, job.arguments)
		timedOut := callCtx.Err() != nil
		cancel()
		if job.ctx.Err() != nil {
			continue
		}
		if err != nil || timedOut {
			output = `{"error":"tool unavailable"}`
		}
		if len(output) > maxToolResultBytes {
			output = `{"error":"tool result too large"}`
		}
		select {
		case results <- toolResult{job.responseID, job.callID, output}:
		case <-ctx.Done():
			return
		case <-job.ctx.Done():
		}
	}
}

type sessionState struct {
	ctx                                              context.Context
	options                                          Options
	allowed                                          map[string]bool
	emit                                             func(Event) error
	audio                                            func([]byte) error
	commands                                         chan<- command
	jobs                                             chan<- toolJob
	responses                                        map[string]*responseState
	seenCalls                                        map[string]bool
	active, audioResponse, audioItem                 string
	audioIndex, audioBytes                           int
	assistantItem, assistantText, userItem, userText string
	resultBytes                                      int
	pendingAudio                                     []audioPacket
	pendingBytes                                     int
}

type audioPacket struct {
	responseID, itemID string
	data               []byte
}

// drainAudio never waits for the hardware queue. Keeping callbacks in the
// select loop lets an interruption clear every unsent sample before flushing
// playback, without an in-flight callback requeueing stale audio afterwards.
func (s *sessionState) drainAudio() error {
	for n := 0; len(s.pendingAudio) > 0 && n < 5; n++ {
		packet := s.pendingAudio[0]
		if s.audioItem != packet.itemID {
			if s.audioItem != "" && s.options.PlaybackEmpty != nil && !s.options.PlaybackEmpty() {
				return nil
			}
			s.audioItem, s.audioResponse, s.audioIndex, s.audioBytes = packet.itemID, packet.responseID, 0, 0
			if err := s.emitEvent(Event{Type: "output_start", Phase: "speaking", ItemID: packet.itemID}); err != nil {
				return err
			}
		}
		if err := s.audio(packet.data); errors.Is(err, ErrAudioWouldBlock) {
			return nil
		} else if err != nil {
			return errors.New("realtime audio delivery failed")
		}
		s.audioBytes += len(packet.data)
		s.pendingBytes -= len(packet.data)
		s.pendingAudio[0] = audioPacket{}
		s.pendingAudio = s.pendingAudio[1:]
	}
	if len(s.pendingAudio) == 0 {
		if r := s.responses[s.active]; r != nil && r.followUp && !r.interrupted {
			r.followUp = false
			return s.emitEvent(Event{Type: "phase", Phase: "listening", FollowUp: true})
		}
	}
	return nil
}

func (s *sessionState) emitEvent(e Event) error {
	if s.emit(e) != nil {
		return errors.New("realtime event delivery failed")
	}
	return nil
}

func (s *sessionState) send(payload any, valid context.Context) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.commands <- command{payload, valid}:
		return nil
	default:
		return errors.New("realtime control queue overflow")
	}
}

func (s *sessionState) handle(e wireEvent) error {
	switch e.Type {
	case "input_audio_buffer.speech_started":
		played := 0
		if s.audioItem != "" && s.options.PlayedAudioMS != nil {
			played = max(0, s.options.PlayedAudioMS())
		}
		for _, response := range s.responses {
			response.interrupted = true
			response.cancel()
		}
		unplayed := make(map[string]bool)
		pendingPlaying := false
		for _, packet := range s.pendingAudio {
			if packet.itemID != s.audioItem {
				unplayed[packet.itemID] = true
			} else {
				pendingPlaying = true
			}
			clear(packet.data)
		}
		s.pendingAudio, s.pendingBytes = nil, 0
		r := s.responses[s.audioResponse]
		fullyHeard := !pendingPlaying && s.audioBytes > 0 && r != nil && r.done && s.options.PlaybackEmpty != nil && s.options.PlaybackEmpty()
		// The playback callback flushes synchronously. Snapshot actual played
		// time first; bytes received or queued cannot measure what was heard.
		if err := s.emitEvent(Event{Type: "interrupt", Phase: "listening"}); err != nil {
			return err
		}
		if s.audioItem != "" {
			if !fullyHeard {
				played = min(played, s.audioBytes/48)
				if err := s.send(map[string]any{"type": "conversation.item.truncate", "item_id": s.audioItem, "content_index": s.audioIndex, "audio_end_ms": played}, s.ctx); err != nil {
					return err
				}
			}
			s.audioItem, s.audioResponse, s.audioBytes = "", "", 0
		}
		for id := range unplayed {
			if err := s.send(map[string]any{"type": "conversation.item.truncate", "item_id": id, "content_index": 0, "audio_end_ms": 0}, s.ctx); err != nil {
				return err
			}
		}
	case "input_audio_buffer.speech_stopped":
		return s.emitEvent(Event{Type: "phase", Phase: "thinking"})
	case "response.created":
		id := e.Response.ID
		if !identifier(id, 256) {
			return errors.New("invalid realtime response identifier")
		}
		if s.responses[id] != nil {
			return nil
		}
		if len(s.responses) >= 64 {
			return errors.New("realtime session response limit reached")
		}
		if old := s.responses[s.active]; old != nil && old.pending > 0 {
			old.interrupted = true
			old.cancel()
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.responses[id] = &responseState{ctx: ctx, cancel: cancel}
		s.active = id
		return s.emitEvent(Event{Type: "phase", Phase: "thinking"})
	case "response.output_audio.delta":
		r := s.responses[e.ResponseID]
		if r == nil || r.interrupted || r.done || s.active != e.ResponseID {
			return nil
		}
		if !identifier(e.ItemID, 256) || e.ContentIndex != 0 {
			return errors.New("invalid realtime audio item")
		}
		data, err := base64.StdEncoding.DecodeString(e.Delta)
		if err != nil || len(data) == 0 || len(data)%2 != 0 || len(data) > 256<<10 {
			return errors.New("invalid realtime PCM packet")
		}
		if len(data) > maxPendingAudioBytes-s.pendingBytes || (len(data)+959)/960 > maxPendingAudioPackets-len(s.pendingAudio) {
			return errors.New("realtime pending audio queue overflow")
		}
		s.pendingBytes += len(data)
		for len(data) > 0 {
			n := min(len(data), 24000*2*20/1000)
			s.pendingAudio = append(s.pendingAudio, audioPacket{e.ResponseID, e.ItemID, data[:n]})
			data = data[n:]
		}
	case "response.output_audio_transcript.delta", "response.output_audio_transcript.done":
		r := s.responses[e.ResponseID]
		if r == nil || r.interrupted || r.done || s.active != e.ResponseID {
			return nil
		}
		if !identifier(e.ItemID, 256) {
			return errors.New("invalid realtime transcript item")
		}
		if s.assistantItem != e.ItemID {
			s.assistantItem, s.assistantText = e.ItemID, ""
		}
		if e.Type == "response.output_audio_transcript.done" {
			s.assistantText = boundedText(e.Transcript)
		} else {
			s.assistantText = boundedText(s.assistantText + e.Delta)
		}
		return s.emitEvent(Event{Type: "transcript", Role: "assistant", Content: s.assistantText, ItemID: e.ItemID})
	case "conversation.item.input_audio_transcription.delta", "conversation.item.input_audio_transcription.completed":
		if s.options.Language == "" {
			return nil
		}
		if !identifier(e.ItemID, 256) {
			return errors.New("invalid realtime transcription item")
		}
		if s.userItem != e.ItemID {
			s.userItem, s.userText = e.ItemID, ""
		}
		if e.Type == "conversation.item.input_audio_transcription.completed" {
			s.userText = boundedText(e.Transcript)
		} else {
			s.userText = boundedText(s.userText + e.Delta)
		}
		return s.emitEvent(Event{Type: "transcript", Role: "user", Content: s.userText, ItemID: e.ItemID})
	case "conversation.item.input_audio_transcription.failed":
		// Transcription is for display only; audio conversation remains usable.
	case "response.done":
		r := s.responses[e.Response.ID]
		if r == nil || r.done {
			return nil
		}
		r.done = true
		if r.interrupted || s.active != e.Response.ID {
			return nil
		}
		if e.Response.Status == "cancelled" {
			r.cancel()
			r.followUp = true
			return nil
		}
		if e.Response.Status != "completed" {
			return errors.New("realtime response failed")
		}
		for _, item := range e.Response.Output {
			if item.Type != "function_call" {
				continue
			}
			if !s.allowed[item.Name] || !identifier(item.CallID, 256) || len(item.Arguments) > 16384 {
				return errors.New("realtime tool request rejected")
			}
			var arguments map[string]any
			if json.Unmarshal([]byte(item.Arguments), &arguments) != nil || arguments == nil {
				return errors.New("invalid realtime tool arguments")
			}
			if s.seenCalls[item.CallID] {
				continue
			}
			if len(s.seenCalls) >= maxToolCalls || r.calls >= 4 {
				return errors.New("realtime tool call limit reached")
			}
			s.seenCalls[item.CallID] = true
			r.pending++
			r.calls++
			select {
			case s.jobs <- toolJob{r.ctx, e.Response.ID, item.CallID, item.Name, item.Arguments}:
			default:
				return errors.New("realtime tool queue overflow")
			}
		}
		if r.calls == 0 {
			r.followUp = true
		}
	}
	return nil
}

func (s *sessionState) toolDone(result toolResult) error {
	r := s.responses[result.responseID]
	if r == nil || r.interrupted || s.active != result.responseID || r.pending == 0 {
		return nil
	}
	s.resultBytes += len(result.output)
	if s.resultBytes > maxSessionToolResultBytes {
		return errors.New("realtime tool result limit reached")
	}
	if err := s.send(map[string]any{"type": "conversation.item.create", "item": map[string]string{"type": "function_call_output", "call_id": result.callID, "output": result.output}}, r.ctx); err != nil {
		return err
	}
	r.pending--
	if r.pending == 0 && !r.continued {
		r.continued = true
		return s.send(map[string]string{"type": "response.create"}, r.ctx)
	}
	return nil
}
