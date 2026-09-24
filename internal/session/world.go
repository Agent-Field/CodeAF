// world.go is the reading of EVERYTHING THIS MACHINE HAS EVER WORKED ON.
//
// Every other reader in this package is asked about one place. [Peek] is asked
// about one transcript, [RecentSessions] about one directory, [ReadTaskIndex]
// about one project's index — and each of them is the right shape for the
// question a session sitting in a directory asks. This file answers the other
// question, the one a person asks when they are not standing anywhere in
// particular: what is going on, everywhere.
//
// It is a READ AND NOTHING ELSE. Nothing here creates a directory, writes a
// file, takes a lock it keeps, or opens a journal for replay. The whole layer is
// four system calls per session — a directory read, meta.json, presence.json,
// and one flock asked as a question — over files that already exist, so it can
// be run on a keystroke and again on a tick without being a thing anybody has
// to budget for.
//
// Two laws shape what it answers with:
//
//   - A LIVE-LOOKING ROW IS NOT A LIVE ROW. The project's index is append-only
//     and a task takes its row when it starts; a machine that lost power, or a
//     codeaf that was killed, leaves rows on disk that say `running` forever.
//     So this layer never repeats a file's claim of liveness. It asks the
//     SESSION, through the presence file it refreshes every few seconds
//     (taskpresence.go): a `running` index row counts as running only when a
//     FRESH presence row for that conversation names that very node, and every
//     other live-looking row is [TaskRollup.Incomplete] — the word the
//     interrupted-task outcome already uses.
//
//     A session with no presence at all is a conversation held open by a build
//     older than that file, and there the old rule still stands: the kernel is
//     asked who holds the journal ([InUse], the same flock the sweep asks) and
//     its rows are believed. That is not a second law, it is the same one with
//     less to go on — presence says which nodes a session has out, the lock says
//     only that somebody has the session, and a reader uses the best answer it
//     was given rather than pretending to the better one.
//
//   - NOBODY HAS TO GO LOOKING FOR "THIS ONE NEEDS ME". A conversation stopped
//     on a question says so in its presence file ([SessionPresence.NeedsPerson])
//     with the one line it is stopped on, and that is the single most valuable
//     fact this layer carries: it is what puts a row at the top of its project
//     ([sortSessions]) instead of leaving it to sink under every idle chat
//     somebody opened since.
//   - THE BUCKET NAME IS NOT A PROJECT NAME. The directory under v3/projects is
//     a workspace path with its separators replaced by dashes, and that encoding
//     is one-way on purpose (cmd/codeaf's chatv3_layout.go: decoding it would be
//     guessing which dashes were separators). The path comes back out of the
//     sessions' own meta.json, which records it, and a bucket whose sessions
//     will not say answers with the encoded name unchanged rather than with a
//     guess dressed up as a fact.

package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// PlacesRoot is where every project's bucket lives under the state root. It is
// [SweepHome]'s root, exported because the sweep is no longer the only caller
// that wants the whole machine — the same directory, asked a different question.
func PlacesRoot() string { return home.Join("v3", placesDirName) }

// RunsRoot is where an adaptive run's node transcripts live under the same state
// root — the directory [orchestrateJournalPath] writes into.
//
// IT IS A SIBLING OF THE PLACES ROOT AND NOT A CHILD OF IT, which is the whole
// reason this name exists. A run's rows are filed in a project's index like every
// other piece of work, and the transcript each row points at is over here; a door
// that took the places root for the machine's whole record therefore refused
// every adaptive journal it had itself written down ([RecordRoots] is the fix).
func RunsRoot() string { return home.Join("v3", runsDirName) }

// runsDirName is that folder's name, spelled once, because
// [orchestrateJournalPath] builds paths into it and this reads them back out —
// two spellings would be a boundary that stops matching the day one moved.
const runsDirName = "runs"

// LooseTasksRoot is the third place a node transcript can be: the parallel tree
// [taskJournalDir] writes into for a session that has no folder of its own.
//
// A CONVERSATION WITH NO PLACE STILL RUNS TASKS, and its nodes' journals go here
// rather than under a project bucket that does not exist. It is a real,
// currently-written directory and not an archaeological one, so a record
// boundary that left it out refused a hosted room its own live transcript.
func LooseTasksRoot() string { return home.Join("v3", looseTasksDirName) }

