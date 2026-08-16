package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	jobsDir                  = ".aforge/jobs"
	defaultJobSeconds        = 900
	maxUndeadlinedJobSeconds = 3600
	maxJobWaitSeconds        = 120
	jobTerminateGrace        = 2 * time.Second
	maxJobLineBytes          = 240
	maxServiceNameBytes      = 80
	// backgroundJobNice is the scheduling priority a detached job's whole
	// process group runs at. +10 yields the interactive machine without
	// starving the job: it still gets every slice nothing else wants, which is
	// when a long scan or build should be running anyway. Foreground sh keeps
	// normal priority — it is on the leaf's critical path.
	backgroundJobNice = 10
)

// jobLogName is where one background job's output lands, and it carries the
// leaf as well as the number because the number alone is unique only within one
// Workspace handle. Concurrent leaves each hold their own handle and now share
// one scratch home, so `1.log` is a name two of them would both claim; the leaf
// is what makes it one file per job again. The same slug every other per-leaf
// file in this package uses (traceName, the spill names), for the same reason:
// one worker, one identity, one spelling.
func jobLogName(leaf string, id int) string {
	slug := pathSlug(leaf)
	if slug == "" {
		return fmt.Sprintf("%d.log", id)
	}
	return fmt.Sprintf("%s-%d.log", slug, id)
}

// setProcessGroupPriority renices a detached process group. It is a variable
// so the spawn path can be exercised where a sandbox forbids the syscall, and
// the error is dropped because not every environment permits renicing at all —
// failing to yield is never a reason to fail the job.
var setProcessGroupPriority = func(pgid, priority int) {
	_ = syscall.Setpriority(syscall.PRIO_PGRP, pgid, priority)
}

type jobState uint8

const (
	jobRunning jobState = iota
	jobExited
	jobTimedOut
	jobKilled
)

type backgroundJob struct {
	id       int
	cmd      *exec.Cmd
	ctx      context.Context
	cancel   context.CancelFunc
	logPath  string
	fullPath string
	started  time.Time
	finished time.Time
	done     chan struct{}
	command  string
	dir      string
	timeout  *time.Timer

	state            jobState
	exitCode         int
	readOffset       int64
	terminalReported bool
	stopRequested    bool
	timedOut         bool
	keepRequested    bool
	keepName         string
	keepHealth       store.ServiceHealth
	promoted         bool
	waited           bool
	settled          bool
}

// jobRegistry belongs to exactly one Toolbox, hence one leaf. Its jobs stay in
// the map after reaping so the model can query their final state and unread log.
type jobRegistry struct {
	workspace *Workspace
	leaf      string
	// results is the leaf's whole-result bound, handed down from the toolbox so
	// a job report and an ordinary tool result are clamped at the same place.
	// See toolBudgets.
	results int

	mutex       sync.Mutex
	jobs        map[int]*backgroundJob
	closed      bool
	closeDone   chan struct{}
	closedCount int
}

func newJobRegistry(workspace *Workspace, leaf string, results int) *jobRegistry {
	if results <= 0 {
		results = maxToolResultBytes
	}
	return &jobRegistry{
		workspace: workspace,
		leaf:      leaf,
		results:   results,
		jobs:      make(map[int]*backgroundJob),
		closeDone: make(chan struct{}),
	}
}

