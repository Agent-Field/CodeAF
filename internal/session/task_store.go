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
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/provider"
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
// FLAT layout's shape demanded: ~/.codeaf/v3/sessions/<workspace>/ held every
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
	Restart *taskRestartRecord `json:"restart,omitempty"`
	ID      uint64             `json:"id"`
	Title   string             `json:"title"`
	Summary string             `json:"summary,omitempty"`
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
	Deliverable string `json:"deliverable,omitempty"`
	Where       string `json:"where,omitempty"`
	// Acceptance is DONE WHEN — what somebody who was not there checks the work
	// against. It is written for every node that has one and it is EMPTY on a
	// quick node, where that is the design and not a loss: nothing checks a quick
	// task, so an acceptance clause there would be a promise with no reader, and
	// the builder leaves it out on purpose (task_quick.go's [Agent.newQuickSpec]).
	// [acceptanceHolds] is that rule, asked of the KIND rather than of this
	// field alone; it stays a refusal for ordinary work.
	Acceptance string `json:"acceptance"`
	// PlanID is the task's id in the plan store (internal/session, planID on
	// [taskSpec]), carried so a resumed node still knows which plan task it is:
	// the pulse's writeback and the store-side dispatch both key on it. Absent
	// in every checkpoint written before the plandb loop existed, which resumes
	// as it always did: no plan task, no plan lines, nothing dispatched.
	PlanID    string   `json:"plan_id,omitempty"`
	DependsOn []uint64 `json:"depends_on,omitempty"`

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
	// CheckBase preserves the before-reading when the worker commits or resumes.
	CheckBase string `json:"checkBase,omitempty"`

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
	// Checks is the repeatable verification this node is checked by
	// ([TaskNode.Checks]). It is here for Family's reason — the check is made when
	// the work comes home, which can be a different process from the one that
	// admitted it — and it is ADDITIVE: a checkpoint written before this field
	// existed decodes without it, which means the node it rebuilds declares no
	// repeatable check and its checker judges by reading. That is the honest
	// answer for an old record rather than a hole, because the alternative would
	// be guessing a command out of its prose (task_checks.go).
	Checks []string `json:"checks,omitempty"`
	// FamilyDeclared is the part of Family this node's CHECKER may re-run, and
	// ChecksRevision is which revision of the assignment both lists were written
	// for ([TaskNode.FamilyDeclared], [TaskNode.checksRevision]). They are as
	// additive as Checks and they fail the same way: a record written before them
	// restores a node whose checker re-runs nothing of its family's, which is the
	// only honest reading of a list an older build filled out of prose.
	FamilyDeclared []string `json:"familyDeclaredChecks,omitempty"`
	ChecksRevision uint64   `json:"checksRevision,omitempty"`
	// FamilyWas is what this node owed its family before a revision moved the goal
	// ([TaskNode.FamilyWas]): kept so that what was required is still readable, and
	// required by nothing.
	FamilyWas []string `json:"familyChecksWas,omitempty"`

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
	// Late is every message about this node's own pieces that came home after
	// its worker had stopped reading, folded into Report above
	// (task_latefold.go), and Landed is the landing's own account beside them.
	// Landed is written only when Late is not empty: everywhere else Report IS
	// the landing's account, and a second copy of it would be the same words
	// under two names ([taskRecord.landedHalf]). They are here because a person
	// reads them, and because a restored node that composed its report from
	// anything but these halves would add a second fold on top of the first.
	Late   []string `json:"late,omitempty"`
	Landed string   `json:"landed,omitempty"`
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
	// CheckedOn is the model the checking pass ran on. It is additive: a record
	// written before it decodes with it empty, so a restarted node judged from the
	// checkpoint scores the worker seat and simply has no high seat to score.
	CheckedOn  string  `json:"checkedOn,omitempty"`
	NextModel  string  `json:"next_model,omitempty"`
	NextEffort *string `json:"next_effort,omitempty"`

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
	// IT IS THE RECIPIENT'S RECORD AND NOT ITS QUEUE. A note accepted onto a
	// queue is read at a step boundary that an unattended session may never
	// reach — the wake declines with nobody there, and the reaper closes the
	// session half an hour after the terminal detached — so a mark made at the
	// enqueue said "announced" about a landing no model ever saw, and this
	// checkpoint then stopped the next life re-telling it. It is written when the
	// note reaches the recipient's own record ([TaskNode.noteRecorded]).
	//
	// A crash between that record and this file leaves this saying "still owed"
	// about a landing that was told — which is why the replay asks the
	// recipient's journal as well ([sessionFile.recorded]), and why the failure
	// here is a landing said twice rather than one lost. A file written before
	// this meaning changed says "announced" about a note that was queued, which
	// is read here exactly as it was written.
	Noted bool `json:"noted,omitempty"`

	// NotedState is the ending that announcement was made for, so that work which
	// later ends somewhere else — a person deciding about a landing nobody could
	// check — is news again while the same landing is not announced twice
	// ([TaskNode.notedLocked]). It is absent from every checkpoint written before
	// it existed, and an absent one reads as "announced, whatever it said".
	NotedState TaskState `json:"noted_state,omitempty"`

	// Attempt is which life of this node's work the row describes, raised each
	// time the node is re-armed ([TaskGraph.reopen]). Absent from older
	// checkpoints, which restore as attempt 0 — the life they were written in —
	// and from every node that has only ever run once.
	Attempt int `json:"attempt,omitempty"`

	// Interrupted says this node was RUNNING when a session ended and that a
	// recovery has consumed that fact. It is the consume-once receipt.
	Interrupted bool `json:"interrupted,omitempty"`

	// Decider is WHO WAS HOLDING THIS NODE'S DECISION when the file was written
	// ([TaskAskOwner], task_run.go's [TaskNode.decider]): the person, or the model
	// under `task.settle = auto` or after somebody handed this one card over.
	//
	// IT IS WRITTEN SO THAT THE FLOOR HAS SOMETHING TO FIRE ON. A node the model
	// was holding is handed back to the person the moment this file is read
	// ([TaskGraph.handBackOnLoad]), because the turn it was going to be decided in
	// died with the process — so the value that survives is never the value that
	// is restored, and what it buys is a hand-back that HAPPENS and can be watched
	// rather than a zero value that happened to look like one.
	//
	// THE EMPTINESS LAW HOLDS. Absent — every checkpoint written before this field
	// existed, and every node nobody ever handed over — reads as the person, which
	// is where every unowned question belongs ([taskDeciderOf] says the same thing
	// on the reading side).
	//
	// AND THE PROJECT INDEX DELIBERATELY DOES NOT CARRY IT. That file is what work
	// CAME TO, appended once and never rewritten, and who is holding a question
	// lasts at most one turn — a row on disk saying `codeaf is deciding` about a
	// conversation that closed hours ago would be a claim nothing could ever
	// correct. It is [TaskIndexEntry.Activity]'s rule about a present that ends
	// seconds after it is recorded, said about a second momentary fact.
	Decider TaskAskOwner `json:"decider,omitempty"`

	// Clashing, Shifted and GroundHeld are WHAT A YOUR-CALL LANDING IS ASKING
	// ABOUT, and they are on the record for the same reason [taskRecord.Decider]
	// is: nothing can work them out again.
	//
	// The names were read out of git's index while the refused merge still stood
	// and the merge was then abandoned, so the index no longer holds them; which
	// of the three roads to a conflicted landing this was is a fact about a merge
	// that has already happened. A checkpoint without them came back with the
	// question intact and the sentence hollowed out — `conflicts with your branch`
	// with no files, on a road that was not a branch conflict at all — and a
	// surface reading the road back out of the report's prose is this program
	// reading its own writing (task_run.go's [TaskNode.shiftedBy] states the law).
	//
	// THE EMPTINESS LAW HOLDS on all three: absent is an absence and never a
	// claim that nothing clashed.
	Clashing   []string `json:"clashing,omitempty"`
	Shifted    bool     `json:"shifted,omitempty"`
	GroundHeld bool     `json:"groundHeld,omitempty"`

	// Resolving says A MERGE ROUND WAS IN FLIGHT when this file was written
	// (task_merge_round.go's [TaskNode.claimResolving]): the person pressed
	// `resolve it` on a conflict card, a worker was opened in the working copy,
	// and it had not landed yet.
	//
	// IT IS ON THE RECORD FOR THE SENTENCE A RESUME OWES. That worker dies with
	// the process, and the node it was working on is left exactly as it was —
	// `your call`, its files still clashing, its card still offering the same
	// three answers. Without this the resume said nothing at all about it, and a
	// person who had pressed a button and watched a round start came back to a
	// card that looked as though they never had (2026-09-09, task 2 of an
	// apartment search: the round's own journal ends mid-read at 03:09:02 and the
	// node still reads `conflicts with your branch`).
	//
	// IT IS NEVER RESTORED AS TRUE. The claim it records exists to stop TWO
	// workers editing one working copy, and the worker it was held for is gone —
	// so a resume that kept the flag would refuse the person's next press forever
	// ([TaskGraph.rehydrate] clears it as it counts it). Nothing restarts the
	// round: a round costs a model call, and a session that spent one on its own
	// initiative on the way up is a session spending the person's money for them.
	Resolving bool `json:"resolving,omitempty"`

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
	// whole so that closing codeaf under the card does not throw away minutes of
	// finished model work ([harnessOfferRecord]). It is set for exactly as long
	// as the card is up — written when the page lands, cleared the moment the
	// wait ends in an answer or a rewrite — so on every other record it is
	// absent, and a checkpoint written before it existed reads exactly as it
	// always did: the design fails with nothing saved.
	Offer *harnessOfferRecord `json:"offer,omitempty"`

	// Quick is a QUICK NODE'S BODY — its line, its list, the ticks against the
	// list and the files it claimed ([quickRecord]) — and absent on every other
	// kind of node.
	//
	// THE CHECKLIST IS THE GRAPH (docs/design/quick-task/DESIGN.md), and until this
	// field existed the graph's own file could label a quick node and not rebuild
	// it: the kind was written and the body was not, so a node read back came home
	// with nothing telling [Agent.runTaskNode] it was quick, nothing a restart
	// could say about how far down its list it got, and nothing for the next
	// checkpoint to write back. A record written before this field decodes with
	// nil here and is settled on the kind alone, exactly as it always was.
	Quick *quickRecord `json:"quick,omitempty"`
	// Unshaped and Unsized are THE TWO READINGS THIS NODE IS STILL OWED — the
	// shaper's brief and the sizing judge's width — both taken beside the node's
	// first worker rather than in front of it ([taskSpec.unshaped],
	// [taskSpec.unsized], task_shape.go and task_divide_sketch.go).
	//
	// THEY SURVIVE THE PROCESS BECAUSE THE WAIT CAN. A person's `/task` admitted
	// while the lanes are full is queued with no worker, and a restart in that
	// window without these fields brings the node back with nothing owed: the
	// brief stays the person's raw sentence and the acceptance the canned
	// stand-in forever, with no road left to write either. Absent in every
	// checkpoint written before they existed, and absent on every node that owes
	// nothing, which is every node but a person's own typed task.
	Unshaped bool `json:"unshaped,omitempty"`
	Unsized  bool `json:"unsized,omitempty"`

	// Expects is the checkable half of the handoff this node was given — what its
	// brief assumes of the world it gets ([TaskNode.Expects], handoffcontract.go).
	// It is on the record because both of its readers outlive a restart: the
	// brief a worker opens on carries it as a section ([TaskNode.instructionOn]),
	// and a node that had not yet started still owes the preflight against it.
	// Absent on every node nobody wrote one for, which is nearly all of them, and
	// on every checkpoint written before it was carried.
	Expects []Expectation `json:"expects,omitempty"`

	// Assignment is what this node is working towards NOW, when that is no longer
	// only what it was admitted with: the revisions the person's own directions
	// made, and the receipts for every line said to it (assignment.go).
	//
	// IT IS ABSENT ON A NODE NOBODY HAS SAID ANYTHING TO, which is nearly all of
	// them, and absent on every checkpoint written before it existed. Read back as
	// nothing it means exactly what it meant then — the admitted brief and
	// acceptance are the effective ones — so an old session opens unchanged, and
	// the fields it would have overlaid are still on the record beside it.
	Assignment *assignmentRecord `json:"assignment,omitempty"`
}

