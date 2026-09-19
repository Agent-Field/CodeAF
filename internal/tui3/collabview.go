package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
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

// refreshCollabChrome re-reads deliveries after a coordinating turn. The home
// beat already snapshots; a management chat that never leaves the conversation
// would otherwise keep an empty memo and paint nothing after deliver/invite.
func (a *app) refreshCollabChrome() {
	if a == nil {
		return
	}
	a.readCollab()
}

// collabParticipantRows is joint discussion as a normal chat: role labels,
// then the source they stand for. Planner/critic are labels a person named,
// not product entities. Nothing is drawn when the list is empty.
func (a *app) collabParticipantRows(width int) []row {
	var out []row
	for _, part := range a.collabView.participants {
		line := collabParticipantLine(a.collabAttributedParticipant(part))
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
		line := a.collabActivityLine(a.collabAttributedActivity(act), kind)
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

func (a *app) collabAttributedParticipant(part CollabParticipant) CollabParticipant {
	part.SourceTitle = a.collabSourceTitle(a.home.world, part.SourceTitle)
	return part
}

func (a *app) collabAttributedActivity(act CollabActivity) CollabActivity {
	act.ToTitle = a.collabSourceTitle(a.home.world, act.ToTitle)
	act.SourceRef = a.collabSourceTitle(a.home.world, act.SourceRef)
	return act
}

// collabNameMemo writes conversation titles onto the snapshot. Closing home
// zeros [homeView.world], so naming has to happen on the beat, not in View.
func (a *app) collabNameMemo() {
	if len(a.collabView.activity) == 0 && len(a.collabView.participants) == 0 {
		return
	}
	world := a.collabTitleWorld()
	for i, act := range a.collabView.activity {
		a.collabView.activity[i].ToTitle = a.collabSourceTitle(world, act.ToTitle)
		a.collabView.activity[i].SourceRef = a.collabSourceTitle(world, act.SourceRef)
	}
	for i, part := range a.collabView.participants {
		a.collabView.participants[i].SourceTitle = a.collabSourceTitle(world, part.SourceTitle)
	}
}

func (a *app) collabTitleWorld() session.World {
	if a == nil {
		return session.World{}
	}
	if len(a.home.world.Sessions()) > 0 {
		return a.home.world
	}
	world, _ := a.readWorldKnown()
	return world
}

// collabSourceTitle is the person-facing source on a painted line. Deliveries
// cite a chat id; the pane names the conversation when the snapshotted world
// already knows it. View never asks the store.
func (a *app) collabSourceTitle(world session.World, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if title := collabTitleOnWorld(world, ref, a.home.folders); title != "" {
		return title
	}
	return ref
}

func collabTitleOnWorld(world session.World, id string, folders homeFoldersReading) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	for _, row := range world.Sessions() {
		if strings.TrimSpace(row.ID) != id {
			continue
		}
		if title := collabTitleIfNamed(id, row.Title); title != "" {
			return title
		}
	}
	if title := collabPlacementTitle(id, folders.members); title != "" {
		return title
	}
	return collabPlacementTitle(id, folders.root.Unfiled)
}

func collabPlacementTitle(id string, places []FolderPlacement) string {
	for _, place := range places {
		if strings.TrimSpace(place.RefID) != id {
			continue
		}
		if title := collabTitleIfNamed(id, place.Title); title != "" {
			return title
		}
	}
	return ""
}

func collabTitleIfNamed(id, title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == id {
		return ""
	}
	return title
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
