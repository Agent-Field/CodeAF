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
//   - THE STEERING LANE, not a tool and not an event. When a job ends, the
//     model learns about it the way it learns anything a person types
//     mid-turn: a line appended to the steering queue (agent.go), drained into
//     the transcript at the next step boundary. A completion is news, not an
//     answer to a question, and the alternative — the model polling `jobs` on
//     a hunch — costs a round trip per hunch and still misses the exit it did
//     not think to check for.
//
//   - NO PUSH MID-BATCH. The note lands at a step boundary and never inside
//     one, for the same reason user steering does: the transcript's tail
//     mid-batch sits between an assistant's tool_calls and their results, and
//     a user message spliced in there is a shape every provider rejects. A job
//     that exits during a tool batch is reported after that batch, which is
//     the first moment the model could act on it anyway.

import (
	"context"
	"encoding/json"
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
	// jobKindVideo is one video render (tools_video.go).
	//
	// A render is asynchronous on the wire — submit, then poll, for as long as
	// ten minutes — and a turn that waited for one would be a conversation held
	// hostage by a file nobody can look at yet. So it is a job for the reason a
	// watch is: everything AROUND it is what a job already is, and only the
	// middle differs. Its middle is one provider call in a goroutine, its kill
	// is that call's context being cancelled — the same stop function a watch
	// has — and its ending is a note on the steering lane carrying the landed
	// path or the failure.
	jobKindVideo
	// jobKindHand is one hand of a fork (fork.go): a copy of the caller's own
	// mind, working a declared slice of the same working copy.
	//
	// IT IS HERE FOR THE REASON A VIDEO RENDER IS, and the reason is the law
	// this file opens with: A HAND IS A STREAM, NOT A BARRIER. `fork` used to
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

	mu    sync.Mutex
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
}

// jobInfo is a job's status copied out from under its lock, so rendering never
// holds it.
type jobInfo struct {
	id      int
	command string
	kind    jobKind
	label   string
	detail  string
	// logPath is where everything this job wrote is spooled. It is copied out
	// with the rest because a job's row has no transcript, no branch and no
	// report to point a person at, and the log is what it points at instead
	// (jobrow.go).
	logPath string
	state   jobState
	code    int
	ticks   int
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
		logPath: j.logPath,
		state:   j.state, code: j.exitCode, ticks: j.ticks, elapsed: elapsed,
	}
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
	signalGroup(j.cmd, sig)
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
	// notify carries a completion note to the steering queue — the WAKING lane
	// (agent.go's [Agent.enqueueSteering]), which queues while a turn runs and
	// starts one when none does. It is a function rather than the Agent itself so
	// the registry has no idea what a turn is — it reports, and the lane decides
	// whether anybody has to answer.
	notify func(string)
	// announce carries one job's row to the roster — the column beside the
	// conversation, where work this session started shows whatever door started
	// it (jobrow.go). It is a function for [jobRegistry.notify]'s reason exactly:
	// the registry reports what a job is doing and has no idea what a roster is,
	// and a caller with nothing to draw leaves it nil and pays nothing.
	announce func(jobInfo)

	mu   sync.Mutex
	seq  int
	jobs []*job
	// watches is the number of watch slots CLAIMED, not the number of watch
	// jobs in the slice. Counting the slice would leave a window between the
	// limit check and the append in which two concurrent starts both pass, and
	// tool calls in one batch run concurrently.
	watches int
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

func newJobRegistry(workspace string, place Place, notify func(string)) *jobRegistry {
	return &jobRegistry{workspace: workspace, place: place, notify: notify}
}

