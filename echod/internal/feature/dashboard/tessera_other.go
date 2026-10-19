//go:build !dot && (spot || panel)

package dashboard

// hasTessera is false on the Spot, whose round screen Tessera has no layout for, and on a panel.
const hasTessera = false
