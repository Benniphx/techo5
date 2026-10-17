package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/HuskerMinion/techo5/echod/internal/lib/mcp"
	"github.com/HuskerMinion/techo5/echod/internal/lib/realtime"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// Realtime voice: a slot chosen for it (config.WakeWord.Realtime) is answered by a speech-to-speech model
// at OpenAI, xAI or Google (lib/realtime), the voice going up as it is heard and the answer coming back
// as speech while it is made. It is a backend like the others, so everything that stops or holds a turn
// (the microphones taken, a call, a ring, the stop word, another wake) stops it, and its speech plays
// through the reply, under the same claim on the speaker, so whatever outranks a reply cuts it off.
//
// The model may call the device's own tools and, with a token for it, Home Assistant's (its MCP server),
// every call under the same lockout as the assistant's own (feature/assistant's TurnGate): once a turn
// has read outside words, nothing that changes anything runs in it. A Home Assistant tool changes
// something unless it is known to only look things up.
//
// Every session is held to a turn's length (realtimeTurn) and the day's minutes (config.Realtime), which
// the provider bills by.

// ToolGate is one turn's lockout, as feature/assistant makes it (SetRealtime).
type ToolGate interface {
	Tools() []llm.Tool
	Run(call llm.ToolCall) string
	Allow(readOnly bool) (ok bool, refused string)
	Saw(outside bool)
	EndRound()
}

var (
	newGate      func() ToolGate
	instructions = func() string { return "You are a voice assistant on a smart speaker. Answer briefly." }
)

// SetRealtime is feature/assistant handing realtime voice its lockout and its instructions, as SetThink
// hands the direct pipeline its thinking: it imports this package, so this one cannot import it.
func SetRealtime(gate func() ToolGate, instr func() string) {
	newGate, instructions = gate, instr
}

const (
	// realtimeTurn is the longest one turn's session may run, whatever happens in it.
	realtimeTurn = 5 * time.Minute
	// realtimeIdle is how long a session is kept open after a turn for the next one (a follow-up).
	realtimeIdle = 60 * time.Second
	// realtimePreroll is the most speech kept while the session opens: what is said meanwhile is sent once
	// it is open.
	realtimePreroll = 16000 * 15
	// mcpWithin is how long Home Assistant has to answer a tool call.
	mcpWithin = 15 * time.Second
)

// readOnlyTools are Home Assistant's tools known to only look things up.
var readOnlyTools = map[string]bool{"GetLiveContext": true, "GetDateTime": true}

// outsideWords is part of a tool's name that says it brings outside words into a turn.
var outsideWords = []string{"calendar", "todo", "search", "web", "news", "feed", "mail", "message"}

type rtBackend struct {
	post func(event)

	// dial opens a session; mcpFor is Home Assistant's tools, or nil; both replaced in tests.
	dial   func(ctx context.Context, c realtime.Config) (rtSession, error)
	mcpFor func(token string) mcpClient
	now    func() time.Time

	mu      sync.Mutex
	sess    rtSession
	sessAt  time.Time // when the session opened, for the day's count
	gen     int       // which session's events are this turn's
	slot    int
	ready   bool
	preroll []int16
	ended   bool // the speaker finished before the session was open
	gate    ToolGate
	tools   map[string]mcp.Tool
	mcp     mcpClient
	idle    *time.Timer
	cap     *time.Timer
	saying  strings.Builder
	spoke   bool // audio of this answer has gone to the reply
}

type rtSession interface {
	Audio([]int16) error
	EndOfSpeech() error
	ToolResult(callID, name, output string) error
	Events() <-chan realtime.Event
	Close() error
}

type mcpClient interface {
	Start(ctx context.Context) error
	Tools(ctx context.Context) ([]mcp.Tool, error)
	Call(ctx context.Context, name string, args map[string]any) (string, bool, error)
}

func newRealtime(post func(event)) *rtBackend {
	return &rtBackend{
		post: post,
		dial: func(ctx context.Context, c realtime.Config) (rtSession, error) { return realtime.Dial(ctx, c) },
		mcpFor: func(token string) mcpClient {
			addr := hass.Get().Address()
			if token == "" || addr == "" {
				return nil
			}
			return mcp.New(addr+"/api/mcp", token)
		},
		now: time.Now,
	}
}

func (r *rtBackend) Name() string { return "realtime" }

// Ready is whether realtime voice is set up, and has minutes left today.
func (r *rtBackend) Ready() bool {
	rc := config.Get().Realtime
	return rc.Ready() && rc.SecondsLeft(r.now()) > 0
}

