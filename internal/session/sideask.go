package session

// A QUESTION ABOUT A CONVERSATION, ASKED BESIDE IT AND NEVER IN IT.
//
// A surface sometimes needs the conversation's own model to answer something
// ABOUT the conversation — codeaf app's crew proposal asks what crew the
// work in it would want — and the answer is for the surface, not for the
// person and not for the model's next turn. It used to be sent as a turn: the
// surface's instruction landed in the transcript as the person's own message,
// the model's machine-shaped answer landed as its reply, both were drawn on the
// page, both came back on every reread, and both stayed in the context of every
// turn after it. A page that has to hide a turn has to hide it in the stream,
// in the listing, in the title, in the retry and in every other surface that
// reads the same journal; a question that never became a turn has nothing to
// hide anywhere.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT IS AN AUXILIARY CALL AND NEVER A TURN. Nothing is appended to
//     a.messages, nothing is journaled but what the call cost, no event reaches
//     any hub, and the next turn reads exactly the conversation it would have
//     read had nobody asked. That last one is the reason it is not a hidden
//     turn: a model whose context holds an exchange the person never saw
//     answers the person's next message about it.
//
//   - IT READS THE CONVERSATION AS TEXT. The messages are rendered the way the
//     summarizer reads them ([summaryLines]): the person's words, the
//     assistant's, each tool call and a clipped result. A transcript carrying
//     tool calls cannot be sent again without the belt it was made against, and
//     the belt is the one thing this call must not offer — so the model reads
//     the work rather than being handed it back to carry on.
//
//   - IT IS BOUNDED. The newest [sideAskBytes] of that text are sent, so a
//     conversation of a thousand turns costs what a long one does, and the call
//     itself is bounded by [sideAskWindow].
//
//   - IT RIDES THE CONVERSATION'S OWN MODEL and is paid for out of the
//     conversation's own pocket, detached from any turn that happens to be
//     running ([Agent.addDetachedUsageAs]): a person asked for it, and it is
//     not free.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const (
	// sideAskBytes is how much of the conversation, rendered as text, one side
	// ask carries: the newest end of it, about sixteen thousand tokens. The
	// question is about the work in hand, and the work in hand is at the end.
	sideAskBytes = 64_000

	// sideAskWindow bounds the model call. Somebody pressed a button and is
	// waiting for what comes back, but nothing else is held up behind it.
	sideAskWindow = 2 * time.Minute

	// SideAskPatience is how long a surface on the far side of a connection
	// waits for a side ask: the call's own window, and a margin for the
	// rendering and the wire around it.
	SideAskPatience = sideAskWindow + 30*time.Second

	// auxRoleSideAsk names a side ask's ledger line, so a person reading the
	// conversation's spending can see what the question cost.
	auxRoleSideAsk = "side-ask"

	// purposeSideAsk is what the model-call log files a side ask under.
	purposeSideAsk callPurpose = "side-ask"

	// sideAskSystem is the side ask's system message. It says plainly that the
	// conversation below is a record and the question is the app's, because a
	// model shown its own work tends to carry on with it.
	sideAskSystem = "You are the assistant in the conversation shown below, between a person and you. " +
		"The app you run in is asking you one question about that conversation. " +
		"Answer only that question, in exactly the form it asks for. " +
		"Do not carry on with the work, do not address the person, and do not call any tools."

	// sideAskElided opens a conversation whose oldest part did not fit.
	sideAskElided = "[the start of the conversation is not shown]\n\n"

	// sideAskNothing stands for a conversation with nothing in it yet.
	sideAskNothing = "(nothing has been said yet)\n\n"
)

// errSideAskQuestion is a side ask with nothing to ask.
var errSideAskQuestion = errors.New("session: a side ask needs a question")

// SideAsk asks the conversation's own model one question about the
// conversation, outside it: the model reads the conversation as text and
// answers, and nothing about the conversation changes. The answer is returned
// as the model wrote it, trimmed; an empty one is an error. ctx bounds the
// call as well as [sideAskWindow] does.
func (a *Agent) SideAsk(ctx context.Context, question string) (string, error) {
	return a.sideAsk(ctx, question, nil)
}

