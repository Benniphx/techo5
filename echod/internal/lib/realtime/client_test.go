package realtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/gorilla/websocket"
)

func testOptions() Options {
	return Options{Model: "gpt-realtime", Voice: "marin", Key: "offline-test-key"}
}

func socketServer(t *testing.T, serve func(*websocket.Conn, *http.Request)) (string, *websocket.Dialer) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		serve(c, r)
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	t.Cleanup(s.Close)
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	d := trustedDialer()
	d.Proxy = nil
	d.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	return "wss" + strings.TrimPrefix(s.URL, "https") + "/v1/realtime?model=gpt-realtime", d
}

func readJSON(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var value map[string]any
	if err := c.ReadJSON(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func sendJSON(t *testing.T, c *websocket.Conn, value any) {
	t.Helper()
	if err := c.WriteJSON(value); err != nil {
		t.Fatal(err)
	}
}

func negotiate(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	request := readJSON(t, c)
	if request["type"] != "session.update" {
		t.Fatalf("initial event must negotiate, got %v", request["type"])
	}
	sendJSON(t, c, map[string]string{"type": "session.created"})
	// Fixed GA acknowledgement, independent of the client's payload.
	sendJSON(t, c, map[string]any{"type": "session.updated", "session": map[string]any{
		"type": "realtime", "output_modalities": []string{"audio"}, "audio": map[string]any{
			"input":  map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "turn_detection": map[string]any{"type": "server_vad", "create_response": true, "interrupt_response": true}},
			"output": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}},
		},
	}})
	return request["session"].(map[string]any)
}

func noEvent(Event) error  { return nil }
func noAudio([]byte) error { return nil }

func TestTLSWebsocketNegotiatesGASessionAndStreamsResampledWakeWithoutGreeting(t *testing.T) {
	q := NewQueue()
	if err := q.Push(tone(16000, 1000)[:1600]); err != nil {
		t.Fatal(err)
	}
	target, dialer := socketServer(t, func(c *websocket.Conn, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer offline-test-key" || r.URL.Query().Get("model") != "gpt-realtime" {
			t.Error("provider credential or model not scoped to handshake")
		}
		session := negotiate(t, c)
		if session["type"] != "realtime" || session["model"] != "gpt-realtime" || session["modalities"] != nil {
			t.Error("legacy beta session configuration")
		}
		input := session["audio"].(map[string]any)["input"].(map[string]any)
		if input["transcription"] != nil {
			t.Error("display transcription should be opt-in")
		}
		vad := input["turn_detection"].(map[string]any)
		if vad["type"] != "server_vad" || vad["create_response"] != true || vad["interrupt_response"] != true {
			t.Error("full-duplex server VAD not enabled")
		}
		message := readJSON(t, c)
		if message["type"] != "input_audio_buffer.append" {
			t.Fatalf("wake must send captured speech, not create a greeting: %v", message["type"])
		}
		data, err := base64.StdEncoding.DecodeString(message["audio"].(string))
		if err != nil || len(data) != 2400*2 {
			t.Errorf("16k hardware input was not resampled to 24k: %d bytes, %v", len(data), err)
		}
		sendJSON(t, c, map[string]string{"type": "error", "message": "private-server-error"})
	})
	var ready atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err := run(ctx, testOptions(), q, func(e Event) error {
		if e.Type == "ready" {
			ready.Add(1)
		}
		return nil
	}, noAudio, target, dialer)
	if err == nil || strings.Contains(err.Error(), "private-server-error") || strings.Contains(err.Error(), "offline-test-key") {
		t.Fatalf("provider error not sanitized: %v", err)
	}
	if ready.Load() != 1 {
		t.Fatalf("expected exactly one listening acknowledgement, got %d", ready.Load())
	}
}

