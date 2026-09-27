package session

// Background execution for the session agent.
//
// A session is a conversation, and a conversation cannot wait ten minutes for
// `npm run dev` to exit — the command never exits, that is the point of it. So
// bash grows one optional argument (background:true, tools.go) and the process
// it starts becomes a JOB: started, registered, watched, and reported on when
// it ends. The model gets an id back in the same beat it made the call.
//
// Three choices here are worth the words:
//
//   - RING + BOUNDED DISK, not one or the other. What a job writes goes to a
//     disk spool bounded to its most recent jobSpoolChunks chunks of
//     jobSpoolChunkBytes each — <id>.log for the live chunk, <id>.log.1 for the
//     one before it, anything earlier discarded with a notice — so the recent
//     log is addressable by the read tool — paged, offset, grepped, the same
//     way any other file is — without a job that prints for a week filling the
//     disk. At a chunk boundary the live bytes are copied once to the backup
//     before the same live inode is truncated; its identity never disappears. Only the last 64KB is kept in memory, and only that tail is
//     ever handed back through a tool result. A watcher that has printed 400MB
//     must not be able to put 400MB in front of the model, and a watcher that
//     printed the one line that matters must not lose it because nobody was
//     polling — the disk answers that for as long as the line is recent, and
//     the notice every footer carries is what keeps the promise honest once it
//     is not (issue #1599).
//
//     WHERE that file is, is landing.go's answer and not this file's: a log is
//     a dropping, so once a session has a folder it lands in the folder rather
//     than in the person's repository. The old reason for keeping it under the
//     workspace — that the read tool reached it with a relative path — is
//     superseded and was never the point: the tool takes an absolute path, and
//     the job card prints one.
//
//   - THE OWED LANE, not a tool and not an event. When a bash job ends, its
//     ending joins the session's boundary batch (agent.go), drained at the next
//     step. The ending carries its output tail and names the log; anything recent
//     remains here behind `jobs output` and on disk, and the footer names the
//     truncation when the beginning has been discarded. A completion is news,
//     not an answer to a question, and the alternative — the model polling
//     `jobs` on a hunch — costs a round trip per hunch and still misses the exit
//     it did not think to check for.
//
//   - NO PUSH MID-BATCH. The ending lands at a step boundary and never inside
//     one, for the same reason user steering does: the transcript's tail
//     mid-batch sits between an assistant's tool_calls and their results, and
//     a user message spliced in there is a shape every provider rejects. A job
//     that exits during a tool batch is reported after that batch, which is
//     the first moment the model could act on it anyway.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

const (
	// jobRingBytes is the in-memory tail. 64KB is a few hundred lines of a
	// build log — enough that `jobs output` after a failure shows the failure,
	// and small enough that a hundred jobs cost megabytes, not gigabytes.
	jobRingBytes = 64 << 10

	// jobSpoolChunkBytes caps one chunk of the disk spool and jobSpoolChunks is
	// how many of them a job keeps — the live chunk plus one older — so a
	// hundred jobs cannot fill the disk either: 8MB each, and what falls out
	// of the window is discarded, with [jobSink.notice] saying so.
	jobSpoolChunkBytes = 4 << 20
	jobSpoolChunks     = 2

	// jobTermGrace is how long a SIGTERM has to work before SIGKILL follows.
	// Two seconds is a server's shutdown hook, not a wait.
	jobTermGrace = 2 * time.Second

	// jobExitNoteLimit caps the log line quoted in the completion note's first
	// line — the headline, which has to fit on one line beside the exit code.
	jobExitNoteLimit = 120

	// jobExitTailLines is how much of a finished job's output rides in the note
	// itself.
	//
	// A COMPLETION IS DELIVERED WHOLE, OR IT IS NOT DELIVERED. The note used to
	// be one line — `job 3 exited 0: BUILD OK` — and a model that read it still
	// knew nothing about what the job had DONE, so its next move was a call to
	// `jobs output`, which is a round trip to learn the thing the note was
	// already about. Fifty lines is `jobs output`'s own default and the tail of
	// a build log where the failure is; the whole log is still on disk and the
	// note still names it.
	jobExitTailLines = jobsDefaultTail

	// jobExitNewsRule is the one clause a job's ending carries about ITSELF: it
	// is news, and it asks for nothing.
	//
	// A HARNESS-AUTHORED MESSAGE CARRIES ITS OWN READING INSTRUCTION, which is
	// the delivery class docs/design/prompt-diet/DESIGN.md §2 files this under
	// and the pattern [standingNewsRule] was already written in — right down to
	// the opening words, because a job's ending and a standing item's firing are
	// the same kind of arrival and a model should not have to learn two frames
	// for it. Saying it here costs the turn a job ends on; saying it on the page
	// cost every request of every turn.
	//
	// IT NAMES WHAT IT IS FORBIDDING for [standingNewsRule]'s reason: a model
	// holding `bash` reads an exit code as an invitation to run the thing again,
	// and a model holding `jobs` reads a quiet job as something to go and check.
	jobExitNewsRule = "— this already happened: relay it if it matters, never re-run it and never poll for it."
)

// jobState is what a job is doing now.
type jobState int

const (
	jobRunning jobState = iota
	jobExited
	jobKilled
)

// jobKind is what KIND of background work a job is: one process that was
// started and will end (bash background:true), or a watch — a command re-run on
// a timer, reporting only when there is news (tools_watch.go).
//
// The kinds share this registry rather than living in two of them because
// everything AROUND them is the same: one id space, one log file per job, one
// list to read, one kill to end it, one shutdown at Close. What differs is only
// what runs in the middle, which is why the difference is a field and a stop
// function rather than a second machine.
type jobKind int

const (
	jobKindBash jobKind = iota
	jobKindWatch
	// jobKindTask is one node of the task graph (task_run.go): a whole child
	// agent working in its own worktree.
	//
	// It is in this registry for the reason a watch is — everything AROUND it is
	// what a job already is: one id space, one log file, one row in the list,
	// one kill, one death at Close. A node's kill is its context being cancelled
	// rather than a signal to a process group, which is the same stop function a
	// watch already has.
	jobKindTask
	// jobKindRender is one provider generation in a goroutine: a video render
	// (tools_video.go) or a music compose (tools_music.go).
	//
	// A render is asynchronous on the wire or simply slow — a video is submit-
	// then-poll for as long as ten minutes, a compose is one long streaming
	// call — and a turn that waited for one would be a conversation held
	// hostage by a file nobody can look at yet. So it is a job for the reason a
	// watch is: everything AROUND it is what a job already is, and only the
	// middle differs. Its middle is one provider call in a goroutine, its kill
	// is that call's context being cancelled — the same stop function a watch
	// has — and its ending is a note on the steering lane carrying the landed
	// path or the failure.
	jobKindRender
)

