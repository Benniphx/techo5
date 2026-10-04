//go:build !panel

package api

import "github.com/ygelfand/go-esphome-device/mdns"

// advertiseMDNS is the library's own advertiser, which lists the interfaces itself.
func advertiseMDNS(cfg mdns.Config) (interface{ Close() error }, error) {
	return mdns.Advertise(cfg)
}