// quickRecord is a quick node's body on disk: the four fields of
// [quickTaskSpec] that are facts about the work. `waits` is not among them — it
// is a receipt for the moment of admission, and the edge it produced is already
// on [taskRecord.DependsOn].
//
// Done is parallel to Items, one tick per item, exactly as it is in memory.
type quickRecord struct {
	Line  string   `json:"line"`
	Items []string `json:"items,omitempty"`
	Done  []bool   `json:"done,omitempty"`
	Files []string `json:"files,omitempty"`
}

// quickRecordLocked copies a quick body out, with the graph held — the lock the
// `items` tool writes the list under ([TaskNode.quickListChange]). nil for every
// node that is not a quick one, which omitempty drops.
func quickRecordLocked(spec *quickTaskSpec) *quickRecord {
	if spec == nil {
		return nil
	}
	return &quickRecord{
		Line:  spec.line,
		Items: append([]string(nil), spec.items...),
		Done:  append([]bool(nil), spec.done...),
		Files: append([]string(nil), spec.files...),
	}
}

// body is the record as a quick node's body again, through the one constructor
// every body comes through ([newQuickTaskSpec]) and with the ticks laid back
// over it. A record carrying more ticks than items — which nothing writes — has
// the surplus dropped rather than trusted, because a tick is only ever a claim
// about an item that exists.
func (r *quickRecord) body() *quickTaskSpec {
	if r == nil {
		return nil
	}
	spec := newQuickTaskSpec(r.Line, append([]string(nil), r.Items...), append([]string(nil), r.Files...))
	for index := range spec.done {
		spec.done[index] = index < len(r.Done) && r.Done[index]
	}
	return spec
}

// assignmentRecord is the overlay and its receipts on disk. The admitted brief,
// deliverable and acceptance keep their own fields above: this is what has been
// said and decided SINCE, and a reader that wants what was first agreed must
// still be able to find it.
type assignmentRecord struct {
	Version     uint64            `json:"version,omitempty"`
	Deliverable string            `json:"deliverable,omitempty"`
	Acceptance  string            `json:"acceptance,omitempty"`
	Revisions   []revisionRecord  `json:"revisions,omitempty"`
	Directions  []directionRecord `json:"directions,omitempty"`
	Next        uint64            `json:"next,omitempty"`
}

// revisionRecord is one accepted move of the goal, with the words that
// authorised it.
type revisionRecord struct {
	Version     uint64    `json:"version"`
	Direction   uint64    `json:"direction,omitempty"`
	Said        string    `json:"said,omitempty"`
	Work        string    `json:"work,omitempty"`
	Deliverable string    `json:"deliverable,omitempty"`
	Acceptance  string    `json:"acceptance,omitempty"`
	At          time.Time `json:"at,omitzero"`
}

