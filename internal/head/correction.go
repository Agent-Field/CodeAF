package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// "That's wrong" is the strongest quality signal a person ever emits, and it
// had no home. The recognizer written for that exact sentence lives in
// redirectCue and returns ("correction", true) for it — and recognizeRedirect
// then throws the reading away, because it requires live work and a delivered
// job is settled and folded within the same reconciler tick that announced it.
// Below that, the head's own routing law taught the model that "that's wrong"
// means retract a notebook belief, so the most likely reply to a factual
// correction of a report was "· let go —" about an unrelated preference. The
// correction fell off the end of the pipeline in three different ways.
//
// What it should be is not new machinery. The delivery gate already runs a
// revision pass over a rejected deliverable, handing the leaf the previous
// attempt and the critique as one input; a user's correction is that same
// shape, arriving from the only party whose opinion outranks the gate's. So a
// correction is a REVISION of the deliverable: the same job, its own words, its
// previous attempt, and what the user says is wrong with it.
//
// The seam is deliberately the smallest one that is honest. store.Command has
// no correction kind and adding one is a store change this does not need: a
// splice already carries a Target meaning "the prior work this one continues",
// the store already accepts a splice aimed at settled work (validateNodeCommand
// exempts splices from the status table), and the instruction is a durable
// payload. So the head journals a splice at the job that was wrong, carrying
// the user's verbatim words and — marked, so the resident can read it back —
// what was delivered. Everything downstream that improves this is additive and
// named in the report: inheriting the predecessor's workspace so the revision
// edits report.md in place rather than writing a second one, and feeding the
// critique to the distiller, whose prompt already asks for exactly this input
// and has never had a wire to it.

const (
	// CorrectionPrefix marks the block a correction splice carries. The
	// resident reads it to know that this splice is a revision of its target
	// rather than a new job that merely follows one.
	CorrectionPrefix = "Correcting delivered work:"
	// correctionAskTail closes the one clarifying question this path may ask.
	// It is the durable rail for the answer: the user replies to it in ordinary
	// words with no cue in them at all, and this is how the next message is
	// recognized as the critique rather than routed as a fresh ask.
	correctionAskTail = "What's wrong with it?"
	// correctionPreviousBytes bounds the previous attempt carried into the
	// revision. It matches the deep slice's per-job budget: enough to revise
	// against, and the files line beside it is how the whole document is read.
	correctionPreviousBytes = 1200
	// correctionAskLineBytes bounds the deliverable quoted back in the question.
	// One clause, so the question stays a question rather than a re-delivery.
	correctionAskLineBytes = 160
)

// IsCorrection reports that a splice is a revision of the work it targets.
// Exported for the resident half, which owns what happens next.
func IsCorrection(instruction string) bool {
	return strings.Contains(instruction, CorrectionPrefix)
}

// SpliceCorrection is the one way a correction is written down. The user's
// words lead and are never touched — the same law every other instruction
// obeys — and the deterministic block follows, in the idiom attached documents
// and quality words already use: a compiler that ignores the prompt cannot lose
// what the sentence was about.
func SpliceCorrection(words string, job store.Node, previous string, files []string) string {
	var block strings.Builder
	block.WriteString(strings.TrimSpace(words))
	block.WriteString("\n\n" + CorrectionPrefix + " " + job.ID)
	if label := surgeryTargetLabel(job); label != "" && label != job.ID {
		block.WriteString(" (" + label + ")")
	}
	if previous = strings.TrimSpace(previous); previous != "" {
		block.WriteString("\nWhat was delivered:\n" + truncateBytes(previous, correctionPreviousBytes))
	}
	if len(files) > 0 {
		block.WriteString("\nFiles it wrote: " + strings.Join(files, ", "))
	}
	block.WriteString("\n\nThis is a revision of that deliverable, not a second opinion about it: " +
		"produce it again with the correction above applied, and keep everything the user did not object to.")
	return block.String()
}

// manageCorrection reads the message as a rejection of work already delivered.
//
// It runs after manageRedirect on purpose. Redirection owns work still in
// flight — that is a plan edit and it must not become a re-delivery — so this
// path only ever sees a correction that redirection declined, which is exactly
// the case where the job is over and there is a deliverable to be wrong about.
func (h *Head) manageCorrection(user store.Message) (bool, error) {
	message := strings.TrimSpace(user.Body)
	if message == "" {
		return false, nil
	}
	asked, waiting, err := h.openCorrectionAsk(user)
	if err != nil {
		return false, err
	}
	job, anchored := asked, waiting
	if !waiting {
		if cue, cued := redirectCue(message); !cued || cue != "correction" {
			return false, nil
		}
		// A question about a deliverable is a question. "sorry, what was that?"
		// carries a correction cue and rejects nothing, and turning it into a
		// re-run would spend money on an answer the reader already has — so the
		// question mark hands it back to the readers that answer questions.
		if strings.HasSuffix(message, "?") {
			return false, nil
		}
		job, anchored, err = h.correctionTarget(user, message)
		if err != nil || !anchored {
			return false, err
		}
	}
	previous := nodeResult(job)
	if strings.TrimSpace(previous) == "" {
		// Nothing was delivered, so there is nothing to be wrong. Whatever this
		// sentence is about, it is not a revision of this job.
		return false, nil
	}
	if redirectReference(message) == "" {
		if waiting {
			// The answer to the question was as contentless as the sentence that
			// raised it. Asking twice is a loop; the router can hold a
			// conversation and this path cannot.
			return false, nil
		}
		return true, h.askWhatIsWrong(user, job, previous)
	}
	return true, h.requestCorrection(user, job, message, previous)
}