// looseTasksDirName is spelled once, for runsDirName's reason.
const looseTasksDirName = "tasks"

// RecordRoots is every directory this machine's task transcripts live under, and
// it is what an ENGINE measures a record read against ([ReadTaskRecordUnder]).
//
// THREE ROOTS BECAUSE THE RECORD IS IN THREE PLACES. An ordinary task writes its
// journal beside the conversation that ran it, under the places root; a
// conversation with no folder writes its nodes' journals into the parallel tree
// instead; and an adaptive run's nodes write theirs under the runs root. Every
// one of those rows is handed to a surface on the same world walk, so a door that
// admitted only the first told a person over a connection that most of their own
// machine's work "could not be read" — the file was there, and the boundary was
// wrong.
func RecordRoots() []string { return []string{PlacesRoot(), RunsRoot(), LooseTasksRoot()} }

// World is every project on this machine, newest first.
type World struct {
	// Projects are the buckets under the places root, ordered by when somebody
	// last spoke in one of their sessions.
	Projects []Project
	// Artifacts are the deliverables this machine's sessions made, newest first.
	// They ride with the world because home draws them under their conversation
	// rows, and a surface on another machine cannot read this machine's global
	// artifacts index. ReadWorld does not fill them because the places root does
	// not say where that index lives; the door that owns the state root does.
	Artifacts []Artifact
	// Read is when this reading was taken. Every age a surface draws is measured
	// from it rather than from time.Now(), so a list drawn from one scan does not
	// have rows aging at different instants.
	Read time.Time
}

// Sessions is every session in the world, flattened, newest first. It is what a
// search over everything ranks, and what a surface counts to decide whether it
// has anything at all to draw.
func (w World) Sessions() []SessionRow {
	var rows []SessionRow
	for _, project := range w.Projects {
		rows = append(rows, project.Sessions...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })
	return rows
}

// Project is one bucket: a workspace, the conversations held in it, and the
// index of work they commissioned.
type Project struct {
	// Bucket is the encoded directory name under the places root. It is an
	// address and never a name — see this file's header.
	Bucket string
	// Dir is the bucket's full path.
	Dir string
	// Path is the real workspace the sessions recorded, and "" when not one of
	// them said. It is read out of meta.json, which is the only authority.
	Path string
	// Name is what to CALL this project on a row: the last element of Path, `~`
	// for the home directory itself, and the encoded bucket name when nothing
	// recorded a path. The emptiness law is kept by never inventing a third
	// answer — a project with no name shows the name it does have.
	Name string
	// Sessions are the conversations held here, in triage order (see
	// [sortSessions]).
	Sessions []SessionRow
}

// At is when somebody last spoke in this project, which is its first session's
// stamp only when nothing is live — so it is taken over the whole list.
func (p Project) At() time.Time {
	var newest time.Time
	for _, row := range p.Sessions {
		if row.At.After(newest) {
			newest = row.At
		}
	}
	return newest
}

// Running is how many of this project's sessions have work actually in flight.
func (p Project) Running() int {
	count := 0
	for _, row := range p.Sessions {
		if row.Tasks.Running > 0 {
			count++
		}
	}
	return count
}

// NeedsPerson is how many of this project's conversations are stopped waiting
// on somebody. It is the one count worth putting on a heading: everything else
// a project can say is about work that is moving or work that is over, and this
// is the number that means "come back here".
func (p Project) NeedsPerson() int {
	count := 0
	for _, row := range p.Sessions {
		if row.NeedsPerson() {
			count++
		}
	}
	return count
}

