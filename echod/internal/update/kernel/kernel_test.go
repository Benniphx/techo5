package kernel

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const page = 2048

// mkImage is a header version 1 boot image like pack.py's: a kernel part holding a tuning slot
// (with table in it, or empty) after some bytes that tell kernels apart, and a small ramdisk.
func mkImage(t *testing.T, tag string, table []byte) []byte {
	t.Helper()
	slot := make([]byte, slotSize)
	copy(slot, slotMagic)
	if len(table) > 0 {
		binary.LittleEndian.PutUint32(slot[8:], uint32(len(table)))
		s := sha256.Sum256(table)
		copy(slot[12:], s[:])
		copy(slot[slotHead:], table)
	}
	k := append([]byte("kernel "+tag+" "), bytes.Repeat([]byte{0x5a}, 3000)...)
	k = append(k, slot...)
	r := []byte("ramdisk " + tag)
	img := make([]byte, page+pad(len(k), page)+pad(len(r), page))
	copy(img, "ANDROID!")
	binary.LittleEndian.PutUint32(img[8:], uint32(len(k)))
	binary.LittleEndian.PutUint32(img[16:], uint32(len(r)))
	binary.LittleEndian.PutUint32(img[36:], page)
	binary.LittleEndian.PutUint32(img[40:], 1)
	copy(img[page:], k)
	copy(img[page+pad(len(k), page):], r)
	im, err := parseImage(img)
	if err != nil {
		t.Fatal(err)
	}
	copy(img[headerID:], im.computedID())
	return img
}

var tableB = bytes.Repeat([]byte{0x01, 0x02}, 600)

type rig struct {
	t *testing.T
	p Paths
}

// newRig is a unit: its boot partition (16 KiB more than the image, as a partition is bigger)
// holding a kernel filled with tableB, and a slot carrying shipped (nil: none).
func newRig(t *testing.T, running, shipped []byte) *rig {
	t.Helper()
	old := knownTables
	knownTables = map[string]string{sha(tableB): "test table"}
	t.Cleanup(func() { knownTables = old })
	d := t.TempDir()
	p := Paths{
		Shipped:    filepath.Join(d, "slot", "boot-athena.img"),
		ShippedSum: filepath.Join(d, "slot", "boot-athena.img.sha256"),
		Boot:       filepath.Join(d, "boot-partition"),
		Dir:        filepath.Join(d, "data", "kernel"),
	}
	part := make([]byte, len(running)+16384)
	copy(part, running)
	for i := len(running); i < len(part); i++ {
		part[i] = 0xee // what was there before: not zeros
	}
	if err := os.WriteFile(p.Boot, part, 0o644); err != nil {
		t.Fatal(err)
	}
	if shipped != nil {
		os.MkdirAll(filepath.Dir(p.Shipped), 0o755)
		os.WriteFile(p.Shipped, shipped, 0o644)
		os.WriteFile(p.ShippedSum, []byte(sha(shipped)+"  boot-athena.img\n"), 0o644)
	}
	return &rig{t, p}
}

func (r *rig) boot() []byte {
	b, err := os.ReadFile(r.p.Boot)
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func (r *rig) holds(img []byte) bool { return bytes.HasPrefix(r.boot(), img) }

func (r *rig) state() string {
	b, _ := os.ReadFile(r.p.file("state"))
	return strings.TrimSpace(string(b))
}

func TestApplyWritesTheSlotsKernelWithTheUnitsTuning(t *testing.T) {
	old := mkImage(t, "old", tableB)
	newShipped := mkImage(t, "new", nil)
	r := newRig(t, old, newShipped)

	written, err := Apply(r.p)
	if err != nil || !written {
		t.Fatalf("Apply = %v, %v; want a write", written, err)
	}
	want := mkImage(t, "new", tableB)
	if !r.holds(want) {
		t.Fatal("the boot partition does not hold the new kernel with the unit's tuning")
	}
	if got := r.state(); !strings.HasPrefix(got, "trial 3 "+sha(want)) || !strings.HasSuffix(got, sha(newShipped)) {
		t.Fatalf("state %q", got)
	}
	prev, _ := os.ReadFile(r.p.file("prev.img"))
	if !bytes.Equal(prev, old) {
		t.Fatal("prev.img is not the kernel that was there")
	}
	// On trial: nothing more is written, even by another call.
	if w, err := Apply(r.p); w || err != nil {
		t.Fatalf("second Apply = %v, %v", w, err)
	}
}

func TestApplyLeavesAnUpToDateKernel(t *testing.T) {
	r := newRig(t, mkImage(t, "same", tableB), mkImage(t, "same", nil))
	before := r.boot()
	if w, err := Apply(r.p); w || err != nil {
		t.Fatalf("Apply = %v, %v; want nothing to do", w, err)
	}
	if !bytes.Equal(before, r.boot()) || r.state() != "" {
		t.Fatal("an up-to-date kernel was touched")
	}
}

func TestApplyWithoutAShippedKernelDoesNothing(t *testing.T) {
	r := newRig(t, mkImage(t, "old", tableB), nil)
	if w, err := Apply(r.p); w || err != nil {
		t.Fatalf("Apply = %v, %v", w, err)
	}
}

func TestApplyRefuses(t *testing.T) {
	cases := map[string]func(r *rig){
		"a shipped kernel that does not match its sum": func(r *rig) {
			os.WriteFile(r.p.ShippedSum, []byte(strings.Repeat("0", 64)+"\n"), 0o644)
		},
		"a running kernel with an unknown tuning": func(r *rig) {
			os.WriteFile(r.p.Boot, mkImage(r.t, "old", []byte{9, 9, 9, 9}), 0o644)
		},
		"a running kernel with an empty slot": func(r *rig) {
			os.WriteFile(r.p.Boot, mkImage(r.t, "old", nil), 0o644)
		},
		"a running kernel whose header id is wrong": func(r *rig) {
			b := r.boot()
			b[page+3] ^= 1
			os.WriteFile(r.p.Boot, b, 0o644)
		},
		"a shipped kernel with a filled slot": func(r *rig) {
			s := mkImage(r.t, "new", tableB)
			os.WriteFile(r.p.Shipped, s, 0o644)
			os.WriteFile(r.p.ShippedSum, []byte(sha(s)), 0o644)
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, mkImage(t, "old", tableB), mkImage(t, "new", nil))
			spoil(r)
			before := r.boot()
			if w, err := Apply(r.p); w || err == nil {
				t.Fatalf("Apply = %v, %v; want a refusal", w, err)
			}
			if !bytes.Equal(before, r.boot()) {
				t.Fatal("the boot partition was written")
			}
		})
	}
}

