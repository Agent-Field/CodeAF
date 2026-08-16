package head

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// RedirectAnchorScore is the BM25 floor a live job must clear before the
	// user's words are read as steering for it. It sits at roughly one solid
	// content-word match against a job's own brief: below that the overlap is
	// as likely to be a coincidence of vocabulary as a reference to the work,
	// and a wrong redirection edits a plan the user never meant to touch.
	RedirectAnchorScore = 0.4
	// RedirectCandidateLimit bounds the disambiguation question. More than a
	// handful of choices is not a question, it is a list.
	RedirectCandidateLimit = 4
	// AdjacencyMessageWindow is how far back the thread is read for the job
	// that spoke last. It is the floor of the router's own thread window — the
	// count the prompt is guaranteed to still carry whatever the big-step
	// truncation has done — because a line the model still carries is a line
	// the user can still be answering, and anything further back is history.
	AdjacencyMessageWindow = threadWindowKeep
	// AdjacencyQuiet is how long a job's own line stays the thing being
	// answered. A running job speaks on a two-minute heartbeat, so two of them
	// is the span in which nothing newer has been said; past that the thread
	// has moved on and mere position proves nothing about what the user means.
	AdjacencyQuiet = 4 * time.Minute
	// adjacencyAncestorDepth bounds the walk from the node that spoke up to the
	// live job it belongs to. A job is a root and its parts, so this is slack.
	adjacencyAncestorDepth = 8
)

// adjacencyTarget is the anchor's discourse arm: the live job of the user's own
// whose message — progress, narration, a delivery, a question — is the last
// thing said before this one. It is bounded twice, by how far the thread window
// reaches and by how long a line stays fresh, because position only means
// anything while the line is still what the conversation is about.
func (h *Head) adjacencyTarget(user store.Message, active []store.SurgeryTarget) (store.SurgeryTarget, bool, error) {
	if h == nil || h.store == nil || len(active) == 0 {
		return store.SurgeryTarget{}, false, nil
	}
	live := make(map[string]store.SurgeryTarget, len(active))
	for _, target := range active {
		live[target.Node.ID] = target
	}
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return store.SurgeryTarget{}, false, err
	}
	if len(recent) > AdjacencyMessageWindow {
		recent = recent[len(recent)-AdjacencyMessageWindow:]
	}
	spoken := user.Time
	if spoken.IsZero() {
		spoken = time.Now()
	}
	for index := len(recent) - 1; index >= 0; index-- {
		message := recent[index]
		if message.Role == store.RoleUser || strings.TrimSpace(message.NodeID) == "" {
			continue
		}
		if !message.Time.IsZero() && spoken.Sub(message.Time) > AdjacencyQuiet {
			// The window closed, and the thread is in order, so everything
			// before this is older still. Nothing here is adjacent to anything.
			return store.SurgeryTarget{}, false, nil
		}
		if target, ok := h.adjacencyOwner(message.NodeID, live); ok {
			return target, true, nil
		}
	}
	return store.SurgeryTarget{}, false, nil
}

// adjacencyOwner walks the node that spoke up to the live job it belongs to. A
// part of a job speaking is the job speaking; the user answers the work, not
// the step.
func (h *Head) adjacencyOwner(nodeID string, live map[string]store.SurgeryTarget) (store.SurgeryTarget, bool) {
	for depth := 0; depth < adjacencyAncestorDepth; depth++ {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" || nodeID == store.RootID {
			return store.SurgeryTarget{}, false
		}
		if target, ok := live[nodeID]; ok {
			return target, true
		}
		node, found, err := h.store.Node(nodeID)
		if err != nil || !found {
			return store.SurgeryTarget{}, false
		}
		nodeID = node.Parent
	}
	return store.SurgeryTarget{}, false
}

// activeUserJobs is the person's own work, still open. It is what every reading
// about work already underway is measured against, and a quiet graph can never
// turn a sentence into one.
//
// The membrane is beltAddressable and nothing else, which is the same membrane
// the board sits behind. It used to be `Origin == OriginUser` on top of it, and
// the extra conjunct was a bug with a transcript: a charter-fired job carries
// OriginTrigger, so it appeared on the board, was described in one sentence, and
// was then invisible to every reading about live work in the next. Trigger work
// IS the person's — it came from a rule they ratified — and one board must not
// disagree with the readings printed underneath it.
func (h *Head) activeUserJobs() ([]store.SurgeryTarget, error) {
	targets, err := h.store.SearchSurgeryTargets("", false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		return nil, err
	}
	active := make([]store.SurgeryTarget, 0, len(targets))
	for _, target := range targets {
		if beltAddressable(target.Node) && target.Node.ID != store.RootID {
			active = append(active, target)
		}
	}
	return active, nil
}

