package filesdoor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// The sentences a refusal carries. They are what a person reads when a take
// fails, so they say what is true in plain words.
const (
	// RemovedLine is said to a device the roster no longer allows, whether it
	// was removed before it asked or while it was fetching.
	RemovedLine = "this computer was removed from your devices"

	couldNotConfirm  = "could not confirm this computer"
	couldNotCheck    = "could not check your devices right now"
	differentVersion = "the two computers run versions of codeaf that cannot move chats between them; update both"
)

// hello is the first message each way. The certificate proves the identity made
// the device, the signature over the tunnel's channel binding proves this
// device holds its key right now, and the door key derived for the certificate's
// device must be the static key the tunnel was made with, so a certificate
// cannot be lent to another machine.
type hello struct {
	V    int           `json:"v"`
	Cert identity.Cert `json:"cert"`
	Sig  []byte        `json:"sig"`
}

// refusal is the door answering and saying no.
type refusal struct {
	Reason string `json:"reason"`
}

func helloFor(dev identity.Dev, tunnel *pair.Tunnel) hello {
	return hello{V: Version, Cert: dev.Cert, Sig: dev.Sign(tunnel.ChannelBinding())}
}

func checkHello(id identity.Identity, tunnel *pair.Tunnel, h hello) error {
	if h.Cert.V != 1 || h.Cert.Verify(id.PublicKey()) != nil {
		return errors.New(couldNotConfirm)
	}
	pub, err := hex.DecodeString(h.Cert.Device)
	if err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(pub), tunnel.ChannelBinding(), h.Sig) {
		return errors.New(couldNotConfirm)
	}
	key, err := DoorKey(id, h.Cert.DeviceID())
	if err != nil || !bytes.Equal(key.PublicKey().Bytes(), tunnel.Peer()) {
		return errors.New(couldNotConfirm)
	}
	return nil
}

func sendJSON(w io.Writer, typ byte, v any) (int, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return writeMessage(w, typ, raw)
}

func sendHello(w io.Writer, tunnel *pair.Tunnel, dev identity.Dev) (int, error) {
	return sendJSON(w, typeHello, helloFor(dev, tunnel))
}

func refuse(w io.Writer, reason string) { _, _ = sendJSON(w, typeRefused, refusal{Reason: reason}) }

// rosterCache refuses stale authority after its bounded lifetime. A refresh
// that fails never extends the previous answer, even during an outage: a door
// that cannot tell whether a device is still allowed stops answering it.
type rosterCache struct {
	fetch func(context.Context) (map[string]bool, error)
	now   func() time.Time
	ttl   time.Duration

	mu      sync.Mutex
	active  map[string]bool
	fetched time.Time
}

func (r *rosterCache) allows(ctx context.Context, device string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if r.active == nil || now.Before(r.fetched) || now.Sub(r.fetched) >= r.ttl {
		rows, err := r.fetch(ctx)
		if err != nil {
			return false, err
		}
		r.active = make(map[string]bool, len(rows))
		for k, v := range rows {
			r.active[k] = v
		}
		r.fetched = now
	}
	return r.active[device], nil
}

// acceptHello reads the first message, which must be a hello from a device of
// this identity that the roster allows, and answers with this device's own.
// Anything else is refused with a sentence and ends the connection before a
// single object is read or sent.
func (d *door) acceptHello(ctx context.Context, in io.Reader, out io.Writer, tunnel *pair.Tunnel) (string, error) {
	typ, body, _, err := readMessage(in, MaxRequestBytes)
	if err != nil {
		return "", err
	}
	var h hello
	reason := ""
	switch {
	case typ != typeHello || json.Unmarshal(body, &h) != nil:
		reason = couldNotConfirm
	case h.V != Version:
		reason = differentVersion
	default:
		if err := checkHello(d.cfg.Identity, tunnel, h); err != nil {
			reason = err.Error()
		} else if allowed, err := d.roster.allows(ctx, h.Cert.DeviceID()); err != nil {
			reason = couldNotCheck
		} else if !allowed {
			reason = RemovedLine
		}
	}
	if reason != "" {
		refuse(out, reason)
		return "", errors.New(reason)
	}
	if _, err := sendHello(out, tunnel, d.cfg.Device); err != nil {
		return "", err
	}
	return h.Cert.DeviceID(), nil
}
