package session

// PRESENCE: WHICH SESSIONS ARE ALIVE RIGHT NOW, AND WHICH OF THEM NEEDS ITS
// PERSON.
//
// task_index.go keeps what this project's work CAME TO — an append-only record,
// written once per landed node, read by anything asking about the past. It is
// the right shape for history and the wrong shape for the present: a row that
// said "running" when it was appended goes on saying so forever, and the moment
// the window that wrote it crashes or is closed, the claim is a lie nobody is
// left to correct. Opening that session again closes its rows
// ([Agent.closeInflightTaskIndexRows]) — but "again" may be next week, and the
// question this file answers is asked NOW, from a DIFFERENT window.
//
// So each live session keeps one small file saying what it is doing at this
// instant, and every other window may read it without opening a journal, taking
// a lock, or knowing anything about the session that wrote it.
//
// ── THE FOUR LAWS ──
//
//   - IT IS A CLAIM ABOUT A LIVE PROCESS, NEVER A RECORD. Nothing here is
//     history and nothing here is ever appended to. The file is rewritten whole
//     on every refresh and describes only this instant; the past is
//     task_index.go's, and a reader that wants to know what happened must go
//     there.
//
//   - IT IS ONLY TRUE WHILE IT IS FRESH. A process that is killed, panics or
//     loses its machine writes no farewell, so a file left behind would claim a
//     session that is gone. THE ONLY THING THAT MAKES A PRESENCE FILE TRUE IS
//     ITS AGE: [presenceHeartbeat] refreshes it while the session lives, and a
//     reader takes it as live only inside [presenceWindow], which is three
//     heartbeats. Past that the session is gone and every "running" it claims is
//     void. That is why there is no cleanup daemon and no pid liveness check —
//     staleness IS the cleanup, and it costs nothing and cannot go wrong.
//
//   - A WRITE THAT FAILS IS DROPPED IN SILENCE. This is a courtesy to other
//     windows, and a session must not break, stall or say anything because a
//     read-only disk would not take a two-hundred-byte file. It is the same
//     bargain [appendTaskIndex] makes, for the same reason.
//
//   - IT SAYS WHAT A PERSON WOULD SAY. The state words are "working",
//     "waiting on you" and "idle", and the reason beside them is one plain
//     line. Nothing in this file is machinery vocabulary, because everything in
//     it is written to be read on a surface.
//
// ── THE FORMAT, AND WHY ──
//
// ONE JSON OBJECT, written whole, in the session's own folder as presence.json:
//
//	~/.aforge/v3/projects/<encoded-workspace>/<session-id>/presence.json
//
// JSON and not JSONL because there is exactly one fact here and it is replaced
// rather than accumulated — the append-only shape task_index.go argues for is
// what you want when two processes share one file, and no two processes ever
// share this one. It sits in the SESSION folder rather than the project bucket
// for the same reason: one writer, one file, so a bucket read is a readdir and
// never a merge, and deleting a session folder takes its presence with it.
//
// The write is TEMP-AND-RENAME in the same directory, exactly as [SaveMeta] is:
// a reader must see the whole of one refresh or the whole of the one before it,
// never half of either. A rename inside a directory is atomic, which is the only
// crash-safety this file needs — there is nothing here worth recovering, so a
// crash mid-write costs one refresh and the next heartbeat repairs it.
//
// The object carries a SCHEMA NUMBER and a reader refuses any number it does not
// know. That is deliberately the conservative reading: a session running a newer
// build is a session this build cannot describe honestly, and the honest answer
// about a session you cannot describe is to say nothing about it at all.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// presenceName is the file, inside one session's folder. It is spelled here
// rather than in place.go because nothing but this file ever names it: a
// presence file is not part of what a session KEEPS, it is a thing a live
// process holds up while it is running.
const presenceName = "presence.json"

// presenceSchema is what this build writes and the only number it reads. See
// this file's header for why an unknown number answers "no presence" rather
// than a best guess.
const presenceSchema = 1