// startBackground starts a shell with file-backed output. Because no parent
// pipe is involved, a verbose build cannot block on a reader in the executor.
func (t *Toolbox) startBackground(ctx context.Context, command string, args map[string]any) Result {
	duration, err := backgroundDuration(ctx, args)
	if err != nil {
		return errorf("could not start background job: %v", err)
	}

	var environment []string
	if t.history != nil {
		if bin, pathErr := store.SkillsBinDir(); pathErr == nil {
			environment = os.Environ()
			environment = replaceEnv(environment, "AFORGE_SKILLS_BIN", bin)
			command = "export PATH=\"${AFORGE_SKILLS_BIN:?}:$PATH\"\n" + command
		}
	}

	r := t.jobs
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return errorf("could not start background job: leaf is ending")
	}

	id := r.workspace.nextJobID()
	full, relative, err := r.workspace.ScratchPath(filepath.Join(jobsDir, jobLogName(r.leaf, id)))
	if err != nil {
		return errorf("could not create background log: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return errorf("could not create background log directory: %v", err)
	}
	logFile, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return errorf("could not create background log %s: %v", relative, err)
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(jobCtx, "bash", "-lc", command)
	configureDetachedCommand(cmd, r.workspace.Root(), logFile)
	if environment != nil {
		cmd.Env = environment
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return errorf("could not start background job: %v", err)
	}
	// Start duplicated the descriptor into the child. The parent closes its
	// copy immediately; cmd.Wait does not need it and the child writes directly.
	_ = logFile.Close()
	// configureDetachedCommand gave the child its own session, so its pid is
	// its process group id and everything it spawns inherits the yield.
	setProcessGroupPriority(cmd.Process.Pid, backgroundJobNice)

	started := time.Now()
	if identity, identityErr := ProcessStartTime(cmd.Process.Pid); identityErr == nil {
		started = identity
	}
	job := &backgroundJob{
		id: id, cmd: cmd, ctx: jobCtx, cancel: cancel,
		logPath: relative, fullPath: full, started: started,
		command: command, dir: r.workspace.Root(),
		done: make(chan struct{}), state: jobRunning,
	}
	r.jobs[id] = job
	job.timeout = time.AfterFunc(duration, func() {
		defer guard.Recover("exec/job timeout")
		if r.markTimedOut(job) {
			job.cancel()
		}
	})
	r.workspace.RecordInternal(r.leaf, full)
	go r.wait(job)
	return Result{Content: fmt.Sprintf("job %d started · log %s", id, filepath.ToSlash(relative))}
}

// markTimedOut flags a still-running job as expired and reports whether the
// cancel is this timer's to fire. A keep already granted outranks the deadline.
func (r *jobRegistry) markTimedOut(job *backgroundJob) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if job.state != jobRunning || job.keepRequested {
		return false
	}
	job.timedOut = true
	return true
}

func backgroundDuration(ctx context.Context, args map[string]any) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	seconds := intArg(args, "t", defaultJobSeconds)
	if seconds <= 0 {
		seconds = defaultJobSeconds
	}
	duration := time.Duration(seconds) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}
		if duration > remaining {
			duration = remaining
		}
	} else if duration > maxUndeadlinedJobSeconds*time.Second {
		duration = maxUndeadlinedJobSeconds * time.Second
	}
	return duration, nil
}

// wait is the sole goroutine that calls cmd.Wait and writes a terminal state.
// A fault here would leave every waiter on job.done blocked forever, so the
// terminal state is published from a defer that runs on the fault path too.
func (r *jobRegistry) wait(job *backgroundJob) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("exec/job wait", recovered)
			r.settleFaulted(job)
		}
	}()
	_ = job.cmd.Wait()
	// A shell can exit after detaching a child into its process group. File
	// output means Wait rightly does not block on that child, so the sole waiter
	// also cleans the remaining group before publishing the terminal state.
	if !r.markWaited(job) {
		terminateDetachedGroup(job.cmd.Process.Pid)
	}
	job.cancel()
	if job.timeout != nil {
		job.timeout.Stop()
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()
	job.finished = time.Now()
	switch {
	case job.stopRequested:
		job.state = jobKilled
	case job.timedOut:
		job.state = jobTimedOut
	default:
		job.state = jobExited
		if job.cmd.ProcessState != nil {
			job.exitCode = job.cmd.ProcessState.ExitCode()
		}
	}
	job.settled = true
	close(job.done)
}

// markWaited records that cmd.Wait returned, and reports whether the job is
// persistent — a keep or a promotion means its group is meant to outlive the
// leaf and must not be swept.
func (r *jobRegistry) markWaited(job *backgroundJob) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job.waited = true
	return job.keepRequested || job.promoted
}

