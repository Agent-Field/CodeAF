package session

// The durable frontier: the graph as a checkpoint, and recovery as a pure
// function of it.
//
// Until this file the task graph lived in one place — memory. Kill the app
// while a node is grinding on a build and everything about that work vanished
// with the process: which nodes were admitted, what they were briefed with,
// which of them had already landed and what their reports said, and — worst —
// the fact that a branch called task/fix-the-reconciler-9c1a2f is sitting in the
// repository with somebody's half-finished work on it and nothing left alive
// that knows why. The worktree survived; the knowledge of it did not.
//
// The research names the contract exactly (harness-research-notes.md §7, Resume
// semantics, arXiv:2608.03836): SCHEMA-VALID CHECKPOINTS, CONSUME-ONCE
// INTERRUPTS, RECOVERY AS A PURE FUNCTION OF DURABLE STATE. They machine-checked
// real frameworks against it and found violations, which is a good reason to
// write the three rules down rather than to assume them:
//
//   - THE CHECKPOINT IS WRITTEN AFTER EVERY TRANSITION, not at exit. A process
//     that is killed does not get to run its shutdown path, so a checkpoint that
//     is only correct at exit is a checkpoint that is only correct when nothing
//     went wrong. Admitted, running, the working copy prepared, the verdict in,
//     merged or conflicted or failed: each of those is a `g.checkpoint()` on the
//     way out of the transition, atomically (tmp + rename), so the file on disk
//     is always a whole graph somebody wrote and never half of two.
//
//   - LOADING IS SCHEMA-VALIDATED, AND A VIOLATION DROPS THE FILE WHOLE. This is
//     state.go's law and it is here for state.go's reason: refusing to start a
//     session because a bookkeeping file has a bad byte costs the person their
//     whole conversation, while starting with no graph costs them a summary they
//     can rebuild by working. One log line naming the path, and never fatal.
//
//   - AN INTERRUPT IS CONSUMED BY EXACTLY ONE RECOVERY. A node that was RUNNING
//     when the process died is interrupted work: nobody will ever finish it,
//     nobody will audit it, and its branch is on disk. Recovery turns it into a
//     failed node whose report says so and names the branch — and it RECORDS in
//     the checkpoint that it did (Interrupted), so the next resume reads plain
//     history rather than interrupting the same node a second time. The state
//     rewrite alone already makes it unrepeatable; the flag is the checkpoint
//     admitting to the consumption, which is the half of the contract a reader
//     can check.
//
// ── RECOVERY IS LOAD, RECONCILE, CONTINUE ──
//
// [Agent.recoverTasks] is three lines of work and no cleverness. LOAD the
// checkpoint. RECONCILE it with the disk — a node that was running is failed
// with its branch named, done and failed nodes come back as history with their
// leavings intact, queued nodes come back queued. CONTINUE by turning the same
// frontier every other transition turns: a queued node whose prerequisites are
// (still) done starts now, a queued node whose prerequisite was interrupted
// fails through the cascade that already exists. There is no resume path
// separate from the ordinary scheduler, because a second scheduler is a second
// set of rules to disagree with the first.
//
// ── A COMPLETION IS ANNOUNCED EXACTLY ONCE, ACROSS LIVES ──
//
// A done node must not re-notify on resume: its note is already in the
// transcript the journal replays, and hearing "task 1 finished" again would tell
// the model that work it has already read about has just happened. So the
// checkpoint records whether a node's completion note was ever handed to the
// steering lane (Noted), and recovery delivers a note ONLY for the nodes that
// never got one — the interrupted node, and the rare node that landed in the
// instant between its completion and the process dying. Those arrive inside the
// single recovery note, in the shape [taskNote] would have produced, so the
// model reads one grammar for one kind of news.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const (
	// taskFileVersion is the schema of <journal>.tasks.json. A file carrying any
	// other version is IGNORED rather than migrated, for state.go's reason: the
	// graph is a record of work that has already happened, and guessing at an
	// older shape risks rehydrating a report into a field that no longer means
	// what it meant — which here would mean briefing a dependent with it.
	taskFileVersion = 1

	// taskDocumentType is the type tag. A file that is JSON, parses, and is not
	// this is not this store's file.
	taskDocumentType = "tasks"
)

// taskCheckpointPath derives the checkpoint from the journal: the same name with
// a different extension, beside it in the same directory.
//
// PER-JOURNAL, exactly as statePath is (state.go), and for the same reason the
// FLAT layout's shape demanded: ~/.aforge/v3/sessions/<workspace>/ held every
// session this workspace ever had, so a single tasks.json there would have been
// every window writing over each other's graphs. A graph belongs to ONE
// conversation, and the journal is what names one conversation — which is also
// what makes "same journal" the definition of a resumed session.
//
// A SESSION FOLDER ANSWERS ITS OWN NAME, exactly as statePath's does: a journal
// called transcript.jsonl is a folder's (place.go), the folder holds one
// conversation, and the checkpoint is the folder's tasks.json.
// [Config.checkpointFile] is the door for a caller holding a [Place]; this is
// what answers a caller holding only the path.
func taskCheckpointPath(sessionFile string) string {
	sessionFile = strings.TrimSpace(sessionFile)
	if sessionFile == "" {
		return ""
	}
	if filepath.Base(sessionFile) == placeTranscript {
		return filepath.Join(filepath.Dir(sessionFile), placeTasks)
	}
	return strings.TrimSuffix(sessionFile, filepath.Ext(sessionFile)) + ".tasks.json"
}

// checkpointFile is where THIS session keeps its graph: the folder's tasks.json
// when the session has a [Place], and the stem-derived sidecar for the legacy
// flat layout the zero Place stands for.
func (c Config) checkpointFile() string {
	if path := c.Place.Tasks(); path != "" {
		return path
	}
	return taskCheckpointPath(c.SessionFile)
}