// The clock the whole file runs on. THE WINDOW IS DERIVED FROM THE HEARTBEAT
// AND NOT WRITTEN DOWN TWICE: a reader tolerating a different span than a
// writer promises is exactly the drift that makes a liveness signal lie, first
// by hiding live sessions and later by keeping dead ones on a surface.
const (
	// presenceHeartbeat is how often a live session refreshes its file even
	// when nothing about it has changed. It is short enough that a window
	// somebody killed leaves a surface within a glance and long enough that an
	// idle session costs a two-hundred-byte write every few seconds.
	presenceHeartbeat = 5 * time.Second
	// presenceWindow is how old a file may be and still be believed: three
	// heartbeats, so a session has to miss three in a row before another window
	// gives up on it. Two would call a session dead over one slow disk.
	presenceWindow = 3 * presenceHeartbeat
	// presenceCloseGrace bounds how long [Agent.Close] waits for the heartbeat
	// to remove the file. Past it the quit goes on and the file is left to go
	// stale, which the law above already covers — a person's quit must never
	// wait on a courtesy to another window.
	presenceCloseGrace = time.Second
)

// PresenceState is what one session is doing, in the words a person would use.
type PresenceState string

const (
	// PresenceWorking says a turn is running: the model is thinking, a tool is
	// out, work is happening.
	PresenceWorking PresenceState = "working"
	// PresenceWaiting says the session has asked its person something and can
	// go no further until they answer. It is the most valuable thing this file
	// says, and it OUTRANKS working: a turn blocked on a question is running in
	// the sense that a process exists, and stopped in every sense a person
	// cares about.
	PresenceWaiting PresenceState = "waiting on you"
	// PresenceIdle says nothing is running and nothing is being asked. The
	// session is open and the cursor is blinking.
	PresenceIdle PresenceState = "idle"
)

// PresenceTask is one piece of work a live session has out right now.
//
// It carries no cost, no token count and no outcome, and that is the whole
// distinction from [TaskIndexEntry]: this is the shortest thing that lets
// another window draw a row saying work is happening. Everything else about the
// task is in the project index, which is the file that answers questions about
// work rather than about processes.
type PresenceTask struct {
	// ID is the node's id inside the session that is running it, decimal —
	// [TaskIndexEntry.ID]'s own spelling, so a row here and a row there about
	// the same node are joinable on (SessionID, ID).
	ID string `json:"id"`
	// Title is the task's title, uncut.
	Title string `json:"title"`
	// State is the node's own word — "running" or "queued". Only unsettled
	// nodes are written here at all, so it is never a landed state: work that
	// finished is the index's to report, not presence's.
	State string `json:"state"`
	// StartedAt is when the node began, and it is zero for a queued node that
	// has not. A surface drawing an age must read that emptiness as "not yet"
	// rather than as an age of zero (the emptiness law).
	StartedAt time.Time `json:"startedAt,omitzero"`
}

// SessionPresence is one live session as another window sees it.
type SessionPresence struct {
	// Schema is [presenceSchema]. It is first in the struct because it is the
	// first thing a reader decides on.
	Schema int `json:"schema"`
	// SessionID is the conversation's id — the same string the journal header
	// carries, the session folder is named, and [TaskIndexEntry.SessionID]
	// records.
	SessionID string `json:"sessionId"`
	// Workspace is the REAL workspace path, exactly as [Meta.Workspace] records
	// it: the resolved project root, or the owned work/ directory. The encoded
	// bucket the folder sits in is not an identity and is not written here.
	Workspace string `json:"workspace"`
	// PID is the process holding this session, recorded so a person looking at
	// two windows can tell which is which. IT IS NOT CONSULTED FOR LIVENESS:
	// pids are reused, and a presence file may be read across a filesystem
	// shared by two machines where the number means nothing at all. Age is the
	// only liveness rule this file has (see the header).
	PID int `json:"pid"`
	// UpdatedAt is when this refresh was written, and it is the clock every
	// freshness judgement is made against. It is IN THE FILE rather than taken
	// from the file's mtime because a copy, a restore or a backup tool can
	// move an mtime without the session ever having been alive; the stamp
	// travels with the claim it dates.
	UpdatedAt time.Time `json:"updatedAt"`
	// State is what the session is doing.
	State PresenceState `json:"state"`
	// Reason is one line about WHY it is waiting, and it is empty for every
	// other state and for a question this file has no words for. A surface must
	// draw nothing at all when it is empty rather than a placeholder.
	Reason string `json:"reason,omitempty"`
	// RunningTasks is the work this session has out right now, in admission
	// order. Nil when there is none, which is most sessions.
	RunningTasks []PresenceTask `json:"runningTasks,omitempty"`
	// Dir is the session folder this was read from, filled in by the reader and
	// never written to the file — the folder already knows where it is, and a
	// path recorded inside it would be a second answer to go wrong the day a
	// state directory moves.
	Dir string `json:"-"`
}