func TestTrialSettles(t *testing.T) {
	r := newRig(t, mkImage(t, "old", tableB), mkImage(t, "new", nil))
	if _, err := Apply(r.p); err != nil {
		t.Fatal(err)
	}
	if again, err := Boot(r.p); again || err != nil {
		t.Fatalf("Boot = %v, %v", again, err)
	}
	if !strings.HasPrefix(r.state(), "trial 2 ") {
		t.Fatalf("state %q after a boot", r.state())
	}
	if err := Settled(r.p); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.state(), "good ") {
		t.Fatalf("state %q after settling", r.state())
	}
	// Good and up to date: nothing more.
	if w, err := Apply(r.p); w || err != nil {
		t.Fatalf("Apply after good = %v, %v", w, err)
	}
	if again, err := Boot(r.p); again || err != nil {
		t.Fatalf("Boot after good = %v, %v", again, err)
	}
}

func TestTrialThatNeverSettlesPutsThePreviousKernelBack(t *testing.T) {
	old := mkImage(t, "old", tableB)
	shipped := mkImage(t, "new", nil)
	r := newRig(t, old, shipped)
	if _, err := Apply(r.p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < Tries; i++ {
		if again, err := Boot(r.p); again || err != nil {
			t.Fatalf("boot %d: %v, %v", i, again, err)
		}
	}
	again, err := Boot(r.p)
	if !again || err != nil {
		t.Fatalf("the last boot = %v, %v; want the previous kernel back and a reboot", again, err)
	}
	if !r.holds(old) {
		t.Fatal("the previous kernel is not back")
	}
	if r.state() != "" {
		t.Fatalf("state %q after the restore", r.state())
	}
	// The failed kernel is not written again.
	if w, err := Apply(r.p); w || err != nil {
		t.Fatalf("Apply of a bad kernel = %v, %v", w, err)
	}
	if !r.holds(old) {
		t.Fatal("the bad kernel was written again")
	}
	if !strings.Contains(Status(r.p), "bad: "+sha(shipped)) {
		t.Fatal("status does not name the bad kernel")
	}
}

func TestBootDropsATrialWhoseWriteNeverLanded(t *testing.T) {
	old := mkImage(t, "old", tableB)
	r := newRig(t, old, mkImage(t, "new", nil))
	if _, err := Apply(r.p); err != nil {
		t.Fatal(err)
	}
	// Power went down after the state was written and before the image was (simulated).
	part := r.boot()
	copy(part, old)
	os.WriteFile(r.p.Boot, part, 0o644)
	if again, err := Boot(r.p); again || err != nil {
		t.Fatalf("Boot = %v, %v", again, err)
	}
	if r.state() != "" || !r.holds(old) {
		t.Fatal("the dropped trial changed something")
	}
}

func TestRestoreRefusesADamagedPreviousKernel(t *testing.T) {
	r := newRig(t, mkImage(t, "old", tableB), mkImage(t, "new", nil))
	if _, err := Apply(r.p); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.ReadFile(r.p.file("prev.img"))
	prev[page+5] ^= 1
	os.WriteFile(r.p.file("prev.img"), prev, 0o644)
	if err := r.p.restore(); err == nil {
		t.Fatal("a prev.img that does not match its sum was written")
	}
}

// TestTheInstallersFill checks the fill against the installer's on real images, when the test is
// given them: ATHENA_BOOT (a released image, slot empty), ATHENA_FILLED (what install-athena.py made
// of it for a unit) and ATHENA_STOCK_TABLE_FROM (a boot image holding the same tuning).
func TestTheInstallersFill(t *testing.T) {
	shipped, filled := os.Getenv("ATHENA_BOOT"), os.Getenv("ATHENA_FILLED")
	if shipped == "" || filled == "" {
		t.Skip("ATHENA_BOOT and ATHENA_FILLED not set")
	}
	s, err := os.ReadFile(shipped)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.ReadFile(filled)
	if err != nil {
		t.Fatal(err)
	}
	ship, err := checkShipped(s)
	if err != nil {
		t.Fatalf("the released image: %v", err)
	}
	fi, err := parseImage(f)
	if err != nil || !fi.idOK() {
		t.Fatalf("the installer's image: %v, id ok %v", err, err == nil && fi.idOK())
	}
	table, err := fi.table()
	if err != nil || table == nil {
		t.Fatalf("the installer's tuning: %v", err)
	}
	if _, ok := knownTables[sha(table)]; !ok {
		t.Fatalf("the installer's tuning %s is not a known one", sha(table))
	}
	mine, err := ship.withTable(table)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mine, f) {
		t.Fatal("the fill differs from the installer's")
	}
	empty, err := fi.withTable(nil)
	if err != nil || !bytes.Equal(empty, s) {
		t.Fatal("emptying the installer's image does not give the released one back")
	}
}
