package clientrun

import (
	"context"
	"errors"
	"slices"
	"sort"
)

// Clients lists public pairing records. Each slice is copied so a Settings
// caller cannot change the permissions held by the executor.
func (r *Runner) Clients() []Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Client, 0, len(r.state.Clients))
	for _, c := range r.state.Clients {
		c.BoxKey = slices.Clone(c.BoxKey)
		c.Methods = slices.Clone(c.Methods)
		c.Reads = slices.Clone(c.Reads)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Pause takes effect after the current round, without revoking the client.
// Returning from it means no dispatch for that client remains in progress.
func (r *Runner) Pause(id string, paused bool) error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.state.Clients[id]
	if !ok {
		return errors.New("paired client not found")
	}
	old := c
	c.Paused = paused
	r.state.Clients[id] = c
	if err := r.save(); err != nil {
		r.state.Clients[id] = old
		return err
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
	return nil
}

// Unpair revokes at the relay before forgetting the local enrollment. Call
// receipts remain in the chat; they must outlive a client's access to it.
func (r *Runner) Unpair(ctx context.Context, id string) error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.state.Clients[id]
	if !ok {
		return errors.New("paired client not found")
	}
	if err := r.cfg.Directory.Revoke(ctx, id); err != nil {
		return err
	}
	delete(r.state.Clients, id)
	if err := r.save(); err != nil {
		c.Paused = true
		r.state.Clients[id] = c
		return err
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
	return nil
}
