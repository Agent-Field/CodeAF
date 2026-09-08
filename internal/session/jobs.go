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
//   - RING + DISK, not one or the other. Everything the job writes goes to a
//     file, so the whole log is addressable by the read tool — paged, offset,
//     grepped, the same way any other file is. Only the last 64KB is kept in
//     memory, and only that tail is ever handed back through a tool result. A
//     watcher that has printed 400MB must not be able to put 400MB in front of
//     the model, and a watcher that printed the one line that matters must not
//     lose it because nobody was polling. Disk answers the second, the ring
//     answers the first.
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
//     step. The ending carries its output tail and names the whole log; anything
//     older remains here behind `jobs output` and on disk. A completion is news,
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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

const (
	// jobRingBytes is the in-memory tail. 64KB is a few hundred lines of a
	// build log — enough that `jobs output` after a failure shows the failure,
	// and small enough that a hundred jobs cost megabytes, not gigabytes.
	jobRingBytes = 64 << 10

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
	// jobKindHand is one hand of a fork (fork.go): a copy of the caller's own
	// mind, working a declared slice of the same working copy.
	//
	// IT IS HERE FOR THE REASON A RENDER IS, and the reason is the law this
	// file opens with: A HAND IS A STREAM, NOT A BARRIER. `fork` used to
	// block its caller's tool call until the SLOWEST hand came home — measured
	// at thirty-nine minutes on a fork whose first hand was finished in ninety
	// seconds, with that finished work sitting unbuilt and unmeasured for
	// thirty-seven of them. So a hand is registered like every other stream this
	// session starts: one id space, one log file, one row in `jobs list`, one
	// kill, one death at Close. Its middle is a child agent's turn, its kill is
	// that turn's context being cancelled — the same stop function a watch has —
	// and its ending is a note on the steering lane carrying its whole report.
	jobKindHand
)

// job is one background command.
//
// The mutex guards the mutable status fields only. It is deliberately NOT the
// Agent's lock: a watcher goroutine reaping a process must never contend with
// the lock a turn holds, least of all the one Interrupt needs to be able to
// take at any moment.
type job struct {
	id      int
	command string
	kind    jobKind
	// label and detail are a watch's short name and its terms ("every 10s on
	// change"), empty for a bash job. They are set once at start and read
	// without the lock.
	label   string
	detail  string
	started time.Time
	logPath string
	cmd     *exec.Cmd
	sink    *jobSink
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
	state   jobState
	code    int
	ticks   int
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
		_ = processgroup.Kill(j.cmd.Process.Pid)
		return
	}
	_ = processgroup.Terminate(j.cmd.Process.Pid)
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
	seq  int
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
	// hands is how many forked hands are OUT — started and not yet reported.
	//
	// IT IS COUNTED RATHER THAN READ OFF THE SLICE, and the reason is a race
	// that would cost a hand's whole report. A hand's job settles a moment
	// BEFORE its report reaches the steering queue, and the thing reading this
	// count is a task node's runner deciding whether to land ([runTaskChild]'s
	// tail loop, through [Agent.childrenOutstanding]). A count taken from the
	// jobs' states would read zero inside that moment, and the runner would land
	// the node on top of a report nobody had read — which is exactly the defect
	// the wait on a sub-task's report was written to close. So the count is
	// raised when the hand goes out and lowered only AFTER its report is on the
	// queue.
	hands int
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
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, errSessionClosed
	}

	directory := droppingsDir(r.place, r.workspace, droppingJobs)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("could not create the jobs directory: %w", err)
	}

	id, logPath, logFile, err := r.claimJobLog(directory)
	if err != nil {
		return nil, err
	}
	return &job{
		id:      id,
		command: command,
		kind:    kind,
		started: time.Now(),
		logPath: logPath,
		// One sink for both streams, as bare's bash does: stdout and stderr
		// interleave in arrival order, which is the order a person reading the
		// log expects them in.
		sink: &jobSink{file: logFile},
		done: make(chan struct{}),
	}, nil
}

