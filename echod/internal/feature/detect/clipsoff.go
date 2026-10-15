package detect

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// Keep wake clips fails closed. Turned off, the recorder stops and the clips go at once (clips.go),
// but the setting itself is saved with the rest of the state, and a save that fails (a full or failing
// flash) used to leave "on" in the file: the next start read it and kept clips again, which nobody
// had asked for. So an off that cannot be saved is also recorded on its own, in clipsOffPath, a file
// next to the state that wins over it at the next start, and the save is tried again every
// clipsRetryEvery until it holds; the file goes once it does. A setting that is saved, either way,
// is the record, and the file goes then too.
var (
	clipsOffPath    = filepath.Join(layout.StateDir, "keep-clips-off")
	saveKeepClips   = func(on bool) error { return config.Set().KeepClips(on) }
	clipsRetryEvery = time.Minute
)

// clipsOffMarked is whether an off that was never saved is on record.
func clipsOffMarked() bool {
	_, err := os.Stat(clipsOffPath)
	return err == nil
}

// markClipsOff records an off the state could not take, synced, so a power cut right after it
// cannot lose it either.
func markClipsOff() error {
	f, err := os.OpenFile(clipsOffPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("off\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// clearClipsOff lets go of the record once the state holds the setting itself.
func clearClipsOff() {
	if err := os.Remove(clipsOffPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("keep wake clips: removing the off record failed", "path", clipsOffPath, "err", err)
	}
}

// keepClipsAtStart is what Keep wake clips starts as: the saved setting, unless an off that never
// reached the state is on record, which wins. That off is saved now if it can be, and tried again
// otherwise.
func (d *Detect) keepClipsAtStart(saved bool) bool {
	if !clipsOffMarked() {
		return saved
	}
	if !saved {
		clearClipsOff()
		return false
	}
	slog.Warn("keep wake clips: turned off before the last stop but never saved; staying off")
	d.saveClipsOff()
	return false
}

// saveClipsOff saves an off the state did not take: now, and again every clipsRetryEvery until it
// holds or Keep wake clips is turned on again (which saves its own setting).
func (d *Detect) saveClipsOff() {
	if d.retryClipsOffOnce() {
		return
	}
	if !d.clipsOffRetrying.CompareAndSwap(false, true) {
		return
	}
	every := clipsRetryEvery
	safe.Go("keep wake clips off", func() {
		defer d.clipsOffRetrying.Store(false)
		for {
			time.Sleep(every)
			if d.retryClipsOffOnce() {
				return
			}
		}
	})
}

// retryClipsOffOnce tries the save once, and reports whether there is nothing left to do: saved, or
// no longer wanted because clips are being kept again.
func (d *Detect) retryClipsOffOnce() bool {
	if d.clips != nil && d.clips.on.Load() {
		return true
	}
	if err := saveKeepClips(false); err != nil {
		slog.Error("saving keep wake clips off failed; trying again", "err", err, "after", clipsRetryEvery)
		return false
	}
	clearClipsOff()
	slog.Info("keep wake clips: off saved")
	return true
}
