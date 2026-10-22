//go:build !panel

package timezone

import (
	"os/exec"
	"time"

	"golang.org/x/sys/unix"
)

// ntpSynced is whether NTP keeps the clock: ntpd clears the kernel's unsynchronized flag once it has
// it in hand.
func ntpSynced() bool {
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	return err == nil && state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0
}

// setSystemClock sets the system clock, and the RTC from it, as boot does after NTP: in UTC, so the
// next boot starts closer.
func setSystemClock(t time.Time) error {
	tv := unix.NsecToTimeval(t.UnixNano())
	if err := unix.Settimeofday(&tv); err != nil {
		return err
	}
	_ = exec.Command("hwclock", "-w", "-u").Run()
	return nil
}