// job is one background command.
//
// The mutex guards the mutable status fields only. It is deliberately NOT the
// Agent's lock: a watcher goroutine reaping a process must never contend with
// the lock a turn holds, least of all the one Interrupt needs to be able to
// take at any moment.
type job struct {
	// The admission generation rejects work reserved before a conversation stop.
	epoch   uint64
	id      int
	command string
	kind    jobKind
	// label and detail are a watch's short name and its terms ("every 10s on
	// change"), empty for a bash job. They are set once at start and read
	// without the lock.
	label  string
	detail string
	// dir is the folder the job was started in — the registry's workspace AT
	// THE FORK, taken once and never moved. It is the job's own because the
	// workspace is not: `anchor_workspace` re-roots the registry mid-session
	// (tools_anchor_workspace.go), and a dev server started before that still
	// runs where it was started, which is what a row naming its folder must say.
	dir     string
	started time.Time
	logPath string
	cmd     *exec.Cmd
	// group is the process group recorded at launch, so a kill is checked
	// against the identity the leader had then and a recycled pid is never
	// signalled (internal/processgroup).
	group processgroup.Group
	sink  *jobSink
	// stop ends a watch's timer loop. It is nil for a bash job, whose end is a
	// signal to a process group instead. See [job.signal].
	stop func()
	// explicitStop marks a task stop requested through `jobs kill`. Shutdown
	// deliberately does not call it: process exit pauses task work for resume.
	explicitStop func()
	// done is closed once the job is final — the process reaped, or the watch
	// loop returned — and the status fields are settled. It is how a killer
	// waits without polling.
	done chan struct{}

	mu sync.Mutex
	// name is the short name this job is CALLED, and it is under the lock because
	// it arrives LATE: the namer is an errand on a goroutine of its own
	// (jobname.go) and answers, when it answers, well after the job started
	// saying things. It is empty for a job that has a label already, and for one
	// whose namer never came back.
	name  string
	state jobState
	// exitCode is meaningful only in jobExited.
	exitCode int
	ended    time.Time
	// ticks counts a watch's completed runs of its command.
	ticks int
	// killRequested marks a kill this session ASKED for — jobs.kill, or Close.
	// Such a job does not report its own death: the caller already knows, and
	// a note saying so would be the agent telling itself what it just did.
	killRequested bool
	// personStopped says the requested death this job is about to have is A
	// PERSON'S STOP ([Agent.cancelJob]) rather than the model's `jobs kill` or a
	// shutdown.
	//
	// IT IS SET BEFORE THE KILL IS ASKED FOR, which is what makes it readable by
	// everybody who matters: [job.requestKill] is what makes the death REQUESTED,
	// so any settle that sees a requested death also sees this mark. It exists
	// because a person's stop is the one requested death that OWES the model a
	// note, so it is the one whose parked worker must be released after that note
	// and not the instant the process dies ([jobRegistry.settleExit]).
	personStopped bool
	// owed says this command was TAKEN OVER from a call that was still waiting
	// for it, and that the work has not yet been told how it ended.
	//
	// IT IS PROVENANCE AND NOT A STATE, and the distinction is the whole of what
	// it is for. `background: true` is a command the work asked to be FREE of, so
	// a job that started that way owes nobody anything and this stays false
	// ([jobRegistry.start]). A foreground call the background-after clock or the
	// command's own timeout took over is a command the work is still WAITING for,
	// so that road sets it (promote.go). A person's steer sets it false again,
	// because a steer is the person redirecting the work and the model must act on
	// their words rather than wait (steer.go). What is left true is exactly the
	// commands somebody is still standing over, which is what a task worker parks
	// on (task_job_park.go).
	owed bool
}

// jobInfo is a job's status copied out from under its lock, so rendering never
// holds it.
type jobInfo struct {
	id      int
	command string
	kind    jobKind
	label   string
	detail  string
	// name is the short name the job is CALLED — the label where the registry
	// minted one, and otherwise whatever the cheap namer answered (jobname.go).
	// It is empty until there is one: naming is an errand and the work never
	// waits on it.
	name string
	// logPath is where everything this job wrote is spooled. It is copied out
	// with the rest because a job's row has no transcript, no branch and no
	// report to point a person at, and the log is what it points at instead
	// (jobnotice.go).
	logPath string
	// dir is where the job was started ([job.dir]).
	dir   string
	state jobState
	code  int
	ticks int
	// started is when the process forked, copied out beside elapsed so a surface
	// can count a live job's clock up on its own beat rather than re-asking the
	// engine for a duration four times a second (jobnotice.go says why both).
	started time.Time
	elapsed time.Duration
}

func (j *job) info() jobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()
	elapsed := time.Since(j.started)
	if j.state != jobRunning {
		elapsed = j.ended.Sub(j.started)
	}
	return jobInfo{
		id: j.id, command: j.command, kind: j.kind, label: j.label, detail: j.detail,
		name:    j.name,
		logPath: j.logPath,
		dir:     j.dir,
		state:   j.state, code: j.exitCode, ticks: j.ticks,
		started: j.started, elapsed: elapsed,
	}
}

// setName gives the job the short name it is called, and reports whether that
// changed anything.
//
// A NAME ARRIVES LATE OR NOT AT ALL, and both are ordinary. The namer is an
// errand on its own goroutine with its own deadline (jobname.go), so this is
// called — if it is called — some seconds after the job started, and the answer
// is dropped when it is empty or when it says what the job is already called.
// The report is what lets the caller publish only when there is news, which is
// the rule every other row on every other surface here is published under.
func (j *job) setName(name string) bool {
	if name == "" {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.name == name {
		return false
	}
	j.name = name
	return true
}

// countTick records one completed run of a watch's command and reports which
// run it was — the tick number a note can name.
func (j *job) countTick() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.ticks++
	return j.ticks
}

func (j *job) tickCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ticks
}

func (j *job) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == jobRunning
}

// requestKill claims the right to end this job, reporting whether the job was
// still alive to claim. The flag it sets is read by the watcher under this same
// lock, which is what makes "killed on purpose" and "died on its own" a
// decision made once rather than a race between two goroutines.
func (j *job) requestKill() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != jobRunning {
		return false
	}
	j.killRequested = true
	return true
}

// payOwed settles this job's debt, reporting whether there was one to settle.
//
// It is idempotent on purpose. The places that pay a debt — the two roads out of
// [jobRegistry.settleExit] and a person's stop ([Agent.cancelJob]) — each pay it
// unconditionally at the moment the ending is in front of the work, and a job can
// reach two of them: a person stops it, and the reaper settles the death they
// asked for a moment later. Whichever gets here first is the one that released
// anybody, and the second has nothing left to hand over.
func (j *job) payOwed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.owed {
		return false
	}
	j.owed = false
	return true
}

// stillOwed reports whether the work that started this command has yet to be
// told how it ended.
func (j *job) stillOwed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.owed
}

// markPersonStopped records that the kill about to be asked for is a person's.
// It is called BEFORE [job.requestKill]; see the field.
func (j *job) markPersonStopped() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.personStopped = true
}

// stoppedByPerson reports whether this job's requested death is a person's stop.
func (j *job) stoppedByPerson() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.personStopped
}

// settledKilled reports whether this job is final and died a death somebody
// ASKED for, which is the state that reports nothing of its own
// ([jobRegistry.settleExit]). It is the job's own account of itself rather than
// a caller inferring the same thing from a failed kill, which can fail for two
// different reasons.
func (j *job) settledKilled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == jobKilled
}

