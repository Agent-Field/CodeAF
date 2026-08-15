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
//     file under the workspace, so the whole log is addressable by the read
//     tool — paged, offset, grepped, the same way any other file is. Only the
//     last 64KB is kept in memory, and only that tail is ever handed back
//     through a tool result. A watcher that has printed 400MB must not be able
//     to put 400MB in front of the model, and a watcher that printed the one
//     line that matters must not lose it because nobody was polling. Disk
//     answers the second, the ring answers the first.
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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// jobsDirName is where the full logs live, under the workspace so the read
	// tool can reach them with a relative path and the person can find them
	// after the session is gone.
	jobsDirName = ".aforge-v3/jobs"

	// jobRingBytes is the in-memory tail. 64KB is a few hundred lines of a
	// build log — enough that `jobs output` after a failure shows the failure,
	// and small enough that a hundred jobs cost megabytes, not gigabytes.
	jobRingBytes = 64 << 10

	// jobTermGrace is how long a SIGTERM has to work before SIGKILL follows.
	// Two seconds is a server's shutdown hook, not a wait.
	jobTermGrace = 2 * time.Second

	// jobExitNoteLimit caps the log line quoted in the completion note. The
	// note is a sentence in the transcript, not the output: the output is in
	// the ring and on disk, and the model reads it if it cares.
	jobExitNoteLimit = 120
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
		state: j.state, code: j.exitCode, ticks: j.ticks, elapsed: elapsed,
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
	// notify carries a completion note to the steering queue. It is a function
	// rather than the Agent itself so the registry has no idea what a turn is —
	// it reports, and the lane decides when the model reads.
	notify func(string)

	mu   sync.Mutex
	seq  int
	jobs []*job
	// watches is the number of watch slots CLAIMED, not the number of watch
	// jobs in the slice. Counting the slice would leave a window between the
	// limit check and the append in which two concurrent starts both pass, and
	// tool calls in one batch run concurrently.
	watches int
}

func newJobRegistry(workspace string, notify func(string)) *jobRegistry {
	return &jobRegistry{workspace: workspace, notify: notify}
}

// newJob makes the shell every job shares — an id, a log file on disk, a sink
// over it — without deciding what will run in the middle.
func (r *jobRegistry) newJob(command string, kind jobKind) (*job, error) {
	directory := filepath.Join(r.workspace, filepath.FromSlash(jobsDirName))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("could not create the jobs directory: %w", err)
	}

	r.mu.Lock()
	r.seq++
	id := r.seq
	r.mu.Unlock()

	logPath := filepath.Join(directory, fmt.Sprintf("%d.log", id))
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("could not open the job log: %w", err)
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

// add puts a job in the registry. It is called once the job is actually
// running — a list between the id reservation and this append shows one fewer
// job, which is the honest answer for work that does not exist yet.
func (r *jobRegistry) add(started *job) {
	r.mu.Lock()
	r.jobs = append(r.jobs, started)
	r.mu.Unlock()
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

	shell, shellArgs := jobShell()
	process := exec.Command(shell, append(shellArgs, command)...)
	process.Dir = r.workspace
	process.Env = os.Environ()
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

// reap waits on one process and, unless the death was asked for, drops a note
// on the steering queue.
func (r *jobRegistry) reap(watched *job) {
	code := waitExitCode(watched.cmd.Wait())
	requested := watched.settle(code)

	if requested || r.notify == nil {
		return
	}
	note := fmt.Sprintf("job %d exited %d", watched.id, code)
	if last := watched.sink.lastNonEmptyLine(); last != "" {
		note += ": " + clip(last, jobExitNoteLimit)
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
