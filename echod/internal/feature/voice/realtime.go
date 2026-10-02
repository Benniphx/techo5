package voice

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/feedback"
	"github.com/HuskerMinion/techo5/echod/internal/feature/mute"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	rtapi "github.com/HuskerMinion/techo5/echod/internal/lib/realtime"
)

// realtime owns a whole multi-turn conversation beside the Assist turn machine.
// An active connection is never replaced or retried without a fresh local wake.
type realtime struct {
	mu             sync.Mutex
	root           context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	busy           atomic.Bool
	failedAttempts int
	nextAttempt    time.Time
}

func (r *realtime) backingOff() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Now().Before(r.nextAttempt)
}

func (r *realtime) attach(ctx context.Context) { r.mu.Lock(); defer r.mu.Unlock(); r.root = ctx }
func (r *realtime) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}
func (r *realtime) shutdown() {
	r.stop()
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (r *realtime) start() {
	r.mu.Lock()
	if r.root == nil || r.root.Err() != nil || r.busy.Load() || (ring.IsSounding() && !ring.Offered()) || phone.Get().Busy() || micTaken() {
		r.mu.Unlock()
		return
	}
	// Muted or unreadable privacy hardware is a closed gate, including connecting.
	cut, err := mute.Get().MutedStrict()
	if err != nil || cut {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(r.root, 120*time.Second)
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	r.busy.Store(true)
	r.mu.Unlock()
	RealtimeActive.Emit(true)
	Changed.Emit(State{Realtime: true, Phase: "connecting"})
	go func() {
		err := r.run(ctx)
		// Report only a generic reason; credential paths, backend text and private
		// transcripts never become the default daemon log.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.mu.Lock()
			r.failedAttempts = min(r.failedAttempts+1, 6)
			r.nextAttempt = time.Now().Add(min(time.Duration(1<<(r.failedAttempts-1))*time.Second, 30*time.Second))
			r.mu.Unlock()
			slog.Warn("realtime voice ended with an error", "reason", err)
			feedback.Failure()
			Changed.Emit(State{Realtime: true, Phase: "error", Reply: "Connection failed. Try a new wake after a short pause."})
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
		cancel()
		if err == nil {
			r.mu.Lock()
			r.failedAttempts = 0
			r.nextAttempt = time.Time{}
			r.mu.Unlock()
		}
		Changed.Emit(State{Realtime: true, Phase: "idle"})
		RealtimeActive.Emit(false)
		r.mu.Lock()
		r.cancel = nil
		r.busy.Store(false)
		close(done)
		r.mu.Unlock()
	}()
}

type realtimeAudio struct {
	mu         sync.Mutex
	pcm        []int16
	resampler  rtapi.Resampler
	p          *speaker.Player
	owner      *speaker.Claim
	framesSent int
	quietSince time.Time
}

const realtimeQueueSamples = speaker.Rate * speaker.Channels * 300 / 1000 // 300 ms local jitter; transport separately buffers provider bursts.
func (a *realtimeAudio) push(data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pcm)+len(data)*2 > realtimeQueueSamples {
		return rtapi.ErrAudioWouldBlock
	}
	a.quietSince = time.Time{}
	a.pcm = append(a.pcm, a.resampler.Run(data)...)
	return nil
}

// begin starts accounting for a new assistant item after draining the old one.
func (a *realtimeAudio) begin() { a.flush() }

// playedMS is a conservative lower bound: subtract both Player's queue and
// the driver's hardware tail. It never counts audio merely received from the API.
func (a *realtimeAudio) playedMS() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return playedMilliseconds(a.framesSent, a.p.Queued())
}
func playedMilliseconds(sent, queued int) int {
	tail := int(speaker.HardwareTail * time.Duration(speaker.Rate) / time.Second)
	return max(0, sent-queued-tail) * 1000 / speaker.Rate
}
func (a *realtimeAudio) flush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pcm = nil
	a.framesSent = 0
	a.quietSince = time.Time{}
	a.resampler.Reset()
	// A higher-priority claim owns the new queue after preemption.
	if a.owner == nil || !a.owner.Preempted() {
		a.p.Drain()
	}
}
func (a *realtimeAudio) empty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pcm) == 0 && a.p.Queued() == 0
}

