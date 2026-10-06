//go:build !dot && !spot

package video

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ffmpegPath is the decoder in the image (tools/linux/build-ffmpeg.sh); a variable for a test daemon
// run from elsewhere (TECHO5_FFMPEG).
var ffmpegPath = func() string {
	if p := os.Getenv("TECHO5_FFMPEG"); p != "" {
		return p
	}
	return "/usr/local/bin/techo5-ffmpeg"
}()

// caFile is where the image keeps the certificate authorities, for https.
const caFile = "/etc/ssl/certs/ca-certificates.crt"

// Installed is whether this image carries the decoder: an older image offers the switches but plays
// nothing.
func Installed() bool {
	_, err := os.Stat(ffmpegPath)
	return err == nil
}

// decoderUser is who the decoder runs as: it parses whatever a stream holds, which is the classic way
// into a program, so it runs as a user that owns nothing and can only reach the network. The image
// makes it (tools/linux/mkrootfs.sh); an older image has nobody, which does as well.
var decoderUsers = []string{"techo5-video", "nobody"}

// decoderCred is the user to run the decoder as, never root: with none to be had, nothing is decoded.
// A variable for the tests, which run as whoever runs them.
var decoderCred = lookupDecoderCred

// decoderEnv is the decoder's whole environment: nothing of the daemon's.
var decoderEnv = []string{"PATH=/usr/bin:/bin", "HOME=/"}

func lookupDecoderCred() (*syscall.Credential, error) {
	for _, name := range decoderUsers {
		u, err := user.Lookup(name)
		if err != nil {
			continue
		}
		uid, err1 := strconv.ParseUint(u.Uid, 10, 32)
		gid, err2 := strconv.ParseUint(u.Gid, 10, 32)
		if err1 != nil || err2 != nil || uid == 0 || gid == 0 {
			continue
		}
		c := &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		// These Android kernels give a network socket only to the inet group.
		if g, err := user.LookupGroup("inet"); err == nil {
			if id, err := strconv.ParseUint(g.Gid, 10, 32); err == nil {
				c.Groups = []uint32{uint32(id)}
			}
		}
		return c, nil
	}
	return nil, errors.New("no user to run the decoder as in this image")
}

// limits are what the decoder may use. Memory is bounded well under what the device has, so a stream
// that makes it grow cannot take the daemon down with it; it may write no file at all (pipes are not
// files), keep no core, and open few.
var limits = []struct {
	res int
	max uint64
}{
	{unix.RLIMIT_AS, 640 << 20},
	{unix.RLIMIT_FSIZE, 0},
	{unix.RLIMIT_CORE, 0},
	{unix.RLIMIT_NOFILE, 64},
}

// niceness is how far below the daemon the decoder runs: the wake word and the speaker come first,
// and a picture that falls behind drops frames rather than holding them up.
const niceness = 5

// decoder is one run of ffmpeg.
type decoder struct {
	cmd    *exec.Cmd
	video  *os.File // the picture, read end (nil for a probe)
	audio  *os.File // the sound, read end (nil when there is none, or for a probe)
	stderr *tail
	done   chan struct{}
	err    error
}

// start runs ffmpeg with args: for a probe, only its standard error is wanted; for decoding, the
// picture on fd 3 and the sound on its standard output.
func start(ctx context.Context, args []string, decode, sound bool) (*decoder, error) {
	cred, err := decoderCred()
	if err != nil {
		return nil, err
	}
	d := &decoder{stderr: &tail{}, done: make(chan struct{})}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	cmd.Dir = "/"
	cmd.Env = decoderEnv
	cmd.Stderr = d.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred, Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var closeAfter []*os.File
	if decode {
		vr, vw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		growPipe(vr)
		d.video = vr
		cmd.ExtraFiles = []*os.File{vw}
		closeAfter = append(closeAfter, vw)
		if sound {
			ar, aw, err := os.Pipe()
			if err != nil {
				vr.Close()
				vw.Close()
				return nil, err
			}
			growPipe(ar)
			d.audio = ar
			cmd.Stdout = aw
			closeAfter = append(closeAfter, aw)
		}
	}
	if err := cmd.Start(); err != nil {
		for _, f := range closeAfter {
			f.Close()
		}
		d.closeReads()
		return nil, err
	}
	for _, f := range closeAfter {
		f.Close() // the child's now
	}
	pid := cmd.Process.Pid
	for _, l := range limits {
		lim := unix.Rlimit{Cur: l.max, Max: l.max}
		_ = unix.Prlimit(pid, l.res, &lim, nil)
	}
	_ = unix.Setpriority(unix.PRIO_PROCESS, pid, niceness)
	d.cmd = cmd
	go func() {
		d.err = cmd.Wait()
		close(d.done)
	}()
	return d, nil
}

// growPipe gives the pipe a megabyte, the most an unprivileged reader could ask for: a second of a
// video's frames does not fit, but several of its sound do, and every bit of slack is a stall the
// decoder rides out.
func growPipe(f *os.File) {
	_, _ = unix.FcntlInt(f.Fd(), unix.F_SETPIPE_SZ, 1<<20)
}

func (d *decoder) closeReads() {
	if d.video != nil {
		d.video.Close()
	}
	if d.audio != nil {
		d.audio.Close()
	}
}

// stop ends the run at once and waits for it.
func (d *decoder) stop() {
	if d.cmd != nil && d.cmd.Process != nil {
		_ = syscall.Kill(-d.cmd.Process.Pid, syscall.SIGKILL)
	}
	d.closeReads()
	<-d.done
}

// why is what went wrong, from what ffmpeg said last.
func (d *decoder) why() error {
	if s := d.stderr.last(); s != "" {
		return errors.New(s)
	}
	if d.err != nil {
		return d.err
	}
	return errors.New("the decoder stopped")
}

// probe asks ffmpeg what is at the address.
func probe(ctx context.Context, url string, insecure bool) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	d, err := start(ctx, ProbeArgs(url, caFileIf(), insecure), false, false)
	if err != nil {
		return Info{}, err
	}
	<-d.done
	if ctx.Err() != nil {
		return Info{}, fmt.Errorf("nothing came from the address in %s", probeTimeout)
	}
	return ParseProbe(d.stderr.all())
}

// probeTimeout is how long the address has to say what it is.
const probeTimeout = 20 * time.Second

func caFileIf() string {
	if _, err := os.Stat(caFile); err == nil {
		return caFile
	}
	return ""
}

// tail keeps what ffmpeg says on its standard error: all of it up to a bound, for a probe, and the last
// line worth saying, for why a run failed.
type tail struct {
	mu   sync.Mutex
	buf  []byte
	line string
}

const tailMost = 64 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) < tailMost {
		t.buf = append(t.buf, p[:min(len(p), tailMost-len(t.buf))]...)
	}
	for _, l := range strings.Split(string(p), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			t.line = clip(l, 200)
		}
	}
	return len(p), nil
}

func (t *tail) all() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.line
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