// taskRecord is one node as it survives the process.
//
// It carries the node's whole life in three parts: the FROZEN SPEC it was
// admitted with (title, summary, request, brief, deliverable, acceptance,
// depends_on, thresholds),
// where it had got to (state, elapsed, and the instants its run began and
// landed), and its LEAVINGS (report, changed, branch, worktree, merge). The
// assembled brief is deliberately absent: it is
// JIT by contract (task_run.go), so a queued node that resumes assembles it from
// the reports its prerequisites left, which is the same thing it would have done
// had nothing died.
type taskRecord struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	// Request is the person's own message, kept because a node that resumes
	// without it would be re-opened on the model's paraphrase alone — the one
	// part of what it was told that nothing downstream could reconstruct
	// (task_brief.go). Absent in every checkpoint written before requests were
	// carried, which resumes exactly as it always did: fewer sections, nothing
	// invented.
	Request string `json:"request,omitempty"`
	// OriginJournal and OriginLine are the pointer to the person's original
	// words in the conversation journal (task_brief.go). Absent in every
	// checkpoint written before origins were carried, which resumes as it
	// always did: the brief has no origin section.
	OriginJournal string `json:"origin_journal,omitempty"`
	OriginLine    int    `json:"origin_line,omitempty"`
	// Admission is the working context the node was admitted with: bounded
	// quotations of what was said around the work and handles to the calls that
	// already ran (admission.go). A resumed node that lost it would be re-opened
	// on the contract alone, which is the same loss Request was carried to end.
	//
	// IT IS A POINTER SO THAT NOTHING IS WRITTEN FOR A NODE THAT HAS NONE, and
	// its own Version field is what an older or newer record is read through
	// ([AdmissionContext.restored]). Absent in every checkpoint written before
	// this existed, which resumes exactly as it always did: fewer sections,
	// nothing invented.
	Admission *AdmissionContext `json:"admission,omitempty"`
	Brief     string            `json:"brief"`
	// Deliverable is what must exist when the node is over. It is omitempty for
	// Request's reason and for one more: a task the PERSON wrote themselves names
	// no deliverable separately, and a heading over nothing is not written
	// (task_person.go).
	Deliverable string   `json:"deliverable,omitempty"`
	Where       string   `json:"where,omitempty"`
	Acceptance  string   `json:"acceptance"`
	DependsOn   []uint64 `json:"depends_on,omitempty"`

	// Ground is the repository or folder the work IS ABOUT and Mode is how the
	// node stands on it ([TaskMode]). Where says which directory the worker typed
	// in; these say which project that directory was a copy of, and a resumed node
	// needs them to find its own branch again — the repository it merges into is
	// the ground, and reading it off the conversation's workspace was the bug this
	// pair exists to end (taskstands.go).
	//
	// THEY ARE ADDITIVE AND ABSENCE IS ORDINARY. A checkpoint written before they
	// existed decodes with neither, and the node it rebuilds falls back to exactly
	// the road it took when it was written.
	Ground string   `json:"ground,omitempty"`
	Mode   TaskMode `json:"groundMode,omitempty"`
	// Home is the root checkout's branch when the task branch was cut. It is
	// additive: an older record without it still gets the detached and protected
	// checks at landing, and simply cannot detect that the checkout moved.
	Home string `json:"home,omitempty"`
	// HomeSha is the commit Home named when the branch was cut. It is additive
	// beside Home so older records take the same name-only landing road as before.
	HomeSha string `json:"homeSha,omitempty"`

	// Rung, Seal, Base and Universe are WHICH COPY OF THE GROUND the work
	// actually happened in (session/groundladder.go): which rung of the ground
	// ladder made the world, the one string that names it, the machine commit the
	// parent's uncommitted world was sealed into, and furrow's name for the fork.
	//
	// THEY ARE ON THE RECORD BECAUSE A LANDING OUTLIVES THE RUN THAT MADE THE
	// WORLD. A node whose work is accepted an hour later, or whose session is
	// resumed after a crash, is landed from a tree rebuilt out of these fields —
	// and a landing that had forgotten them would take the parent's own
	// unfinished edits back into their history, and look for a branch that lives
	// in a fork in the repository next door. They are additive: a checkpoint
	// written before they existed decodes with none of them and lands exactly as
	// it always did, which is the branch-in-a-worktree road.
	Rung     GroundRung `json:"groundRung,omitempty"`
	Seal     string     `json:"groundSeal,omitempty"`
	Base     string     `json:"groundBase,omitempty"`
	Universe string     `json:"groundUniverse,omitempty"`

	// Frozen is the world every part of THIS node starts from, written when it
	// divided ([TaskNode.Frozen]). It is here for the reason the four above are —
	// a landing outlives the run that made the world — and for one more: a part
	// resumed after a restart prepares its working copy on this road and nowhere
	// else, so a checkpoint that had forgotten the freeze would hand it the
	// parent's tree as it stands now and put that one part in a world none of its
	// siblings ever saw. Additive: a checkpoint written before it existed decodes
	// without it and seals exactly as it always did.
	Frozen string `json:"groundFrozen,omitempty"`
	// Family is the checks this node owns for the family it handed out
	// ([TaskNode.Family]). It is here because the parent's own check is made
	// after every part is home, which can be a different process from the one
	// that divided — a resumed node that had forgotten it would be a check
	// nobody ever makes.
	Family []string `json:"familyChecks,omitempty"`

	// Parent and Depth are the node's FAMILY: which node handed this work out
	// (0 at a root) and how many tasks deep it sits (1 for a conversation's own
	// work). They are absent in every checkpoint written before a task could
	// hand work out, which resumes as the flat graph it was.
	//
	// WHAT DOES NOT SURVIVE IS THE OWNER. The agent that ran the parent died
	// with the process, so a resumed sub-task is run and reported by the
	// conversation — the only agent left to do either — and its report reaches
	// the person rather than a model that no longer exists (task_run.go's
	// [TaskGraph.runner], [Agent.deliverTaskNote]).
	Parent uint64 `json:"parent,omitempty"`
	Depth  int    `json:"depth,omitempty"`

	State  TaskState `json:"state"`
	Report string    `json:"report,omitempty"`
	// Ending is why a failed node stopped where it did (task_contract.go's
	// [TaskEnding]), and absent on every node that finished and on every
	// checkpoint written before the field existed — which a surface draws as it
	// always did, "stopped — branch kept".
	Ending TaskEnding `json:"ending,omitempty"`

	// Claim is the WORK'S OWN account of itself, kept beside the composed report
	// so that a verdict landing after a resume can rebuild the card without
	// guessing which half of the report the last auditor wrote (task_run.go's
	// [TaskNode.claim]). Absent in every checkpoint written before this field
	// existed, which resumes exactly as it always did: the report is carried
	// whole.
	Claim string `json:"claim,omitempty"`

	// Result is what the work produced, kept whole up to one cap with a pointer
	// to the rest of it (task_result.go). Claim above is that same answer cut to
	// the card's three lines; this is what a dependent's brief, a landing note
	// and a continuation's finding are built from, so a session resumed from this
	// file hands the work's answer on rather than the preview it kept.
	//
	// It is a pointer so that absence is ordinary: every checkpoint written
	// before this field existed decodes with nil here, and every reader falls
	// back to the report exactly as it did then.
	Result *taskResultRecord `json:"result,omitempty"`

	Changed  []string `json:"changed,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	Merge    string   `json:"merge,omitempty"`

	// Wrote is what a node's own hands have written SO FAR, kept while it runs
	// rather than only when it lands ([TaskNode.noteWrote]).
	//
	// IT IS ON THE RECORD BECAUSE A LANDING STAGES BY NAME. Only the paths a node
	// wrote come home (task_run.go's [stageTaskWork]), and a process that died
	// took the run's own tally of them with it while leaving the files on disk —
	// so without this the run that resumes stages only what IT wrote and quietly
	// abandons everything the first attempt made. Absent in every checkpoint
	// written before this field existed, and a node that never ran has none.
	Wrote []string `json:"wrote,omitempty"`

	// Journal is where the node's own transcript was written — the file a
	// person's "open that task" replays (task_room.go's [Agent.TaskJournal]).
	// It is on the record because the name is MINTED WITH A TIMESTAMP
	// ([taskJournalPath]) and cannot be recomputed: a resumed session that did
	// not carry it opened every finished task on an empty page with the whole
	// transcript sitting on disk beside it. Absent in every checkpoint written
	// before the field existed, and then [Agent.TaskJournal] finds the file by
	// its id in the session's own tasks/ directory ([findTaskJournal]).
	Journal string `json:"journal,omitempty"`

	// Beat is the heartbeat sidecar a RUNNING node is writing, and "" for every
	// node that is not running (task_beat.go). It is on the record so that a
	// reader holding this file never has to guess at a path — "is this row still
	// moving" is answered by opening the file this field names — and it is
	// deliberately not read back on a resume: the process that was writing it is
	// gone, and the next run mints the path again from the node's own id.
	Beat string `json:"beat,omitempty"`

	// Model is the model this node was admitted to run on, and empty when it
	// simply took the conversation's — including on every checkpoint written
	// before a task could carry one, which resumes exactly as it always did.
	Model string `json:"model,omitempty"`

	// Effort is the rung on the effort ladder this node's workers ask for, and
	// empty when nobody set one and the ladder decides from further down
	// (internal/effort) — including on every checkpoint written before a task
	// could carry one, which resumes exactly as it always did.
	Effort string `json:"effort,omitempty"`

	// MaxSteps and NoProgress are the node's own thresholds, 0 when it named
	// none and the defaults apply.
	MaxSteps   int `json:"max_steps,omitempty"`
	NoProgress int `json:"no_progress,omitempty"`

	// ElapsedMS is the node's age: how long it has been running, frozen at the
	// moment it landed. Milliseconds because a duration in JSON should be a
	// number a person can read, not a Go-shaped string.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
	// StartedAt and EndedAt are the instants the node began running and landed.
	// THEY ARE ADDITIVE AND ABSENCE IS ORDINARY. A checkpoint written before
	// these facts were carried decodes with neither, and a node whose process
	// never recorded one keeps the zero time rather than inventing a clock.
	StartedAt time.Time `json:"startedAt,omitzero"`
	EndedAt   time.Time `json:"endedAt,omitzero"`

	// CostUSD and the four token counts are the node's BILL: what every agent it
	// took — the worker, each repair round, the auditor, a design thread — spent
	// between them ([Agent.foldTaskUsage]). The dollars are the price somebody
	// published at the time; the tokens are what actually happened, which is why
	// both are kept and neither is derived from the other.
	//
	// Every one of them is absent from a checkpoint written before a node
	// carried a bill, which resumes as zero — the same thing an unpriced model
	// leaves behind, and the same thing every surface here already draws as
	// nothing rather than as "$0.00".
	CostUSD    float64 `json:"costUsd,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`

	// Noted says this node's completion note has been handed to the steering
	// lane. It is what stops a resumed session re-announcing work the transcript
	// already carries.
	//
	// It is HANDED OVER, not read: a note enqueued in the instant before the
	// process died never reached the transcript and is lost. That window is one
	// step boundary wide and closing it would mean the graph reaching into the
	// turn loop to ask whether a message had drained yet, which is a coupling
	// worth more than the case it buys.
	Noted bool `json:"noted,omitempty"`

	// Interrupted says this node was RUNNING when a session ended and that a
	// recovery has consumed that fact. It is the consume-once receipt.
	Interrupted bool `json:"interrupted,omitempty"`

	// Kind is what sort of node this was ([TaskKind]), and empty is the ordinary
	// one: work in a worktree. It is on the record for ONE reader — the recovery
	// that has to say what an interrupted node left behind — because the two
	// kinds leave behind different things, and a harness design told "it had not
	// got as far as a working copy" would be a sentence about machinery it was
	// never going to have (see [interrupt]).
	//
	// A DESIGN STILL WRITING IS NEVER RE-RUN. It was running when the session
	// ended and a recovery turns it into a failed node before the graph holds
	// it, so nothing here has to rebuild the goal it was designing from. The one
	// exception is a design whose page was FINISHED and waiting on a person —
	// that one carries its page in Offer and comes back to ask again.
	Kind TaskKind `json:"kind,omitempty"`

	// Offer is a finished harness page waiting on the person's answer, carried
	// whole so that closing aforge under the card does not throw away minutes of
	// finished model work ([harnessOfferRecord]). It is set for exactly as long
	// as the card is up — written when the page lands, cleared the moment the
	// wait ends in an answer or a rewrite — so on every other record it is
	// absent, and a checkpoint written before it existed reads exactly as it
	// always did: the design fails with nothing saved.
	Offer *harnessOfferRecord `json:"offer,omitempty"`
}

