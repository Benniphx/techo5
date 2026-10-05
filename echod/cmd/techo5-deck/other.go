//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"runtime"
)

// macOS and Linux come next (docs/deck-plan.md, step 3). Until then the agent runs there, opens
// websites and runs its scripts, but presses no keys and lists no apps.

var errNotYet = errors.New("pressing keys isn't on " + runtime.GOOS + " yet")

func canPressKeys() bool          { return false }
func pressCombo(combo) error      { return errNotYet }
func typeText(string) error       { return errNotYet }
func listApps() map[string]string { return map[string]string{} }
func openApp(string) error        { return errNotYet }
func setStartup(bool) error       { return errors.New("start at sign-in isn't on " + runtime.GOOS + " yet") }

func openURL(u string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, u).Start()
}

func runScript(command string) error { return exec.Command("/bin/sh", "-c", command).Start() }
