package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// leftPanel is `since you left` (docs/design/home-mission-control/DESIGN.md §1,
// §3 P3): what happened on its own while nobody was looking, newest first — a
// line per task that landed, a line per file a conversation made, the watches
// that fired and what memory learned. The heading carries how long the person
// was away, measured from the look stamp.
//
// EVERY LINE IS A DOOR (SCREEN 1a). A task opens its record, a file opens
// itself, a firing opens standing and memory's line opens memory — the switcher's
// own ledger rows ([switcherReading.addLedger]), each carrying its door.
type leftPanel struct{ homePanelBase }

// SAID ONCE ACROSS THE COLUMNS. A landing whose check is still the person's is
// drawn by `needs you`'s `unread` group, one column over and higher up the
// page ([needsChecking]); this panel drew it a second time, as an ordinary thing
// that happened while nobody was looking. It comes back here the moment it stops
// being a question — answered, or aged out of the group — because then it IS
// just something that happened.
func (leftPanel) rows(in *homeGridInput) homePanelRows {
	checking := needsChecking(in)
	lines := make([]homeLine, 0, len(in.ledger))
	for _, row := range in.ledger {
		if row.task != nil && checking[taskLedgerKey(*row.task)] {
			continue
		}
		lines = append(lines, leftLine(row, in.now))
	}
	out := homePanelCut(in, panelLeft, lines)
	if len(lines) > 0 {
		out.said = sinceAt(in.seen, in.now)
	}
	return out
}

// leftLine is one ledger row as a door of home's column. The place word rides
// [homeLine.project] as it always has; the row itself rides the cell, because a
// task's and a file's doors are the row's own and not a place's.
//
// THE MARGIN IS WHEN IT HAPPENED. It used to be a task's cost on one row and a
// conversation's name on the next — two unrelated facts down one edge of one
// panel — and both are the row's description now, drawn under the cursor
// ([switcherRow.note]); the edge reads `3h`, `1h`, like every row of the field
// (owner, 2026-09-15).
func leftLine(row switcherRow, now time.Time) homeLine {
	cell := &homeCell{panel: panelLeft, title: row.title, right: sinceAt(row.at, now), row: &row,
		sub: strings.TrimSpace(row.note), grows: strings.TrimSpace(row.note) != ""}
	return homeLine{kind: homeLedger, project: row.place, dir: leftKey(row),
		view: row.item, item: row.item.Item, cell: cell}
}

// taskLedgerKey is ONE piece of work's identity across home's columns: the
// conversation that ran it and the node's number, which is the only pair that
// is unique ([session.TaskIndexEntry.ID] repeats across sessions). It is spelled
// here because three readers ask it — this panel's own key, the `unread`
// group's set of what it is already drawing, and the comparison between them
// (homepanel_needs.go's [needsChecking]).
func taskLedgerKey(task session.TaskIndexEntry) string {
	return task.SessionID + "/" + task.ID
}

// leftKey is a ledger line's identity beside its place word ([homeLine.sameRow]):
// the task, the file, or — for a firing or memory's line — its words.
func leftKey(row switcherRow) string {
	switch {
	case row.task != nil:
		return taskLedgerKey(*row.task)
	case row.path != "":
		return row.path
	}
	return row.title
}

// ── the reading's half: what landed and what was made ───────────────────────

// The words a ledger line is drawn with.
const (
	// ledgerMadeWord leads a file's line: `made report.md`.
	ledgerMadeWord = "made "
	// ledgerMadePlace is a file line's place word. No place owns a file — the
	// door is the file itself — so the word is the line's identity and nothing
	// the router opens.
	ledgerMadePlace = "made"
)

// ledgerLanded is a line per piece of work that landed since the look stamp:
// `<label> · <outcome>`, with the cost as its description when there was one.
//
// ONE LINE PER PIECE OF WORK. [session.LandedSince] hands back a task's parts
// and an adaptive run's workers too, and a person away for a night asked for
// four things, not forty; the parts are on the record the line opens.
func ledgerLanded(world session.World, seen time.Time) []switcherRow {
	var out []switcherRow
	for _, landed := range session.LandedSince(&world, seen) {
		if landed.Entry.Parent != "" || landed.Session.ArchivedTasks[landed.Entry.ID] {
			continue
		}
		entry := landed.Entry
		note := ""
		if entry.Cost > 0 {
			note = dollars(entry.Cost)
		}
		out = append(out, switcherRow{kind: switcherLedger, session: landed.Session, title: ledgerTaskLine(entry, landed.Session),
			place: pageTasks.word(), at: entry.EndedAt, task: &entry, note: note})
	}
	return out
}

