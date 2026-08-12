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
// Three bounds make it affordable and honest.
//   - It fires for work the PERSON asked for, in a room, and never for the
//     resident's own practice, sentinels or standing furniture. Self-directed
//     work has nobody waiting on an answer, and paying a turn to narrate it to
//     an empty room is the exact cost this system is careful about.
//   - It fires once, off the delivery row the resident already posts, and the
//     poll's own cursor is what makes it once.
//   - It has no tools. The work is finished; there is nothing left to do but
//     say what it came to. The belt's definitions are the largest part of an
//     ordinary turn's prompt and none of them can be reached from here.
const (
	// absorbMaxTokens bounds the absorption turn. It is the aside's cap for the
	// same reason: this is one or two sentences over a result that is already on
	// screen, and a budget big enough to re-paste the deliverable is a budget
	// that invites re-pasting it.
	absorbMaxTokens = 600
	// absorbResultBytes bounds how much of the delivered result is read back to
	// the head. Enough to absorb what it says; never so much that a long
	// deliverable becomes the prompt.
	absorbResultBytes = 4000
	// absorbStreamSuffix keys the absorption turn's deltas apart from the room's
	// own turn, exactly as an aside's are: the person may be typing while this
	// runs, and their reply's stream must not have this one's words in it.
	absorbStreamSuffix = "#delivered"
)

// AbsorbStreamSession is the stream key the absorption turn's deltas carry.
func AbsorbStreamSession(sessionID string) string { return sessionID + absorbStreamSuffix }

// absorbPrompt is the absorption turn's own system message.
//
// It is a separate prompt for the aside's cache reason — a prompt that shared
// the orchestrator's prefix would compete with it for one cache entry — and
// because it is a genuinely different job. The orchestrator's prompt is mostly
// about what its hands are; this one has none, and its whole content is what to
// say when the thing they asked for has landed.
const absorbPrompt = `The work the person asked for has finished. The finished result will be handed to them WHOLE and word for word, directly underneath whatever you write, by the code that posts this message. You are not writing the answer; the answer exists, it was verified by the work, and it is going out intact.

You write the sentence in front of it. One sentence, occasionally two. Say what the result means for what they actually asked — the finding, the verdict, the bottom line, or simply that here is the thing they asked for and what shape it came out.

Never restate, summarise, paraphrase, re-derive or continue the result. Anything you say about its substance that the result does not itself say is something you invented, and it will sit one line above the verified text contradicting it. If a fact is worth telling them, it is already below you. Where the result names files it wrote, you may say once where the material lives.

If the result fell short of what they asked for, say that instead, in one sentence, plainly and without apology — what is missing is more useful to them than what is present.

If you have nothing worth adding, say nothing at all: an empty reply is correct here, and the result still reaches them.

Speak entirely in their terms. The machinery's names for itself — node, leaf, graph, splice, worker, craft, charter, board, task — belong to the machinery, never to this sentence.`

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
	delivered := deliveredText(node, message)
	framing := ""
	response, err := client.CompleteWithMessages(
		provider.WithStreamSession(ctx, AbsorbStreamSession(message.SessionID)),
		[]ai.Message{textMessage("system", absorbPrompt), textMessage("user", prompt)},
		ai.WithMaxTokens(absorbMaxTokens))
	if err == nil && response != nil {
		framing = strings.TrimSpace(response.Text())
	}
	// The frame is optional and the deliverable is not. A provider that refused,
	// or a turn with nothing to add, costs the person a sentence of context and
	// never the answer — which is the whole difference between relaying and
	// re-authoring, stated at the one seam where the two could be confused.
	body := RelayDelivery(framing, delivered)
	if body == "" {
		return
	}
	// Unannotated, like every other ordinary reply: attribution in the thread is
	// reserved for a model the person asked for by name, and this turn is the
	// head talking in its own voice on whatever the room already runs.
	if err := h.postAgent(message.SessionID, body, 0); err != nil {
		log.Printf("head absorb %s: %v", node.ID, err)
	}
}

// deliveredText is the finished work as the person is owed it: the job's own
// settled summary, or the row that announced it when the summary has gone.
func deliveredText(node store.Node, message store.Message) string {
	if result := strings.TrimSpace(node.Summary); result != "" {
		return result
	}
	return strings.TrimSpace(message.Body)
}

// RelayDelivery composes what the head says when a job lands: its own short
// frame, and then the delivered work itself, verbatim.
//
// This is the closure contract's delivery half made structural. The head owns
// the discourse — one mouth — and for a while that was read as licence to
// ANSWER the ask a second time out of its own knowledge, with the researched
// result merely shown to it and then bounded to four kilobytes on the way in.
// Measured, a job that spent twelve leaves verifying twelve profiles against
// live documentation was followed by two head messages that both opened "Here
// are the 12 technical profiles:", contradicted each other on plain fact —
// Chroma stores Parquet files, Chroma stores SQLite metadata — and matched the
// verified text on neither. Two generations disagreeing is proof they were
// generated; nothing relayed can disagree with itself.
//
// So the substance travels and only the substance: the frame is the head's, the
// body is the work's, and the body is copied rather than described. Where the
// two together will not fit one message, the FRAME is what gives way, because a
// missing sentence of context costs the person a courtesy and a missing
// paragraph of the deliverable costs them the answer.
func RelayDelivery(framing, delivered string) string {
	framing, delivered = strings.TrimSpace(framing), strings.TrimSpace(delivered)
	if delivered == "" {
		return framing
	}
	if framing == "" || strings.Contains(delivered, framing) {
		return truncateBytes(delivered, store.MaxMessageBytes)
	}
	if len(framing)+len(relayJoin)+len(delivered) > store.MaxMessageBytes {
		return truncateBytes(delivered, store.MaxMessageBytes)
	}
	return framing + relayJoin + delivered
}

// relayJoin separates the head's sentence from the work's own words. A blank
// line and nothing else: a heading here would be the head announcing the
// deliverable rather than handing it over.
const relayJoin = "\n\n"

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
		body.WriteString("\n\nThe work that was done for it: " + title)
	}
	result := strings.TrimSpace(node.Summary)
	if result == "" {
		result = strings.TrimSpace(message.Body)
	}
	// Only the opening of the result travels into the prompt, and that is now
	// honest rather than a compromise. The turn writes a frame, so it needs
	// enough of the work to know what the work concluded; it does not need the
	// work, because the work is what the code posts underneath it. For as long
	// as this turn was expected to ANSWER, the same bound was a quiet
	// mutilation — a sixteen-kilobyte deliverable arriving as its first quarter
	// and a model told to produce the rest in its own words, which is exactly
	// what it did.
	body.WriteString("\n\nThe opening of what it delivered. The WHOLE of this text, " +
		"not this excerpt, is posted verbatim directly below your sentence:\n" +
		truncateBytes(result, absorbResultBytes))
	// "Say what this means" alone produced turns that pointed at the card —
	// "the full report is above" — which is a receptionist's answer, and the
	// one thing this turn must never be (user-reported, 2026-08-11). Pointing
	// is still wrong, and it is wrong for the opposite reason now: there is
	// nowhere to point, because the thing itself is directly beneath the line
	// being written.
	body.WriteString("\n\nWrite only the sentence that goes in front of it — what this " +
		"means for what they asked. Do not reproduce, summarise or continue the " +
		"result; it follows your words in full. Never write \"the report is above\", " +
		"\"see the file\" or \"the card has it\": there is nothing to point at, the " +
		"work is right here.")
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
