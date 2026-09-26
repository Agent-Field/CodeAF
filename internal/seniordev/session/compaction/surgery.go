//go:build !windows

// Post-compaction message surgery: the overflow history selection, the replay
// of the message that overflowed, and the auto-continue text. These helpers
// are pure apart from the injected ID factory.
package compaction

import (
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
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
	return prefix + "The conversation was compacted: the state record above replaces the older transcript, and the most recent messages are retained verbatim. Continue from the current state."
}

func rebasePart(raw msgmodel.Part, base msgmodel.PartBase) msgmodel.Part {
	switch part := raw.(type) {
	case msgmodel.TextPart:
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
	case msgmodel.CompactionPart:
		part.PartBase = base
		return part
	default:
		panic("compaction: unknown part type " + raw.PartType())
	}
}
