package clientproto

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/nacl/box"
)

// A queued call is the phone's one way to ask a computer to do something (design
// 3.5). The outer body the relay sees and the phone signs is {number, at, method,
// target, sealed}; the sealed bytes are this plaintext, anonymously sealed to ONE
// computer's executor key. Every field the outer body repeats must equal the
// inner one: SealAnonymous authenticates no sender, so without that check a
// second paired phone could re-wrap the first phone's sealed bytes under its own
// signature (design 12, T4).

// CallVersion is the version of the call plaintext.
const CallVersion = 1

const (
	// CreateLifetime is how long a create waits for a sleeping computer: a chat that
	// starts a day late is a surprise, not a feature (design 6.1).
	CreateLifetime = time.Hour
	// CallLifetime is how long every other call waits. It is also the hard ceiling for
	// any call: a computer refuses a call whose not_after is further from its at.
	CallLifetime = MaxAge
)

// The methods a phone may queue, and the one that creates (design 6.1). The
// enrollment names the subset an owner allowed (enroll.go); these are the names.
const (
	MethodCreate      = "agents.create"
	MethodSend        = "agents.send"
	MethodStop        = "agents.stop"
	MethodAnswer      = "asks.answer"
	MethodInboxAnswer = "inbox.answer"
	MethodTaskStart   = "tasks.start"
)

// PhoneMethods is every method a phone may be enrolled for, in one place so the
// enrollment, the runner's routing and the phone's own list cannot drift apart.
func PhoneMethods() []string {
	return []string{MethodCreate, MethodSend, MethodStop, MethodAnswer, MethodInboxAnswer, MethodTaskStart}
}

// ErrCall says a call did not open, is malformed, or disagrees with its outer body.
// ErrCallExpired says it is well formed and its time has passed, which a computer
// answers with its own outcome (`expired`) rather than as a bad call.
var (
	ErrCall        = errors.New("clientproto: call does not open")
	ErrCallExpired = errors.New("clientproto: call has expired")
)

// CallPlain is the plaintext of a queued call. Params is the method's own JSON
// object, exactly as the phone wrote it; the computer decodes it with the method's
// allow-list and refuses a key it does not know.
type CallPlain struct {
	V        int             `json:"v"`
	Client   string          `json:"client"`
	Number   int64           `json:"number"`
	At       int64           `json:"at"`
	NotAfter int64           `json:"not_after"`
	Target   string          `json:"target"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
	FaceID   bool            `json:"face_id"`
}

// OuterCall is what the phone signed and the relay stored for the call.
type OuterCall struct {
	Number int64
	At     int64
	Method string
	Target string
}

// SealCall seals the call to one computer's executor box key (from its card).
// r is the ephemeral key source (crypto/rand.Reader outside tests).
func SealCall(p CallPlain, computerBoxPub *[32]byte, r io.Reader) ([]byte, error) {
	if r == nil {
		r = rand.Reader
	}
	raw, err := jsonMarshal(p)
	if err != nil {
		return nil, err
	}
	return box.SealAnonymous(nil, raw, computerBoxPub, r)
}

// OpenCall opens a sealed call with the computer's executor key pair and decodes
// it strictly: a wrong key, a changed byte, an unknown key, a version other than
// one, or trailing data is ErrCall. It does not check the fields: call Check.
func OpenCall(sealed []byte, computerBoxPub, computerBoxPriv *[32]byte) (CallPlain, error) {
	raw, ok := box.OpenAnonymous(nil, sealed, computerBoxPub, computerBoxPriv)
	if !ok {
		return CallPlain{}, ErrCall
	}
	var p CallPlain
	if jsonUnmarshalStrict(raw, &p) != nil || p.V != CallVersion {
		return CallPlain{}, ErrCall
	}
	return p, nil
}

// Check is the computer's rule for an opened call. It must come from the phone
// whose queue it was listed in (client), be addressed to this computer (self),
// repeat the outer number, time, method and target exactly, and give itself a
// lifetime that begins at its own `at` and is no longer than CallLifetime. Only
// then is its time looked at: past not_after is ErrCallExpired, so an expired but
// otherwise good call is told apart from a forged one. now is unix milliseconds.
func (p CallPlain) Check(o OuterCall, client, self string, now int64) error {
	if p.V != CallVersion || p.Client != client || p.Target != self || o.Target != self ||
		p.Number != o.Number || p.At != o.At || p.Method != o.Method || p.Number <= 0 ||
		p.NotAfter <= p.At || p.NotAfter-p.At > CallLifetime.Milliseconds() || p.Method == "" {
		return ErrCall
	}
	if now > p.NotAfter {
		return ErrCallExpired
	}
	return nil
}