// harnessOfferRecord is one finished page as the checkpoint carries it: enough
// to raise the same card again in the next session and to save the same entry
// if the person says yes. The page travels as the bytes subharness.Encode
// writes; cues and justification ride beside it because a page has no field for
// either (harness_build.go's harnessDesign says why); goal, model and effort
// are what rebuild the node's design spec, which is deliberately not
// checkpointed anywhere else (task.go's taskSpec.design).
type harnessOfferRecord struct {
	Goal          string          `json:"goal"`
	Model         string          `json:"model,omitempty"`
	Effort        string          `json:"effort,omitempty"`
	Page          json.RawMessage `json:"page"`
	Cues          []string        `json:"cues,omitempty"`
	Justification string          `json:"justification,omitempty"`
}

// runRecord is ONE ROW OF AN ADAPTIVE RUN as it survives the process — the run's
// own row, or one worker under it.
//
// IT IS A RECORD AND NOT A NODE, and that is the whole shape of this feature. A
// run's execution does not survive: the orchestrator, its frontier and its
// context all died with the process, and nothing here rebuilds any of them
// (internal/manual/chat/adaptive-runs.md says so to the person). What died with
// them that had no business dying was the VISIBILITY — the rows were only ever
// published at the instant they moved, so a conversation reopened tomorrow drew
// an empty column beside a transcript full of runs. These entries are what the
// column redraws from: settled rows, kept as history, that cannot be scheduled,
// cannot be stopped and are not work.
//
// THE FIELDS ARE THE NOTICE'S FIELDS, because the row this restores to is a
// [TaskNotice] and there is exactly one shape of a run's row in this package
// (orchestrate.go's [orchestrateFamily.publish] keeps the live ones). Doing is
// deliberately NOT among them: it is the phase a row is in while it is moving
// ("forming the work"), and nothing restored from here is moving.
type runRecord struct {
	ID     uint64 `json:"id"`
	Run    string `json:"run,omitempty"`
	Node   string `json:"node,omitempty"`
	Parent uint64 `json:"parent,omitempty"`
	Title  string `json:"title,omitempty"`
	// Kind is what sort of row this is, and "" is a run's — which is what every
	// record written before background jobs had rows carries, and the honest
	// reading of a field an older file does not have. It has to survive because
	// it is the one fact that decides how the row is DRAWN: a job's row shows its
	// log where a run's shows how its branch came home, and it refuses the ✕ that
	// a run's row offers (session's TaskKindJob, internal/tui3's task.go).
	Kind TaskKind `json:"kind,omitempty"`

	State   TaskState `json:"state"`
	Stopped bool      `json:"stopped,omitempty"`
	Report  string    `json:"report,omitempty"`
	Model   string    `json:"model,omitempty"`
	CostUSD float64   `json:"costUsd,omitempty"`

	// ElapsedMS is whatever age the row was last published with, frozen. A run's
	// rows do not carry one today — the family publishes no Elapsed — so it is
	// absent on every record this code writes, and it is here rather than left
	// out because the record's job is to carry the notice, not to decide which
	// half of it matters. Zero renders as nothing, which is the emptiness law.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
}

// taskDocument is the file: a type tag, a version, the id counter, the nodes in
// admission order, and the adaptive runs' rows.
//
// Seq is on it because ids must not be reused across a resume: a second session
// that started counting from one would admit a node with the id of a node whose
// report is still in the transcript, and every sentence either of them appears
// in would be about the wrong work. It covers a run's rows too, because those
// take their ids from the same counter (orchestrate.go's family seam reuses
// [TaskGraph.reserve] precisely so that no run's row can collide with a task's).
//
// THE VERSION DOES NOT MOVE FOR Runs, AND THAT IS THE POINT. A file carrying any
// other version is IGNORED rather than migrated, so bumping it would throw away
// every graph written before this change — the exact loss this seam exists to
// stop. Runs is an added, omitted-when-empty field: an older checkpoint decodes
// with no runs in it and resumes precisely as it always did, and this code's own
// files are still version 1 documents that older builds can read.
type taskDocument struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Seq     uint64       `json:"seq"`
	Nodes   []taskRecord `json:"nodes"`
	Runs    []runRecord  `json:"runs,omitempty"`
}

