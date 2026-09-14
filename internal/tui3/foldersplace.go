package tui3

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// ── THE FOLDERS PLACE, AS A READING ─────────────────────────────────────────
//
// Data in, rows out: a folder's page and one selected record become the lines
// the place draws, at a width, in a palette. Nothing here holds the surface, so
// nothing here can read a disk or ask the engine a question — every fact drawn
// arrived in a [workspaceview.FolderPage] or a [workspaceview.FolderItem] the
// engine already answered (place_folders.go asks; this draws).
//
// FOUR WORDS FOR WHAT A PERSON FINDS IN A FOLDER, and they are presentation, not
// four kinds of record: a Folder is a collection; a Chat is a conversation; Work
// is either a finite task a conversation ran or ongoing work that keeps going on
// its own; an Artifact is a file. The records keep their own owners.

// The kind words, spelled once. The row's tail says the small one; the
// inspector's heading says the capitalized one.
const (
	collectionKindFolder   = "Folder"
	collectionKindChat     = "Chat"
	collectionKindWork     = "Work"
	collectionKindArtifact = "Artifact"
)

// foldersInspectCol is the narrowest the inspector is ever drawn beside the
// list, and foldersListMin the narrowest the list beside it. A window narrower
// than both with the gap between them has no inspector column: the detail view
// is a key away instead ([foldersInspectAt]).
const (
	foldersInspectCol = 40
	foldersListMin    = 44
	foldersInspectMax = 64
	foldersInspectAt  = foldersListMin + homeGutter + foldersInspectCol
	// foldersLabelCol is the dim label column of the inspector's facts.
	foldersLabelCol = 10
)

// foldersSplit is how a body of this width divides. THE INSPECTOR'S WIDTH IS A
// FUNCTION OF THE WINDOW AND OF NOTHING ELSE, which is what keeps the list from
// moving when the selection does: a column sized to what it is showing would
// push the list sideways every time the cursor landed on a longer title.
func foldersSplit(width int) (list, inspect int) {
	if width < foldersInspectAt {
		return width, 0
	}
	inspect = max(foldersInspectCol, min(foldersInspectMax, width*2/5))
	return width - homeGutter - inspect, inspect
}

// collectionKindOf is the capitalized kind a record is shown as.
func collectionKindOf(ref workspace.Ref) string {
	switch ref.Kind {
	case workspace.CollectionKind:
		return collectionKindFolder
	case workspace.ConversationKind:
		return collectionKindChat
	case workspace.TaskKind, workspace.StandingKind:
		return collectionKindWork
	}
	return collectionKindArtifact
}

// collectionTailKind is the row's own small word for its kind. Ongoing work says so,
// because a finite task and a responsibility that keeps going are both Work and
// only one of them can still be doing something tomorrow.
func collectionTailKind(ref workspace.Ref) string {
	if ref.Kind == workspace.StandingKind {
		return "ongoing work"
	}
	return strings.ToLower(collectionKindOf(ref))
}

// collectionGlyph is the slot a row leads with. Every mark is the vocabulary's
// (internal/tui2/tokens), resolved through whatever the caller's surface folds
// the terminal's repertoire into.
func collectionGlyph(row workspaceview.FolderRow) tokens.GlyphID {
	if !row.Available {
		return tokens.GMissing
	}
	switch row.Ref.Kind {
	case workspace.CollectionKind:
		return tokens.GFolder
	case workspace.ConversationKind:
		return tokens.GPromptChat
	case workspace.TaskKind, workspace.StandingKind:
		return tokens.GActionWork
	}
	switch strings.ToLower(filepath.Ext(row.Ref.ID)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return tokens.GFileImage
	case ".mp3", ".wav", ".m4a", ".flac", ".ogg":
		return tokens.GFileAudio
	case ".mp4", ".mov", ".webm", ".mkv":
		return tokens.GFileVideo
	}
	return tokens.GFileDocument
}

// collectionRowTitle is what a row is called: its owner's title, and for a record
// its owner cannot name, the address it was filed under — a row that lost its
// record keeps saying which record it was.
func collectionRowTitle(row workspaceview.FolderRow) string {
	title := strings.TrimSpace(drawableLine(row.Title))
	if title != "" {
		return title
	}
	if row.Ref.Kind == workspace.ArtifactKind {
		return filepath.Base(row.Ref.ID)
	}
	if row.Ref.Kind == workspace.TaskKind {
		return "work " + row.Ref.ID
	}
	return drawableLine(row.Ref.ID)
}

// collectionRowTail is the dim words at the right of a row: its kind, what its owner
// says it is doing, and `placed` when this folder's rules reach it. A record
// whose owner could not be found says `missing` in place of a state.
func collectionRowTail(row workspaceview.FolderRow) string {
	words := []string{collectionTailKind(row.Ref)}
	switch {
	case !row.Available:
		words = append(words, "missing")
	case strings.TrimSpace(row.State) != "":
		words = append(words, drawableLine(row.State))
	}
	if row.Placed {
		words = append(words, "placed")
	}
	return strings.Join(words, " · ")
}

