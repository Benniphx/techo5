//go:build live

package onvifback

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// A session with a real camera: TECHO5_CAM_URL (rtsp://host/path), and a login file as the owner keeps
// one (TECHO5_CAM_LOGINS: a line "default user: X password: Y"). Three seconds of silence, so nothing is
// heard; TECHO5_CAM_TONE=1 sends a quiet tone instead.
func TestLiveCamera(t *testing.T) {
	addr := os.Getenv("TECHO5_CAM_URL")
	raw, err := os.ReadFile(os.Getenv("TECHO5_CAM_LOGINS"))
	if addr == "" || err != nil {
		t.Skip("TECHO5_CAM_URL and TECHO5_CAM_LOGINS")
	}
	var user, pass string
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			switch strings.ToLower(strings.TrimSuffix(f[i], ":")) {
			case "user":
				user = f[i+1]
			case "password":
				pass = strings.Join(f[i+1:], " ")
			}
		}
		if user != "" && pass != "" {
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	s, err := Open(ctx, addr, user, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t.Logf("open in %s, %s", time.Since(start).Round(time.Millisecond), s.Codec())
	frame := make([]int16, PacketSamples)
	for i := 0; i < 150; i++ { // 3 s, in time
		if os.Getenv("TECHO5_CAM_TONE") == "1" {
			for j := range frame {
				if (i*PacketSamples+j)%16 < 8 {
					frame[j] = 1500
				} else {
					frame[j] = -1500
				}
			}
		}
		if err := s.Write(frame); err != nil {
			t.Fatalf("after %d packets: %v", i, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-s.Done():
		t.Fatal("the camera closed the session")
	default:
	}
}
