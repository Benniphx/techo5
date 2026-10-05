//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// osascript runs a script and says what macOS said when it refused: most often the missing
// Accessibility permission, which macOS asks for the first time.
func osascript(lang, script string) error {
	out, err := exec.Command("osascript", "-l", lang, "-e", script).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if strings.Contains(msg, "-1743") || strings.Contains(msg, "-25211") || strings.Contains(msg, "not allowed") {
		return errors.New("macOS hasn't allowed it to press keys: System Settings > Privacy & Security > Accessibility, turn on the agent (or the Terminal it runs in)")
	}
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("osascript: %s", msg)
}

// canPressKeys asks System Events whether this program may control the computer.
func canPressKeys() bool {
	out, err := exec.Command("osascript", "-e", `tell application "System Events" to get UI elements enabled`).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func pressCombo(c combo) error {
	lang, script, err := macKeyScript(c)
	if err != nil {
		return err
	}
	return osascript(lang, script)
}

func typeText(s string) error {
	if s == "" {
		return errors.New("nothing to type")
	}
	return osascript("AppleScript", macTypeScript(s))
}

// listApps is the apps in the Applications folders, by name.
func listApps() map[string]string {
	home, _ := os.UserHomeDir()
	apps := map[string]string{}
	for _, dir := range []string{"/Applications", "/Applications/Utilities", "/System/Applications",
		"/System/Applications/Utilities", filepath.Join(home, "Applications")} {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.app"))
		for _, p := range matches {
			name := strings.TrimSuffix(filepath.Base(p), ".app")
			if _, dup := apps[name]; !dup && !skipApp(name) {
				apps[name] = p
			}
		}
	}
	return apps
}

func openApp(path string) error { return exec.Command("open", "-a", path).Start() }
func openURL(u string) error    { return exec.Command("open", u).Start() }

func runScript(command string) error { return exec.Command("/bin/sh", "-c", command).Start() }

const launchLabel = "org.techo5.deck"

// setStartup writes a LaunchAgent that starts the agent at sign-in, or removes it.
func setStartup(on bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	p := filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
 <key>Label</key><string>%s</string>
 <key>ProgramArguments</key><array><string>%s</string></array>
 <key>RunAtLoad</key><true/>
 <key>KeepAlive</key><true/>
</dict></plist>
`, launchLabel, xmlEscape(exe))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
