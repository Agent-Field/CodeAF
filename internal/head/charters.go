package head

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// answerAgentQuestion routes replies to the durable reverse-direction queue.
// An explicit QuestionSeq wins; otherwise the store applies the same
// no-intervening-user-turn recency rule as ordinary conversational askbacks.
func (h *Head) answerAgentQuestion(user store.Message) (bool, error) {
	question, found, err := h.store.QuestionForAnswer(user.SessionID, user.Seq, user.QuestionSeq)
	if err != nil || !found {
		return false, err
	}
	if question.Status == store.QuestionAnswered || question.Status == store.QuestionExpired {
		return true, nil
	}
	answer := strings.TrimSpace(user.Body)
	if option, selected := selectQuestionOption(user.Body, question.Options); selected {
		answer = strings.TrimSpace(option.Label)
		if answer == "" {
			answer = strings.TrimSpace(option.Value)
		}
		if handled, err := h.answerCharterFiringQuestion(user, question.Seq, option); handled {
			return true, err
		}
		if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, answer, user.Seq); err != nil {
			return true, err
		}
		return true, h.applyAgentQuestionOption(user, question, option)
	}
	if charterID, ok := charterQuestionID(question.Options); ok {
		if cadence := extractCadence(user.Body); cadence != "" {
			if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, cadence, user.Seq); err != nil {
				return true, err
			}
			return true, h.requestCharterCommand(user, store.CommandCharterCadence, charterID, cadence)
		}
		// Preserve the existing charter behavior: unrelated free text remains
		// ordinary conversation rather than accidentally ratifying spend.
		return false, nil
	}
	if answer == "" {
		return true, h.postAgent(user.SessionID, "Tell me what you want me to use for that question.", 0)
	}
	if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, answer, user.Seq); err != nil {
		return true, err
	}
	if question.OriginCommandSeq != 0 {
		return true, h.continueAgentCompilerQuestion(user, question, answer)
	}
	return true, h.postAgent(user.SessionID, "Got it — I’ll use that.", 0)
}

func (h *Head) applyAgentQuestionOption(user store.Message, question store.AgentQuestion, option store.QuestionOption) error {
	parts := strings.Split(option.Value, ":")
	if len(parts) >= 3 && parts[0] == "charter" {
		id := parts[2]
		switch parts[1] {
		case "ratify":
			return h.requestCharterCommand(user, store.CommandCharterRatify, id, option.Label)
		case "pause":
			return h.requestCharterCommand(user, store.CommandCharterPause, id, option.Label)
		case "retire":
			return h.requestCharterCommand(user, store.CommandCharterRetire, id, option.Label)
		case "once":
			return h.requestCharterCommand(user, store.CommandCharterOnce, id, option.Label)
		case "cadence":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterCadence, id,
					strings.Join(parts[3:], ":"))
			}
			return h.askForCadence(user.SessionID, id)
		}
	}
	answer := strings.TrimSpace(option.Label)
	if answer == "" {
		answer = strings.TrimSpace(option.Value)
	}
	if question.OriginCommandSeq != 0 {
		return h.continueAgentCompilerQuestion(user, question, answer)
	}
	return h.postAgent(user.SessionID, "Got it — I’ll use that.", 0)
}

func (h *Head) continueAgentCompilerQuestion(user store.Message, question store.AgentQuestion, answer string) error {
	source, found, err := h.store.CommandBySeq(question.OriginCommandSeq)
	if err != nil {
		return err
	}
	if !found || source.Kind != store.CommandSplice || strings.TrimSpace(answer) == "" {
		return h.postAgent(user.SessionID, "Tell me which option you want, or answer in your own words.", 0)
	}
	instruction := source.Instruction + "\n\nAnswer to compiler question: " + answer
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: store.CommandSplice, Instruction: instruction,
	})
	if err != nil {
		return err
	}
	return h.postAgent(user.SessionID, "Got it — proceeding with that choice.", command.Seq)
}