// SessionRow is one conversation as the world sees it: its identity from
// meta.json, whether a window is holding it right now, and what the project's
// index says it ran.
type SessionRow struct {
	// ID is the session id, which is its folder's name.
	ID string
	// Dir is the session folder and Transcript its journal — the path a resume
	// is asked for.
	Dir        string
	Transcript string
	// Project is the bucket's display name and ProjectDir its path, carried on
	// the row so that a flattened list still cites where a hit came from.
	Project    string
	ProjectDir string
	// Title is the name the session settled on, and "" for one nothing ever
	// named. A surface derives a readable name; this layer does not invent one.
	Title string
	// Workspace is the tools root recorded for the conversation, and Owned marks
	// the session whose workspace is its own work/ directory.
	Workspace string
	Owned     bool
	// Model is what it was last on.
	Model string
	// At is when the PERSON last spoke, which is the ordering law everywhere in
	// this codebase (place.go's [Meta.LastUserAt]) and deliberately not the
	// file's modification time.
	At time.Time
	// Created is when the folder was minted.
	Created time.Time
	// Open reports that a window is holding this journal AT THIS INSTANT. It is
	// the kernel's answer and not a file's claim — see this file's header.
	Open bool
	// Presence is what the conversation SAYS it is doing, and Live whether it
	// said so recently enough to be believed ([SessionPresence.Fresh]). The
	// pair is deliberately not collapsed into one nullable value: a surface asks
	// "is this alive" far more often than it asks what the claim was, and Live
	// is the whole of that question.
	//
	// A live conversation and an OPEN one are not the same fact. Open is a lock
	// held; Live is a session refreshing a file and naming what it has out. A
	// build older than presence.json is open and not live, which is exactly the
	// case this layer degrades for rather than lies about.
	Presence SessionPresence
	Live     bool
	// Spend is what THE CONVERSATION ITSELF has cost — the turns and the
	// auxiliary calls beside them — and Tokens what it weighed, input plus
	// output as one sum. Both are read off meta.json, which the session stamps
	// at the end of every turn (placemeta.go), so this layer answers them
	// without opening a single transcript.
	//
	// THEY ARE NOT [TaskRollup.Spend] AND MUST NOT BE ADDED TO IT HERE. This is
	// the talking; that is the work the talking commissioned, and the two are
	// counted in two different files by two different writers. A surface that
	// wants the whole bill adds them where it draws it, and says so.
	//
	// Zero is "nobody could say" — a conversation held under a build older than
	// the stamp, or one that has not finished a turn — and under the emptiness
	// law a surface draws nothing for it.
	Spend  float64
	Tokens int
	// Tasks is what the project's index says this session ran.
	Tasks TaskRollup
	// Places are the folders this conversation turned out to be ABOUT beyond
	// the one it is standing in, newest first, exactly as places.go accrued
	// them onto the meta. Home draws them; nothing here weighs them.
	//
	// A MISSING FIELD IS EVERY CONVERSATION TODAY. The set arrived on meta.json
	// additively (place.go's [Meta.Places]), so a conversation held under an
	// older build has none and a surface draws nothing for it — which is the
	// emptiness law and not a conversation about nowhere.
	Places []PlaceRef
	// Archived says the person put this conversation away from home's resting
	// list ([Meta.Archived]); home gathers such rows under one folded line.
	Archived bool
	// ArchivedTasks is the person's per-task visibility choice from metadata.
	ArchivedTasks map[string]bool
}

// NeedsPerson reports that this conversation is stopped waiting on somebody. It
// answers false for a conversation that is not live at all, because a claim
// nobody has refreshed is not a claim about now — a window killed while a
// question was on screen is not still asking it.
func (r SessionRow) NeedsPerson() bool { return r.Live && r.Presence.NeedsPerson() }

// Doing is the word the conversation uses for itself — `working`, `waiting on
// you`, `idle` — and "" for one that is not live.
//
// IT IS THE PRESENCE FILE'S OWN WORD AND NOT A TRANSLATION OF IT. The states
// are already written in the words a person would use (taskpresence.go), and a
// surface mapping them to a second vocabulary would be the one place the two
// could come to disagree about what a session is doing.
func (r SessionRow) Doing() string {
	if !r.Live {
		return ""
	}
	return string(r.Presence.State)
}

// Reason is the one line behind a question this conversation is stopped on, and
// "" whenever there is not one — which a surface draws as nothing at all rather
// than as a placeholder.
func (r SessionRow) Reason() string {
	if !r.NeedsPerson() {
		return ""
	}
	return strings.TrimSpace(r.Presence.Reason)
}

