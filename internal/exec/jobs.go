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

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	jobsDir                  = ".aforge/jobs"
	defaultJobSeconds        = 900
	maxUndeadlinedJobSeconds = 3600
	maxJobWaitSeconds        = 120
	jobTerminateGrace        = 2 * time.Second
	maxJobLineBytes          = 240
)

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

	state            jobState
	exitCode         int
	readOffset       int64
	terminalReported bool
	stopRequested    bool
}

// jobRegistry belongs to exactly one Toolbox, hence one leaf. Its jobs stay in
// the map after reaping so the model can query their final state and unread log.
type jobRegistry struct {
	workspace *Workspace
	nodeID    int

	mutex       sync.Mutex
	jobs        map[int]*backgroundJob
	closed      bool
	closeDone   chan struct{}
	closedCount int
}

func newJobRegistry(workspace *Workspace, nodeID int) *jobRegistry {
	return &jobRegistry{
		workspace: workspace,
		nodeID:    nodeID,
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
	relative := filepath.Join(jobsDir, fmt.Sprintf("%d.log", id))
	full, err := r.workspace.Resolve(relative)
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

	jobCtx, cancel := context.WithTimeout(context.Background(), duration)
	cmd := exec.CommandContext(jobCtx, "bash", "-lc", command)
	cmd.Dir = r.workspace.Root()
	if environment != nil {
		cmd.Env = environment
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

	job := &backgroundJob{
		id: id, cmd: cmd, ctx: jobCtx, cancel: cancel,
		logPath: relative, fullPath: full, started: time.Now(),
		done: make(chan struct{}), state: jobRunning,
	}
	r.jobs[id] = job
	r.workspace.Record(r.nodeID, full)
	go r.wait(job)
	return Result{Content: fmt.Sprintf("job %d started · log %s", id, filepath.ToSlash(relative))}
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
func (r *jobRegistry) wait(job *backgroundJob) {
	_ = job.cmd.Wait()
	// A shell can exit after detaching a child into its process group. File
	// output means Wait rightly does not block on that child, so the sole waiter
	// also cleans the remaining group before publishing the terminal state.
	terminateDetachedGroup(job.cmd.Process.Pid)
	job.cancel()

	r.mutex.Lock()
	defer r.mutex.Unlock()
	job.finished = time.Now()
	switch {
	case job.stopRequested:
		job.state = jobKilled
	case errors.Is(job.ctx.Err(), context.DeadlineExceeded):
		job.state = jobTimedOut
	default:
		job.state = jobExited
		if job.cmd.ProcessState != nil {
			job.exitCode = job.cmd.ProcessState.ExitCode()
		}
	}
	close(job.done)
}

// job implements the four behaviors of the universal job tool.
func (t *Toolbox) job(args map[string]any) Result {
	id := intArg(args, "id", 0)
	if id == 0 {
		return Result{Content: t.jobs.list()}
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

func (r *jobRegistry) waitFor(id int, duration time.Duration) error {
	r.mutex.Lock()
	job := r.jobs[id]
	if job == nil {
		r.mutex.Unlock()
		return fmt.Errorf("no background job %d", id)
	}
	done := job.done
	running := job.state == jobRunning
	r.mutex.Unlock()
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

func (r *jobRegistry) kill(id int) error {
	r.mutex.Lock()
	job := r.jobs[id]
	if job == nil {
		r.mutex.Unlock()
		return fmt.Errorf("no background job %d", id)
	}
	if job.state != jobRunning {
		r.mutex.Unlock()
		return nil
	}
	job.stopRequested = true
	done := job.done
	pid := job.cmd.Process.Pid
	r.mutex.Unlock()

	terminateProcessGroup(pid, done)
	return nil
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
	output, err := readSince(job.fullPath, &job.readOffset)
	header := fmt.Sprintf("job %d · %s · %s", job.id, job.status(), job.age(time.Now()))
	if err != nil {
		return errorf("%s\ncould not read log: %v", header, err)
	}
	if output == "" {
		return Result{Content: header}
	}
	return Result{Content: clamp(header + "\n" + output)}
}

func readSince(path string, offset *int64) (string, error) {
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
	return clamp(string(data)), nil
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
		last := lastLogLine(job.fullPath)
		if last == "" {
			last = "(no output)"
		}
		lines = append(lines, fmt.Sprintf("job %d · %s · %s · last: %s", id, job.status(), job.age(now), last))
	}
	return clamp(strings.Join(lines, "\n"))
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
		last := lastLogLine(job.fullPath)
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
	return clamp(strings.Join(lines, "\n"))
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

func lastLogLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	readBytes := int64(maxToolResultBytes)
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
			return compactJobLine(clamp(line))
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
	return line[:head] + "..." + line[len(line)-tail:]
}

// close terminates and reaps every process that still survives the leaf. It is
// idempotent because normal landing and scheduler abandonment can race.
func (r *jobRegistry) close() int {
	r.mutex.Lock()
	if r.closed {
		done := r.closeDone
		r.mutex.Unlock()
		<-done
		r.mutex.Lock()
		count := r.closedCount
		r.mutex.Unlock()
		return count
	}
	r.closed = true
	jobs := make([]*backgroundJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		if job.state == jobRunning {
			job.stopRequested = true
			jobs = append(jobs, job)
		}
	}
	r.mutex.Unlock()

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
			goto closed
		}
	}
	if !deadline.Stop() {
		select {
		case <-deadline.C:
		default:
		}
	}

closed:
	r.mutex.Lock()
	r.closedCount = len(jobs)
	close(r.closeDone)
	r.mutex.Unlock()
	return len(jobs)
}

// Close is used by leaf teardown and tests. Log files and final registry state
// remain; only surviving process groups are terminated.
func (t *Toolbox) Close() int {
	return t.jobs.close()
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
	c.mutex.Lock()
	c.tools = tools
	abandoned := c.abandoned
	c.mutex.Unlock()
	if abandoned {
		tools.Close()
	}
}

func (c *leafControl) detach(tools *Toolbox, terminated int) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	if c.tools == tools {
		c.tools = nil
	}
	if terminated > c.terminated {
		c.terminated = terminated
	}
	c.mutex.Unlock()
}

func (c *leafControl) terminate() int {
	if c == nil {
		return 0
	}
	c.mutex.Lock()
	c.abandoned = true
	tools := c.tools
	terminated := c.terminated
	c.mutex.Unlock()
	if tools == nil {
		return terminated
	}
	terminated = tools.Close()
	c.mutex.Lock()
	if terminated > c.terminated {
		c.terminated = terminated
	}
	c.mutex.Unlock()
	return terminated
}