// SideAskStream answers a side ask while yielding answer text in arrival order.
// On failure it returns the text received so far alongside the error.
func (a *Agent) SideAskStream(ctx context.Context, question string, onText func(string)) (string, error) {
	return a.sideAsk(ctx, question, onText)
}

func (a *Agent) sideAsk(ctx context.Context, question string, onText func(string)) (string, error) {
	if err := a.CheckWork(); err != nil {
		return "", err
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errSideAskQuestion
	}
	a.mu.Lock()
	model, closed := a.model, a.closed
	// The slice is copied under the lock and read outside it: a message is
	// replaced whole when compaction edits one, never written in place, so the
	// copy is a stable reading of the conversation as it stood.
	messages := append([]ai.Message(nil), a.messages...)
	a.mu.Unlock()
	if closed {
		return "", errors.New("the conversation is closed")
	}
	ask := sideAskPrompt(messages, question)
	ctx = provider.WithRole(provider.WithoutStream(withDetachedUsage(ctx)), lane.RoleAuxiliary)
	ctx, cancel := context.WithTimeout(ctx, sideAskWindow)
	defer cancel()
	var streamed strings.Builder
	var replaced bool
	if onText != nil {
		ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
			if event.Kind == provider.StreamReplaced && streamed.Len() > 0 {
				replaced = true
				cancel()
			}
			if !replaced && event.Kind == provider.StreamDelta && event.Delta != "" {
				streamed.WriteString(event.Delta)
				onText(event.Delta)
			}
		})
	}
	ctx = provider.WithReasoningEffort(ctx, provider.EffortLow)
	if onText != nil {
		ctx = provider.WithRequiredReasoningEffort(ctx, provider.EffortOff)
	}
	response, answered, err := a.completeWithNamedModel(ctx, purposeSideAsk,
		[]ai.Message{textMessage("system", sideAskSystem), textMessage("user", ask)}, model)
	if response != nil {
		a.addDetachedUsageAs(response, answered, 1, auxRoleSideAsk)
	}
	if replaced {
		return strings.TrimSpace(streamed.String()), errors.New("the side ask stream was interrupted before completion")
	}
	if err != nil {
		return strings.TrimSpace(streamed.String()), err
	}
	if response == nil {
		return strings.TrimSpace(streamed.String()), errEmptyAnswer
	}
	text := strings.TrimSpace(response.Text())
	if text == "" {
		return "", errEmptyAnswer
	}
	return text, nil
}

// sideAskPrompt is the user message of one side ask: the conversation as the
// summarizer reads it, newest [sideAskBytes] of it, and then the question.
func sideAskPrompt(messages []ai.Message, question string) string {
	var region []ai.Message
	for _, message := range messages {
		if message.Role != "system" {
			region = append(region, message)
		}
	}
	lines := summaryLines(region)
	// A COMPACTED CONVERSATION OPENS ON ITS SUMMARY, which [summaryLines] skips
	// because the summarizer carries it separately. Here it is the only account
	// of everything before it, so it is read as the conversation's first line.
	if len(region) > 0 {
		if text := messageContentText(region[0]); strings.HasPrefix(text, summaryNotePrefix) {
			if body := summaryNoteBody(text); body != "" {
				lines = append([]string{"EARLIER, SUMMARIZED: " + body + "\n\n"}, lines...)
			}
		}
	}
	// The newest lines that fit, oldest first; a single line longer than the
	// whole allowance keeps its start and its end.
	kept, size := len(lines), 0
	for kept > 0 && size+len(lines[kept-1]) <= sideAskBytes {
		kept--
		size += len(lines[kept])
	}
	var b strings.Builder
	b.WriteString("The conversation so far:\n\n")
	switch {
	case len(lines) == 0:
		b.WriteString(sideAskNothing)
	case kept == len(lines):
		if len(lines) > 1 {
			b.WriteString(sideAskElided)
		}
		b.WriteString(strings.TrimRight(clipMiddle(lines[len(lines)-1], sideAskBytes), "\n") + "\n\n")
	default:
		if kept > 0 {
			b.WriteString(sideAskElided)
		}
		for _, line := range lines[kept:] {
			b.WriteString(line)
		}
	}
	b.WriteString("The app's question:\n\n")
	b.WriteString(question)
	return b.String()
}
