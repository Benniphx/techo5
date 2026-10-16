// Package kernel updates the 2nd gen Show 8's kernel with the root filesystem.
//
// athena boots TECHO5's mainline kernel from its boot partition, which an update of the system slots
// never touched: a kernel fix reached a unit only through the installer. So the athena rootfs carries
// the kernel it was tested with (the released boot image, its tuning slot empty), and once that slot
// has proved itself (committed good) the kernel goes to the boot partition the way the installer puts
// it there: the unit's own amp tuning, taken from the kernel it runs now, in the slot, the header id
// made again.
//
// A new kernel is on trial, as a new slot is: it has three boots to reach a daemon that settles.
// Every boot takes a try (Boot); a daemon that settles commits it (Settled). One that runs out of
// tries puts the previous boot image back, which is kept for that, and is not written again. A kernel
// that hangs before any of this runs cannot be caught here; the only way back from one is the way the
// unit was installed (mtkclient), so kernels are tested on units before they are released.
//
// The state is a directory on the data partition, which every slot shares:
//
//	state       trial <tries> <written sha256> <written length> <shipped sha256>
//	            | good <written sha256> <shipped sha256>
//	prev.img    the boot partition's image before the last write (its own length)
//	prev.sha256 its sha256
//	bad         shipped kernels that failed their trial, one sha256 a line
package kernel

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tries is how many boots a new kernel has to settle in, as slotctl gives a new slot.
const Tries = 3

// Paths are where everything is. Default is the unit's; tests use their own.
type Paths struct {
	Shipped    string // the slot's boot image, slot empty
	ShippedSum string // its sha256, hex (sha256sum's format is taken too)
	Boot       string // the boot partition
	Dir        string // the state directory
}

// Default are the unit's paths. Boot is found by its partition name at run time (BootDevice).
var Default = Paths{
	Shipped:    "/usr/share/techo5/kernel/boot-athena.img",
	ShippedSum: "/usr/share/techo5/kernel/boot-athena.img.sha256",
	Dir:        "/data/techo5-linux/kernel",
}

// Log takes the lines worth keeping in the boot log.
var Log = func(format string, a ...any) { fmt.Fprintf(os.Stderr, "kernel: "+format+"\n", a...) }

type state struct {
	kind    string // "", "trial" or "good"
	tries   int
	written string
	length  int
	shipped string
}

func (p Paths) file(name string) string { return filepath.Join(p.Dir, name) }

func (p Paths) readState() (state, error) {
	b, err := os.ReadFile(p.file("state"))
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	f := strings.Fields(string(b))
	switch {
	case len(f) == 5 && f[0] == "trial":
		t, err1 := strconv.Atoi(f[1])
		n, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			break
		}
		return state{kind: "trial", tries: t, written: f[2], length: n, shipped: f[4]}, nil
	case len(f) == 3 && f[0] == "good":
		return state{kind: "good", written: f[1], shipped: f[2]}, nil
	}
	return state{}, fmt.Errorf("state file says %q", strings.TrimSpace(string(b)))
}