// ledgerTaskLine is a landed task in one line: its label and what it came to.
//
// A TASK THAT STOPPED INCOMPLETE SAYS WHY IN THE OUTCOME'S PLACE. The first
// sentence of a report is what the work came to only when the work finished;
// for a run the wire cut or a worker that went in circles, the ending is the
// news, in the rail's own words for it ([endingWord]).
//
// A ROW WITH NO NAME OF ITS OWN IS NAMED FOR ITS CONVERSATION rather than drawn
// as a bare outcome, so every line still says whose work it was.
func ledgerTaskLine(entry session.TaskIndexEntry, row session.SessionRow) string {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = strings.TrimSpace(entry.Title)
	}
	// A PROGRAM'S WORK SAYS WHOSE IT IS, with its badge after its name
	// (programbadge.go) — the brackets alone, because this line is measured and
	// painted whole by the cell that draws it.
	label = programText(label, entry.Program)
	if label == "" {
		label = homeName(row)
	}
	outcome := endingWord(entry.Ending)
	if outcome == "" {
		outcome = switcherFirstLine(entry.Outcome)
	}
	if outcome == "" {
		return label
	}
	return label + rowSep + outcome
}

// ledgerMade is a line per file a conversation made since the look stamp:
// `made <name>`, with the conversation's title as its description.
func ledgerMade(world session.World, made []session.Artifact) []switcherRow {
	out := make([]switcherRow, 0, len(made))
	for _, artifact := range made {
		out = append(out, switcherRow{kind: switcherLedger, title: ledgerMadeWord + filepath.Base(artifact.Path),
			place: ledgerMadePlace, at: artifact.Created, path: artifact.Path,
			note: ledgerMadeIn(world, artifact.Session)})
	}
	return out
}

// ledgerMadeIn is the name of the conversation that made a file, and "" for one
// this world does not hold.
func ledgerMadeIn(world session.World, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, row := range world.Sessions() {
		if row.ID == id {
			return homeName(row)
		}
	}
	return ""
}

// ── the beat's half: which files, read where the disk may be read ───────────

// homeMadeIndex is the last reading of the files made since the look stamp, and
// what it was read against, so a beat on which neither the index nor the stamp
// moved costs one stat and no parse.
type homeMadeIndex struct {
	path string
	size int64
	mod  time.Time
	seen time.Time
	rows []session.Artifact
}

// madeSince is the files made since the look stamp, ON HOME'S BEAT AND NEVER ON
// A DRAW (place_home.go's [app.readSwitchLedger]).
//
// THE INDEX BELONGS TO THE MACHINE THAT MADE THE FILES. Over --host the rows
// arrive with the world ([session.World.Artifacts]) and this machine's index is
// somebody else's, so they are filtered by when they were made rather than read
// off a path here — the same rule [session.ArtifactsSince] keeps, over the rows
// the world already carried.
func (a *app) madeSince(seen time.Time) []session.Artifact {
	if seen.IsZero() {
		return nil
	}
	if a.hosted() {
		var out []session.Artifact
		for _, row := range a.home.world.Artifacts {
			if row.Created.After(seen) {
				out = append(out, row)
			}
		}
		return out
	}
	path := a.artifactsIndex()
	cache := &a.home.made
	info, err := os.Stat(path)
	if err != nil {
		*cache = homeMadeIndex{}
		return nil
	}
	if cache.path == path && cache.size == info.Size() && cache.mod.Equal(info.ModTime()) && cache.seen.Equal(seen) {
		return cache.rows
	}
	*cache = homeMadeIndex{path: path, size: info.Size(), mod: info.ModTime(), seen: seen,
		rows: session.ArtifactsSince(path, seen)}
	return cache.rows
}

// ── the doors ───────────────────────────────────────────────────────────────

// leftEnter is enter on a ledger line whose door is its own row: a task opens
// itself, a file opens itself. It reports false for a line whose door is a
// place, which [app.homeLedgerEnter] then opens as it always has.
func (a *app) leftEnter(line homeLine) (tea.Cmd, bool) {
	if line.cell == nil || line.cell.row == nil {
		return nil, false
	}
	row := line.cell.row
	switch {
	case row.task != nil:
		// THE TASK ITSELF, through the door the tasks place takes for the same
		// row: its live room when this window is running it, its record
		// otherwise (homepanel_running.go's [app.openTaskDoor]).
		return a.openTaskDoor(row.task), true
	case row.path != "" && a.hosted():
		// A FILE THE FAR MACHINE MADE IS FETCHED BEFORE IT IS OPENED, by the
		// same door `/open <path>` takes over a connection (remoteopen.go).
		if a.rfiles == nil {
			return nil, true
		}
		return a.openRemotePath(row.path), true
	case row.path != "":
		return a.homeOpenPath(row.path), true
	}
	return nil, false
}
