package dlna

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/web"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/metrics"
)

// SSDP: how controllers find the renderer. A controller asks the network who is there (M-SEARCH, to
// the multicast group), and the device answers it directly with where its description is; the device
// also says it is here on its own (NOTIFY alive) when it starts and every so often after, and goodbye
// (NOTIFY byebye) when it goes off.

const (
	ssdpAddr   = "239.255.255.250:1900"
	maxAge     = 1800
	aliveEvery = 15 * time.Minute

	deviceType = "urn:schemas-upnp-org:device:MediaRenderer:1"
	avtType    = "urn:schemas-upnp-org:service:AVTransport:1"
	rcType     = "urn:schemas-upnp-org:service:RenderingControl:1"
	cmType     = "urn:schemas-upnp-org:service:ConnectionManager:1"
	serverName = "Linux UPnP/1.0 TECHO5/1.0"
)

// targets are what the device is, as SSDP names it: each is announced, and each is an answer to a search.
func targets() []string {
	return []string{"upnp:rootdevice", udn(), deviceType, avtType, rcType, cmType}
}

func usn(target string) string {
	id := udn()
	if target == id {
		return id
	}
	return id + "::" + target
}

// announce answers searches and says the device is here, until ctx ends; then it says goodbye.
func (f *Feature) announce(ctx context.Context) {
	group, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return
	}
	listen, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		slog.Warn("dlna: listening for searches failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { listen.Close() })
	defer stop()
	slog.Info("dlna: renderer on", "name", friendlyName())

	go func() {
		for {
			notify(group, "ssdp:alive")
			select {
			case <-ctx.Done():
				notify(group, "ssdp:byebye")
				return
			case <-time.After(aliveEvery):
			}
		}
	}()

	buf := make([]byte, 2048)
	for {
		n, from, err := listen.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("dlna: reading searches failed", "err", err)
			}
			return
		}
		st, mx, ok := parseSearch(buf[:n])
		if !ok {
			continue
		}
		var answer []string
		for _, t := range targets() {
			if st == "ssdp:all" || st == t {
				answer = append(answer, t)
			}
		}
		if len(answer) == 0 {
			continue
		}
		go reply(ctx, from, answer, mx)
	}
}

// parseSearch reads an M-SEARCH: what is being looked for (ST) and how long answers may be spread over (MX).
func parseSearch(b []byte) (st string, mx int, ok bool) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(b)))
	if err != nil || req.Method != "M-SEARCH" {
		return "", 0, false
	}
	if strings.Trim(req.Header.Get("MAN"), `"`) != "ssdp:discover" {
		return "", 0, false
	}
	mx, _ = strconv.Atoi(req.Header.Get("MX"))
	return strings.TrimSpace(req.Header.Get("ST")), min(max(mx, 1), 5), true
}

// reply answers a search, after a moment up to mx seconds, as SSDP asks so a network of devices does
// not all answer at once.
func reply(ctx context.Context, to *net.UDPAddr, answer []string, mx int) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(rand.Int64N(int64(mx) * int64(time.Second) / 2))):
	}
	conn, err := net.DialUDP("udp4", nil, to)
	if err != nil {
		return
	}
	defer conn.Close()
	host := conn.LocalAddr().(*net.UDPAddr).IP.String()
	for _, t := range answer {
		msg := "HTTP/1.1 200 OK\r\n" +
			fmt.Sprintf("CACHE-CONTROL: max-age=%d\r\n", maxAge) +
			"DATE: " + time.Now().UTC().Format(http.TimeFormat) + "\r\n" +
			"EXT:\r\n" +
			"LOCATION: " + location(host) + "\r\n" +
			"SERVER: " + serverName + "\r\n" +
			"ST: " + t + "\r\n" +
			"USN: " + usn(t) + "\r\n\r\n"
		_, _ = conn.Write([]byte(msg))
	}
}

// notify announces every target on every address the device has.
func notify(group *net.UDPAddr, nts string) {
	for _, ip := range metrics.Addresses() {
		if ip.To4() == nil {
			continue
		}
		conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: ip}, group)
		if err != nil {
			continue
		}
		for _, t := range targets() {
			msg := "NOTIFY * HTTP/1.1\r\n" +
				"HOST: " + ssdpAddr + "\r\n" +
				fmt.Sprintf("CACHE-CONTROL: max-age=%d\r\n", maxAge) +
				"LOCATION: " + location(ip.String()) + "\r\n" +
				"NT: " + t + "\r\n" +
				"NTS: " + nts + "\r\n" +
				"SERVER: " + serverName + "\r\n" +
				"USN: " + usn(t) + "\r\n\r\n"
			_, _ = conn.Write([]byte(msg))
		}
		conn.Close()
	}
}

func location(host string) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(web.Port)) + pathPrefix + "device.xml"
}
