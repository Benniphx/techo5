//go:build !dot && !spot

package all

// notOnThisDevice is empty on the Echo Show 5: registered is its whole list.
var notOnThisDevice []string

// deviceSpecific is empty too: the Show has no entity of its own beyond the shared list.
var deviceSpecific []string
