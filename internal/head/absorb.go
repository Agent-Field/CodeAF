package head

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The turn that closes the loop a person started when they asked for something.
//
// A commissioned job used to end with its own deliverable posted into the thread
// by the resident and nothing else: the answer to their question arrived as a
// card written by the work, in the work's own register, and the colleague they
// had been talking to never said a word about it. The person's own sketch of
// what should happen is exactly this file: "then it responds the task result to
// the user based on the initial ask — it contains all info in its context to
// respond: past chat, main user text, the task we created, the result."
//
// So a settled job wakes the head for ONE short turn. It is the DELIVERY
// sentence class of the product stance — the head speaking, in the thread, in
// its own voice — and it is deliberately not a second copy of the delivery card
// beside it: the card carries the artifact rows, the money and the elapsed, and
// re-saying those is the duplication the card exists to end. What the head adds
// is the one thing only it has: the ask this answers, and what the result means
// for it.
//
// It was, for a while, a deliberately tiny turn: no tools, 600 tokens, a
// four-kilobyte peek at the result, and a standing order not to restate anything
// — because the whole result was pasted verbatim underneath whatever it wrote.
// That shape answered the paraphrase incident (§13.4, two generated summaries
// disagreeing with each other and with the verified text) by making generation
// structurally impossible, and it bought that with the person's actual answer:
// what they got was a machine's output with one polite line on top of it.
//
// The answer contract inverts it (chat-simplify §2.6). RAW IN — the turn now
// reads the result whole, opens the file when that is where the substance lives,
// and can read the plan or the board if the ask needs it. COMPOSED OUT — what it
// writes is the answer to the question that started the work, sized to its
// content, findings first and the path after. The verbatim result is still
// journaled and still rendered as the job's own deliverable; it is the record,
// not the message. And the defect the tiny turn was guarding against is
// addressed at its actual cause: a model that has read the whole deliverable is
// not the model that invented the other three quarters of it.
//
// Three bounds keep it affordable and honest.
//   - It fires for work the PERSON asked for, in a room, and never for the
//     resident's own practice, sentinels or standing furniture. Self-directed
//     work has nobody waiting on an answer, and paying a turn to narrate it to
//     an empty room is the exact cost this system is careful about.
//   - It fires once, off the delivery row the resident already posts, and the
//     poll's own cursor is what makes it once.
//   - Its hands are the READS and nothing else (beltReadOnly). The work is
//     finished: there is everything to find out and nothing left to change, and
//     a turn woken by an outcome must not be able to start anything.
const (
	// absorbToolCallCap bounds the looking. The shape this turn has is open the
	// result, open the file it points at, and answer — with room for a second
	// file, a plan read, and one recovery after a tool error. Past that it is not
	// composing an answer, it is browsing.
	absorbToolCallCap = 6
	// absorbSpentReads is what a read past the cap is told. It is a tool result
	// rather than a hard stop so the turn still ends in an answer.
	absorbSpentReads = "that is as much reading as this turn gets — answer their question now from what you have, " +
		"and say plainly what you could not find out"
	// absorbHandless is what a call to anything but a read is told. The
	// definitions offered to this turn are the reads alone, so reaching for a
	// hand is a model recalling a belt it was not given.
	absorbHandless = "this turn has no hands — the work is finished and nothing here starts, changes or stops anything. " +
		"Read what you need and answer them"
	// absorbStreamSuffix keys the absorption turn's deltas apart from the room's
	// own turn, exactly as an aside's are: the person may be typing while this
	// runs, and their reply's stream must not have this one's words in it.
	absorbStreamSuffix = "#delivered"
)

// AbsorbStreamSession is the stream key the absorption turn's deltas carry.
func AbsorbStreamSession(sessionID string) string { return sessionID + absorbStreamSuffix }

