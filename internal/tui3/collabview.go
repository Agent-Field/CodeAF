package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// withCollabRows appends participant labels and sent/request/reply activity
// from the memo. It is a layout pass, not a store read: the beat already
// filled [app.collabView]. An empty reading adds nothing, which is the
// emptiness law (never `0 participants`).
func (a *app) withCollabRows(out []row, width int) []row {
	extra := a.collabTranscriptRows(width)
	if len(extra) == 0 {
		return out
	}
	if len(out) > 0 {
		out = append(out, row{entry: -1})
	}
	return append(out, extra...)
}

func (a *app) collabTranscriptRows(width int) []row {
	var out []row
	out = append(out, a.collabParticipantRows(width)...)
	out = append(out, a.collabActivityRows(width)...)
	return out
}

// collabParticipantRows is joint discussion as a normal chat: role labels,
// then the source they stand for. Planner/critic are labels a person named,
// not product entities. Nothing is drawn when the list is empty.
func (a *app) collabParticipantRows(width int) []row {
	var out []row
	for _, part := range a.collabView.participants {
		line := collabParticipantLine(part)
		if line == "" {
			continue
		}
		out = append(out, a.collabDimRows(line, width)...)
	}
	return out
}

func collabParticipantLine(part CollabParticipant) string {
	role := strings.TrimSpace(part.Role)
	source := strings.TrimSpace(part.SourceTitle)
	if role == "" {
		return source
	}
	if source == "" || source == role {
		return role
	}
	return role + " · " + source
}

// collabActivityRows is the management chat's visible traffic: request, reply,
// sent, each with a source. Machinery states are dropped, not painted.
func (a *app) collabActivityRows(width int) []row {
	var out []row
	for _, act := range a.collabView.activity {
		kind := collabKindWord(act.Kind)
		if kind == "" {
			continue
		}
		line := a.collabActivityLine(act, kind)
		if line == "" {
			continue
		}
		out = append(out, a.collabDimRows(line, width)...)
	}
	return out
}

func collabKindWord(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case collabRequestWord:
		return collabRequestWord
	case collabReplyWord:
		return collabReplyWord
	case collabSentWord:
		return collabSentWord
	default:
		return ""
	}
}

func (a *app) collabActivityLine(act CollabActivity, kind string) string {
	mark := a.collabKindGlyph(kind)
	source := strings.TrimSpace(act.ToTitle)
	if source == "" {
		source = strings.TrimSpace(act.SourceRef)
	}
	body := strings.TrimSpace(act.Body)
	line := strings.TrimSpace(mark + " " + kind)
	if source != "" {
		line += " " + collabSourceWord + " " + source
	}
	if body != "" {
		line += " · " + body
	}
	return strings.TrimSpace(line)
}

func (a *app) collabKindGlyph(kind string) string {
	switch kind {
	case collabReplyWord:
		return a.icon(tokens.GReplyIn)
	case collabSentWord:
		return a.icon(tokens.GActionCoordinate)
	default:
		return a.icon(tokens.GActionCommunicate)
	}
}

func (a *app) collabDimRows(text string, width int) []row {
	var out []row
	for i, line := range wrap(text, max(1, width-2)) {
		lead := "· "
		if i > 0 {
			lead = "  "
		}
		out = append(out, row{text: a.pal.dim(lead + line), entry: -1})
	}
	return out
}
