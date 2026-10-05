// techo5-deck is the TECHO5 Deck agent: run on a computer, it lets a paired Echo Show's deck buttons
// press shortcuts and media keys, type text, open apps and websites, and run the scripts the
// computer's owner lists, over the local network (docs/deck.md).
//
// It does only what a request names, and runs only scripts from its own list, which only somebody at
// this computer can edit: a paired Show cannot run a command nobody here put there.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/zeroconf/v2"

	"github.com/HuskerMinion/techo5/echod/internal/lib/deckwire"
)

// version is set at build time.
var version = "dev"

func main() {
	port := flag.Int("port", deckwire.Port, "the port to listen on")
	name := flag.String("name", "", "the name the Show lists this computer by (default: the computer's name)")
	dir := flag.String("dir", "", "where the key and the script list are kept (default: your settings folder)")
	newKey := flag.Bool("new-key", false, "make a new pairing key; every paired Show has to be paired again")
	startup := flag.String("startup", "", `"on" to start when you sign in, "off" to stop that`)
	flag.Parse()

	if *startup != "" {
		if err := setStartup(*startup == "on"); err != nil {
			log.Fatalf("start at sign-in: %v", err)
		}
		fmt.Printf("Start at sign-in: %s.\n", *startup)
		return
	}

	home, err := agentDir(*dir)
	if err != nil {
		log.Fatal(err)
	}
	key, err := loadKey(filepath.Join(home, "key"), *newKey)
	if err != nil {
		log.Fatal(err)
	}
	scripts := filepath.Join(home, "scripts.txt")
	if err := ensureScriptsFile(scripts); err != nil {
		log.Fatal(err)
	}
	if *name == "" {
		*name, _ = os.Hostname()
	}

	a := &agent{name: *name, scriptsPath: scripts}
	a.refresh()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("listening on port %d: %v (is another agent running?)", *port, err)
	}
	if srv, err := zeroconf.Register(*name, deckwire.Service, "local.", *port, []string{"v=1", "os=" + runtime.GOOS}, nil); err == nil {
		defer srv.Shutdown()
	} else {
		log.Printf("not advertised on the network (%v); add this computer on the Show by its address", err)
	}

	fmt.Printf(`TECHO5 Deck agent %s

  Computer:     %s
  Pairing key:  %s
  Listening:    port %d, your local network only
  Scripts:      %s (%d)
  Keys:         %s

On the Show's setup page, Screen & Photos -> Deck -> Computers: pick this computer and type the key.
Leave this window open while you use the deck.

`, version, *name, key, *port, scripts, len(a.hello().Scripts), keysNote())

	var sem = make(chan struct{}, 8)
	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		if !local(c.RemoteAddr()) {
			log.Printf("refused %s: not on the local network", c.RemoteAddr())
			c.Close()
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			c.Close()
			continue
		}
		go func() {
			defer func() { <-sem }()
			a.serve(c, key)
		}()
	}
}

// agentDir is where the key and the script list live, made if missing.
func agentDir(dir string) (string, error) {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "TECHO5 Deck")
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// loadKey reads the pairing key, making one the first time (or when asked for a new one).
func loadKey(path string, fresh bool) (string, error) {
	if !fresh {
		if b, err := os.ReadFile(path); err == nil {
			if k := strings.TrimSpace(string(b)); len(deckwire.NormalizeKey(k)) >= 16 {
				return k, nil
			}
		}
	}
	k, err := deckwire.NewKey()
	if err != nil {
		return "", err
	}
	return k, os.WriteFile(path, []byte(k+"\n"), 0o600)
}

// local is whether a connection comes from this computer or the local network: private, link-local
// and loopback addresses. The agent is not for anything farther away.
func local(a net.Addr) bool {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return false
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return false
	}
	ip = ip.Unmap()
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

type agent struct {
	name        string
	scriptsPath string

	mu      sync.Mutex
	scripts []script
	apps    map[string]string // name -> what opens it
	appList []string
}

func (a *agent) refresh() {
	scripts, err := readScripts(a.scriptsPath)
	if err != nil {
		log.Printf("reading %s: %v", a.scriptsPath, err)
	}
	apps := listApps()
	names := make([]string, 0, len(apps))
	for n := range apps {
		names = append(names, n)
	}
	sortFold(names)
	if len(names) > deckwire.MaxNames {
		names = names[:deckwire.MaxNames]
	}
	a.mu.Lock()
	a.scripts, a.apps, a.appList = scripts, apps, names
	a.mu.Unlock()
}

