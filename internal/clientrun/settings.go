package clientrun

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/Agent-Field/codeaf/internal/clientproto"
)

// Client is the Settings view of an enrolled phone derived from the relay and
// directory, paired with this computer's local pause flag. No secret is held.
type Client struct {
	ID      string   `json:"id"`
	SignKey []byte   `json:"sign_key,omitempty"`
	BoxKey  []byte   `json:"box_key"`
	Methods []string `json:"methods"`
	Reads   []string `json:"reads"`
	Ver     uint64   `json:"ver"`
	Paused  bool     `json:"paused"`
}

// PhoneReads returns the shared phone snapshot read list from clientproto so
// the enrollment and the runner share one source of truth.
func PhoneReads() []string {
	return clientproto.PhoneReads()
}

// PhoneMethods returns the shared phone queued-call method list from
// clientproto so the enrollment and the runner share one source of truth.
func PhoneMethods() []string {
	return clientproto.PhoneMethods()
}

// Clients lists derived enrolled phone records sorted by device ID. Each slice
// is cloned so a caller cannot mutate permissions held by the runner.
func (r *Runner) Clients() []Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := slices.Sorted(maps.Keys(r.enrolled))
	out := make([]Client, 0, len(ids))
	for _, id := range ids {
		if r.isTombstonedLocked(id) {
			continue
		}
		out = append(out, r.clientOfLocked(id, r.enrolled[id]))
	}
	return out
}

// ErrClientNotFound is what pausing or unpairing a phone answers for one this runner has
// no enrolled record of, or has already forgotten. The host still forgets the phone on its
// side when it hears it, so a solo computer cannot stay awake for a phone that is gone.
var ErrClientNotFound = errors.New("paired client not found")

// Pause updates the local pause flag for an enrolled phone without revoking the
// phone at the relay. It takes mu rather than the round lock so a pause issued
// while a round is in flight is observed by the post-claim check.
func (r *Runner) Pause(id string, paused bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.enrolled[id]; !ok || r.isTombstonedLocked(id) {
		return ErrClientNotFound
	}
	was := r.state.Paused[id]
	if paused {
		r.state.Paused[id] = true
	} else {
		delete(r.state.Paused, id)
	}
	if err := r.save(); err != nil {
		if was {
			r.state.Paused[id] = true
		} else {
			delete(r.state.Paused, id)
		}
		return err
	}
	r.notifyChanged()
	return nil
}

// Unpair revokes the phone at the relay (POST /v1/dir/devices/{id}/revoke) and
// writes a local tombstone, deleting its keys, receipts,outcomes, watermark,
// and create map even if a dispatch is currently in flight.
func (r *Runner) Unpair(ctx context.Context, id string) error {
	r.mu.Lock()
	if _, ok := r.enrolled[id]; !ok || r.isTombstonedLocked(id) {
		r.mu.Unlock()
		return ErrClientNotFound
	}
	r.mu.Unlock()
	if r.cfg.Directory == nil {
		return errors.New("client runner has no directory client")
	}
	if err := r.cfg.Directory.Revoke(ctx, id); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.tombstoneClientLocked(id, r.lastDirVersion); err != nil {
		return err
	}
	r.notifyChanged()
	return nil
}
