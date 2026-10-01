package streaming

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// avahiConf announces what the receivers register and nothing of its own: no workstation record and no
// host details. The daemon announces itself (ESPHome, Sendspin) on its own, beside avahi.
const avahiConf = `[server]
use-ipv4=yes
use-ipv6=yes
ratelimit-interval-usec=1000000
ratelimit-burst=1000
[wide-area]
enable-wide-area=no
[publish]
publish-hinfo=no
publish-workstation=no
[reflector]
enable-reflector=no
[rlimits]
`

// superviseAvahi runs avahi-daemon for the receivers to register with, until ctx ends.
func superviseAvahi(ctx context.Context) {
	conf := filepath.Join(runDir, "avahi-daemon.conf")
	if err := os.WriteFile(conf, []byte(avahiConf), 0o644); err != nil {
		slog.Error("streaming: writing avahi's configuration failed", "err", err)
		return
	}
	// Where avahi keeps its pid; /run is cleared at boot.
	_ = os.MkdirAll("/run/avahi-daemon", 0o755)
	ensureBus()
	supervise(ctx, "avahi", avahiPath, []string{"-f", conf, "--no-rlimits"}, nil)
}

// busSocket is the system bus avahi registers on; dbusPath starts one.
var (
	busSocket = "/run/dbus/system_bus_socket"
	dbusPath  = "/usr/bin/dbus-daemon"
)

// ensureBus starts the system bus when nothing has yet. Bluetooth's start brings it up too, but only
// once the radio is there, and avahi will not run without it. Started detached, as Bluetooth's start
// does, so it outlives the daemon; that start sees it running and leaves it be.
func ensureBus() {
	if _, err := os.Stat(busSocket); err == nil {
		return
	}
	if out, err := exec.Command("pidof", "dbus-daemon").Output(); err == nil && len(out) > 0 {
		return
	}
	_ = os.MkdirAll(filepath.Dir(busSocket), 0o755)
	_ = exec.Command("dbus-uuidgen", "--ensure").Run()
	if err := exec.Command(dbusPath, "--system", "--fork", "--nopidfile").Run(); err != nil {
		slog.Warn("streaming: starting the system bus failed; avahi needs it", "err", err)
		return
	}
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(busSocket); err == nil {
			return
		}
	}
}
