package head

import (
	"fmt"
	"strings"
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
)

// redirectIntent is one recognized mid-flight redirection: which cue class
// heard it, and the live jobs it could plausibly be aimed at, best first.
type redirectIntent struct {
	Cue        string
	Candidates []store.SurgeryTarget
	Certain    bool
}

// manageRedirect reads the user's message as a revision event for work already
// in flight. It is deterministic for the same reason surgery is: whether a
// sentence redirects the running plan or asks for something new is a question
// about the live graph, and a routing model that guesses it wrong either edits
// a plan nobody touched or starts a second job doing the same thing twice.
//
// It runs after surgery on purpose. "cancel", "pause", and their neighbours
// are surgery's vocabulary and stay surgery's, unchanged.
func (h *Head) manageRedirect(user store.Message) (bool, error) {
	intent, redirecting, err := h.recognizeRedirect(user.Body)
	if err != nil || !redirecting {
		return false, err
	}
	if intent.Certain {
		return true, h.requestRedirect(user, intent.Candidates[0].Node.ID, user.Body)
	}
	return true, h.askRedirectTarget(user, intent)
}

// recognizeRedirect requires two independent signals before it fires: a cue
// that the sentence corrects, adds, cuts, or redirects, and an anchor tying it
// to work that is actually live. Either alone is ordinary conversation.
func (h *Head) recognizeRedirect(message string) (redirectIntent, bool, error) {
	message = strings.TrimSpace(message)
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
	switch {
	case len(anchored) == 1:
		return redirectIntent{Cue: cue, Candidates: anchored, Certain: true}, true, nil
	case len(anchored) > 1:
		return redirectIntent{Cue: cue, Candidates: clipTargets(anchored)}, true, nil
	case !refersToLiveWork(message, len(active)):
		return redirectIntent{}, false, nil
	case len(active) == 1:
		// One job running and the user said "the job". There is nothing else
		// they could mean, and asking would be theatre.
		return redirectIntent{Cue: cue, Candidates: active, Certain: true}, true, nil
	default:
		candidates := ranked
		if len(candidates) == 0 {
			candidates = active
		}
		return redirectIntent{Cue: cue, Candidates: clipTargets(candidates)}, true, nil
	}
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

func (h *Head) requestRedirect(user store.Message, target, message string) error {
	// No acknowledgement on success, on purpose. The only honest receipt is
	// the one that knows what actually changed in the plan and who was told,
	// and that is written a moment later by the reconciler that did it.
	if _, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: store.CommandRedirect,
		Target: target, Instruction: strings.TrimSpace(message),
	}); err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return nil
}

// askRedirectTarget is the single structured question this path is allowed:
// the same words could steer a running job or start a new one, and picking for
// the user either edits the wrong plan or duplicates the work.
func (h *Head) askRedirectTarget(user store.Message, intent redirectIntent) error {
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
	if ask, _, err := h.store.ShouldAsk(store.QuestionCategoryRedirectTarget); err == nil && !ask {
		if err := h.store.RecordAssumedWithDefault(store.QuestionCategoryRedirectTarget, "1",
			user.SessionID, prompt); err == nil {
			return h.requestRedirect(user, intent.Candidates[0].Node.ID, message)
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
func (h *Head) applyRedirectOption(user store.Message, option store.QuestionOption) (bool, error) {
	action, target, message, ok := store.DecodeRedirectOption(option.Value)
	if !ok {
		return false, nil
	}
	switch action {
	case "apply":
		return true, h.requestRedirect(user, target, message)
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

// redirectCue names the four ways a person changes work already underway. They
// are phrases rather than keywords: "also" beginning a sentence is a scope
// addition, "also" in the middle of one is usually just prose.
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
		contains("that's wrong", "thats wrong", "that is wrong", "i meant ", "not what i meant"):
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
