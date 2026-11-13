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

// errandReply carries a refusal as words the near end turns back into its
// sentinel, so the app's model_not_allowed and a bad ask's reason both survive
// the hop instead of arriving as bare text.
type errandReply struct {
	Answer  session.ErrandAnswer `json:"answer"`
	Refused string               `json:"refused,omitempty"`
	Invalid string               `json:"invalid,omitempty"`
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
	reply, err := runErrandHere(door, ask)
	if err != nil {
		return nil, true, err
	}
	data, err := json.Marshal(reply)
	return data, true, err
}

// runErrandHere runs the errand on this end, under this end's limits:
// the ask is normalized again here, because these are the ones that hold.
func runErrandHere(door errandDoor, ask session.ErrandAsk) (errandReply, error) {
	var reply errandReply
	ask, err := ask.Normalize()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), ask.Deadline)
		defer cancel()
		reply.Answer, err = door.RunErrand(ctx, ask)
	}
	return reply, reply.keep(err)
}

// keep puts a refusal into the reply as words; any other error is returned.
func (r *errandReply) keep(err error) error {
	switch {
	case errors.Is(err, session.ErrErrandRoute):
		r.Refused = errandWords(err, session.ErrErrandRoute)
	case errors.Is(err, session.ErrErrandAsk):
		r.Invalid = errandWords(err, session.ErrErrandAsk)
	default:
		return err
	}
	return nil
}

// err is the refusal the far end kept, as its sentinel again, or nil.
func (r errandReply) err() error {
	switch {
	case r.Refused != "":
		return fmt.Errorf("%w: %s", session.ErrErrandRoute, r.Refused)
	case r.Invalid != "":
		return fmt.Errorf("%w: %s", session.ErrErrandAsk, r.Invalid)
	}
	return nil
}

func errandWords(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

// RunErrand asks the owning engine. An owner from before errands is an error
// that says so, which the app keeps as unavailable and logs in these words.
//
// THE CALL WAITS AS LONG AS THE ERRAND MAY TAKE, plus one getter's span for
// the hop itself, never a getter's span alone: a factory brief is a model
// call of up to 45 seconds, and at [callDeadline] every slow answer came back
// as a link that gave up.
func (a *Agent) RunErrand(ctx context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	if !a.c.Welcome().Errand {
		return session.ErrandAnswer{}, errors.New("the engine holding this conversation does not run errands")
	}
	ask, err := ask.Normalize()
	if err != nil {
		return session.ErrandAnswer{}, err
	}
	data, err := a.c.callWithin(ctx, methodErrand, ask, ask.Deadline+callDeadline)
	if err != nil {
		return session.ErrandAnswer{}, err
	}
	var reply errandReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return session.ErrandAnswer{}, err
	}
	return reply.Answer, reply.err()
}