// taskStore is the file and the lock that serializes writes to it.
//
// The lock is the STORE's rather than the graph's, and it is taken OUTSIDE the
// graph's for the whole snapshot-and-write: two nodes landing at once would
// otherwise be free to serialize their snapshots in one order and their writes
// in the other, leaving the older graph on disk as the last word. The ordering
// is store.mu → graph.mu, always, and nothing in this file ever takes them the
// other way round.
type taskStore struct {
	mu   sync.Mutex
	path string
	// closed says the session behind this store has left. A write that arrives
	// after that is not a late checkpoint, it is a goroutine that outlived the
	// close — a woken turn's hand-off, a job's last transition — and what it
	// would write is a graph nobody will resume from this process again: the
	// close already let every waiting writer finish, so anything after it can
	// only overwrite a settled file with a stale "running". Dropped, silently,
	// because the log line for it would blame a file that is perfectly fine.
	closed bool
}

func newTaskStore(path string) *taskStore {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return &taskStore{path: path}
}

// save snapshots the graph and writes it, atomically.
//
// A FAILED WRITE IS A LOG LINE, NOT AN ERROR RETURNED UPWARDS. Its caller is a
// state transition — a node starting, a node landing — and there is nothing
// useful a transition could do with the news that a bookkeeping file could not
// be written: the work is real either way, and refusing to run a task because a
// checkpoint could not be saved would trade the whole feature for the resume of
// it.
func (s *taskStore) save(graph *TaskGraph) {
	if s == nil || graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	document := graph.document()
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		log.Printf("session: could not encode the task checkpoint %s: %v", s.path, err)
		return
	}
	if directory := filepath.Dir(s.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
			return
		}
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
		return
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
	}
}

// close makes every later save a no-op. It is the session close's to call, and
// it sits under the same lock as save so a write already on its way to the
// rename finishes whole before the door shuts behind it.
func (s *taskStore) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// ── the graph, written down ─────────────────────────────────────────────────

// checkpoint persists the graph after a transition. It is a no-op for a graph
// with no file behind it — a session with no journal, and every scripted graph
// in the tests — which is why every transition may call it unconditionally.
//
// It must be called with the graph's lock RELEASED: the write takes the store's
// lock and then the graph's, and a caller holding the graph's would close the
// cycle.
func (g *TaskGraph) checkpoint() {
	if g == nil {
		return
	}
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	store.save(g)
}

// document is the graph as the file sees it, in admission order — the order that
// makes the frontier deterministic, and the order a person reads the file in.
func (g *TaskGraph) document() taskDocument {
	g.mu.Lock()
	defer g.mu.Unlock()
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: g.seq}
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil {
			continue
		}
		document.Nodes = append(document.Nodes, node.recordLocked())
	}
	// AND THE ADAPTIVE RUNS' ROWS GO DOWN IN THE SAME DOCUMENT, written by the
	// same store on the same beats. There is no second writer of tasks.json: a
	// family's transitions reach the disk by handing their rows to the graph
	// ([TaskGraph.keepRunRows]), and this is the only place that turns a row into
	// a record.
	for _, notice := range g.runRowsLocked() {
		document.Runs = append(document.Runs, runRowRecord(notice))
	}
	return document
}

// runRowRecord is one live row written down, and [runRowNotice] reads it back.
// They are a pair and they are next to each other so that a field added to one
// is missing from the other in the same eyeful.
func runRowRecord(notice TaskNotice) runRecord {
	return runRecord{
		ID:        notice.ID,
		Run:       notice.Run,
		Node:      notice.Node,
		Parent:    notice.Parent,
		Title:     notice.Title,
		Kind:      notice.Kind,
		State:     notice.State,
		Stopped:   notice.Stopped,
		Report:    notice.Report,
		Model:     notice.Model,
		CostUSD:   notice.CostUSD,
		ElapsedMS: notice.Elapsed.Milliseconds(),
	}
}

// runRowNotice is one record as the row a column draws again, SETTLED.
//
// Done stays done and failed stays failed, verbatim: those rows said their last
// word before the process ended and nothing has happened to them since. A row
// that was still QUEUED OR MOVING is the only one this changes, and it changes
// because the truth about it changed while nobody was watching — the work behind
// it stopped existing the moment the process did. It settles the way a run's own
// nodes settle when they are called off (orchestrate.go's
// [orchestrateFamily.retire]): failed, with Stopped beside it, because nothing
// went wrong with the work and nobody made a finding about it.
//
// A RESTORED ROW IS NEVER MOVING, which is why Doing is not restored and why
// Elapsed is whatever was frozen onto it. Nothing here re-enters the frontier:
// these rows are not in the graph's `nodes` and never were, so there is nothing
// for a scheduler to find (task_run.go's [TaskGraph.runs] says it at length).
func runRowNotice(record runRecord) TaskNotice {
	notice := TaskNotice{
		ID:      record.ID,
		Run:     record.Run,
		Node:    record.Node,
		Parent:  record.Parent,
		Title:   record.Title,
		Kind:    record.Kind,
		State:   record.State,
		Stopped: record.Stopped,
		Report:  record.Report,
		Model:   record.Model,
		CostUSD: record.CostUSD,
		Elapsed: time.Duration(record.ElapsedMS) * time.Millisecond,
	}
	if !notice.State.settled() {
		notice.State, notice.Stopped = TaskFailed, true
		// A JOB'S ROW KEEPS ITS OWN SENTENCE, because for a job that sentence is
		// not prose — it is where the log IS ([jobRowLead] mints
		// `job 3 · log /…/3.log`, and [jobNoticeFromRow] reads the path back out
		// of it on the way to a surface).
		//
		// OVERWRITING IT LOST THE ONE THING THE WORK LEFT BEHIND, and it lost it
		// in the commonest case there is: a job still running when aforge closed
		// is exactly the job somebody reopens the conversation to look at, and it
		// came back with no path at all. The sentence that replaced it was written
		// when this row was DRAWN — it read well under a row on the task column —
		// and nothing draws it now.
		//
		// WHAT IT SAID IS STILL SAID, by the state rather than by prose: the job
		// comes back stopped, which is what the column and the page both show, and
		// "it ended when aforge closed" is what stopped MEANS for a process that
		// cannot outlive the program that forked it.
		if record.Kind != TaskKindJob {
			notice.Report = orchestrateEndedReport
		}
	}
	return notice
}

// A JOB'S OWN VERSION OF THIS SENTENCE IS GONE, AND SO IS THE CHOICE BETWEEN
// THEM. `it ended when aforge closed; its log is kept` was written for a job's
// row on the task column, where it read beside the work it was about. A job has
// no row there any more, and the field the sentence was written into is the one
// carrying the log's path — so the sentence had stopped being read and had
// started deleting the path instead ([runRowNotice] says the rest).
//
// orchestrateEndedReport is what a row of an adaptive run says for itself when
// it was still moving as aforge closed.
//
// IT IS THE SENTENCE AND NOT A STATE WORD, because the state word is already
// "stopped" and it would be answering the wrong question: a person looking at
// this row wants to know why it stopped, and the answer is that the program it
// was running inside went away. The second clause is the useful half — what the
// run got through is on disk, in the same journal the run's page reads
// (orchestrate.go's orchestrateJournalPath) — and it is the same promise the
// sibling sentence for a subharness makes (subharness_run.go's
// subharnessInterruptedReport).
const orchestrateEndedReport = "it ended when aforge closed; its journal is kept"

