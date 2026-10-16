package echod

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/update/kernel"
)

// newKernelCmd is the 2nd gen Show 8's kernel update (update/kernel), which etc/techo5/boot.sh runs:
// boot early at every boot, settled and then apply once the daemon has settled on a good slot.
func newKernelCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "kernel",
		Short: "The 2nd gen Show 8's kernel: the one its root filesystem carries, on trial like a slot",
	}
	paths := func() (kernel.Paths, error) {
		if !layout.AthenaMainline() {
			return kernel.Paths{}, errors.New("only the 2nd gen Show 8 on TECHO5's mainline kernel updates its kernel")
		}
		p := kernel.Default
		dev, err := kernel.BootDevice()
		if err != nil {
			return p, err
		}
		p.Boot = dev
		return p, nil
	}
	reboot := func() error {
		syscall.Sync()
		return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "The kernel's trial state and the kernel this slot carries",
		RunE: func(*cobra.Command, []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			fmt.Print(kernel.Status(p))
			return nil
		},
	}
	boot := &cobra.Command{
		Use:   "boot",
		Short: "Take a try for a kernel on trial; put the previous one back when it has none left",
		RunE: func(*cobra.Command, []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			again, err := kernel.Boot(p)
			if err != nil {
				return err
			}
			if again {
				return reboot()
			}
			return nil
		},
	}
	settled := &cobra.Command{
		Use:   "settled",
		Short: "The daemon settled: a kernel on trial is good",
		RunE: func(*cobra.Command, []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			return kernel.Settled(p)
		},
	}
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Write this slot's kernel to the boot partition when it holds another one, and reboot",
		RunE: func(*cobra.Command, []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			written, err := kernel.Apply(p)
			if err != nil {
				return err
			}
			if written {
				kernel.Log("rebooting into the new kernel")
				return reboot()
			}
			return nil
		},
	}
	c.AddCommand(status, boot, settled, apply)
	return c
}
