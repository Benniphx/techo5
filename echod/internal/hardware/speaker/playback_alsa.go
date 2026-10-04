//go:build !panel

package speaker

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/lib/alsa"
)

// openDevice takes the mixer, the DRAM hold where the board needs one, the routes the card needs
// before it opens (openSequence), and the playback stream at the card's own rate (hwRate).
func openDevice() (playback, *alsa.Mixer, *os.File, error) {
	m, err := alsa.OpenMixer(Card)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("speaker: opening mixer: %w", err)
	}

	// Before the playback device, so its driver sees the AFE in use and takes the DRAM ring.
	var hold *os.File
	if DRAMHold != "" {
		hold, err = os.OpenFile(DRAMHold, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			_ = m.Close()
			return nil, nil, nil, fmt.Errorf("speaker: holding %s: %w", DRAMHold, err)
		}
	}

	// A card whose front end reaches the codec only through mixer routes will not open the device
	// before they are on (openSequence).
	applyTo(m, openSequence)

	// The ring is the same length of time at whatever rate the card runs (hwRate).
	pb, err := alsa.OpenPlayback(Card, PlaybackDevice, alsa.Config{
		Channels:   Channels,
		Rate:       hwRate,
		Format:     alsa.FormatS16_LE,
		Bits:       Bits,
		PeriodSize: period * hwRate / Rate,
		Periods:    periods,
		StartFull:  startFull,
	})
	if err != nil {
		_ = m.Close()
		if hold != nil {
			_ = hold.Close()
		}
		return nil, nil, nil, fmt.Errorf("speaker: opening playback: %w", err)
	}
	if startFull && !pb.StartsFull() {
		slog.Warn("the kernel would not hold playback until its ring is full; it starts on the first period")
	}
	return pb, m, hold, nil
}