// settle makes a job final: the log is closed, the status fields stop moving,
// and done is released. It reports whether the death was REQUESTED, which is
// the one thing the caller needs to decide whether to say anything about it.
//
// It is called exactly once per job, by the single goroutine that owns the
// job's middle — the reaper for a process, the timer loop for a watch — which
// is what makes the close of done safe without a second flag guarding it.
func (j *job) settle(code int) bool {
	// The log file closes before the status is final, so a reader that sees a
	// finished job sees a complete file.
	j.sink.close()

	j.mu.Lock()
	requested := j.killRequested
	j.ended = time.Now()
	if requested {
		j.state = jobKilled
	} else {
		j.state = jobExited
		j.exitCode = code
	}
	j.mu.Unlock()

	// Signalled before any note: a killer waiting on done must not wait behind
	// a steering append it does not care about.
	close(j.done)
	return requested
}

// signal is how a kill reaches a job, whichever kind it is: a process group
// gets the signal, a watch gets its loop cancelled and settles itself on the
// way out.
//
// A watch ignores the DIFFERENCE between SIGTERM and SIGKILL on purpose. There
// is no process of its own to be polite to — the tick command, if one is
// running, is killed by its context — so the first cancel is already the whole
// of what the second one would ask for.
func (j *job) signal(sig syscall.Signal) {
	if j.stop != nil {
		j.stop()
		return
	}
	if j.cmd == nil || j.cmd.Process == nil {
		return
	}
	if sig == syscall.SIGKILL {
		_ = j.group.Kill()
		return
	}
	_ = j.group.Terminate()
}

// ── the registry ────────────────────────────────────────────────────────────

// jobRegistry is the session's background work. It lives for the session, not
// for a turn: a job started in one turn is still running, and still listable,
// three turns later, and Close is what ends it.
type jobRegistry struct {
	workspace string
	// place is the session folder, and it is what decides where the logs land
	// (landing.go). The zero Place keeps them under the workspace, which is the
	// legacy layout and the only thing a caller that has not adopted a folder
	// can mean.
	place Place
	// notify carries a completion note to the OWED lane (agent.go's
	// [Agent.enqueueJobNote]), which queues while a turn runs and starts one when
	// none does. It is a function rather than the Agent itself so the registry
	// has no idea what a turn is — it reports, and the lane decides whether
	// anybody has to answer.
	//
	// THE NOTE IS THE ENDING AS THIS FILE COMPOSED IT. The lane decides only
	// whether anybody has to answer; it does not reshape the news first
	// ([jobNote] states the law).
	notify func(string)
	// notifyWatch carries a watch's news, and the bool is WHICH KIND OF NEWS IT
	// IS: false for a periodic tick, true for the tick that ENDED the watch —
	// `until` matched, the output went quiet, the command failed its way out
	// (tools_watch.go's [jobRegistry.watchTick]).
	//
	// The split is here rather than at the lane's door because this is the only
	// place that knows the difference. A tick is telemetry with a complete log
	// behind it and must never interrupt a running turn; the FIRING is the answer
	// to the question the watch was started for, and it is the last thing that
	// watch will ever say — so it is owed exactly as a process job's exit is, and
	// agent.go's lane reads this bool to decide which of the two it queues.
	notifyWatch func(string, string, bool)
	// announce carries one job's row to the roster — the column beside the
	// conversation, where work this session started shows whatever door started
	// it (jobrow.go). It is a function for [jobRegistry.notify]'s reason exactly:
	// the registry reports what a job is doing and has no idea what a roster is,
	// and a caller with nothing to draw leaves it nil and pays nothing.
	//
	// IT IS ALSO THE NAMER'S DOOR. The function the agent hangs here
	// ([Agent.announceJobRow]) starts the cheap namer on first sight, on a
	// goroutine, so a new starter inherits the name by announcing and the job
	// never waits to be named (jobname.go).
	announce func(jobInfo)
	// paid releases whoever is WAITING on a command this registry took over from
	// a call that had not finished asking for it ([job.owed]).
	//
	// IT IS FOR THE ENDINGS THAT CARRY NO NOTE, and those only. An ending with a
	// note releases in the same locked step as its own append, which is the only
	// shape with no instant between the queue and the wake
	// ([userMessage.ending]); what is left for this hook is the deaths that
	// deliberately report nothing — a `jobs kill`, a shutdown — and the registry
	// with no lane to report into at all.
	//
	// It is a function for [jobRegistry.notify]'s reason exactly: the registry
	// reports what a job is doing and has no idea what a park is, and a caller
	// with nobody parked leaves it nil and pays nothing.
	paid func()

	mu   sync.Mutex
	jobs []*job
	// watches is the number of watch slots CLAIMED, not the number of watch
	// jobs in the slice. Counting the slice would leave a window between the
	// limit check and the append in which two concurrent starts both pass, and
	// tool calls in one batch run concurrently.
	watches int
	// closed says this session has quit and the registry is shut: [jobRegistry.shutdown]
	// sets it, and [jobRegistry.newJob] refuses afterwards.
	//
	// A REGISTRY WITH NO SUCH FLAG WAS HOW WORK OUTLIVED A SESSION. The round
	// that ends every job walks the slice below, so anything that had not put
	// itself in it yet was invisible to the quit — and then registered into a
	// session that had left, opening its log in a folder nothing would ever read
	// again (issue #381). The graph's own bounded stop is what catches the case
	// this closes behind ([TaskGraph.stopAll]); this is the door itself learning
	// to say no.
	closed bool
	epoch  uint64
}

func newJobRegistry(workspace string, place Place, notify func(string), watch ...func(string, string, bool)) *jobRegistry {
	registry := &jobRegistry{workspace: workspace, place: place, notify: notify}
	if len(watch) > 0 {
		registry.notifyWatch = watch[0]
	}
	return registry
}

// errSessionClosed is what BOTH doors of a shut registry say, and they say it
// in one voice on purpose: a caller cannot tell whether it was refused before
// its log was made or after, and has no reason to want to. Every caller answers
// it the same way — carry on without a log.
var errSessionClosed = errors.New("this session has closed; nothing new starts in it")

// newJob makes the shell every job shares — an id, a log file on disk, a sink
// over it — without deciding what will run in the middle.
//
// ITS REFUSAL IS THE CHEAP ONE AND NOT THE LOAD-BEARING ONE. This check cannot
// be what makes the law true, because the lock goes down again before the log
// is created and the job does not JOIN the registry until its caller adds it;
// a quit landing in that gap would walk the slice and finish before the job
// arrived. [jobRegistry.join] is where the law is actually kept. This is here
// so that the overwhelmingly common case — a session already closed when the
// call is made — costs nothing and creates no directory.
func (r *jobRegistry) newJob(command string, kind jobKind) (*job, error) {
	// NOTHING STARTS IN A SESSION THAT HAS LEFT. The refusal is here, ahead of
	// the directory, because the first thing this function does is CREATE one:
	// a job claimed during a quit put the jobs folder back the moment after it
	// was taken away. Every caller of this already answers an error by carrying
	// on without a log, which is the honest shape for work that is ending.
	r.mu.Lock()
	closed, epoch, workspace := r.closed, r.epoch, r.workspace
	r.mu.Unlock()
	if closed {
		return nil, errSessionClosed
	}

	directory := droppingsDir(r.place, workspace, droppingJobs)
	root, err := openJobRetentionDir(directory, true)
	if err != nil {
		return nil, fmt.Errorf("could not create the jobs directory: %w", err)
	}
	root.Close()

	id, logPath, logFile, err := r.claimJobLog(directory)
	if err != nil {
		return nil, err
	}
	started := &job{
		epoch:   epoch,
		id:      id,
		command: command,
		kind:    kind,
		dir:     workspace,
		started: time.Now(),
		logPath: logPath,
		done:    make(chan struct{}),
	}
	started.sink = newJobSink(logFile, logPath)
	// Retention maintenance (issue #1601) runs when the sink closes — the
	// maintenance also runs at claim and startup, never per Write.
	started.sink.finishRetention = func() { jobRetentionFinish(started.sink) }
	return started, nil
}