// NeedsPerson reports whether this session is stopped waiting on somebody.
//
// It is a METHOD and not a field, because [SessionPresence.State] already says
// so and a bool written beside it would be a second source of truth that could
// disagree with the word next to it on the same row.
func (p SessionPresence) NeedsPerson() bool { return p.State == PresenceWaiting }

// Holds reports whether this session names one node id among the work it has
// out at this instant.
//
// IT IS THE JOIN, AND IT IS WRITTEN ONCE. Two surfaces now ask the same question
// of a presence row — the home page, through [SessionRow.Runs], and a session's
// own roster and history page, through [Elsewhere.Runs] — and a second loop
// spelling the same comparison is the second place the two could come to
// disagree about whether a task is running. The id is trimmed on both sides
// because it is a handle a person types and a file records, not a number
// anything does arithmetic on ([TaskIndexEntry.ID]).
//
// A CALLER MUST ALREADY HAVE DECIDED THIS ROW IS FRESH. Nothing here looks at
// the clock: [ReadSessionPresence] refuses a stale file outright, so a row that
// reached a caller is a row inside the window, and asking again here would be a
// second freshness rule to keep in step with the first.
func (p SessionPresence) Holds(id string) bool {
	want := strings.TrimSpace(id)
	if want == "" {
		return false
	}
	for _, task := range p.RunningTasks {
		if strings.TrimSpace(task.ID) == want {
			return true
		}
	}
	return false
}

// Fresh reports whether this claim is still worth believing at now — see the
// second law in this file's header.
func (p SessionPresence) Fresh(now time.Time) bool {
	if p.UpdatedAt.IsZero() {
		return false
	}
	// A stamp in the FUTURE is believed rather than refused: a machine whose
	// clock is a minute ahead of ours is the ordinary cause, and treating that
	// session as dead would hide a live window over a clock skew.
	return !p.UpdatedAt.Before(now.Add(-presenceWindow))
}

// ── the writer ──────────────────────────────────────────────────────────────

// presenceDesk is the one live session's own presence: where its file is, the
// heartbeat that refreshes it, and the reasons behind whatever it is currently
// waiting on.
//
// It is a struct on the agent rather than a set of fields because everything in
// it belongs to one goroutine's lifetime, and because the agent must be able to
// keep asking questions of it while its own lock is held (see
// [Agent.nudgePresence]).
type presenceDesk struct {
	agent *Agent
	path  string
	// stop is closed once by [Agent.stopPresence]; done is closed by the
	// heartbeat when it has removed the file and gone.
	stop chan struct{}
	done chan struct{}
	once sync.Once
	// nudge carries "something changed, write now". It is buffered to one and
	// sent to without blocking, because its senders hold the agent's lock and
	// a presence file must never be able to stall a turn.
	nudge chan struct{}
	// every is how often the heartbeat writes, and 0 is [presenceHeartbeat]. It
	// is a field for the tests, on [TaskGraph.pollEvery]'s own terms: five real
	// seconds is the right cadence for a machine and the wrong one for a test
	// suite. Nothing outside a test ever sets it.
	every time.Duration

	// mu guards the reasons below and NOTHING else. It is the desk's own lock
	// and is never held across a write or across the agent's lock.
	mu sync.Mutex
	// asks is the questions this session has out, in the order they were
	// raised, with the one line each of them would be described by. Only the
	// approval gate fills it (consent.go); every other lane a person can be
	// asked on still makes the session say it is waiting, with no reason — see
	// [Agent.presenceSnapshot].
	asks []presenceAsk
	// askSeq names them, and it is the desk's own numbering rather than
	// consent's: the reason is banked before the question is minted, so there
	// is no id to borrow yet.
	askSeq uint64
}

