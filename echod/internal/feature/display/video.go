//go:build !dot && !spot

package display

import (
	"context"
	"image"
	"log/slog"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/video"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// The video page (render_video.go): a video from feature/video, full screen over the clock, the
// dashboards and the deck. A call, a ring, the PIN pad, a browser asking for the setup page, the
// pairing and Wi-Fi pages, a camera, the settings and a voice turn go over it, and it pauses under
// them (video.Covered). A tap brings up its controls for a few seconds; a swipe down, or Stop, ends it.
//
// The picture never goes through the canvas. Once frames come, the frame loop draws only the
// controls there, and paintVideo hands each frame to the panel as the video's clock reaches it
// (screen.PresentFrame), blending the controls over it while they are up.

// videoControls is how long the controls stay up after a tap.
const videoControls = 4 * time.Second

// videoRecheck is how often the frame loop looks again at everything else while a video plays: a
// call or a ring coming over it is noticed within this, or at once when it wakes the loop.
const videoRecheck = 500 * time.Millisecond

// videoFailShown is how long the page says a video could not be played.
const videoFailShown = 6 * time.Second

// videoTap is a tap on the video page's controls.
type videoTap int

const (
	videoTapNone videoTap = iota
	videoTapPlay
	videoTapStop
	videoTapQuieter
	videoTapLouder
)

// videoCovered is whether something in the scene goes over the video, so it is not the page.
func videoCovered(s scene) bool {
	turn := s.phase == "listening" || s.phase == "thinking" || s.phase == "replying"
	return s.call.Phase != phone.Idle || s.ring.any() || s.pin.open || s.setupAsking || s.showVideoAsk || s.bt.Pairing ||
		s.showWifi || s.showCamera || s.showSheet || turn
}

// videoScene fills in the video's part of the scene, and tells the player whether it is covered.
func (d *Display) videoScene(s *scene, now time.Time) {
	st := video.Get().State()
	s.video = st
	if id, from, title, ok := video.Get().Asking(); ok {
		s.showVideoAsk, s.videoAsk = true, videoAsk{id: id, from: from, title: title}
	}
	d.mu.Lock()
	tried := d.videoTried
	d.mu.Unlock()
	failedRecently := st.Failed != 0 && st.Failed == tried && now.Sub(st.ErrAt) < videoFailShown
	up := st.Active() && st.Phase != video.Asking
	s.showVideo = (up || failedRecently) && !videoCovered(*s)
	video.Get().Covered(up && videoCovered(*s))
	d.mu.Lock()
	if up {
		d.videoTried = st.ID
	}
	controls := now.Before(d.videoUntil) || st.Phase == video.Paused || st.Phase == video.Loading || !st.Frames
	s.videoControls = controls
	s.videoLive = s.showVideo && up && st.Frames
	d.videoOnScreen = s.showVideo
	d.mu.Unlock()
}

// videoNext is how long the frame loop may wait before it looks again while the video page is up.
func (d *Display) videoNext(now time.Time) time.Duration {
	d.mu.Lock()
	until := d.videoUntil
	d.mu.Unlock()
	wait := videoRecheck
	if left := until.Sub(now); left > 0 && left < wait {
		wait = left + 10*time.Millisecond // the controls go when they are due to
	}
	return wait
}

// videoUp is whether the last frame drew the video page: then every finger is its.
func (d *Display) videoUp() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.videoOnScreen
}

// videoGesture is a finger on the video page.
func (d *Display) videoGesture(g touch.Gesture) {
	defer d.wake()
	if !video.Get().State().Active() {
		// The page saying a video could not be played: any touch puts it away.
		d.mu.Lock()
		d.videoTried = 0
		d.mu.Unlock()
		return
	}
	switch g.Kind {
	case touch.SwipeDown:
		slog.Info("screen: video stopped by a swipe")
		go video.Get().Stop()
	case touch.SwipeUp:
		d.showVideoControls()
	case touch.Tap:
		d.mu.Lock()
		shown := time.Now().Before(d.videoUntil)
		d.mu.Unlock()
		st := video.Get().State()
		controls := shown || st.Phase == video.Paused || st.Phase == video.Loading || !st.Frames
		if !controls {
			d.showVideoControls()
			return
		}
		switch d.r.videoTapped(image.Pt(g.X, g.Y)) {
		case videoTapPlay:
			go video.Get().Toggle()
			d.showVideoControls()
		case videoTapStop:
			slog.Info("screen: video stopped")
			go video.Get().Stop()
		case videoTapQuieter:
			go media.Get().Adjust(-1)
			d.showVideoControls()
		case videoTapLouder:
			go media.Get().Adjust(+1)
			d.showVideoControls()
		default:
			// Off the controls: they go, and the picture is the whole screen again.
			d.mu.Lock()
			d.videoUntil = time.Time{}
			d.mu.Unlock()
		}
	}
}

