package doorbell

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/onvifevent"
)

func visitor(source, state, op string) onvifevent.Event {
	return onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/Visitor", Op: op,
		Items: map[string]string{"Source": source, "State": state}}
}

func TestPressed(t *testing.T) {
	for _, c := range []struct {
		name string
		ev   onvifevent.Event
		ch   int
		want bool
	}{
		{"press on its channel", visitor("000", "true", "Changed"), 0, true},
		{"press on the second channel", visitor("001", "true", "Changed"), 1, true},
		{"another camera's press", visitor("001", "true", "Changed"), 0, false},
		{"the press ending", visitor("000", "false", "Changed"), 0, false},
		{"the state as a subscription opens", visitor("000", "true", "Initialized"), 0, false},
		{"a person, not a press", onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/PeopleDetect", Op: "Changed",
			Items: map[string]string{"Source": "000", "State": "true"}}, 0, false},
		{"a single camera with no source", onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/Visitor", Op: "Changed",
			Items: map[string]string{"State": "true"}}, 0, true},
		{"a source that is not a number", visitor("door", "true", "Changed"), 0, false},
	} {
		if got := pressed(c.ev, c.ch); got != c.want {
			t.Errorf("%s: pressed = %v, want %v", c.name, got, c.want)
		}
	}
}

// testFeature is a doorbell whose ring and camera are counters, on a clock the test moves.
func testFeature() (f *Feature, rings, shows *atomic.Int32, clock *time.Time) {
	rings, shows = new(atomic.Int32), new(atomic.Int32)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	clock = &now
	var mu sync.Mutex
	f = &Feature{
		poke: make(chan struct{}, 1),
		ring: func() { rings.Add(1) },
		show: func(string) { shows.Add(1) },
		now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return *clock
		},
	}
	return f, rings, shows, clock
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestPressRingsOnceForAQuickPair(t *testing.T) {
	f, rings, shows, clock := testFeature()
	f.press("reolink:0")
	f.press("reolink:0") // the same press, reported twice
	eventually(t, "one ring and one camera", func() bool { return rings.Load() == 1 && shows.Load() == 1 })
	*clock = clock.Add(again)
	f.press("reolink:0")
	eventually(t, "a second press rings again", func() bool { return rings.Load() == 2 && shows.Load() == 2 })
	if f.Status().LastPress != *clock {
		t.Errorf("last press %v, want %v", f.Status().LastPress, *clock)
	}
}

// hub is a recorder that answers every pull with the events queued for it, then waits.
type hub struct {
	mu     sync.Mutex
	queue  []string // notifications, one per pull
	closed atomic.Bool
}

const env = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:wsa5="http://www.w3.org/2005/08/addressing"><s:Body>%s</s:Body></s:Envelope>`

func note(source, state, op string) string {
	return `<wsnt:NotificationMessage><wsnt:Topic>tns1:RuleEngine/MyRuleDetector/Visitor</wsnt:Topic><wsnt:Message><tt:Message PropertyOperation="` + op +
		`"><tt:Source><tt:SimpleItem Name="Source" Value="` + source + `"/></tt:Source><tt:Data><tt:SimpleItem Name="State" Value="` + state +
		`"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>`
}

func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	ct := r.Header.Get("Content-Type")
	reply := func(body string) { io.WriteString(w, strings.Replace(env, "%s", body, 1)) }
	switch {
	case strings.Contains(ct, "GetSystemDateAndTime"), strings.Contains(ct, "GetCapabilities"):
		reply("")
	case strings.Contains(ct, "CreatePullPoint"):
		reply(`<CreatePullPointSubscriptionResponse><SubscriptionReference><wsa5:Address>http://198.51.100.9/onvif/PullSubManager?Idx=1</wsa5:Address></SubscriptionReference></CreatePullPointSubscriptionResponse>`)
	case strings.Contains(ct, "PullMessages"):
		h.mu.Lock()
		var next string
		if len(h.queue) > 0 {
			next, h.queue = h.queue[0], h.queue[1:]
		}
		h.mu.Unlock()
		if next == "" {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
		}
		reply(`<PullMessagesResponse>` + next + `</PullMessagesResponse>`)
	case strings.Contains(ct, "Unsubscribe"):
		h.closed.Store(true)
		reply("")
	default:
		reply("")
	}
}

func TestListenRingsForItsDoorbellOnly(t *testing.T) {
	h := &hub{queue: []string{
		note("000", "true", "Initialized"), // pressed as the subscription opened: not a new press
		note("001", "true", "Changed"),     // the other camera's button
		note("000", "true", "Changed") + note("000", "false", "Changed"),
	}}
	srv := httptest.NewServer(h)
	defer srv.Close()

	f, rings, shows, _ := testFeature()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- f.listen(ctx, target{camera: "reolink:0", base: srv.URL, user: "admin", pass: "x", channel: 0})
	}()

	eventually(t, "the press rings", func() bool { return rings.Load() == 1 && shows.Load() == 1 })
	if !f.Status().Listening {
		t.Error("status does not say listening")
	}
	time.Sleep(100 * time.Millisecond)
	if rings.Load() != 1 {
		t.Errorf("%d rings, want 1", rings.Load())
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("listen ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listen did not stop")
	}
	eventually(t, "the subscription is closed", h.closed.Load)
}

func TestExplain(t *testing.T) {
	tg := target{base: "http://192.0.2.5:8000"}
	if got := explain(onvifevent.ErrAuth, tg); got != "the recorder refused the login" {
		t.Errorf("auth: %q", got)
	}
	if got := explain(errors.New("dial tcp 192.0.2.5:8000: connect: connection refused"), tg); !strings.Contains(got, "turn ONVIF on") {
		t.Errorf("refused: %q", got)
	}
}

func TestChimeRenders(t *testing.T) {
	pcm := render(48000)
	secs := float64(len(pcm)) / 48000
	if secs < 3 || secs > 6 {
		t.Errorf("chime lasts %.1f s", secs)
	}
	most := 0
	for _, s := range pcm {
		most = max(most, abs(int(s)))
	}
	if most < 30000 || most > 32767 {
		t.Errorf("peak %d, want near full scale and unclipped", most)
	}
	// It dies away: the last tenth is far quieter than the first strike.
	tail := 0
	for _, s := range pcm[len(pcm)*9/10:] {
		tail = max(tail, abs(int(s)))
	}
	if tail > most/4 {
		t.Errorf("tail peak %d of %d: the bars do not ring out", tail, most)
	}
	if n := dingDong(); len(n) != 1 || n[0].Clip == nil || n[0].Ms < 3000 {
		t.Errorf("dingDong = %+v", n)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