// Start opens a turn: on the session kept from the last turn, or a new one, opened meanwhile, with what
// is said until it is open kept to send then.
func (r *rtBackend) Start(string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idle != nil {
		r.idle.Stop()
	}
	if newGate != nil {
		r.gate = newGate()
	}
	r.preroll, r.ended, r.spoke = r.preroll[:0], false, false
	r.saying.Reset()
	limit := min(realtimeTurn, time.Duration(config.Get().Realtime.SecondsLeft(r.now()))*time.Second)
	if r.sess != nil && r.ready {
		r.armCap(r.gen, limit)
		return nil // a follow-up: the same session, which remembers what was said
	}
	r.closeLocked()
	r.gen++
	gen := r.gen
	r.armCap(gen, limit)
	safe.Go("realtime open", func() { r.open(gen) })
	return nil
}

// armCap ends session gen's turn at d, with r.mu held.
func (r *rtBackend) armCap(gen int, d time.Duration) {
	if r.cap != nil {
		r.cap.Stop()
	}
	r.cap = time.AfterFunc(d, func() {
		r.mu.Lock()
		stale := r.gen != gen
		r.mu.Unlock()
		if stale {
			return
		}
		slog.Warn("realtime: the turn ran past its time, or the day's minutes", "limit", d.Round(time.Second))
		_ = r.Stop()
		r.post(event{kind: evError, code: "realtime-limit", msg: "realtime voice reached its time limit"})
	})
}

