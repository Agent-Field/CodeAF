package head

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The receipt wake — the head hearing back about a change it made.
//
// A head turn journals a command and answers in the same breath: "cancelling
// that", "handing this over". Both sentences are written BEFORE anything has
// been decided, because the funnel is asynchronous by design — the tasker
// compiles the ask, the revision judge reads the words, the store's legality
// table has the last say, and all of that happens after the turn has ended. So
// the head has always spoken about a future it could not see, and two things
// went wrong there, repeatedly:
//
//   - The commit-before-shape defect. "I've put that in hand" is true and empty.
//     What the person wants to know is what was UNDERSTOOD — the reading the
//     compiler settled on, the shape it chose — and that arrives one tick later
//     as a receipt in a machine voice, or is filed on a card and never spoken.
//   - The stale correction. The head says "cancelling that job"; the command is
//     then refused because the job finished a second earlier. The refusal is
//     posted by the reconciler in its own words, underneath a sentence of the
//     head's that is now false, and nobody owns the contradiction.
//
// So the settlement wakes the head. The absorb path (absorb.go) already proved
// the shape for finished WORK; this is the same shape for a settled COMMAND, and
// between them the head hears back about everything it starts. The consequence
// worth naming: "I'll tell you what it decides" stopped being a promise the
// machinery cannot keep, which is why the lexical promise-policing that used to
// stand in front of every reply is gone rather than merely relaxed.
const (
	// receiptStreamSuffix keys the wake's deltas apart from the room's own turn,
	// exactly as the delivery turn's are: the person may be typing while this
	// runs, and their reply's stream must not have this one's words in it.
	receiptStreamSuffix = "#receipt"
	// receiptWait is how long the wake may take before the poll gives up on it.
	// The poll is the head's only mail loop; a provider hanging here would stop
	// the person's next message being answered, and a sentence about a change
	// that has already been applied is never worth that.
	receiptWait = 45 * time.Second
)

// ReceiptStreamSession is the stream key the wake's deltas carry.
func ReceiptStreamSession(sessionID string) string { return sessionID + receiptStreamSuffix }

// receiptRow reports whether one journal row is a command settling.
//
// It is the narrowest reading that finds all three shapes settleCommand posts:
// an applied receipt filed on the job's card (a system row with a node and no
// session), an applied receipt with nowhere to be filed (a system row in the
// room), and a refusal (an agent row in the room, because a refusal always
// speaks). What they share is the only column that matters here — the command
// they settle. Nothing else in the journal carries one.
func receiptRow(message store.Message) bool {
	return message.CommandSeq != 0 && message.Role != store.RoleUser &&
		strings.TrimSpace(message.Body) != ""
}

// receiptInterprets reports whether a settled command of this kind comes back
// carrying something the head could not have known when it spoke.
//
// This is the triviality guard, and it is drawn by KIND rather than by reading
// the receipt's words, because the question is not how the sentence is worded —
// it is whether anything between the funnel and the receipt exercised judgment.
// A splice was compiled: its receipt carries the reading, the assumptions, the
// craft it recognised, the shape it chose. A redirect and an expedite went past
// the revision judge: their receipts say what actually happened to the remaining
// plan, which is the one thing the head cannot predict when it promises a
// change. An amendment was attached to live work by the same route.
//
// A pause, a resume, a cancel, a reprioritize, a charter retirement: the store
// did exactly what the verb says and the receipt says so. The head already said
// that sentence, better, in the person's own words. Waking to say it again is
// the duplication the whole three-class law exists to prevent.
//
// A REFUSAL of any kind wakes regardless — see wakeReceipt. Being wrong is
// always news.
func receiptInterprets(kind store.CommandKind) bool {
	switch kind {
	case store.CommandSplice, store.CommandAmend, store.CommandRedirect,
		store.CommandExpedite, store.CommandCraftRun:
		return true
	}
	return false
}