func (h *Head) answerPendingQuestion(user store.Message) (bool, error) {
	question, pending, err := h.store.PendingQuestion(user.SessionID, user.Seq)
	if err != nil || !pending {
		return false, err
	}
	option, selected := selectQuestionOption(user.Body, question.Options)
	if selected {
		if question.QuestionSeq != 0 {
			if handled, err := h.answerCharterFiringQuestion(user, question.QuestionSeq, option); handled {
				return true, err
			}
		}
		return true, h.applyQuestionOption(user, question, option)
	}

	if charterID, ok := charterQuestionID(question.Options); ok {
		if cadence := extractCadence(user.Body); cadence != "" {
			return true, h.requestCharterCommand(user, store.CommandCharterCadence, charterID, cadence)
		}
		// Ratification keeps free text available to the conversational router;
		// only an explicit choice or cadence phrase crosses a durable transition.
		return false, nil
	}
	if question.CommandSeq == 0 {
		return false, nil
	}
	return true, h.continueCompilerQuestion(user, question, strings.TrimSpace(user.Body))
}

func selectQuestionOption(reply string, options []store.QuestionOption) (store.QuestionOption, bool) {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(reply), " .,!?:;\t\n\r"))
	if number, err := strconv.Atoi(normalized); err == nil && number > 0 && number <= len(options) {
		return options[number-1], true
	}
	for _, option := range options {
		if normalized == strings.ToLower(strings.TrimSpace(option.Label)) ||
			(option.Value != "" && normalized == strings.ToLower(strings.TrimSpace(option.Value))) {
			return option, true
		}
	}
	for _, option := range options {
		label := strings.ToLower(strings.TrimSpace(option.Label))
		value := strings.ToLower(strings.TrimSpace(option.Value))
		if affirmativeRailReply(normalized) &&
			(strings.HasPrefix(label, "yes") || strings.Contains(value, ":ratify:") || strings.Contains(value, ":fire:")) {
			return option, true
		}
		if negativeReply(normalized) &&
			(strings.Contains(label, "not standing") || strings.Contains(value, ":once:") ||
				strings.Contains(value, ":decline:") ||
				strings.HasPrefix(label, "keep ") || strings.Contains(value, "surgery:keep:")) {
			return option, true
		}
	}
	return store.QuestionOption{}, false
}

func negativeReply(reply string) bool {
	switch reply {
	case "n", "no", "no thanks", "decline", "never", "not standing", "once", "just once":
		return true
	default:
		return false
	}
}

func charterQuestionID(options []store.QuestionOption) (string, bool) {
	for _, option := range options {
		parts := strings.Split(option.Value, ":")
		if len(parts) >= 3 && parts[0] == "charter" && strings.TrimSpace(parts[2]) != "" {
			return parts[2], true
		}
	}
	return "", false
}

func (h *Head) applyQuestionOption(user store.Message, question store.Message, option store.QuestionOption) error {
	if action, kind, target, instruction, ok := decodeSurgeryOption(option.Value); ok {
		switch action {
		case "select":
			return h.resolveSurgery(user, kind, target, instruction, false)
		case "apply":
			return h.resolveSurgery(user, kind, target, instruction, true)
		case "keep":
			return h.postAgent(user.SessionID, "Keeping it as-is.", 0)
		}
	}
	parts := strings.Split(option.Value, ":")
	if len(parts) >= 3 && parts[0] == "charter" {
		id := parts[2]
		switch parts[1] {
		case "ratify":
			return h.requestCharterCommand(user, store.CommandCharterRatify, id, option.Label)
		case "pause":
			return h.requestCharterCommand(user, store.CommandCharterPause, id, option.Label)
		case "retire":
			return h.requestCharterCommand(user, store.CommandCharterRetire, id, option.Label)
		case "once":
			return h.requestCharterCommand(user, store.CommandCharterOnce, id, option.Label)
		case "cadence":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterCadence, id,
					strings.Join(parts[3:], ":"))
			}
			return h.askForCadence(user.SessionID, id)
		case "fire":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterFire, id, "wake:"+parts[3])
		case "decline":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing proposal is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterDecline, id, "wake:"+parts[3])
		case "always":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterAlways, id, "wake:"+parts[3])
		case "never":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterNever, id, "wake:"+parts[3])
		case "probation":
			return h.requestCharterCommand(user, store.CommandCharterProbation, id, "back to asking")
		}
	}
	answer := strings.TrimSpace(option.Label)
	if answer == "" {
		answer = strings.TrimSpace(option.Value)
	}
	return h.continueCompilerQuestion(user, question, answer)
}