// recordLocked copies one node out, with the graph held.
func (n *TaskNode) recordLocked() taskRecord {
	elapsed := n.ageLocked()
	changed := make([]string, len(n.changed))
	copy(changed, n.changed)
	wrote := make([]string, len(n.wrote))
	copy(wrote, n.wrote)
	dependsOn := make([]uint64, len(n.dependsOn))
	copy(dependsOn, n.dependsOn)
	// THE PULSE IS NAMED ONLY WHILE THERE IS ONE. A landed node's liveness is its
	// final state, and a path to a file the runner has already removed would be a
	// row inviting a reader to draw a conclusion from a missing file.
	beat := ""
	if n.state == TaskRunning && n.graph != nil {
		beat = n.graph.store.beatPath(n.id)
	}
	return taskRecord{
		ID:            n.id,
		Title:         n.spec.title,
		Summary:       n.spec.summary,
		Request:       n.spec.request,
		OriginJournal: n.spec.origin.journal,
		OriginLine:    n.spec.origin.line,
		Admission:     recordedAdmission(n.spec.admission),
		Brief:         n.spec.brief,
		Deliverable:   n.spec.deliverable,
		Where:         n.spec.where,
		Ground:        n.Ground,
		Mode:          n.Mode,
		Home:          n.Home,
		HomeSha:       n.HomeSha,
		Rung:          n.Rung,
		Seal:          n.Seal,
		Base:          n.Base,
		Universe:      n.Universe,
		Frozen:        n.Frozen,
		Family:        n.Family,
		Acceptance:    n.spec.acceptance,
		DependsOn:     dependsOn,
		Parent:        n.parent,
		Depth:         n.depth,
		State:         n.state,
		Report:        n.report,
		Ending:        n.endingLocked(),
		Claim:         n.claim,
		Result:        resultRecordOf(n.produced),
		Changed:       changed,
		Wrote:         wrote,
		Branch:        n.branch,
		Worktree:      n.worktree,
		Merge:         n.merge,
		Journal:       n.journal,
		Beat:          beat,
		Model:         n.spec.model,
		Effort:        n.spec.effort.String(),
		MaxSteps:      n.spec.maxSteps,
		NoProgress:    n.spec.noProgress,
		ElapsedMS:     elapsed.Milliseconds(),
		StartedAt:     n.started,
		EndedAt:       n.ended,
		CostUSD:       n.cost,
		Input:         n.input,
		Output:        n.output,
		CacheRead:     n.cacheRead,
		CacheWrite:    n.cacheWrite,
		Noted:         n.noted,
		Interrupted:   n.interrupted,
		Kind:          n.kind,
		Offer:         n.offer,
	}
}

// ── the file, read back ─────────────────────────────────────────────────────

// loadTaskCheckpoint reads one checkpoint and reports whether there is a graph
// in it. Everything that is not a whole valid document is a false and at most
// one log line: a missing file is the ordinary case (a fresh session) and says
// nothing at all.
func loadTaskCheckpoint(path string) (taskDocument, bool) {
	if strings.TrimSpace(path) == "" {
		return taskDocument{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("session: ignoring unreadable task checkpoint %s: %v", path, err)
		}
		return taskDocument{}, false
	}
	document, err := decodeTasks(content)
	if err != nil {
		log.Printf("session: ignoring corrupt task checkpoint %s: %v", path, err)
		return taskDocument{}, false
	}
	return document, true
}

// decodeTasks parses and VALIDATES one checkpoint. Every rule below is a rule
// this store enforces on the way out, so a file that breaks one was not written
// by this code — and a half-loaded graph is a graph nobody scheduled, which is
// the one thing worse than no graph at all.
//
// The edge rules are where the validation earns its place. A dependency naming a
// node the file does not contain is a node that can never be assembled a brief;
// a dependency on a LATER id is a cycle the frontier would wait on forever,
// because ids are minted in admission order and an edge can only ever point
// backwards.
func decodeTasks(content []byte) (taskDocument, error) {
	var document taskDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return taskDocument{}, err
	}
	if document.Type != taskDocumentType {
		return taskDocument{}, fmt.Errorf("type is %q, want %q", document.Type, taskDocumentType)
	}
	if document.Version != taskFileVersion {
		return taskDocument{}, fmt.Errorf("version %d, want %d", document.Version, taskFileVersion)
	}
	seen := make(map[uint64]bool, len(document.Nodes))
	var highest uint64
	for _, record := range document.Nodes {
		switch {
		case record.ID == 0:
			return taskDocument{}, fmt.Errorf("a node has no id")
		case seen[record.ID]:
			return taskDocument{}, fmt.Errorf("node %d appears twice", record.ID)
		case strings.TrimSpace(record.Title) == "":
			return taskDocument{}, fmt.Errorf("node %d has no title", record.ID)
		case strings.TrimSpace(record.Brief) == "":
			return taskDocument{}, fmt.Errorf("node %d has no brief", record.ID)
		case strings.TrimSpace(record.Acceptance) == "":
			return taskDocument{}, fmt.Errorf("node %d has no acceptance", record.ID)
		case !validTaskState(record.State):
			return taskDocument{}, fmt.Errorf("node %d is in state %q", record.ID, record.State)
		case !validMergeOutcome(record.Merge):
			return taskDocument{}, fmt.Errorf("node %d has merge outcome %q", record.ID, record.Merge)
		case record.MaxSteps < 0 || record.NoProgress < 0:
			return taskDocument{}, fmt.Errorf("node %d has a negative threshold", record.ID)
		case record.ElapsedMS < 0:
			return taskDocument{}, fmt.Errorf("node %d has a negative elapsed", record.ID)
		}
		for _, dependency := range record.DependsOn {
			if !seen[dependency] {
				return taskDocument{}, fmt.Errorf("node %d waits on %d, which is not in this graph", record.ID, dependency)
			}
		}
		// A PARENT IS AN EARLIER NODE, by the same argument the edges are
		// validated by: ids are minted in admission order, so a node cannot have
		// been handed out by work that did not exist yet, and a family pointing
		// forwards is a tree a roster would draw as a cycle.
		if record.Parent != 0 && !seen[record.Parent] {
			return taskDocument{}, fmt.Errorf("node %d was handed out by %d, which is not in this graph", record.ID, record.Parent)
		}
		seen[record.ID] = true
		if record.ID > highest {
			highest = record.ID
		}
	}
	// THE RUNS' ROWS ARE VALIDATED ON THEIR OWN TERMS AND THEY ARE NOT NODES. No
	// edge rules apply — a run's rows have no dependencies, and their tree is the
	// Parent field the family filled — but the two rules that make a row a row do:
	// it has an id, and it is in a state this package knows. The id space is
	// SHARED with the nodes, so a row wearing a node's id is a file where one
	// number names two pieces of work, which is the one corruption a roster
	// could not draw its way out of.
	drawn := make(map[uint64]bool, len(document.Runs))
	for _, record := range document.Runs {
		switch {
		case record.ID == 0:
			return taskDocument{}, fmt.Errorf("a run's row has no id")
		case seen[record.ID]:
			return taskDocument{}, fmt.Errorf("run row %d is also a node", record.ID)
		case drawn[record.ID]:
			return taskDocument{}, fmt.Errorf("run row %d appears twice", record.ID)
		case !validTaskState(record.State):
			return taskDocument{}, fmt.Errorf("run row %d is in state %q", record.ID, record.State)
		case record.ElapsedMS < 0:
			return taskDocument{}, fmt.Errorf("run row %d has a negative elapsed", record.ID)
		}
		drawn[record.ID] = true
		if record.ID > highest {
			highest = record.ID
		}
	}
	if document.Seq < highest {
		return taskDocument{}, fmt.Errorf("the id counter is %d behind node %d", document.Seq, highest)
	}
	return document, nil
}