type presenceAsk struct {
	id     uint64
	reason string
}

// startPresence begins this session's presence, or does nothing at all.
//
// TWO KINDS OF AGENT KEEP NO PRESENCE and both answer here. A session with no
// folder — a memory-only conversation, or the legacy flat layout — has nowhere
// to put the file and no id a reader could join it on. AND A TASK NODE'S AGENT
// IS NOT A SESSION: it is one node of the conversation that spawned it, running
// in a worktree with nobody in front of it, and its work is already announced by
// its parent's own presence. A node writing here would put a second, competing
// claim about "this session" in play (the argument [Agent.recoverTasks] and
// [Agent.closeInflightTaskIndexRows] both make about InTask).
//
// It is called once, from the constructor, before the agent is reachable — which
// is what lets [Agent.presence] be read everywhere afterwards without a lock,
// exactly as [Agent.id] and [Agent.cacheKey] are.
func (a *Agent) startPresence() {
	if a.config.InTask {
		return
	}
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	desk := &presenceDesk{
		agent: a,
		path:  filepath.Join(dir, presenceName),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		nudge: make(chan struct{}, 1),
	}
	a.presence = desk
	go desk.beat()
}

// stopPresence ends the presence and takes the file with it.
//
// THE CLEAN CLOSE REMOVES THE FILE, so a window somebody quit stops appearing on
// another window's surface at once rather than at the end of the freshness
// window. The window is what covers every UNclean end — a kill, a panic, a
// laptop closing — and this is the courtesy for the ordinary one.
func (a *Agent) stopPresence() {
	desk := a.presence
	if desk == nil {
		return
	}
	desk.once.Do(func() { close(desk.stop) })
	timer := time.NewTimer(presenceCloseGrace)
	defer timer.Stop()
	select {
	case <-desk.done:
	case <-timer.C:
		// The heartbeat is wedged on a disk that will not answer. The quit goes
		// on without it and the file it left behind goes stale on its own.
	}
}

// nudgePresence asks for a refresh now, because something a reader cares about
// just changed.
//
// IT NEVER BLOCKS AND NEVER TAKES THE AGENT'S LOCK, because its callers are
// holding that lock when they call it (agent.go's turn seam). A nudge that finds
// the channel already full does nothing, which is right: the write it would have
// asked for has not happened yet and will carry this change with it.
func (a *Agent) nudgePresence() {
	desk := a.presence
	if desk == nil {
		return
	}
	select {
	case desk.nudge <- struct{}{}:
	default:
	}
}

// presenceWaiting banks the one line describing a question this session has just
// put to its person, and answers the func that takes it back down. The refresh
// is asked for at both ends, so "waiting on you" appears and clears without
// anybody waiting a heartbeat for it.
func (a *Agent) presenceWaiting(reason string) func() {
	desk := a.presence
	if desk == nil {
		return func() {}
	}
	desk.mu.Lock()
	desk.askSeq++
	id := desk.askSeq
	desk.asks = append(desk.asks, presenceAsk{id: id, reason: strings.TrimSpace(reason)})
	desk.mu.Unlock()
	a.nudgePresence()
	return func() {
		desk.mu.Lock()
		for at, ask := range desk.asks {
			if ask.id == id {
				desk.asks = append(desk.asks[:at], desk.asks[at+1:]...)
				break
			}
		}
		desk.mu.Unlock()
		a.nudgePresence()
	}
}

