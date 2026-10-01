package streaming

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Restarting: soon after a program that ran a while stops, then backing off to a minute for one that
// keeps stopping at once (a bad stream makes shairport-sync give up, for one).
var (
	restartFirst = 2 * time.Second
	restartMost  = time.Minute
	ranLong      = 30 * time.Second
)

// supervise runs the program at path with args until ctx ends, starting it again whenever it stops.
// stdout is where its standard output goes (nil for nowhere), and env is added to the daemon's own
// environment for it; what it logs goes to the daemon's log, line by line, under name.
func supervise(ctx context.Context, name, path string, args []string, stdout io.Writer, env ...string) {
	wait := restartFirst
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdout = stdout
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stderr = &logLines{name: name}
		// Asked to stop, a program gets a moment to say goodbye on the network before it is killed.
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 3 * time.Second
		start := time.Now()
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		slog.Warn("streaming: a program stopped", "program", name, "after", time.Since(start).Round(time.Second), "err", err)
		if time.Since(start) > ranLong {
			wait = restartFirst
		} else {
			wait = min(wait*2, restartMost)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// logLines is a program's standard error, in the daemon's log a line at a time, the last ones kept
// short: these programs say a lot, and only what goes wrong is worth the space.
type logLines struct {
	name string
	part []byte
}

func (l *logLines) Write(p []byte) (int, error) {
	l.part = append(l.part, p...)
	for {
		i := indexByte(l.part, '\n')
		if i < 0 {
			break
		}
		line := string(l.part[:i])
		l.part = l.part[i+1:]
		if line != "" {
			slog.Debug("streaming: program says", "program", l.name, "line", clip(line, 300))
		}
	}
	if len(l.part) > 4096 {
		l.part = l.part[:0]
	}
	return len(p), nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