func (h *Head) rankRedirectTargets(message string, active []store.SurgeryTarget) ([]store.SurgeryTarget, error) {
	reference := redirectReference(message)
	if reference == "" {
		return nil, nil
	}
	matches, err := h.store.SearchSurgeryTargets(reference, false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(active))
	for _, target := range active {
		live[target.Node.ID] = true
	}
	ranked := make([]store.SurgeryTarget, 0, len(matches))
	for _, match := range matches {
		if live[match.Node.ID] {
			ranked = append(ranked, match)
		}
	}
	return ranked, nil
}

func (h *Head) requestRedirect(ctx context.Context, user store.Message, target, message string) error {
	return h.requestRevision(ctx, user, store.CommandRedirect, target, message)
}

// requestRevision journals one mid-flight revision and says so. Both verbs carry
// the user's words verbatim and differ only in what the reconciler does with
// them: change the work, or hurry it.
//
// The acknowledgement used to be withheld here, on the argument that the only
// honest receipt is the one that knows what actually changed in the plan. That
// receipt does get written a moment later by the reconciler — and it is filed
// under the job's own card, where ambient progress lives and the thread does
// not look. So the route was silent to the only reader that mattered. What is
// said now is the handoff and nothing more, which is exactly what is true at
// this moment; the counts still follow from the party that knows them.
func (h *Head) requestRevision(ctx context.Context, user store.Message, kind store.CommandKind, target, message string) error {
	seq, err := h.journalRevision(user, kind, target, message)
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.speakRevision(ctx, user, kind, target, seq)
}

// journalRevision is requestRevision's returning half. The toolbelt needs the
// seq to tie its reply to durable work, and the failure to report back to the
// model rather than to the user.
func (h *Head) journalRevision(user store.Message, kind store.CommandKind, target, message string) (int64, error) {
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind,
		Target: target, Instruction: strings.TrimSpace(message),
	})
	if err != nil {
		return 0, err
	}
	return command.Seq, nil
}