// directionRecord is one line said to the node, with who said it and what became
// of it. It is on the checkpoint because a direction nobody has read is what
// stops a landing publishing, and a process that died between the words and the
// landing must not come back having forgotten them.
type directionRecord struct {
	ID      uint64    `json:"id"`
	Words   string    `json:"words"`
	At      time.Time `json:"at,omitzero"`
	From    string    `json:"from,omitempty"`
	State   string    `json:"state,omitempty"`
	Version uint64    `json:"version,omitempty"`
	// Source is the person's message this was forwarded from (task_forward.go),
	// absent on a line said into the node's own room and on every checkpoint
	// written before forwarding existed.
	Source *sourceRecord `json:"source,omitempty"`
}

// sourceRecord is [personSourceID] on disk. THE SCOPE IS WRITTEN WITH THE
// NUMBER and is not optional: the number alone is one opening of one session's
// count of the person's messages, and a later opening counts again from a
// history that compaction may have folded. Restored without its scope it would
// let a session recognise tomorrow's first message as a direction it already
// holds — dropping a genuinely new correction as a repeat — which is the one
// failure this pair exists to make impossible.
type sourceRecord struct {
	Scope string `json:"scope"`
	Seq   uint64 `json:"seq"`
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
//
// NEITHER IS Paused, AND FOR A STRONGER REASON. It is a question put to a person
// by a run that is still going, and the run died with the process — so a restored
// row wearing it would ask for money on behalf of an orchestrator that no longer
// exists, and no answer could reach one. Left out, the row comes back settled
// like every other row that was moving, which is the truth about it.
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
	// StartedAt and EndedAt are when the row's work began and ended. A row that
	// came back without them drew a finished run with no age, and the places
	// that order work by activity had nothing to order it by.
	StartedAt time.Time `json:"startedAt,omitzero"`
	EndedAt   time.Time `json:"endedAt,omitzero"`

	// Copy is WHERE THE RUN'S WORK HAPPENED (task_run_copy.go). It is the one
	// fact about a run that nothing else can recover: the directory is derived
	// from the run's own number and could be worked out again, but the branch is
	// minted at random when the copy is cut and is written nowhere else. A row
	// saved before this field existed decodes with nil, and a run with no copy
	// recorded is one that cannot be carried on — which [runCopyTree] says out
	// loud rather than repairing.
	Copy *TaskCopyRecord `json:"copy,omitempty"`

	// ElapsedMS is whatever age the row was last published with, frozen. A
	// hand-off's run row carries its wall time from the row that settles it —
	// the span from the hand-off to the instant its program was gone, or its
	// engine answered ([runSpan]) — and an adaptive family's rows publish no
	// Elapsed, so theirs is absent. Zero renders as nothing, which is the
	// emptiness law.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`

	// Ending, Branch, Merge, Result and Changed are HOW THE ROW ENDED AND WHERE
	// ITS WORK IS, which a settled run row carries and a conversation reopened
	// tomorrow must still say. They were left out, and the drop was visible: a
	// program that judged its own work unfinished came back as `a fault: …` —
	// the failed-with-no-ending reading — instead of its own sentence, a run
	// ended by a limit its person set lost which limit it was, and a row whose
	// work was kept on a branch came back naming no branch at all. Each is
	// omitted when empty, so an older file decodes exactly as it always did.
	Ending  TaskEnding `json:"ending,omitempty"`
	Branch  string     `json:"branch,omitempty"`
	Merge   string     `json:"merge,omitempty"`
	Result  string     `json:"result,omitempty"`
	Changed []string   `json:"changed,omitempty"`
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
	// duringWrite is run inside the write, and is nil everywhere except in the
	// one test that has to ask what a READER can see while a write is still
	// deciding ([TaskGraph.admitWritten]'s publication boundary). There is no
	// other way to stand inside that window on purpose.
	duringWrite func()
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
	err := s.write(graph)
	// A save after the door shut stays SILENT, exactly as it always was: it is a
	// goroutine that outlived the close, and the log line would blame a file that
	// is perfectly fine (see [taskStore.closed]).
	if err == nil || errors.Is(err, errStoreClosed) {
		return
	}
	log.Printf("session: could not write the task checkpoint %s: %v", s.pathOf(), err)
}

// write is the same snapshot-and-write, ANSWERING FOR ITSELF. It exists because
// one caller cannot treat a failure as bookkeeping: a correction admitted to a
// node's record and acknowledged to the person has to be on the disk the engine
// would resume from, or a crash between the two loses it — or, worse, loses only
// its identity, so the person's retry arrives as a second correction
// (task_room.go's admission law).
//
// Everything else still goes through [taskStore.save] and still only logs.
func (s *taskStore) write(graph *TaskGraph) error {
	if s == nil || graph == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	return s.writeLocked(graph.document())
}

