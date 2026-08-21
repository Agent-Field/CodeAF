package session

// ELSEWHERE: THE WORK THIS PROJECT'S OTHER WINDOWS HAVE OUT RIGHT NOW.
//
// taskpresence.go answers "which sessions are alive", one file per session, and
// world.go answers "what is going on everywhere" for the home page. This file is
// the third question, and it is the one a person sitting IN a session asks: the
// window beside this one is running something — where is it on my screen?
//
// It exists because neither of the other two answers it. The project's index
// (task_index.go) is the only list a session's own surfaces read, and AN
// ORDINARY TASK WRITES NO ROW UNTIL IT LANDS — [Agent.reportTaskNode] appends on
// a final state and on no other, so work another window started five minutes ago
// is not in the file at all and cannot be drawn from it. The rows that ARE
// written while work runs come from an orchestrated run (orchestrate.go, which
// files a `running` row up front and promises a second row closing it), and
// those are exactly the rows that go on claiming `running` forever when the
// window that wrote them dies.
//
// So the two halves of this file are the two halves of one honest answer:
//
//   - [Elsewhere.Tasks] is what the other windows SAY they have out, which is
//     the only place ordinary cross-window work can be read from.
//   - [Elsewhere.Runs] judges an index row that claims to be running, which is
//     [SessionRow.Runs]'s judgement said over a set of windows rather than over
//     one. THE RULE IS WORLD.GO'S AND IS NOT RESTATED HERE: a live-looking row
//     counts as live only when a fresh presence file from the session that wrote
//     it names that very node, and everything else is a record of work that
//     stopped.
//
// IT IS A READING AND NOT A SUBSCRIPTION. One readdir of the bucket, one small
// JSON per session folder, and one meta.json per session for its name — cheap
// enough to take on a clock and far too expensive to take on a frame, so every
// caller holds the value it was given for a few seconds and asks again
// (internal/tui3's own cache says how long).

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ElsewhereTask is one piece of work another window has out, with enough of the
// window on it to say whose it is.
//
// It carries the presence row's task WHOLE rather than flattening it, because
// the fields a surface wants — the title, the state, when it began — are already
// spelled there and copying them out would be a second spelling of each.
type ElsewhereTask struct {
	// SessionID is the conversation holding it, which is the id
	// [TaskIndexEntry.SessionID] records and the session folder is named.
	SessionID string
	// Session is what to CALL that window: the title it settled on, and "" for
	// one nothing ever named. THE EMPTINESS LAW HOLDS: a window with no name has
	// no name, and inventing one out of its id would put a string of hex where a
	// person expects words. What a surface draws instead of it is the surface's
	// own business.
	Session string
	// Task is the work itself, exactly as the other window described it.
	Task PresenceTask
}

// Elsewhere is one reading of every OTHER window open on one project.
//
// The rows are unexported because nothing outside this package should be able to
// read a claim without going through the methods that judge it — a caller
// holding raw presence rows is a caller one loop away from repeating a file's
// claim of liveness, which is the mistake world.go's first law exists to
// prevent.
type Elsewhere struct {
	rows  []SessionPresence
	names map[string]string
	// Read is when this reading was taken, so a cache holding it can say how old
	// it is without keeping a second stamp beside it.
	Read time.Time
}

// ReadElsewhere is every live window in ONE project bucket except the caller's
// own, with each window's name resolved once.
//
// exclude is the caller's own session ids, on [ReadProjectPresence]'s terms: the
// window a surface is being drawn in must never appear on it as somebody else.
//
// IT IS SEVERAL IDS AND NOT ONE, because "the caller" stopped being one
// conversation. A process can hold several sessions on one project at once — one
// on screen and the rest open behind it — and every one of them writes the same
// presence file every other window reads. Excluding only the one in front would
// put this process's OWN other conversations on its own `away` rows as
// `another window`, and tell somebody to go to a window that is two keystrokes
// away in the terminal they are already sitting in.
func ReadElsewhere(bucket string, now time.Time, exclude ...string) Elsewhere {
	out := Elsewhere{Read: now}
	rows := ReadProjectPresence(bucket, now, exclude...)
	if len(rows) == 0 {
		return out
	}
	out.rows = rows
	out.names = make(map[string]string, len(rows))
	for _, row := range rows {
		// THE NAME IS READ HERE AND NOT ON EVERY ASK. meta.json is a second file
		// per window; a surface calling [Elsewhere.Tasks] on a frame would open
		// it thirty times a second for a string that changes once a session.
		meta, _ := LoadMeta(row.Dir)
		if title := strings.TrimSpace(meta.Title); title != "" {
			out.names[row.SessionID] = title
		}
	}
	return out
}

