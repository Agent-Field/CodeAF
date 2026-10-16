package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE SIDE ASK, BOTH HALVES ───────────────────────────────────────────────
//
// A surface asks the conversation's own model one question about the
// conversation, and the answer comes back to the surface alone: no turn runs,
// nothing is written into the conversation, and no event reaches the room
// (internal/session's sideask.go has the why). The app's crew proposal
// (crews.propose) is the caller: the crew proposal it reads is the surface's business,
// and drawn in the chat it was the app's plumbing printed as the person's words
// and the model's reply.
//
// IT CROSSES THE WIRE BECAUSE EVERY APP CONVERSATION IS ON IT. The app holds a
// *remote.Agent even for a conversation it built itself (a loopback), so a door
// the session has and this file does not is a door the app cannot reach.
//
// IT IS WORK AND NOT A GETTER ([callClass]): up to two minutes of a model call
// that owes a running turn nothing, so it runs on a goroutine of its own and
// the client waits [session.SideAskPatience] for it rather than the ten
// seconds a getter gets.

// MethodSideAsk asks the engine's agent one question about its conversation,
// outside it.
const MethodSideAsk = "Conversation.SideAsk" // SideAskArgs → string

// SideAskArgs is the question, and how long the surface will wait. Zero Budget
// uses the engine's own ceiling; a negative Budget has already expired and
// never starts a model call.
type SideAskArgs struct {
	Question string        `json:"question"`
	Budget   time.Duration `json:"budget,omitempty"`
}

// sideAskDoor is the engine agent's side ask.
type sideAskDoor interface {
	SideAsk(ctx context.Context, question string) (string, error)
}

// sideAskKnown is [Welcome.SideAsk]: whether the agent this engine has open
// answers the side ask.
func sideAskKnown(agent any) bool { _, ok := agent.(sideAskDoor); return ok }

// sideAskOffWord is an engine that does not answer the side ask. It is said
// rather than worked around: sending the question as a turn instead is the
// exact thing the door exists to stop.
const sideAskOffWord = "this engine cannot be asked about a conversation outside it; update codeaf"

// sideAskWithin uses the shorter of the surface's budget and the engine's
// ceiling, which is the surface's own patience.
func sideAskWithin(budget time.Duration) (context.Context, context.CancelFunc) {
	if budget == 0 || budget > session.SideAskPatience {
		budget = session.SideAskPatience
	}
	return context.WithTimeout(context.Background(), budget)
}

// sideAskCall answers the side ask from agent, and says whether the method was
// it at all. A false hands the call on to the refusal an engine from before
// this door answers.
func sideAskCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	if call.Method != MethodSideAsk {
		return nil, false, nil
	}
	door, ok := agent.(sideAskDoor)
	if !ok {
		return nil, true, errors.New(sideAskOffWord)
	}
	args, err := arg[SideAskArgs](call)
	if err != nil {
		return nil, true, err
	}
	if args.Budget < 0 {
		return nil, true, context.DeadlineExceeded
	}
	ctx, cancel := sideAskWithin(args.Budget)
	defer cancel()
	answer, err := door.SideAsk(ctx, args.Question)
	if err != nil {
		return nil, true, err
	}
	payload, err := json.Marshal(answer)
	return payload, true, err
}

// SideAsk asks the engine's agent one question about its conversation,
// outside it, and answers what the model said. An engine without the door is
// refused here, before anything is written.
//
// ctx's deadline bounds the call, and is told to the engine so its model call
// gives up with it; with no deadline the call waits
// [session.SideAskPatience].
func (a *Agent) SideAsk(ctx context.Context, question string) (string, error) {
	if !a.c.Welcome().SideAsk {
		return "", errors.New(sideAskOffWord)
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("a side ask needs a question")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	budget := session.SideAskPatience
	if deadline, ok := ctx.Deadline(); ok {
		budget = time.Until(deadline)
		if budget <= 0 {
			return "", context.DeadlineExceeded
		}
	}
	payload, err := a.c.callWithin(ctx, MethodSideAsk, SideAskArgs{Question: question, Budget: budget}, budget)
	if err != nil {
		return "", err
	}
	var answer string
	if err := json.Unmarshal(payload, &answer); err != nil {
		return "", err
	}
	return answer, nil
}