func (h *Head) continueCompilerQuestion(user store.Message, question store.Message, answer string) error {
	source, found, err := h.store.CommandBySeq(question.CommandSeq)
	if err != nil {
		return err
	}
	if !found || source.Kind != store.CommandSplice || strings.TrimSpace(answer) == "" {
		return h.postAgent(user.SessionID, "Tell me which option you want, or answer in your own words.", 0)
	}
	instruction := source.Instruction + "\n\nAnswer to compiler question: " + answer
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: store.CommandSplice, Instruction: instruction,
	})
	if err != nil {
		return err
	}
	return h.postAgent(user.SessionID, "Got it — proceeding with that choice.", command.Seq)
}

func (h *Head) askForCadence(sessionID, charterID string) error {
	return h.postQuestion(sessionID, "What cadence should I use?", 0, []store.QuestionOption{
		{Label: "hourly", Value: "charter:cadence:" + charterID + ":hourly"},
		{Label: "daily", Value: "charter:cadence:" + charterID + ":daily"},
		{Label: "weekly", Value: "charter:cadence:" + charterID + ":weekly"},
	})
}

func (h *Head) requestCharterCommand(user store.Message, kind store.CommandKind, id, instruction string) error {
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: id, Instruction: instruction,
	})
	if err != nil {
		return err
	}
	return h.acknowledgeCharterCommand(user, command)
}

func (h *Head) answerCharterFiringQuestion(user store.Message, questionSeq int64, option store.QuestionOption) (bool, error) {
	kind, id, instruction, ok := charterFiringCommand(option)
	if !ok {
		return false, nil
	}
	resolution := strings.TrimSpace(option.Label)
	if resolution == "" {
		resolution = strings.TrimSpace(option.Value)
	}
	command, requested, err := h.store.ResolveQuestionWithCommand(questionSeq, resolution, user.Seq, store.Command{
		SessionID: user.SessionID, Kind: kind, Target: id, Instruction: instruction,
	})
	if err != nil || !requested {
		return true, err
	}
	return true, h.acknowledgeCharterCommand(user, command)
}

func charterFiringCommand(option store.QuestionOption) (store.CommandKind, string, string, bool) {
	parts := strings.Split(option.Value, ":")
	if len(parts) != 4 || parts[0] != "charter" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", "", false
	}
	var kind store.CommandKind
	switch parts[1] {
	case "fire":
		kind = store.CommandCharterFire
	case "decline":
		kind = store.CommandCharterDecline
	case "always":
		kind = store.CommandCharterAlways
	case "never":
		kind = store.CommandCharterNever
	default:
		return "", "", "", false
	}
	return kind, parts[2], "wake:" + parts[3], true
}

func (h *Head) acknowledgeCharterCommand(user store.Message, command store.Command) error {
	reply := "Updating that standing charter."
	switch command.Kind {
	case store.CommandCharterRatify:
		reply = "Standing it up."
	case store.CommandCharterPause:
		reply = "Pausing that charter."
	case store.CommandCharterRetire:
		reply = "Retiring that charter."
	case store.CommandCharterOnce:
		reply = "Keeping it one-time."
	case store.CommandCharterCadence:
		reply = "Changing that cadence."
	case store.CommandCharterFire:
		reply = "Approved for this time."
	case store.CommandCharterDecline:
		reply = "Okay — I won’t do this firing. I’ll ask again next time."
	case store.CommandCharterAlways:
		reply = "I’ll take this one and handle future firings on my own."
	case store.CommandCharterNever:
		reply = "I won’t do that, and I’m pausing the charter."
	case store.CommandCharterProbation:
		reply = "I’ll ask before firing again."
	}
	return h.postAgent(user.SessionID, reply, command.Seq)
}