// settleFaulted publishes a terminal state for a job whose waiter faulted.
// Killed is the honest reading: nobody can say what the process did, and every
// waiter is owed an answer rather than a silent forever.
func (r *jobRegistry) settleFaulted(job *backgroundJob) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if job.settled {
		return
	}
	job.settled = true
	job.waited = true
	job.finished = time.Now()
	job.state = jobKilled
	close(job.done)
}

// job implements the four behaviors of the universal job tool.
func (t *Toolbox) job(args map[string]any) Result {
	id := intArg(args, "id", 0)
	if id == 0 {
		return Result{Content: t.jobs.list()}
	}
	if raw, ok := args["keep"]; ok {
		keep, ok := raw.(map[string]any)
		if !ok {
			return errorf("keep must be an object with name and health")
		}
		name := strings.TrimSpace(stringArg(keep, "name"))
		health, err := store.ParseServiceHealth(stringArg(keep, "health"))
		if err != nil {
			return errorf("could not request service promotion: %v", err)
		}
		if err := t.jobs.requestKeep(id, name, health); err != nil {
			return errorf("could not request service promotion: %v", err)
		}
		return Result{Content: fmt.Sprintf("job %d promotion requested · %s · %s", id, name, health.Suffix())}
	}
	if boolArg(args, "kill") {
		if err := t.jobs.kill(id); err != nil {
			return errorf("%v", err)
		}
	} else if seconds := intArg(args, "wait", 0); seconds > 0 {
		if seconds > maxJobWaitSeconds {
			seconds = maxJobWaitSeconds
		}
		if err := t.jobs.waitFor(id, time.Duration(seconds)*time.Second); err != nil {
			return errorf("%v", err)
		}
	}
	return t.jobs.read(id)
}

// ServiceRequest is a live hand-off from a leaf registry to the resident. Its
// metadata is immutable; Adopt and Stop are idempotent terminal decisions.
type ServiceRequest struct {
	JobID      int
	Name       string
	Command    string
	Dir        string
	Health     store.ServiceHealth
	LogPath    string
	PID        int
	StartedAt  time.Time
	LeafNodeID string

	registry *jobRegistry
	job      *backgroundJob
	mutex    sync.Mutex
	settled  bool
}

