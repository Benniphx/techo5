//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestRealtimeDashboardPreservesVoicePhaseAndPagePriority(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Dashboard().Mode(config.DashboardStreamed); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, phase                    string
		camera, drawer, realtime, want bool
	}{
		{name: "voice", realtime: true, want: true},
		{name: "idle voice with music", phase: "idle", realtime: true, want: true},
		{name: "stock turn", want: false},
		{name: "camera", realtime: true, camera: true},
		{name: "drawer", realtime: true, drawer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			phase := test.phase
			if phase == "" {
				phase = "listening"
			}
			s := scene{phase: phase, realtime: test.realtime, nowPlaying: true, showCamera: test.camera}
			d := &Display{}
			d.dashScene(&s, test.drawer)
			if s.showDash != test.want || s.phase != phase || !s.nowPlaying {
				t.Fatalf("dashboard=%v phase=%s playing=%v", s.showDash, s.phase, s.nowPlaying)
			}
		})
	}
}
