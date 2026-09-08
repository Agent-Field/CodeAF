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
	if !confirmed {
		asked, err := h.askSurgeryConfirm(user.SessionID, node, kind, target, instruction, impact)
		if err != nil || asked {
			return err
		}
	}
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: target, Instruction: instruction,
	})
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.postAgent(user.SessionID, surgeryQueuedReceipt(kind, node, instruction), command.Seq)
}

// ConfirmSurgery is the confirm law offered to whoever else can journal a node
// command — today, the task page's single-key stop and restart.
//
// The gates are the product's promise that nothing large is thrown away without
// somebody naming the loss out loud, and a keypress is not a smaller intention
// than a sentence. A key that journalled directly bought, for one accidental
// press, exactly what the conversational path has always had to ask for. So the
// key path asks this first: it reports true when it has asked, and the caller
// journals nothing — the durable question is the reply, and answering it
// replays the command through the same option machinery a spoken confirm does.
// False means the loss is small, and small work is simply done.
func (h *Head) ConfirmSurgery(sessionID string, kind store.CommandKind, target string) (bool, error) {
	if h == nil || h.store == nil {
		return false, nil
	}
	node, found, err := h.store.Node(target)
	if err != nil || !found {
		return false, err
	}
	impact, err := h.store.Impact(target, time.Now())
	if err != nil {
		return false, err
	}
	return h.askSurgeryConfirm(sessionID, node, kind, target,
		restartInstruction(kind, ""), impact)
}

// askSurgeryConfirm asks the one blocking question a large loss requires, and
// reports whether it asked. A silent gate is not an error, it is permission.
func (h *Head) askSurgeryConfirm(sessionID string, node store.Node, kind store.CommandKind,
	target, instruction string, impact store.SurgeryImpact) (bool, error) {
	if !surgeryNeedsConfirmation(kind, impact) {
		return false, nil
	}
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
		if err := h.store.RecordAssumedWithDefault(store.QuestionCategorySurgeryConfirm, "2", sessionID, prompt); err == nil {
			return true, h.postAgent(sessionID, "Assuming the default: "+surgeryKeepLabel(kind)+".", 0)
		}
	}
	body := askBody(prompt, options, askConfig(store.QuestionConfirm,
		store.QuestionCategorySurgeryConfirm, "2", allowFree))
	question, err := h.store.AskQuestion(store.AgentQuestion{
		SessionID: sessionID, Text: body, OriginNodeID: target,
		Urgency: store.QuestionBlocking, Category: store.QuestionCategorySurgeryConfirm,
		DefaultAnswer: "2", Options: options,
	})
	if err != nil {
		return true, err
	}
	_, err = h.store.SurfaceQuestion(question.Seq)
	return true, err
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
		parts = append(parts, fmt.Sprintf("~%s spent", moneyUSD(impact.Cost)))
	}
	if len(parts) == 0 {
		parts = append(parts, "the current partial will be discarded")
	}
	cascade := impact.OpenNodes
	if kind == store.CommandRestart {
		cascade = impact.Nodes
	}
	if cascade > SurgeryCascadeGateNodes {
		parts = append(parts, fmt.Sprintf("%d steps affected", cascade))
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

// surgeryTargetHint says what a candidate row is doing, in the words a person
// already has. The status values are the store's own vocabulary — "claimed"
// tells a reader nothing, and it is the first thing they read when the head has
// to ask which job they meant.
func surgeryTargetHint(target store.SurgeryTarget) string {
	status := surgeryStatusWord(target.Node.Status)
	if target.Node.Held {
		status = "paused"
	}
	if strings.TrimSpace(target.Age) == "" {
		return status
	}
	return status + " · " + target.Age
}

// surgeryStatusWord translates one store status into the word a person would
// use for it. Only "claimed" and "pending" genuinely need it — the rest already
// say themselves — but the translation is total so a new status cannot leak
// through by being forgotten here.
func surgeryStatusWord(status store.Status) string {
	switch status {
	case store.Pending:
		return "waiting"
	case store.Claimed:
		return "starting"
	case store.Running:
		return "working"
	case store.Done:
		return "done"
	case store.Failed:
		return "failed"
	case store.Cancelled:
		return "cancelled"
	default:
		return string(status)
	}
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