// absorbPrompt is the absorption turn's own system message: the answer contract,
// stated to the one turn that exists to keep it.
//
// It is a separate prompt for the aside's cache reason — a prompt that shared
// the orchestrator's prefix would compete with it for one cache entry — and
// because it is a genuinely different job. The orchestrator's prompt is mostly
// about what its hands are; this one has reads and a question to answer.
const absorbPrompt = `Work the person asked for has finished, and you are the one who talks to them. This turn is the ANSWER to the ask that started it — the last mile between work settling and a person knowing what came of it.

Read before you write. The result is below in full. When its substance is in a file the work wrote, open that file and read it: a pointer is not an answer, and you have the reads to go and get it. Read the plan, another result or the board too if that is what answering honestly takes.

Then answer THEIR question, in your own voice. The finding, the verdict, the numbers, the recommendation — first, in the opening sentence, before anything about the work itself. Size the answer to what there is to say: one line when one line is the whole truth of it, several paragraphs when the answer genuinely has parts. Where a document was produced, name its path AFTER the substance, never instead of it.

If the work fell short of what they asked, say that plainly and say what is missing. A shortfall named is worth more to them than a completion announced.

Never report completion. "It's done", "the task finished", "the report is ready above" are not answers — that the machinery finished is not news to somebody who asked a question. Neither is a wall of the work's own output with a sentence on top: that is the record, and it is already kept.

Say only what you actually read this turn. Every number is quoted from something in front of you, never worked out, rounded or remembered. If the result and a file disagree, the file the work wrote is what happened.

Speak entirely in their terms. The machinery's names for itself — node, leaf, graph, splice, subtree, worker, craft, charter, board, task — belong to the machinery, never to this answer.`

// deliveredRow reports whether one journal row is a finished job speaking. It is
// the same three-column reading every surface makes of a delivery: the resident
// posted it, it is anchored to a node, and no command of the person's produced
// it. Widening `answerable` to include it was the other way to wake the head and
// is the wrong one — the fold machinery builds a turn out of the person's
// CONTIGUOUS words, and a delivery folded into that run would swallow whatever
// they typed next.
func deliveredRow(message store.Message) bool {
	return message.Role == store.RoleSystem &&
		strings.TrimSpace(message.NodeID) != "" &&
		message.CommandSeq == 0 &&
		strings.TrimSpace(message.SessionID) != ""
}

// absorbable is the membrane: whose work this was. A job the person commissioned
// in a room is theirs and is owed an answer; the resident's own practice, its
// standing furniture and its territory bookkeeping are not, and a turn spent
// narrating those to a room nobody asked in is a turn spent on nobody.
func absorbable(node store.Node) bool {
	switch node.Group {
	case store.TerritoryGroup, charterNodeGroup, store.PracticeGroup:
		return false
	}
	return node.Parent == store.RootID &&
		node.Status == store.Done &&
		node.Provenance.Origin == store.OriginUser &&
		strings.TrimSpace(node.Provenance.SessionID) != ""
}

