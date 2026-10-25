package clientrun

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	if err := r.register(ctx, p); err != nil {
		return Grant{}, err
	}
	p.BoxKey = slices.Clone(p.BoxKey)
	p.Methods = slices.Clone(p.Methods)
	p.Reads = slices.Clone(p.Reads)
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
	probe := [32]byte{1}
	if _, err := curve25519.X25519(probe[:], p.BoxKey); err != nil {
		return errors.New("client encryption key cannot receive sealed data")
	}
	if len(p.Reads) == 0 || len(p.Reads) > len(PhoneReads()) {
		return fmt.Errorf("choose one to %d snapshot methods", len(PhoneReads()))
	}
	for _, name := range p.Reads {
		if !slices.Contains(PhoneReads(), name) {
			return errors.New("unsupported snapshot method: " + name)
		}
	}
	for _, name := range p.Methods {
		if !slices.Contains(PhoneMethods(), name) {
			return errors.New("unsupported queued method: " + name)
		}
	}
	return nil
}

// register permits a second trusted executor to enroll the same client locally
// without changing the relay's immutable method scope or sharing private keys.
func (r *Runner) register(ctx context.Context, p Pairing) error {
	exists, err := r.registered(ctx, p)
	if err != nil || exists {
		return err
	}
	body := struct {
		Cert    identity.Cert `json:"cert"`
		Methods []string      `json:"methods"`
		Name    string        `json:"name"`
	}{p.Cert, p.Methods, ""}
	if err := r.request(ctx, "PUT", clientPath(p.Cert.DeviceID()), body, nil); err != nil {
		// Another executor may have registered it, or the acknowledgement may
		// have been lost. Only the exact existing client scope permits recovery.
		exists, checkErr := r.registered(ctx, p)
		if checkErr == nil && exists {
			return nil
		}
		return err
	}
	return nil
}
func (r *Runner) registered(ctx context.Context, p Pairing) (bool, error) {
	listing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return false, err
	}
	d, exists := listing.Devices[p.Cert.DeviceID()]
	if !exists {
		return false, nil
	}
	if d.Revoked || d.Kind != "client" {
		return false, errors.New("device is not an active scoped client")
	}
	allowed, requested := slices.Clone(d.Methods), slices.Clone(p.Methods)
	slices.Sort(allowed)
	slices.Sort(requested)
	if !slices.Equal(allowed, requested) {
		return false, errors.New("client permissions differ; pair a new key to change scope")
	}
	return true, nil
}

// PhoneReads is the default phone snapshot grant. Each call returns its own slice.
func PhoneReads() []string {
	return []string{"agents.entries", "asks.list", "agents.list", "inbox.list", "tasks.list", "spend.today", "crews.list", "crews.get", "crews.items", "crews.log"}
}

// PhoneMethods is the default phone queued-call grant.
func PhoneMethods() []string {
	return []string{"agents.create", "agents.send", "agents.stop", "asks.answer", "inbox.answer", "tasks.start", "crews.create", "crews.message", "crews.decide"}
}
