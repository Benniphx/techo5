package echod

import (
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"testing"
)

func TestRealtimeAnalysisBoundsWorkAndAlignsFrames(t *testing.T) {
	frameBytes := mic.Channels * mic.Bits / 8
	capture := make([]byte, 8*mic.Rate*frameBytes+2)
	window := realtimeAnalysis(capture)
	if len(window) != mic.Rate/2*frameBytes || len(window)%frameBytes != 0 {
		t.Fatal("unbounded or partial-frame correlation window")
	}
	if len(realtimeAnalysis(make([]byte, frameBytes-1))) != 0 {
		t.Fatal("partial frame included")
	}
}
