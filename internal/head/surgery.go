package head

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// SurgerySpendGateUSD is recorded spend above which cancel/restart needs
	// explicit consent.
	SurgerySpendGateUSD = store.SurgerySpendGateUSD
	// SurgeryRuntimeGate is live runtime above which cancellation needs consent.
	SurgeryRuntimeGate = store.SurgeryRuntimeGate
	// SurgeryCascadeGateNodes gates every operation that affects a larger tree.
	SurgeryCascadeGateNodes = store.SurgeryCascadeGateNodes
)

type surgeryIntent struct {
	Kind          store.CommandKind
	Reference     string
	Instruction   string
	IncludeLeaves bool
}

func isSurgeryCommand(kind store.CommandKind) bool {
	switch kind {
	case store.CommandCancel, store.CommandPause, store.CommandResume, store.CommandAmend,
		store.CommandReprioritize, store.CommandRestart:
		return true
	default:
		return false
	}
}

// manageSurgery is deterministic on purpose. Existing-work mutations should
// not depend on a model inventing a node id, and ambiguity should look exactly
// like charter ambiguity: BM25 candidates, then one structured askback.
func (h *Head) manageSurgery(user store.Message) (bool, error) {
	intent, managing := nodeSurgery(user.Body)
	if !managing {
		return false, nil
	}
	intent.Instruction = strings.TrimSpace(user.Body)
	if class, isClass := classSelector(user.Body); isClass {
		return true, h.resolveClassSurgery(user, intent.Kind, intent.Instruction, class, "", false)
	}
	matches, err := h.surgeryMatches(intent)
	if err != nil {
		return true, err
	}
	if mentionsClass(user.Body) && !surgeryMatchIsDecisive(matches) {
		// The message named a status class but did not resolve as one, and the
		// content path has nothing confident to show for it. Both halves of the
		// live failure live here — one weak match acted on silently, and a flat
		// "I couldn't find any" said over a board full of work — so this asks
		// with the live work in hand instead.
		if asked, err := h.askSurgeryTarget(user, intent, matches); asked || err != nil {
			return true, err
		}
	}
	if len(matches) == 0 {
		// Decline, do not answer. "try again" on the job that just failed found
		// nothing here — the reconciler folds a settled job in the same tick that
		// announces it, so by the time the user has read the failure the target
		// is folded and invisible to a verb's status filter — and this arm
		// claimed the sentence anyway and replied "I couldn't find any current
		// work that matches." over a board the user could still see. It was the
		// last word on a question two better readers were waiting to take: the
		// belt reads settled work now, and the router carries the fold roots in
		// its snapshot. A deterministic arm that resolved nothing has learned
		// nothing, and the honest thing to do with a sentence you did not
		// understand is to hand it on.
		return false, nil
	}
	if len(matches) > 1 {
		return true, h.postSurgeryChoice(user, intent, matches)
	}
	return true, h.resolveSurgery(user, intent.Kind, matches[0].Node.ID, intent.Instruction, false)
}

// surgeryMatchIsDecisive reports that the content path can be trusted on its
// own. More than one match already ends in the ordinary numbered question, so
// only the lone match has to earn its silence: it must clear the same anchor
// floor redirection uses before a sentence counts as being about a job.
func surgeryMatchIsDecisive(matches []store.SurgeryTarget) bool {
	if len(matches) > 1 {
		return true
	}
	return len(matches) == 1 && matches[0].Score >= ClassFallbackFloor
}

// askSurgeryTarget widens a doubtful reading into the ordinary numbered choice
// over live work. The doubtful match leads, since it is still the best guess —
// it just may not act on its own. A quiet graph has nothing to offer, so it
// reports no question and the caller says so plainly instead.
func (h *Head) askSurgeryTarget(user store.Message, intent surgeryIntent, matches []store.SurgeryTarget) (bool, error) {
	live, err := h.surgeryMatches(surgeryIntent{Kind: intent.Kind, IncludeLeaves: intent.IncludeLeaves})
	if err != nil {
		return false, err
	}
	candidates := append([]store.SurgeryTarget(nil), matches...)
	for _, target := range live {
		duplicate := false
		for _, candidate := range candidates {
			if candidate.Node.ID == target.Node.ID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, target)
		}
	}
	if len(candidates) == 0 {
		return false, nil
	}
	return true, h.postSurgeryChoice(user, intent, candidates)
}

