package wscollab

import (
	"context"
	"fmt"
	"strings"
)

// Participant is one attributed contributor to a discussion. ActorID is
// minted by software. Role is a configurable label — planner and critic are
// strings here, not types. Origin on a representative is always agent.
type Participant struct {
	ActorID    string
	Represents string
	Role       string
	Origin     string
}

// Discussion is one shared conversation. Invite adds participants with
// ancestor dedupe so a conflict has one seat per distinct parent and one Root
// (A17). It does not merge the folders those chats live in.
type Discussion struct {
	ID           string
	Participants []Participant
}

// OpenDiscussion starts or returns a discussion. A separate chat is optional
// product work in wsapi; this is the participant record the router keeps.
func (r *Router) OpenDiscussion(ctx context.Context, id string) (Discussion, error) {
	if strings.TrimSpace(id) == "" {
		return Discussion{}, fmt.Errorf("%w: discussion id is empty", ErrInvalid)
	}
	got, err := r.store.GetDiscussion(ctx, id)
	if err == nil {
		return got, nil
	}
	if err != ErrNotFound {
		return Discussion{}, err
	}
	opened := Discussion{ID: id}
	if err := r.store.PutDiscussion(ctx, opened); err != nil {
		return Discussion{}, err
	}
	return opened, nil
}

// Invite adds participants to a discussion, deduplicating by who they
// represent. Existing seats win. Root is named at most once.
func (r *Router) Invite(ctx context.Context, discussionID string, incoming []Participant) (Discussion, error) {
	d, err := r.OpenDiscussion(ctx, discussionID)
	if err != nil {
		return Discussion{}, err
	}
	merged, err := mergeParticipants(d.Participants, incoming)
	if err != nil {
		return Discussion{}, err
	}
	d.Participants = merged
	if err := r.store.PutDiscussion(ctx, d); err != nil {
		return Discussion{}, err
	}
	return d, nil
}

func mergeParticipants(have, add []Participant) ([]Participant, error) {
	out := append([]Participant(nil), have...)
	seen := map[string]bool{}
	for _, p := range out {
		seen[representKey(p)] = true
	}
	for _, p := range add {
		filled, err := fillParticipant(p)
		if err != nil {
			return nil, err
		}
		key := representKey(filled)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filled)
	}
	return out, nil
}

func fillParticipant(p Participant) (Participant, error) {
	if strings.TrimSpace(p.Represents) == "" {
		return Participant{}, fmt.Errorf("%w: participant represents nobody", ErrInvalid)
	}
	if p.Origin == "" {
		p.Origin = OriginAgent
	}
	if p.Origin != OriginAgent {
		return Participant{}, fmt.Errorf("%w: a representative cannot stamp person origin", ErrInvalid)
	}
	if p.ActorID == "" {
		id, err := mintID()
		if err != nil {
			return Participant{}, err
		}
		p.ActorID = id
	}
	return p, nil
}

func representKey(p Participant) string {
	return p.Represents
}
