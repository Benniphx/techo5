package nearest

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How a claim proves it comes from the house: an HMAC keyed from the house word, over when it was sent, a
// nonce, the device it is for and the claim (which carries its turn), the way announcements are signed
// (feature/announce) but with a key of its own, so a signature of one can never pass for the other.
// Without it anything on the network could stop every device in the house answering, by claiming every
// wake word louder than all of them. Naming the device it is for keeps a claim seen on its way to one
// device from being sent on to another.

const (
	authHeader = "X-Techo5-Nearest"
	authLabel  = "techo5-nearest hmac"

	// skew is how far a sender's clock may be from this one's. The devices keep time from the network;
	// a claim is only of use for a fraction of a second, so a replay is what this is about.
	skew = 5 * time.Second

	// noncesKept bounds the nonces remembered. A house makes a claim per device per wake word.
	noncesKept = 1024

	// maxBody is more than any claim.
	maxBody = 1 << 12
)

func key(word string) []byte {
	sum := sha256.Sum256([]byte(authLabel + ":" + word))
	return sum[:]
}

// mac is the signature over a claim to the device called to. The name is compared without regard to
// case, as the devices find each other.
func mac(word, to, ts, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	m := hmac.New(sha256.New, key(word))
	m.Write([]byte("v1\n" + ts + "\n" + nonce + "\n" + strings.ToLower(to) + "\n" + hex.EncodeToString(sum[:]) + "\n"))
	return m.Sum(nil)
}

// sign is the header for a claim about to be sent to the device called to.
func sign(word, to string, body []byte, now time.Time) string {
	var n [12]byte
	_, _ = rand.Read(n[:])
	ts, nonce := strconv.FormatInt(now.Unix(), 10), hex.EncodeToString(n[:])
	return "v1 " + ts + " " + nonce + " " + hex.EncodeToString(mac(word, to, ts, nonce, body))
}

var (
	errUnsigned = errors.New("not signed by this house")
	errStale    = errors.New("signed at a time too far from this device's clock")
	errReplayed = errors.New("a claim already taken")
)

// verify checks a claim's header against the house word, as a claim for the device called me.
func verify(word, me, header string, body []byte, now time.Time) error {
	if word == "" {
		return errUnsigned
	}
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != "v1" {
		return errUnsigned
	}
	got, err := hex.DecodeString(parts[3])
	if err != nil || !hmac.Equal(got, mac(word, me, parts[1], parts[2], body)) {
		return errUnsigned
	}
	sec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return errUnsigned
	}
	if d := now.Sub(time.Unix(sec, 0)); d > skew || d < -skew {
		return errStale
	}
	if !fresh(parts[2], now) {
		return errReplayed
	}
	return nil
}

var nonces struct {
	sync.Mutex
	seen map[string]time.Time
}

// fresh is whether a nonce has not been seen within the skew, remembering it.
func fresh(nonce string, now time.Time) bool {
	nonces.Lock()
	defer nonces.Unlock()
	if nonces.seen == nil {
		nonces.seen = map[string]time.Time{}
	}
	if _, ok := nonces.seen[nonce]; ok {
		return false
	}
	if len(nonces.seen) >= noncesKept {
		for k, at := range nonces.seen {
			if now.Sub(at) > 2*skew {
				delete(nonces.seen, k)
			}
		}
		if len(nonces.seen) >= noncesKept {
			return false
		}
	}
	nonces.seen[nonce] = now
	return true
}

func readBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, errors.New("too long for a claim")
	}
	return b, nil
}

// answeredHeader proves a refusal comes from a device of this house. Devices are found over mDNS, which
// anything on the network can answer, so a refusal saying "answered here already" is only believed with
// this on it: otherwise a host that knows nothing could stand every device in the house down.
const answeredHeader = "X-Techo5-Nearest-Answered"

// answeredMAC is that proof: over the refused claim's nonce, the device that sent it and the device that
// answered.
func answeredMAC(word, nonce, from, by string) string {
	m := hmac.New(sha256.New, key(word))
	m.Write([]byte("answered\n" + nonce + "\n" + strings.ToLower(from) + "\n" + strings.ToLower(by) + "\n"))
	return hex.EncodeToString(m.Sum(nil))
}

// nonceOf is the nonce in a claim's signature header, empty for none.
func nonceOf(header string) string {
	if parts := strings.Fields(header); len(parts) == 4 {
		return parts[2]
	}
	return ""
}

// answeredBy is whether a refusal's proof checks out, for a claim signed with header, sent by from to by.
func answeredBy(word, header, proof, from, by string) bool {
	nonce := nonceOf(header)
	if word == "" || nonce == "" || proof == "" {
		return false
	}
	return hmac.Equal([]byte(proof), []byte(answeredMAC(word, nonce, from, by)))
}