func (h *Head) postSurgeryChoice(user store.Message, intent surgeryIntent, matches []store.SurgeryTarget) error {
	options := make([]store.QuestionOption, 0, len(matches))
	for _, match := range matches {
		options = append(options, store.QuestionOption{
			Label: surgeryTargetLabel(match.Node),
			Hint:  surgeryTargetHint(match),
			Value: encodeSurgeryOption("select", intent.Kind, match.Node.ID, intent.Instruction),
		})
	}
	return h.postQuestion(user.SessionID, "Which job do you mean?", 0, options)
}

// surgeryAllowedStatuses is the one table saying which statuses each verb may
// legally touch. Both the content path and the class path read it.
func surgeryAllowedStatuses(kind store.CommandKind) []store.Status {
	if kind == store.CommandRestart {
		return []store.Status{store.Failed, store.Cancelled}
	}
	return []store.Status{store.Pending, store.Claimed, store.Running}
}

func (h *Head) surgeryMatches(intent surgeryIntent) ([]store.SurgeryTarget, error) {
	allowed := surgeryAllowedStatuses(intent.Kind)
	matches, err := h.store.SearchSurgeryTargets(intent.Reference, intent.IncludeLeaves, allowed...)
	if err != nil {
		return nil, err
	}
	filtered := matches[:0]
	for _, match := range matches {
		if !surgeryEligible(match.Node, intent.Kind) {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered, nil
}

// surgeryEligible is the per-node half of the allowed-status table: the hold
// and priority state a verb needs beyond a legal status. It is separate so the
// toolbelt reads the same rule before naming a target the store would refuse.
func surgeryEligible(node store.Node, kind store.CommandKind) bool {
	switch kind {
	case store.CommandResume:
		return node.Held
	case store.CommandPause:
		return !node.Held
	case store.CommandReprioritize:
		return node.Status == store.Pending
	}
	return true
}

func (h *Head) resolveSurgery(user store.Message, kind store.CommandKind, target, instruction string, confirmed bool) error {
	instruction = restartInstruction(kind, instruction)
	node, found, err := h.store.Node(target)
	if err != nil {
		return err
	}
	if !found {
		return h.postAgent(user.SessionID, "That work is no longer available.", 0)
	}
	impact, err := h.store.Impact(target, time.Now())
	if err != nil {
		return err
	}
	if !confirmed && surgeryNeedsConfirmation(kind, impact) {
		verb := surgeryVerb(kind)
		prompt := fmt.Sprintf("%s %s? %s", upperFirst(verb), surgeryTargetLabel(node), surgeryLoss(kind, impact))
		prompt = strings.TrimSpace(prompt)
		allowFree := false
		options := []store.QuestionOption{
			{Label: "yes, " + verb + " it", Value: encodeSurgeryOption("apply", kind, target, instruction)},
			{Label: surgeryKeepLabel(kind), Value: encodeSurgeryOption("keep", kind, target, instruction)},
		}
		ask, _, gateErr := h.store.ShouldAsk(store.QuestionCategorySurgeryConfirm)
		if gateErr == nil && !ask {
			if err := h.store.RecordAssumedWithDefault(store.QuestionCategorySurgeryConfirm, "2", user.SessionID, prompt); err == nil {
				return h.postAgent(user.SessionID, "Assuming the default: "+surgeryKeepLabel(kind)+".", 0)
			}
		}
		body := store.QuestionMessageBody(prompt, options, store.QuestionConfig{
			Kind: store.QuestionConfirm, Category: store.QuestionCategorySurgeryConfirm,
			Default: "2", AllowFree: &allowFree,
		})
		question, err := h.store.AskQuestion(store.AgentQuestion{
			SessionID: user.SessionID, Text: body, OriginNodeID: target,
			Urgency: store.QuestionBlocking, Category: store.QuestionCategorySurgeryConfirm,
			DefaultAnswer: "2", Options: options,
		})
		if err != nil {
			return err
		}
		_, err = h.store.SurfaceQuestion(question.Seq)
		return err
	}
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: target, Instruction: instruction,
	})
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.postAgent(user.SessionID, surgeryQueuedReceipt(kind, node, instruction), command.Seq)
}