// claimJobLog reserves the persistent id, creates the spool, and holds its
// writer lease before publishing the marker, under one directory lock.
func (r *jobRegistry) claimJobLog(directory string) (int, string, *os.File, error) {
	return jobRetentionClaim(directory)
}

// join is the ONE door into the registry's slice, and the place the closed
// session's law is actually kept.
//
// THE CHECK AND THE APPEND HAPPEN UNDER ONE HOLD OF THE LOCK. That is the whole
// reason this is a function. [jobRegistry.newJob] also refuses a closed
// registry, but it must let the lock go to create the log, and a job does not
// arrive here until its caller has filled it in — so a quit landing in that gap
// sets `closed`, walks the slice, and returns before the job appends itself.
// The job would then be running in a session that had left, behind the one
// round that would ever have killed it, which is the defect the flag was added
// to close and not a smaller one (issue #381).
//
// A JOB THAT CANNOT JOIN TAKES ITS LOG BACK OUT OF THE FOLDER. Nothing will
// ever read it — no row, no id anybody was given, no round that will settle
// it — and leaving the file behind would put the jobs directory back a moment
// after the quit took it away, which is the visible half of the same bug.
func (r *jobRegistry) join(started *job) error {
	r.mu.Lock()
	if r.closed || started.epoch != r.epoch {
		r.mu.Unlock()
		started.sink.close()
		if started.logPath != "" {
			_ = jobRetentionDiscard(started.logPath)
		}
		return errSessionClosed
	}
	r.jobs = append(r.jobs, started)
	r.mu.Unlock()
	return nil
}

// add puts a job in the registry and publishes its row. It is called once the
// job is actually running — a list between the id reservation and this append
// shows one fewer job, which is the honest answer for work that does not exist
// yet. A refused job gets no row, because there is no job to have one.
func (r *jobRegistry) add(started *job) error {
	if err := r.join(started); err != nil {
		return err
	}
	r.announceRow(started)
	return nil
}

// announceRow publishes one job's row, and it is the ONE PLACE that decides
// which jobs have one.
//
// A TASK NODE DOES NOT. It is in this registry for everything around it — one id
// space, one log, one kill, one death at Close ([jobKindTask]) — and it already
// has a roster row of its own, published by the graph that runs it. A second row
// here would draw the same piece of work twice and count it twice.
func (r *jobRegistry) announceRow(one *job) {
	if r.announce == nil || one.kind == jobKindTask {
		return
	}
	r.announce(one.info())
}

// settled makes one job final and publishes the row's ending in the same beat.
//
// It exists so that the three places a job can end — a process reaped
// ([jobRegistry.settleExit]), a goroutine finishing ([jobRegistry.finish]), a
// watch's loop returning (tools_watch.go) — cannot disagree about whether the
// roster was told. It reports what [job.settle] reports: whether the death was
// one this session ASKED for.
func (r *jobRegistry) settled(one *job, code int) bool {
	requested := one.settle(code)
	r.announceRow(one)
	return requested
}

// start launches one command in the background and returns as soon as the
// process exists.
//
// The context of the tool call is deliberately NOT passed to the process: a
// turn's context is cancelled when the turn ends, and a background job whose
// whole purpose is to outlive the turn would be killed by the very act of
// answering the person. The job's lifetime is the session's, and Close is the
// only thing that ends it early.
func (r *jobRegistry) start(command string) (*job, error) {
	started, err := r.newJob(command, jobKindBash)
	if err != nil {
		return nil, err
	}

	// THE SAME STREAMING SHELL A FOREGROUND CALL GETS (internal/exec/bare's
	// streaming.go). A background job is the one place where block-buffered
	// output does the most damage — nobody is watching the pipe, so a log that
	// stays empty until exit is a job that looks dead for as long as it runs —
	// and this used to be a hand-copied three-line shell choice with no
	// buffering fix in it at all.
	shell, shellArgs := bare.StreamingShell(command)
	process := exec.Command(shell, shellArgs...)
	process.Dir = started.dir
	process.Env = bare.StreamingEnv()
	// Setsid puts the job and everything it spawns in one process group, so a
	// kill reaches the whole tree — the leader of a new session leads its own
	// group, so every `kill -pgid` here works exactly as it did under Setpgid.
	// A dev server that forks a compiler must not survive the kill of its
	// parent. AND IT TAKES THE TERMINAL AWAY: a job has no controlling tty, so
	// a child that opens /dev/tty — a CLI that is itself a screen, a prompt
	// that insists on the keyboard — is refused instead of painting over the
	// person's frame. That was measured, not imagined: two review CLIs run as
	// jobs drew their own output across the top of a running conversation.
	processgroup.ConfigureDetached(process)
	process.Stdout = started.sink
	process.Stderr = started.sink

	if err := process.Start(); err != nil {
		started.sink.close()
		return nil, fmt.Errorf("could not start the command: %w", err)
	}
	started.cmd = process
	started.group = processgroup.CaptureGroup(process.Process.Pid)
	// A JOB REFUSED AT THE DOOR TAKES ITS PROCESS WITH IT. This one is already
	// forked, so simply returning the error would leave exactly the orphan the
	// refusal exists to prevent — a process running for a session that has
	// left, with no row, no id and no round that will ever kill it.
	//
	// The kill reaches the whole GROUP, not just the shell, because a shell
	// that has already forked a compiler would otherwise leave the compiler
	// behind. Start has just returned, so there is a process to name: on unix
	// that is `kill(-pid)` against the session this job leads, and on Windows
	// it is `taskkill /T` against the process group it was given.
	if err := r.add(started); err != nil {
		_ = started.group.Kill()
		return nil, err
	}

	go r.reap(started)
	// AND THE SUBTREE GETS ITS OWN BOUND. A bash job is a process group with a
	// tree under it, and nothing else bounds what that tree may burn; the watcher
	// cuts it when it passes its ceiling and tells the run why (jobbound.go).
	go r.watchSubtreeBound(started)
	return started, nil
}

// startTask registers one task node as a job so `jobs list` shows it and
// `jobs kill` ends it. The node's own goroutine runs it (task_run.go) and
// settles the job when it lands.
//
// It does NOT report its own end on the steering lane. A node's completion note
// carries the report, the changed files and the merge outcome, and it is sent
// by the executor (see [Agent.reportTaskNode]); a second line here saying "job
// 3 exited 0" would be the registry narrating what the node just explained.
func (r *jobRegistry) startTask(id uint64, title string, cancel context.CancelFunc, explicit ...func()) (*job, error) {
	started, err := r.newJob(title, jobKindTask)
	if err != nil {
		return nil, err
	}
	started.label = fmt.Sprintf("task %d", id)
	started.detail = title
	started.stop = cancel
	if len(explicit) > 0 {
		started.explicitStop = explicit[0]
	}
	if err := r.add(started); err != nil {
		return nil, err
	}
	return started, nil
}