// collectionRowLine is one row of the list at a width: the mark, the title, and the
// tail pushed to the right edge. The title gives way first; the tail keeps its
// words until there is no room for a title at all.
func collectionRowLine(row workspaceview.FolderRow, mark string, width int, pal palette) string {
	lead := " " + mark + " "
	room := width - ansi.StringWidth(lead) - 1
	tail := collectionRowTail(row)
	// A NARROW ROW GIVES UP THE RECORD'S OWN STATE BEFORE IT CUTS `placed`: the
	// state is on the details, and `placed` is the one word on the row that says
	// this folder's rules reach it.
	if ansi.StringWidth(tail) > room/2 && row.Available {
		tail = collectionRowTail(workspaceview.FolderRow{ResolvedRef: workspace.ResolvedRef{Ref: row.Ref, Available: true}, Placed: row.Placed})
	}
	tailRoom := min(ansi.StringWidth(tail), max(0, room/2))
	title := fit(collectionRowTitle(row), max(0, room-tailRoom-2))
	left := lead + title
	if !row.Available {
		left = pal.dim(left)
	}
	right := fit(tail, tailRoom)
	return padTo(left, width-ansi.StringWidth(right)-1) + pal.dim(right) + " "
}

// foldersCrumb is the path walked, outermost first, as the body's first line.
// It is THE PATH THE PERSON WALKED and not a folder's one true parent: a folder
// filed in two places is reached through either, and the walk back out goes the
// way it came in.
func foldersCrumb(path []workspace.Collection, width int, pal palette) string {
	parts := []string{"folders"}
	for _, folder := range path {
		parts = append(parts, drawableLine(folder.Name))
	}
	last := len(parts) - 1
	shown := make([]string, len(parts))
	for i, part := range parts {
		if i == last {
			shown[i] = pal.bold(part)
		} else {
			shown[i] = pal.dim(part)
		}
	}
	return " " + fit(strings.Join(shown, pal.dim(" / ")), width-1)
}

// collectionInspect is everything the inspector draws for one selection.
type collectionInspect struct {
	row workspaceview.FolderRow
	// here is the folder the row is listed in, and "" at the top level.
	here string
	// item is the close reading, and nil until it has arrived for this row.
	item    *workspaceview.FolderItem
	itemErr string
	// chat is the conversation behind a Chat row, and owner the conversation that
	// ran a finite piece of Work — both out of the world the surface already holds.
	chat, owner *session.SessionRow
	// preview is the drawn opening of an Artifact, and previewNote the one line
	// said instead of it (not text, could not be read).
	preview     []string
	previewNote string
	size        int64
	modified    time.Time
	path        func(string) string
}

