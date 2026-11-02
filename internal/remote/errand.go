package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// methodErrand runs an errand on the engine that owns the conversation, where
// its budget, provider keys and model policy are the authoritative ones.
const methodErrand = "conversation.errand"

type errandDoor interface {
	RunErrand(context.Context, session.ErrandAsk) (session.ErrandAnswer, error)
}

func errandKnown(agent any) bool { _, ok := agent.(errandDoor); return ok }

// errandReply carries a route refusal as words the near end turns back into
// [session.ErrErrandRoute], so the app's model_not_allowed survives the hop.
type errandReply struct {
	Answer  session.ErrandAnswer `json:"answer"`
	Refused string               `json:"refused,omitempty"`
}

func errandCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	if call.Method != methodErrand {
		return nil, false, nil
	}
	door, ok := agent.(errandDoor)
	if !ok {
		return nil, true, errors.New("errands unavailable")
	}
	ask, err := arg[session.ErrandAsk](call)
	if err != nil {
		return nil, true, err
	}
	// Normalize again here: this end's limits are the ones that hold.
	if ask, err = ask.Normalize(); err != nil {
		return nil, true, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), ask.Deadline)
	defer cancel()
	var reply errandReply
	reply.Answer, err = door.RunErrand(ctx, ask)
	if errors.Is(err, session.ErrErrandRoute) {
		reply.Refused, err = strings.TrimPrefix(err.Error(), session.ErrErrandRoute.Error()+": "), nil
	}
	if err != nil {
		return nil, true, err
	}
	data, err := json.Marshal(reply)
	return data, true, err
}

// RunErrand asks the owning engine. An owner from before errands answers
// unavailable, the same empty answer an app door without the door gives.
func (a *Agent) RunErrand(ctx context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	if !a.c.Welcome().Errand {
		return session.ErrandAnswer{Reason: "unavailable"}, nil
	}
	data, err := a.c.call(ctx, methodErrand, ask)
	if err != nil {
		return session.ErrandAnswer{}, err
	}
	var reply errandReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return session.ErrandAnswer{}, err
	}
	if reply.Refused != "" {
		return session.ErrandAnswer{}, fmt.Errorf("%w: %s", session.ErrErrandRoute, reply.Refused)
	}
	return reply.Answer, nil
}