// startRender registers one provider generation — a video render, a music
// compose — as a job and hands back the job and the context its provider call
// must run under.
//
// The context is the BACKGROUND one and never the turn's, for [jobRegistry.start]'s
// reason exactly: a turn's context is cancelled when the turn ends, and a render
// whose whole purpose is to outlive the turn would be killed by the act of
// answering the person. Its cancel is the job's stop function, so `jobs kill`
// and Close both reach it through [job.signal].
//
// What runs in the middle is the caller's (tools_video.go, tools_music.go), as
// a watch's loop is tools_watch.go's: this registry owns the id, the log, the
// row and the kill.
func (r *jobRegistry) startRender(label, prompt string) (*job, context.Context, error) {
	started, err := r.newJob(prompt, jobKindRender)
	if err != nil {
		return nil, nil, err
	}
	started.label = label
	started.detail = prompt

	ctx, cancel := context.WithCancel(context.Background())
	started.stop = cancel
	if err := r.add(started); err != nil {
		// The context is cut rather than dropped: the caller is about to be
		// handed an error instead of it, and a live cancel nobody holds is a
		// leak vet will name.
		cancel()
		return nil, nil, err
	}
	return started, ctx, nil
}

// owedRunning reports whether any command this registry took over from a call
// that was waiting for it has yet to have its ending handed over. It is what
// holds a task worker's next question back until the answer is in front of it
// ([Agent.parkOnOwedJob]).
//
// THE TWO LOCKS ARE NEVER HELD AT ONCE. The slice is snapshotted under the
// registry's ([jobRegistry.all]) and each job is then asked under its own, which
// is the discipline every other walk of this list keeps.
func (r *jobRegistry) owedRunning() bool {
	if r == nil {
		return false
	}
	for _, candidate := range r.all() {
		if candidate.stillOwed() {
			return true
		}
	}
	return false
}

// payDebt hands one job's debt over and releases whoever was parked on it. It is
// called where the ending becomes READABLE and never where it becomes true; see
// [jobRegistry.settleExit] for the difference and why it is the whole point.
//
// NO LOCK OF THIS REGISTRY'S OR OF THE JOB'S IS HELD ACROSS THE CALLBACK. That is
// the law jobrow.go states for `announce`, and it holds here for its reason: the
// hook takes the agent's lock, and a registry lock held across it would put the
// two lock orders together.
func (r *jobRegistry) payDebt(one *job) {
	if !one.payOwed() {
		return
	}
	r.releaseParked()
}

// releaseParked is the hook itself, for the roads that have already cleared the
// debt and only owe the release.
func (r *jobRegistry) releaseParked() {
	if r.paid == nil {
		return
	}
	r.paid()
}

// finish settles a job whose middle was a goroutine rather than a process, and
// drops its one note on the steering queue.
//
// It is [jobRegistry.reap] for the kinds that have nothing to wait on: the same
// two rules hold, which are that a death this session ASKED for says nothing —
// the caller already knows — and that this kind's note is a sentence, not bash
// output.
func (r *jobRegistry) finish(done *job, code int, note string) {
	if requested := r.settled(done, code); requested {
		return
	}
	if r.notify == nil {
		return
	}
	if done.sink.notice() != "" {
		if note == "" {
			note = fmt.Sprintf("job %d finished", done.id)
		}
		note += "\n\n[job " + strconv.Itoa(done.id) + " · " + done.sink.logFooter(done.logPath) + "]"
	}
	if note == "" {
		return
	}
	r.notify(note)
}

// adoption is what the road taking a running command over knows about it. Both
// facts belong to the caller because only the caller knows WHICH road this is:
// a clock, a timeout, a person's key or a person's steer all take the same
// process into the same registry and mean different things by it.
type adoption struct {
	// quiet leaves the person-visible row to the caller, to be published after
	// the claim: the process and the job id become one fact under bare's adoption
	// lock, and the row goes out once that lock is released.
	quiet bool
	// owed says the call that started this command is still WAITING for it, so
	// the work it belongs to may not be asked for its next step until the ending
	// has been handed over ([job.owed]).
	owed bool
}

// adopt takes over a foreground bash process that is ALREADY RUNNING and makes
// it a job (promote.go states the whole design).
//
// It is [jobRegistry.start] with the fork already done: same job shell around
// it, same log file, same row, same kill, same death at Close. Two things are
// different and both are consequences of the process being somebody else's
// first.
//
// The OUTPUT is redirected rather than captured from the beginning: bare has
// been accumulating it into a rolling tail, and [bare.BashCall.Attach] replays
// that tail into this job's sink before pointing the stream at it. So the log
// opens with what the person was already watching, and continues without a gap.
//
// The WAIT is not this registry's, because Go permits exactly one per command
// and bare's is already in flight. The exit code arrives on a channel instead,
// and [jobRegistry.settleExit] does everything it would have done after a Wait
// of its own. THIS IS STILL THE ONLY REAPER: nothing in bare decides a job is
// over, notes an exit, or writes a status word.
//
// WHAT THE CALLER KNOWS AND THIS DOES NOT is [adoption], the two facts about the
// road the takeover came down.
func (r *jobRegistry) adopt(taken *bare.BashCall, how adoption) (*job, error) {
	started, err := r.newJob(taken.Command(), jobKindBash)
	if err != nil {
		return nil, err
	}
	// The process, its group and its pid are unchanged by the adoption — bash
	// started it with Setpgid, so a kill still reaches the whole tree exactly as
	// it does for a job this registry forked itself.
	started.cmd = taken.Process()
	started.group = processgroup.CaptureGroup(started.cmd.Process.Pid)
	// The provenance is written before the job joins the registry, which is the
	// last instant this goroutine is the only one that can see it.
	started.owed = how.owed
	// THE JOIN COMES BEFORE THE ATTACH, so that a refused adoption never points
	// bash's streams at a sink this registry has just closed and a log it has
	// just removed. A quiet adoption takes the same door — it only declines the
	// ROW, never the check.
	join := r.add
	if how.quiet {
		join = r.join
	}
	if err := join(started); err != nil {
		return nil, err
	}
	taken.Attach(started.sink)

	// The receive happens INSIDE the goroutine: written as an argument it would
	// be evaluated here, and the adoption would block until the process exited.
	go func() { r.settleExit(started, <-taken.Exit()) }()
	// The adopted process is a bash subtree like any other and gets the same
	// bound ([jobRegistry.watchSubtreeBound]).
	go r.watchSubtreeBound(started)
	return started, nil
}

// reap waits on one process and, unless the death was asked for, drops a note
// on the steering queue.
func (r *jobRegistry) reap(watched *job) {
	r.settleExit(watched, waitExitCode(watched.cmd.Wait()))
}