// requestCorrection journals the revision and says which deliverable it is
// about. The receipt names the work rather than the mechanism, and it promises
// only what the command actually does: the same job, done again, with the
// correction in hand.
func (h *Head) requestCorrection(user store.Message, job store.Node, message, previous string) error {
	command, err := h.store.RequestCommand(store.Command{
		SessionID:   user.SessionID,
		Kind:        store.CommandSplice,
		Target:      job.ID,
		Instruction: SpliceCorrection(message, job, previous, resultFiles(job)),
		Attachments: append([]string(nil), user.Attachments...),
	})
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	// The receipt promises exactly what the command carries and no more: the
	// deliverable it is about, and that the previous version goes back in with
	// the correction. Whether the revision lands in the predecessor's own
	// workspace is the resident's half and is not claimed here.
	return h.postAgent(user.SessionID,
		"Taking that back to "+surgeryTargetLabel(job)+" — redoing it with the previous version and your correction in hand.",
		command.Seq)
}

// askWhatIsWrong is the one question this path may ask, and it exists because
// the alternative is worse than a question. "This is wrong" with no specifics
// compiles into an assumption wearing a receipt — a re-guess at the same job
// with nothing new in it — or, worse, into a brand-new unrelated job. Naming
// what was delivered is what makes it one honest question rather than a shrug:
// the user learns which thing is being talked about in the same breath they are
// asked what is wrong with it.
//
// It is anchored to the node so the answer is adjacent to this job for every
// reader that already understands adjacency, and it ends in a fixed sentence so
// the next message — which will carry no cue at all — is read as the critique.
func (h *Head) askWhatIsWrong(user store.Message, job store.Node, previous string) error {
	summary := truncateBytes(firstLine(previous), correctionAskLineBytes)
	body := "That was " + surgeryTargetLabel(job)
	if summary != "" {
		body += " — " + summary
	}
	body += ". " + correctionAskTail
	_, err := h.store.PostMessage(store.Message{
		SessionID: user.SessionID,
		Role:      store.RoleAgent,
		Body:      body,
		NodeID:    job.ID,
	})
	return err
}

// openCorrectionAsk reports that the last thing said to the user was this
// path's own question, and which job it was about. A question the user is
// answering right now outranks every cue test: they are not going to repeat
// "that's wrong", they are going to say what is wrong.
func (h *Head) openCorrectionAsk(user store.Message) (store.Node, bool, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return store.Node{}, false, err
	}
	for index := len(recent) - 1; index >= 0; index-- {
		message := recent[index]
		if message.Role == store.RoleUser {
			continue
		}
		if message.Role != store.RoleAgent ||
			!strings.HasSuffix(strings.TrimSpace(message.Body), correctionAskTail) ||
			strings.TrimSpace(message.NodeID) == "" {
			// Something else was the last word, so no question is open.
			return store.Node{}, false, nil
		}
		node, found, err := h.store.Node(message.NodeID)
		if err != nil || !found || !beltAddressable(node) {
			return store.Node{}, false, err
		}
		return node, true, nil
	}
	return store.Node{}, false, nil
}

// correctionTarget is the anchor, and it has redirection's two arms pointed at
// settled work instead of live work.
//
// Adjacency leads, because a correction is a reply: the deliverable was posted
// into the thread anchored to its own node, and the sentence that follows it is
// about it whether or not it borrows a single one of its words. Vocabulary is
// the fallback for the case adjacency cannot cover — a correction typed after
// the conversation has moved on — and it clears the same anchor floor
// redirection uses, because the doubt is the same doubt.
func (h *Head) correctionTarget(user store.Message, message string) (store.Node, bool, error) {
	if node, found, err := h.settledAdjacency(user); err != nil || found {
		return node, found, err
	}
	reference := redirectReference(message)
	if reference == "" {
		return store.Node{}, false, nil
	}
	targets, err := h.store.SearchSurgeryTargets(reference, false)
	if err != nil {
		return store.Node{}, false, err
	}
	for _, target := range targets {
		if target.Score < RedirectAnchorScore || !correctable(target.Node) {
			continue
		}
		return target.Node, true, nil
	}
	return store.Node{}, false, nil
}

// settledAdjacency is adjacencyTarget's settled twin: the job whose own message
// is the last thing said before this one. It carries no freshness bound, and
// that is the difference. A running job speaks on a heartbeat, so position
// there only means something for a few minutes; a delivered result is the last
// word until the user answers it, however long they take to read it. The thread
// window is the whole bound, which is the same thing as saying: while it is
// still on screen, it is still what the conversation is about.
func (h *Head) settledAdjacency(user store.Message) (store.Node, bool, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return store.Node{}, false, err
	}
	if len(recent) > AdjacencyMessageWindow {
		recent = recent[len(recent)-AdjacencyMessageWindow:]
	}
	for index := len(recent) - 1; index >= 0; index-- {
		message := recent[index]
		if message.Role == store.RoleUser || strings.TrimSpace(message.NodeID) == "" {
			continue
		}
		nodeID := message.NodeID
		for depth := 0; depth < adjacencyAncestorDepth; depth++ {
			if nodeID = strings.TrimSpace(nodeID); nodeID == "" || nodeID == store.RootID {
				break
			}
			node, found, readErr := h.store.Node(nodeID)
			if readErr != nil || !found {
				break
			}
			if correctable(node) {
				return node, true, nil
			}
			nodeID = node.Parent
		}
	}
	return store.Node{}, false, nil
}

// correctable is the membrane: the user's own work, finished, and not the
// permanent spine. Cancelled work is included deliberately — "that's wrong, I
// didn't want it stopped" is a correction of the same shape.
func correctable(node store.Node) bool {
	if node.ID == store.RootID || !beltAddressable(node) {
		return false
	}
	switch node.Status {
	case store.Done, store.Failed, store.Cancelled:
		return true
	}
	return false
}