// Runs reports whether one row of the project's index is work that is HAPPENING
// rather than work the file merely remembers starting.
//
// IT IS THE ONE PLACE THAT JUDGEMENT IS MADE. [rollUp] counts with it and every
// surface drawing a word beside a row asks it, so a screen can never say
// `running` on a row the count called incomplete. See this file's header for
// the rule itself; the ladder is: a live conversation's own list of what it has
// out, then — for a conversation too old to keep one — the lock.
func (r SessionRow) Runs(entry TaskIndexEntry) bool {
	if !entry.Live() {
		return false
	}
	if r.Live {
		// The join itself is [SessionPresence.Holds], which a session's own
		// surfaces ask through [Elsewhere.Runs] as well — one comparison, so the
		// home page and the roster can never disagree about whether a node is out.
		return r.Presence.Holds(entry.ID)
	}
	return r.Open
}

// Phase is which of its lives one running row of the index is in — the worker,
// the check, a repair round, the reading that sizes the work — in the words
// task_contract.go exports, and "" for a row that is merely working, one that has
// landed, and one nothing alive can say anything about.
//
// IT IS A LADDER OF TWO, AND THE ORDER IS THE POINT. [TaskIndexEntry.Phase] is
// filled by the process that HOLDS the graph and travels nowhere (task_index.go),
// so it is the right answer for this window's own work and empty for everybody
// else's; the presence file is what crosses a window ([PresenceTask.Phase]), and
// it is asked second so a session's own rows never take the slower answer.
//
// A ROW NOTHING IS BEHIND SAYS NOTHING. A conversation whose presence has gone
// stale is one no live claim exists for, and a phase read off its last file would
// be this surface narrating a minute that ended when the window did — the same
// judgement [SessionRow.Runs] makes one method up.
func (r SessionRow) Phase(entry TaskIndexEntry) string {
	if phase := strings.TrimSpace(entry.Phase); phase != "" {
		return phase
	}
	if !r.Live {
		return ""
	}
	return r.Presence.Phase(entry.ID)
}

// TaskRollup is one session's share of its project's index, counted.
type TaskRollup struct {
	// Rows are this session's entries, newest first, exactly as
	// [ReadTaskIndex] returned them.
	Rows []TaskIndexEntry
	// Running is work the index calls running or queued AND THE SESSION ITSELF
	// STILL NAMES as out — the only rows this layer will call live. See
	// [rollUp], which is the one place that judgement is made.
	Running int
	// Incomplete is every other live-looking row: work that was under way when
	// the window went, or that the conversation no longer counts among what it
	// has out. The word is the one the interrupted-task outcome already uses,
	// because it is the same fact.
	Incomplete int
	// Done and Failed are the landed rows, counted by what they came to.
	Done   int
	Failed int
	// Spend is the sum of the rows' cost, in dollars, and Tokens the sum of
	// what they weighed. Zero means nobody could say, and under the emptiness
	// law a surface draws nothing for either.
	Spend  float64
	Tokens int
	// Newest is when the most recent of these rows landed, and zero when the
	// only rows are ones that have not.
	Newest time.Time
}

// Total is how many rows this session owns.
func (r TaskRollup) Total() int { return len(r.Rows) }

// LandedTask is one piece of work that finished, with the conversation that
// ran it — the pair a "since you left" line is drawn from.
type LandedTask struct {
	Entry   TaskIndexEntry
	Session SessionRow
}

// LandedSince is every task in the world that LANDED after since, newest first:
// a row whose landing instant is past the stamp and whose status is neither
// running nor queued.
//
// IT READS NOTHING. The rows are the world's own ([SessionRow.Tasks], one index
// read per bucket already paid for by [ReadWorld]), so this is a filter a
// surface may call on a draw.
//
// EVERY LANDED ROW IS HERE, a task's parts and an adaptive run's workers
// included: [TaskIndexEntry.Parent] says which rows are, and a surface that
// wants one line per piece of work drops those itself rather than this deciding
// for every caller what counts as one.
//
// A ZERO STAMP ANSWERS NOTHING, for [ArtifactsSince]'s reason: a machine with no
// look yet has no origin, and the first look marks nothing as news (look.go).
func LandedSince(w *World, since time.Time) []LandedTask {
	if w == nil || since.IsZero() {
		return nil
	}
	var out []LandedTask
	for _, project := range w.Projects {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				if entry.Live() || !entry.EndedAt.After(since) {
					continue
				}
				out = append(out, LandedTask{Entry: entry, Session: row})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Entry.EndedAt.After(out[j].Entry.EndedAt)
	})
	return out
}

