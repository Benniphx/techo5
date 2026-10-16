package kernel

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// An athena boot image is an Android boot image (header version 1) as tools/linux/athena/kernel's
// pack.py makes it: the kernel with the device tree after it, and a small ramdisk of firmware. The
// device tree holds the amp tuning slot (tools/linux/athena/kernel/dsp_slot.py). The released image
// carries the slot empty; the installer fills it with the unit's own tuning and re-signs the header.
// The code here is the installer's (tools/install-athena.py, BootImage, fill_slot, read_slot), so a
// kernel this writes is the one the installer would have.

const (
	slotSize = 8192
	slotHead = 44
	slotMax  = slotSize - slotHead
	headerID = 576 // the 32-byte id in the header: SHA-1 over the parts, zero padded
)

var slotMagic = []byte("T5DSP" + "CF1")

// knownTables are the amp tunings the installer accepts, by the sha256 of the table (install-athena.py,
// KNOWN_AMP_TABLES). A slot holding anything else is not written into a new kernel.
var knownTables = map[string]string{
	"8c14a75c68e77d071cfc512f2fbeb002351eb2f26b0ee945bd94e87b10c45c49": "Fire OS 7080, table A",
	"5f3f4ddbcb081b6ac49a361c529ef99fcb010cee0100ae37088d831601ba9248": "Fire OS 7080, table B",
}

type image struct {
	data                              []byte
	kernelSize, ramdiskSize, secondSz uint32
	page, version                     uint32
	kernelAt, ramdiskAt, secondAt     int
	dtbo, dtb                         []byte
	extent                            int // where the image ends, page aligned: the bytes LK reads
}

func pad(n, page int) int { return (n + page - 1) / page * page }

// parseImage reads an Android boot image header, version 0 to 2.
func parseImage(data []byte) (*image, error) {
	if len(data) < 1652 || !bytes.Equal(data[:8], []byte("ANDROID!")) {
		return nil, errors.New("not an Android boot image")
	}
	u := func(at int) uint32 { return binary.LittleEndian.Uint32(data[at:]) }
	im := &image{data: data, kernelSize: u(8), ramdiskSize: u(16), secondSz: u(24), page: u(36), version: u(40)}
	switch im.page {
	case 2048, 4096, 8192, 16384:
	default:
		return nil, fmt.Errorf("page size %d", im.page)
	}
	if im.version > 2 {
		return nil, fmt.Errorf("header version %d", im.version)
	}
	p := int(im.page)
	im.kernelAt = p
	im.ramdiskAt = im.kernelAt + pad(int(im.kernelSize), p)
	im.secondAt = im.ramdiskAt + pad(int(im.ramdiskSize), p)
	end := im.secondAt + int(im.secondSz)
	if end > len(data) {
		return nil, fmt.Errorf("%d bytes, shorter than its header says (%d)", len(data), end)
	}
	im.extent = im.secondAt + pad(int(im.secondSz), p)
	if im.version >= 1 {
		size := int(u(1632))
		off := int(binary.LittleEndian.Uint64(data[1636:]))
		if size > 0 {
			if off < 0 || off+size > len(data) {
				return nil, errors.New("ends inside its recovery dtbo")
			}
			im.dtbo = data[off : off+size]
			im.extent = max(im.extent, pad(off+size, p))
		}
	}
	if im.version >= 2 {
		size := int(u(1648))
		at := im.secondAt + pad(int(im.secondSz), p) + pad(len(im.dtbo), p)
		if at+size > len(data) {
			return nil, errors.New("ends inside its dtb")
		}
		im.dtb = data[at : at+size]
		im.extent = max(im.extent, pad(at+size, p))
	}
	if im.extent > len(data) {
		im.extent = len(data)
	}
	return im, nil
}

func (im *image) part(at int, size uint32) []byte { return im.data[at : at+int(size)] }

// computedID is the header's id as mkbootimg (and pack.py) make it.
func (im *image) computedID() []byte {
	h := sha1.New()
	parts := [][]byte{im.part(im.kernelAt, im.kernelSize), im.part(im.ramdiskAt, im.ramdiskSize), im.part(im.secondAt, im.secondSz)}
	if im.version >= 1 {
		parts = append(parts, im.dtbo)
	}
	if im.version >= 2 {
		parts = append(parts, im.dtb)
	}
	var n [4]byte
	for _, b := range parts {
		h.Write(b)
		binary.LittleEndian.PutUint32(n[:], uint32(len(b)))
		h.Write(n[:])
	}
	id := make([]byte, 32)
	copy(id, h.Sum(nil))
	return id
}

func (im *image) idOK() bool { return bytes.Equal(im.data[headerID:headerID+32], im.computedID()) }

// slotAt is where the tuning slot is: the magic exactly once, the whole slot inside the parts LK loads.
func (im *image) slotAt() (int, error) {
	if n := bytes.Count(im.data[:im.extent], slotMagic); n != 1 {
		return 0, fmt.Errorf("%d amp tuning slots, not one", n)
	}
	at := bytes.Index(im.data, slotMagic)
	if at < im.kernelAt || at+slotSize > im.secondAt+int(im.secondSz) {
		return 0, errors.New("the amp tuning slot is not inside the kernel, ramdisk or second stage")
	}
	return at, nil
}

// table is the tuning in the slot, checked against the slot's own sha256; nil when the slot is empty.
func (im *image) table() ([]byte, error) {
	at, err := im.slotAt()
	if err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint32(im.data[at+8:]))
	if n == 0 {
		return nil, nil
	}
	if n > slotMax || n%2 != 0 {
		return nil, fmt.Errorf("amp tuning slot length %d", n)
	}
	t := im.data[at+slotHead : at+slotHead+n]
	if sum := sha256.Sum256(t); !bytes.Equal(sum[:], im.data[at+12:at+slotHead]) {
		return nil, errors.New("the amp tuning in the slot does not match its own SHA-256")
	}
	return t, nil
}

// withTable is a copy of the image's first extent bytes with table in its slot (nil: emptied) and the
// header id recomputed; the same size.
func (im *image) withTable(table []byte) ([]byte, error) {
	if len(table) > slotMax || len(table)%2 != 0 {
		return nil, fmt.Errorf("amp tuning of %d bytes", len(table))
	}
	at, err := im.slotAt()
	if err != nil {
		return nil, err
	}
	out := bytes.Clone(im.data[:im.extent])
	clear(out[at+8 : at+slotSize])
	if len(table) > 0 {
		binary.LittleEndian.PutUint32(out[at+8:], uint32(len(table)))
		sum := sha256.Sum256(table)
		copy(out[at+12:], sum[:])
		copy(out[at+slotHead:], table)
	}
	o, err := parseImage(out)
	if err != nil {
		return nil, err
	}
	copy(out[headerID:], o.computedID())
	return out, nil
}

// checkShipped: the kernel a slot carries, as released: a valid image, its id right, its slot empty.
func checkShipped(data []byte) (*image, error) {
	im, err := parseImage(data)
	if err != nil {
		return nil, err
	}
	if !im.idOK() {
		return nil, errors.New("its header id does not match its contents")
	}
	t, err := im.table()
	if err != nil {
		return nil, err
	}
	if t != nil {
		return nil, errors.New("its amp tuning slot is already filled")
	}
	if im.extent != len(data) {
		return nil, fmt.Errorf("%d bytes after the image's end", len(data)-im.extent)
	}
	return im, nil
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
