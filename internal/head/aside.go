package head

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Ephemeral asks (8.2.9): directions are journal, curiosity is cheap.
//
// The law it completes is 5.18's: `@task` SPEECH lands durably in the task's
// thread, because it is direction and direction has to be replayable. A
// QUESTION about running work — "why did you pick that one?", "what is it
// actually doing right now?" — is not direction. It changes nothing, it is
// asked and answered and finished, and putting it through an ordinary turn
// makes the orchestrator carry both halves of it in every subsequent prompt, in
// the most expensive position there is, forever.
//
// "Ephemeral" here means ephemeral TO THE MODEL'S CONTEXT and never to the
// record. Journal-is-truth holds and 5.20's no-dead-air rule holds, so:
//
//   - The exchange journals a COLLAPSED ONE-LINE STUB in the room where it was
//     asked — "▸ asked → answered" — with both halves under it as a part. It is
//     durable, expandable, searchable and referable later. The echo the person
//     needs exists; it just does not take a turn's worth of space.
//   - The full exchange stays OUT of the orchestrator's context. What the next
//     turn's thread window sees is the one line, because the window renders
//     bodies and the exchange lives in the part.
//   - The full exchange stays out of the orchestrator's PROMPT CACHE, under its
//     own cache key rather than below a floor. 12.4.1's prefix ordering is
//     cache-critical and regression-tested, and an ephemeral turn that shared
//     the orchestrator's prefix would either reorder it or evict it — costing
//     more than the turn it saved. A different system message is a different
//     prefix from byte zero, which is the cleanest separation available and the
//     one this takes.
//
// It is also a different SHAPE of call, and that is where most of the saving is:
// no tools. An aside is curiosity, and curiosity has no authority — so the belt
// is not sent, the ~4.4k tokens of definitions are not paid for, and there is
// no arm by which a question about running work could change it.

const (
	// asideMaxTokens bounds one aside. It is half the answering turn's cap
	// because an aside answers ONE question about something already on the board
	// and never carries a deliverable — the artifact law's whole point is that
	// deliverables leave this budget entirely, and an aside cannot write one.
	asideMaxTokens = 600
	// asideQuestionBytes bounds what may be asked. Past this it is not a
	// curiosity question, it is a direction, and directions are journal.
	asideQuestionBytes = 600
	// asideStubBytes keeps the collapsed line to a line.
	asideStubBytes = 90
	// asideStreamSuffix keys an aside's deltas apart from the room's own turn.
	//
	// 8.2.9 named the prerequisite exactly: without a session key an ephemeral
	// turn's deltas render into whatever room is listening. Keys landed, but one
	// key per ROOM is not enough here, because an aside can run while that room's
	// turn is streaming and the two would share it. A surface that has not
	// learned this key renders nothing, which is the correct failure: an aside
	// silently typing itself into the main transcript is exactly the bug.
	asideStreamSuffix = "#aside"
)

// AsideStub opens the collapsed line. It is a constant so a surface can
// recognize the row it must draw folded, and so the stub reads the same in the
// journal, in a search result and on screen.
const AsideStub = "▸ asked → answered"

// AsideStreamSession is the stream key an ephemeral turn's deltas carry.
func AsideStreamSession(sessionID string) string { return sessionID + asideStreamSuffix }

// asidePrompt is the ephemeral turn's own system message.
//
// It is a separate constant rather than a variation on the orchestrator's, and
// that is the cache decision made concrete: two prompts that shared a prefix
// would compete for one cache entry, and the orchestrator's — which is paid for
// on every ordinary message — must never lose that competition to a question
// nobody will refer to again. It deliberately does not even open with the same
// word, so "its own cache key" is a property a test can state exactly rather
// than an argument about how many leading bytes an endpoint forgives.
//
// It is also a genuinely different job. The orchestrator's prompt is mostly
// about what its hands are and what they may not do; this one has no hands.
const asidePrompt = `Answer one aside — a question the person asked on the side, about work that is already under way or about something already said. You have no tools and you change nothing: this is a question, not an instruction, and if it turns out to be an instruction the honest answer is to say that it needs to be asked in the conversation proper so it can be acted on.

Answer from what is in front of you and from nothing else. If the answer is not there, say plainly that it is not — an aside is cheap precisely because it is allowed to come back empty, and a confident reconstruction is worth less than nothing here.

Be short. Two or three sentences, no preamble, no restating the question, no offer to go and do anything. Speak in their terms: a step is a step, the work is the work, and none of the machinery's own names for itself belong in the answer.`

// ErrAsideEmpty says the aside carried no question.
var ErrAsideEmpty = errors.New("head aside: nothing was asked")

