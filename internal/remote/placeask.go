package remote

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE PLACES MODEL ASK, BOTH HALVES ───────────────────────────────────────
//
// internal/placegraph decides when a chat should be offered a place or a group
// of chats a new one, and asks the engine's role door the one question rules
// could not answer. That door is the ENGINE's for the team asks' reason
// (wire_teams.go): the model, its key and its bill are on the engine's machine,
// and a surface on another one must reach them over this wire or not at all.
// This file is the method, the engine answering it from the agent it has open,
// and the surface asking it, in one place for teamask.go's reason.
//
// IT CARRIES A BUDGET, NOT A DEADLINE, for [TeamNameArgs.Budget]'s reason: the
// engine's clock is not this one's. The asker bounds its wait, and the engine
// bounds the model call by the same span so it stops when nobody is listening.

// MethodPlacesAsk asks the engine one internal/placegraph question on its own
// place role and answers with the raw text and the model that wrote it.
const MethodPlacesAsk = "Places.Ask" // PlacesAskArgs → session.PlacesAnswer

// PlacesQuestion is [placegraph.ModelRequest] as it crosses the wire. It is a
// struct of its own because the placegraph type carries no JSON tags, and a
// wire shape that took its field names from Go spelling would change the day
// somebody renamed a field there.
type PlacesQuestion struct {
	Role   roles.Role `json:"role"`
	System string     `json:"system"`
	User   string     `json:"user"`
}

// PlacesAskArgs is one places question and how long the asker will wait. Zero
// Budget uses the engine's ceiling; a negative Budget has already expired and
// never starts a model call.
type PlacesAskArgs struct {
	Request PlacesQuestion `json:"request"`
	Budget  time.Duration  `json:"budget,omitempty"`
}

// placeAskDoor is the engine agent's places ask, as *session.Agent answers it.
type placeAskDoor interface {
	AskPlaces(ctx context.Context, req placegraph.ModelRequest) (session.PlacesAnswer, error)
}

// placeAskKnown is [Welcome.PlaceAsk]: whether the agent this engine has open
// answers the places ask.
func placeAskKnown(agent any) bool { _, ok := agent.(placeAskDoor); return ok }

// placeAskOffWord is an engine that does not answer the places ask. Nobody is
// shown it; it is the error the ask fails with, which the recommender reads as
// it reads any failed ask — rules alone, and no model's opinion.
const placeAskOffWord = "this engine cannot answer questions about places"

// placeAskCeiling bounds a peer's request. A filing or a suggestion is offered
// in the background after a reply, so nothing on screen waits on it, but an
// engine that held a model call open past this would be spending for an asker
// that has long since given up.
const placeAskCeiling = 60 * time.Second

// placeAskWithin uses the shorter of the asker's budget and the engine's
// ceiling.
func placeAskWithin(budget time.Duration) (context.Context, context.CancelFunc) {
	if budget == 0 || budget > placeAskCeiling {
		budget = placeAskCeiling
	}
	return context.WithTimeout(context.Background(), budget)
}

// placeAskCall answers the places ask from agent, and says whether the method
// was it at all. A false hands the call on to the refusal an engine from before
// this door answers.
func placeAskCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	if call.Method != MethodPlacesAsk {
		return nil, false, nil
	}
	door, ok := agent.(placeAskDoor)
	if !ok {
		return nil, true, errors.New(placeAskOffWord)
	}
	args, err := arg[PlacesAskArgs](call)
	if err != nil {
		return nil, true, err
	}
	if args.Budget < 0 {
		return nil, true, context.DeadlineExceeded
	}
	ctx, cancel := placeAskWithin(args.Budget)
	defer cancel()
	answer, err := door.AskPlaces(ctx, placegraph.ModelRequest{
		Role: args.Request.Role, System: args.Request.System, User: args.Request.User,
	})
	if err != nil {
		return nil, true, err
	}
	payload, err := json.Marshal(answer)
	return payload, true, err
}

// AskPlaces asks the engine one internal/placegraph question: the recommender's
// asker, over the wire. An engine without the ask is refused here, before
// anything is written, and what is left of ctx's deadline is sent as the
// engine's budget so the model call gives up when this end does.
func (a *Agent) AskPlaces(ctx context.Context, req placegraph.ModelRequest) (session.PlacesAnswer, error) {
	if !a.c.Welcome().PlaceAsk {
		return session.PlacesAnswer{}, errors.New(placeAskOffWord)
	}
	budget, ok := teamAskBudget(ctx)
	if !ok {
		return session.PlacesAnswer{}, context.DeadlineExceeded
	}
	// A model question may take the advertised minute; the ordinary ten-second
	// getter deadline must not discard its answer while the engine is spending.
	payload, err := a.c.callWithin(ctx, MethodPlacesAsk, PlacesAskArgs{
		Request: PlacesQuestion{Role: req.Role, System: req.System, User: req.User},
		Budget:  budget,
	}, placeAskCeiling)
	if err != nil {
		return session.PlacesAnswer{}, err
	}
	var out session.PlacesAnswer
	if err := json.Unmarshal(payload, &out); err != nil {
		return session.PlacesAnswer{}, err
	}
	return out, nil
}
