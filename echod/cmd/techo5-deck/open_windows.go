//go:build windows

package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// listApps is the Start menu's shortcuts, for everybody and for this user, by name.
func listApps() map[string]string {
	apps := map[string]string{}
	for _, root := range []string{
		filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
	} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".lnk") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			low := strings.ToLower(name)
			if strings.Contains(low, "uninstall") || strings.Contains(low, "readme") || strings.Contains(low, "help") {
				return nil
			}
			if _, dup := apps[name]; !dup {
				apps[name] = p
			}
			return nil
		})
	}
	return apps
}

func shellOpen(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func openURL(u string) error    { return shellOpen(u) }
func openApp(path string) error { return shellOpen(path) }

// runScript starts a command from the script list and doesn't wait for it, without a console window
// of its own.
func runScript(command string) error {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/d /s /c "` + command + `"`, HideWindow: true}
	return cmd.Start()
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// setStartup starts the agent when this user signs in, or stops doing so.
func setStartup(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("TECHO5 Deck"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("TECHO5 Deck", `"`+exe+`"`)
}