func (d *Display) showVideoControls() {
	d.mu.Lock()
	d.videoUntil = time.Now().Add(videoControls)
	d.mu.Unlock()
}

// videoAskGesture is a finger on the question about a DLNA video: only the two answers answer it.
func (d *Display) videoAskGesture(g touch.Gesture, id uint64) {
	if g.Kind != touch.Tap || d.r == nil {
		return
	}
	if allow, answered := d.r.askTap(g.X, g.Y); answered {
		go video.Get().Answer(id, allow)
	}
}

// paintVideo shows the video's frames as they fall due, until wait has passed, something wakes the
// frame loop, or there is no picture any more. redraw is the controls having changed: the frame on
// the panel is put up again with the new ones over it.
func (d *Display) paintVideo(ctx context.Context, wait time.Duration, over image.Rectangle) {
	deadline := time.Now().Add(wait)
	redraw := true
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		fr, due, ok := video.Get().Next()
		if !ok {
			return
		}
		switch {
		case fr != nil && d.videoDark:
			video.Get().Done(fr)
			continue
		case fr != nil:
			if err := d.dev.PresentFrame(fr.Pix, over); err != nil {
				slog.Warn("presenting a video frame failed", "err", err)
			}
			video.Get().Done(d.videoLast)
			d.videoLast, redraw = fr, false
			continue
		case redraw && d.videoLast != nil && !d.videoDark:
			if err := d.dev.PresentFrame(d.videoLast.Pix, over); err != nil {
				slog.Warn("presenting a video frame failed", "err", err)
			}
			redraw = false
		}
		due = max(due, 2*time.Millisecond)
		if left := time.Until(deadline); left < due {
			if left <= 0 {
				return
			}
			due = left
		}
		timer.Reset(due)
		select {
		case <-ctx.Done():
			return
		case <-d.poke:
			return
		case <-timer.C:
		}
	}
}

// answerVideoShots hands whoever asked for a screenshot the video page as it is on the panel.
func (d *Display) answerVideoShots() {
	for {
		select {
		case ch := <-d.shots:
			ch <- d.videoShot()
		default:
			return
		}
	}
}

// dropVideoFrame gives back the frame kept for the panel once the video page is gone.
func (d *Display) dropVideoFrame() {
	if d.videoLast != nil {
		video.Get().Done(d.videoLast)
		d.videoLast = nil
	}
}

// videoShot is a screenshot of the video page: the frame on the panel turned back to the canvas's
// way up, with the canvas (the controls) over it.
func (d *Display) videoShot() *image.RGBA {
	img := image.NewRGBA(d.r.dst.Rect)
	if fr := d.videoLast; fr != nil && d.dev != nil {
		pw, ph := d.dev.FrameSize()
		bgra := d.dev.PixFmt() == "bgra"
		w, h := img.Rect.Dx(), img.Rect.Dy()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				px, py := x, y
				if d.dev.Rotated() {
					px, py = pw-1-y, x
				}
				if px < 0 || py < 0 || px >= pw || py >= ph {
					continue
				}
				i := (py*pw + px) * 4
				o := img.PixOffset(x, y)
				p := fr.Pix[i : i+4 : i+4]
				if bgra {
					img.Pix[o], img.Pix[o+1], img.Pix[o+2] = p[2], p[1], p[0]
				} else {
					img.Pix[o], img.Pix[o+1], img.Pix[o+2] = p[0], p[1], p[2]
				}
				img.Pix[o+3] = 255
			}
		}
	}
	// The controls, as the panel blends them.
	src := d.r.dst
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := uint32(src.Pix[i+3])
		if a == 0 {
			continue
		}
		for k := 0; k < 3; k++ {
			img.Pix[i+k] = uint8(uint32(src.Pix[i+k]) + uint32(img.Pix[i+k])*(255-a)/255)
		}
	}
	return img
}
