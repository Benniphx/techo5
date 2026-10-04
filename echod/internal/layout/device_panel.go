//go:build panel

package layout

import (
	"os/exec"
	"strings"
)

// TECHO5 Panel: an Android tablet or a Meta Portal, with echod running inside an ordinary app rather
// than as an init service. Nothing is rooted, so everything the daemon keeps lives in the app's own
// files directory, and the app hands it the microphone over a local socket (see mic/capture_panel.go).
const (
	// PackageName is the Android app echod runs inside. Its files directory is the only place the
	// daemon may write.
	PackageName = "app.techo5.panel"

	// AndroidDir is where an in-place update would put the binary. The app ships the daemon in its
	// own APK and updates come with the app, so nothing is ever written here.
	AndroidDir = StateDir + "/bin"
	BinaryName = "techo5"
	StateDir   = "/data/user/0/" + PackageName + "/files"

	Service     = AndroidDir + "/" + BinaryName
	ServiceName = "techo5"

	StockLabel = "u:object_r:system_file:s0"

	StartAnimation = ""
	StopAnimation  = ""

	FirewallHook = ""

	// LogTag is the daemon's logcat tag: `adb logcat -s techo5`.
	LogTag = "techo5"

	Manufacturer = "TECHO5"

	// DefaultName is the fallback display name when a device has none recorded.
	DefaultName = "TECHO5 Panel"

	LogPath = StateDir + "/techo5.log"
	BootLog = StateDir + "/boot.log"
)

var AnimationScripts = []string{}

// Board is the Android device name and Model what the tablet calls itself, read once at start from
// the system properties an app may read.
var (
	Board = getprop("ro.product.device", "android")
	Model = "TECHO5 Panel (" + getprop("ro.product.model", "Android") + ")"
)

// Checkers and Crown are Echo Show boards; a panel is neither.
func Checkers() bool { return false }

func Crown() bool { return false }

func getprop(name, fallback string) string {
	out, err := exec.Command("/system/bin/getprop", name).Output()
	if v := strings.TrimSpace(string(out)); err == nil && v != "" {
		return v
	}
	return fallback
}
