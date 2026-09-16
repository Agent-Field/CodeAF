package tui3

import (
	"os"
	"path/filepath"
	"strings"
)

// recentPanel is `where you were`: this window's own conversation first, in
// bold, with the last thing said in it on the line under it; then the most
// recently active of the rest; then `N more · type to find one`.
//
// IT IS EVERY CONVERSATION NOT WAITING ON A PERSON. One that is waiting is on
// `needs you`, and a row drawn twice is the same reading said twice. One that is
// mid-turn or coming here stays: `running` lists the work a conversation sent
// out (tasks and jobs), never the conversation itself, so a chat dropped from
// here for moving would be on no panel at all. And this window's own
// conversation is always the first row, because `where you were` without the
// place you were is not an answer.
//
// AND THE ERRANDS STAND OVER IT. An `ask here` exchange is a conversation this
// window started a minute ago, with no row in the world at all
// (homeexchange.go), and the top of this panel is where the thing you asked
// for belongs.
type recentPanel struct{ homePanelBase }

func (recentPanel) rows(in *homeGridInput) homePanelRows {
	var own *switcherRow
	var rest []switcherRow
	for _, row := range in.rows {
		switch {
		case row.kind != switcherConversation:
		case row.here:
			mine := row
			own = &mine
		case !row.needs:
			rest = append(rest, row)
		}
	}
	lines := append([]homeLine(nil), in.errands...)
	// The panel hands the layout as many conversations as its budget in the
	// order table, and the layout draws five of them or, in a tall frame, more.
	room := homeSlotOf(panelRecent).most
	if own != nil {
		lines = append(lines, switcherRowLine(*own, recentOwnCell(*own, in)))
		room--
	}
	shown := min(room, len(rest))
	for _, row := range rest[:shown] {
		lines = append(lines, switcherRowLine(row, recentCell(row, in)))
	}
	return homePanelRows{lines: lines, more: len(rest) - shown}
}

// recentOwnCell is this window's own row: bold, `here` at the margin, and the
// last thing its person said on the line under it — read from the journal's
// tail on the beat (homecardread.go), never on a draw, and nothing at all until
// that reading has come back.
//
// A BRAND-NEW LAUNCH IS ONE LINE. `new conversation` with `here` is the shell
// this launch minted, and it keeps its row because it IS this window — but
// until its person has said something in it there is no last thing said, and
// the only fact about it is where it is. The record of the first message is the
// row's own time ([session.SessionRow.At] is the person's last word, and zero on
// a shell nobody has spoken in), so no tail read off the journal can put a line
// under it first.
//
// `here` IS THE FIRST WORD OF ITS DESCRIPTION, not its margin. THE RIGHT MARGIN
// OF EVERY ROW OF THE FIELD IS A TIME (owner, 2026-09-15): this row's is when
// its person last spoke in it, like every row under it, and the word that says
// it is this window's own stands where the row's other facts do — permanently,
// because it is true whether or not the cursor is here. On a frame with no
// description column the word is not said at all: it would cost the column a
// row on the frames that have the fewest, and the bold already says it.
func recentOwnCell(row switcherRow, in *homeGridInput) *homeCell {
	said := ""
	if !row.session.At.IsZero() {
		said = switcherFirstLine(in.last[row.session.Transcript].LastUser)
	}
	if in.desc {
		said = rowClauses(homeHereWord, said)
	}
	return &homeCell{panel: panelRecent, title: row.title, right: row.age, bold: true, sub: said}
}

// recentCell is any other row: its age at the margin — or the one fact that
// decides what enter will do, `another window`, `folder gone`, which is the one
// thing allowed to stand in a time's place because it is the truth about the
// key — and, under the cursor, its project's name and the last thing its person
// said in it.
//
// THE PROJECT IS A DESCRIPTION AND NOT A TAG. It used to stand beside the age
// (`infra  3h`) on a row from another folder and nowhere on a row from this
// window's own, so the margin was one word wide on some rows and two on others
// (owner, 2026-09-15: "what are these and why are they absent elsewhere?"). It
// is the first clause of the description now, still only where it says
// something — a row of this window's own folder, a scratch folder or the home
// directory has no project word ([chatProjectTag]).
func recentCell(row switcherRow, in *homeGridInput) *homeCell {
	// THE LAST THING THIS PERSON SAID IN IT, under the cursor — which is the one
	// fact that tells two conversations with similar names apart, and the same
	// reading this window's own row has always drawn ([recentOwnCell]). It is
	// empty until the journal's tail has come back ([app.askHomeLeftOff] asks for
	// the row being read, off the draw), and a row with nothing to say draws
	// nothing rather than a gap.
	cell := &homeCell{panel: panelRecent, title: row.title, right: switcherMarginWord(row)}
	project := ""
	if homeBucketOf(row.session.Transcript) != in.bucket {
		project = chatProjectTag(row, in.tilde)
	}
	said := ""
	if in.desc {
		said = switcherFirstLine(in.last[row.session.Transcript].LastUser)
	}
	cell.sub = rowClauses(project, said)
	cell.grows = cell.sub != ""
	cell.hold = cell.right != row.age
	if row.door && cell.right == homeHeldShort {
		cell.door = takeoverHeldDoorWord
	}
	return cell
}

// rowClauses joins the clauses of one row's description that are not empty, so
// a row with a project and nothing said yet reads `infra` and not `infra · `.
func rowClauses(clauses ...string) string {
	var kept []string
	for _, clause := range clauses {
		if clause = strings.TrimSpace(clause); clause != "" {
			kept = append(kept, clause)
		}
	}
	return strings.Join(kept, rowSep)
}

// chatProjectTag is the project word a chat row wears, and NOTHING FOR A
// FOLDER THAT IS NOT A PROJECT: the home directory, whose word is a lone `~`,
// and a scratch folder at the top of the temporary directory, whose word is a
// name somebody made up for a minute (DESIGN §1, "what is retired"). The
// projects panel still lists both, as the paths they are.
// The rule itself is [chatProjectWord] (projecttag.go), because the tasks place
// asks the same question about the same conversations. This panel has already
// settled the own-folder half of it — a row of this window's own bucket never
// reaches here ([recentCell]) — so it hands no folder to compare.
func chatProjectTag(row switcherRow, tilde string) string {
	return chatProjectWord(chatFolder{project: row.project, workspace: row.session.Workspace}, "", tilde)
}

// homeScratchRoots are the directories a throwaway folder is made at the top
// of: the system's temporary directory, and `/tmp` spelled as people type it.
var homeScratchRoots = []string{filepath.Clean(os.TempDir()), "/tmp"}

// homeScratchFolder reports a workspace that is the home directory itself or
// a folder directly inside a temporary root — `/tmp/af-stop-ws` is scratch,
// `/tmp/build/site` is somebody's checkout.
func homeScratchFolder(path, tilde string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	if tilde != "" && clean == filepath.Clean(tilde) {
		return true
	}
	for _, root := range homeScratchRoots {
		if clean == root || filepath.Dir(clean) == root {
			return true
		}
	}
	return false
}