func validTaskState(state TaskState) bool {
	switch state {
	case TaskQueued, TaskRunning, TaskDone, TaskFailed, TaskUnverified:
		return true
	}
	return false
}

func validMergeOutcome(merge string) bool {
	switch merge {
	case "", mergeMerged, mergeConflicted, mergeInPlace, mergeAborted, mergeKept:
		return true
	}
	return false
}

// ── recovery ────────────────────────────────────────────────────────────────

// taskRecovery is what one recovery found, in the four categories a person and a
// model both need kept apart, plus the notes nobody ever got.
type taskRecovery struct {
	done        int
	failed      int
	unverified  int
	interrupted int
	waiting     int
	// designs is counted apart from interrupted, and has to be: an interrupted
	// task RESUMES and often has a branch to go and look at, and a design does
	// neither — it is over, and nothing was saved ([interrupt]). Filing it under
	// "interrupted" would promise a person the next session will pick it up.
	designs int
	// asking is the third kind of design ending: the page was FINISHED and the
	// card was up when the session closed, so it comes back and asks again
	// ([interrupt]'s Offer branch). Counted apart from designs because the two
	// sentences are opposites — one kept everything, the other kept nothing.
	asking int
	// runs is a SUBHARNESS that was running when the process ended. It is
	// counted apart from interrupted for the reason designs are, and its answer
	// is the design's: a run does not resume, because the input it was given is
	// not in the checkpoint and nothing may guess at it ([interrupt]). It is not
	// counted with designs either — "a design did not finish" and "a run did not
	// finish" are two different pieces of news, and one sentence for both would
	// send somebody looking at the wrong thing.
	runs int
	// branches are the interrupted nodes' branches that are still on disk. They
	// are the whole reason the summary is worth reading: a kept branch is work
	// the person still has.
	branches []string
	// notes are the completion notes that were never handed over, in the shape
	// [taskNote] would have produced for them.
	notes []string
}

// any reports whether the recovery restored anything at all. A checkpoint that
// held an empty graph — a session that proposed nothing — is not news.
func (r taskRecovery) any() bool {
	return r.done+r.failed+r.unverified+r.interrupted+r.designs+r.asking+r.runs+r.waiting > 0
}

// note is the ONE line the person and the model read about a resumed graph,
// with whatever was never announced attached under it.
//
//	recovered task graph: 2 done · 1 interrupted (branch task/fix-it-9c1a2f kept) · 1 waiting
//
//	task 3 queued: Fix the reconciler
//	paused — it resumes; branch task/fix-it-9c1a2f kept
//
// A design that was still being written gets a clause of its own, because it is
// the one interrupted node that does NOT resume ([interrupt]):
//
//	recovered task graph: 2 done · 1 design did not finish (nothing saved) · 1 waiting
func (r taskRecovery) note() string {
	if !r.any() {
		return ""
	}
	parts := make([]string, 0, 5)
	if r.done > 0 {
		parts = append(parts, strconv.Itoa(r.done)+" done")
	}
	if r.failed > 0 {
		parts = append(parts, strconv.Itoa(r.failed)+" failed")
	}
	if r.unverified > 0 {
		parts = append(parts, strconv.Itoa(r.unverified)+" unverified")
	}
	if r.interrupted > 0 {
		parts = append(parts, strconv.Itoa(r.interrupted)+" interrupted ("+keptBranches(r.branches)+")")
	}
	if r.designs > 0 {
		word := " design did not finish (nothing saved)"
		if r.designs > 1 {
			word = " designs did not finish (nothing saved)"
		}
		parts = append(parts, strconv.Itoa(r.designs)+word)
	}
	if r.asking > 0 {
		word := " design asks again"
		if r.asking > 1 {
			word = " designs ask again"
		}
		parts = append(parts, strconv.Itoa(r.asking)+word)
	}
	if r.runs > 0 {
		word := " run did not finish"
		if r.runs > 1 {
			word = " runs did not finish"
		}
		parts = append(parts, strconv.Itoa(r.runs)+word)
	}
	if r.waiting > 0 {
		parts = append(parts, strconv.Itoa(r.waiting)+" waiting")
	}
	note := "recovered task graph: " + strings.Join(parts, " · ")
	if len(r.notes) > 0 {
		note += "\n\n" + strings.Join(r.notes, "\n\n")
	}
	return note
}

// keptBranches names what an interrupt left behind, or says plainly that it left
// nothing — "1 interrupted" with no clause would leave the person wondering
// whether there is a branch to go and look at.
func keptBranches(branches []string) string {
	switch len(branches) {
	case 0:
		return "no branch kept"
	case 1:
		return "branch " + branches[0] + " kept"
	default:
		return "branches " + strings.Join(branches, ", ") + " kept"
	}
}

// recoverTasks is the whole resume: load, reconcile, continue.
//
// It runs at Agent construction, which is the only moment at which "this journal
// has been opened again" is a fact rather than a guess, and it runs for a
// CONVERSATION only: a node's own agent has a journal of its own with no graph
// under it, and a node that rehydrated a graph would be a second scheduler
// running inside a worktree.
func (a *Agent) recoverTasks() {
	if a.config.InTask {
		return
	}
	document, found := loadTaskCheckpoint(a.config.checkpointFile())
	if !found || (len(document.Nodes) == 0 && len(document.Runs) == 0) {
		return
	}
	// THE RUN NAMES ARE CLAIMED BEFORE ANY RUN CAN BE STARTED. A run's name is a
	// counter on the agent and it starts again at one in every process, so
	// without this the first run of a resumed conversation would wear the name of
	// one already on the column — and would write its journal into that run's
	// folder (orchestrate.go's orchestrateJournalPath).
	a.reserveRunNames(document.Runs)

	graph := a.graph()
	recovery := graph.rehydrate(document, a.config.Workspace, a.settlePolicy())
	// The consume-once receipt reaches the disk BEFORE anything else happens: a
	// second crash between here and the first turn must not hand the same
	// interrupt to a second recovery.
	graph.checkpoint()

	if note := recovery.note(); note != "" {
		// THE AMBIENT LANE, not the waking one (agent.go): this runs at
		// construction, before anybody has said anything, and an account of what
		// the last process left behind is context for the first turn rather than
		// a reason to start one. A session that opened by talking to itself about
		// yesterday's interrupt would be answering a question nobody asked.
		a.enqueueAmbientNote(note)
	}
	// CONTINUE — the same frontier every other transition turns. A queued node
	// whose prerequisites are done starts now; one whose prerequisite was
	// interrupted fails through the cascade that already exists.
	graph.runFrontier()
}

