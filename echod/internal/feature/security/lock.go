package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The settings lock: a PIN the device asks for before its settings screen opens, so guests and children
// can use the device (the clock, the music, the voice assistant, the call button) without changing it.
// Off until a PIN is set, from Home Assistant (the settings_lock_pin action) or the setup page; the
// "Settings lock" switch shows whether one is set, and turning it off clears it, which is the way back in
// from a forgotten PIN. A right PIN opens the settings for a couple of minutes; wrong ones in a row make
// the device wait longer and longer before it takes another.

const (
	unlockFor   = 2 * time.Minute
	freeTries   = 5 // wrong PINs before the device starts making people wait
	firstWait   = 30 * time.Second
	longestWait = 15 * time.Minute
	pinMin      = 4
	pinMax      = 8
)

var lock struct {
	mu            sync.Mutex
	unlockedUntil time.Time
	fails         int
	blockedUntil  time.Time
}

// ErrPIN is a PIN that is not 4 to 8 digits.
var ErrPIN = errors.New("a PIN is 4 to 8 digits")

// LockSet is whether a PIN is set.
func LockSet() bool { return webPages && config.Get().Security.LockPIN != "" }

// Locked is whether the settings screen asks for the PIN now.
func Locked() bool {
	if !LockSet() {
		return false
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	return time.Now().After(lock.unlockedUntil)
}

// TryPIN checks a PIN typed on the screen. A right one opens the settings for a while; a wrong one counts
// toward the wait, and while the device is making people wait, wait is how much longer.
func TryPIN(pin string) (ok bool, wait time.Duration) {
	stored := config.Get().Security.LockPIN
	if stored == "" {
		return true, 0
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	now := time.Now()
	if now.Before(lock.blockedUntil) {
		return false, lock.blockedUntil.Sub(now)
	}
	if pinMatches(stored, pin) {
		lock.fails, lock.unlockedUntil = 0, now.Add(unlockFor)
		slog.Info("settings lock: opened with the PIN")
		return true, 0
	}
	lock.fails++
	slog.Warn("settings lock: a wrong PIN", "in_a_row", lock.fails)
	if lock.fails >= freeTries {
		d := firstWait << min(lock.fails-freeTries, 10)
		lock.blockedUntil = now.Add(min(d, longestWait))
		return false, lock.blockedUntil.Sub(now)
	}
	return false, 0
}

// Relock locks again now, as the settings screen closes.
func Relock() {
	lock.mu.Lock()
	lock.unlockedUntil = time.Time{}
	lock.mu.Unlock()
}

// SetPIN sets the PIN, or clears it with "".
func (f *Feature) SetPIN(pin string) error {
	pin = strings.TrimSpace(pin)
	saved := ""
	if pin != "" {
		if !validPIN(pin) {
			return ErrPIN
		}
		saved = hashPIN(pin)
	}
	if err := config.Set().Security().LockPIN(saved); err != nil {
		return err
	}
	lock.mu.Lock()
	lock.fails, lock.blockedUntil, lock.unlockedUntil = 0, time.Time{}, time.Time{}
	lock.mu.Unlock()
	f.lockSw.Set(saved != "")
	slog.Info("settings lock", "on", saved != "")
	f.Changed.Emit(struct{}{})
	return nil
}

func validPIN(pin string) bool {
	if len(pin) < pinMin || len(pin) > pinMax {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hashPIN is the PIN as it is kept: a random salt and the SHA-256 of salt and PIN, in hex.
func hashPIN(pin string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := sha256.Sum256(append(append([]byte{}, salt...), pin...))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(sum[:])
}

func pinMatches(stored, pin string) bool {
	s, h, ok := strings.Cut(stored, ":")
	if !ok {
		return false
	}
	salt, err1 := hex.DecodeString(s)
	want, err2 := hex.DecodeString(h)
	if err1 != nil || err2 != nil {
		return false
	}
	sum := sha256.Sum256(append(append([]byte{}, salt...), pin...))
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}

func (f *Feature) buildLock() {
	f.lockSw = &esphome.Switch{
		Base: esphome.Base{ObjectID: "settings_lock", Name: "Settings lock", Icon: "mdi:lock", Category: esphome.CategoryConfig},
		OnCommand: func(on bool) {
			if on {
				// A PIN comes from the action or the setup page; the switch can only show it, or clear it.
				f.lockSw.Set(LockSet())
				if !LockSet() {
					slog.Warn("settings lock: set a PIN with the settings_lock_pin action first")
				}
				return
			}
			if err := f.SetPIN(""); err != nil {
				slog.Error("settings lock: clearing the PIN failed", "err", err)
			}
		},
	}
}

// lockAction sets the PIN from Home Assistant: 4 to 8 digits, or empty to clear it.
func (f *Feature) lockAction() *esphome.Action {
	return &esphome.Action{
		Name: "settings_lock_pin",
		Args: []esphome.Arg{{Name: "pin", Type: esphome.ArgString}},
		Run: func(c esphome.Call) (any, error) {
			if !encrypted() {
				return nil, errors.New("settings_lock_pin: the device has no API encryption key; set one first")
			}
			return nil, f.SetPIN(c.String("pin"))
		},
	}
}