// settleExit is what happens the moment a process's exit code is known, however
// it became known: the job is made final, and unless the death was asked for a
// note goes on the steering queue.
func (r *jobRegistry) settleExit(watched *job, code int) {
	requested := r.settled(watched, code)

	if requested {
		// A DEATH THIS SESSION ASKED FOR RELEASES THE WORK AT ONCE. The registry's
		// own rule is that such a job reports nothing — the caller already knows,
		// and at shutdown the journal it would be written to is closing — so there
		// is no news to wait for and nothing to be gained by holding a parked
		// worker until its bound runs out.
		//
		// A PERSON'S STOP OWNS THE ENDING IT ASKED FOR, and it is the one
		// exception. It is the only requested death with a note coming
		// ([Agent.cancelJob]), and releasing here would release from INSIDE that
		// person's kill, while their line was still unwritten — one of the two
		// interleavings that let a worker wake to an empty queue. So this road
		// steps over it and the stop speaks for itself.
		if !watched.stoppedByPerson() {
			r.payDebt(watched)
		}
		return
	}
	// AND THE DEBT IS CLEARED BEFORE THE NOTE, WHICH IS THE OPPOSITE OF WHERE IT
	// LOOKS LIKE IT BELONGS. The release no longer happens here at all: an ending
	// is marked as one and released in the same locked step as its append
	// ([userMessage.ending]), which is the only shape with no instant between the
	// two. What is left for this road is the debt itself, and it has to be gone
	// BEFORE the note is queued — a note that released first would wake the park,
	// which would read itself still owed, and park again on a generation nothing
	// will ever close.
	//
	// The answer is kept because a registry with no notify lane still has to
	// release the park itself below.
	owed := watched.payOwed()
	if r.notify != nil {
		note := fmt.Sprintf("job %d exited %d", watched.id, code)
		if last := watched.sink.lastNonEmptyLine(); last != "" {
			note += ": " + clip(last, jobExitNoteLimit)
		}
		// AND THE NOTE SAYS, IN ITS OWN FRAME, THAT IT IS NEWS. It rides the
		// steering lane and arrives as a user-role message, which read cold is
		// indistinguishable from somebody typing "job 3 exited 0" — and a model
		// reading it that way runs the command again to see what they meant.
		note += "\n" + jobExitNewsRule
		// AND THE OUTPUT COMES WITH IT. A watch's note is its own sentence and
		// needs none of this; a bash job's ending is the moment its output finally
		// means something, and a note that withheld it would be an invitation to
		// make one more call for what the note was already about.
		if watched.kind == jobKindBash {
			tail := watched.sink.tail(jobExitTailLines)
			if strings.TrimSpace(tail) != "" || watched.sink.notice() != "" || watched.sink.retentionLost() {
				if strings.TrimSpace(tail) != "" {
					note += "\n\n" + tail
				}
				note += "\n\n[job " + strconv.Itoa(watched.id) + " · last " +
					strconv.Itoa(jobExitTailLines) + " lines · " + watched.sink.logFooter(watched.logPath) + "]"
			}
		}
		r.notify(note)
		return
	}
	// A REGISTRY WITH NO LANE TO REPORT INTO HAS NO NOTE FOR THE RELEASE TO RIDE
	// WITH, so it is made here. Nothing is coming, and a worker held until its
	// bound over an ending nobody will ever speak is the wait costing what it was
	// written to save.
	if owed {
		r.releaseParked()
	}
}

func (r *jobRegistry) all() []*job {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := make([]*job, len(r.jobs))
	copy(snapshot, r.jobs)
	return snapshot
}

func (r *jobRegistry) find(id int) *job {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, candidate := range r.jobs {
		if candidate.id == id {
			return candidate
		}
	}
	return nil
}

// kill ends one job: SIGTERM to the process group, SIGKILL after the grace.
//
// IT TAKES THE CALL'S CONTEXT so that its two graces end when the turn does
// ([waitDoneUnder]). The signals are sent either way — a job the person asked to
// end is ended whatever else is happening — and what cancellation buys is the
// four seconds this used to spend watching for an exit nobody was waiting for
// any more.
func (r *jobRegistry) kill(ctx context.Context, id int) (string, bool) {
	target := r.find(id)
	if target == nil {
		return fmt.Sprintf("No job %d.", id), true
	}
	if !target.requestKill() {
		info := target.info()
		return fmt.Sprintf("Job %d already %s.", id, statusText(info)), true
	}
	if target.explicitStop != nil {
		target.explicitStop()
	}
	target.signal(syscall.SIGTERM)
	if !waitDoneUnder(ctx, target.done, jobTermGrace) {
		target.signal(syscall.SIGKILL)
		// The second wait is bounded too: a process wedged in an
		// uninterruptible sleep is not something a tool call can fix, and
		// hanging the turn on it would be worse than reporting the SIGKILL.
		waitDoneUnder(ctx, target.done, jobTermGrace)
	}
	// A watch is stopped, not killed: there was no process of its own to end,
	// and "stopped" is the word its list row and its notes already use.
	if target.kind == jobKindWatch {
		return fmt.Sprintf("watch %s (job %d) stopped", target.label, id), false
	}
	// A task is STOPPED and its branch is KEPT. The words matter: nothing the
	// node wrote is thrown away by ending it, and the completion note that
	// follows names the branch the work is on.
	if target.kind == jobKindTask {
		return fmt.Sprintf("%s (job %d) stopped; its branch is kept", target.label, id), false
	}
	// A render is STOPPED and nothing was saved, which is the whole of what the
	// model needs to know: no file landed, and no note about this job is
	// coming. The label is the noun — "video", "music" — so the sentence names
	// what was lost without this switch growing a case per modality.
	if target.kind == jobKindRender {
		return fmt.Sprintf("%s (job %d) stopped; no %s was saved", target.label, id, target.label), false
	}
	return fmt.Sprintf("job %d killed", id), false
}

// shutdown ends every running job at Close: one SIGTERM round, ONE shared
// grace for all of them, then SIGKILL for whatever is left.
//
// The grace is shared rather than per-job because it is a person's quit: ten
// running jobs must not mean twenty seconds. Nothing here self-reports — every
// kill is requested — so no note can land on a queue whose journal is about to
// close.
func (r *jobRegistry) shutdown(grace time.Duration) {
	// THE DOOR CLOSES BEFORE THE ROUND WALKS THE ROOM, so that nothing can join
	// the list behind the walk (see the `closed` field).
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	var claimed []*job
	for _, candidate := range r.all() {
		if candidate.requestKill() {
			claimed = append(claimed, candidate)
		}
	}
	if len(claimed) == 0 {
		return
	}
	for _, target := range claimed {
		target.signal(syscall.SIGTERM)
	}
	deadline := time.Now().Add(grace)
	for _, target := range claimed {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		waitDone(target.done, remaining)
	}
	// SIGKILL and do not wait: the group is gone, the watcher goroutine will
	// reap it on its own, and Close owes the person a prompt, not a funeral.
	for _, target := range claimed {
		if target.running() {
			target.signal(syscall.SIGKILL)
		}
	}
}

// ── rendering ───────────────────────────────────────────────────────────────

func statusText(info jobInfo) string {
	switch info.state {
	case jobExited:
		// A watch has no exit code of its own to report — it ends because its
		// `until` matched, or because its command kept failing, and both of
		// those arrived as a note. "stopped" is the whole status.
		if info.kind == jobKindWatch {
			return "stopped"
		}
		// Nor has a task: its outcome is a STATE (done, failed) that reached
		// the model in its own note, and an exit code here would be a second,
		// dumber account of the same ending.
		if info.kind == jobKindTask {
			return "finished"
		}
		// Nor has a render: it either landed a file or failed, and both of
		// those already reached the model as a note.
		if info.kind == jobKindRender {
			return "finished"
		}
		return fmt.Sprintf("exited(%d)", info.code)
	case jobKilled:
		return "killed"
	default:
		return "running"
	}
}