func (p Paths) writeState(s state) error {
	var line string
	switch s.kind {
	case "trial":
		line = fmt.Sprintf("trial %d %s %d %s", s.tries, s.written, s.length, s.shipped)
	case "good":
		line = fmt.Sprintf("good %s %s", s.written, s.shipped)
	default:
		err := os.Remove(p.file("state"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeFile(p.file("state"), []byte(line+"\n"))
}

// writeFile writes atomically: a temporary file, synced, renamed over the old one.
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (p Paths) bad() map[string]bool {
	m := map[string]bool{}
	b, _ := os.ReadFile(p.file("bad"))
	for _, l := range strings.Fields(string(b)) {
		m[l] = true
	}
	return m
}

func (p Paths) markBad(shipped string) error {
	b, _ := os.ReadFile(p.file("bad"))
	return writeFile(p.file("bad"), append(b, []byte(shipped+"\n")...))
}

// readBoot is the whole boot partition.
func (p Paths) readBoot() ([]byte, error) {
	f, err := os.Open(p.Boot)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dropCache(f)
	return io.ReadAll(f)
}

// writeBoot writes img at the start of the boot partition, syncs, and reads it back past the cache.
func (p Paths) writeBoot(img []byte) error {
	f, err := os.OpenFile(p.Boot, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if size, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	} else if int64(len(img)) > size {
		return fmt.Errorf("%d bytes do not fit the %d-byte boot partition", len(img), size)
	}
	if _, err := f.WriteAt(img, 0); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	dropCache(f)
	back := make([]byte, len(img))
	if _, err := f.ReadAt(back, 0); err != nil {
		return err
	}
	if !bytes.Equal(back, img) {
		return errors.New("the boot partition does not read back what was written")
	}
	return nil
}

// shipped is the slot's kernel, checked against its sum and as an image. ok false: this slot carries
// none.
func (p Paths) shipped() (img []byte, sum string, ok bool, err error) {
	img, err = os.ReadFile(p.Shipped)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	s, err := os.ReadFile(p.ShippedSum)
	if err != nil {
		return nil, "", false, fmt.Errorf("the slot's kernel has no sum: %w", err)
	}
	want := strings.Fields(string(s))
	sum = sha(img)
	if len(want) == 0 || !strings.EqualFold(want[0], sum) {
		return nil, "", false, fmt.Errorf("the slot's kernel is %s, its sum says %s", sum, strings.TrimSpace(string(s)))
	}
	if _, err := checkShipped(img); err != nil {
		return nil, "", false, fmt.Errorf("the slot's kernel: %w", err)
	}
	return img, sum, true, nil
}

// Status says what the state is, for people.
func Status(p Paths) string {
	s, err := p.readState()
	var b strings.Builder
	switch {
	case err != nil:
		fmt.Fprintf(&b, "state: unreadable: %v\n", err)
	case s.kind == "trial":
		fmt.Fprintf(&b, "state: on trial, %d boots left; written %s, shipped %s\n", s.tries, s.written, s.shipped)
	case s.kind == "good":
		fmt.Fprintf(&b, "state: good; written %s, shipped %s\n", s.written, s.shipped)
	default:
		b.WriteString("state: none (no kernel written by an update)\n")
	}
	if _, sum, ok, err := p.shipped(); err != nil {
		fmt.Fprintf(&b, "slot's kernel: %v\n", err)
	} else if ok {
		fmt.Fprintf(&b, "slot's kernel: %s\n", sum)
	} else {
		b.WriteString("slot's kernel: none\n")
	}
	for s := range p.bad() {
		fmt.Fprintf(&b, "bad: %s\n", s)
	}
	return b.String()
}

// Boot runs early at every boot. A kernel on trial takes a try; one with none left is replaced by
// the previous image and marked bad, and reboot says to reboot into it.
func Boot(p Paths) (reboot bool, err error) {
	s, err := p.readState()
	if err != nil || s.kind != "trial" {
		return false, err
	}
	boot, err := p.readBoot()
	if err != nil {
		return false, err
	}
	if len(boot) < s.length || sha(boot[:s.length]) != s.written {
		// The state went down before the write, and the write never happened (or the image was
		// replaced since by other means): there is nothing on trial.
		Log("the boot partition does not hold the kernel on trial; the trial is dropped")
		return false, p.writeState(state{})
	}
	if s.tries > 0 {
		s.tries--
		Log("the kernel is on trial, %d boots left after this one", s.tries)
		return false, p.writeState(s)
	}
	Log("the kernel used all its tries without settling; putting the previous one back")
	if err := p.markBad(s.shipped); err != nil {
		return false, err
	}
	if err := p.restore(); err != nil {
		return false, err
	}
	return true, p.writeState(state{})
}

// Settled commits a kernel on trial: the daemon has run long enough on it.
func Settled(p Paths) error {
	s, err := p.readState()
	if err != nil || s.kind != "trial" {
		return err
	}
	Log("the kernel settled; it is good")
	return p.writeState(state{kind: "good", written: s.written, shipped: s.shipped})
}

// Apply writes the slot's kernel to the boot partition when it holds another one. reboot says it was
// written, and the unit should boot into it now. Nothing is written while a kernel is on trial, for a
// kernel that failed, or when the tuning in the running kernel is not one the installer knows.
func Apply(p Paths) (reboot bool, err error) {
	s, err := p.readState()
	if err != nil {
		return false, err
	}
	if s.kind == "trial" {
		return false, nil
	}
	img, sum, ok, err := p.shipped()
	if err != nil || !ok {
		return false, err
	}
	if p.bad()[sum] {
		Log("the slot's kernel %s failed its trial before; not written again", sum)
		return false, nil
	}
	part, err := p.readBoot()
	if err != nil {
		return false, err
	}
	cur, err := parseImage(part)
	if err != nil {
		return false, fmt.Errorf("the boot partition: %w", err)
	}
	if !cur.idOK() {
		return false, errors.New("the boot partition's header id does not match its contents; not touching it")
	}
	table, err := cur.table()
	if err != nil {
		return false, fmt.Errorf("the running kernel's amp tuning: %w", err)
	}
	if table == nil {
		return false, errors.New("the running kernel's amp tuning slot is empty; run the installer")
	}
	name, known := knownTables[sha(table)]
	if !known {
		return false, fmt.Errorf("the running kernel's amp tuning (%s) is not one the installer knows", sha(table))
	}
	if same, err := cur.withTable(nil); err == nil && bytes.Equal(same, img) {
		return false, nil // up to date
	}
	ship, _ := parseImage(img)
	filled, err := ship.withTable(table)
	if err != nil {
		return false, err
	}
	if f, err := parseImage(filled); err != nil || !f.idOK() {
		return false, errors.New("the filled kernel does not check out")
	}
	prev := part[:cur.extent]
	if err := writeFile(p.file("prev.img"), prev); err != nil {
		return false, fmt.Errorf("keeping the previous kernel: %w", err)
	}
	if err := writeFile(p.file("prev.sha256"), []byte(sha(prev)+"\n")); err != nil {
		return false, err
	}
	trial := state{kind: "trial", tries: Tries, written: sha(filled), length: len(filled), shipped: sum}
	if err := p.writeState(trial); err != nil {
		return false, err
	}
	Log("writing the slot's kernel %s with this unit's amp tuning (%s)", sum, name)
	if err := p.writeBoot(filled); err != nil {
		Log("the write failed (%v); putting the previous kernel back", err)
		if rerr := p.restore(); rerr != nil {
			return false, fmt.Errorf("writing the kernel: %v; and putting the previous one back: %w", err, rerr)
		}
		return false, errors.Join(fmt.Errorf("writing the kernel: %w", err), p.writeState(s))
	}
	return true, nil
}

// restore writes the kept previous image back, after checking it is the one kept.
func (p Paths) restore() error {
	prev, err := os.ReadFile(p.file("prev.img"))
	if err != nil {
		return fmt.Errorf("the previous kernel: %w", err)
	}
	want, _ := os.ReadFile(p.file("prev.sha256"))
	if f := strings.Fields(string(want)); len(f) == 0 || f[0] != sha(prev) {
		return errors.New("the kept previous kernel does not match its sum; not writing it")
	}
	if im, err := parseImage(prev); err != nil || !im.idOK() {
		return errors.New("the kept previous kernel is not a whole boot image; not writing it")
	}
	return p.writeBoot(prev)
}