// NewElsewhere is a reading built from presence rows a caller already holds,
// with each window's name supplied rather than looked up.
//
// IT APPLIES THE FRESHNESS RULE ITSELF and drops every row that fails it, which
// is the whole reason this door is safe to have. [ReadProjectPresence] already
// hands presence rows to anybody who asks, so the rows are not the secret —
// what this type is protecting is the JUDGEMENT, and a reading assembled out of
// claims nobody dated would let a caller smuggle a dead window past it. The
// clock is handed in for the same reason [readWorld]'s is: one instant, so two
// rows of one reading cannot age differently.
//
// names may be nil, and a window it does not name has no name (see
// [ElsewhereTask.Session]).
func NewElsewhere(now time.Time, names map[string]string, rows ...SessionPresence) Elsewhere {
	out := Elsewhere{Read: now, names: names}
	for _, row := range rows {
		if strings.TrimSpace(row.SessionID) == "" || !row.Fresh(now) {
			continue
		}
		out.rows = append(out.rows, row)
	}
	sortPresence(out.rows)
	return out
}

// Elsewhere is [ReadElsewhere] over this session's own project bucket, with this
// session left out of it.
//
// It answers the empty reading for a session with no folder — a memory-only
// conversation has no bucket to look in and no id to leave out, which is
// [Agent.ProjectPresence]'s own answer to the same shortage.
func (a *Agent) Elsewhere() Elsewhere { return a.ElsewhereExcept() }

// ElsewhereExcept is the same reading with MORE OF THE CALLER LEFT OUT: this
// session, and every other session id the caller says is its own.
//
// A surface holding several conversations at once passes the ids of the ones it
// is not drawing (internal/tui3's keeper.go). They are live windows on this
// machine and every OTHER terminal sees them as exactly that, correctly — but
// they are not elsewhere from here, and a row telling somebody to go to a window
// they are already inside is the same wrong refusal home used to make about
// another project.
func (a *Agent) ElsewhereExcept(others ...string) Elsewhere {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return Elsewhere{Read: time.Now()}
	}
	bucket := filepath.Dir(dir)
	if bucket == "" || bucket == "." {
		return Elsewhere{Read: time.Now()}
	}
	return ReadElsewhere(bucket, time.Now(), append([]string{a.config.Place.ID()}, others...)...)
}

// Any reports whether another window is open on this project at all. It is the
// cheapest form of the question and the one a surface asks before it decides
// whether a section exists.
func (e Elsewhere) Any() bool { return len(e.rows) > 0 }

// Runs reports whether one row of the project's index is work HAPPENING in
// another window at this instant.
//
// IT IS [SessionRow.Runs] OVER A SET, and the join is the same one on
// (SessionID, ID) — the pair both files spell the same way on purpose. A row
// this answers false for is a record: either work that landed, or work that was
// under way when a window went and that nobody is left to finish.
//
// IT ANSWERS FALSE FOR THIS SESSION'S OWN ROWS, always, because this session was
// excluded from the reading. That is deliberate and not a gap: a surface knows
// its own graph, which is a better answer about its own work than any file, and
// a second opinion here would be the one that disagreed with it.
func (e Elsewhere) Runs(entry TaskIndexEntry) bool {
	if !entry.Live() {
		return false
	}
	held := strings.TrimSpace(entry.SessionID)
	if held == "" {
		return false
	}
	for _, row := range e.rows {
		if row.SessionID == held {
			return row.Holds(entry.ID)
		}
	}
	return false
}

// Tasks is every piece of work the other windows have out, newest window first
// ([sortPresence]'s order, carried through).
//
// THIS IS THE ONLY DOOR ONTO ORDINARY CROSS-WINDOW WORK, for the reason in this
// file's header: a task that has not landed has no row in the project's index,
// so a surface that only read the index would show another window's finished
// work and none of what it is doing now.
func (e Elsewhere) Tasks() []ElsewhereTask {
	var out []ElsewhereTask
	for _, row := range e.rows {
		for _, task := range row.RunningTasks {
			out = append(out, ElsewhereTask{
				SessionID: row.SessionID,
				Session:   e.names[row.SessionID],
				Task:      task,
			})
		}
	}
	return out
}

