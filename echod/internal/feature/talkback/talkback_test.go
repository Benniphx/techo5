//go:build !dot

package talkback

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// fakeSession is a camera that takes whatever it is sent, and hangs up when told to.
type fakeSession struct {
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func (s *fakeSession) Write([]int16) error   { return nil }
func (s *fakeSession) Codec() string         { return "PCMU" }
func (s *fakeSession) Done() <-chan struct{} { return s.done }
func (s *fakeSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *fakeSession) wasClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

const door = "camera.front_door"

// setUp is a device with the switch on and the door's address given, and a camera behind open that is
// handed back on the channel, or err.
func setUp(t *testing.T, err error) chan *fakeSession {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if e := config.Set().Security().TalkBack(true); e != nil {
		t.Fatal(e)
	}
	if e := config.Set().TalkBack().Cameras(map[string]string{door: "rtsp://192.168.1.40:554/h264Preview_01_main"}); e != nil {
		t.Fatal(e)
	}
	opened := make(chan *fakeSession, 4)
	was := open
	open = func(ctx context.Context, addr, user, pass string) (session, error) {
		if err != nil {
			return nil, err
		}
		s := &fakeSession{done: make(chan struct{})}
		opened <- s
		return s, nil
	}
	wasCheck := check
	check = 10 * time.Millisecond
	t.Cleanup(func() {
		shared.Stop()
		open, check = was, wasCheck
		home.Get().HideCamera()
	})
	return opened
}

// waitFor waits for the talk to reach phase.
func waitFor(t *testing.T, phase Phase) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := shared.State(); st.Phase == phase {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the talk did not reach phase %d; it is %+v", phase, shared.State())
	return State{}
}

// Talk is offered only with the switch on, for a camera with somewhere to send it, and never for the
// device's own.
func TestTalkIsOfferedOnlyWhereItCanWork(t *testing.T) {
	setUp(t, nil)
	if !Offered(door) {
		t.Error("a camera with an address was not offered Talk")
	}
	for _, e := range []string{"camera.deck", home.LocalCamera, ""} {
		if Offered(e) {
			t.Errorf("%q was offered Talk", e)
		}
	}
	config.Set().Security().TalkBack(false)
	if Offered(door) {
		t.Error("Talk was offered with the switch off")
	}
}

// A talk runs while the view of its camera is up, holds the view up, and ends, letting the camera go,
// when the view closes. A view that closes is not a failure to show.
func TestATalkLastsWhileItsViewIsUp(t *testing.T) {
	opened := setUp(t, nil)
	home.Get().ShowCamera(door, time.Second)
	shared.Start(door)
	s := <-opened
	waitFor(t, Talking)
	time.Sleep(1500 * time.Millisecond) // past the view's own second: the talk is holding it
	if v, up := home.Get().Camera(); !up || v.Entity != door {
		t.Fatal("the view timed out under a talk")
	}
	if st := shared.State(); st.Phase != Talking || st.Left <= 0 || st.Left > maxTalk {
		t.Fatalf("mid-talk the state is %+v", st)
	}
	home.Get().HideCamera()
	st := waitFor(t, Idle)
	if st.Error != "" {
		t.Errorf("a closed view was shown as a failure: %q", st.Error)
	}
	if !s.wasClosed() {
		t.Error("the camera was not let go")
	}
}

// The switch going off ends a talk; a second tap on Talk does too, at once.
func TestATalkEndsWhenAskedTo(t *testing.T) {
	opened := setUp(t, nil)
	home.Get().ShowCamera(door, time.Minute)
	shared.Start(door)
	<-opened
	waitFor(t, Talking)
	config.Set().Security().TalkBack(false)
	waitFor(t, Idle)

	config.Set().Security().TalkBack(true)
	shared.Toggle(door)
	s := <-opened
	waitFor(t, Talking)
	shared.Toggle(door)
	if st := shared.State(); st.Phase != Idle || !s.wasClosed() {
		t.Errorf("a second tap left %+v, closed %v", st, s.wasClosed())
	}
}

// A camera that hangs up, or will not take a talk at all, says so on the camera page.
func TestACameraThatWillNotTalkSaysWhy(t *testing.T) {
	opened := setUp(t, nil)
	home.Get().ShowCamera(door, time.Minute)
	check = time.Hour // only the camera ends this one
	shared.Start(door)
	s := <-opened
	waitFor(t, Talking)
	close(s.done)
	if st := waitFor(t, Idle); !strings.Contains(st.Error, "hung up") {
		t.Errorf("a hang-up showed %q", st.Error)
	}

	setUp(t, errors.New("the camera offers no talk-back channel"))
	home.Get().ShowCamera(door, time.Minute)
	shared.Start(door)
	if st := waitFor(t, Idle); st.Error != "the camera offers no talk-back channel" {
		t.Errorf("a refusal showed %q", st.Error)
	}
}

// The log gets an address without its login.
func TestRedacted(t *testing.T) {
	if got := redacted("rtsp://admin:secret@192.168.1.40:554/x"); strings.Contains(got, "secret") || strings.Contains(got, "admin") {
		t.Errorf("redacted to %q", got)
	}
}