// drained includes the codec tail when handing playback to the next API item.
func (a *realtimeAudio) drained() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pcm) != 0 || a.p.Queued() != 0 {
		a.quietSince = time.Time{}
		return false
	}
	if a.quietSince.IsZero() {
		a.quietSince = time.Now()
		return false
	}
	return time.Since(a.quietSince) >= speaker.HardwareTail
}
func (a *realtimeAudio) play(ctx context.Context) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		a.mu.Lock()
		if ctx.Err() != nil {
			a.mu.Unlock()
			return nil
		}
		// Queue no more than 20 ms in Player. Interrupt holds this same lock, so
		// no old sample can be queued after flush returns.
		for burst := 0; burst < 3 && a.p.Queued() < 960 && len(a.pcm) > 0; burst++ {
			n := min(960, len(a.pcm))
			if !a.p.TryPlay(a.pcm[:n]) {
				a.mu.Unlock()
				return errors.New("realtime playback unavailable")
			}
			a.framesSent += n / speaker.Channels
			a.pcm = a.pcm[n:]
		}
		a.mu.Unlock()
	}
}

func (r *realtime) run(parent context.Context) error {
	pilot, err := readRealtimeConfig()
	if err != nil {
		return err
	}
	options, err := pilot.options()
	if err != nil {
		return err
	}
	ctx, cancelCause := context.WithCancelCause(parent)
	cancel := func() { cancelCause(context.Canceled) }
	defer cancel()
	source := mic.Get()
	frames, unlisten := source.Listen("realtime")
	defer unlisten()
	q := rtapi.NewQueue()
	defer q.Clear()
	if err := q.Push(source.Recent(500 * time.Millisecond)); err != nil {
		return err
	}
	captureDone := make(chan struct{})
	go func() {
		defer close(captureDone)
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-frames:
				if !ok {
					cancelCause(errors.New("realtime microphones unavailable"))
					return
				}
				if err := q.Push(frame); err != nil {
					cancelCause(err)
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-captureDone }()

	// Preserve the loopback/AEC hardware path. Adaptation is phase-controlled for
	// full duplex. Freeze during near-end speech; allow learning while only the
	// assistant speaks, and freeze again as soon as server VAD sees a user.
	// The real-device spike must validate this policy before acceptance.
	source.SetAdapting(false)
	defer source.SetAdapting(true)
	sound := speaker.Sound()
	sound.Backgrounds().Duck("realtime", true)
	defer sound.Backgrounds().Duck("realtime", false)
	a := &realtimeAudio{p: speaker.Get()}
	held := sound.ClaimSpeech("realtime", func(playCtx context.Context, p *speaker.Player) error {
		// Another sound (alarm/call/announcement) taking this claim ends our session.
		joined, stop := context.WithCancel(ctx)
		defer stop()
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			select {
			case <-playCtx.Done():
				cancel()
				stop()
			case <-joined.Done():
			}
		}()
		err := a.play(joined)
		if err != nil {
			cancelCause(err)
		}
		stop()
		<-watcherDone
		return err
	})
	a.owner = held
	defer func() { cancel(); a.flush(); <-held.Done() }()

	result := make(chan error, 1)
	state := State{Realtime: true, Phase: "connecting"}
	events := make(chan rtapi.Event, 32)
	transcripts := newRealtimeTranscripts()
	options.PlayedAudioMS = a.playedMS
	options.PlaybackEmpty = a.drained
	go func() {
		result <- rtapi.Run(ctx, options, q,
			func(e rtapi.Event) error {
				if e.Type == "transcript" {
					transcripts.put(e)
					return nil
				}
				if e.Type == "output_start" {
					a.begin()
				}
				if e.Type == "interrupt" {
					transcripts.clear()
					source.SetAdapting(false)
					a.flush()
				}
				select {
				case events <- e:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				default:
					return errors.New("realtime control queue overflow")
				}
			}, a.push)
	}()
	defer func() { cancel(); <-result }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	opened := time.Now()
	listeningUntil := time.Time{}
	drainUntil := time.Time{}
	terminalUntil := time.Time{}
	terminalQuiet := time.Time{}
	apply := func(e rtapi.Event) {
		if e.Terminal || e.Phase == "thanks" || e.Phase == "idle" {
			terminalUntil = time.Now().Add(2 * time.Second)
			return
		}
		if e.Type == "interrupt" {
			state.Phase = "listening"
			state.Reply = ""
			listeningUntil = time.Time{}
			drainUntil = time.Time{}
		}
		if e.Phase != "" {
			switch e.Phase {
			case "listening":
				state.Phase = "listening"
				if !e.FollowUp && e.Type != "ready" {
					listeningUntil = time.Time{}
					drainUntil = time.Time{}
				}
			case "thinking":
				state.Phase = "thinking"
				listeningUntil = time.Time{}
			case "speaking":
				state.Phase = "replying"
				listeningUntil = time.Time{}
			}
		}
		// The server sends cumulative user/assistant transcripts. Replacing the
		// displayed value avoids merging already cumulative fragments a second time.
		if e.Type == "transcript" {
			switch e.Role {
			case "user":
				state.Heard = e.Text()
			case "assistant":
				state.Reply = e.Text()
			}
		}
		if e.Type == "ready" || e.FollowUp {
			listeningUntil = time.Now().Add(15 * time.Second)
		}
		source.SetAdapting(state.Phase == "replying")
		Changed.Emit(state)
	}
	for {
		select {
		case <-ctx.Done():
			return realtimeOutcome(ctx, ctx.Err())
		case err := <-result:
			// Leave a receipt for the deferred join after consuming the result.
			result <- err
			return realtimeOutcome(ctx, err)
		case <-transcripts.wake:
			for _, e := range transcripts.take() {
				apply(e)
			}
		case e := <-events:
			apply(e)
		case now := <-tick.C:
			if !terminalUntil.IsZero() {
				if !a.empty() {
					terminalQuiet = time.Time{}
				} else if terminalQuiet.IsZero() {
					terminalQuiet = now
				}
				if now.After(terminalUntil) || (!terminalQuiet.IsZero() && now.Sub(terminalQuiet) >= speaker.HardwareTail) {
					return nil
				}
			}
			cut, err := mute.Get().MutedStrict()
			if err != nil || cut || (ring.IsSounding() && !ring.Offered()) || phone.Get().Busy() || micTaken() {
				return nil
			}
			// The follow-up window starts after local buffered playback, not after
			// the provider has finished sending it. The absolute 120 s cap still holds.
			if state.Phase == "listening" && !listeningUntil.IsZero() {
				if !a.empty() {
					drainUntil = now.Add(speaker.HardwareTail)
				}
				if !drainUntil.IsZero() {
					listeningUntil = drainUntil.Add(15 * time.Second)
				}
				if now.After(listeningUntil) {
					return nil
				}
			}
			elapsed := now.Sub(opened)
			if elapsed >= 110*time.Second {
				remaining := max(0, 120-int(elapsed/time.Second))
				if state.Remaining != remaining {
					state.Remaining = remaining
					Changed.Emit(state)
				}
			}
		}
	}
}

// Cancellation carries the capture failure atomically. Regardless of which
// ready select branch wins, a microphone/queue failure cannot look like Stop.
func realtimeOutcome(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

// Transcript deltas are cumulative snapshots, so coalesce each role while the
// UI is busy. Audio and interruption processing never wait for rendering.
type realtimeTranscripts struct {
	mu              sync.Mutex
	user, assistant *rtapi.Event
	wake            chan struct{}
}

func newRealtimeTranscripts() *realtimeTranscripts {
	return &realtimeTranscripts{wake: make(chan struct{}, 1)}
}
func (m *realtimeTranscripts) put(e rtapi.Event) {
	m.mu.Lock()
	switch e.Role {
	case "user":
		m.user = &e
	case "assistant":
		m.assistant = &e
	}
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *realtimeTranscripts) take() []rtapi.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []rtapi.Event
	if m.user != nil {
		result = append(result, *m.user)
	}
	if m.assistant != nil {
		result = append(result, *m.assistant)
	}
	m.user, m.assistant = nil, nil
	return result
}
func (m *realtimeTranscripts) clear() { m.mu.Lock(); m.user, m.assistant = nil, nil; m.mu.Unlock() }