func (r *jobRegistry) requestKeep(id int, name string, health store.ServiceHealth) error {
	if name == "" {
		return errors.New("service name is required")
	}
	if len(name) > maxServiceNameBytes || strings.ContainsAny(name, "\r\n\t") {
		return errors.New("service name must be one short line")
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job := r.jobs[id]
	if job == nil {
		return fmt.Errorf("no background job %d", id)
	}
	if job.state != jobRunning || job.waited {
		return fmt.Errorf("background job %d is no longer running", id)
	}
	if job.keepRequested {
		if job.keepName == name && job.keepHealth == health {
			return nil
		}
		return fmt.Errorf("background job %d already requested promotion as %q", id, job.keepName)
	}
	job.keepRequested = true
	job.keepName = name
	job.keepHealth = health
	if job.timeout != nil {
		job.timeout.Stop()
	}
	return nil
}

func (r *jobRegistry) serviceRequests(leafNodeID string) []ServiceRequest {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	ids := make([]int, 0, len(r.jobs))
	for id, job := range r.jobs {
		if job.keepRequested && job.state == jobRunning {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	requests := make([]ServiceRequest, 0, len(ids))
	for _, id := range ids {
		job := r.jobs[id]
		requests = append(requests, ServiceRequest{
			JobID: id, Name: job.keepName, Command: job.command, Dir: job.dir,
			Health: job.keepHealth, LogPath: job.fullPath, PID: job.cmd.Process.Pid,
			StartedAt: job.started, LeafNodeID: leafNodeID, registry: r, job: job,
		})
	}
	return requests
}

// Adopt removes the process from the leaf registry. The existing waiter keeps
// reaping the shell when it eventually exits; leaf teardown can no longer see
// or kill it.
func (request *ServiceRequest) Adopt() {
	if request == nil || !request.settle() {
		return
	}
	request.registry.promote(request.job, request.JobID)
}

// Stop returns a declined or expired request to the ordinary teardown rule.
func (request *ServiceRequest) Stop() {
	if request == nil || !request.settle() {
		return
	}
	if pid, done, running := request.registry.releaseKeep(request.job); running {
		terminateProcessGroup(pid, done)
	}
}

// settle takes the request's one-shot resolution and reports whether this
// caller is the one that got it. Adopt and Stop can race — a resident deciding
// while the lease expires — and only the winner acts on the job.
func (request *ServiceRequest) settle() bool {
	request.mutex.Lock()
	defer request.mutex.Unlock()
	if request.settled {
		return false
	}
	request.settled = true
	return true
}

// promote is Adopt's critical section: the job leaves the registry, and the
// flag keeps a concurrent sweep from claiming it on the way out.
func (r *jobRegistry) promote(job *backgroundJob, id int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job.promoted = true
	delete(r.jobs, id)
}

// releaseKeep drops the keep and, when the shell is still running, records the
// stop and hands back what terminating its group needs.
func (r *jobRegistry) releaseKeep(job *backgroundJob) (pid int, done <-chan struct{}, running bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job.keepRequested = false
	if job.state != jobRunning {
		return 0, nil, false
	}
	job.stopRequested = true
	return job.cmd.Process.Pid, job.done, true
}

func (r *jobRegistry) waitFor(id int, duration time.Duration) error {
	done, running, err := r.waitHandle(id)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	return nil
}

// waitHandle takes a job's done channel and its liveness from the same instant,
// so a waiter cannot decide to block on a state that has already moved on.
func (r *jobRegistry) waitHandle(id int) (done <-chan struct{}, running bool, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job := r.jobs[id]
	if job == nil {
		return nil, false, fmt.Errorf("no background job %d", id)
	}
	return job.done, job.state == jobRunning, nil
}

func (r *jobRegistry) kill(id int) error {
	pid, done, running, err := r.markStopped(id)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	terminateProcessGroup(pid, done)
	return nil
}

// markStopped records the stop request and hands back what terminating the
// group needs. running is false when the job has already landed.
func (r *jobRegistry) markStopped(id int) (pid int, done <-chan struct{}, running bool, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job := r.jobs[id]
	if job == nil {
		return 0, nil, false, fmt.Errorf("no background job %d", id)
	}
	if job.state != jobRunning {
		return 0, nil, false, nil
	}
	job.stopRequested = true
	return job.cmd.Process.Pid, job.done, true, nil
}

func terminateProcessGroup(pid int, done <-chan struct{}) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	timer := time.NewTimer(jobTerminateGrace)
	select {
	case <-done:
		timer.Stop()
		return
	case <-timer.C:
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	<-done
}

func terminateDetachedGroup(pid int) {
	if !processGroupAlive(pid) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(jobTerminateGrace)
	for processGroupAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processGroupAlive(pid) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func processGroupAlive(pid int) bool {
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (r *jobRegistry) read(id int) Result {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	job := r.jobs[id]
	if job == nil {
		return errorf("no background job %d", id)
	}
	output, err := readSince(job.fullPath, &job.readOffset, r.results)
	header := fmt.Sprintf("job %d · %s · %s", job.id, job.status(), job.age(time.Now()))
	if err != nil {
		return errorf("%s\ncould not read log: %v", header, err)
	}
	if output == "" {
		return Result{Content: header, shape: shapeCommand}
	}
	// A job's log is command output like any other, so if it has to be cut the
	// cut comes off the front and the end — where a crash, a stack trace or the
	// readiness line lives — is what survives. See resultShape.
	return Result{Content: clamp(header+"\n"+output, r.results), shape: shapeCommand}
}

func readSince(path string, offset *int64, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	// Read a fixed snapshot. A process that appends faster than this call reads
	// must not turn an efficient status check into an unbounded tail -f.
	if *offset > info.Size() {
		*offset = 0 // the model may have truncated the composable log with sh
	}
	length := info.Size() - *offset
	data, err := io.ReadAll(io.NewSectionReader(file, *offset, length))
	if err != nil {
		return "", err
	}
	*offset = info.Size()
	return clamp(string(data), limit), nil
}

func (r *jobRegistry) list() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if len(r.jobs) == 0 {
		return "(no background jobs)"
	}
	ids := make([]int, 0, len(r.jobs))
	for id := range r.jobs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	lines := make([]string, 0, len(ids))
	now := time.Now()
	for _, id := range ids {
		job := r.jobs[id]
		last := lastLogLine(job.fullPath, r.results)
		if last == "" {
			last = "(no output)"
		}
		lines = append(lines, fmt.Sprintf("job %d · %s · %s · last: %s", id, job.status(), job.age(now), last))
	}
	return clamp(strings.Join(lines, "\n"), r.results)
}

// report emits every running job and consumes each terminal transition once.
func (r *jobRegistry) report() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if len(r.jobs) == 0 {
		return ""
	}
	ids := make([]int, 0, len(r.jobs))
	for id := range r.jobs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	now := time.Now()
	var lines []string
	for _, id := range ids {
		job := r.jobs[id]
		last := lastLogLine(job.fullPath, r.results)
		if job.state == jobRunning {
			line := fmt.Sprintf("[job %d · running %s", id, job.age(now))
			if last != "" {
				line += " · last: " + last
			}
			lines = append(lines, line+"]")
			continue
		}
		if job.terminalReported {
			continue
		}
		job.terminalReported = true
		line := fmt.Sprintf("[job %d · %s after %s", id, job.status(), job.age(now))
		if last != "" && (job.state != jobExited || job.exitCode != 0) {
			line += " · last: " + last
		}
		lines = append(lines, line+"]")
	}
	return clamp(strings.Join(lines, "\n"), r.results)
}

func (job *backgroundJob) status() string {
	switch job.state {
	case jobRunning:
		return "running"
	case jobExited:
		return fmt.Sprintf("exited %d", job.exitCode)
	case jobTimedOut:
		return "timed out"
	case jobKilled:
		return "killed"
	default:
		return "running"
	}
}

func (job *backgroundJob) age(now time.Time) string {
	end := now
	if !job.finished.IsZero() {
		end = job.finished
	}
	age := end.Sub(job.started).Round(time.Second)
	if age < time.Second {
		return "0s"
	}
	return age.String()
}

var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func lastLogLine(path string, limit int) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	readBytes := int64(limit)
	if info.Size() < readBytes {
		readBytes = info.Size()
	}
	data := make([]byte, readBytes)
	if _, err := file.ReadAt(data, info.Size()-readBytes); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(ansiEscape.ReplaceAllString(lines[index], ""))
		if line != "" {
			return compactJobLine(clamp(line, limit))
		}
	}
	return ""
}

func compactJobLine(line string) string {
	line = strings.Join(strings.Fields(line), " ")
	if len(line) <= maxJobLineBytes {
		return line
	}
	head := maxJobLineBytes * 2 / 3
	tail := maxJobLineBytes - head - len("...")
	return wholeRunesHead(line[:head]) + "..." + wholeRunesTail(line[len(line)-tail:])
}

// close terminates and reaps every process that still survives the leaf. It is
// idempotent because normal landing and scheduler abandonment can race.
func (r *jobRegistry) close() int {
	jobs, done, first := r.claimSweep()
	if !first {
		<-done
		return r.sweptCount()
	}
	reap(jobs)
	r.publishSweep(len(jobs))
	return len(jobs)
}

// claimSweep takes the one-shot right to run the sweep. The winner gets the
// jobs to terminate; every later caller gets the channel to wait on instead.
func (r *jobRegistry) claimSweep() (jobs []*backgroundJob, done chan struct{}, first bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return nil, r.closeDone, false
	}
	r.closed = true
	jobs = make([]*backgroundJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		if job.state == jobRunning && !job.keepRequested && !job.promoted {
			job.stopRequested = true
			jobs = append(jobs, job)
		}
	}
	return jobs, r.closeDone, true
}