// claimJobLog takes the next id whose log file this session can CREATE, and
// returns it with the file already open.
//
// The name is claimed, not merely chosen. The counter behind it is this
// process's own and starts at one in every window, so `1.log` is a name two
// aforges in one directory both pick within a minute of each other — and the
// open that used to be here truncated whatever was already at the name. Nothing
// visible went wrong: the older session's writer kept its own file offset, so
// its log became a hole where its first pages had been followed by two runs
// interleaved by byte position, and `jobs output` showed the person a mixture
// neither process had any idea it was in.
//
// So the create is O_EXCL and a taken name simply means take the next one. This
// is tools_image.go's collision loop, for its reason — a check that only ASKED
// whether the file existed would hand two racing openers the same answer — with
// the second race that two processes are also racing. The visible consequence is
// that a second window's job ids start above one rather than at it, which is the
// honest thing for them to do: the ids are what `jobs output` is addressed by,
// so two jobs may not share one.
func (r *jobRegistry) claimJobLog(directory string) (int, string, *os.File, error) {
	for attempt := 0; attempt < 1000; attempt++ {
		r.mu.Lock()
		r.seq++
		id := r.seq
		r.mu.Unlock()

		logPath := filepath.Join(directory, fmt.Sprintf("%d.log", id))
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			return id, logPath, logFile, nil
		}
		if !os.IsExist(err) {
			return 0, "", nil, fmt.Errorf("could not open the job log: %w", err)
		}
	}
	return 0, "", nil, fmt.Errorf("could not open the job log: %s is full of them", directory)
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
	if r.closed {
		r.mu.Unlock()
		started.sink.close()
		if started.logPath != "" {
			_ = os.Remove(started.logPath)
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
	process.Dir = r.workspace
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
		_ = processgroup.Kill(process.Process.Pid)
		return nil, err
	}

	go r.reap(started)
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

// startHand registers one forked hand as a job and hands back the job and the
// context its turn must run under.
//
// IT IS [jobRegistry.startVideo] WITH ONE MORE FACT KEPT, and everything else
// about it is the same argument: the context is the BACKGROUND one and never
// the turn's, because a hand whose whole purpose is to outlive the tool call
// that opened it would be killed by the act of answering that call. Its cancel
// is the job's stop function, so `jobs kill`, [Agent.Interrupt] and Close all
// reach it through [job.signal].
//
// The one more fact is [jobRegistry.hands]: a hand is OUT from this instant
// until its report is delivered, which is a longer life than the job's own and
// is the life a node's landing has to wait on. See the field.
func (r *jobRegistry) startHand(label, role string) (*job, context.Context, error) {
	started, err := r.newJob(role, jobKindHand)
	if err != nil {
		return nil, nil, err
	}
	started.label = label
	started.detail = role

	ctx, cancel := context.WithCancel(context.Background())
	started.stop = cancel
	r.mu.Lock()
	r.hands++
	r.mu.Unlock()
	if err := r.add(started); err != nil {
		// THE HAND COMES BACK IN. It was counted OUT a line ago, and this one is
		// never going anywhere; a count left raised would be a node's landing
		// waiting forever on a report from a hand that was refused at the door
		// (see [jobRegistry.hands]).
		r.handHome()
		cancel()
		return nil, nil, err
	}
	return started, ctx, nil
}

// handHome lowers the out-count, and it is called AFTER the hand's report is on
// the steering queue rather than when its job settles. See [jobRegistry.hands].
func (r *jobRegistry) handHome() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.hands > 0 {
		r.hands--
	}
	r.mu.Unlock()
}

// handsOutstanding reports whether any hand this session forked has yet to
// deliver its report. It is what keeps a node's landing from closing on top of
// one ([Agent.childrenOutstanding]).
func (r *jobRegistry) handsOutstanding() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hands > 0
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