// formatElapsed keeps a job's age to one glance: milliseconds under a second,
// one decimal of seconds above it.
func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Second {
		return fmt.Sprintf("%dms", elapsed.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", elapsed.Seconds())
}

func (r *jobRegistry) list() string {
	jobs := r.all()
	if len(jobs) == 0 {
		return "No background jobs."
	}
	var rendered strings.Builder
	for index, listed := range jobs {
		if index > 0 {
			rendered.WriteString("\n")
		}
		info := listed.info()
		// A watch's row leads with its KIND and its name, because those are what
		// the model will use next: "watch app" is the thing it started, and the
		// terms after it ("every 10s on change") are the answer to "why haven't I
		// heard anything" without a second call.
		// A task's row leads with the node, because that is what the model asked
		// for and what it will kill by: the id it was given back, the title it
		// wrote, and how long the node has been working.
		// A render's row is a task's row for the same reason: the label says
		// what kind of thing is running, and the detail is the prompt it was
		// given, which is how a person picks one of three renders out of a list.
		if info.kind == jobKindTask || info.kind == jobKindRender {
			fmt.Fprintf(&rendered, "job %d · %s · %s · %s · %s",
				info.id, info.label, statusText(info), formatElapsed(info.elapsed),
				clip(firstLine(info.detail), hintLimit))
			continue
		}
		if info.kind == jobKindWatch {
			fmt.Fprintf(&rendered, "job %d · watch %s · %s · %s · %d ticks · %s · %s",
				info.id, info.label, statusText(info), formatElapsed(info.elapsed),
				info.ticks, info.detail, clip(firstLine(info.command), hintLimit))
			continue
		}
		fmt.Fprintf(&rendered, "job %d · %s · %s · %s",
			info.id, statusText(info), formatElapsed(info.elapsed),
			clip(firstLine(info.command), hintLimit))
	}
	return rendered.String()
}

// output renders one job's recent lines with a footer naming the log. The
// footer is the point of the whole design: it tells the model where the rest
// is, so the answer to "I need more" is a read call and not a bigger tail —
// and while the spool has discarded anything, the footer says that instead of
// calling what remains full.
func (r *jobRegistry) output(id, lines int) (string, bool) {
	target := r.find(id)
	if target == nil {
		return fmt.Sprintf("No job %d.", id), true
	}
	info := target.info()
	tail := target.sink.tail(lines)
	if strings.TrimSpace(tail) == "" {
		tail = "(no output)"
	}
	ending := target.sink.logFooter(target.logPath)
	return fmt.Sprintf("%s\n\n[job %d · %s · showing last %d lines · %s]",
		tail, id, statusText(info), lines, ending), false
}

// ── the output sink: ring in memory, everything on disk ─────────────────────

// jobSink drains stdout and stderr into a bounded tail and two disk chunks.
// The live inode is never replaced, preserving both the job ID reservation and
// the writer lease held by retention. Disk failures stop spooling, not draining.
type jobSink struct {
	mu              sync.Mutex
	ring            []byte
	file            *os.File
	closed          bool
	base            string
	chunkBytes      int64
	spoolBytes      int64
	spoolBroken     bool
	brokenText      string
	noticeText      string
	backupInfo      os.FileInfo
	hook            spoolHook
	finishRetention func()
	retentionText   string
}

// spoolHook is the seam the tests inject spool failures through. It replaces
// the append into the live chunk, so a test can fail one chosen write or
// return a short count deterministically. Production leaves it nil.
type spoolHook func(data []byte) (int, error)

// newJobSink opens a sink over one spool file at the production limits.
func newJobSink(file *os.File, base string) *jobSink {
	return &jobSink{
		file:       file,
		base:       base,
		chunkBytes: jobSpoolChunkBytes,
	}
}

// Write always reports success. A write error here is a full disk or a removed
// workspace, and the honest response to that is to keep the job running with
// the in-memory tail intact: returning the error would make Go's copier close
// the pipe, and the job would die of a logging problem. The failure is
// recorded once — [jobSink.notice] carries it — spool retries stop, and the
// drain and the ring go on.
func (s *jobSink) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil && !s.closed {
		s.spoolLocked(data)
	}
	// A single Write may be megabytes, and no temporary grows to match it:
	// only its newest jobRingBytes reaches the ring.
	if len(data) > jobRingBytes {
		start := len(data) - jobRingBytes
		// Do not cut a rune in half: a tail starting mid-character renders as
		// a replacement glyph in the model's context for no reason.
		for start < len(data) && !utf8RuneStart(data[start]) {
			start++
		}
		s.ring = append(s.ring[:0], data[start:]...)
	} else {
		s.ring = append(s.ring, data...)
	}
	// Trimming at twice the cap rather than at the cap makes this amortized:
	// trimming on every write would copy the whole ring per line of output.
	if len(s.ring) > jobRingBytes*2 {
		s.trimLocked()
	}
	return len(data), nil
}

// spoolLocked appends data to the bounded spool, rotating the live chunk when
// it fills. Input is sliced without copying; each full chunk is copied once
// during rotation with a fixed buffer, regardless of the size of a Write.
func (s *jobSink) spoolLocked(data []byte) {
	if s.spoolBroken || s.base == "" {
		return
	}
	for len(data) > 0 {
		if s.spoolBytes >= s.chunkBytes {
			if !s.rotateLocked() {
				return
			}
		}
		room := s.chunkBytes - s.spoolBytes
		if int64(len(data)) < room {
			room = int64(len(data))
		}
		piece := data[:room]
		written, err := s.spoolWriteLocked(piece)
		if err != nil {
			s.breakSpoolLocked(err)
			return
		}
		if written < len(piece) {
			s.breakSpoolLocked(fmt.Errorf("short write: %d of %d bytes", written, len(piece)))
			return
		}
		s.spoolBytes += int64(written)
		data = data[room:]
	}
}

// spoolWriteLocked is the one door the bytes leave through. hook is nil in
// production and set by tests to fail a chosen write deterministically.
func (s *jobSink) spoolWriteLocked(piece []byte) (int, error) {
	if s.hook != nil {
		return s.hook(piece)
	}
	return s.file.Write(piece)
}

// rotateLocked preserves the live inode and copies at most one chunk. The
// previous backup is removed before the copy, so even during rotation there
// are at most two chunks. A backup is only removed if this sink created it.
func (s *jobSink) rotateLocked() bool {
	root, err := os.OpenRoot(filepath.Dir(s.base))
	if err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	defer root.Close()
	name := filepath.Base(s.base)
	info, err := root.Lstat(name)
	owned, ownErr := s.file.Stat()
	if err != nil || ownErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, owned) {
		s.breakSpoolLocked(fmt.Errorf("live log identity changed"))
		return false
	}
	backup := name + ".1"
	if s.backupInfo != nil {
		info, err := root.Lstat(backup)
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, s.backupInfo) {
			s.breakSpoolLocked(fmt.Errorf("retained log identity changed"))
			return false
		}
		if err := root.Remove(backup); err != nil {
			s.breakSpoolLocked(err)
			return false
		}
		s.backupInfo = nil
		s.noticeText = "log truncated: only the most recent " + spoolSizeText(s.chunkBytes*jobSpoolChunks) + " is kept"
	}
	file, err := root.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	s.backupInfo, err = file.Stat()
	if err != nil {
		_ = file.Close()
		s.breakSpoolLocked(err)
		return false
	}
	n, copyErr := io.CopyBuffer(file, io.NewSectionReader(s.file, 0, s.spoolBytes), make([]byte, 32<<10))
	closeErr := file.Close()
	if copyErr == nil && n != s.spoolBytes {
		copyErr = io.ErrShortWrite
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	if err := s.file.Truncate(0); err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		s.breakSpoolLocked(err)
		return false
	}
	s.spoolBytes = 0
	return true
}