// ReadWorld is [ReadHome] over a named places root, which is what a test hands
// a directory it built.
//
// A root that is not there is a machine that has not held a conversation yet and
// answers an empty world, never an error: there is nothing a caller could do
// with the news, and drawing nothing is the right screen for it.
func ReadWorld(root string) World {
	return readWorld(root, time.Now())
}

// ReadHome is every project under this machine's state root.
func ReadHome() World { return ReadWorld(PlacesRoot()) }

// Adopt puts the conversation a window is sitting in into the world when the
// walk did not find it, and reports whether it had to.
//
// THE WALK CAN BE TOO EARLY FOR THE CONVERSATION IT WAS ASKED FROM. A fresh
// launch mints a folder and a meta.json with no `lastUserAt`, and [readSessionRow]
// skips exactly that shape on purpose — an empty shell is not a conversation
// somebody has had. But a person who opens home FROM that shell is sitting in
// it, and a screen that listed every conversation on the machine except the one
// on the terminal behind it would be emptier than the machine actually is. So
// the surface hands over what it knows — the journal it holds, the title, the
// workspace, the model — and this fills in whatever the folder can add, under
// the project the folder belongs to, named by the one rule every other project
// is named by ([projectName]).
//
// IT INVENTS NOTHING OUTSIDE THE ROOT. A journal that is not a session folder's
// `transcript.jsonl` two levels under `root` is a memory-only surface or a test
// fixture standing somewhere else, and the world answers for the root alone.
// A conversation the walk already found is left exactly as the walk read it.
func (w *World) Adopt(root string, seed SessionRow, now time.Time) bool {
	transcript := filepath.Clean(strings.TrimSpace(seed.Transcript))
	if transcript == "." || filepath.Base(transcript) != placeTranscript {
		return false
	}
	dir := filepath.Dir(transcript)
	bucketDir := filepath.Dir(dir)
	if filepath.Dir(bucketDir) != filepath.Clean(strings.TrimSpace(root)) {
		return false
	}
	for _, project := range w.Projects {
		for _, row := range project.Sessions {
			if row.Transcript == transcript {
				return false
			}
		}
	}
	meta, _ := LoadMeta(dir)
	row := SessionRow{
		ID:            filepath.Base(dir),
		Dir:           dir,
		Transcript:    transcript,
		Title:         firstWord(seed.Title, meta.Title),
		Workspace:     firstWord(seed.Workspace, meta.Workspace),
		Owned:         meta.Owned,
		Model:         firstWord(seed.Model, meta.Model),
		At:            meta.LastUserAt,
		Created:       meta.Created,
		Spend:         meta.SpentUSD,
		Tokens:        meta.Tokens,
		Open:          InUse(transcript),
		Archived:      meta.Archived,
		ArchivedTasks: meta.ArchivedTasks,
		Places:        metaPlaces(meta),
	}
	row.Presence, row.Live = ReadSessionPresence(dir, now)
	var mine []TaskIndexEntry
	for _, entry := range ReadTaskIndex(filepath.Join(bucketDir, taskIndexName)) {
		if strings.TrimSpace(entry.SessionID) == row.ID {
			mine = append(mine, entry)
		}
	}
	row.Tasks = rollUp(mine, row)
	at := -1
	for i := range w.Projects {
		if w.Projects[i].Dir == bucketDir {
			at = i
			break
		}
	}
	if at < 0 {
		bucket := filepath.Base(bucketDir)
		project := Project{Bucket: bucket, Dir: bucketDir, Path: projectPath(row)}
		project.Name = projectName(project.Path, bucket)
		w.Projects = append(w.Projects, project)
		at = len(w.Projects) - 1
	}
	project := &w.Projects[at]
	row.Project, row.ProjectDir = project.Name, project.Path
	project.Sessions = append(project.Sessions, row)
	sortSessions(project.Sessions)
	sort.SliceStable(w.Projects, func(i, j int) bool {
		return w.Projects[i].At().After(w.Projects[j].At())
	})
	return true
}

