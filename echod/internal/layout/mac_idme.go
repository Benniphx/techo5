//go:build !panel

package layout

import "os"

// readMAC is the address idme recorded at the factory.
func readMAC() ([]byte, error) { return os.ReadFile(MACPath) }
