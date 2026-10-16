//go:build !linux

package kernel

import (
	"errors"
	"os"
)

func dropCache(*os.File) {}

// BootDevice exists only on the device.
func BootDevice() (string, error) { return "", errors.New("no boot partition here") }