// beat is the heartbeat: one write now, one on every nudge, one on every tick,
// and a removal on the way out.
func (d *presenceDesk) beat() {
	defer close(d.done)
	every := d.every
	if every <= 0 {
		every = presenceHeartbeat
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	// The first write happens before the first tick so a session announces
	// itself the moment it opens rather than a heartbeat later.
	d.write()
	for {
		select {
		case <-d.stop:
			d.remove()
			return
		case <-d.nudge:
			d.write()
		case <-ticker.C:
			d.write()
		}
	}
}

// write puts this instant on disk, whole. Every failure is silence — see the
// third law in this file's header.
func (d *presenceDesk) write() {
	raw, err := json.Marshal(d.agent.presenceSnapshot(time.Now()))
	if err != nil {
		return
	}
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	// TEMP-AND-RENAME IN THE SAME DIRECTORY, so a reader never sees half a
	// refresh and a crash mid-write costs one heartbeat. The dot prefix keeps
	// a temp file that outlived its process from looking like anything a
	// person needs to think about.
	tmp, err := os.CreateTemp(dir, ".presence-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	if err := os.Rename(name, d.path); err != nil {
		os.Remove(name)
	}
}

// remove takes the file away on a clean close. Like every other write here the
// outcome is dropped: a file that is already gone — a session folder swept out
// from under this process — is the ordinary case, and a file that will not go is
// one the freshness window is about to make meaningless anyway.
func (d *presenceDesk) remove() {
	_ = os.Remove(d.path)
}

// presenceSnapshot is this session as its presence file describes it.
//
// The two halves are read under two different locks and NEVER at the same time:
// the conversation's state under a.mu, then the graph under its own. That is
// this package's standing rule about the task graph (session.go's `tasks`
// field) — a node's lock is taken by goroutines that finish minutes later, and
// holding a.mu across one is holding the lock Interrupt has to be able to take.
func (a *Agent) presenceSnapshot(now time.Time) SessionPresence {
	a.mu.Lock()
	snapshot := SessionPresence{
		Schema:    presenceSchema,
		SessionID: a.sessionID(),
		Workspace: a.config.Workspace,
		PID:       os.Getpid(),
		UpdatedAt: now,
		State:     PresenceIdle,
	}
	// EVERY LANE A PERSON CAN BE ASKED ON COUNTS, not only the approval gate:
	// a session stopped on a connect question, a sub-harness offer or a task
	// proposal is just as stuck, and a surface that only knew about consent
	// would leave those windows looking idle while they waited (consent.go,
	// connect.go, harness.go, task.go each hold one of these).
	waiting := len(a.consent) > 0 || len(a.connectAsks) > 0 || len(a.harnessAsks) > 0 || len(a.taskAnswers) > 0
	switch {
	case waiting:
		// WAITING OUTRANKS WORKING. A turn blocked on a question still has
		// a.running set, and the thing worth saying about it is the question.
		snapshot.State = PresenceWaiting
	case a.running:
		snapshot.State = PresenceWorking
	}
	a.mu.Unlock()

	if snapshot.State == PresenceWaiting {
		snapshot.Reason = a.presenceReason()
	}
	snapshot.RunningTasks = a.presenceTasks()
	return snapshot
}

// presenceReason is the oldest outstanding question's one line, or "" when the
// lane that raised it had no words to offer. Empty is an honest answer and the
// only alternative — inventing a sentence about a question this file cannot see
// — would put words on a surface that nothing in the session ever said.
func (a *Agent) presenceReason() string {
	desk := a.presence
	if desk == nil {
		return ""
	}
	desk.mu.Lock()
	defer desk.mu.Unlock()
	if len(desk.asks) == 0 {
		return ""
	}
	return desk.asks[0].reason
}

// presenceTasks is the work this session has out, read off the graph WITHOUT
// building one: most conversations never groom a task, and a heartbeat must not
// be the thing that gives them a scheduler ([Agent.liveTaskRows]'s own terms).
//
// ONLY UNSETTLED NODES ARE HERE. A node that landed is the project index's to
// report, and a presence file listing finished work would be the record this
// file refuses to be.
func (a *Agent) presenceTasks() []PresenceTask {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var out []PresenceTask
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.state.settled() {
			continue
		}
		out = append(out, PresenceTask{
			ID:        strconv.FormatUint(node.id, 10),
			Title:     strings.TrimSpace(node.spec.title),
			State:     string(node.state),
			StartedAt: node.started,
		})
	}
	return out
}