// rehydrate rebuilds the graph from a checkpoint and reconciles it with the
// disk. It is the pure half of recovery: no provider, no scheduling, nothing
// that cannot be done with a file and a repository.
//
// THE ONLY RECONCILIATION IS THE INTERRUPT, because it is the only record whose
// truth changed while nobody was watching. A done node is done, a failed node
// failed, a queued node never started — but a RUNNING node names work that
// stopped existing the moment the process did, and what it left is on disk under
// a branch nobody is going to come back for unless somebody says its name.
func (g *TaskGraph) rehydrate(document taskDocument, workspace string, settle TaskSettle) taskRecovery {
	var recovery taskRecovery
	records := make([]taskRecord, 0, len(document.Nodes))
	for _, record := range document.Nodes {
		if record.State == TaskRunning {
			var kept string
			harness := record.Kind == TaskKindHarness
			running := record.Kind == TaskKindSubharness
			record, kept = interrupt(record, workspace)
			// A DESIGN IS COUNTED APART, and which way it went is read off what
			// the interrupt made of it: back on the frontier with its page (it
			// asks again), or over with nothing saved. Counting either beside
			// ordinary interrupted work would be the summary promising a resume
			// that is not that kind of resume.
			switch {
			case harness && record.State == TaskQueued:
				recovery.asking++
			case harness:
				recovery.designs++
			case running:
				recovery.runs++
			default:
				recovery.interrupted++
			}
			if kept != "" {
				recovery.branches = append(recovery.branches, kept)
			}
		} else {
			switch record.State {
			case TaskDone:
				recovery.done++
			case TaskFailed:
				recovery.failed++
			case TaskUnverified:
				// Counted apart from both: it is not work that failed and it is
				// not work still to come, it is work waiting on a person
				// (task_contract.go's TaskUnverified). A resumed session that
				// filed it under "waiting" would be telling somebody the
				// scheduler will get to it, and the scheduler never will.
				recovery.unverified++
			default:
				recovery.waiting++
			}
		}
		records = append(records, record)
	}

	g.mu.Lock()
	if g.nodes == nil {
		g.nodes = make(map[uint64]*TaskNode, len(records))
	}
	if document.Seq > g.seq {
		g.seq = document.Seq
	}
	// THE ADAPTIVE RUNS' ROWS COME BACK AS ROWS AND AS NOTHING ELSE. They are put
	// where the live ones live — [TaskGraph.runs], which the roster replay walks —
	// so the column has ONE door and ONE row-space whether a run is happening now
	// or happened yesterday. Nothing about them is scheduled, because they are not
	// in `nodes`: the loop below that turns records into [TaskNode]s never sees
	// them, and the frontier this recovery turns afterwards has nothing of theirs
	// to find.
	//
	// The grouping is read off the rows themselves: a row with no parent is a
	// run's own row and opens a family; a worker hangs under the id its Parent
	// names. That is the order [TaskGraph.document] wrote them in, and it is the
	// order they are drawn in.
	//
	// They are deliberately absent from [taskRecovery]: that note is the model's
	// account of the graph it can still act on, and a restored run's rows are
	// history a person reads on the column. Telling the model about work it
	// cannot touch would invite it to say something about it.
	for _, record := range document.Runs {
		root := record.Parent
		if root == 0 {
			root = record.ID
		}
		if g.runs == nil {
			g.runs = make(map[uint64][]TaskNotice, 1)
		}
		if _, held := g.runs[root]; !held {
			g.runRuns = append(g.runRuns, root)
		}
		g.runs[root] = append(g.runs[root], runRowNotice(record))
	}
	var unannounced []*TaskNode
	for _, record := range records {
		node := restoreNode(g, record)
		g.nodes[node.id] = node
		g.order = append(g.order, node.id)
		if node.state != TaskQueued && !node.noted {
			unannounced = append(unannounced, node)
		}
	}
	g.mu.Unlock()

	// The notes nobody ever got, in the shape a fresh run would have produced —
	// and marked as handed over, so this is the only life of this session in
	// which they are said.
	//
	// THEY ARE RE-TOLD AND NOT ARRIVING, so nothing here is put to the session's
	// goal owner: the landing already happened, in a life of this session that
	// has ended, and counting it now would count one failure twice
	// ([Agent.quietAddress]). A graph with no conversation behind it — every
	// test that builds one by hand — reads as a person's, which is the posture
	// every such caller already had.
	address := landingAddress{person: true}
	if g.home != nil {
		address = g.home.quietAddress()
	}
	for _, node := range unannounced {
		recovery.notes = append(recovery.notes, taskNote(node.notice(), taskURI(node.journalPath()), settle, address))
		node.markNoted()
	}
	return recovery
}

// restoreNode is one record as a node again. Its done channel is CLOSED for a
// terminal node — a waiter on finished work must not block on a run that is
// never going to happen — and open for a queued one, which is genuinely still
// waiting.
func restoreNode(graph *TaskGraph, record taskRecord) *TaskNode {
	node := &TaskNode{
		graph:     graph,
		id:        record.ID,
		dependsOn: record.DependsOn,
		parent:    record.Parent,
		depth:     record.Depth,
		done:      make(chan struct{}),
		spec: taskSpec{
			parent:      record.Parent,
			depth:       record.Depth,
			title:       record.Title,
			summary:     record.Summary,
			request:     record.Request,
			origin:      taskOrigin{journal: record.OriginJournal, line: record.OriginLine},
			admission:   restoredAdmission(record.Admission),
			brief:       record.Brief,
			deliverable: record.Deliverable,
			where:       record.Where,
			acceptance:  record.Acceptance,
			dependsOn:   record.DependsOn,
			model:       record.Model,
			effort:      restoredRung(record.Effort),
			maxSteps:    record.MaxSteps,
			noProgress:  record.NoProgress,
		},
		Ground:      record.Ground,
		Mode:        record.Mode,
		Home:        record.Home,
		HomeSha:     record.HomeSha,
		Rung:        record.Rung,
		Seal:        record.Seal,
		Base:        record.Base,
		Universe:    record.Universe,
		Frozen:      record.Frozen,
		Family:      record.Family,
		state:       record.State,
		report:      record.Report,
		ending:      record.Ending,
		kind:        record.Kind,
		claim:       record.Claim,
		produced:    resultFromRecord(record.Result),
		changed:     record.Changed,
		wrote:       record.Wrote,
		branch:      record.Branch,
		worktree:    record.Worktree,
		merge:       record.Merge,
		journal:     record.Journal,
		elapsed:     time.Duration(record.ElapsedMS) * time.Millisecond,
		started:     record.StartedAt,
		ended:       record.EndedAt,
		cost:        record.CostUSD,
		input:       record.Input,
		output:      record.Output,
		cacheRead:   record.CacheRead,
		cacheWrite:  record.CacheWrite,
		noted:       record.Noted,
		interrupted: record.Interrupted,
		offer:       record.Offer,
	}
	// AND WHETHER THIS WORK MAY STILL DISCOVER THAT IT IS WIDE. The road is not
	// on the record, because it is not a fact about the work — it is a reading
	// of the work, and it is re-taken here from the same text
	// ([Agent.armDivision], task_divide.go). What a restart does lose is the
	// sizing judge's own yes, which lived in the session that has gone; a task
	// armed only by that comes back as one worker, which is the safe direction
	// for a reading to fail in.
	//
	// THE KIND IS ASKED OF THE RECORD AND NOT OF THE REBUILT SPEC, because on
	// this path the record is the only thing that knows. [taskSpec.design] is
	// rebuilt from the Offer below, and a design whose page was never finished
	// comes back carrying none at all — so [Agent.armDivision]'s own kind guard
	// would read a design as ordinary work here and arm a page writer.
	if record.Kind == "" {
		node.spec.armed = graph.home.armDivision(node.spec)
	}
	// A QUEUED DESIGN IS ONLY EVER A FINISHED PAGE ASKING AGAIN ([interrupt]'s
	// Offer branch), and the Offer is the one record that can rebuild the design
	// spec the checkpoint otherwise never carries — without this line the
	// frontier would hand the node to an ordinary worker in a worktree, which is
	// the exact failure task.go's taskSpec.design warns about.
	if record.Kind == TaskKindHarness && record.State == TaskQueued && record.Offer != nil {
		designEffort, ok := provider.ParseEffort(record.Offer.Effort)
		if !ok {
			designEffort = provider.EffortNone
		}
		node.spec.design = &harnessDesignSpec{
			goal:   record.Offer.Goal,
			model:  record.Offer.Model,
			effort: designEffort,
			resume: record.Offer,
		}
	}
	if record.State != TaskQueued {
		close(node.done)
	}
	return node
}

