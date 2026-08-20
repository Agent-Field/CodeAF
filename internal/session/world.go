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
// three system calls per session — a directory read, meta.json, and one flock
// asked as a question — over files that already exist, so it can be run on a
// keystroke and again on a tick without being a thing anybody has to budget for.
//
// Two laws shape what it answers with:
//
//   - A LIVE-LOOKING ROW IS NOT A LIVE ROW. The project's index is append-only
//     and a task takes its row when it starts; a machine that lost power, or an
//     aforge that was killed, leaves rows on disk that say `running` forever.
//     So this layer never repeats a file's claim of liveness. It asks the kernel
//     who is holding the journal ([InUse], the same flock the sweep asks) and a
//     running row belongs to a session nobody is holding is reported as
//     [TaskRollup.Incomplete] — the word the interrupted-task outcome already
//     uses — rather than as work in flight.
//   - THE BUCKET NAME IS NOT A PROJECT NAME. The directory under v3/projects is
//     a workspace path with its separators replaced by dashes, and that encoding
//     is one-way on purpose (cmd/aforge's chatv3_layout.go: decoding it would be
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

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// PlacesRoot is where every project's bucket lives under the state root. It is
// [SweepHome]'s root, exported because the sweep is no longer the only caller
// that wants the whole machine — the same directory, asked a different question.
func PlacesRoot() string { return home.Join("v3", placesDirName) }

// World is every project on this machine, newest first.
type World struct {
	// Projects are the buckets under the places root, ordered by when somebody
	// last spoke in one of their sessions.
	Projects []Project
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
	// Tasks is what the project's index says this session ran.
	Tasks TaskRollup
}

// TaskRollup is one session's share of its project's index, counted.
type TaskRollup struct {
	// Rows are this session's entries, newest first, exactly as
	// [ReadTaskIndex] returned them.
	Rows []TaskIndexEntry
	// Running is work the index calls running or queued IN A SESSION SOMEBODY IS
	// HOLDING — the only rows this layer will call live.
	Running int
	// Incomplete is the same rows in a session nobody is holding: work that was
	// under way when the window went. The word is the one the interrupted-task
	// outcome already uses, because it is the same fact.
	Incomplete int
	// Done and Failed are the landed rows, counted by what they came to.
	Done   int
	Failed int
	// Spend is the sum of the rows' cost, in dollars. Zero means nobody could
	// say, and under the emptiness law a surface draws nothing for it.
	Spend float64
	// Newest is when the most recent of these rows landed, and zero when the
	// only rows are ones that have not.
	Newest time.Time
}

// Total is how many rows this session owns.
func (r TaskRollup) Total() int { return len(r.Rows) }

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
		project, ok := readProject(filepath.Join(root, bucket.Name()), bucket.Name())
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
func readProject(dir, bucket string) (Project, bool) {
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
		row, ok := readSessionRow(filepath.Join(dir, entry.Name()), entry.Name())
		if !ok {
			continue
		}
		if project.Path == "" {
			project.Path = projectPath(row)
		}
		row.Tasks = rollUp(byTask[row.ID], row.Open)
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
// the empty shell a launch mints and the groom reuses (cmd/aforge's
// v3ScanBucket), and it is skipped too. But a folder whose meta.json is MISSING
// or unreadable is kept, for the reason the sweep keeps it: a session that
// cannot say what it is, stays, because hiding somebody's conversation on the
// strength of a lookup file is the more expensive mistake.
func readSessionRow(dir, id string) (SessionRow, bool) {
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
	return SessionRow{
		ID:         id,
		Dir:        dir,
		Transcript: transcript,
		Title:      strings.TrimSpace(meta.Title),
		Workspace:  strings.TrimSpace(meta.Workspace),
		Owned:      meta.Owned,
		Model:      strings.TrimSpace(meta.Model),
		At:         at,
		Created:    meta.Created,
		Open:       InUse(transcript),
	}, true
}

// rollUp counts one session's rows, and it is the ONE place a running row is
// judged. See this file's header: a row saying `running` is a row saying what
// was true when it was written, and only the lock says what is true now.
func rollUp(rows []TaskIndexEntry, open bool) TaskRollup {
	rollup := TaskRollup{Rows: rows}
	for _, row := range rows {
		switch {
		case row.Live() && open:
			rollup.Running++
		case row.Live():
			rollup.Incomplete++
		case row.Status == string(TaskFailed):
			rollup.Failed++
		case row.Status == string(TaskDone):
			rollup.Done++
		}
		rollup.Spend += row.Cost
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
// element, `~` for the home directory itself — a header reading "santoshkumar"
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
//	work in flight        somebody is holding it and something is running
//	work left running     rows that never landed, in a session nobody holds
//	everything else       by when the person last spoke, newest first
//
// The middle rung is the one worth having. A session whose window went while a
// task was under way is the row a person most wants to find again, and by
// recency alone it sinks under every idle chat they opened since.
func sortSessions(rows []SessionRow) {
	rank := func(row SessionRow) int {
		switch {
		case row.Tasks.Running > 0:
			return 0
		case row.Tasks.Incomplete > 0:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := rank(rows[i]), rank(rows[j]); a != b {
			return a < b
		}
		return rows[i].At.After(rows[j].At)
	})
}