func TestBargeInFlushesBeforeTruncateAndRejectsLateResponseAudio(t *testing.T) {
	q := NewQueue()
	var played atomic.Int32
	var interrupts, packets, starts atomic.Int32
	var transcripts []string
	o := testOptions()
	o.PlayedAudioMS = func() int { return int(played.Load()) }
	target, dialer := socketServer(t, func(c *websocket.Conn, _ *http.Request) {
		negotiate(t, c)
		sendJSON(t, c, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
		for _, delta := range []string{"Hallo ", "du"} {
			sendJSON(t, c, map[string]string{"type": "response.output_audio_transcript.delta", "response_id": "r1", "item_id": "item1", "delta": delta})
		}
		sendJSON(t, c, audioDelta("r1", "item1", 4800))
		sendJSON(t, c, map[string]any{"type": "response.done", "response": map[string]string{"id": "r1", "status": "completed"}})
		// The item remains truncatable even after response.done while the local
		// hardware is still playing; received bytes are deliberately > played.
		sendJSON(t, c, map[string]string{"type": "input_audio_buffer.speech_started"})
		sendJSON(t, c, audioDelta("r1", "item1", 960))
		sendJSON(t, c, map[string]string{"type": "response.output_audio_transcript.delta", "response_id": "r1", "item_id": "item1", "delta": " stale"})
		truncate := readJSON(t, c)
		if truncate["type"] != "conversation.item.truncate" || truncate["item_id"] != "item1" || truncate["audio_end_ms"] != float64(37) {
			t.Errorf("truncate must describe actually heard audio: %v", truncate)
		}
		if interrupts.Load() != 1 || played.Load() != 0 {
			t.Error("truncate preceded the synchronous playback flush")
		}
		sendJSON(t, c, map[string]any{"type": "response.created", "response": map[string]string{"id": "r2"}})
		sendJSON(t, c, audioDelta("r2", "item2", 960))
		sendJSON(t, c, map[string]any{"type": "response.done", "response": map[string]string{"id": "r2", "status": "completed"}})
		sendJSON(t, c, map[string]string{"type": "error"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err := run(ctx, o, q, func(e Event) error {
		switch e.Type {
		case "output_start":
			starts.Add(1)
			played.Store(0)
		case "interrupt":
			interrupts.Add(1)
			played.Store(0)
		case "transcript":
			transcripts = append(transcripts, e.Text())
		}
		return nil
	}, func(data []byte) error {
		if len(data) > 960 {
			t.Error("provider packet not split into 20ms hardware writes")
		}
		packets.Add(1)
		played.Store(37)
		return nil
	}, target, dialer)
	if err == nil {
		t.Fatal("expected fake server termination")
	}
	if packets.Load() != 6 || starts.Load() != 2 {
		t.Fatalf("stale audio replayed or new response discarded: %d packets, %d starts", packets.Load(), starts.Load())
	}
	if !slices.Equal(transcripts, []string{"Hallo ", "Hallo du"}) {
		t.Fatalf("transcripts must be cumulative and reject stale generations: %v", transcripts)
	}
}

func audioDelta(responseID, itemID string, bytes int) map[string]any {
	return map[string]any{"type": "response.output_audio.delta", "response_id": responseID, "item_id": itemID, "content_index": 0, "delta": base64.StdEncoding.EncodeToString(make([]byte, bytes))}
}

func TestUntrustedTLSCannotInheritGlobalSkipVerify(t *testing.T) {
	target, _ := socketServer(t, func(*websocket.Conn, *http.Request) { t.Error("untrusted TLS server received credential") })
	old := http.DefaultTransport
	http.DefaultTransport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer func() { http.DefaultTransport = old }()
	d := trustedDialer()
	d.Proxy = nil
	if d.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("provider inherited a TLS bypass")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := run(ctx, testOptions(), NewQueue(), noEvent, noAudio, target, d)
	if err == nil || strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "offline-test-key") {
		t.Fatalf("untrusted provider TLS accepted or error leaked details: %v", err)
	}
}

func TestCancelDuringHandshakeAndNegotiationIsPrompt(t *testing.T) {
	t.Run("handshake", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		accepted := make(chan net.Conn, 1)
		go func() {
			c, err := listener.Accept()
			if err == nil {
				accepted <- c
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			result <- run(ctx, testOptions(), NewQueue(), noEvent, noAudio, "wss://"+listener.Addr().String(), trustedDialer())
		}()
		c := <-accepted
		defer c.Close()
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("handshake lost cancellation: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("TLS handshake ignored cancellation")
		}
	})
	t.Run("negotiation", func(t *testing.T) {
		seen := make(chan struct{})
		target, d := socketServer(t, func(c *websocket.Conn, _ *http.Request) { readJSON(t, c); close(seen); c.ReadMessage() })
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- run(ctx, testOptions(), NewQueue(), noEvent, noAudio, target, d) }()
		<-seen
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("negotiation lost cancellation: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("socket workers did not join after cancellation")
		}
	})
}

func TestWebsocketReadLimitAndInvalidAudioFailClosed(t *testing.T) {
	for name, packet := range map[string]any{
		"oversized":      map[string]string{"type": "unknown", "data": strings.Repeat("x", maxEventBytes)},
		"odd-pcm":        audioDelta("r1", "item1", 3),
		"invalid-base64": map[string]string{"type": "response.output_audio.delta", "response_id": "r1", "item_id": "item1", "delta": "!"},
	} {
		t.Run(name, func(t *testing.T) {
			target, d := socketServer(t, func(c *websocket.Conn, _ *http.Request) {
				negotiate(t, c)
				sendJSON(t, c, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
				// The client may close while an oversized frame is still written.
				c.WriteJSON(packet)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if err := run(ctx, testOptions(), NewQueue(), noEvent, func([]byte) error { t.Error("invalid PCM reached hardware"); return nil }, target, d); err == nil {
				t.Fatal("malformed server frame accepted")
			}
		})
	}
}

func testState(t *testing.T, o Options) (*sessionState, chan command, chan toolJob, *[]Event) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_, allowed, err := o.sessionUpdate()
	if err != nil {
		t.Fatal(err)
	}
	commands, jobs := make(chan command, 32), make(chan toolJob, 4)
	events := new([]Event)
	s := &sessionState{ctx: ctx, options: o, allowed: allowed, emit: func(e Event) error { *events = append(*events, e); return nil }, audio: noAudio, commands: commands, jobs: jobs, responses: make(map[string]*responseState), seenCalls: make(map[string]bool)}
	return s, commands, jobs, events
}

func handleMap(t *testing.T, s *sessionState, data any) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var e wireEvent
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if err := s.handle(e); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackBackpressureDelaysFollowUpButNeverInterrupt(t *testing.T) {
	s, commands, _, events := testState(t, testOptions())
	blocked, delivered := true, 0
	s.audio = func([]byte) error {
		if blocked {
			return ErrAudioWouldBlock
		}
		delivered++
		return nil
	}
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	handleMap(t, s, audioDelta("r1", "item1", 960*3))
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	handleMap(t, s, map[string]any{"type": "response.done", "response": map[string]string{"id": "r1", "status": "completed"}})
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	for _, e := range *events {
		if e.FollowUp {
			t.Fatal("follow-up started before unsent PCM drained")
		}
	}
	blocked = false
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	if delivered != 3 || !(*events)[len(*events)-1].FollowUp {
		t.Fatal("PCM did not resume with exactly one follow-up")
	}
	// New blocked output can still be interrupted without any callback waiting
	// for playback room, and it truncates already-completed item1 correctly.
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r2"}})
	blocked = true
	handleMap(t, s, audioDelta("r2", "item2", 960*10))
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	handleMap(t, s, map[string]string{"type": "input_audio_buffer.speech_started"})
	if s.pendingBytes != 0 || len(s.pendingAudio) != 0 {
		t.Fatal("interrupted output retained unsent PCM")
	}
	truncate := (<-commands).payload.(map[string]any)
	if truncate["audio_end_ms"] != 0 {
		t.Fatal("received/queued output falsely counted as heard audio")
	}
	blocked = false
	if err := s.drainAudio(); err != nil || delivered != 3 {
		t.Fatal("late pending PCM played after interruption")
	}
}

func TestPendingPCMQueueIsBounded(t *testing.T) {
	s, _, _, _ := testState(t, testOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	data := wireEvent{Type: "response.output_audio.delta", ResponseID: "r1", ItemID: "item1", Delta: base64.StdEncoding.EncodeToString(make([]byte, 24000*2))}
	for range 60 {
		if err := s.handle(data); err != nil {
			t.Fatal(err)
		}
	}
	if s.pendingBytes != maxPendingAudioBytes {
		t.Fatal("PCM queue did not retain exact bound")
	}
	if err := s.handle(data); err == nil {
		t.Fatal("pending PCM grew past its bound")
	}
	s, _, _, _ = testState(t, testOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	data.Delta = base64.StdEncoding.EncodeToString([]byte{0, 0})
	for range maxPendingAudioPackets {
		if err := s.handle(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.handle(data); err == nil {
		t.Fatal("tiny PCM deltas bypassed the packet metadata bound")
	}
}

func TestNextAudioItemWaitsForLocalDrainAndUnplayedItemsTruncateAtZero(t *testing.T) {
	o := testOptions()
	empty := false
	o.PlaybackEmpty = func() bool { return empty }
	o.PlayedAudioMS = func() int { return 10 }
	s, commands, _, events := testState(t, o)
	var playedItems []string
	s.audio = func([]byte) error { playedItems = append(playedItems, s.audioItem); return nil }
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	handleMap(t, s, audioDelta("r1", "item1", 960))
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	handleMap(t, s, map[string]any{"type": "response.done", "response": map[string]string{"id": "r1", "status": "completed"}})
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r2"}})
	handleMap(t, s, audioDelta("r2", "item2", 960))
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	if s.audioItem != "item1" || !slices.Equal(playedItems, []string{"item1"}) {
		t.Fatal("next item replaced playback accounting before local drain")
	}
	starts := 0
	for _, e := range *events {
		if e.Type == "output_start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatal("next output_start preceded the previous hardware tail")
	}
	handleMap(t, s, map[string]string{"type": "input_audio_buffer.speech_started"})
	first := (<-commands).payload.(map[string]any)
	second := (<-commands).payload.(map[string]any)
	if first["item_id"] != "item1" || first["audio_end_ms"] != 10 || second["item_id"] != "item2" || second["audio_end_ms"] != 0 {
		t.Fatalf("heard and wholly unplayed items were not truncated separately: %v %v", first, second)
	}
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r3"}})
	handleMap(t, s, audioDelta("r3", "item3", 960))
	empty = true
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(playedItems, []string{"item1", "item3"}) {
		t.Fatal("interrupted unplayed item was replayed during handoff")
	}
}

func TestFullyPlayedCompletedAudioNeedsNoTruncate(t *testing.T) {
	o := testOptions()
	o.PlaybackEmpty = func() bool { return true }
	s, commands, _, _ := testState(t, o)
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	handleMap(t, s, audioDelta("r1", "item1", 960))
	if err := s.drainAudio(); err != nil {
		t.Fatal(err)
	}
	handleMap(t, s, map[string]any{"type": "response.done", "response": map[string]string{"id": "r1", "status": "completed"}})
	handleMap(t, s, map[string]string{"type": "input_audio_buffer.speech_started"})
	if len(commands) != 0 {
		t.Fatal("fully heard completed item was needlessly truncated")
	}
}

func toolsOptions() Options {
	o := testOptions()
	o.Tools = []llm.Tool{{Name: "get_status", Description: "Read status", Parameters: map[string]any{"type": "object"}}}
	o.RunTool = func(context.Context, string, string) (string, error) { return "{}", nil }
	return o
}

func TestCompletedToolCallsAreAllowlistedOnceAndContinueAfterAllResults(t *testing.T) {
	s, commands, jobs, events := testState(t, toolsOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	done := wireEvent{Type: "response.done", Response: wireResponse{ID: "r1", Status: "completed", Output: []wireItem{
		{Type: "function_call", Name: "get_status", CallID: "call1", Arguments: "{}"},
		{Type: "function_call", Name: "get_status", CallID: "call2", Arguments: "{}"},
	}}}
	if err := s.handle(done); err != nil {
		t.Fatal(err)
	}
	if err := s.handle(done); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatal("duplicate completion reran tools")
	}
	for _, e := range *events {
		if e.FollowUp {
			t.Fatal("tool turn prematurely opened follow-up window")
		}
	}
	if err := s.toolDone(toolResult{"r1", "call1", `{"status":"ok"}`}); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatal("model continued before remaining tool returned")
	}
	if err := s.toolDone(toolResult{"r1", "call2", `{}`}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"conversation.item.create", "conversation.item.create", "response.create"} {
		cmd := <-commands
		var kind string
		switch value := cmd.payload.(type) {
		case map[string]any:
			kind = value["type"].(string)
		case map[string]string:
			kind = value["type"]
		}
		if kind != expected {
			t.Fatalf("tool result/continuation order incorrect: %q expected %q", kind, expected)
		}
	}
	if err := s.toolDone(toolResult{"r1", "call2", `{}`}); err != nil || len(commands) != 0 {
		t.Fatal("duplicate tool result recreated response")
	}
}

func TestInterruptionCancelsToolJobAndQueuedContinuation(t *testing.T) {
	s, commands, jobs, _ := testState(t, toolsOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	if err := s.handle(wireEvent{Type: "response.done", Response: wireResponse{ID: "r1", Status: "completed", Output: []wireItem{{Type: "function_call", Name: "get_status", CallID: "call1", Arguments: "{}"}}}}); err != nil {
		t.Fatal(err)
	}
	job := <-jobs
	if err := s.toolDone(toolResult{"r1", "call1", `{}`}); err != nil {
		t.Fatal(err)
	}
	handleMap(t, s, map[string]string{"type": "input_audio_buffer.speech_started"})
	if job.ctx.Err() == nil {
		t.Fatal("tool context survived interruption")
	}
	for len(commands) > 0 {
		if cmd := <-commands; cmd.valid.Err() == nil {
			t.Fatal("queued continuation survived interruption")
		}
	}
	if err := s.toolDone(toolResult{"r1", "call1", `{}`}); err != nil || len(commands) > 0 {
		t.Fatal("late tool result continued cancelled response")
	}
}

func TestToolWorkerRemainsCancellableAndSanitizesErrorsAndOversizedResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs, results := make(chan toolJob, 4), make(chan toolResult, 4)
	done, entered := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		toolLoop(ctx, func(ctx context.Context, name, _ string) (string, error) {
			switch name {
			case "fail":
				return "", errors.New("private-provider-key")
			case "huge":
				return strings.Repeat("x", maxToolResultBytes+1), nil
			default:
				close(entered)
				<-ctx.Done()
				return "", ctx.Err()
			}
		}, jobs, results)
	}()
	jobs <- toolJob{ctx: ctx, name: "fail"}
	jobs <- toolJob{ctx: ctx, name: "huge"}
	if got := (<-results).output; strings.Contains(got, "private-provider-key") || !strings.Contains(got, "unavailable") {
		t.Fatalf("tool error leaked: %s", got)
	}
	if got := (<-results).output; len(got) > 100 || !strings.Contains(got, "too large") {
		t.Fatal("oversized result escaped bound")
	}
	jobs <- toolJob{ctx: ctx, name: "wait"}
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active tool worker did not join cancellation")
	}
}

func TestSocketReaderInterruptsRunningToolAndJoinsIt(t *testing.T) {
	o := toolsOptions()
	entered, exited := make(chan struct{}), make(chan struct{})
	o.RunTool = func(ctx context.Context, _, _ string) (string, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return "", ctx.Err()
	}
	target, d := socketServer(t, func(c *websocket.Conn, _ *http.Request) {
		negotiate(t, c)
		sendJSON(t, c, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
		sendJSON(t, c, wireEvent{Type: "response.done", Response: wireResponse{ID: "r1", Status: "completed", Output: []wireItem{{Type: "function_call", Name: "get_status", CallID: "call1", Arguments: "{}"}}}})
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("tool worker did not start")
		}
		sendJSON(t, c, map[string]string{"type": "input_audio_buffer.speech_started"})
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Fatal("tool blocked VAD processing")
		}
		sendJSON(t, c, map[string]any{"type": "response.created", "response": map[string]string{"id": "r2"}})
		sendJSON(t, c, audioDelta("r2", "item2", 960))
		sendJSON(t, c, map[string]any{"type": "response.done", "response": map[string]string{"id": "r2", "status": "completed"}})
		sendJSON(t, c, map[string]string{"type": "error"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var interrupts, packets int
	err := run(ctx, o, NewQueue(), func(e Event) error {
		if e.Type == "interrupt" {
			interrupts++
		}
		return nil
	}, func([]byte) error { packets++; return nil }, target, d)
	if err == nil || interrupts != 1 || packets != 1 {
		t.Fatalf("cancelled tool prevented next audio turn: %v, interrupts %d packets %d", err, interrupts, packets)
	}
	select {
	case <-exited:
	default:
		t.Fatal("Run returned before tool joined")
	}
}

func TestUnknownToolsAndSessionToolLimitsFailClosed(t *testing.T) {
	s, _, jobs, _ := testState(t, toolsOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	if err := s.handle(wireEvent{Type: "response.done", Response: wireResponse{ID: "r1", Status: "completed", Output: []wireItem{{Type: "function_call", Name: "execute_shell", CallID: "call1", Arguments: "{}"}}}}); err == nil || len(jobs) != 0 {
		t.Fatal("unadvertised function reached tool executor")
	}
	s, _, _, _ = testState(t, toolsOptions())
	for range maxToolCalls {
		s.seenCalls[strings.Repeat("x", len(s.seenCalls)+1)] = true
	}
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	if err := s.handle(wireEvent{Type: "response.done", Response: wireResponse{ID: "r1", Status: "completed", Output: []wireItem{{Type: "function_call", Name: "get_status", CallID: "call33", Arguments: "{}"}}}}); err == nil {
		t.Fatal("session tool count cap bypassed")
	}
	s, _, _, _ = testState(t, toolsOptions())
	handleMap(t, s, map[string]any{"type": "response.created", "response": map[string]string{"id": "r1"}})
	s.responses["r1"].pending = 1
	s.resultBytes = maxSessionToolResultBytes
	if err := s.toolDone(toolResult{"r1", "call1", "{}"}); err == nil {
		t.Fatal("session tool result byte cap bypassed")
	}
}

func TestTranscriptsBoundUnicodeAndLanguageIsExplicit(t *testing.T) {
	o := testOptions()
	o.Language = "de"
	s, _, _, events := testState(t, o)
	handleMap(t, s, map[string]string{"type": "conversation.item.input_audio_transcription.completed", "item_id": "user1", "transcript": strings.Repeat("ä", 1300)})
	got := (*events)[0].Text()
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxTranscriptRunes {
		t.Fatal("transcript cap cut UTF-8 or allowed excessive text")
	}
	update, _, err := o.sessionUpdate()
	if err != nil {
		t.Fatal(err)
	}
	input := update["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
	if input["transcription"].(map[string]any)["language"] != "de" {
		t.Fatal("configured display language was ignored")
	}
}