// firstWord is the first of two strings that says anything, trimmed.
func firstWord(a, b string) string {
	if a = strings.TrimSpace(a); a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

// readWorld is the testable one, with the clock handed in so that ages are
// measured from one instant.
func readWorld(root string, now time.Time) World {
	world := World{Read: now}
	buckets, err := os.ReadDir(root)
	if err != nil {
		// Including fs.ErrNotExist, which is the ordinary case on a fresh
		// machine and not a fault worth carrying up.
		if !errors.Is(err, fs.ErrNotExist) {
			return world
		}
		return world
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		project, ok := readProject(filepath.Join(root, bucket.Name()), bucket.Name(), now)
		if !ok {
			continue
		}
		world.Projects = append(world.Projects, project)
	}
	// Projects by when somebody was last in one, newest first. A project with a
	// session running sorts by that session's stamp like any other: home orders
	// the ROWS by attention and the SECTIONS by recency, so that the shape of
	// the page does not jump about while something runs.
	sort.SliceStable(world.Projects, func(i, j int) bool {
		return world.Projects[i].At().After(world.Projects[j].At())
	})
	return world
}

// readProject reads one bucket. It answers false for a bucket holding no
// conversation anybody ever spoke in — an encoded directory left behind by a
// launch that opened and closed is not a project, and a section header over no
// rows is a heading that says nothing.
func readProject(dir, bucket string, now time.Time) (Project, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Project{}, false
	}
	// The index is the BUCKET's, read once for every session in it
	// ([TaskIndexPath]'s law), and grouped below by the session that ran each
	// row.
	byTask := map[string][]TaskIndexEntry{}
	for _, row := range ReadTaskIndex(filepath.Join(dir, taskIndexName)) {
		id := strings.TrimSpace(row.SessionID)
		if id == "" {
			continue
		}
		byTask[id] = append(byTask[id], row)
	}

	project := Project{Bucket: bucket, Dir: dir}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		row, ok := readSessionRow(filepath.Join(dir, entry.Name()), entry.Name(), now)
		if !ok {
			continue
		}
		if project.Path == "" {
			project.Path = projectPath(row)
		}
		row.Tasks = rollUp(byTask[row.ID], row)
		project.Sessions = append(project.Sessions, row)
	}
	if len(project.Sessions) == 0 {
		return Project{}, false
	}
	project.Name = projectName(project.Path, bucket)
	for i := range project.Sessions {
		project.Sessions[i].Project = project.Name
		project.Sessions[i].ProjectDir = project.Path
	}
	sortSessions(project.Sessions)
	return project, true
}

// readSessionRow reads one session folder, and answers false for a folder that
// is not a conversation somebody has had.
//
// TWO RULES, AND THEY PULL IN OPPOSITE DIRECTIONS ON PURPOSE. A folder with no
// transcript in it is not a session at all and is skipped. A folder whose
// meta.json says nobody has ever spoken — a `lastUserAt` that is not there — is
// the empty shell a launch mints and the groom reuses (cmd/codeaf's
// v3ScanBucket), and it is skipped too. But a folder whose meta.json is MISSING
// or unreadable is kept, for the reason the sweep keeps it: a session that
// cannot say what it is, stays, because hiding somebody's conversation on the
// strength of a lookup file is the more expensive mistake.
func readSessionRow(dir, id string, now time.Time) (SessionRow, bool) {
	place := Place{Dir: dir}
	transcript := place.Transcript()
	info, err := os.Stat(transcript)
	if err != nil || info.IsDir() {
		return SessionRow{}, false
	}
	meta, _ := LoadMeta(dir)
	named := strings.TrimSpace(meta.ID) != ""
	if named && meta.LastUserAt.IsZero() {
		return SessionRow{}, false
	}
	at := meta.LastUserAt
	if at.IsZero() {
		at = info.ModTime()
	}
	// What the conversation says about itself, believed only if it said so
	// recently ([ReadSessionPresence] applies the window; nothing here second-
	// guesses it). A conversation that is not live answers the zero presence,
	// and every reader of this row asks Live before it asks anything else.
	presence, live := ReadSessionPresence(dir, now)
	return SessionRow{
		ID:            id,
		Dir:           dir,
		Transcript:    transcript,
		Title:         strings.TrimSpace(meta.Title),
		Workspace:     strings.TrimSpace(meta.Workspace),
		Owned:         meta.Owned,
		Model:         strings.TrimSpace(meta.Model),
		At:            at,
		Created:       meta.Created,
		Spend:         meta.SpentUSD,
		Tokens:        meta.Tokens,
		Open:          InUse(transcript),
		Presence:      presence,
		Live:          live,
		Archived:      meta.Archived,
		ArchivedTasks: meta.ArchivedTasks,
		Places:        metaPlaces(meta),
	}, true
}