// absorbDelivery speaks one settled job back to the person who asked for it.
//
// Best effort by construction: a provider that refuses, a node that has gone,
// a room that cannot be read — none of them may stop the poll, because the
// delivery itself has already landed and the person has it. What they lose is a
// sentence, never the answer.
func (h *Head) absorbDelivery(ctx context.Context, message store.Message) {
	if h == nil || h.store == nil {
		return
	}
	node, found, err := h.store.Node(strings.TrimSpace(message.NodeID))
	if err != nil || !found || !absorbable(node) {
		return
	}
	// One answer per settle, claimed before anything is spent on it. The poll
	// wakes on a ROW, and a settled job can put more than one row of its own in
	// a room — the delivery itself, a rail note, a continuation line — each of
	// which reads as a delivery by the same three columns. Measured, one job
	// produced two absorption turns and both landed with answers_seq 0, so
	// neither superseded the other and the person read the same answer twice,
	// in two versions that disagreed. The seq is not the guard here and never
	// could be: nothing in this route is answering a numbered ask. What is
	// exactly once is the job, so the job's id is the claim.
	if !h.claimAbsorb(node.ID) {
		return
	}
	client, err := h.clientFor(message)
	if err != nil || client == nil {
		return
	}
	prompt, err := h.absorbPromptFor(message, node)
	if err != nil {
		return
	}
	answer := h.deliveryAnswer(ctx, client, message, prompt)
	// A provider that refused, or a turn with nothing to say, costs the person the
	// spoken answer and never the work: the delivery row is journaled, the job's
	// card draws it, and a read reaches it forever. Posting the raw result a
	// second time in the head's own voice is what this contract stopped doing.
	if answer == "" {
		return
	}
	// Unannotated, like every other ordinary reply: attribution in the thread is
	// reserved for a model the person asked for by name, and this turn is the
	// head talking in its own voice on whatever the room already runs.
	if err := h.postAgent(message.SessionID, answer, 0); err != nil {
		log.Printf("head absorb %s: %v", node.ID, err)
	}
}

// deliveryAnswer runs the bounded read-and-answer loop.
//
// It is the ordinary turn loop with its hands taken off: the same shape, the
// same tool-result protocol, the same one-runaway-bound discipline — and a belt
// that is the reads and nothing else, guarded twice. The definitions offered
// carry only reads, and a call to anything else is refused in the loop rather
// than dispatched, because a turn woken BY an outcome must not be able to start
// another one.
func (h *Head) deliveryAnswer(ctx context.Context, client Client, message store.Message, prompt string) string {
	streamed := provider.WithStreamSession(ctx, AbsorbStreamSession(message.SessionID))
	messages := []ai.Message{textMessage("system", absorbPrompt), textMessage("user", prompt)}
	definitions := beltReadDefinitions()
	run := &beltRun{head: h, user: message}
	spent := 0
	for turn := 0; turn < absorbToolCallCap+2; turn++ {
		response, err := client.CompleteWithMessages(streamed, messages, ai.WithTools(definitions))
		if err != nil || response == nil {
			return ""
		}
		calls := response.ToolCalls()
		if len(calls) == 0 {
			return strings.TrimSpace(response.Text())
		}
		messages = append(messages, ai.Message{
			Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: response.Text()}},
			ToolCalls: calls,
		})
		for _, call := range calls {
			name := call.Function.Name
			var body string
			switch {
			case !beltReadOnly(name):
				body = absorbHandless
			case spent >= absorbToolCallCap:
				body = absorbSpentReads
			default:
				spent++
				result, failed := run.execute(name, call.Function.Arguments)
				if failed {
					result = "ERROR: " + result
				}
				body = result
			}
			messages = append(messages, ai.Message{
				Role: "tool", ToolCallID: call.ID,
				Content: []ai.ContentPart{{Type: "text", Text: body}},
			})
		}
	}
	return ""
}

// deliveredText is the finished work as the person is owed it: the job's own
// settled summary, or the row that announced it when the summary has gone.
func deliveredText(node store.Node, message store.Message) string {
	if result := strings.TrimSpace(node.Summary); result != "" {
		return result
	}
	return strings.TrimSpace(message.Body)
}

// The relay that used to stand here — the head's frame, then the whole delivered
// text copied underneath it — is gone, and the incident it was written for is
// worth keeping in view. A job spent twelve leaves verifying twelve technical
// profiles against live documentation; the head then wrote two messages that
// both opened "Here are the 12 technical profiles:", disagreed with each other
// on plain fact, and matched the verified text on neither. Two generations
// disagreeing is proof they were generated.
//
// The relay answered that by copying rather than describing. It also meant the
// person's answer was the work's raw output, and the head's contribution one
// line of ceremony above it. What answers the same incident now is the thing
// that was actually missing: the turn reads the WHOLE deliverable, and the whole
// file behind it, before it says anything. A summary written over four kilobytes
// of a sixteen-kilobyte report was a paraphrase because three quarters of it had
// to be invented; a summary written over all of it is a summary. The verbatim
// text stays exactly where it always was — the delivery row, the job's card, any
// read — and is never posted twice.