// postQuestion carries the same choices twice on purpose: durable option rows
// for continuation and validation, and the structured JSON payload inside the
// body that the TUI's question components render.
func (h *Head) postQuestion(sessionID, body string, commandSeq int64, options []store.QuestionOption) error {
	_, err := h.store.PostMessage(store.Message{
		SessionID: sessionID, Role: store.RoleAgent,
		Body:       store.QuestionMessageBody(body, options),
		CommandSeq: commandSeq, Options: options,
	})
	return err
}

func (h *Head) manageCharter(user store.Message) (bool, error) {
	kind, reference, cadence, managing := charterManagement(user.Body)
	if !managing {
		return false, nil
	}
	matches, err := h.store.SearchActiveCharters(reference)
	if err != nil {
		return true, err
	}
	if len(matches) == 0 {
		// "pause" is shared vocabulary. If it names no standing charter, let
		// ordinary node surgery try the live graph before claiming a miss.
		if kind == store.CommandCharterPause {
			return false, nil
		}
		return true, h.postAgent(user.SessionID, "I couldn't match that to an active charter.", 0)
	}
	if len(matches) > 1 {
		options := make([]store.QuestionOption, 0, len(matches))
		for _, charter := range matches {
			value := "charter:" + charterOptionAction(kind) + ":" + charter.ID
			if kind == store.CommandCharterCadence {
				value += ":" + cadence
			}
			options = append(options, store.QuestionOption{
				Label: firstLine(charter.Invariant), Value: value,
			})
		}
		return true, h.postQuestion(user.SessionID, "Which standing charter do you mean?", 0, options)
	}
	return true, h.requestCharterCommand(user, kind, matches[0].ID, managementInstruction(kind, cadence))
}

func charterManagement(message string) (store.CommandKind, string, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	kind := store.CommandKind("")
	cadence := ""
	switch {
	case strings.Contains(lower, "back to asking"):
		kind = store.CommandCharterProbation
	case strings.Contains(lower, "stop watching") || strings.Contains(lower, "stop monitoring") ||
		strings.HasPrefix(lower, "retire "):
		kind = store.CommandCharterRetire
	case strings.HasPrefix(lower, "pause ") || strings.Contains(lower, " pause the "):
		kind = store.CommandCharterPause
	default:
		cadence = extractCadence(message)
		if cadence != "" && (strings.HasPrefix(lower, "make ") || strings.HasPrefix(lower, "change ") ||
			strings.HasPrefix(lower, "set ")) {
			kind = store.CommandCharterCadence
		}
	}
	if kind == "" {
		return "", "", "", false
	}
	if kind == store.CommandCharterCadence && cadence == "" {
		return "", "", "", false
	}
	reference := charterReference(lower, cadence)
	return kind, reference, cadence, true
}

func charterReference(message, cadence string) string {
	if cadence != "" {
		message = strings.ReplaceAll(message, strings.ToLower(cadence), " ")
	}
	stop := map[string]bool{
		"please": true, "stop": true, "watching": true, "watch": true,
		"monitoring": true, "monitor": true, "retire": true, "pause": true,
		"make": true, "change": true, "set": true, "cadence": true,
		"back": true, "asking": true, "the": true, "it": true, "to": true,
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

func charterOptionAction(kind store.CommandKind) string {
	switch kind {
	case store.CommandCharterPause:
		return "pause"
	case store.CommandCharterRetire:
		return "retire"
	case store.CommandCharterProbation:
		return "probation"
	default:
		return "cadence"
	}
}

func managementInstruction(kind store.CommandKind, cadence string) string {
	if kind == store.CommandCharterCadence {
		return cadence
	}
	return string(kind)
}