// collectionInspectLines is the inspector at a width. Facts nobody could state are
// not drawn at all (the emptiness law): no `—`, no `unknown`, no placeholder
// while a reading is still on its way.
func collectionInspectLines(in collectionInspect, width int, pal palette) []string {
	if width < 12 {
		return nil
	}
	path := in.path
	if path == nil {
		path = func(p string) string { return p }
	}
	row := in.row
	var out []string
	heading := collectionKindOf(row.Ref)
	if row.Ref.Kind == workspace.StandingKind {
		heading += " · ongoing"
	}
	out = append(out, pal.bold(fit(heading, width)))
	if row.Ref.Kind == workspace.StandingKind && in.item != nil && in.item.Standing != nil {
		// THE PERSON'S WORDS LEAD, as on every surface that opens an item
		// (internal/standing's [standing.Item.Words]).
		for i, line := range wrap(drawableLine(in.item.Standing.Words), width) {
			if i == 3 {
				break
			}
			out = append(out, pal.ink(line))
		}
	} else {
		for i, line := range wrap(collectionRowTitle(row), width) {
			if i == 2 {
				break
			}
			out = append(out, pal.ink(line))
		}
	}
	out = append(out, "")
	fact := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines := wrap(value, max(4, width-foldersLabelCol))
		for i, line := range lines {
			if i == 3 {
				break
			}
			lead := strings.Repeat(" ", foldersLabelCol)
			if i == 0 {
				lead = padTo(pal.dim(label), foldersLabelCol)
			}
			out = append(out, lead+fit(line, width-foldersLabelCol))
		}
	}
	failed := func(label, section string) bool {
		if in.item == nil || in.item.Errors[section] == "" {
			return false
		}
		fact(label, pal.warn("could not be read: "+drawableLine(in.item.Errors[section])))
		return true
	}

	if !row.Available {
		fact("missing", drawableLine(row.Unavailable))
	}
	if in.here != "" {
		fact("here", collectionRelation(row))
	}
	switch row.Ref.Kind {
	case workspace.ConversationKind:
		fact("state", drawableLine(row.State))
		fact("project", drawableLine(path(row.Location)))
		if in.chat != nil {
			fact("spoke", sinceWord(in.chat.At))
		}
	case workspace.TaskKind:
		fact("state", drawableLine(row.State))
		fact("phase", drawableLine(row.Phase))
		if in.owner != nil {
			fact("in chat", drawableLine(in.owner.Title))
		}
		fact("where", drawableLine(path(row.Location)))
	case workspace.StandingKind:
		if in.item != nil && in.item.Standing != nil {
			item := in.item.Standing
			fact("state", standingStateWords(*item))
			fact("wakes", drawableLine(item.When.CardWords()))
			fact("report", drawableLine(item.Does.Report))
			if !item.LastChecked.IsZero() {
				fact("checked", strings.TrimSpace(sinceWord(item.LastChecked)+" · "+drawableLine(item.LastCheckLine)))
			}
			if item.Runs > 0 {
				fact("runs", strings.TrimSpace(fmt.Sprintf("%d · last %s", item.Runs, drawableLine(item.LastOutcome))))
			}
			fact("project", path(item.Workspace))
		} else {
			failed("state", "standing")
			fact("state", drawableLine(row.State))
			fact("project", drawableLine(path(row.Location)))
		}
	case workspace.ArtifactKind:
		fact("where", drawableLine(path(row.Location)))
		if in.size > 0 || !in.modified.IsZero() {
			fact("size", strings.Trim(sizeWord(in.size)+" · changed "+sinceWord(in.modified), " ·"))
		}
	}
	if in.item != nil {
		if !failed("filed in", "filed_in") {
			fact("filed in", collectionNames(in.item.FiledIn))
		}
		if !failed("placed in", "governed_by") {
			fact("placed in", governingNames(in.item.GovernedBy))
		}
		if !failed("rules", "rules") {
			for i, rule := range in.item.Rules {
				label := ""
				if i == 0 {
					label = "rules"
				}
				fact(label, "“"+drawableLine(rule.Prompt())+"”")
			}
		}
		if !failed("shared", "context") {
			for i, record := range in.item.Context {
				label := ""
				if i == 0 {
					label = "shared"
				}
				fact(label, fmt.Sprintf("%s · v%d", drawableLine(record.Title), record.Revision))
			}
		}
		if in.item.Standing != nil && in.item.Standing.Adoption != nil {
			fact("set up", adoptionWord(*in.item.Standing.Adoption))
		}
	}
	if in.itemErr != "" {
		fact("detail", pal.warn("could not be read: "+drawableLine(in.itemErr)))
	}
	if row.Ref.Kind == workspace.ArtifactKind {
		if in.previewNote != "" {
			out = append(out, "", pal.dim(fit(in.previewNote, width)))
		} else if len(in.preview) > 0 {
			out = append(out, "")
			out = append(out, in.preview...)
		}
	}
	return out
}

// collectionRelation says how a row is bound to the folder it is listed in. ONLY A
// PLACEMENT GIVES A FOLDER'S RULES ANY REACH, and the line says which one this is
// in those words rather than leaving a person to guess what "in" means.
func collectionRelation(row workspaceview.FolderRow) string {
	switch {
	case row.Filed && row.Placed:
		return "filed and placed here · this folder's rules reach it"
	case row.Placed:
		return "placed here · this folder's rules reach it"
	case row.Filed:
		return "filed here · filing does not apply this folder's rules"
	}
	return ""
}

func collectionNames(folders []workspace.Collection) string {
	names := make([]string, 0, len(folders))
	for _, folder := range folders {
		names = append(names, drawableLine(folder.Name))
	}
	return strings.Join(names, " · ")
}

// governingNames is where a record is placed, nearest first, with how far away
// a folder that reaches it through another folder is.
func governingNames(folders []workspace.GoverningCollection) string {
	names := make([]string, 0, len(folders))
	for _, folder := range folders {
		name := drawableLine(folder.Name)
		if folder.Depth > 0 {
			name += fmt.Sprintf(" (through %d folder%s)", folder.Depth, collectionPlural(folder.Depth))
		}
		names = append(names, name)
	}
	return strings.Join(names, " · ")
}

// standingStateWords is an item's own status in a person's words. A stopped item
// says it was stopped; one that ended for another reason says that reason.
func standingStateWords(item standing.Item) string {
	switch item.Status {
	case standing.StatusRetired:
		if why := strings.TrimSpace(item.RetiredWhy); why != "" && why != standing.StoppedWhy {
			return "ended · " + drawableLine(why)
		}
		return "stopped"
	case standing.StatusPaused:
		return "paused"
	}
	if needs := strings.TrimSpace(item.NeedsPerson); needs != "" {
		return "waiting on you · " + drawableLine(needs)
	}
	return string(item.Status)
}

func adoptionWord(adoption standing.Adoption) string {
	switch adoption.Via {
	case standing.DoorTerminal:
		return "in the terminal"
	case standing.DoorChat:
		return "in a chat"
	}
	return ""
}

// sinceWord is [since] said as an age, and nothing for a moment nobody recorded.
func sinceWord(at time.Time) string {
	word := since(at)
	if word == "" || word == "now" || strings.Contains(word, " ") {
		return word
	}
	return word + " ago"
}

func sizeWord(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1024:
		return fmt.Sprintf("%d bytes", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func collectionPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
