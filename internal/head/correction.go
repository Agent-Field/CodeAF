package head

import (
	"strings"
	"time"

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

// correctionDisputeLine is the sentence that outranks the predecessor's own
// account of itself.
//
// A leaf ended "Everything is verified. The browser builds cleanly, launches a
// Fyne window…" while the person watching it was typing "I keep getting could
// not load, no page is loading". The delivery gate believed the leaf, and the
// revision spawned from the user's words inherited that belief as an input: the
// previous attempt arrives as trusted context, and "verified" inside it reads as
// settled ground the revision may build on rather than the exact claim under
// dispute. Then it re-verifies nothing, and says "verified" again.
//
// So the block says which of the two accounts is evidence. It names no symptom
// and no phrase — what counts as a claim, and what would settle it, is the
// model's judgment on the words in front of it. It only fixes the standing:
// the user was there, the predecessor is a witness for itself.
const correctionDisputeLine = "Where the previous attempt claims something works, was tested, or was verified, " +
	"treat that claim as disputed rather than established — the user is reporting what actually happened when " +
	"they used it, and they outrank it. Check those claims yourself before repeating any of them."

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
	block.WriteString("\n" + correctionDisputeLine)
	return block.String()
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

// correctionAnchor is which delivered work a correction is about. Rivals is
// non-empty only when the two readings disagree and neither one deserves to win
// silently; the caller asks one plain question and the answer settles it.
type correctionAnchor struct {
	Job    store.Node
	Rivals []store.Node
}

// CorrectionQuiet bounds adjacency once more than one delivered job is in the
// window. With a single settled job, position needs no clock: a result is the
// last word until the user answers it, however long they take to read it, and
// that argument is still exactly right. With four, the last node-anchored line
// is as likely to be some other job's heartbeat as the deliverable being
// corrected — so stale position stops being evidence and becomes a question.
const CorrectionQuiet = 20 * time.Minute

// correctionTarget is the anchor, and it has redirection's two arms pointed at
// settled work instead of live work.
//
// The order used to be adjacency first, returning on the first hit, which made
// the vocabulary arm unreachable whenever any job had spoken. That is right with
// one job and wrong with four: "that's wrong, the test you added doesn't compile"
// names the work in its own words, and answering it with whichever job narrated
// most recently buys a paid revision of the wrong deliverable. So the words go
// first when the sentence has any — they are the deliberate signal — and position
// is the tiebreak for the follow-ups that carry no content at all. When the two
// arms name different work, neither is quietly preferred: one question settles
// it, which is the same reasoning the redirect path already applies in the same
// situation.
func (h *Head) correctionTarget(user store.Message, message string) (correctionAnchor, bool, error) {
	described, err := h.describedCorrections(message)
	if err != nil {
		return correctionAnchor{}, false, err
	}
	switch {
	case len(described) == 1:
		// The words name one delivered job and no other clears the floor. That is
		// the most deliberate signal this path ever gets, and whichever job
		// happened to narrate most recently does not outrank it.
		return correctionAnchor{Job: described[0]}, true, nil
	case len(described) > 1:
		return correctionAnchor{Job: described[0], Rivals: described}, true, nil
	}
	spoken, adjacentFound, err := h.settledAdjacency(user)
	if err != nil || !adjacentFound {
		return correctionAnchor{}, false, err
	}
	if len(spoken.Others) == 0 || spoken.Fresh(user) {
		return correctionAnchor{Job: spoken.Job}, true, nil
	}
	// Several jobs delivered into this window and the newest of them said its
	// piece long enough ago that being last proves nothing. The sentence carries
	// no words to break the tie, so the person does.
	return correctionAnchor{
		Job:    spoken.Job,
		Rivals: append([]store.Node{spoken.Job}, spoken.Others...),
	}, true, nil
}

// describedCorrections is the vocabulary arm: the delivered jobs the user's own
// words name, best first, at the same anchor floor redirection uses, because
// the doubt is the same doubt. One match is an answer; several are a question.
func (h *Head) describedCorrections(message string) ([]store.Node, error) {
	reference := redirectReference(message)
	if reference == "" {
		return nil, nil
	}
	targets, err := h.store.SearchSurgeryTargets(reference, false)
	if err != nil {
		return nil, err
	}
	named := make([]store.Node, 0, RedirectCandidateLimit)
	for _, target := range targets {
		if target.Score < RedirectAnchorScore || !correctable(target.Node) {
			continue
		}
		named = append(named, target.Node)
		if len(named) == RedirectCandidateLimit {
			break
		}
	}
	return named, nil
}

// settledSpeaker is what position alone can say: the delivered job whose own
// message is the last thing before this one, when it said it, and which other
// delivered jobs also spoke inside the same window.
type settledSpeaker struct {
	Job    store.Node
	Spoke  time.Time
	Others []store.Node
}

// Fresh reports that the last word is still recent enough to be the thing being
// answered. A line with no time on it is treated as fresh: the fixtures and the
// oldest journal rows carry none, and inventing staleness out of a missing
// timestamp would silently move work that has always resolved.
func (speaker settledSpeaker) Fresh(user store.Message) bool {
	if speaker.Spoke.IsZero() {
		return true
	}
	asked := user.Time
	if asked.IsZero() {
		asked = time.Now()
	}
	return asked.Sub(speaker.Spoke) <= CorrectionQuiet
}

// settledAdjacency is adjacencyTarget's settled twin: the job whose own message
// is the last thing said before this one. It reads the whole window rather than
// stopping at the first hit, because how many jobs delivered into this stretch of
// conversation is the difference between position being evidence and position
// being a coin toss.
func (h *Head) settledAdjacency(user store.Message) (settledSpeaker, bool, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return settledSpeaker{}, false, err
	}
	if len(recent) > AdjacencyMessageWindow {
		recent = recent[len(recent)-AdjacencyMessageWindow:]
	}
	var speaker settledSpeaker
	found := false
	seen := make(map[string]bool, 4)
	for index := len(recent) - 1; index >= 0; index-- {
		message := recent[index]
		if message.Role == store.RoleUser || strings.TrimSpace(message.NodeID) == "" {
			continue
		}
		node, ok := h.correctableOwner(message.NodeID)
		if !ok || seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		if !found {
			speaker.Job, speaker.Spoke, found = node, message.Time, true
			continue
		}
		speaker.Others = append(speaker.Others, node)
	}
	return speaker, found, nil
}

// correctableOwner walks the node that spoke up to the delivered job it belongs
// to. A part of a job speaking is the job speaking.
func (h *Head) correctableOwner(nodeID string) (store.Node, bool) {
	for depth := 0; depth < adjacencyAncestorDepth; depth++ {
		if nodeID = strings.TrimSpace(nodeID); nodeID == "" || nodeID == store.RootID {
			return store.Node{}, false
		}
		node, found, err := h.store.Node(nodeID)
		if err != nil || !found {
			return store.Node{}, false
		}
		if correctable(node) {
			return node, true
		}
		nodeID = node.Parent
	}
	return store.Node{}, false
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
