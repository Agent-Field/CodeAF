package pairbox

import "context"

// generationState belongs to one request, so a long poll cannot cross into a
// later mailbox after the nameplate it was waiting on is released and reused.
type generationState struct {
	expected string
	known    bool
	tell     func(string)
}

type generationKey struct{}

// fence runs under Memory's lock with the mailbox operation, so a concurrent
// deletion cannot slip between checking the opening and using its messages.
func fence(ctx context.Context, box *mailbox) error {
	state, _ := ctx.Value(generationKey{}).(*generationState)
	if state == nil {
		return nil
	}
	if state.known && state.expected != box.generation {
		return ErrGone
	}
	state.expected, state.known = box.generation, true
	state.tell(box.generation)
	return nil
}