// applyRedirectOption settles every redirection answer: which job the words
// were for, and whether a running leaf the revision wanted gone may be stopped.
func (h *Head) applyRedirectOption(ctx context.Context, user store.Message, option store.QuestionOption) (bool, error) {
	action, target, message, ok := store.DecodeRedirectOption(option.Value)
	if !ok {
		return false, nil
	}
	switch action {
	case "apply":
		return true, h.requestRedirect(ctx, user, target, message)
	case "new":
		command, err := h.store.RequestCommand(store.Command{
			SessionID: user.SessionID, Kind: store.CommandSplice, Instruction: message,
		})
		if err != nil {
			return true, h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		return true, h.postAgent(user.SessionID, "Starting that as new work.", command.Seq)
	case "correct":
		node, found, err := h.store.Node(target)
		if err != nil || !found {
			return true, h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		previous := h.jobResult(node)
		if strings.TrimSpace(previous) == "" {
			return true, h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		return true, h.requestCorrection(user, node, message, previous)
	case "cancel":
		return true, h.resolveSurgery(user, store.CommandCancel, target, message, true)
	case "keep":
		return true, h.postAgent(user.SessionID, "Leaving it to finish.", 0)
	}
	return false, nil
}

// oldestRunningTarget is the answer to "which one" when nothing in the sentence
// and nothing in the conversation says. Somebody impatient is impatient about
// the thing that has been going longest, and work that has actually started
// outranks work that has not.
func oldestRunningTarget(targets []store.SurgeryTarget) store.SurgeryTarget {
	best := targets[0]
	bestRunning := runningStatus(best.Node)
	for _, target := range targets[1:] {
		running := runningStatus(target.Node)
		switch {
		case running && !bestRunning:
		case running == bestRunning && target.Node.CreatedSeq < best.Node.CreatedSeq:
		default:
			continue
		}
		best, bestRunning = target, running
	}
	return best
}

func runningStatus(node store.Node) bool {
	return node.Status == store.Running || node.Status == store.Claimed
}

// redirectCue names the five ways a person changes work already underway. They
// are phrases rather than keywords: "also" beginning a sentence is a scope
// addition, "also" in the middle of one is usually just prose.
//
// Urgency is tested last so the four content classes keep their meaning: a
// sentence that both cuts scope and presses for speed is a scope cut, and the
// redirect path already tells the running workers.
func redirectCue(message string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	hasPrefix := func(prefixes ...string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(lower, prefix) {
				return true
			}
		}
		return false
	}
	contains := func(phrases ...string) bool {
		for _, phrase := range phrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
		return false
	}
	switch {
	case hasPrefix("actually ", "no, ", "no — ", "no - ", "wait,", "wait —", "wait -", "wait, ", "wait ",
		"correction", "i meant ", "sorry, "),
		contains("that's wrong", "thats wrong", "that is wrong", "i meant ", "not what i meant"),
		impatientCorrection(lower):
		return "correction", true
	case hasPrefix("also ", "and also ", "include ", "plus "),
		contains("while you're at it", "while you are at it", "while youre at it",
			"also cover", "also include", "as well as"),
		contains("add ") && contains(" to it", " to that", " to the job", " to the task", " to it.", " to that."):
		return "scope-add", true
	case hasPrefix("skip ", "forget the ", "leave out ", "drop the "),
		contains("don't bother", "dont bother", "no need for", "no need to",
			"you can drop", "you can skip", "leave out the", "no longer need"):
		return "scope-cut", true
	case hasPrefix("instead ", "change of plan", "focus on "),
		contains(" instead", "change of plan", "rather than"):
		return "redirect", true
	case urgencyCued(lower):
		return urgencyCue, true
	default:
		return "", false
	}
}

// refersToLiveWork is the deictic half of the anchor: the sentence points at
// the running work without naming any of its words.
//
// A bare pronoun used to count only when exactly one thing was running, and the
// argument for that was resolution: with four jobs live, "it" names none of them
// and the reading was refused rather than guessed. But refusing is not neutral —
// "this is taking forever" with four jobs running fell out of every reading and
// landed on the router as ordinary new work, so impatience switched itself off
// at exactly the load that produces it.
//
// cued is what makes the difference safe. On the redirect path a cue has already
// established that this sentence changes work already underway, so "it" is doing
// referential work and the only open question is which job — settled where
// ambiguity belongs, by one plain question, or for the class that must never ask,
// by the oldest thing running. Without a cue the same pronoun is as likely to be
// "thanks, that helps", and one thing running stays the whole licence.
func refersToLiveWork(message string, cued bool, active int) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, phrase := range []string{
		"the job", "that job", "this job", "the task", "that task", "this task",
		"the current run", "the run", "the current job", "what you're doing",
		"what you are doing", "what youre doing", "the work you", "that work",
		// Shared history is deixis too: "the problem we started" names live
		// work as surely as "that job" does, without borrowing any of its words.
		"we started", "we began", "you started", "we were doing", "already started",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !cued && active != 1 {
		return false
	}
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		switch word {
		case "it", "that", "this", "them", "those":
			return true
		}
	}
	return false
}

// redirectReference strips the cue vocabulary before matching, so the score
// measures what the message is about rather than how it was phrased.
func redirectReference(message string) string {
	stop := map[string]bool{
		"actually": true, "no": true, "wait": true, "sorry": true, "correction": true,
		"also": true, "and": true, "plus": true, "include": true, "add": true,
		"skip": true, "forget": true, "leave": true, "out": true, "drop": true,
		"bother": true, "need": true, "instead": true, "focus": true, "on": true,
		// "plan" is not in here, and its absence is deliberate. It was stripped
		// as cue vocabulary — "change the plan" says how, not what — but it is
		// also the most ordinary word a person uses for the thing itself, and
		// stripping it left "what is the plan for the market research?" scoring
		// on "market research" alone and "what's the plan?" scoring on nothing
		// at all. A cue word that is also a subject belongs to the subject.
		"change": true, "of": true, "rather": true, "than": true,
		"while": true, "you": true, "your": true, "youre": true, "re": true,
		"at": true, "it": true, "that": true, "this": true, "them": true, "those": true,
		"the": true, "a": true, "an": true, "is": true, "isn": true, "not": true,
		"i": true, "meant": true, "we": true, "do": true, "don": true, "t": true,
		"can": true, "with": true, "for": true, "to": true, "job": true, "task": true,
		"work": true, "run": true, "doing": true, "use": true, "using": true,
		"make": true, "let": true, "s": true, "wrong": true, "cover": true, "as": true,
		"well": true, "please": true, "just": true, "now": true, "longer": true,
		// Impatience vocabulary. It says when, never what, so it must not be
		// allowed to score against a job that happens to be about speed.
		"fast": true, "faster": true, "quickly": true, "quicker": true,
		"hurry": true, "asap": true, "immediately": true, "urgent": true,
		"urgently": true, "sooner": true, "right": true, "away": true,
		"give": true, "me": true, "my": true, "want": true, "answer": true,
		"result": true, "results": true, "finish": true, "complete": true,
		"started": true, "began": true,
	}
	var kept []string
	for _, word := range strings.FieldsFunc(strings.ToLower(message), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len(word) < 2 || stop[word] {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}
