package session

// THE STORE RECORDS LIVENESS AT THE CADENCE OF THE WORK.
//
// ── THE HOLE IT FILLS ──
//
// tasks.json is written at ADMISSION and at LANDING (task_store.go), and between
// those two moments a node can spend eleven minutes doing anything at all. From
// outside the process the row says `"state": "running"` and nothing else, so a
// node calling a model every twenty seconds and a node wedged on a build that
// will never return are the same two bytes on disk. Every reader that needed the
// difference invented its own answer: the marathon runner reconstructs liveness
// from the mtimes of the node journals, a second window has the session's
// presence file (taskpresence.go) and its five-second clock, and anybody holding
// only the checkpoint has nothing.
//
// So each RUNNING node writes its own pulse, and everything that wanted to know
// gets it for free.
//
// ── WHAT IT SAYS, AND WHEN ──
//
// Three facts and no more: WHICH PHASE the node is in, WHEN ITS LAST REQUEST
// STARTED, and WHEN THAT REQUEST CAME BACK. Together they answer the only
// question an outside reader is really asking — is this thing moving — without
// anybody having to model what "moving" means: a request that started four
// minutes ago and has not come back is a node inside a long call, a request that
// came back four minutes ago with nothing since is a node inside a long tool, and
// a row whose numbers are seconds old is a node working normally.
//
// IT IS WRITTEN AT EVERY PROVIDER CALL BOUNDARY, which is the cadence of the work
// itself rather than a clock somebody chose: loop.go takes the pulse either side
// of [Agent.completeWithRetry], the one line in this package where a request
// actually goes out. There is no ticker, nothing to start and nothing to stop, so
// a node that is genuinely wedged writes nothing new — which is the news.
//
// ── A SIDECAR, NOT THE CHECKPOINT ──
//
// tasks.json is the graph, whole, re-encoded and atomically replaced on every
// write; putting a pulse in it would mean re-serializing every node in the family
// a few times a minute and racing every landing that does the same. So the pulse
// is a FILE PER NODE, two hundred bytes, beside the node's own transcript:
//
//	<session folder>/tasks/<id>.beat.json
//
// The store owns the name ([taskStore.beatPath]) and the row names the file
// (task_store.go's taskRecord.Beat), so a reader that has tasks.json open never
// has to guess at a path — and a reader that only has the directory finds the
// pulse sitting next to the transcript it was already reading mtimes off.
//
// ── AND STALENESS IS THE CLEANUP ──
//
// The file is removed when the node lands, because the checkpoint's own row is
// the authority the moment there is a final state to read. A process that is
// killed removes nothing, and a beat left behind is read exactly as
// taskpresence.go reads a stale presence file: by its age, which is the only
// thing that was ever true about it. A write that fails is dropped in silence,
// for [appendTaskIndex]'s reason — a node must not stall or fail because a
// two-hundred-byte courtesy could not be written.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The phases a running node can be in, in the plain words every other file here
// writes. They are the node's three lives and not the machinery's names for
// them: the check is "checking" and never "audit", for task_audit.go's
// vocabulary law — this file is read by other programs, but the words a program
// reads end up on somebody's screen.
const (
	// taskBeatWorking is the node's own worker, in its worktree.
	taskBeatWorking = "working"
	// taskBeatChecking is the gate looking at what the worker left.
	taskBeatChecking = "checking"
	// taskBeatRepairing is a repair round closing named gaps.
	taskBeatRepairing = "repairing"
)

// taskBeatSuffix names the sidecar. It is not `.jsonl` and it is not `_<id>`,
// so [findTaskJournal] — which looks for `<stamp>_<id>.jsonl` in this same
// directory — can never mistake a pulse for a transcript.
const taskBeatSuffix = ".beat.json"

// taskBeatRow is one running node's pulse as the file holds it.
type taskBeatRow struct {
	// Node is the node's id, and Title what a reader would call it.
	Node  uint64 `json:"node"`
	Title string `json:"title,omitempty"`
	// Phase is one of the three words above.
	Phase string `json:"phase"`
	// Started is when the node began, so a reader can age the whole run and not
	// only the last request.
	Started time.Time `json:"started"`
	// Requests is how many requests this node's agents have STARTED — the
	// worker's, the checker's and every repair round's, because they are all the
	// same node working. A count rather than a rate: a reader comparing two
	// readings gets the rate, and a rate computed here would be this file having
	// an opinion.
	Requests int `json:"requests"`
	// RequestStarted and RequestFinished are the last request's two edges. When
	// started is the later of the two, a request is in flight.
	RequestStarted  time.Time `json:"request_started,omitempty"`
	RequestFinished time.Time `json:"request_finished,omitempty"`
	// UpdatedAt is when this file was last written, which is the one fact that
	// makes the rest of it believable.
	UpdatedAt time.Time `json:"updated_at"`
}

