package clientrun

import (
	"context"
	"errors"
	"maps"
	"slices"
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

// PhoneReads is the default phone snapshot read list (excluding deferred crews).
func PhoneReads() []string {
	return []string{"agents.entries", "asks.list", "agents.list", "inbox.list", "tasks.list", "spend.today"}
}

// PhoneMethods is the default phone queued-call method list (excluding deferred crews).
func PhoneMethods() []string {
	return []string{"agents.create", "agents.send", "agents.stop", "asks.answer", "inbox.answer", "tasks.start"}
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

// Pause updates the local pause flag for an enrolled phone after any round in
// flight, without revoking the phone at the relay.
func (r *Runner) Pause(id string, paused bool) error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.enrolled[id]; !ok || r.isTombstonedLocked(id) {
		return errors.New("paired client not found")
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
// writes a local tombstone, deleting its keys, receipts, and outcomes.
func (r *Runner) Unpair(ctx context.Context, id string) error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	if _, ok := r.enrolled[id]; !ok || r.isTombstonedLocked(id) {
		r.mu.Unlock()
		return errors.New("paired client not found")
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