// Ask runs one ephemeral turn and journals its collapsed stub.
//
// It deliberately does NOT touch the turn machinery: turnCancel, turnFold and
// the partial belong to the conversation's own turn, and an aside asked while
// one is in flight must leave it exactly as it found it. That is 8.2.9's "the
// in-flight turn undisturbed", and it is why nothing here locks turnMu.
func (h *Head) Ask(ctx context.Context, sessionID, question string) (string, error) {
	if h == nil || h.store == nil {
		return "", errors.New("head aside: no graph behind this surface")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", ErrAsideEmpty
	}
	question = truncateBytes(question, asideQuestionBytes)

	client, err := h.clientFor(store.Message{SessionID: sessionID})
	if err != nil {
		return "", fmt.Errorf("head aside: %w", err)
	}
	prompt, err := h.asidePromptFor(sessionID, question)
	if err != nil {
		return "", err
	}
	// Keyed apart from the room's own turn, so deltas can never be mistaken for
	// the reply the person is waiting on.
	response, err := client.CompleteWithMessages(
		provider.WithStreamSession(ctx, AsideStreamSession(sessionID)),
		[]ai.Message{textMessage("system", asidePrompt), textMessage("user", prompt)},
		ai.WithMaxTokens(asideMaxTokens))
	if err != nil {
		return "", fmt.Errorf("head aside: %w", err)
	}
	answer := ""
	if response != nil {
		answer = strings.TrimSpace(response.Text())
	}
	if answer == "" {
		// 5.20 rule 4: no dead-air sends. An aside that came back with nothing
		// still owes the person a row saying so, or they watched nothing happen.
		answer = providerErrorReply
	}
	served := ""
	if response != nil {
		served = strings.TrimSpace(response.Model)
	}
	if err := h.postAside(sessionID, question, answer, served); err != nil {
		return answer, err
	}
	return answer, nil
}

// asidePromptFor is what the ephemeral turn is given to answer from.
//
// It carries the same grounding an ordinary turn opens with — the conversation
// and the live board — because an aside is almost always about one of those two
// things, and it carries neither the notebook, nor the deep slices, nor the
// readings, nor the manual, because a question that needs those is a question
// for the conversation proper. The order is the orchestrator's own for the same
// reason it is there: appended-to first, rewritten-in-place after.
func (h *Head) asidePromptFor(sessionID, question string) (string, error) {
	// Read straight off the journal rather than through the turn's own window.
	// That window is a kept fold shared with the conversation, and an aside is
	// allowed to run beside an in-flight turn — resetting its fold from the side
	// would make the turn re-fold the whole day to answer a question that was
	// not its own. This costs one bounded query and touches nothing.
	recent, err := h.store.MessageTail(sessionID, threadWindowKeep)
	if err != nil {
		return "", fmt.Errorf("head aside: read recent thread: %w", err)
	}
	kept := make([]store.Message, 0, len(recent))
	for _, message := range recent {
		if personRelevant(message) {
			kept = append(kept, message)
		}
	}
	rendered := h.renderThread(kept)
	var body strings.Builder
	body.WriteString("The conversation so far:\n" + rendered)
	body.WriteString("\n\nLive board (what is running right now):\n" +
		h.boardFor(sessionID, rendered, nil, time.Now()))
	body.WriteString("\n\nThe aside, verbatim:\n" + question)
	return body.String(), nil
}

// postAside journals the collapsed row.
//
// The role is SYSTEM, and that choice does three things at once: the head's own
// poll never answers a system row, so an aside cannot trigger a turn; the
// existing chat's question reader only ever looks at agent rows, so a stub whose
// text happens to end in a question mark can never be mistaken for an askback;
// and every surface already draws system rows as the dim telemetry tier, which
// is what a collapsed row is (5.13).
func (h *Head) postAside(sessionID, question, answer, model string) error {
	_, err := thread.Post(h.store, store.Message{
		SessionID: sessionID,
		Role:      store.RoleSystem,
		Body:      asideStubLine(question),
		Parts: []store.MessagePart{store.AsideRef(store.AsidePart{
			Question: question, Answer: answer, Model: model,
		})},
	})
	if err != nil {
		return fmt.Errorf("head aside: journal the stub: %w", err)
	}
	return nil
}

// asideStubLine is the one line the thread keeps.
//
// It names what was asked, briefly, because a column of identical "asked →
// answered" rows is a row nobody can refer back to — and referring back to it is
// half of why the stub is durable at all. What it must never do is grow: this
// line lands in the head's own thread window on every later turn, and 8.2.9's
// whole economy is that an aside costs the conversation one line rather than a
// turn.
func asideStubLine(question string) string {
	first := firstLine(strings.TrimSpace(question))
	if first == "" {
		return AsideStub
	}
	return AsideStub + " · " + truncateBytes(first, asideStubBytes)
}