// newJob makes the shell every job shares — an id, a log file on disk, a sink
// over it — without deciding what will run in the middle.
func (r *jobRegistry) newJob(command string, kind jobKind) (*job, error) {
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

// add puts a job in the registry. It is called once the job is actually
// running — a list between the id reservation and this append shows one fewer
// job, which is the honest answer for work that does not exist yet.
func (r *jobRegistry) add(started *job) {
	r.mu.Lock()
	r.jobs = append(r.jobs, started)
	r.mu.Unlock()
	r.announceRow(started)
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
	// Setpgid puts the job and everything it spawns in one process group, so a
	// kill reaches the whole tree. A dev server that forks a compiler must not
	// survive the kill of its parent.
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Stdout = started.sink
	process.Stderr = started.sink

	if err := process.Start(); err != nil {
		started.sink.close()
		return nil, fmt.Errorf("could not start the command: %w", err)
	}
	started.cmd = process
	r.add(started)

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
	r.add(started)
	return started, nil
}

// startVideo registers one video render as a job and hands back the job and the
// context its provider call must run under.
//
// The context is the BACKGROUND one and never the turn's, for [jobRegistry.start]'s
// reason exactly: a turn's context is cancelled when the turn ends, and a render
// whose whole purpose is to outlive the turn would be killed by the act of
// answering the person. Its cancel is the job's stop function, so `jobs kill`
// and Close both reach it through [job.signal].
//
// What runs in the middle is the caller's (tools_video.go), as a watch's loop is
// tools_watch.go's: this registry owns the id, the log, the row and the kill.
func (r *jobRegistry) startVideo(label, prompt string) (*job, context.Context, error) {
	started, err := r.newJob(prompt, jobKindVideo)
	if err != nil {
		return nil, nil, err
	}
	started.label = label
	started.detail = prompt

	ctx, cancel := context.WithCancel(context.Background())
	started.stop = cancel
	r.add(started)
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
	r.add(started)
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
// the caller already knows — and that the note is a sentence, not the output.
func (r *jobRegistry) finish(done *job, code int, note string) {
	if requested := r.settled(done, code); requested {
		return
	}
	if note == "" || r.notify == nil {
		return
	}
	r.notify(note)
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
func (r *jobRegistry) adopt(taken *bare.BashCall) (*job, error) {
	started, err := r.newJob(taken.Command(), jobKindBash)
	if err != nil {
		return nil, err
	}
	// The process, its group and its pid are unchanged by the adoption — bash
	// started it with Setpgid, so a kill still reaches the whole tree exactly as
	// it does for a job this registry forked itself.
	started.cmd = taken.Process()
	taken.Attach(started.sink)
	r.add(started)

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

	if requested || r.notify == nil {
		return
	}
	note := fmt.Sprintf("job %d exited %d", watched.id, code)
	if last := watched.sink.lastNonEmptyLine(); last != "" {
		note += ": " + clip(last, jobExitNoteLimit)
	}
	// AND THE OUTPUT COMES WITH IT. A watch's note is its own sentence and needs
	// none of this; a bash job's ending is the moment its output finally means
	// something, and a note that withheld it would be an invitation to make one
	// more call for what the note was already about.
	if watched.kind == jobKindBash {
		if tail := watched.sink.tail(jobExitTailLines); strings.TrimSpace(tail) != "" {
			note += "\n\n" + tail + "\n\n[job " + strconv.Itoa(watched.id) + " · last " +
				strconv.Itoa(jobExitTailLines) + " lines · full log: " + watched.logPath + "]"
		}
	}
	r.notify(note)
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
func (r *jobRegistry) kill(id int) (string, bool) {
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
	if !waitDone(target.done, jobTermGrace) {
		target.signal(syscall.SIGKILL)
		// The second wait is bounded too: a process wedged in an
		// uninterruptible sleep is not something a tool call can fix, and
		// hanging the turn on it would be worse than reporting the SIGKILL.
		waitDone(target.done, jobTermGrace)
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
	// model needs to know: no file landed, and no note about this job is coming.
	if target.kind == jobKindVideo {
		return fmt.Sprintf("%s (job %d) stopped; no video was saved", target.label, id), false
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
		if info.kind == jobKindVideo {
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
		if info.kind == jobKindTask || info.kind == jobKindVideo || info.kind == jobKindHand {
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

// signalGroup signals the job's whole process group, falling back to the
// process itself if the group is already gone.
//
// It is only ever called for a job whose state is still running, so the pid has
// not been reaped and cannot have been recycled onto somebody else's process.
func signalGroup(command *exec.Cmd, signal syscall.Signal) {
	if command.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(command.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, signal)
		return
	}
	_ = command.Process.Signal(signal)
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
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case <-done:
		return true
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
	if err := json.Unmarshal(args, &parsed); err != nil {
		return parsed, err
	}
	return parsed, nil
}