// open dials the session for turn gen, gathering the tools first.
func (r *rtBackend) open(gen int) {
	rc := config.Get().Realtime
	ctx := context.Background()
	r.mu.Lock()
	gate := r.gate
	r.mu.Unlock()

	var fns []realtime.Function
	if gate != nil {
		for _, t := range gate.Tools() {
			fns = append(fns, realtime.Function{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
		}
	}
	tools := map[string]mcp.Tool{}
	var mc mcpClient
	if c := r.mcpFor(rc.MCPToken); c != nil {
		tctx, cancel := context.WithTimeout(ctx, mcpWithin)
		ts, err := func() ([]mcp.Tool, error) {
			if err := c.Start(tctx); err != nil {
				return nil, err
			}
			return c.Tools(tctx)
		}()
		cancel()
		if err != nil {
			slog.Warn("realtime: Home Assistant's tools could not be had; the device's own only", "err", err)
		} else {
			mc = c
			for _, t := range ts {
				if _, clash := tools[t.Name]; clash {
					continue
				}
				tools[t.Name] = t
				fns = append(fns, realtime.Function{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
			}
		}
	}

	sess, err := r.dial(ctx, realtime.Config{
		Provider: rc.Provider, Key: rc.Key, Endpoint: rc.Endpoint, Model: rc.Model, Voice: rc.Voice,
		Instructions: instructions(), Functions: fns,
	})
	r.mu.Lock()
	if r.gen != gen {
		r.mu.Unlock()
		if sess != nil {
			_ = sess.Close()
		}
		return
	}
	if err != nil {
		r.mu.Unlock()
		slog.Warn("realtime: the session could not be opened", "provider", rc.Provider, "err", err)
		r.post(event{kind: evError, code: "realtime-failed", msg: err.Error()})
		return
	}
	r.sess, r.sessAt, r.ready, r.tools, r.mcp = sess, r.now(), true, tools, mc
	pre, ended := r.preroll, r.ended
	r.preroll = nil
	r.mu.Unlock()
	slog.Info("realtime: session open", "provider", rc.Provider, "functions", len(fns))

	safe.Go("realtime events", func() { r.pump(gen, sess) })
	if len(pre) > 0 {
		_ = sess.Audio(pre)
	}
	if ended {
		_ = sess.EndOfSpeech()
	}
}

// Audio sends a frame of the microphone, or keeps it while the session opens.
func (r *rtBackend) Audio(frame []byte) error {
	pcm := make([]int16, len(frame)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(frame[2*i:]))
	}
	r.mu.Lock()
	sess, ready := r.sess, r.ready
	if !ready {
		if len(r.preroll)+len(pcm) <= realtimePreroll {
			r.preroll = append(r.preroll, pcm...)
		}
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return sess.Audio(pcm)
}

// End says the speaker has finished: the answer is asked for.
func (r *rtBackend) End() error {
	r.mu.Lock()
	sess, ready := r.sess, r.ready
	if !ready {
		r.ended = true
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return sess.EndOfSpeech()
}

// Stop ends the session at once, and what it was saying with it.
func (r *rtBackend) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gen++
	r.closeLocked()
	if r.cap != nil {
		r.cap.Stop()
	}
	return nil
}

// closeLocked closes the session and counts its minutes, with r.mu held.
func (r *rtBackend) closeLocked() {
	if r.idle != nil {
		r.idle.Stop()
	}
	if r.sess == nil {
		r.ready = false
		return
	}
	_ = r.sess.Close()
	secs := int(r.now().Sub(r.sessAt).Round(time.Second) / time.Second)
	if err := config.Set().Realtime().Used(max(secs, 1), r.now()); err != nil {
		slog.Warn("realtime: the day's minutes could not be counted", "err", err)
	}
	slog.Info("realtime: session closed", "seconds", secs)
	r.sess, r.ready, r.mcp, r.tools = nil, false, nil, nil
}

// pump turns the session's events into the conversation's, until the session closes or another takes
// its place.
func (r *rtBackend) pump(gen int, sess rtSession) {
	for e := range sess.Events() {
		r.mu.Lock()
		stale := r.gen != gen
		r.mu.Unlock()
		if stale {
			return
		}
		switch e.Kind {
		case realtime.Heard:
			slog.Info("realtime: heard", "text", e.Text)
			r.post(event{kind: evHeard, text: strings.TrimSpace(e.Text)})
		case realtime.Saying:
			r.mu.Lock()
			r.saying.WriteString(e.Text)
			r.mu.Unlock()
		case realtime.Said:
			r.mu.Lock()
			r.saying.Reset()
			r.saying.WriteString(e.Text)
			r.mu.Unlock()
		case realtime.Audio:
			pcm := media.ToVoiceRate(e.PCM, e.Rate)
			out := make([]byte, 2*len(pcm))
			for i, s := range pcm {
				binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
			}
			r.mu.Lock()
			r.spoke = true
			r.mu.Unlock()
			r.post(event{kind: evStreamAudio, audio: out})
		case realtime.Call:
			out := r.call(e.Name, e.Args)
			_ = sess.ToolResult(e.CallID, e.Name, out)
		case realtime.Done, realtime.Interrupt:
			r.finish()
		case realtime.Failed:
			slog.Warn("realtime: the session failed", "err", e.Err)
			_ = r.Stop()
			r.post(event{kind: evError, code: "realtime-failed", msg: errorText(e.Err)})
			return
		}
	}
}

// finish is an answer done: its words, its end, and the session kept a while for a follow-up.
func (r *rtBackend) finish() {
	r.mu.Lock()
	said := strings.TrimSpace(r.saying.String())
	r.saying.Reset()
	spoke := r.spoke
	r.spoke = false
	gen := r.gen
	if r.cap != nil {
		r.cap.Stop()
	}
	if r.idle != nil {
		r.idle.Stop()
	}
	r.idle = time.AfterFunc(realtimeIdle, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.gen == gen {
			r.gen++
			r.closeLocked()
		}
	})
	r.mu.Unlock()

	if said != "" {
		slog.Info("realtime: replying", "text", said)
		r.post(event{kind: evReplyText, text: said})
		if strings.HasSuffix(said, "?") {
			r.post(event{kind: evContinue})
		}
	}
	if spoke {
		r.post(event{kind: evStreamEnd})
	}
	r.post(event{kind: evRunEnd})
}

// call runs a function the model called, under the turn's lockout: the device's own through the gate,
// Home Assistant's through its MCP server once the gate allows it.
func (r *rtBackend) call(name, args string) string {
	r.mu.Lock()
	gate, mc := r.gate, r.mcp
	t, fromHA := r.tools[name]
	r.mu.Unlock()
	if gate == nil {
		return "error: no tools are available"
	}
	defer gate.EndRound() // the model asks for one call at a time: each is a round

	if !fromHA {
		var tc llm.ToolCall
		tc.Function.Name, tc.Function.Arguments = name, args
		out := gate.Run(tc)
		slog.Info("realtime: tool", "name", name, "args", args, "result", out)
		return out
	}
	readOnly := t.ReadOnly() || readOnlyTools[name]
	if ok, refused := gate.Allow(readOnly); !ok {
		slog.Warn("realtime: a Home Assistant tool refused after reading outside words", "name", name)
		return refused
	}
	var a map[string]any
	if strings.TrimSpace(args) != "" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "error: the arguments were not JSON"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpWithin)
	defer cancel()
	out, failed, err := mc.Call(ctx, name, a)
	gate.Saw(bringsOutsideWords(name))
	slog.Info("realtime: Home Assistant tool", "name", name, "args", args, "result", out, "failed", failed)
	if err != nil {
		return "error: " + err.Error()
	}
	if failed {
		return "error: " + out
	}
	return out
}

func bringsOutsideWords(name string) bool {
	n := strings.ToLower(name)
	for _, w := range outsideWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func errorText(err error) string {
	if err == nil {
		return "the session ended"
	}
	return err.Error()
}
