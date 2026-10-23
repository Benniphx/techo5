package speaker

import (
	"bytes"
	"testing"
)

// A 16-bit sample goes to the top of the 32: full scale stays full scale and the sign is kept.
func TestWidenS16KeepsTheScale(t *testing.T) {
	in := []byte{0x34, 0x12, 0x00, 0x80, 0xff, 0x7f}
	want := []byte{0, 0, 0x34, 0x12, 0, 0, 0x00, 0x80, 0, 0, 0xff, 0x7f}
	if got := widenS16(in, nil); !bytes.Equal(got, want) {
		t.Fatalf("widenS16 = % x, want % x", got, want)
	}
}