// rollUp counts one session's rows, and it is the ONE place a running row is
// judged. See this file's header: a row saying `running` says what was true
// when it was appended, and the conversation itself says what is true now.
//
// THE JOIN IS ON (SessionID, ID), which is the pair both files spell the same
// way on purpose — [PresenceTask.ID] carries [TaskIndexEntry.ID]'s own
// spelling. A node the index calls running and the live conversation still
// names among what it has out is running. A node the LIVE conversation does not
// name is finished, abandoned or was never resumed, and it is incomplete
// whatever the index's oldest word for it was.
//
// The fallback is the second half of the header's first law: a session with no
// fresh presence is one held by a build that does not write the file, and there
// the lock is the best answer there is.
func rollUp(rows []TaskIndexEntry, held SessionRow) TaskRollup {
	rollup := TaskRollup{Rows: rows}
	for _, row := range rows {
		switch {
		case held.Runs(row):
			rollup.Running++
		case row.Live(), row.Status == string(TaskInterrupted):
			// A run nothing is driving any more is incomplete whether its row
			// still claims running or already says interrupted.
			rollup.Incomplete++
		case row.Status == string(TaskFailed):
			rollup.Failed++
		case row.Status == string(TaskDone):
			rollup.Done++
		}
		rollup.Spend += row.Cost
		rollup.Tokens += row.Tokens
		if row.EndedAt.After(rollup.Newest) {
			rollup.Newest = row.EndedAt
		}
	}
	return rollup
}

// projectPath is the workspace a session says it belongs to, and "" for one
// whose answer is inside the bucket rather than out in the world.
//
// An OWNED session's workspace is its own work/ directory (place.go), which
// names the session and not the project — so the launch directory, which is
// where the person was actually standing, is the one that answers for it.
func projectPath(row SessionRow) string {
	if !row.Owned && row.Workspace != "" {
		return row.Workspace
	}
	// The bucket of an owned session is encoded from where the person stood, and
	// meta.json records that as LaunchDir — but this row does not carry it,
	// because the surface has no use for it. Read it once, here.
	meta, _ := LoadMeta(row.Dir)
	if dir := strings.TrimSpace(meta.LaunchDir); dir != "" {
		return dir
	}
	return ""
}

// projectName is what a section header says. It is the workspace's last
// element, `~` for the home directory itself — a header reading "example"
// is the machine's answer to a question nobody asked — and the encoded bucket
// name when nothing recorded a path at all.
func projectName(path, bucket string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return bucket
	}
	if house, err := os.UserHomeDir(); err == nil && filepath.Clean(house) == filepath.Clean(path) {
		return "~"
	}
	if name := filepath.Base(path); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	return bucket
}

// sortSessions is the triage order inside a project, and it is the rail's order
// said in the terms this layer can see (the rail's [railGroup] ranks nodes;
// this ranks conversations):
//
//	needs somebody        the conversation is stopped on a question
//	work in flight        it is alive and something is out
//	work left running     rows that never landed, in a conversation nobody holds
//	everything else       by when the person last spoke, newest first
//
// THE TOP RUNG IS THE WHOLE POINT OF THE ORDER. A conversation waiting on an
// answer costs nothing to give and blocks everything behind it, and it is the
// one row that can sit at the bottom of a recency list for a day without
// anybody noticing — which is precisely why it goes first and why the presence
// file exists at all (taskpresence.go).
//
// The third rung is the one worth having under it: a conversation whose window
// went while a task was under way is the row a person most wants to find again,
// and by recency alone it sinks under every idle chat they opened since.
func sortSessions(rows []SessionRow) {
	rank := func(row SessionRow) int {
		switch {
		case row.NeedsPerson():
			return 0
		case row.Tasks.Running > 0:
			return 1
		case row.Tasks.Incomplete > 0:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := rank(rows[i]), rank(rows[j]); a != b {
			return a < b
		}
		return rows[i].At.After(rows[j].At)
	})
}