// harnessInterruptedReport is what a design that was still being written when
// the process ended says for itself. It is the wording a design that ran out of
// its own clock already uses, because from the person's side the two are one
// fact: the page was not finished and nothing was kept.
const harnessInterruptedReport = "the design did not finish before aforge closed; nothing was saved"

// interrupt turns a node that was running into the failed node it became when
// the process died, and reports which branch — if any — is still on disk for the
// person to go and look at.
//
// It CHECKS rather than assumes. A branch named in a checkpoint may have been
// merged, deleted or pruned by the person in between, and a report promising
// work on a branch that is gone is worse than no report: it is the harness
// telling somebody their work is safe when it is not.
// interruptedAt is when a node that SETTLES on interruption ended, which is
// now: the process is closing and this is the last moment anything knew about
// this node.
//
// AN INTERRUPT IS AN ENDING FOR THE KINDS THAT SETTLE ON IT. A design still
// writing and a run that cannot be re-entered are both handed back as failed
// and never return to the frontier, so this is the only instant anything will
// ever have for them — and without it their rows rebuild undated while
// [Agent.TaskIndex] has the live graph row REPLACE the durable one, discarding
// the better stamp closeInflightTaskIndexRows had written. An ordinary task
// takes none of this: it goes back on the frontier queued, has not ended, and
// must not be stamped as though it had.
//
// A record that already carries one keeps it, because a node that landed and
// was then caught by the close ended when it landed.
func interruptedAt(record taskRecord) time.Time {
	if !record.EndedAt.IsZero() {
		return record.EndedAt
	}
	return time.Now()
}

func interrupt(record taskRecord, workspace string) (taskRecord, string) {
	record.Interrupted = true
	// The completion note is owed: nobody ever announced this node, because
	// nothing was alive to announce it.
	record.Noted = false

	if record.Kind == TaskKindHarness {
		// A DESIGN WHOSE PAGE WAS FINISHED COMES BACK AND ASKS AGAIN. The card is
		// a question, and closing the terminal is not an answer to it: the page
		// on the Offer is minutes of finished model work that nothing but the
		// person's word may throw away. It goes back on the frontier as the
		// design it is — [restoreNode] rebuilds the design spec from the Offer,
		// which is the one record that CAN rebuild it — and the next session
		// raises the same card over the same page.
		if record.Offer != nil && len(record.Offer.Page) > 0 {
			record.State = TaskQueued
			record.Report = ""
			return record, ""
		}

		// A DESIGN STILL WRITING IS NEVER RE-RUN, AND THIS IS THE LINE THAT
		// MAKES IT TRUE.
		//
		// A design has nothing on disk to point at, ever: no worktree, no branch,
		// no files, and nothing reaches the registry until somebody approves the
		// card (harness_task.go). Half-written, it also has nothing to re-enter —
		// what tells [Agent.runTaskNode] to hand a node to the designer instead
		// of a worker is [taskSpec.design], which is only rebuilt from a finished
		// Offer (task.go says why it is otherwise not checkpointed). So a
		// mid-write design put back on the frontier is a node the next session
		// would run as an ORDINARY WORKER, in a worktree, against the designer's
		// brief — which is not the work anybody asked for, and it would spend
		// real money doing it.
		//
		// It settles instead, with the same sentence a design that ran out of time
		// says, because the two are the same fact from the person's side: the page
		// was still being written and nothing was kept.
		record.State = TaskFailed
		record.Report = harnessInterruptedReport
		record.EndedAt = interruptedAt(record)
		return record, ""
	}

	if record.Kind == TaskKindSubharness {
		// A RUN IS NEVER RE-RUN, and this is the line that makes it true.
		//
		// The argument is the design's one above, arrived at from the other side.
		// What tells [Agent.runTaskNode] to hand a node to a program rather than
		// to a worker is [taskSpec.run], and that field is not in the checkpoint:
		// a run's input is the material of one conversation, and there is no
		// finished-page record here that could rebuild it. So a run put back on
		// the frontier is a node the next session would run as an ORDINARY WORKER
		// in a worktree against a brief nobody wrote, spending real money on work
		// nobody asked for.
		//
		// It settles instead, saying the one thing that is true of it: it did not
		// finish, and what it got through is in its journal.
		record.State = TaskFailed
		record.Report = subharnessInterruptedReport
		record.EndedAt = interruptedAt(record)
		return record, ""
	}

	// A process exit pauses ordinary work; it does not make a finding about it.
	// Put the node back on the frontier so the next session resumes it once.
	record.State = TaskQueued

	if record.Merge == mergeInPlace {
		// There was no repository to branch from, so its edits are already in the
		// person's tree — calling that "aborted" would say work was thrown away
		// that is sitting in front of them.
		record.Report = "paused — it resumes; whatever it wrote is in your tree"
		return record, ""
	}
	record.Merge = mergeAborted

	branch := strings.TrimSpace(record.Branch)
	switch {
	case branch == "":
		record.Report = "paused — it resumes"
		return record, ""
	case !branchOnDisk(workspace, branch):
		record.Report = "paused — its previous branch " + branch + " is gone, so it resumes in a fresh working copy"
		return record, ""
	}
	report := "paused — it resumes; branch " + branch + " kept"
	// IT IS A WORKING COPY AND NEVER A `worktree` IN THIS SENTENCE. The word is
	// the machinery's, which this house bans in anything a person reads, and it
	// is not even reliably true: a repository task is grounded in a fork whenever
	// furrow can make one (groundladder.go), and the directory named here is then
	// a copy of the whole folder rather than anything git has registered. The two
	// sentences above already call it a working copy.
	if worktree := strings.TrimSpace(record.Worktree); worktree != "" {
		if _, err := os.Stat(worktree); err == nil {
			report += ", its working copy is at " + worktree
		}
	}
	record.Report = report
	return record, branch
}

// branchOnDisk reports whether the node's branch is still in the repository.
func branchOnDisk(workspace, branch string) bool {
	root, ok := repositoryRoot(workspace)
	if !ok {
		return false
	}
	_, err := git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// restoredRung reads one rung back off a checkpoint. A WORD THIS BUILD DOES NOT
// KNOW IS ABSENCE, not a refusal: a file written by a build with a rung this one
// dropped must resume the work rather than fail to load it, and the ladder's
// next rung down is a correct answer where an unreadable checkpoint is not.
func restoredRung(word string) effort.Rung {
	rung, ok := effort.Parse(word)
	if !ok {
		return effort.None
	}
	return rung
}