// breakSpoolLocked leaves the fd and its writer lease held until close. A
// failed spool must not look like an abandoned log while its job is running.
func (s *jobSink) breakSpoolLocked(err error) {
	if s.spoolBroken {
		return
	}
	s.spoolBroken = true
	s.brokenText = "job log stopped: " + err.Error()
}

func (s *jobSink) noticeLocked() string {
	parts := []string{}
	if s.retentionText != "" {
		parts = append(parts, s.retentionText)
	}
	if s.brokenText != "" {
		parts = append(parts, s.brokenText)
	}
	if s.noticeText != "" {
		parts = append(parts, s.noticeText)
	} else if s.backupInfo != nil {
		parts = append(parts, "log rotated")
	}
	return strings.Join(parts, "; ")
}

// notice records lost history and logging failures without stopping the job.
func (s *jobSink) notice() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.noticeLocked()
}

// logFooter names both chunks from the first rotation, even before any bytes
// are discarded. Calling only the live chunk the full log would be false.
func (s *jobSink) logFooter(path string) string {
	if s.retentionLost() {
		ending := "log evicted by the retention budget · the lines above are the in-memory tail"
		if notice := s.notice(); notice != "" {
			ending = notice + " · " + ending
		}
		return ending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notice := s.noticeLocked()
	if notice == "" {
		return "full log: " + path
	}
	if s.backupInfo != nil {
		return notice + " · log files: " + path + ".1, " + path
	}
	return notice + " · log file: " + path
}

// A previous successful read does not prove a later retained log still exists.
func (s *jobSink) retentionLost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base == "" || !s.closed {
		return false
	}
	_, err := os.Lstat(s.base)
	return os.IsNotExist(err)
}

// spoolSizeText keeps the notice's figures readable: megabytes at the
// production limits, bytes at the ones the tests set.
func spoolSizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

func (s *jobSink) trimLocked() {
	if len(s.ring) <= jobRingBytes {
		return
	}
	start := len(s.ring) - jobRingBytes
	// Do not cut a rune in half: a tail starting mid-character renders as a
	// replacement glyph in the model's context for no reason.
	for start < len(s.ring) && !utf8RuneStart(s.ring[start]) {
		start++
	}
	s.ring = append([]byte(nil), s.ring[start:]...)
}

// close settles the spool. A close that fails is surfaced the same way a
// failed write is — through [jobSink.notice] — and never turned into an error
// the job's ending would have to carry.
func (s *jobSink) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			s.breakSpoolLocked(fmt.Errorf("close: %w", err))
		}
	}
	s.closed = true
	finish := s.finishRetention
	s.finishRetention = nil
	s.mu.Unlock()
	// Maintenance takes the directory lock only after releasing the writer
	// lease and sink mutex, so it cannot deadlock with another job's claim.
	if finish != nil {
		finish()
	}
}

// tail returns the last n lines of the ring.
func (s *jobSink) tail(n int) string {
	if n <= 0 {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text := strings.TrimRight(string(s.ring), "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// text is everything the ring is holding, verbatim. It is what a caller bounds
// for itself — the sentence a promoted call answers with (promote.go), the tail
// on a completion note — rather than a second opinion about how much of a job's
// output anybody may see.
func (s *jobSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.ring)
}

// lastNonEmptyLine is the one line the completion note quotes. Blank lines are
// skipped because a job whose last write was a newline still has something to
// say about how it went.
func (s *jobSink) lastNonEmptyLine() string {
	// A JOB WITH NO SINK HAS SAID NOTHING, and this reads as exactly that. The
	// footer walks every live row (jobfooter.go's runningFooter), and a row
	// built without a sink used to take this call as a nil-pointer panic that
	// the guard then swallowed — eighteen recovered faults per test run, each
	// one a tool result silently losing its footer.
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := strings.Split(string(s.ring), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if trimmed := strings.TrimSpace(lines[index]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ── process plumbing ────────────────────────────────────────────────────────

// jobShell mirrors bare's shell preference (/bin/bash, bash on PATH, sh). It is
// a copy rather than a call because bare's is unexported and this slice wraps
// that package rather than editing it; the order is three lines and it is the
// same three lines.
//
// IT IS NOT FOR ANYTHING WHOSE OUTPUT SOMEBODY READS WHILE IT RUNS. A bash job
// goes through [bare.StreamingShell] instead, which is this choice plus the
// line-buffering that keeps a long command's log from being empty until it
// exits. What is left on this one is a watch's tick and a standing order's step
// — commands that are short by construction and read only after they end.
func jobShell() (string, []string) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"-c"}
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		return bash, []string{"-c"}
	}
	return "sh", []string{"-c"}
}

// waitExitCode extracts an exit code from cmd.Wait's error, -1 when the process
// was signalled or the code is unavailable.
func waitExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// waitDone waits for a closed channel, reporting whether it closed in time.
func waitDone(done <-chan struct{}, within time.Duration) bool {
	return waitDoneUnder(context.Background(), done, within)
}

// waitDoneUnder is [waitDone] WITH THE CALLER'S CANCELLATION ON IT, and it is
// the first rung of the second stage a stop now has (agent.go's
// [Agent.Abandon]).
//
// THE GRACES ARE THE POINT. A kill spends two seconds waiting out a SIGTERM and
// two more waiting out the SIGKILL behind it, and until this arm existed it
// spent them whatever had happened outside — so a person who stopped the turn
// stood through four seconds of a wait that had already been made pointless by
// the cancellation. A grace is a courtesy extended to a process that might still
// exit cleanly; it is not a promise to keep waiting after the reason for waiting
// is gone.
//
// A CANCELLED WAIT REPORTS false, which is the same answer the timer gives, and
// it is the honest one: the caller asked whether the process ended inside the
// window and it did not. The escalation behind it — SIGKILL after the SIGTERM —
// is exactly what a cut grace should hand to, and the reaper finishes behind us.
func waitDoneUnder(ctx context.Context, done <-chan struct{}, within time.Duration) bool {
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

// jobsArguments is the jobs tool's wire arguments. id and tail are pointers so
// "absent" and "zero" stay different answers: tail:0 is a request for nothing,
// while an absent tail is a request for the default.
type jobsArguments struct {
	Action string `json:"action"`
	ID     *int   `json:"id"`
	Tail   *int   `json:"tail"`
}

func parseJobsArguments(args json.RawMessage) (jobsArguments, error) {
	var parsed jobsArguments
	if err := decodeToolArguments(args, &parsed); err != nil {
		return parsed, err
	}
	return parsed, nil
}
