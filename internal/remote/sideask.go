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

// MethodSideAskStream asks a side question with per-call text deltas.
const MethodSideAskStream = "Conversation.SideAskStream"

type sideAskStreamDoor interface {
	SideAskStream(context.Context, string, func(string)) (string, error)
}

type sideAskStreamAnswer struct {
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

// SideAskArgs is the question, and how long the surface will wait. Zero Budget
// uses the engine's own ceiling; a negative Budget has already expired and
// never starts a model call.
type SideAskArgs struct {
	Question string        `json:"question"`
	Budget   time.Duration `json:"budget,omitempty"`
	// Stream correlates ephemeral deltas with the requesting client alone.
	Stream uint64 `json:"stream,omitempty"`
}

// sideAskDoor is the engine agent's side ask.
type sideAskDoor interface {
	SideAsk(ctx context.Context, question string) (string, error)
}

// sideAskKnown is [Welcome.SideAsk]: whether the agent this engine has open
// answers the side ask.
func sideAskKnown(agent any) bool { _, ok := agent.(sideAskDoor); return ok }

func sideAskStreamKnown(agent any) bool { _, ok := agent.(sideAskStreamDoor); return ok }

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
func sideAskCall(agent WrappedAgent, call Frame, send func(Frame) error) (json.RawMessage, bool, error) {
	if call.Method != MethodSideAsk && call.Method != MethodSideAskStream {
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
	if call.Method == MethodSideAskStream {
		streaming, ok := agent.(sideAskStreamDoor)
		if !ok {
			return nil, true, errors.New(sideAskOffWord)
		}
		answer, askErr := streaming.SideAskStream(ctx, args.Question, func(text string) {
			payload, _ := json.Marshal(text)
			if err := send(Frame{Kind: "sideAskDelta", ID: args.Stream, Payload: payload}); err != nil {
				cancel()
			}
		})
		result := sideAskStreamAnswer{Text: answer}
		if askErr != nil {
			result.Error = askErr.Error()
		}
		payload, err := json.Marshal(result)
		return payload, true, err
	}
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

// SideAskStream yields only this call's text, outside turn streams and replay.
func (a *Agent) SideAskStream(ctx context.Context, question string, onText func(string)) (string, error) {
	if !a.c.Welcome().SideAskStream {
		answer, err := a.SideAsk(ctx, question)
		if answer != "" && onText != nil {
			onText(answer)
		}
		return answer, err
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("a side ask needs a question")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	budget := session.SideAskPatience
	if deadline, ok := ctx.Deadline(); ok {
		budget = time.Until(deadline)
		if budget <= 0 {
			return "", context.DeadlineExceeded
		}
	}
	id := a.c.seq.Add(1)
	chunks := make(chan string, 64)
	a.c.mu.Lock()
	if a.c.sideAsks == nil {
		a.c.sideAsks = make(map[uint64]func(string))
	}
	a.c.sideAsks[id] = func(text string) {
		select {
		case chunks <- text:
		case <-ctx.Done():
		}
	}
	a.c.mu.Unlock()
	defer func() { a.c.mu.Lock(); delete(a.c.sideAsks, id); a.c.mu.Unlock() }()
	done := make(chan result, 1)
	go func() {
		payload, err := a.c.callWithin(ctx, MethodSideAskStream, SideAskArgs{Question: question, Budget: budget, Stream: id}, budget)
		done <- result{payload: payload, err: err}
	}()
	var received strings.Builder
	deliver := func(text string) {
		received.WriteString(text)
		if onText != nil {
			onText(text)
		}
	}
	for {
		select {
		case text := <-chunks:
			deliver(text)
		case reply := <-done:
			for len(chunks) > 0 {
				deliver(<-chunks)
			}
			if reply.err != nil {
				return strings.TrimSpace(received.String()), reply.err
			}
			var answer sideAskStreamAnswer
			if err := json.Unmarshal(reply.payload, &answer); err != nil {
				return strings.TrimSpace(received.String()), err
			}
			if answer.Error != "" {
				return answer.Text, errors.New(answer.Error)
			}
			return answer.Text, nil
		}
	}
}

func (c *Client) sideAskDelta(frame Frame) {
	var text string
	if json.Unmarshal(frame.Payload, &text) != nil {
		return
	}
	c.mu.Lock()
	deliver := c.sideAsks[frame.ID]
	c.mu.Unlock()
	if deliver != nil {
		deliver(text)
	}
}