// restartInstruction is the one place a restart's model words are read. It sits
// on the journaling funnel rather than in the recognizers, so every route to a
// restart — the deterministic cue, the router's own command, a confirmed
// question replayed later — carries the same reading.
func restartInstruction(kind store.CommandKind, instruction string) string {
	if kind != store.CommandRestart {
		return instruction
	}
	return MarkRestartModel(instruction)
}

func surgeryNeedsConfirmation(kind store.CommandKind, impact store.SurgeryImpact) bool {
	cascade := 1
	switch kind {
	case store.CommandCancel, store.CommandPause, store.CommandResume:
		cascade = impact.OpenNodes
	case store.CommandRestart:
		cascade = impact.Nodes
	}
	if cascade > SurgeryCascadeGateNodes {
		return true
	}
	if kind != store.CommandCancel && kind != store.CommandRestart {
		return false
	}
	return impact.Cost > SurgerySpendGateUSD || impact.RunningFor > SurgeryRuntimeGate
}

func surgeryLoss(kind store.CommandKind, impact store.SurgeryImpact) string {
	parts := make([]string, 0, 2)
	if impact.RunningFor > 0 {
		minutes := int(impact.RunningFor.Round(time.Minute) / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		parts = append(parts, fmt.Sprintf("%d %s in", minutes, pluralWord(minutes, "minute", "minutes")))
	}
	if impact.Cost > 0 {
		parts = append(parts, fmt.Sprintf("~$%.2f spent", impact.Cost))
	}
	if len(parts) == 0 {
		parts = append(parts, "the current partial will be discarded")
	}
	cascade := impact.OpenNodes
	if kind == store.CommandRestart {
		cascade = impact.Nodes
	}
	if cascade > SurgeryCascadeGateNodes {
		parts = append(parts, fmt.Sprintf("%d nodes affected", cascade))
	}
	return strings.Join(parts, " and ") + "."
}

func surgeryQueuedReceipt(kind store.CommandKind, node store.Node, instruction string) string {
	label := surgeryTargetLabel(node)
	if kind == store.CommandRestart {
		// The receipt names the model because the command carries it. Saying it
		// out loud is also the only way a wrong reading costs one word to fix
		// rather than a whole re-run on the slot the user was trying to leave.
		return "Restarting " + label + restartModelReceipt(instruction) + "."
	}
	switch kind {
	case store.CommandCancel:
		return "Cancelling " + label + "."
	case store.CommandPause:
		return "Pausing " + label + "."
	case store.CommandResume:
		return "Resuming " + label + "."
	case store.CommandAmend:
		return "Amending " + label + "."
	case store.CommandReprioritize:
		return "Moving " + label + " first."
	case store.CommandRestart:
		return "Restarting " + label + "."
	default:
		return "Updating " + label + "."
	}
}

func surgeryTargetLabel(node store.Node) string {
	if label := strings.TrimSpace(node.Title); label != "" {
		return firstLine(label)
	}
	if label := firstLine(node.Brief); label != "" {
		return label
	}
	return node.ID
}

func surgeryTargetHint(target store.SurgeryTarget) string {
	status := string(target.Node.Status)
	if target.Node.Held {
		status = "paused"
	}
	if strings.TrimSpace(target.Age) == "" {
		return status
	}
	return status + " · " + target.Age
}

func surgeryVerb(kind store.CommandKind) string {
	switch kind {
	case store.CommandCancel:
		return "cancel"
	case store.CommandPause:
		return "pause"
	case store.CommandResume:
		return "resume"
	case store.CommandAmend:
		return "amend"
	case store.CommandReprioritize:
		return "reprioritize"
	case store.CommandRestart:
		return "restart"
	default:
		return "change"
	}
}

func surgeryKeepLabel(kind store.CommandKind) string {
	if kind == store.CommandRestart {
		return "keep it failed"
	}
	return "keep going"
}

func encodeSurgeryOption(action string, kind store.CommandKind, target, instruction string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(instruction))
	return strings.Join([]string{"surgery", action, string(kind), target, encoded}, ":")
}

func decodeSurgeryOption(value string) (action string, kind store.CommandKind, target, instruction string, ok bool) {
	parts := strings.SplitN(value, ":", 5)
	if len(parts) != 5 || parts[0] != "surgery" || strings.TrimSpace(parts[3]) == "" {
		return "", "", "", "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return "", "", "", "", false
	}
	kind = store.CommandKind(parts[2])
	switch kind {
	case store.CommandCancel, store.CommandPause, store.CommandResume, store.CommandAmend,
		store.CommandReprioritize, store.CommandRestart:
	default:
		return "", "", "", "", false
	}
	return parts[1], kind, parts[3], string(decoded), true
}