// expectReceipt records that a head turn journaled one command, so its
// settlement is something this head owes a sentence for.
//
// In process and not in the journal, for claimAbsorb's reason exactly: what is
// being protected is one process's poll acting on rows, and a restart re-reads
// nothing because the cursor is durable. What a restart loses is the wake for a
// command journaled seconds before it — the person keeps the receipt itself,
// which is the record either way.
//
// The map holds one int per command a head turn has issued and not yet heard
// back about; nothing in a process's lifetime makes that a size worth managing.
func (h *Head) expectReceipt(seq int64) {
	if h == nil || seq == 0 {
		return
	}
	h.receiptMu.Lock()
	defer h.receiptMu.Unlock()
	if h.awaitingReceipt == nil {
		h.awaitingReceipt = map[int64]bool{}
	}
	h.awaitingReceipt[seq] = true
}

// claimReceipt takes the right to speak for one settled command, once, and
// answers "did a head turn journal this one" in the same operation.
//
// Both questions have to be one question. A command fired from a page — the task
// room's own cancel button, a craft run started from a card — is answered where
// it was fired, and the head appearing in the conversation to narrate a button
// the person just pressed is the same unbidden second voice this whole design is
// careful about. So the claim is the registration: no registration, no wake.
func (h *Head) claimReceipt(seq int64) bool {
	if h == nil || seq == 0 {
		return false
	}
	h.receiptMu.Lock()
	defer h.receiptMu.Unlock()
	if !h.awaitingReceipt[seq] {
		return false
	}
	delete(h.awaitingReceipt, seq)
	return true
}

// receiptWakePrompt is the wake turn's own system message.
//
// It is a separate prompt for the delivery turn's reason — a prompt sharing the
// orchestrator's prefix would compete with it for one cache entry — and because
// it is a genuinely different job. This turn has no hands and one question in
// front of it: what became of the thing I said was happening?
const receiptWakePrompt = `A change you put in hand a moment ago has come back settled. You are the one who told them it was happening, so you are the one who says what actually became of it.

In front of you: what they asked for, what you said when you handed it over, and what the workforce came back with — how it read the ask, the shape it chose, what it actually changed.

Write one or two sentences, in your own voice, saying what is now true. How they were understood, and what is under way: "it's underway — read as a competitor sweep on the three names, going at it in three steps". Where the reading differs from what they asked for, that difference is the news and goes first, so they can correct it while correcting is still cheap.

If it was REFUSED, correct yourself plainly. You said it was happening and it is not; say what is true instead, in one sentence, without apology and without explaining the machinery.

Say nothing the receipt does not say — the shape, the reading and the count are its facts, not yours to round or improve. Never repeat what you already said a moment ago; they read it, and a sentence that only says it again is noise. If the receipt genuinely adds nothing to what they already know, reply with nothing at all: an empty reply is correct here and costs them nothing.

Speak entirely in their terms. The machinery's names for itself — node, leaf, graph, splice, subtree, worker, craft, charter, board, task, command — belong to the machinery, never to this sentence.`

// wakeReceiptBounded is the poll's door onto the wake, under a clock.
func (h *Head) wakeReceiptBounded(ctx context.Context, message store.Message) {
	bounded, cancel := context.WithTimeout(ctx, receiptWait)
	defer cancel()
	h.wakeReceipt(bounded, message)
}

