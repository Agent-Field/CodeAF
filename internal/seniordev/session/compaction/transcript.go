//go:build !windows

package compaction

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// SerializeTranscript flattens the conversation being summarized into ONE
// plain-text block for the summary model call (the caller wraps it in
// <conversation> tags). Handing the summarizer the session as a live message
// array invites the model to keep coding — leaking tool-call markup as text or
// narrating its next action — instead of serializing state. A flattened
// transcript cannot be "continued": there is no open tool call, no assistant
// turn to extend, only data to read.
//
// Each tool output is head/tail truncated (HeadTailTruncate) so the serialized
// block is bounded and, in practice, smaller than the message array it
// replaces.
func SerializeTranscript(messages []msgmodel.WithParts, maxToolChars float64) string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		switch info := message.Info.(type) {
		case msgmodel.User:
			if text := plainText(message.Parts); text != "" {
				lines = append(lines, "[User]: "+text)
			}
		case msgmodel.Assistant:
			if text := plainText(message.Parts); text != "" {
				lines = append(lines, "[Assistant]: "+text)
			}
			if calls := toolCallLines(message.Parts); calls != "" {
				lines = append(lines, "[Assistant tool calls]: "+calls)
			}
			for _, result := range toolResultLines(message.Parts, maxToolChars) {
				lines = append(lines, result)
			}
			_ = info
		}
	}
	return strings.Join(lines, "\n")
}

// plainText joins the non-synthetic, non-ignored text parts of a message.
func plainText(parts msgmodel.Parts) string {
	collected := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(msgmodel.TextPart)
		if !ok || boolPointer(part.Ignored) || boolPointer(part.Synthetic) {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			collected = append(collected, text)
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

// toolCallLines renders "name(input); name(input)" for the tool calls in a
// message. The raw input JSON is included but capped, so a large write/edit
// payload cannot dominate the serialized transcript.
func toolCallLines(parts msgmodel.Parts) string {
	calls := make([]string, 0)
	for _, raw := range parts {
		part, ok := raw.(msgmodel.ToolPart)
		if !ok {
			continue
		}
		input := ""
		if raw := part.State.ToolInput(); len(raw) > 0 {
			input = HeadTailTruncate(string(raw), toolCallInputMaxChars)
		}
		calls = append(calls, part.Tool+"("+input+")")
	}
	return strings.Join(calls, "; ")
}

// toolResultLines renders one "[Tool result]" / "[Tool error]" line per tool
// part, truncated to the observation cap.
func toolResultLines(parts msgmodel.Parts, maxToolChars float64) []string {
	lines := make([]string, 0)
	for _, raw := range parts {
		part, ok := raw.(msgmodel.ToolPart)
		if !ok {
			continue
		}
		switch state := part.State.(type) {
		case msgmodel.ToolStateCompleted:
			if output := strings.TrimSpace(state.Output); output != "" {
				lines = append(lines,
					"[Tool result]: "+HeadTailTruncate(state.Output, maxToolChars))
			}
		case msgmodel.ToolStateError:
			if errText := strings.TrimSpace(state.Error); errText != "" {
				lines = append(lines,
					"[Tool error]: "+HeadTailTruncate(state.Error, maxToolChars))
			}
		}
	}
	return lines
}

const toolCallInputMaxChars = 500

// CapTranscript bounds a flattened transcript to maxChars, cutting the middle
// so the beginning and the latest state both survive. It returns the number
// of characters removed so the boundary can report it.
func CapTranscript(transcript string, maxChars float64) (string, float64) {
	length := float64(charCount(transcript))
	if maxChars <= 0 || length <= maxChars {
		return transcript, 0
	}
	headChars := int(maxChars / 4)
	tailChars := int(maxChars) - headChars
	head := sliceChars(transcript, 0, headChars)
	tail := sliceChars(transcript, charCount(transcript)-tailChars, charCount(transcript))
	omitted := length - float64(headChars) - float64(tailChars)
	return head + "\n[... transcript cut here: " + strconv.FormatFloat(omitted, 'f', -1, 64) +
		" chars of the middle omitted ...]\n" + tail, omitted
}
