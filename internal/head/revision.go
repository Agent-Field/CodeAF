package head

import (
	"context"
	"fmt"
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

// redirectIntent is one recognized mid-flight redirection: which cue class
// heard it, and the live jobs it could plausibly be aimed at, best first.
type redirectIntent struct {
	Cue        string
	Candidates []store.SurgeryTarget
	Certain    bool
	// Floorless marks a question assembled without a single candidate clearing
	// the anchor floor. Nothing about the ranking is evidence in that case, so
	// the question must actually be asked rather than quietly assumed away.
	Floorless bool
}

// manageRedirect reads the user's message as a revision event for work already
// in flight. It is deterministic for the same reason surgery is: whether a
// sentence redirects the running plan or asks for something new is a question
// about the live graph, and a routing model that guesses it wrong either edits
// a plan nobody touched or starts a second job doing the same thing twice.
//
// It runs after surgery on purpose. "cancel", "pause", and their neighbours
// are surgery's vocabulary and stay surgery's, unchanged.
func (h *Head) manageRedirect(ctx context.Context, user store.Message) (bool, error) {
	intent, redirecting, err := h.recognizeRedirect(user)
	if err != nil || !redirecting {
		return false, err
	}
	if intent.Cue == urgencyCue {
		// Urgency is the one class that never asks. The question would spend
		// the wait it is meant to shorten, and everything expedite does is
		// reversible, so the best-ranked live job takes the pressure and the
		// receipt names it.
		return true, h.requestRevision(ctx, user, store.CommandExpedite, intent.Candidates[0].Node.ID, user.Body)
	}
	if intent.Certain {
		return true, h.requestRedirect(ctx, user, intent.Candidates[0].Node.ID, user.Body)
	}
	return true, h.askRedirectTarget(ctx, user, intent)
}

// recognizeRedirect requires two independent signals before it fires: a cue
// that the sentence corrects, adds, cuts, or redirects, and an anchor tying it
// to work that is actually live. Either alone is ordinary conversation.
//
// The anchor has two arms. Vocabulary is the older one and it is not enough:
// "make sure you review the changes" shares no word with a job whose brief says
// middleware, and means that job entirely, because that job spoke a moment ago.
// So adjacency reads position in the conversation instead, and rides in as a
// candidate with a strong prior — strong enough to anchor a sentence no word
// anchors, never strong enough to overrule a job the user's own words name.
func (h *Head) recognizeRedirect(user store.Message) (redirectIntent, bool, error) {
	message := strings.TrimSpace(user.Body)
	cue, cued := redirectCue(message)
	if !cued {
		return redirectIntent{}, false, nil
	}
	active, err := h.activeUserJobs()
	if err != nil || len(active) == 0 {
		return redirectIntent{}, false, err
	}
	ranked, err := h.rankRedirectTargets(message, active)
	if err != nil {
		return redirectIntent{}, false, err
	}
	anchored := make([]store.SurgeryTarget, 0, len(ranked))
	for _, target := range ranked {
		if target.Score >= RedirectAnchorScore {
			anchored = append(anchored, target)
		}
	}
	adjacent, adjoins, err := h.adjacencyTarget(user, active)
	if err != nil {
		return redirectIntent{}, false, err
	}
	switch {
	case len(anchored) == 1 && (!adjoins || anchored[0].Node.ID == adjacent.Node.ID):
		return redirectIntent{Cue: cue, Candidates: anchored, Certain: true}, true, nil
	case len(anchored) == 1:
		// The words name one job and the conversation points at another, and
		// both readings are as good as this path ever gets. Choosing either
		// silently edits a plan the user may not have meant, so the words go
		// first — they are the more deliberate signal — and the question settles
		// it.
		return redirectIntent{Cue: cue, Candidates: []store.SurgeryTarget{anchored[0], adjacent}}, true, nil
	case len(anchored) > 1:
		return redirectIntent{Cue: cue, Candidates: clipTargets(promoteTarget(anchored, adjacent, adjoins))}, true, nil
	case adjoins:
		// No shared vocabulary at all, and the job the user is replying to said
		// something a moment ago. That is the whole signal, and it is the one a
		// person would use.
		return redirectIntent{Cue: cue, Candidates: []store.SurgeryTarget{adjacent}, Certain: true}, true, nil
	case !refersToLiveWork(message, len(active)):
		return redirectIntent{}, false, nil
	case len(active) == 1:
		// One job running and the user said "the job". There is nothing else
		// they could mean, and asking would be theatre.
		return redirectIntent{Cue: cue, Candidates: active, Certain: true}, true, nil
	default:
		// Nothing cleared the floor. The ranked list is therefore not a shortlist
		// of what the user might have meant — it is the set of jobs whose briefs
		// happen to share a word with the sentence, which is the coincidence the
		// floor exists to reject. Offering only those hid the user's actual work
		// behind a 0.05 match, and the assume-the-default path below could pick
		// that match without ever showing it. So the offer is the user's live
		// jobs, and this question is one they answer themselves.
		return redirectIntent{Cue: cue, Candidates: clipTargets(active), Floorless: true}, true, nil
	}
}

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

// spliceContinuity is the last thing the adjacency reading is good for. Every
// layer above it declined, so this genuinely is new work — but new work typed
// while a job was mid-sentence is work about that job often enough that running
// the two side by side is never the safer guess. Target on a splice already
// means "the prior work this one continues", which is how a promoted reflex
// hands its partial forward; a job that has not finished yet is the same claim.
func (h *Head) spliceContinuity(user store.Message) string {
	active, err := h.activeUserJobs()
	if err != nil || len(active) == 0 {
		return ""
	}
	adjacent, adjoins, err := h.adjacencyTarget(user, active)
	if err != nil || !adjoins {
		return ""
	}
	return adjacent.Node.ID
}

// promoteTarget puts the adjacent job at the head of a candidate list it is
// already part of. The question is the same question; the strong prior only
// decides which option is offered as the default.
func promoteTarget(candidates []store.SurgeryTarget, adjacent store.SurgeryTarget, adjoins bool) []store.SurgeryTarget {
	if !adjoins {
		return candidates
	}
	promoted := []store.SurgeryTarget{adjacent}
	for _, candidate := range candidates {
		if candidate.Node.ID != adjacent.Node.ID {
			promoted = append(promoted, candidate)
		}
	}
	return promoted
}

// activeUserJobs is the whole precondition for this path: the user's own work,
// still open. A quiet graph can never turn a sentence into a redirection.
func (h *Head) activeUserJobs() ([]store.SurgeryTarget, error) {
	targets, err := h.store.SearchSurgeryTargets("", false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		return nil, err
	}
	active := make([]store.SurgeryTarget, 0, len(targets))
	for _, target := range targets {
		if target.Node.Provenance.Origin == store.OriginUser {
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

// askRedirectTarget is the single structured question this path is allowed:
// the same words could steer a running job or start a new one, and picking for
// the user either edits the wrong plan or duplicates the work.
func (h *Head) askRedirectTarget(ctx context.Context, user store.Message, intent redirectIntent) error {
	message := strings.TrimSpace(user.Body)
	options := make([]store.QuestionOption, 0, len(intent.Candidates)+1)
	for _, candidate := range intent.Candidates {
		options = append(options, store.QuestionOption{
			Label: "apply it to " + surgeryTargetLabel(candidate.Node),
			Hint:  surgeryTargetHint(candidate),
			Value: store.RedirectOptionValue("apply", candidate.Node.ID, message),
		})
	}
	options = append(options, store.QuestionOption{
		Label: "start it as new work",
		Value: store.RedirectOptionValue("new", "", message),
	})
	prompt := fmt.Sprintf("Apply that to %s, or start it as new work?",
		surgeryTargetLabel(intent.Candidates[0].Node))
	// The assume-the-default shortcut is for a question whose default is
	// evidence. When nothing cleared the anchor floor there is no evidence to
	// default to, and steering a plan on the first row of an arbitrary list is
	// precisely the wrong edit made silently.
	if ask, _, err := h.store.ShouldAsk(store.QuestionCategoryRedirectTarget); err == nil && !ask && !intent.Floorless {
		if err := h.store.RecordAssumedWithDefault(store.QuestionCategoryRedirectTarget, "1",
			user.SessionID, prompt); err == nil {
			return h.requestRedirect(ctx, user, intent.Candidates[0].Node.ID, message)
		}
	}
	body := store.QuestionMessageBody(prompt, options, store.QuestionConfig{
		Kind: store.QuestionChoose, Category: store.QuestionCategoryRedirectTarget, Default: "1",
	})
	question, err := h.store.AskQuestion(store.AgentQuestion{
		SessionID: user.SessionID, Text: body, OriginNodeID: intent.Candidates[0].Node.ID,
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryRedirectTarget,
		DefaultAnswer: "1", Options: options,
	})
	if err != nil {
		return err
	}
	_, err = h.store.SurfaceQuestion(question.Seq)
	return err
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
	case "cancel":
		return true, h.resolveSurgery(user, store.CommandCancel, target, message, true)
	case "keep":
		return true, h.postAgent(user.SessionID, "Leaving it to finish.", 0)
	}
	return false, nil
}

func clipTargets(targets []store.SurgeryTarget) []store.SurgeryTarget {
	if len(targets) > RedirectCandidateLimit {
		return targets[:RedirectCandidateLimit]
	}
	return targets
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
// the running work without naming any of its words. A bare pronoun counts only
// when there is exactly one thing it could point at.
func refersToLiveWork(message string, active int) bool {
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
	if active != 1 {
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
		"change": true, "of": true, "plan": true, "rather": true, "than": true,
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