// ── THE REST OF THE MACHINE ─────────────────────────────────────────────────
//
// Everything above this line is about ONE project, because for most of this
// file's life a terminal was one project. It is not any more: the keeper
// (internal/tui3) holds several conversations across several projects in one
// process, and "what is running outside this conversation" stopped meaning
// "what is running in this directory". The reading below is the wider answer,
// and it is asked for by name — `tasks` with `scope: "everywhere"` — never
// taken on an ordinary turn.

// OtherProjectTask is one running node in a project that is NOT this one.
//
// It carries [ElsewhereTask] whole rather than restating its three fields,
// because a row from another project is the same fact about a further-away
// window and the emptiness law on the window's name is already written there.
type OtherProjectTask struct {
	ElsewhereTask
	// Mine reports that the process reading this is the very process holding
	// that conversation — a session the keeper has open behind this one, in
	// another project.
	//
	// IT IS THE PID, AND THE PID IS NOT A LIVENESS TEST. taskpresence.go is
	// emphatic that a presence file's pid must never decide whether a session
	// is alive: pids are reused, and a state directory shared between two
	// machines makes the number meaningless. This is the other question. Asked
	// as "is this number MY number", a reused pid on another machine cannot
	// answer yes to a process that is not running, and the worst a collision
	// could do is call a stranger's window `open here` — while the alternative
	// is telling somebody to go to a window they are already sitting in, which
	// is the refusal [ReadElsewhere] exists to stop this build making.
	// Liveness is still [SessionRow.Live]'s, decided before this is read.
	Mine bool
}

// OtherProject is one project on this machine with live work in it: what to
// call it, where it is, and the nodes its live conversations have out.
type OtherProject struct {
	// Name is world.go's own naming of a bucket ([projectName]) and Path the
	// workspace its sessions recorded — "" when none of them said, which a
	// surface draws as nothing rather than as a guess.
	Name string
	Path string
	// Tasks are the running nodes, the newest-spoken conversation first.
	Tasks []OtherProjectTask
}

// ReadOtherProjects is every project under a places root EXCEPT one, with what
// each one's live conversations have out right now. A project with nothing
// running is not in the answer at all: a heading over no rows says nothing, and
// under the emptiness law a quiet project is quiet rather than "0 running".
//
// THE READING AND THE LIVENESS RULE ARE WORLD.GO'S AND ARE NOT RESTATED HERE.
// [readWorld] is the one reader of the whole machine, [SessionRow.Live] is the
// one judgement of whether a conversation's claim about itself is still worth
// believing, and [projectName] is the one naming of a bucket. A second copy of
// any of the three here would be the place the machine-wide answer and the home
// page came to disagree about the same window.
//
// skip is the caller's own bucket directory, already answered for above by
// [Elsewhere]; pid is the reading process, for [OtherProjectTask.Mine].
func ReadOtherProjects(root, skip string, now time.Time, pid int) []OtherProject {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	skip = strings.TrimSpace(skip)
	var out []OtherProject
	for _, project := range readWorld(root, now).Projects {
		if skip != "" && filepath.Clean(project.Dir) == filepath.Clean(skip) {
			continue
		}
		group := OtherProject{Name: project.Name, Path: project.Path}
		for _, row := range project.Sessions {
			if !row.Live {
				continue
			}
			for _, task := range row.Presence.RunningTasks {
				group.Tasks = append(group.Tasks, OtherProjectTask{
					ElsewhereTask: ElsewhereTask{
						SessionID: row.ID,
						Session:   row.Title,
						Task:      task,
					},
					Mine: pid != 0 && row.Presence.PID == pid,
				})
			}
		}
		if len(group.Tasks) == 0 {
			continue
		}
		out = append(out, group)
	}
	return out
}

// OtherProjects is [ReadOtherProjects] over the root this session's own folder
// sits in, with this session's project left out.
//
// THE ROOT COMES OUT OF THE PLACE AND NOT OUT OF [PlacesRoot]. The session
// folder already knows where it lives — bucket, then root, two elements up —
// and reading the state root a second way would be a second answer to go wrong
// the day one of them is pointed somewhere else. It is also what lets a test
// build a machine in a temporary directory.
//
// It answers nil for a conversation with no folder, which has no root to look
// in — [Agent.ElsewhereExcept]'s own answer to the same shortage.
func (a *Agent) OtherProjects(now time.Time) []OtherProject {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return nil
	}
	bucket := filepath.Dir(dir)
	if bucket == "" || bucket == "." {
		return nil
	}
	root := filepath.Dir(bucket)
	if root == "" || root == "." {
		return nil
	}
	return ReadOtherProjects(root, bucket, now, os.Getpid())
}