func nodeSurgery(message string) (surgeryIntent, bool) {
	// The cue has to sit at the start of the INSTRUCTION, which is not always
	// the start of the recording. Filler is skipped, and then a sentence that
	// withdraws its own opening is declined outright rather than acted on from
	// its first three words — dictation.go argues both.
	lower := instructionOpening(strings.ToLower(strings.TrimSpace(message)))
	if selfRepairsAfterCue(lower) {
		return surgeryIntent{}, false
	}
	intent := surgeryIntent{}
	switch {
	case strings.HasPrefix(lower, "cancel ") || lower == "cancel" || strings.HasPrefix(lower, "stop "):
		intent.Kind = store.CommandCancel
	case strings.HasPrefix(lower, "pause ") || strings.HasPrefix(lower, "hold "):
		intent.Kind = store.CommandPause
	case strings.HasPrefix(lower, "resume ") || strings.HasPrefix(lower, "unpause "):
		intent.Kind = store.CommandResume
	case strings.HasPrefix(lower, "restart ") || strings.HasPrefix(lower, "retry ") ||
		strings.HasPrefix(lower, "rerun ") || (strings.HasPrefix(lower, "try ") && strings.HasSuffix(lower, " again")):
		intent.Kind = store.CommandRestart
	case strings.HasPrefix(lower, "prioritize ") || strings.HasPrefix(lower, "reprioritize ") ||
		(strings.HasPrefix(lower, "do ") && strings.Contains(lower, " first")) || strings.Contains(lower, " before the other"):
		intent.Kind = store.CommandReprioritize
	case (strings.HasPrefix(lower, "actually ") || strings.HasPrefix(lower, "amend ") ||
		strings.HasPrefix(lower, "change ") || strings.HasPrefix(lower, "update ")) &&
		(strings.Contains(lower, " task") || strings.Contains(lower, " job") || strings.Contains(lower, " it") ||
			strings.Contains(lower, " that") || strings.Contains(lower, " one") || strings.Contains(lower, " also")):
		intent.Kind = store.CommandAmend
	default:
		return surgeryIntent{}, false
	}
	intent.IncludeLeaves = intent.Kind == store.CommandAmend || intent.Kind == store.CommandReprioritize ||
		intent.Kind == store.CommandRestart || containsSurgeryLevelWord(lower)
	intent.Reference = surgeryReference(lower)
	if intent.Kind == store.CommandAmend {
		intent.Reference = amendmentReference(lower)
	}
	return intent, true
}

func amendmentReference(message string) string {
	words := strings.FieldsFunc(message, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for index, word := range words {
		switch word {
		case "task", "job", "one", "it", "that":
			return surgeryReference(strings.Join(words[:index], " "))
		}
	}
	return surgeryReference(message)
}

func containsSurgeryLevelWord(message string) bool {
	for _, word := range []string{" task", " step", " leaf", " part", " one"} {
		if strings.Contains(" "+message, word) {
			return true
		}
	}
	return false
}

func surgeryReference(message string) string {
	stop := map[string]bool{
		"please": true, "cancel": true, "stop": true, "pause": true, "hold": true,
		"resume": true, "unpause": true, "restart": true, "retry": true, "rerun": true,
		"try": true, "again": true, "actually": true, "make": true, "amend": true,
		"change": true, "update": true, "prioritize": true, "reprioritize": true,
		"do": true, "first": true, "before": true, "other": true, "the": true,
		"a": true, "an": true, "job": true, "work": true, "one": true, "task": true,
		"step": true, "leaf": true, "part": true, "it": true, "that": true,
		"this": true, "while": true, "i": true, "think": true, "failed": true,
		"running": true, "pending": true, "to": true, "now": true, "then": true,
		"queued": true, "waiting": true, "ones": true,
	}
	var kept []string
	for _, word := range surgeryWords(message) {
		// Class vocabulary names a set, never content. Left in, a status word
		// scores against whichever brief happens to share it and answers a
		// question about the board with an unrelated node.
		if stop[word] || classVocabulary[word] != "" {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// surgeryWords is the single tokenization every reference reader shares, so the
// stop-list and the class vocabulary always see the same words.
func surgeryWords(message string) []string {
	return strings.FieldsFunc(message, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