// wakeReceipt speaks one settled command back to the person it was made for.
//
// Best effort by construction, like the delivery turn: a provider that refuses,
// a command that cannot be read, a room that has gone — none of them may stop
// the poll, because the change itself has already been applied or refused and
// the receipt is journaled either way. What is lost is a sentence.
func (h *Head) wakeReceipt(ctx context.Context, message store.Message) {
	if h == nil || h.store == nil {
		return
	}
	command, found, err := h.store.CommandBySeq(message.CommandSeq)
	if err != nil || !found || command.Status == store.CommandPending {
		return
	}
	room := strings.TrimSpace(command.SessionID)
	if room == "" {
		return
	}
	rejected := command.Status == store.CommandRejected
	// The two gates on what is worth a turn. A kind the head does not speak for
	// has its receipt AS the answer — receiptVoice posts it in the agent's own
	// voice — and a second sentence over the top of it is the thread talking to
	// itself. Of the kinds the head does speak for, only the ones that were
	// interpreted on the way through carry news; the rest were done exactly as
	// asked, and the head already said so in better words.
	if !rejected && (!resident.HeadSpeaksFor(command.Kind) || !receiptInterprets(command.Kind)) {
		return
	}
	// Claimed before anything is spent on it, and the claim is also the proof
	// that a head turn is what journaled this command.
	if !h.claimReceipt(command.Seq) {
		return
	}
	client, err := h.clientFor(store.Message{SessionID: room})
	if err != nil || client == nil {
		return
	}
	prompt, err := h.receiptWakePromptFor(command, message, rejected)
	if err != nil {
		return
	}
	response, err := client.CompleteWithMessages(
		provider.WithStreamSession(ctx, ReceiptStreamSession(room)),
		[]ai.Message{textMessage("system", receiptWakePrompt), textMessage("user", prompt)})
	if err != nil || response == nil {
		return
	}
	// Nothing to add is a correct outcome and posts nothing. The receipt is
	// journaled, the card carries it, and a sentence that only repeats what the
	// head said a minute ago is the duplication this wake is bounded against.
	said := strings.TrimSpace(response.Text())
	if said == "" {
		return
	}
	// Posted with no command seq of its own. The head's ORIGINAL line already
	// carries this command's number, and that is what ties a reply to the work on
	// the surfaces that draw cards from it; a second row wearing the same number
	// would overwrite the card's account of how the ask was read with a sentence
	// written about the same thing one tick later.
	if err := h.postAgent(room, said, 0); err != nil {
		log.Printf("head receipt wake %d: %v", command.Seq, err)
	}
}

// receiptWakePromptFor assembles what the wake answers from: the conversation
// (which holds both their ask and the head's own line about it), the words the
// command carried, and the receipt itself.
//
// The receipt travels whole. It is one message by construction — the reconciler
// bounds it at the message limit before journaling it — and it is the entire
// substance of this turn, so there is nothing here for a byte budget to buy.
func (h *Head) receiptWakePromptFor(command store.Command, receipt store.Message, rejected bool) (string, error) {
	room := strings.TrimSpace(command.SessionID)
	recent, err := h.store.MessageTail(room, threadWindowKeep)
	if err != nil {
		return "", fmt.Errorf("head receipt wake: read recent thread: %w", err)
	}
	kept := make([]store.Message, 0, len(recent))
	for _, prior := range recent {
		if personRelevant(prior) {
			kept = append(kept, prior)
		}
	}
	var body strings.Builder
	body.WriteString("The conversation so far, ending with what you yourself said when you handed this over:\n" +
		h.renderThread(kept))
	if words := strings.TrimSpace(command.Instruction); words != "" {
		body.WriteString("\n\nWhat you handed over, in the words you sent with it:\n" + words)
	}
	if rejected {
		body.WriteString("\n\nIt was REFUSED. Nothing changed. The reason given:\n" +
			strings.TrimSpace(receipt.Body))
		body.WriteString("\n\nYou have already told them this was happening. Correct that, plainly, in one sentence.")
		return body.String(), nil
	}
	body.WriteString("\n\nIt was applied, and this is what the workforce made of it — its own reading, " +
		"the shape it chose, and what it changed:\n" + strings.TrimSpace(receipt.Body))
	body.WriteString("\n\nSay what is now true, in one or two sentences of your own. " +
		"Not that it was received — what it was understood to be, and what is under way.")
	return body.String(), nil
}
