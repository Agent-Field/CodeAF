package clientrun

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/identity"
	"golang.org/x/crypto/curve25519"
)

// Pairing is a locally approved certificate and independent encryption key.
// Reads name supported snapshots; Methods is the queued-call permission list.
type Pairing struct {
	Cert    identity.Cert `json:"cert"`
	BoxKey  []byte        `json:"box_key"`
	Methods []string      `json:"methods"`
	Reads   []string      `json:"reads"`
	Name    string        `json:"name"`
}

// Client is the Settings view of a local pairing. No secret is returned.
type Client struct {
	Pairing
	ID     string `json:"id"`
	Paused bool   `json:"paused"`
}

// Grant contains only public connection information. Registering the client
// at the relay always precedes releasing this certificate to it.
type Grant struct {
	Cert     identity.Cert `json:"cert"`
	Identity []byte        `json:"identity"`
	Relay    string        `json:"relay"`
	Writer   string        `json:"writer"`
	BoxKey   []byte        `json:"box_key"`
}

// Pair enrolls a scoped client and persists its public encryption key locally.
func (r *Runner) Pair(ctx context.Context, p Pairing) (Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkPairing(p); err != nil {
		return Grant{}, err
	}
	if _, ok := r.state.Clients[p.Cert.DeviceID()]; ok {
		return Grant{}, errors.New("client already paired")
	}
	if err := r.boxKey(); err != nil {
		return Grant{}, err
	}
	// Keep the key before registration: a crash must never strand queued calls
	// encrypted to a grant whose private half did not reach the disk.
	if err := r.save(); err != nil {
		return Grant{}, err
	}
	body := struct {
		Cert    identity.Cert `json:"cert"`
		Methods []string      `json:"methods"`
		Name    string        `json:"name"`
	}{p.Cert, p.Methods, ""}
	if err := r.request(ctx, "PUT", clientPath(p.Cert.DeviceID()), body, nil); err != nil {
		return Grant{}, err
	}
	c := Client{Pairing: p, ID: p.Cert.DeviceID()}
	r.state.Clients[c.ID] = c
	if err := r.save(); err != nil {
		delete(r.state.Clients, c.ID)
		return Grant{}, err
	}
	pub, err := curve25519.X25519(r.state.Secret, curve25519.Basepoint)
	if err != nil {
		return Grant{}, err
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
	return Grant{p.Cert, r.cfg.Signer.IdentityKey(), r.cfg.Relay, r.cfg.Signer.Cert().DeviceID(), pub}, nil
}

func (r *Runner) checkPairing(p Pairing) error {
	if r.cfg.Signer == nil || p.Cert.Verify(r.cfg.Signer.IdentityKey()) != nil {
		return errors.New("client certificate does not belong to this identity")
	}
	pub, err := hex.DecodeString(p.Cert.Device)
	if err != nil || len(pub) != 32 || len(p.BoxKey) != 32 {
		return errors.New("client keys must be 32 bytes")
	}
	if len(p.Reads) == 0 || len(p.Reads) > 8 {
		return errors.New("choose one to eight snapshot methods")
	}
	for _, name := range p.Reads {
		if !slices.Contains([]string{"agents.list", "asks.list", "tasks.list", "inbox.list", "spend.today"}, name) {
			return errors.New("unsupported snapshot method: " + name)
		}
	}
	for _, name := range p.Methods {
		if !slices.Contains([]string{"asks.answer", "agents.send", "agents.stop", "agents.create"}, name) {
			return errors.New("unsupported queued method: " + name)
		}
	}
	return nil
}