// ── the readers ─────────────────────────────────────────────────────────────

// ReadSessionPresence reads one session folder's presence, and reports false for
// every reason there is not one to believe: no file, an unreadable file, a
// schema this build does not know, a session with no id, and — the case the
// whole file turns on — a claim older than [presenceWindow].
//
// The session folder's own name is trusted over the id inside the file when the
// file has none, because the folder IS the session id (place.go's [Place.ID]).
func ReadSessionPresence(dir string, now time.Time) (SessionPresence, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return SessionPresence{}, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, presenceName))
	if err != nil {
		return SessionPresence{}, false
	}
	var presence SessionPresence
	if json.Unmarshal(raw, &presence) != nil {
		return SessionPresence{}, false
	}
	if presence.Schema != presenceSchema {
		return SessionPresence{}, false
	}
	if strings.TrimSpace(presence.SessionID) == "" {
		presence.SessionID = filepath.Base(dir)
	}
	if strings.TrimSpace(presence.SessionID) == "" || !presence.Fresh(now) {
		return SessionPresence{}, false
	}
	presence.Dir = dir
	return presence, true
}

// ReadProjectPresence is every live session in ONE project bucket — the
// directory holding a workspace's session folders — most recently refreshed
// first.
//
// exclude is the caller's OWN session id, dropped from the answer. Every caller
// of this has one: a rail drawing "what else is running" must not draw the
// window it is being drawn in, and making that the caller's business would be
// making it the caller's bug. An empty exclude drops nothing.
func ReadProjectPresence(bucket string, now time.Time, exclude string) []SessionPresence {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil
	}
	entries, err := os.ReadDir(bucket)
	if err != nil {
		// A bucket that is not there is a project nobody has opened, which is
		// an answer and not a failure — [SweepPlaces]'s own reading.
		return nil
	}
	exclude = strings.TrimSpace(exclude)
	var out []SessionPresence
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		presence, ok := ReadSessionPresence(filepath.Join(bucket, entry.Name()), now)
		if !ok || (exclude != "" && presence.SessionID == exclude) {
			continue
		}
		out = append(out, presence)
	}
	sortPresence(out)
	return out
}

// ReadAllPresence is every live session on this machine, across every project:
// one readdir of the projects root and one of each bucket under it. It is the
// union home is built on, and it opens no journal and takes no lock.
func ReadAllPresence(root string, now time.Time, exclude string) []SessionPresence {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	buckets, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []SessionPresence
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		out = append(out, ReadProjectPresence(filepath.Join(root, bucket.Name()), now, exclude)...)
	}
	sortPresence(out)
	return out
}

// HomePresence is [ReadAllPresence] over this machine's real state root, which
// is the same path [SweepHome] sweeps. It is the one door a surface should need.
func HomePresence(exclude string) []SessionPresence {
	return ReadAllPresence(home.Join("v3", placesDirName), time.Now(), exclude)
}

// ProjectPresence is the OTHER live windows open on this session's project —
// this agent's own bucket, with this agent left out of it.
//
// It answers nil for a session with no folder, which has no bucket to look in
// and no id to leave out.
func (a *Agent) ProjectPresence() []SessionPresence {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return nil
	}
	bucket := filepath.Dir(dir)
	if bucket == "" || bucket == "." {
		return nil
	}
	return ReadProjectPresence(bucket, time.Now(), a.config.Place.ID())
}

// sortPresence puts the rows in one stable order: most recently refreshed
// first, and by session id for two written in the same instant, so a readdir's
// order never reaches a surface. TRIAGE IS NOT DONE HERE — which of these needs
// somebody most is a question about how a surface groups rows (internal/tui3's
// railGroup), and answering it twice in two places is how the two come to
// disagree.
func sortPresence(rows []SessionPresence) {
	sort.SliceStable(rows, func(a, b int) bool {
		if !rows[a].UpdatedAt.Equal(rows[b].UpdatedAt) {
			return rows[a].UpdatedAt.After(rows[b].UpdatedAt)
		}
		return rows[a].SessionID < rows[b].SessionID
	})
}
