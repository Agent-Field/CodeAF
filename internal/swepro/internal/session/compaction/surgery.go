// Post-compaction message surgery ports src/session/compaction.ts:535-561 and
// 710-795. These helpers are pure apart from the injected ID factory.
package compaction

import (
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
)

type Replay struct {
	Info  msgmodel.User
	Parts msgmodel.Parts
}

type OverflowHistory struct {
	Messages []msgmodel.WithParts
	Replay   *Replay
}

func selectOverflowHistory(
	messages []msgmodel.WithParts, parentID string, overflow bool,
) OverflowHistory {
	if !overflow {
		return OverflowHistory{Messages: messages}
	}
	index := -1
	for i, message := range messages {
		if message.Info.MessageID() == parentID {
			index = i
			break
		}
	}
	var replay *Replay
	selected := messages
	for i := index - 1; i >= 0; i-- {
		user, ok := messages[i].Info.(msgmodel.User)
		if ok && !hasCompaction(messages[i].Parts) {
			replay = &Replay{Info: user, Parts: messages[i].Parts}
			selected = messages[:i]
			break
		}
	}
	hasContent := false
	if replay != nil {
		for _, message := range selected {
			if _, ok := message.Info.(msgmodel.User); ok && !hasCompaction(message.Parts) {
				hasContent = true
				break
			}
		}
	}
	if !hasContent {
		return OverflowHistory{Messages: messages}
	}
	return OverflowHistory{Messages: selected, Replay: replay}
}

func buildReplayParts(
	replay Replay, sessionID, messageID string, newID func(prefix string) string,
) msgmodel.Parts {
	out := msgmodel.Parts{}
	for _, raw := range replay.Parts {
		if _, ok := raw.(msgmodel.CompactionPart); ok {
			continue
		}
		base := msgmodel.PartBase{
			ID: newID("part"), MessageID: messageID, SessionID: sessionID,
		}
		if file, ok := raw.(msgmodel.FilePart); ok && msgmodel.IsMedia(file.Mime) {
			filename := "file"
			if file.Filename != nil {
				filename = *file.Filename
			}
			out = append(out, msgmodel.TextPart{
				PartBase: base,
				Text:     "[Attached " + file.Mime + ": " + filename + "]",
			})
			continue
		}
		out = append(out, rebasePart(raw, base))
	}
	return out
}

func autoContinueText(overflow bool) string {
	prefix := ""
	if overflow {
		prefix = "The previous request exceeded the provider's size limit due to large media attachments. " +
			"The conversation was compacted and media files were removed from context. If the user was asking " +
			"about attached images or files, explain that the attachments were too large to process and suggest " +
			"they try again with smaller or fewer files.\n\n"
	}
	return prefix + "Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed."
}

func buildAuditorPin(agent, directory string) string {
	if !strings.HasPrefix(agent, "auditor") {
		return ""
	}
	return strings.Join([]string{
		"# AUDITOR VERDICT CONTRACT (pinned — reproduce VERBATIM under '## Next Steps')",
		"Rewrite " + directory + "/.codeaf/auditor-verdict.json with the FINAL verdict",
		"before finishing: step2_signal.commands must list every executed command",
		"with exit code/output, clause_coverage must carry {clause, evidence} entries",
		"for the inventory clauses and matrix cells probed, and blockers[] must be",
		"populated on fail. The file on disk may still be an early skeleton — a",
		"session that ends without rewriting it is discarded as evidence-free.",
	}, "\n")
}

func assemblePinnedPrompt(base string, pins ...string) string {
	filtered := []string{}
	for _, pin := range pins {
		if pin != "" {
			filtered = append(filtered, pin)
		}
	}
	if len(filtered) == 0 {
		return base
	}
	return base + "\n\n" + strings.Join(filtered, "\n\n")
}

func rebasePart(raw msgmodel.Part, base msgmodel.PartBase) msgmodel.Part {
	switch part := raw.(type) {
	case msgmodel.TextPart:
		part.PartBase = base
		return part
	case msgmodel.SubtaskPart:
		part.PartBase = base
		return part
	case msgmodel.ReasoningPart:
		part.PartBase = base
		return part
	case msgmodel.FilePart:
		part.PartBase = base
		return part
	case msgmodel.ToolPart:
		part.PartBase = base
		return part
	case msgmodel.StepStartPart:
		part.PartBase = base
		return part
	case msgmodel.StepFinishPart:
		part.PartBase = base
		return part
	case msgmodel.SnapshotPart:
		part.PartBase = base
		return part
	case msgmodel.PatchPart:
		part.PartBase = base
		return part
	case msgmodel.AgentPart:
		part.PartBase = base
		return part
	case msgmodel.RetryPart:
		part.PartBase = base
		return part
	case msgmodel.CompactionPart:
		part.PartBase = base
		return part
	default:
		panic("compaction: unknown part type " + raw.PartType())
	}
}