func (a *agent) hello() deckwire.Hello {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := deckwire.Hello{Name: a.name, OS: runtime.GOOS, Version: version, Keys: canPressKeys(), Apps: a.appList}
	for _, s := range a.scripts {
		h.Scripts = append(h.Scripts, s.name)
	}
	return h
}

// serve answers one Show's requests until it hangs up. Requests are few and far between (a finger on
// a button), so a Show sending them faster than a few a second is held back.
func (a *agent) serve(c net.Conn, key string) {
	defer c.Close()
	conn, err := deckwire.Accept(c, key)
	if err != nil {
		log.Printf("%s: %v", c.RemoteAddr(), err)
		return
	}
	from := hostOf(c.RemoteAddr())
	last := time.Time{}
	for {
		var req deckwire.Request
		if err := conn.Receive(&req, 2*time.Minute); err != nil {
			return
		}
		if wait := 100*time.Millisecond - time.Since(last); wait > 0 {
			time.Sleep(wait)
		}
		last = time.Now()
		reply := a.do(req)
		if req.Op != deckwire.OpHello {
			// Typed text can be anything, a password included: the log says only how much.
			what := req.Arg
			if req.Op == deckwire.OpType {
				what = fmt.Sprintf("(%d characters)", len([]rune(req.Arg)))
			}
			if reply.OK {
				log.Printf("%s: %s %s", from, req.Op, what)
			} else {
				log.Printf("%s: %s %s: %s", from, req.Op, what, reply.Error)
			}
		}
		if conn.Send(reply) != nil {
			return
		}
	}
}

func hostOf(a net.Addr) string {
	h, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return h
}

func (a *agent) do(req deckwire.Request) deckwire.Reply {
	if len(req.Arg) > deckwire.MaxArg {
		return deckwire.Reply{Error: "too long"}
	}
	fail := func(err error) deckwire.Reply {
		if err != nil {
			return deckwire.Reply{Error: err.Error()}
		}
		return deckwire.Reply{OK: true}
	}
	switch req.Op {
	case deckwire.OpHello:
		h := a.hello()
		return deckwire.Reply{OK: true, Hello: &h}
	case deckwire.OpRefresh:
		a.refresh()
		h := a.hello()
		return deckwire.Reply{OK: true, Hello: &h}
	case deckwire.OpKeys:
		combo, err := parseCombo(req.Arg)
		if err != nil {
			return fail(err)
		}
		return fail(pressCombo(combo))
	case deckwire.OpType:
		return fail(typeText(req.Arg))
	case deckwire.OpOpen:
		if isWebAddress(req.Arg) {
			return fail(openURL(req.Arg))
		}
		a.mu.Lock()
		target, ok := a.apps[req.Arg]
		a.mu.Unlock()
		if !ok {
			return fail(fmt.Errorf("no app called %q here", req.Arg))
		}
		return fail(openApp(target))
	case deckwire.OpRun:
		a.mu.Lock()
		var cmd string
		for _, s := range a.scripts {
			if s.name == req.Arg {
				cmd = s.command
			}
		}
		a.mu.Unlock()
		if cmd == "" {
			return fail(fmt.Errorf("no script called %q in %s", req.Arg, filepath.Base(a.scriptsPath)))
		}
		return fail(runScript(cmd))
	}
	return deckwire.Reply{Error: "not something this agent does"}
}

// isWebAddress is whether s is an http or https address, the only kind the agent opens as one.
func isWebAddress(s string) bool {
	l := strings.ToLower(s)
	return (strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")) && !strings.ContainsAny(s, " \t\r\n\"")
}

func keysNote() string {
	if canPressKeys() {
		return "can press keys and type"
	}
	switch runtime.GOOS {
	case "darwin":
		return "not allowed yet: System Settings > Privacy & Security > Accessibility, turn on this agent (or the Terminal it runs in), then start it again"
	case "linux":
		return "not allowed to make a keyboard yet: see the Linux part of the Deck guide (the input group)"
	}
	return "can't press keys on this system"
}