// publishSweep records what the sweep terminated and releases everyone waiting.
func (r *jobRegistry) publishSweep(count int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.closedCount = count
	close(r.closeDone)
}

// sweptCount reads what the sweep recorded, for the callers that only waited.
func (r *jobRegistry) sweptCount() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.closedCount
}

// reap signals the whole set, then waits on it. Anything still alive when the
// grace expires is killed outright and reaped, so close never returns leaving a
// survivor unaccounted for.
func reap(jobs []*backgroundJob) {
	for _, job := range jobs {
		_ = syscall.Kill(-job.cmd.Process.Pid, syscall.SIGTERM)
	}
	deadline := time.NewTimer(jobTerminateGrace)
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-deadline.C:
			for _, survivor := range jobs {
				select {
				case <-survivor.done:
				default:
					_ = syscall.Kill(-survivor.cmd.Process.Pid, syscall.SIGKILL)
				}
			}
			for _, survivor := range jobs {
				<-survivor.done
			}
			return
		}
	}
	if !deadline.Stop() {
		select {
		case <-deadline.C:
		default:
		}
	}
}

// Close is used by leaf teardown and tests. Log files and final registry state
// remain; only surviving process groups are terminated.
func (t *Toolbox) Close() int {
	return t.jobs.close()
}

// ServiceRequests snapshots live promotion leases before leaf teardown.
func (t *Toolbox) ServiceRequests(leafNodeID string) []ServiceRequest {
	return t.jobs.serviceRequests(leafNodeID)
}

