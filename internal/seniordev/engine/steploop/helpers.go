//go:build !windows

package steploop

import (
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// BackScanResult is what a newest-first scan of the transcript finds.
type BackScanResult struct {
	LastUser      *msgmodel.User
	LastAssistant *msgmodel.Assistant
	LastFinished  *msgmodel.Assistant
	Tasks         []msgmodel.Part
}

// BackScan scans chronological filtered messages from newest to oldest.
func BackScan(msgs []msgmodel.WithParts) BackScanResult {
	var out BackScanResult
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		switch info := msg.Info.(type) {
		case msgmodel.User:
			if out.LastUser == nil {
				copy := info
				out.LastUser = &copy
			}
		case msgmodel.Assistant:
			if out.LastAssistant == nil {
				copy := info
				out.LastAssistant = &copy
			}
			if out.LastFinished == nil && info.Finish != nil && *info.Finish != "" {
				copy := info
				out.LastFinished = &copy
			}
		}
		if out.LastUser != nil && out.LastFinished != nil {
			break
		}
		if out.LastFinished == nil {
			for _, part := range msg.Parts {
				switch part.(type) {
				case msgmodel.CompactionPart:
					out.Tasks = append(out.Tasks, part)
				}
			}
		}
	}
	return out
}

// CompactedAfter reports whether a completed compaction summary is newer (by
// ascending message ID) than the given assistant. The token count recorded on
// that assistant is then stale: the boundary has already replaced the context
// it measured. Re-checking it would create a boundary loop, because the
// verbatim tail keeps that very message -- and its count -- after every
// compaction, and the projection places the tail after the summary, where a
// newest-first scan finds it first.
func CompactedAfter(msgs []msgmodel.WithParts, assistant msgmodel.Assistant) bool {
	for _, msg := range msgs {
		candidate, ok := msg.Info.(msgmodel.Assistant)
		if !ok || !boolValue(candidate.Summary) || candidate.Error != nil ||
			candidate.Finish == nil || *candidate.Finish == "" {
			continue
		}
		if idLess(assistant.ID, candidate.ID) {
			return true
		}
	}
	return false
}

// ShouldExit reports the natural exit: the last assistant finished for a
// reason other than tool calls, none of its persisted tool parts is pending
// on this side (provider-executed tool parts do not count), and the last user
// message is older than it.
func ShouldExit(lastUser *msgmodel.User, lastAssistant *msgmodel.Assistant, msgs []msgmodel.WithParts) bool {
	if lastUser == nil || lastAssistant == nil || lastAssistant.Finish == nil || *lastAssistant.Finish == "" {
		return false
	}
	if *lastAssistant.Finish == orFinishToolCalls {
		return false
	}

	var persisted *msgmodel.WithParts
	for i := len(msgs) - 1; i >= 0; i-- {
		assistant, ok := msgs[i].Info.(msgmodel.Assistant)
		if ok && assistant.ID == lastAssistant.ID {
			persisted = &msgs[i]
			break
		}
	}
	if persisted != nil {
		for _, raw := range persisted.Parts {
			part, ok := raw.(msgmodel.ToolPart)
			if ok && !part.ProviderExecuted() {
				return false
			}
		}
	}
	return idLess(lastUser.ID, lastAssistant.ID)
}

const orFinishToolCalls = "tool-calls"

// idLess orders two message IDs lexicographically by UTF-16 code unit. IDs
// are ASCII in practice, so this is plain lexicographic order; the UTF-16
// form keeps the comparison well-defined for any string.
func idLess(left, right string) bool {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// WrapLateUserText wraps the text of any user message that arrived after the
// last finished assistant in a system reminder, so the model treats it as an
// interjection. The input must be a fresh store load because this mutates
// text-part values in place.
func WrapLateUserText(msgs []msgmodel.WithParts, lastFinished msgmodel.Assistant) {
	for mi := range msgs {
		user, ok := msgs[mi].Info.(msgmodel.User)
		if !ok || !idLess(lastFinished.ID, user.ID) {
			continue
		}
		for partIdx, raw := range msgs[mi].Parts {
			part, ok := raw.(msgmodel.TextPart)
			if !ok || boolValue(part.Ignored) || boolValue(part.Synthetic) || strings.TrimSpace(part.Text) == "" {
				continue
			}
			part.Text = strings.Join([]string{
				"<system-reminder>",
				"The user sent the following message:",
				part.Text,
				"",
				"Please address this message and continue with your tasks.",
				"</system-reminder>",
			}, "\n")
			msgs[mi].Parts[partIdx] = part
		}
	}
}

func boolValue(v *bool) bool { return v != nil && *v }

func newestFirst(msgs []msgmodel.WithParts) []msgmodel.WithParts {
	out := append([]msgmodel.WithParts(nil), msgs...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