// claimAbsorb takes the right to answer for one settled job, once.
//
// In process and not in the journal, because the thing being protected is one
// process's poll re-reading rows it has already acted on. A restart re-reads
// nothing — the cursor is durable — so a durable claim would buy nothing and
// would owe a schema.
func (h *Head) claimAbsorb(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if h == nil || nodeID == "" {
		return false
	}
	h.absorbedMu.Lock()
	defer h.absorbedMu.Unlock()
	if h.absorbed == nil {
		h.absorbed = map[string]bool{}
	}
	if h.absorbed[nodeID] {
		return false
	}
	h.absorbed[nodeID] = true
	return true
}

// absorbPromptFor assembles what the turn answers from — the four things the
// person listed: the conversation so far, their original ask, the work that was
// commissioned from it, and what came back.
//
// Their ask is carried explicitly rather than left to be found in the thread,
// because it may be far above the window by the time a long job lands, and it is
// the one thing this turn exists to answer. It comes off the job's own
// provenance, which is where the splice stamped their verbatim words.
func (h *Head) absorbPromptFor(message store.Message, node store.Node) (string, error) {
	recent, err := h.store.MessageTail(message.SessionID, threadWindowKeep)
	if err != nil {
		return "", fmt.Errorf("head absorb: read recent thread: %w", err)
	}
	kept := make([]store.Message, 0, len(recent))
	for _, prior := range recent {
		// The delivery row itself is quoted below in full; keeping it here as
		// well would put the result in the prompt twice.
		if prior.Seq != message.Seq && personRelevant(prior) {
			kept = append(kept, prior)
		}
	}
	var body strings.Builder
	body.WriteString("The conversation so far:\n" + h.renderThread(kept))
	if ask := strings.TrimSpace(node.Provenance.Intent); ask != "" {
		body.WriteString("\n\nWhat they asked for, in their own words:\n" + ask)
	}
	if title := strings.TrimSpace(node.Title); title != "" {
		// The id travels with the title because the reads need one. It is for the
		// tools and never for the sentence — a raw id said out loud is the
		// machinery talking about itself, which the prompt above forbids.
		body.WriteString("\n\nThe work that was done for it: " + title +
			" (id " + node.ID + ", for your reads — never say an id to them)")
	}
	result := strings.TrimSpace(node.Summary)
	if result == "" {
		result = strings.TrimSpace(message.Body)
	}
	// The result travels WHOLE. The four-kilobyte clip that used to stand here was
	// the single most expensive line in this file: a sixteen-kilobyte deliverable
	// arrived as its first quarter, and a model asked to speak about it produced
	// the other three quarters in its own words — which is exactly what a model
	// does with a sentence that stops mid-argument. What the loop reads is raw;
	// what it writes is composed; those are two different rules and only the
	// second one is about brevity.
	body.WriteString("\n\nWhat it delivered, in full:\n" + result)
	body.WriteString("\n\nNow answer what they asked. Open any file this names before you write " +
		"about what is in it — a result that says where the answer is has not given you the answer. " +
		"Lead with the substance; the path comes after it, and never instead of it.")
	return body.String(), nil
}

// absorbWait is how long the absorption turn may take before the poll gives up
// on it. The poll is the head's only mail loop: a provider hanging here would
// stop the person's next message being answered, and a sentence about finished
// work is never worth that.
const absorbWait = 45 * time.Second

func (h *Head) absorbDeliveryBounded(ctx context.Context, message store.Message) {
	bounded, cancel := context.WithTimeout(ctx, absorbWait)
	defer cancel()
	h.absorbDelivery(bounded, message)
}
