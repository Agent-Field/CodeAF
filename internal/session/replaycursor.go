package session

import "crypto/rand"

// ReplayCursor names a turn within one live agent. Reopening the same journal
// creates a different owner, so an old snapshot cannot suppress a new engine.
type ReplayCursor struct {
	Owner string `json:",omitempty"`
	Turn  uint64 `json:",omitempty"`
}

func (c ReplayCursor) Covers(other ReplayCursor) bool {
	return c.Owner != "" && c.Owner == other.Owner && other.Turn != 0 && other.Turn <= c.Turn
}

func (a *Agent) newReplayHubLocked() *eventHub {
	if a.replayCursor.Owner == "" {
		a.replayCursor.Owner = rand.Text()
	}
	a.replayCursor.Turn++
	hub := newEventHub()
	hub.replayCursor = a.replayCursor
	return hub
}

// AttachReplayCursor captures the replay ownership boundary with the same lock
// as the transcript and current-turn observer. That observer owns the current
// turn; canonical streams through this cursor are already represented.
func (a *Agent) AttachReplayCursor() ([]DisplayEntry, <-chan Event, func(), ReplayCursor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entries, events, stop := a.attachReplayLocked()
	return entries, events, stop, a.replayCursor
}
