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
	SurgerySpendGateUSD = 0.25
	// SurgeryRuntimeGate is live runtime above which cancellation needs consent.
	SurgeryRuntimeGate = 5 * time.Minute
	// SurgeryCascadeGateNodes gates every operation that affects a larger tree.
	SurgeryCascadeGateNodes = 3
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
	matches, err := h.surgeryMatches(intent)
	if err != nil {
		return true, err
	}
	if len(matches) == 0 {
		what := strings.TrimSpace(intent.Reference)
		if what == "" {
			return true, h.postAgent(user.SessionID, "I couldn't find any current work that matches.", 0)
		}
		return true, h.postAgent(user.SessionID,
			fmt.Sprintf("I couldn't find any current work matching %q.", what), 0)
	}
	if len(matches) > 1 {
		options := make([]store.QuestionOption, 0, len(matches))
		for _, match := range matches {
			options = append(options, store.QuestionOption{
				Label: surgeryTargetLabel(match.Node),
				Hint:  surgeryTargetHint(match),
				Value: encodeSurgeryOption("select", intent.Kind, match.Node.ID, intent.Instruction),
			})
		}
		return true, h.postQuestion(user.SessionID, "Which job do you mean?", 0, options)
	}
	return true, h.resolveSurgery(user, intent.Kind, matches[0].Node.ID, intent.Instruction, false)
}

func (h *Head) surgeryMatches(intent surgeryIntent) ([]store.SurgeryTarget, error) {
	var allowed []store.Status
	switch intent.Kind {
	case store.CommandRestart:
		allowed = []store.Status{store.Failed, store.Cancelled}
	default:
		allowed = []store.Status{store.Pending, store.Claimed, store.Running}
	}
	matches, err := h.store.SearchSurgeryTargets(intent.Reference, intent.IncludeLeaves, allowed...)
	if err != nil {
		return nil, err
	}
	filtered := matches[:0]
	for _, match := range matches {
		switch intent.Kind {
		case store.CommandResume:
			if !match.Node.Held {
				continue
			}
		case store.CommandPause:
			if match.Node.Held {
				continue
			}
		case store.CommandReprioritize:
			if match.Node.Status != store.Pending {
				continue
			}
		}
		filtered = append(filtered, match)
	}
	return filtered, nil
}

func (h *Head) resolveSurgery(user store.Message, kind store.CommandKind, target, instruction string, confirmed bool) error {
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
		_, err := h.store.PostMessage(store.Message{
			SessionID: user.SessionID, Role: store.RoleAgent, NodeID: target,
			Body: store.QuestionMessageBody(prompt, options, store.QuestionConfig{
				Kind: store.QuestionConfirm, Default: "2", AllowFree: &allowFree,
			}),
			Options: options,
		})
		return err
	}
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: target, Instruction: instruction,
	})
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.postAgent(user.SessionID, surgeryQueuedReceipt(kind, node), command.Seq)
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

func surgeryQueuedReceipt(kind store.CommandKind, node store.Node) string {
	label := surgeryTargetLabel(node)
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
	lower := strings.ToLower(strings.TrimSpace(message))
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
	}
	var kept []string
	for _, word := range strings.FieldsFunc(message, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if !stop[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
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