// leafControl is the narrow bridge from the scheduler watchdog to a leaf's
// per-Toolbox registry. An abandonment that wins before attach prevents later
// jobs from starting; one that wins after attach closes them immediately.
type leafControl struct {
	mutex      sync.Mutex
	tools      *Toolbox
	abandoned  bool
	terminated int
}

func (c *leafControl) attach(tools *Toolbox) {
	if c == nil {
		return
	}
	if c.adopt(tools) {
		tools.Close()
	}
}

// adopt installs the toolbox and reports whether abandonment already won the
// race, in which case the caller closes what it has just attached.
func (c *leafControl) adopt(tools *Toolbox) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.tools = tools
	return c.abandoned
}

func (c *leafControl) detach(tools *Toolbox, terminated int) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.tools == tools {
		c.tools = nil
	}
	if terminated > c.terminated {
		c.terminated = terminated
	}
}

func (c *leafControl) terminate() int {
	if c == nil {
		return 0
	}
	tools, terminated := c.abandon()
	if tools == nil {
		return terminated
	}
	terminated = tools.ForceClose()
	c.recordTerminated(terminated)
	return terminated
}

// abandon marks the leaf abandoned and hands back the toolbox to close if one
// has attached, together with what an earlier detach already counted.
func (c *leafControl) abandon() (*Toolbox, int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.abandoned = true
	return c.tools, c.terminated
}

// recordTerminated keeps the high-water mark: a later, emptier close of the
// same leaf must not erase what an earlier one counted.
func (c *leafControl) recordTerminated(terminated int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if terminated > c.terminated {
		c.terminated = terminated
	}
}

// ForceClose is the scheduler-abandonment path. A wedged leaf cannot leave a
// keep request behind without a resident available to decide it.
func (t *Toolbox) ForceClose() int {
	t.jobs.dropKeeps()
	return t.jobs.close()
}

// dropKeeps clears every keep and promotion so the sweep that follows claims
// the whole registry.
func (r *jobRegistry) dropKeeps() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, job := range r.jobs {
		job.keepRequested = false
		job.promoted = false
	}
}