// working reports whether a request is in flight: the last start is not answered
// by a finish.
func (r taskBeatRow) working() bool {
	return !r.RequestStarted.IsZero() && r.RequestFinished.Before(r.RequestStarted)
}

// taskBeat is one node's pulse and the file it writes.
//
// Its mutex is its own and is taken by nothing else. It is written from every
// agent standing in for the node — the worker, the checker, a repair round — and
// it must never be a reason any of them waits on the graph.
type taskBeat struct {
	mu   sync.Mutex
	path string
	row  taskBeatRow
}

// newTaskBeat builds a node's pulse. It writes nothing: the first row goes down
// when the node is armed, off the graph's lock.
func newTaskBeat(path string, id uint64, title string, started time.Time) *taskBeat {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if started.IsZero() {
		started = time.Now()
	}
	return &taskBeat{path: path, row: taskBeatRow{
		Node:    id,
		Title:   title,
		Phase:   taskBeatWorking,
		Started: started,
	}}
}

// arm puts the first row down: the node exists, it is working, and it has made
// no requests yet. It is the only write here that is not a call boundary, and it
// is what makes a node that dies before its first request still visible as one
// that started.
func (b *taskBeat) arm() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.write()
}

// began records that a request has gone out, and ended that one has come back.
// They are the two edges loop.go takes either side of the wire.
func (b *taskBeat) began() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.row.Requests++
	b.row.RequestStarted = time.Now()
	b.write()
}

func (b *taskBeat) ended() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.row.RequestFinished = time.Now()
	b.write()
}

// phase moves the node into one of the three words and hands back the way out.
//
// IT IS A STACK OF ONE AND THE CALLER HOLDS THE OTHER END, which is what lets an
// audit that ends in an error or a repair round that is killed still leave the
// node saying what it is actually doing: `defer node.beatPhase(x)()` is the whole
// contract, and there is no unwinding for anybody to forget.
func (b *taskBeat) phase(name string) func() {
	if b == nil {
		return func() {}
	}
	b.mu.Lock()
	was := b.row.Phase
	b.row.Phase = name
	b.write()
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.row.Phase = was
		b.write()
	}
}

// stop takes the file away. The node has landed and the checkpoint's own row is
// the authority from here.
func (b *taskBeat) stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = os.Remove(b.path)
}

// write puts the row down, whole, temp-and-rename. Called with the lock held.
//
// The rename is what makes a reader see one refresh or the one before it and
// never half of either — [SaveMeta]'s bargain and taskpresence.go's, said again
// because it is the same requirement: a file two processes read while one writes.
func (b *taskBeat) write() {
	b.row.UpdatedAt = time.Now()
	encoded, err := json.Marshal(b.row)
	if err != nil {
		return
	}
	if directory := filepath.Dir(b.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return
		}
	}
	temporary := b.path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		return
	}
	if err := os.Rename(temporary, b.path); err != nil {
		_ = os.Remove(temporary)
	}
}

// readTaskBeat reads one node's pulse back. It is the whole read side, and it is
// here rather than in a reader's own package so that the file's shape has one
// definition — the same argument [runRowRecord] and [runRowNotice] are neighbours
// for.
//
// A missing file is the ordinary answer for a node that is not running, and it is
// a false rather than an error: a reader asking whether work is alive is not
// asking a question that can fail.
func readTaskBeat(path string) (taskBeatRow, bool) {
	if strings.TrimSpace(path) == "" {
		return taskBeatRow{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return taskBeatRow{}, false
	}
	var row taskBeatRow
	if err := json.Unmarshal(content, &row); err != nil {
		return taskBeatRow{}, false
	}
	return row, true
}

// beatPath is where one node's pulse lives: beside its transcript, under its id.
//
// THE STORE OWNS THE NAME because the store owns the checkpoint that names it,
// and the two must never be able to disagree. It is arithmetic on the
// checkpoint's own path rather than on a [Place], so the legacy flat layout — a
// stem-derived tasks.json with no session folder — gets a directory of its own
// beside the file instead of nothing at all.
func (s *taskStore) beatPath(id uint64) string {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.path), placeNodeJournals, fmt.Sprintf("%d%s", id, taskBeatSuffix))
}