// writeLocked writes ONE ALREADY-TAKEN SNAPSHOT with the store's lock held.
//
// It takes the document rather than the graph because its other caller writes
// while holding the GRAPH's lock too ([TaskGraph.admitWritten]), and asking the
// graph for a document in there would take that lock a second time.
func (s *taskStore) writeLocked(document taskDocument) error {
	if s == nil {
		return nil
	}
	if s.closed {
		// The session has gone. Nothing written now would be resumed from, and a
		// caller asking for a promise gets a refusal rather than a false yes.
		return errStoreClosed
	}
	if s.duringWrite != nil {
		s.duringWrite()
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(s.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// errStoreClosed is a checkpoint asked for after the session's door shut.
var errStoreClosed = errors.New("this session's task checkpoint is closed")

// pathOf is the file this store writes, for a log line about a nil store.
func (s *taskStore) pathOf() string {
	if s == nil {
		return ""
	}
	return s.path
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

// admitWritten puts something on the record and on the disk AS ONE ACT, and
// takes it back off the record if the disk refuses.
//
// ── THE PUBLICATION BOUNDARY IS THE GRAPH'S LOCK, HELD ACROSS THE WRITE ──
//
// Every reader of a node's record — the drain that carries directions into a
// worker's next request, the landing that revalidates against them, the count of
// unread ones — reads the assignment under the GRAPH's lock and knows nothing
// about the store's. So holding only the store's lock would publish the
// direction to those readers the instant it went into the record, while its own
// write was still deciding: a worker could read and act on a correction that a
// failed write then rolled back, and the person would be told nothing was sent.
//
// Holding the graph's lock from the admission to the commit closes that with no
// second copy of the record and no staged state to keep in step: a reader is
// either before the whole act or after it, and after a failure there is nothing
// there to see. The store's lock is held outside it, so no unrelated checkpoint
// can publish a half-decided admission either. The ordering is store.mu →
// graph.mu, which is this file's only ordering.
//
// WHAT IT COSTS is that task readers wait for one file write, and it is paid
// only by a NAMED correction — a sentence a person typed. Every transition still
// checkpoints through [taskStore.save], which holds nothing while it writes.
//
// take and drop are called with the graph's lock ALREADY HELD and must not take
// it again ([TaskNode.heardDirectionLocked], [TaskNode.forgetDirectionLocked]).
//
// A GRAPH WITH NO FILE BEHIND IT PROMISES NOTHING AND SAYS SO — the admission
// still happens, and there is no write to fail. That is a session whose tasks do
// not survive the process at all, so the promise made here is exactly as strong
// as the store is: about resuming THIS engine, and nothing more.
func (g *TaskGraph) admitWritten(take func() directionHeard, drop func(directionHeard)) (directionHeard, error) {
	if g == nil {
		return take(), nil
	}
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		return take(), nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	heard := take()
	// Only a fresh admission changes the record. A repeat, a refusal or a message
	// out of order writes nothing, which is also why neither can fail here.
	if !heard.fresh() {
		return heard, nil
	}
	if err := store.writeLocked(g.documentLocked()); err != nil {
		drop(heard)
		return heard, err
	}
	return heard, nil
}

// dropWritten is the same boundary for the other direction: something comes off
// the record and off the disk together, with no moment in between that a reader
// can see one and not the other. The error is whether the disk agrees again.
func (g *TaskGraph) dropWritten(drop func()) error {
	if g == nil {
		drop()
		return nil
	}
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		drop()
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	drop()
	return store.writeLocked(g.documentLocked())
}

// document is the graph as the file sees it, in admission order — the order that
// makes the frontier deterministic, and the order a person reads the file in.
func (g *TaskGraph) document() taskDocument {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.documentLocked()
}

// documentLocked is the same reading with the graph's lock already held, for the
// admission that publishes and writes under one hold of it
// ([TaskGraph.admitWritten]).
func (g *TaskGraph) documentLocked() taskDocument {
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
	// AN INTERRUPTED ROW IS WRITTEN DOWN AS THE MOVING ROW IT WAS. Interrupted
	// is what a reader makes of a row that was moving when its process went
	// away ([runRowNotice]); it is not a state this file holds, and the row was
	// written back verbatim, so the next reopen refused the whole checkpoint and
	// the conversation lost every task it had, finished ones included. Written
	// as running, it comes back interrupted again, and an older build reads it.
	state := notice.State
	if state == TaskInterrupted {
		state = TaskRunning
	}
	return runRecord{
		ID:        notice.ID,
		Run:       notice.Run,
		Node:      notice.Node,
		Parent:    notice.Parent,
		Title:     notice.Title,
		Kind:      notice.Kind,
		State:     state,
		Stopped:   notice.Stopped,
		Report:    notice.Report,
		Model:     notice.Model,
		CostUSD:   notice.CostUSD,
		ElapsedMS: notice.Elapsed.Milliseconds(),
		StartedAt: notice.StartedAt,
		EndedAt:   notice.EndedAt,
		Copy:      notice.Copy,
		Ending:    notice.Ending,
		Branch:    notice.Branch,
		Merge:     notice.Merge,
		Result:    notice.Result,
		Changed:   append([]string(nil), notice.Changed...),
	}
}

// runRowNotice is one record as the row a column draws again.
//
// A row that said its last word before the process ended comes back verbatim:
// done stays done, failed stays failed, and nothing has happened to either
// since. A row that was still QUEUED OR MOVING is the only one this changes,
// and it changes because the truth about it changed while nobody was watching —
// nothing has been driving it since the process went away.
//
// IT COMES BACK INTERRUPTED AND NOT FAILED. `failed` says something went wrong
// with the work and `stopped` says a person ended it, and neither happened:
// nothing was found out and nobody decided anything ([TaskInterrupted]).
//
// A RESTORED ROW IS NOT MOVING, which is why Doing is not restored and why
// Elapsed is whatever was frozen onto it. Nothing here re-enters the frontier:
// these rows are not in the graph's `nodes` and never were, so there is nothing
// for a scheduler to find (task_run.go's [TaskGraph.runs] says it at length).
// That is a fact about this reader and not a statement that the work is over.
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

		StartedAt: record.StartedAt,
		EndedAt:   record.EndedAt,
		Copy:      record.Copy,
		Ending:    record.Ending,
		Branch:    record.Branch,
		Merge:     record.Merge,
		Result:    record.Result,
		Changed:   append([]string(nil), record.Changed...),
	}
	if !notice.State.settled() {
		// WORK NOTHING IS DRIVING IS INTERRUPTED, NOT FAILED. This row was live
		// when the process that held it went away, and that is a fact about the
		// window rather than about the work: nothing was found out, nobody
		// decided anything, and every step it took is in its store. Stamping it
		// failed and stopped told a person their work had gone wrong and had been
		// ended by somebody, and neither was true ([TaskInterrupted]).
		//
		// A JOB IS THE ONE THING THAT REALLY DID END. A forked process cannot
		// outlive the program that forked it, so there is nothing to continue and
		// `stopped` is the honest word for it.
		if record.Kind != TaskKindJob {
			notice.State = TaskInterrupted
			return notice
		}
		notice.State, notice.Stopped = TaskFailed, true
		// A JOB'S ROW KEEPS ITS OWN SENTENCE, because for a job that sentence is
		// not prose — it is where the log IS ([jobRowLead] mints
		// `job 3 · log /…/3.log`, and [jobNoticeFromRow] reads the path back out
		// of it on the way to a surface).
		//
		// OVERWRITING IT LOST THE ONE THING THE WORK LEFT BEHIND, and it lost it
		// in the commonest case there is: a job still running when codeaf closed
		// is exactly the job somebody reopens the conversation to look at, and it
		// came back with no path at all. The sentence that replaced it was written
		// when this row was DRAWN — it read well under a row on the task column —
		// and nothing draws it now.
		//
		// WHAT IT SAID IS STILL SAID, by the state rather than by prose: the job
		// comes back stopped, which is what the column and the page both show, and
		// "it ended when codeaf closed" is what stopped MEANS for a process that
		// cannot outlive the program that forked it.
	}
	return notice
}

// A JOB'S OWN VERSION OF THIS SENTENCE IS GONE, AND SO IS THE CHOICE BETWEEN
// THEM. `it ended when codeaf closed; its log is kept` was written for a job's
// row on the task column, where it read beside the work it was about. A job has
// no row there any more, and the field the sentence was written into is the one
// carrying the log's path — so the sentence had stopped being read and had
// started deleting the path instead ([runRowNotice] says the rest).
//
// AND THE RUN'S OWN VERSION OF IT IS GONE TOO, for a different reason: it was
// not true. `it ended when codeaf closed; its journal is kept` said the work was
// over, and the work is not over — nothing is driving it and every step it took
// is in its store. What the sentence was carrying is now carried by the reading:
// the state is [TaskInterrupted] and the row asks whether to continue it
// ([TaskAskContinue]), whose own words say that nothing is driving it and that
// everything it did is kept.

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
		Restart:        taskRestartLocked(n.spec),
		ID:             n.id,
		Title:          n.spec.title,
		Summary:        n.spec.summary,
		Request:        n.spec.request,
		OriginJournal:  n.spec.origin.journal,
		OriginLine:     n.spec.origin.line,
		Admission:      recordedAdmission(n.spec.admission),
		Brief:          n.spec.brief,
		Deliverable:    n.spec.deliverable,
		Where:          n.spec.where,
		Ground:         n.Ground,
		Mode:           n.Mode,
		Home:           n.Home,
		HomeSha:        n.HomeSha,
		Rung:           n.Rung,
		Seal:           n.Seal,
		Base:           n.Base,
		CheckBase:      n.CheckBase,
		Universe:       n.Universe,
		Frozen:         n.Frozen,
		Family:         n.Family,
		Checks:         n.Checks,
		FamilyDeclared: n.FamilyDeclared,
		ChecksRevision: n.checksRevision,
		FamilyWas:      n.FamilyWas,
		Acceptance:     n.spec.acceptance,
		PlanID:         n.spec.planID,
		DependsOn:      dependsOn,
		Parent:         n.parent,
		Depth:          n.depth,
		State:          n.state,
		Report:         n.report,
		Late:           append([]string(nil), n.late...),
		Landed:         n.foldedLandedLocked(),
		Ending:         n.endingLocked(),
		Claim:          n.claim,
		Result:         resultRecordOf(n.produced),
		Changed:        changed,
		Wrote:          wrote,
		Branch:         n.branch,
		Worktree:       n.worktree,
		Merge:          n.merge,
		Journal:        n.journal,
		Beat:           beat,
		Model:          n.spec.model,
		CheckedOn:      n.checkedOn,
		NextModel:      n.nextModel,
		NextEffort:     n.nextEffort,
		Effort:         n.spec.effort.String(),
		MaxSteps:       n.spec.maxSteps,
		NoProgress:     n.spec.noProgress,
		ElapsedMS:      elapsed.Milliseconds(),
		StartedAt:      n.started,
		EndedAt:        n.ended,
		CostUSD:        n.cost,
		Input:          n.input,
		Output:         n.output,
		CacheRead:      n.cacheRead,
		CacheWrite:     n.cacheWrite,
		Noted:          n.notedRead,
		NotedState:     n.notedState,
		Attempt:        n.attempt,
		Interrupted:    n.interrupted,
		Decider:        n.decider,
		Clashing:       n.clashing,
		Shifted:        n.shifted,
		GroundHeld:     n.groundHeld,
		Resolving:      n.resolving,
		Kind:           n.kind,
		Offer:          n.offer,
		Quick:          quickRecordLocked(n.spec.quick),
		Unshaped:       n.spec.unshaped,
		Unsized:        n.spec.unsized,
		Expects:        append([]Expectation(nil), n.spec.expects...),
		Assignment:     recordedAssignment(n.assignment),
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

// recordOwesAcceptance reports whether a node record is one this file should
// have been given an acceptance for, and was not. A kind that declares it
// carries none owes nothing ([acceptanceHolds]).
//
// A PLAN-BORN NODE OWES THIS FILE NO ACCEPTANCE EITHER: it is a task out of the
// plan store, admitted with the store's id and nothing else, and what it is held
// to lives there. THE READER MAY NOT REFUSE WHAT THE WRITER WRITES. It did
// (2026-09-18): the first part a run handed to the tree made the whole file
// unreadable, the conversation reopened with no tasks, and its next save
// replaced twenty of them with nothing.
//
// The question has a name of its own because [decodeTasks] is a road already
// longer than its ledger row allows to grow (complexityDebt).
func recordOwesAcceptance(record taskRecord) bool {
	if strings.TrimSpace(record.PlanID) != "" {
		return false
	}
	return !acceptanceHolds(record.Kind, record.Acceptance)
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
//
// AND EVERY RULE HERE IS ASKED OF THE KIND THE RECORD SAYS IT IS. The acceptance
// rule is the one that has an exception, and it is a real one rather than a
// loosening: a quick task is admitted with no acceptance BY DESIGN
// ([kindWithoutAcceptance]), so asking it for one refused the whole document —
// and a conversation that had ever run a quick task reopened with no task graph
// at all, its finished rows, its running work and its whole family gone. Measured
// on a real checkpoint, 2026-09-11. The rule stays strict for ordinary work,
// which is where it was earning its keep.
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
		case recordOwesAcceptance(record):
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
		// A FILE AN EARLIER BUILD WROTE WITH AN INTERRUPTED ROW is read, not set
		// aside whole: that build wrote the row back as the reader had drawn it
		// ([runRowRecord] says why that no longer happens), and refusing the
		// file for it cost the conversation every task it had.
		case !validTaskState(record.State) && record.State != TaskInterrupted:
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
	// deliveries are the durable deliveries the re-told notes are: one per
	// landing this session still owes, settled when the recipient's record holds
	// the note ([durableDelivery]). Without them a resume marked its own
	// re-telling as said the moment it composed it, so a session closed unread
	// twice lost the landing exactly as the first enqueue-time mark did.
	deliveries []durableDelivery
	done       int
	failed     int
	unverified int
	// cutRounds is how many of those `your call` nodes had a MERGE ROUND in
	// flight when the process ended (task_merge_round.go). They are a subset of
	// unverified and never a category of their own: the node is in exactly the
	// state it was in before the round started, and what the clause adds is the
	// one thing the card cannot say for itself — that the press the person
	// remembers making did happen, and bought nothing.
	cutRounds   int
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
	// quick is a QUICK TASK the close caught, running or still waiting. It is
	// counted apart for the reason runs are — it does not resume ([interrupt]) —
	// and apart from runs because what it left is different: not a journal but
	// whatever it had written in the person's own folder, which its own note
	// names beside the items it had ticked.
	quick int
	// branches are the interrupted nodes' branches that are still on disk. They
	// are the whole reason the summary is worth reading: a kept branch is work
	// the person still has.
	branches []string
	// failedBranches and unverifiedBranches are the same fact for the two states
	// that settle rather than resume: a node that ended failed or unverified kept
	// its deliverable on its own `task/<slug>` branch and did NOT merge it home
	// ([keptWork]), and the note that counts it must name that branch or a person
	// told "1 incomplete" has nowhere to go and look. They are the chat-side half
	// of the `kept_branch` and `verdict` #1182 put on the headless envelope.
	failedBranches     []string
	unverifiedBranches []string
	// notes are the completion notes that were never handed over, in the shape
	// [taskNote] would have produced for them.
	notes []string
	// handedBack are the landings the AUTO-SETTLE FLOOR took off the model on the
	// way in ([TaskGraph.handBackOnLoad]) and that are still waiting on a decision.
	// They are here for the caller to publish once it holds an agent, because a
	// notice is read with the graph let go of ([TaskNode.notice] states the
	// ordering) and this half of recovery is a pure function of a file.
	//
	// THEY ARE DELIBERATELY NOT AMONG THE NOTES. The model has already been told
	// about each of these landings — that is what put the question in its hands —
	// and a resumed session opening by telling it the same landing again would be
	// news about nothing that happened. What changed is who is holding the
	// question, and the person is the one who needs to see that: it reaches them
	// as an ordinary task update, which is the same lane the end-of-turn floor
	// publishes on.
	handedBack []*TaskNode
}

// any reports whether the recovery restored anything at all. A checkpoint that
// held an empty graph — a session that proposed nothing — is not news.
func (r taskRecovery) any() bool {
	return r.done+r.failed+r.unverified+r.interrupted+r.designs+r.asking+r.runs+r.quick+r.waiting > 0
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
//
// cutRoundsWord is the clause under the your-call count for the rounds that
// died with the process, and "" when none did.
//
// IT SAYS "WAS CUT" AND NEVER "FAILED". Nothing was found wrong with the work
// and nothing was lost: the round's worker stopped existing, the working copy is
// where it was, and the card is offering the same three answers. The one thing
// it must NOT imply is that anything is being retried — nothing restarts a round
// but the person pressing again.
func cutRoundsWord(unverified, cut int) string {
	switch {
	case cut <= 0:
		return ""
	case cut == 1 && unverified == 1:
		return "its merge round was cut"
	case cut == 1:
		return "1 with a merge round cut"
	default:
		return strconv.Itoa(cut) + " with a merge round cut"
	}
}

func (r taskRecovery) note() string {
	if !r.any() {
		return ""
	}
	parts := make([]string, 0, 5)
	if r.done > 0 {
		parts = append(parts, strconv.Itoa(r.done)+" done")
	}
	// THE WORDS ARE THE TIER WORDS AND NOT THE STATES' OWN (task_status.go).
	// `failed` sent a person looking for a fault in work nobody had judged, and
	// `unverified` was the machinery describing itself; the counts are the same
	// counts, said in the words every other place a task is drawn now uses.
	if r.failed > 0 {
		clause := strconv.Itoa(r.failed) + " " + taskWordIncomplete
		clause = withKeptBranches(clause, r.failedBranches)
		parts = append(parts, clause)
	}
	if r.unverified > 0 {
		clause := strconv.Itoa(r.unverified) + " " + taskWordYourCall
		if word := cutRoundsWord(r.unverified, r.cutRounds); word != "" {
			clause += " (" + word + ")"
		}
		clause = withKeptBranches(clause, r.unverifiedBranches)
		parts = append(parts, clause)
	}
	if r.interrupted > 0 {
		parts = append(parts, strconv.Itoa(r.interrupted)+" interrupted ("+keptBranches(r.branches)+")")
	}
	// THE KINDS THAT DO NOT RESUME each say so in a clause of their own, in the
	// order a person meets them, and each is one counted phrase — which is why
	// they are a table and not four copies of the same three lines.
	for _, clause := range []string{
		countedClause(r.designs, "design did not finish (nothing saved)", "designs did not finish (nothing saved)"),
		countedClause(r.asking, "design asks again", "designs ask again"),
		countedClause(r.runs, "run did not finish", "runs did not finish"),
		countedClause(r.quick, "quick task did not finish", "quick tasks did not finish"),
	} {
		if clause != "" {
			parts = append(parts, clause)
		}
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

// countedClause is one count and its phrase, singular or plural, and "" for a
// count of nothing — the emptiness law, so the note has no "0 designs" in it.
func countedClause(count int, one, many string) string {
	switch {
	case count <= 0:
		return ""
	case count == 1:
		return "1 " + one
	}
	return strconv.Itoa(count) + " " + many
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

// withKeptBranches hangs the "(branch <b> kept)" clause on a counted category
// that kept work, and leaves the clause alone when it kept none: a failed node
// that never reached a repository has no branch to name, and a clause saying so
// would send a person looking for work that was never there. It reuses
// [keptBranches] so a category that kept several wears the same plural clause an
// interrupt does.
func withKeptBranches(clause string, branches []string) string {
	if len(branches) == 0 {
		return clause
	}
	return clause + " (" + keptBranches(branches) + ")"
}

// appendKeptBranch adds the branch a settled node's work was kept on, or nothing
// for a node whose work came home, was laid in place, or never had a branch.
func appendKeptBranch(branches []string, record taskRecord) []string {
	if branch := keptBranchOf(record.Branch, record.Merge); branch != "" {
		branches = append(branches, branch)
	}
	return branches
}

// keptBranchOf names the branch a node's work was KEPT on, or "" for work that
// came home or was laid in place. The three merge words that keep a branch are
// task_run.go's own: [mergeAborted] for work kept instead of merged
// ([keptWork], the ending a failure, a stop or a refused gate takes),
// [mergeConflicted] for a merge that would not go cleanly, and [mergeKept] for a
// landing deliberately left on a protected, moved or detached checkout.
func keptBranchOf(branch, merge string) string {
	if branch = strings.TrimSpace(branch); branch == "" {
		return ""
	}
	switch merge {
	case mergeAborted, mergeConflicted, mergeKept:
		return branch
	}
	return ""
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
	if !found {
		a.setAsideRefusedCheckpoint()
		return
	}
	if len(document.Nodes) == 0 && len(document.Runs) == 0 {
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
	// AND THE HAND-BACK IS TOLD. Every landing the floor took off the model on the
	// way in is one ordinary task update, on the lane a surface already folds into
	// the row it is drawing — the same lane and the same shape the end-of-turn
	// floor publishes on (task_run.go's [Agent.handBackUnsettled]). Nothing is
	// attached yet on a fresh process and the sends fall on an empty room, which is
	// correct: what the surface reads then is the roster replay, and these nodes
	// are in it saying the person is deciding. On a window attaching to a
	// conversation that is already open, this is the update that takes the `codeaf
	// is deciding` row off the card.
	for _, node := range recovery.handedBack {
		a.emitTaskUpdate(node.notice())
	}

	if note := recovery.note(); note != "" {
		// THE AMBIENT LANE, not the waking one (agent.go): this runs at
		// construction, before anybody has said anything, and an account of what
		// the last process left behind is context for the first turn rather than
		// a reason to start one. A session that opened by talking to itself about
		// yesterday's interrupt would be answering a question nobody asked.
		line := userText(note)
		line.delivered = recovery.deliveries
		a.accept(delivery{origin: fromRuntime, kind: msgNotice, note: line})
	}
	// CONTINUE — the same frontier every other transition turns. A queued node
	// whose prerequisites are done starts now; one whose prerequisite was
	// interrupted fails through the cascade that already exists.
	graph.runFrontier()
	// A saved naming failure is not an earned name. Retry only those rows from
	// their saved briefs; reopening never renames valid work or reruns a task.
	for _, record := range document.Nodes {
		if unusableName(record.Title) || namesTheInstruction(record.Title) {
			graph.nameNode(graph.node(record.ID))
		}
	}
}

// reconcile files ONE record and answers it as the graph is to hold it: a node
// the close caught is turned into what it became ([interrupt]) and counted under
// its own clause, and every other is counted as it stands ([taskRecovery.countSettled]).
//
// Which records the close caught is [nothingIsComingBackForIt]'s to say.
//
// AND THE KINDS THAT DO NOT RESUME ARE COUNTED APART. Which way a design went is
// read off what the interrupt made of it: back on the frontier with its page (it
// asks again), or over with nothing saved. Counting either beside ordinary
// interrupted work would be the summary promising a resume that is not that kind
// of resume — and so would counting a run or a quick task there, under a clause
// that says `no branch kept` about work that never had a branch and is not
// coming back.
// refusedCheckpointSuffix names a checkpoint this build could not read, beside
// the path it was read from, with the second it was set aside.
const refusedCheckpointSuffix = ".refused-"

// setAsideRefusedCheckpoint is what a conversation does with a checkpoint that
// is THERE and that it cannot read. A REFUSED CHECKPOINT IS NEVER OVERWRITTEN:
// the graph opens empty, and its first save used to land on the same path, so
// one record a newer or an older build spelled differently cost the person
// every task the conversation had run, with nothing left to recover them from.
// The file is moved beside itself instead. AND AN ID IS NEVER REUSED: the
// counter lived only in that file, so it is raised past every task that left a
// journal or a working copy on disk, or the next task would answer to a number
// the transcript already uses for another.
func (a *Agent) setAsideRefusedCheckpoint() {
	path := a.config.checkpointFile()
	if strings.TrimSpace(path) == "" {
		return
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return
	}
	aside := path + refusedCheckpointSuffix + strconv.FormatInt(time.Now().Unix(), 10)
	if err := os.Rename(path, aside); err != nil {
		log.Printf("session: could not set the refused task checkpoint aside: %v", err)
	} else {
		log.Printf("session: the refused task checkpoint is kept at %s", aside)
	}
	highest := highestTaskOnDisk(a.config.Place.NodeJournals(), a.config.Place.Trees())
	graph := a.graph()
	if graph == nil {
		return
	}
	graph.mu.Lock()
	if highest > graph.seq {
		graph.seq = highest
	}
	graph.mu.Unlock()
}

// highestTaskOnDisk is the largest task number that left a folder or a journal
// behind in the places a conversation keeps them, read from the names alone.
func highestTaskOnDisk(dirs ...string) uint64 {
	var highest uint64
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			// A working copy is named by its number alone; a journal is
			// `<when>_<number>.jsonl`. The number is what follows the last
			// underscore once the extension is gone.
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			if cut := strings.LastIndexByte(name, '_'); cut >= 0 {
				name = name[cut+1:]
			}
			if id, err := strconv.ParseUint(name, 10, 64); err == nil && id > highest {
				highest = id
			}
		}
	}
	return highest
}

func (r *taskRecovery) reconcile(record taskRecord, workspace string) taskRecord {
	if !nothingIsComingBackForIt(record) {
		r.countSettled(&record)
		return record
	}
	kind := record.Kind
	record, kept := interrupt(record, workspace)
	switch {
	case kind == TaskKindHarness && record.State == TaskQueued:
		r.asking++
	case kind == TaskKindHarness:
		r.designs++
	case kind == TaskKindSubharness:
		r.runs++
	case kind == TaskKindQuick:
		r.quick++
	default:
		r.interrupted++
	}
	if kept != "" {
		r.branches = append(r.branches, kept)
	}
	return record
}

// countSettled files ONE node that was not running when the file was written,
// and it is a function of its own because [TaskGraph.rehydrate] is at its
// ending budget (internal/session's complexity ratchet) — the counting is a
// decision about one record and nothing about the graph, so it reads better
// here anyway.
//
// It takes a POINTER because one of the four answers edits the record: a merge
// round's claim is dropped as it is counted, and the record the caller goes on
// to restore has to be the edited one.
func (r *taskRecovery) countSettled(record *taskRecord) {
	switch record.State {
	case TaskDone:
		r.done++
	case TaskFailed:
		r.failed++
		r.failedBranches = appendKeptBranch(r.failedBranches, *record)
	case TaskUnverified:
		// Counted apart from both: it is not work that failed and it is not work
		// still to come, it is work waiting on a person (task_contract.go's
		// TaskUnverified). A resumed session that filed it under "waiting" would
		// be telling somebody the scheduler will get to it, and the scheduler
		// never will.
		r.unverified++
		r.unverifiedBranches = appendKeptBranch(r.unverifiedBranches, *record)
		// AND A ROUND THAT WAS IN FLIGHT IS SAID OUT LOUD. The person pressed
		// `resolve it`, a worker opened in the working copy, and the process died
		// under it — so the card is back offering the same three answers it
		// offered before they pressed, which without a word about it reads as a
		// press that never happened. The claim itself is dropped here: it existed
		// to stop a second worker joining the first, and the first is gone
		// ([taskRecord.Resolving] states the whole rule).
		if record.Resolving {
			r.cutRounds++
			record.Resolving = false
		}
	default:
		r.waiting++
	}
}

// nothingIsComingBackForIt is which restored records [interrupt] has to settle:
// the work whose state on the record is a state nothing is in.
//
// A RUNNING NODE IS THE WHOLE OF IT, ALMOST. Its worker stopped existing the
// moment the process did, and what it left is on disk under a branch nobody is
// going to come back for unless somebody says its name.
//
// The exception is a QUICK node that was still WAITING ITS TURN. It never
// started, so nothing about it was interrupted in the ordinary sense; what puts
// it here is that it must not start now. Its body is on the record
// ([taskRecord.Quick]), so it could be rebuilt — but nothing of a quick task
// outlives its window: it was asked for by a turn that is over, it would run in
// whatever folder this session's runner stands in rather than the one its
// caller was in, and its answer would arrive in a conversation that has since
// forgotten why it wanted one ([interrupt] carries the whole argument).
func nothingIsComingBackForIt(record taskRecord) bool {
	return record.State == TaskRunning ||
		(record.Kind == TaskKindQuick && record.State == TaskQueued)
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
// [nothingIsComingBackForIt] is the whole of which records those are.
func (g *TaskGraph) rehydrate(document taskDocument, workspace string, settle TaskSettle) taskRecovery {
	var recovery taskRecovery
	records := make([]taskRecord, 0, len(document.Nodes))
	for _, record := range document.Nodes {
		records = append(records, recovery.reconcile(record, workspace))
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

	// The notes nobody ever got, in the shape a fresh run would have produced.
	// They are QUEUED and not announced: a life that closes before anybody reads
	// one still owes it, and the next life says it again ([durableDelivery]).
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
	notes, deliveries := g.owedNotes(unannounced, settle, address)
	recovery.notes = append(recovery.notes, notes...)
	recovery.deliveries = append(recovery.deliveries, deliveries...)
	// AND THE FLOOR, WHICH IS THE ONE RECONCILIATION THIS FILE MAKES BESIDE THE
	// INTERRUPT. A node the record says the model was holding has no turn left to
	// be decided in, so it comes back to the person here — before the frontier
	// turns, before anything is drawn, and before the checkpoint above it is
	// rewritten, so the file on disk stops saying it too
	// ([TaskGraph.handBackOnLoad] states the law).
	recovery.handedBack = g.handBackOnLoad()
	return recovery
}

// owedNotes composes the landings this session still owes and the deliveries
// that settle them.
//
// A RE-TELLING IS A DELIVERY LIKE ANY OTHER: queued, not announced. A life that
// closes before anybody reads one still owes it, and the next life says it
// again — which is what a resume that marked its own re-telling as said the
// moment it composed it could not do ([durableDelivery]).
//
// AND THE RECORD IS ASKED FIRST. The checkpoint says these are owed, and the
// checkpoint may simply not have been written: a process killed between the
// recipient recording a note and the file being saved comes back here with the
// mark off. The journal is the record that WAS written, so a landing whose
// delivery id is already on one of its lines is settled rather than said twice
// ([sessionFile.recorded]).
func (g *TaskGraph) owedNotes(unannounced []*TaskNode, settle TaskSettle, address landingAddress) ([]string, []durableDelivery) {
	var (
		notes      []string
		deliveries []durableDelivery
	)
	for _, node := range unannounced {
		claim, claimed := node.claimNote(node.attemptNow())
		if !claimed {
			continue
		}
		delivery := node.settlesNote(claim)
		if g.home != nil && g.home.hasRecorded(delivery.id) {
			delivery.settled()
			continue
		}
		// A RE-TELLING IS READ THE SAME WAY THE FIRST TELLING WOULD HAVE BEEN, so
		// it opens on the same lead ([landingNoteLead]). A resumed session is in
		// fact the shape that needs it most: nobody has typed, the note is the
		// whole message, and the model has no turn behind it to infer who wrote it.
		//
		// THE NOTICE IS READ ONCE, for [Agent.reportTaskNode]'s reason: a lead and
		// a head composed from two readings of a node could name two different
		// landings of it.
		notice := node.notice()
		notes = append(notes, landingNoteLead(notice)+taskNote(notice, taskURI(node.journalPath()), settle, address))
		node.noteQueued(claim)
		deliveries = append(deliveries, delivery)
	}
	return notes, deliveries
}

// restoreNode is one record as a node again. Its done channel is CLOSED for a
// terminal node — a waiter on finished work must not block on a run that is
// never going to happen — and open for a queued one, which is genuinely still
// waiting.
func restoreNode(graph *TaskGraph, record taskRecord) *TaskNode {
	// Keep a readable fallback while the normal naming lane repairs old failures.
	if unusableName(record.Title) || namesTheInstruction(record.Title) {
		subject := record.Summary
		if strings.TrimSpace(subject) == "" {
			subject = record.Request
		}
		if strings.TrimSpace(subject) == "" {
			subject = record.Brief
		}
		record.Title = taskPersonTitle(subject)
	}
	node := &TaskNode{
		nextModel:  record.NextModel,
		nextEffort: record.NextEffort, graph: graph,
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
			planID:      record.PlanID,
			dependsOn:   record.DependsOn,
			model:       record.Model,
			effort:      restoredRung(record.Effort),
			maxSteps:    record.MaxSteps,
			noProgress:  record.NoProgress,
			expects:     record.Expects,
			// AND A QUICK NODE'S BODY, which is what tells the runner it is quick
			// and what the next checkpoint writes back ([taskRecord.Quick]).
			quick: record.Quick.body(),
			// AND THE READINGS THIS NODE IS STILL OWED, so that a `/task` that was
			// queued when the engine went down still has its brief written and its
			// width read beside the worker it finally gets ([taskRecord.Unshaped]).
			unshaped: record.Unshaped,
			unsized:  record.Unsized,
		},
		// THE PREFLIGHT IS OWED ONLY BY A NODE THAT HAS NOT RUN. The contract is
		// answered before a node's first step (task_run.go's
		// [Agent.briefMatchesItsWorld]), so a record that started has answered it,
		// and handing it the manifest again would put a question already settled
		// to a working copy that work has since changed. The brief keeps its
		// section either way, above.
		Expects:        expectsOwed(record),
		Ground:         record.Ground,
		Mode:           record.Mode,
		Home:           record.Home,
		HomeSha:        record.HomeSha,
		Rung:           record.Rung,
		Seal:           record.Seal,
		Base:           record.Base,
		CheckBase:      record.CheckBase,
		Universe:       record.Universe,
		Frozen:         record.Frozen,
		Family:         record.Family,
		Checks:         record.Checks,
		FamilyDeclared: record.FamilyDeclared,
		checksRevision: record.ChecksRevision,
		FamilyWas:      record.FamilyWas,
		state:          record.State,
		report:         record.Report,
		landed:         record.landedHalf(),
		late:           append([]string(nil), record.Late...),
		ending:         record.Ending,
		kind:           record.Kind,
		claim:          record.Claim,
		checkedOn:      record.CheckedOn,
		produced:       resultFromRecord(record.Result),
		changed:        record.Changed,
		wrote:          record.Wrote,
		branch:         record.Branch,
		worktree:       record.Worktree,
		merge:          record.Merge,
		journal:        record.Journal,
		elapsed:        time.Duration(record.ElapsedMS) * time.Millisecond,
		started:        record.StartedAt,
		ended:          record.EndedAt,
		cost:           record.CostUSD,
		input:          record.Input,
		output:         record.Output,
		cacheRead:      record.CacheRead,
		cacheWrite:     record.CacheWrite,
		noted:          record.Noted,
		notedRead:      record.Noted,
		notedState:     record.NotedState,
		attempt:        record.Attempt,
		interrupted:    record.Interrupted,
		// AND WHO WAS HOLDING ITS DECISION, which is read back exactly as it was
		// written and then put right by the floor a few lines above this node's
		// arrival ([TaskGraph.handBackOnLoad], called out of [TaskGraph.rehydrate]).
		// Restoring it faithfully and handing it back deliberately is the whole
		// point: the alternative — dropping it here — is the right answer with no
		// act behind it, which is what nothing could seed and nothing could watch.
		decider: record.Decider,
		// AND WHAT ITS LANDING WAS ASKING ABOUT, faithfully: the files, and which
		// of the three roads put them there ([taskRecord.Clashing]).
		clashing:   record.Clashing,
		shifted:    record.Shifted,
		groundHeld: record.GroundHeld,
		offer:      record.Offer,
		assignment: restoredAssignment(record.Assignment),
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
	restoreTaskRestart(node, record.Restart)
	if record.State != TaskQueued {
		close(node.done)
	}
	return node
}

// expectsOwed is the manifest a restored node still has to answer: all of it
// for a node that never ran, and nothing for one that did.
func expectsOwed(record taskRecord) []Expectation {
	if !record.StartedAt.IsZero() || record.Interrupted {
		return nil
	}
	return record.Expects
}

// harnessInterruptedReport is what a design that was still being written when
// the process ended says for itself. It is the wording a design that ran out of
// its own clock already uses, because from the person's side the two are one
// fact: the page was not finished and nothing was kept.
const harnessInterruptedReport = "the design did not finish before codeaf closed; nothing was saved"

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

		// An interrupted design settles until the person explicitly retries it.
		// New records retain its runner inputs; older rows may only have a result.
		record.State = TaskFailed
		record.Report = harnessInterruptedReport
		record.EndedAt = interruptedAt(record)
		return record, ""
	}

	if record.Kind == TaskKindSubharness {
		// A saved workflow does not replay automatically after interruption.
		// Explicit Retry may restart it using the persisted runner and input.
		record.State = TaskFailed
		record.Report = subharnessInterruptedReport
		record.EndedAt = interruptedAt(record)
		return record, ""
	}

	if record.Kind == TaskKindQuick {
		// Quick work settles when its process disappears. The saved checklist
		// supports an explicit retry, but opening a conversation spends nothing
		// on replaying an unfinished quick task without the person's request.
		record.Report = quickReportOnClose(record)
		record.State = TaskFailed
		record.Changed = mergePaths(record.Changed, record.Wrote)
		record.EndedAt = interruptedAt(record)
		return record, ""
	}

	// A process exit pauses ordinary work; it does not make a finding about it.
	// Put the node back on the frontier so the next session resumes it once.
	record.State = TaskQueued
	// AND THE CUT ATTEMPT'S REASON DOES NOT RIDE INTO THE NEXT ONE. A node
	// machinery cut where it stood carries [TaskEndingInterrupted] on its record
	// (task_run.go's paused road), and [TaskNode.end] writes only the FIRST cause —
	// so a resumed attempt that failed for a reason of its own would still read as
	// the interruption that never was its. The node is about to run again and the
	// ending belongs to the attempt that just ended, so it is cleared here with it.
	record.Ending = ""

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

// LoadLandedForJudge reads a session's persisted task checkpoint at tasksPath and
// returns one TaskLanding per node in a final state (Done, Failed or Unverified),
// rebuilt from the record so a reader that arrives after the process that ran the
// node is gone can still judge it. It mirrors (*TaskNode).landing() with two
// differences forced by reading off disk: High is the persisted CheckedOn (empty
// on older records), and Tokens is the record's own Input+Output, because the
// live room's usage a landing also counts is gone with the process. A missing or
// unreadable checkpoint yields no landings and no error, the same nothing
// recoverTasks reads it as.
func LoadLandedForJudge(tasksPath string) ([]TaskLanding, error) {
	document, ok := loadTaskCheckpoint(tasksPath)
	if !ok {
		return nil, nil
	}
	var landed []TaskLanding
	for _, record := range document.Nodes {
		switch record.State {
		case TaskDone, TaskFailed, TaskUnverified:
		default:
			continue
		}
		landed = append(landed, TaskLanding{
			ID:          record.ID,
			State:       record.State,
			Brief:       record.Brief,
			Deliverable: record.Deliverable,
			Report:      record.Report,
			Claim:       record.Claim,
			Ending:      string(record.Ending),
			Wrote:       record.Wrote,
			Changed:     len(record.Changed),
			Checks:      record.Checks,
			Worker:      record.Model,
			High:        record.CheckedOn,
			CostUSD:     record.CostUSD,
			Tokens:      record.Input + record.Output,
			Attempt:     record.Attempt,
		})
	}
	return landed, nil
}