// stopHands ends every hand still out, the way the person's interrupt means it.
//
// A BACKGROUND JOB SURVIVES AN INTERRUPT AND A HAND DOES NOT, and the difference
// is whose work it is. A job is a command the person asked to be left running; a
// hand is THIS MIND, copied, finishing the answer that was just interrupted —
// and an answer nobody is waiting for any more has no hands to keep out. Every
// kill here is a requested one, so no hand reports itself onto a queue whose
// turn has just been cancelled.
func (r *jobRegistry) stopHands() {
	if r == nil {
		return
	}
	for _, candidate := range r.all() {
		if candidate.kind != jobKindHand {
			continue
		}
		if candidate.requestKill() {
			candidate.signal(syscall.SIGTERM)
		}
	}
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
	if note == "" || r.notify == nil {
		return
	}
	// A goroutine's ending is one sentence its caller wrote, so that sentence is
	// already the whole ending this file composed for it.
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
		// AND THE OUTPUT COMES WITH IT. A watch's note is its own sentence and
		// needs none of this; a bash job's ending is the moment its output finally
		// means something, and a note that withheld it would be an invitation to
		// make one more call for what the note was already about.
		if watched.kind == jobKindBash {
			if tail := watched.sink.tail(jobExitTailLines); strings.TrimSpace(tail) != "" {
				note += "\n\n" + tail + "\n\n[job " + strconv.Itoa(watched.id) + " · last " +
					strconv.Itoa(jobExitTailLines) + " lines · full log: " + watched.logPath + "]"
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
	// A hand is STOPPED and whatever it had already written is STILL THERE: the
	// hands share the caller's working copy, so ending one throws nothing away
	// and leaves a slice that may be half-made. The model is told both, because
	// the second is the half it would otherwise assume away.
	if target.kind == jobKindHand {
		return fmt.Sprintf("%s (job %d) stopped; what it had already written is still in your working copy "+
			"and may be half-made — no report is coming", target.label, id), false
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
		// Nor has a hand: its outcome is a WORD (done, out of rounds, stopped)
		// that reached the model in its own report, and an exit code here would
		// be a second, dumber account of the same ending.
		if info.kind == jobKindHand {
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
		// A hand's row is a task's row for the same reason again: the label says
		// which hand it is and the detail is the one line it was told, which is
		// how a model picks one of four hands out of a list.
		if info.kind == jobKindTask || info.kind == jobKindRender || info.kind == jobKindHand {
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

// output renders one job's recent lines with a footer naming the full log. The
// footer is the point of the whole design: it tells the model where the rest
// is, so the answer to "I need more" is a read call and not a bigger tail.
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
	return fmt.Sprintf("%s\n\n[job %d · %s · showing last %d lines · full log: %s]",
		tail, id, statusText(info), lines, target.logPath), false
}

// ── the output sink: ring in memory, everything on disk ─────────────────────

// jobSink is one job's output: a rolling in-memory tail and the full spool.
//
// It is an io.Writer set as both Stdout and Stderr, so Go's exec package feeds
// it from two goroutines — the mutex is load-bearing, not decoration.
type jobSink struct {
	mu     sync.Mutex
	ring   []byte
	file   *os.File
	closed bool
}

// Write always reports success. A write error here is a full disk or a removed
// workspace, and the honest response to that is to keep the job running with
// the in-memory tail intact: returning the error would make Go's copier close
// the pipe, and the job would die of a logging problem.
func (s *jobSink) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil && !s.closed {
		_, _ = s.file.Write(data)
	}
	s.ring = append(s.ring, data...)
	// Trimming at twice the cap rather than at the cap makes this amortized:
	// trimming on every write would copy the whole ring per line of output.
	if len(s.ring) > jobRingBytes*2 {
		s.trimLocked()
	}
	return len(data), nil
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

func (s *jobSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil && !s.closed {
		_ = s.file.Close()
	}
	s.closed = true
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
