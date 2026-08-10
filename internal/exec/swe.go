package exec

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// SWE is the second kind of worker: a whole software-engineering pipeline taken
// as one leaf.
//
// Everything about it that is interesting is on the far side of a process
// boundary. The engine is vendored at internal/swepro and keeps process-global
// state — env knobs it sets on itself, a plandb singleton, a working directory
// it owns — so a run of it is a child process rather than a goroutine, reached
// by re-execing the aforge binary with a sentinel in its environment
// (cmd/aforge/swepro.go). What this file does is therefore not "call the
// engine" but "be a well-behaved leaf while a subprocess does the work":
// compose the goal, own the workspace's git precondition, read the NDJSON the
// child writes, and turn its terminal line into an Outcome that nothing
// downstream can tell apart from a linear one.
//
// The law it is written against is docs/SUBHARNESSES.md, "A swe leaf is an
// ordinary node — the checklist". Control between events, steering drained at
// the same boundary, milestones through Share, stage transitions through
// Progress, the whole stream to the node's flight recorder, spend in Usage, and a
// verdict this executor sets itself because — alone among the workers — it owns
// a verifier.
type SWE struct {
	workspace *Workspace
	// model is aforge's spelling of the leaf's pinned model. The engine wants
	// its own, and enginePool below is the whole of the translation.
	model    string
	apiKey   string
	baseURL  string
	deadline time.Duration
	maxCost  float64
	// binary is what gets exec'd. It is this process's own executable, because
	// the engine *is* this binary under a sentinel; the field exists so a test
	// can point the same machinery at a stub that speaks NDJSON and nothing
	// else.
	binary string
	// extraEnv is appended last, after the pins. Tests own it.
	extraEnv []string
	// pollEvery is how often control and steering are read while the child is
	// quiet. Between events they are read anyway; this is the floor under a
	// stage that takes ten minutes and says nothing.
	pollEvery time.Duration
}

// SWESubharness is this worker's name, and the one string the graph carries
// about it. It lives here rather than in a table because the executor, the
// registration and the profile key must all spell it the same way and there is
// no second spelling that would be right.
const SWESubharness = "swe"

// sweproSentinelEnv turns the aforge binary into the engine. The name is
// pinned from the other side by cmd/aforge/swepro_test.go — both halves of the
// re-exec have to agree on it, and there is no third place it could live
// without one of them importing the other.
const sweproSentinelEnv = "AFORGE_SWEPRO"

// DefaultSWEMaxCost is the engine's cumulative spend ceiling for one leaf, in
// dollars. It is a backstop rather than a budget: a coding pipeline that has
// spent ten dollars on one issue has stopped converging, and the ceiling turns
// that from an open tap into a budget-exhausted terminal the continuation
// replan already knows what to do with. AFORGE_SWE_MAX_COST moves it.
const DefaultSWEMaxCost = 10.0

const (
	// sweControlPoll is the quiet-stream heartbeat for control and steering.
	sweControlPoll = 2 * time.Second
	// sweTerminateGrace is how long the process group has to die politely
	// before it is killed. The engine checkpoints on the way down, and a
	// checkpoint is what makes a paused leaf cheap to resume.
	sweTerminateGrace = 10 * time.Second
	// sweStderrTail bounds what a death with no terminal event may report.
	sweStderrTail = 20
	// sweArtifactLimit bounds the file list handed downstream. A refactor that
	// touches four hundred files is a real outcome; four hundred paths pasted
	// into three dependents' contexts is not.
	sweArtifactLimit = 50
	// sweLandingReserve is how much of the leaf's clock is kept back from the
	// engine's own --max-hours, so the engine reaches its ceiling and writes a
	// checkpoint instead of being killed mid-merge by our deadline.
	sweLandingReserve = 5 * time.Minute
)

// NewSWE builds the worker. The workspace is the leaf's own directory and the
// engine's repository both — this executor does not make a second one, because
// a coding change that lands somewhere the rest of the job cannot see has not
// landed.
func NewSWE(workspace *Workspace, model, apiKey, baseURL string, deadline time.Duration) *SWE {
	if deadline <= 0 {
		deadline = SubharnessFor(SWESubharness).Deadline(0)
	}
	binary, err := os.Executable()
	if err != nil {
		// Nothing can be spawned without it, and discovering that at spawn time
		// with an empty argv[0] reads as a mysterious exec failure. Empty here
		// is refused loudly in Run.
		binary = ""
	}
	return &SWE{
		workspace: workspace, model: strings.TrimSpace(model),
		apiKey: strings.TrimSpace(apiKey), baseURL: strings.TrimSpace(baseURL),
		deadline: deadline, maxCost: DefaultSWEMaxCost, binary: binary,
		pollEvery: sweControlPoll,
	}
}

// WithMaxCost sets the engine's dollar ceiling for one leaf. Zero and below
// keep the default: a ceiling of nothing is not a policy anybody meant.
func (s *SWE) WithMaxCost(usd float64) *SWE {
	if usd > 0 {
		s.maxCost = usd
	}
	return s
}

func (s *SWE) Subharness() string { return SWESubharness }

// Run drives one engine process from start to terminal line.
func (s *SWE) Run(ctx context.Context, task Task) (*Outcome, error) {
	started := time.Now()
	outcome := &Outcome{Stop: StopDone}
	if s.workspace == nil {
		return nil, fmt.Errorf("node %d: the swe worker was built without a workspace", task.NodeID)
	}
	if s.binary == "" {
		return nil, fmt.Errorf("node %d: the swe worker cannot find its own executable to re-exec", task.NodeID)
	}

	runCtx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	trace := newTracer(s.workspace, task.NodeID)
	defer trace.close()

	directory := s.workspace.Root()
	// The leaf's spilled output and its own flight recorder both live in the
	// workspace, and the engine audits the change set it finds there. It
	// git-excludes its own sidecars; ours get the same treatment, or a
	// 47,000-line trace of the run shows up in the diff and the auditor —
	// correctly — refuses to ship it. Both directories are named because the
	// recorder moved out of .obs and an exclusion that covered it by accident
	// would stop covering it silently. They are handed to the initializer
	// rather than written after it, because the recorder is already open by now
	// and an exclusion that arrives after the baseline commit excludes nothing.
	initialized, err := ensureGitRepository(runCtx, directory, obsDir+"/", traceDir+"/")
	if err != nil {
		outcome.Stop = StopError
		outcome.Text = err.Error()
		trace.note("workspace: " + err.Error())
		return s.land(ctx, task, outcome, started, repoState{}), fmt.Errorf("node %d: %w", task.NodeID, err)
	}
	if initialized {
		trace.note("workspace: no committed git repository here — initialised one and committed a baseline")
	} else {
		trace.note("workspace: an existing git repository, run in place")
	}
	before := readRepoState(runCtx, directory)

	goal := sweGoal(task)
	resuming := resumableCheckpoint(directory)
	argv := s.argv(directory, goal, resuming)
	trace.note("engine: " + s.binary + " " + strings.Join(argv[:len(argv)-2], " ") + " -- <goal>")
	if resuming {
		trace.note("engine: a resume checkpoint was found — continuing the previous run rather than starting over")
	}

	command := exec.Command(s.binary, argv...)
	command.Dir = directory
	command.Env = s.environ(directory)
	// Its own process group, so a cancel reaches the engine's own children —
	// the auto-resume supervisor re-execs this binary again, and a TERM to the
	// leader alone would leave the grandchild running against a leaf nobody is
	// waiting for any more.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr := &tailWriter{limit: sweStderrTail}
	command.Stderr = &traceTee{trace: trace, prefix: "stderr: ", also: stderr}
	pipe, err := command.StdoutPipe()
	if err != nil {
		outcome.Stop = StopError
		return s.land(ctx, task, outcome, started, before), fmt.Errorf("node %d: swe stdout: %w", task.NodeID, err)
	}
	if err := command.Start(); err != nil {
		outcome.Stop = StopError
		return s.land(ctx, task, outcome, started, before), fmt.Errorf("node %d: swe start: %w", task.NodeID, err)
	}

	lines := make(chan string, 256)
	go func() {
		defer guard.Recover("exec/swe stdout")
		defer close(lines)
		reader := bufio.NewReaderSize(pipe, 128*1024)
		for {
			line, readErr := reader.ReadString('\n')
			if strings.TrimSpace(line) != "" {
				lines <- line
			}
			if readErr != nil {
				return
			}
		}
	}()

	state := &sweRun{task: task, trace: trace, outcome: outcome}
	stopped := StopReason("")
	killed := false
	// The kill timer outlives the polite request and must not outlive the
	// child: a pid group is recycled, and a SIGKILL fired ten seconds after the
	// process it was meant for has been reaped is a signal to a stranger.
	var hard *time.Timer
	defer func() {
		if hard != nil {
			hard.Stop()
		}
	}()
	kill := func(reason StopReason, note string) {
		if killed {
			return
		}
		killed = true
		stopped = reason
		trace.note(note)
		hard = s.signalGroup(command, trace)
	}

	ticker := time.NewTicker(s.pollEvery)
	defer ticker.Stop()
	expired := runCtx.Done()
	for open := true; open; {
		select {
		case line, more := <-lines:
			if !more {
				open = false
				break
			}
			state.consume(line)
			if action, note := state.poll(); action != ControlNone {
				kill(stopFor(action), note)
			}
		case <-ticker.C:
			// The tick is the trace's record boundary. Every NDJSON line the
			// engine emits is traced and a busy stream is a thousand a second,
			// so the lines are batched and landed here — one flush per poll
			// interval, which is what a crash can cost and what someone reading
			// the file during a run has to wait for.
			trace.flush()
			if action, note := state.poll(); action != ControlNone {
				kill(stopFor(action), note)
			}
		case <-expired:
			expired = nil
			kill(StopDeadline, "the leaf's wall clock ran out — stopping the engine")
		}
	}
	waitErr := command.Wait()

	return s.settle(ctx, task, state, started, before, stopped, waitErr, stderr.String())
}

// stopFor names the ending a control action produces. Pause and cancel kill the
// same way — the engine checkpoints on the way down either way — and differ
// only in what the store is told, which is exactly the difference between "come
// back to this" and "this is over".
func stopFor(action ControlAction) StopReason {
	if action == ControlPause {
		return StopPaused
	}
	return StopCancelled
}

// settle turns a finished child into an Outcome. It is one function rather than
// six returns for linear.land's reason: there are several ways out of the loop
// above and a verdict set on some of them is worse than none at all.
func (s *SWE) settle(
	ctx context.Context, task Task, state *sweRun, started time.Time,
	before repoState, stopped StopReason, waitErr error, stderrTail string,
) (*Outcome, error) {
	outcome := state.outcome
	outcome.Usage = state.usage()
	outcome.Turns = state.turns
	outcome.ToolCalls = state.turns

	// A user's decision outranks whatever the engine managed to say on its way
	// down: a killed run may still have flushed a terminal line, and reporting
	// that as the leaf's ending would make a cancel look like a failure.
	switch stopped {
	case StopCancelled, StopPaused, StopDeadline:
		outcome.Stop = stopped
		outcome.Text = state.text(stopped, nil)
		return s.land(ctx, task, outcome, started, before), nil
	}

	terminal := state.terminal
	if terminal == nil {
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictProviderFailure
		reason := strings.TrimSpace(stderrTail)
		if reason == "" && waitErr != nil {
			reason = waitErr.Error()
		}
		if reason == "" {
			reason = "it exited without saying how it went"
		}
		outcome.Text = state.text(StopError, nil)
		state.trace.note("engine: died with no terminal event — " + reason)
		return s.land(ctx, task, outcome, started, before),
			fmt.Errorf("node %d: the coding pipeline stopped without a verdict: %s", task.NodeID, reason)
	}

	outcome.Usage.Cost = terminal.cost()
	reason := strings.TrimSpace(terminal.Message)
	if reason == "" {
		reason = "it gave no reason"
	}
	var runErr error
	switch terminal.Status {
	case "pass":
		// The one executor in the process that is allowed to say this. The
		// engine's audit gate plus the repository's own build and tests are a
		// real verifier, which is precisely the condition linear.go:794-795
		// reserves a self-set verdict for.
		outcome.Stop = StopDone
		outcome.Verdict = provider.VerdictVerifiedSuccess
	case "budget-exhausted", "budget-exhausted-retries":
		// Not a failure — a leaf that was still working when the money ran out.
		// Exhausted is what the overrun-continuation replan reads.
		outcome.Stop = StopBudget
		outcome.Exhausted = StopBudget
	case "refused":
		// The engine's intake gate declined the goal. That is a fact about the
		// choice that routed the work here, not about the model that would have
		// done it, so it must neither grade nor buy an escalation — and
		// unverified success is this codebase's only inert "nothing was
		// checked" verdict.
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictUnverifiedSuccess
		runErr = fmt.Errorf("node %d: the coding pipeline declined this goal: %s", task.NodeID, reason)
	case "escalated", "fail":
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictSemanticFailure
		runErr = fmt.Errorf("node %d: the coding pipeline could not finish (%s): %s",
			task.NodeID, terminal.Status, reason)
	default:
		// "crashed", and anything a later engine adds. A crash is the harness
		// falling over rather than the model failing, so it is weather.
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictProviderFailure
		runErr = fmt.Errorf("node %d: the coding pipeline crashed: %s", task.NodeID, reason)
	}
	outcome.Text = state.text(outcome.Stop, terminal)
	s.calibrate(outcome, state.fit, terminal.Status, time.Since(started))
	return s.land(ctx, task, outcome, started, before), runErr
}

// calibrate is this worker saying, in its own words, how the job it just did sat
// against what it is built for.
//
// It is written here and nowhere else because the evidence is here and nowhere
// else: the engine's root-cut band, its intake classification and its audit
// ceiling are facts the pipeline produced on the way past, and no reader further
// down the graph could reconstruct any of them from a cost and a verdict. The
// sentences go into the profile record and from there into the recalibration
// call that rewrites this worker's three anchor examples — which is the whole
// mechanism by which the boundary between the generalist and this worker moves
// on measurement instead of on the paragraph somebody wrote before it had ever
// run.
//
// Every note is about this worker's own envelope. None of them names another
// worker or asks for one: choosing is the compiler's job and the judge's, and a
// note that made the choice would be an executor deciding what reaches it.
func (s *SWE) calibrate(outcome *Outcome, fit sweFit, status string, elapsed time.Duration) {
	// Under the floor. The engine judged the whole goal small enough to run as
	// one coder leaf with no plan at all, or its intake read the issue as
	// trivial — either way the planning and auditing this worker exists for was
	// overhead on this job.
	switch {
	case fit.rootCut:
		band := fit.band
		if band == "" {
			band = "unstated"
		}
		outcome.Calibrate("the engine judged this goal small enough to run whole, with no plan at all " +
			"(root-cut: " + band + ") — a lighter worker may have sufficed")
	case fit.class == "trivial":
		outcome.Calibrate("the engine's intake read this issue as trivial — a lighter worker may have sufficed")
	}

	// Far under budget on both clocks. One of the two alone is ordinary — a
	// cheap run can still take an hour, and a fast one can still cost — so the
	// note is spent only when the job finished well inside both.
	if s.maxCost > 0 && s.deadline > 0 && outcome.Usage.Cost > 0 &&
		outcome.Usage.Cost < sweUnderBudgetShare*s.maxCost &&
		elapsed < time.Duration(float64(s.deadline)*sweUnderBudgetShare) {
		outcome.Calibrate(fmt.Sprintf(
			"it finished on $%.4f of a $%s ceiling in %s of %s — far inside this worker's envelope; "+
				"a lighter worker may have sufficed",
			outcome.Usage.Cost, trimFloat(s.maxCost),
			elapsed.Round(time.Second), s.deadline.Round(time.Second)))
	}

	// The other end. A run that spent its ceiling, or an audit loop that used
	// every cycle it had, is this worker at full stretch — and a ruler rewritten
	// without that half would learn only that the boundary is too high.
	switch status {
	case "budget-exhausted", "budget-exhausted-retries":
		outcome.Calibrate("it spent its whole cost ceiling and was still working — " +
			"this sat at the top of this worker's envelope")
	}
	if fit.auditMax > 0 && fit.auditCycle >= fit.auditMax {
		outcome.Calibrate(fmt.Sprintf(
			"the audit needed every one of its %d cycles — this sat at the top of this worker's envelope",
			fit.auditMax))
	}
}

// sweUnderBudgetShare is how little of a ceiling counts as "far inside". A fifth
// is deliberately generous in the safe direction: a run that used a quarter of
// its money and half its clock is an ordinary comfortable run, and a note that
// fired on it would push the ruler down on evidence that says nothing.
const sweUnderBudgetShare = 0.2

// land collects what the leaf left behind and grades it, exactly as linear.land
// does. verdictFor is shared deliberately: a verdict this executor did not set
// itself must be read by the same law every other leaf is read by.
func (s *SWE) land(ctx context.Context, task Task, outcome *Outcome, started time.Time, before repoState) *Outcome {
	if extra := s.recordArtifacts(ctx, task, before); extra > 0 {
		outcome.Text = strings.TrimSpace(outcome.Text) +
			fmt.Sprintf("\n\n(%d further changed files are named in the run's trace rather than here)", extra)
	}
	outcome.Artifacts = s.workspace.Artifacts(task.NodeID)
	outcome.Elapsed = time.Since(started)
	outcome.Verdict = verdictFor(outcome)
	provider.Report(ctx, outcome.Verdict)
	return outcome
}

// recordArtifacts names the files this run changed and returns how many were
// left out of the bounded list.
//
// It is a before/after read rather than a plain `git status`, because the
// engine commits: it merges each judged worktree onto the branch, so at the end
// of a successful run the working tree is frequently clean and the whole change
// lives between two commits. Reading only the porcelain would have reported a
// finished refactor as having touched nothing.
func (s *SWE) recordArtifacts(ctx context.Context, task Task, before repoState) int {
	directory := s.workspace.Root()
	after := readRepoState(ctx, directory)
	if after.top == "" {
		return 0
	}
	changed := map[string]bool{}
	for path := range after.dirty {
		if !before.dirty[path] {
			changed[path] = true
		}
	}
	if before.head != "" && after.head != "" && before.head != after.head {
		for _, path := range gitLines(ctx, directory, "diff", "--name-only", before.head, after.head) {
			changed[path] = true
		}
	}
	// git speaks in paths relative to the repository's top, and the repository
	// may be an ancestor of the workspace — a leaf working in one directory of
	// a monorepo must not claim its siblings' files. It may also be spelled
	// differently: on macOS /var is a symlink to /private/var, so the same
	// directory has two honest names and a naive Rel between them escapes.
	top, err := filepath.EvalSymlinks(after.top)
	if err != nil {
		top = after.top
	}
	real, err := filepath.EvalSymlinks(directory)
	if err != nil {
		real = directory
	}
	paths := make([]string, 0, len(changed))
	for path := range changed {
		inside, err := filepath.Rel(real, filepath.Join(top, path))
		if err != nil || strings.HasPrefix(inside, "..") || sweSidecar(inside) {
			continue
		}
		paths = append(paths, inside)
	}
	sort.Strings(paths)
	overflow := 0
	if len(paths) > sweArtifactLimit {
		overflow = len(paths) - sweArtifactLimit
		paths = paths[:sweArtifactLimit]
	}
	for _, path := range paths {
		s.workspace.Record(task.NodeID, filepath.Join(directory, path))
	}
	return overflow
}

// sweSidecar names the engine's own bookkeeping. It git-excludes these itself,
// but a workspace that was already a repository may not, and a checkpoint file
// listed as a deliverable is noise in every downstream context.
func sweSidecar(path string) bool {
	head, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	switch head {
	case ".codeaf", ".plandb.db", ".obs", ".aforge":
		return true
	}
	return strings.HasPrefix(head, ".plandb.db")
}

// argv is the engine's command line. The goal goes last, behind `--`, so a
// brief that opens with a dash is a goal rather than an unknown option.
func (s *SWE) argv(directory, goal string, resuming bool) []string {
	command := "run"
	if resuming {
		command = "resume"
	}
	argv := []string{command, "--dir", directory, "--format", "json"}
	if pool := enginePool(s.model); pool != "" {
		// One pinned model in both pools. aforge already decided what this leaf
		// runs on — work model, boost, or an escalation rung — and a specialist
		// that quietly substituted its own vendor defaults would make the
		// router's ledger a record of models nobody chose.
		argv = append(argv, "--high", pool, "--low", pool)
	}
	argv = append(argv,
		"--max-cost", trimFloat(s.maxCost),
		"--max-hours", trimFloat(s.maxHours()),
		"--", goal)
	return argv
}

// maxHours keeps a landing reserve back from the leaf's own clock, so the
// engine crosses its ceiling — and checkpoints — before our deadline kills it.
func (s *SWE) maxHours() float64 {
	budget := s.deadline - sweLandingReserve
	if reserve := s.deadline / 10; sweLandingReserve > reserve {
		budget = s.deadline - reserve
	}
	if budget < time.Minute {
		budget = time.Minute
	}
	return budget.Hours()
}

// environ is the child's whole environment: this process's, with the pins that
// make it the engine written over the top.
//
// Inheriting rather than composing from nothing is deliberate — the child needs
// HOME for its git identity and credential helpers, PATH for git itself, and
// TMPDIR for its worktrees, and a list of "the four variables an engine needs"
// is a list that is wrong the first time the engine grows a fifth.
func (s *SWE) environ(directory string) []string {
	pinned := map[string]string{
		sweproSentinelEnv: "1",
		// The engine refuses to run without an AgentField control plane
		// answering. Embedded, there is nobody to answer: aforge is the plane.
		// "off" is the one value the embedding patch added to that gate.
		"CODEAF_CP_URL":      "off",
		"OPENROUTER_API_KEY": s.apiKey,
		// The plandb singleton, kept beside the run it belongs to. This is the
		// path the engine's own resume arm picks when the variable is unset, so
		// setting it explicitly changes nothing except that a run and its
		// resume cannot disagree about where the database was.
		"PLANDB_DB": filepath.Join(directory, ".plandb.db"),
	}
	if s.baseURL != "" {
		pinned["OPENROUTER_BASE_URL"] = s.baseURL
	}
	// The engine's LLM auditor is off unless the operator turns it on. aforge
	// already owns a taste layer — the delivery gate — and stacking a second
	// judge inside the engine, at whatever tier the leaf's model happens to
	// be, measured as a judge that never signs: four issues, real work
	// committed, mechanical checks green, every verdict a refusal. The
	// engine's machine verification (build, tests, discovered CI) stays on;
	// what it can prove it still proves.
	if os.Getenv("CODEAF_AUDITOR") == "" {
		pinned["CODEAF_AUDITOR"] = "0"
	}
	environ := os.Environ()
	kept := make([]string, 0, len(environ)+len(pinned)+len(s.extraEnv))
	venvBin := projectVenvBin(directory)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := pinned[name]; overridden {
			continue
		}
		// The project's own venv outranks the machine's PATH for the child. The
		// engine verifies by running the commands it discovers — `pytest`, not
		// `.venv/bin/pytest` — and a generalist agent would have found the venv
		// itself where the engine mechanically takes the first interpreter PATH
		// offers. A repository that carries its toolchain gets judged by it.
		if venvBin != "" && name == "PATH" {
			entry = "PATH=" + venvBin + string(os.PathListSeparator) + entry[len("PATH="):]
		}
		kept = append(kept, entry)
	}
	names := make([]string, 0, len(pinned))
	for name := range pinned {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		kept = append(kept, name+"="+pinned[name])
	}
	return append(kept, s.extraEnv...)
}

// projectVenvBin is the workspace's own Python toolchain, if it carries one
// under either conventional name. Empty when it does not, which leaves the
// child's PATH exactly as inherited.
func projectVenvBin(directory string) string {
	for _, name := range []string{".venv", "venv"} {
		bin := filepath.Join(directory, name, "bin")
		if info, err := os.Stat(bin); err == nil && info.IsDir() {
			return bin
		}
	}
	return ""
}

// signalGroup takes the whole tree down politely, then not politely. The group
// signal is what reaches the engine's auto-resume grandchild; the fallback to
// the leader alone is for the case Darwin refuses a group signal in, which
// exec/services.go already learned the hard way.
// It returns the hard-kill timer so the caller can stop it the moment the child
// is reaped — a SIGKILL that arrives after a pid has been recycled belongs to
// somebody else's process.
func (s *SWE) signalGroup(command *exec.Cmd, trace *tracer) *time.Timer {
	if command.Process == nil {
		return nil
	}
	pid := command.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		_ = command.Process.Signal(syscall.SIGTERM)
	}
	return time.AfterFunc(sweTerminateGrace, func() {
		defer guard.Recover("exec/swe kill")
		trace.note("engine: it did not stop when asked — killing the process group")
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			_ = command.Process.Kill()
		}
	})
}

// enginePool translates aforge's model id into the engine's.
//
// aforge names a model `<vendor>/<model>`; the engine names it
// `openrouter/<vendor>/<model>` (internal/swepro/codeaf/args.go's
// defaultHighModels is the pinned example). A leading `~` is aforge's
// panel-file marker and is not part of any model's name.
func enginePool(model string) string {
	model = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "~"))
	if model == "" {
		return ""
	}
	if strings.HasPrefix(model, "openrouter/") {
		return model
	}
	return "openrouter/" + model
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// sweGoal composes the instruction, in linear's order and for linear's reason:
// the goal orients it, the inputs are the only upstream work it may know about,
// and its own brief comes last so it is the freshest thing in the prompt.
//
// The one paragraph linear adds that this does not is the share invitation. A
// swe leaf has no share tool — its siblings hear from it through milestones the
// executor posts, not through anything the engine can call — and an instruction
// to use a tool that does not exist is how a run spends a cycle looking for it.
func sweGoal(task Task) string {
	var block strings.Builder
	if goal := strings.TrimSpace(task.Goal); goal != "" {
		fmt.Fprintf(&block, "This work is part of a larger goal:\n%s\n\n", goal)
	}
	if len(task.Inputs) > 0 {
		block.WriteString("Results from earlier work, which you already have and must not gather again. " +
			"Where two of them speak to the same quantity, the LATER one stands: a corrected figure " +
			"replaces its predecessor, and reaching back past a correction to the number it corrected " +
			"is the one way to be wrong with everything you need in hand:\n")
		for _, input := range task.Inputs {
			fmt.Fprintf(&block, "\n=== from %q ===\n%s\n", input.Title, input.Result)
			if len(input.Artifacts) > 0 {
				fmt.Fprintf(&block, "(files: %s — read them if you need the full detail)\n", strings.Join(input.Artifacts, ", "))
			}
		}
		block.WriteString("\n")
	}
	block.WriteString("Your work:\n")
	block.WriteString(task.Brief)
	block.WriteString(outputClause(task))
	return block.String()
}

// ── the workspace's git precondition ─────────────────────────────────────────

// repoState is what the repository looked like at one instant: which commit it
// was on and which paths were dirty.
type repoState struct {
	top   string
	head  string
	dirty map[string]bool
}

func readRepoState(ctx context.Context, directory string) repoState {
	state := repoState{dirty: map[string]bool{}}
	tops := gitLines(ctx, directory, "rev-parse", "--show-toplevel")
	if len(tops) == 0 {
		return state
	}
	state.top = tops[0]
	if heads := gitLines(ctx, directory, "rev-parse", "HEAD"); len(heads) > 0 {
		state.head = heads[0]
	}
	for _, line := range gitLines(ctx, directory, "status", "--porcelain") {
		if path := porcelainPath(line); path != "" {
			state.dirty[path] = true
		}
	}
	return state
}

// porcelainPath reads the path out of one `git status --porcelain` line. A
// rename carries both sides as "old -> new"; the new one is what changed.
func porcelainPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	path := strings.TrimSpace(line[3:])
	if _, renamed, found := strings.Cut(path, " -> "); found {
		path = renamed
	}
	return strings.Trim(strings.TrimSpace(path), `"`)
}

// ensureGitRepository makes the workspace something the engine can run in. It
// reports whether it had to create the repository, because that fact belongs in
// the trace: a leaf that ran against a repository it made itself has no history
// to reason from, and a reader of the trace should not have to guess.
// excludeFromGit appends a pattern to the repository's local exclude file,
// once. Local means .git/info/exclude: invisible to the diff, gone with the
// clone, and never an edit to anything the repository tracks.
// excludeFromGit adds one pattern to the repository's untracked-file exclusions.
//
// The path comes from git rather than from string arithmetic. `.git` is a
// directory in an ordinary clone and a file pointing elsewhere in a worktree,
// and the previous version — which stat'd `.git` and refused anything that was
// not a directory — therefore did nothing at all in a worktree, silently, on
// the layout a person is most likely to hand a coding worker. `rev-parse
// --git-path info/exclude` answers with the file git will actually read in
// either layout, and answers with an error outside a repository, which is the
// one case where doing nothing is still right.
//
// info/exclude, never .gitignore: the repository's tracked files are the
// deliverable and are not ours to edit.
func excludeFromGit(ctx context.Context, directory, pattern string) {
	lines := gitLines(ctx, directory, "rev-parse", "--git-path", "info/exclude")
	if len(lines) == 0 {
		return
	}
	path := strings.TrimSpace(lines[0])
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	if existing, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			if strings.TrimSpace(line) == pattern {
				return
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer handle.Close()
	fmt.Fprintln(handle, pattern)
}

// ensureGitRepository leaves the workspace as a repository with at least one
// commit, and reports whether it had to make one.
//
// exclude names the harness's own directories, and it is applied here rather
// than by the caller afterwards because of when the baseline is written. The
// leaf's flight recorder is opened before this runs — it has to be, or the
// repository work itself goes unrecorded — so by the time `add -A` sees the
// workspace the trace file already exists. Excluding it afterwards is too late
// twice over: it is in the baseline commit, and git will keep reporting its
// modifications because exclusions only ever apply to untracked files. So the
// patterns go in between the repository existing and anything being staged,
// which is the one window where they do what they are for.
func ensureGitRepository(ctx context.Context, directory string, exclude ...string) (bool, error) {
	committed := gitQuiet(ctx, directory, "rev-parse", "--verify", "HEAD") == nil
	if !committed && gitQuiet(ctx, directory, "rev-parse", "--git-dir") != nil {
		if err := gitQuiet(ctx, directory, "init"); err != nil {
			return false, fmt.Errorf("the swe worker needs a git repository and could not make one here: %w", err)
		}
	}
	for _, pattern := range exclude {
		excludeFromGit(ctx, directory, pattern)
	}
	if committed {
		// An existing repository keeps its own history; it needed the
		// exclusions above and nothing else.
		return false, nil
	}
	if err := gitQuiet(ctx, directory, "add", "-A"); err != nil {
		return false, fmt.Errorf("the swe worker could not stage the workspace: %w", err)
	}
	// An identity on the command line rather than in the user's config: this
	// commit is the harness's own baseline and must not teach a person's
	// repository who they are.
	if err := gitQuiet(ctx, directory,
		"-c", "user.name=aforge",
		"-c", "user.email=agentfield-bot@users.noreply.github.com",
		"-c", "commit.gpgsign=false",
		"commit", "--allow-empty", "--no-verify",
		"-m", "aforge: baseline before the swe worker ran",
	); err != nil {
		return false, fmt.Errorf("the swe worker could not commit a baseline: %w", err)
	}
	return true, nil
}

func gitQuiet(ctx context.Context, directory string, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	return command.Run()
}

func gitLines(ctx context.Context, directory string, args ...string) []string {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	out, err := command.Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if trimmed := strings.TrimRight(line, "\r"); strings.TrimSpace(trimmed) != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// sweResumableStatuses is internal/swepro/codeaf/resume.go's own table, read
// from outside. The engine decides for itself whether a checkpoint is
// resumable; this is only the question of whether to ask it, and asking when
// the answer is no costs a process that prints "nothing to continue" and exits
// zero with no terminal event — which this executor would have to report as a
// death.
var sweResumableStatuses = map[string]bool{
	"fail": true, "crashed": true, "escalated": true,
	"budget-exhausted": true, "budget-exhausted-retries": true,
}

// resumableCheckpoint reports whether a previous engine run left something
// worth continuing. This is the earned advantage docs/SUBHARNESSES.md promises
// a restarted swe leaf: the node restarts like any other node, and resumes the
// engine instead of starting over.
func resumableCheckpoint(directory string) bool {
	raw, err := os.ReadFile(filepath.Join(directory, ".codeaf", "resume-checkpoint.json"))
	if err != nil {
		return false
	}
	var row struct {
		Goal        string `json:"goal"`
		FinalStatus string `json:"finalStatus"`
	}
	if json.Unmarshal(raw, &row) != nil || strings.TrimSpace(row.Goal) == "" {
		return false
	}
	return sweResumableStatuses[row.FinalStatus]
}

// ── the event stream ─────────────────────────────────────────────────────────

// sweEvent is both families of line at once.
//
// The engine writes two shapes on one pipe (internal/swepro/EVENTS-CONTRACT.md):
// its own compact control-plane records — {type, stage, status, message, data} —
// and the raw instance-bus payloads — {id, type, properties}. Decoding into one
// struct and discriminating on which half is populated is honest here because
// the two shapes share no field names; the alternative is deciding what a line
// is by looking at it twice.
type sweEvent struct {
	Type       string          `json:"type"`
	Stage      string          `json:"stage"`
	Status     string          `json:"status"`
	Message    string          `json:"message"`
	SessionID  string          `json:"session_id"`
	Data       map[string]any  `json:"data"`
	Properties json.RawMessage `json:"properties"`
}

func (e sweEvent) number(key string) float64 {
	if value, ok := e.Data[key].(float64); ok {
		return value
	}
	return 0
}

func (e sweEvent) count(key string) int { return int(e.number(key)) }

func (e sweEvent) cost() float64 { return e.number("cost_usd") }

// sweRun is one child process's running state: what it has said, what it has
// cost, and what the leaf has been told to do about it.
type sweRun struct {
	task    Task
	trace   *tracer
	outcome *Outcome

	terminal *sweEvent
	turns    int
	// tokens is keyed by assistant message id and holds the last figure that
	// message reported. The engine republishes a message as it streams, each
	// time with the running totals, so summing every update would count the
	// same tokens several times over.
	tokens map[string]sweTokens
	// steered is every line the user sent that this run could not act on.
	steered []string
	// polled throttles the between-lines poll: a stream can deliver a thousand
	// deltas a second and a store query per delta is a store query too many.
	polled time.Time

	// fit is what the engine said about the size of the job it was handed, kept
	// as it streams past because the sentences that use it are written at the
	// end. None of it changes what runs — it is read once, in settle, and turned
	// into prose for the ruler that decides what reaches this worker next time.
	fit sweFit
}

// sweFit is the engine's own sizing judgements, harvested from the stream.
type sweFit struct {
	// rootCut is the strongest signal there is: the engine looked at the whole
	// goal and decided it was one leaf, no decomposition at all.
	rootCut bool
	// band is the sizing estimate the cut was made from ("xs", "s", "m", …).
	band string
	// class is the intake classifier's reading: trivial, focused or vague.
	class string
	// auditCycle and auditMax are the last audit verdict's position against its
	// own ceiling. Equal means the audit loop used everything it had.
	auditCycle, auditMax int
}

type sweTokens struct {
	prompt, completion, cached int
}

func (r *sweRun) usage() Usage {
	usage := Usage{Calls: r.turns}
	for _, tokens := range r.tokens {
		usage.PromptTokens += tokens.prompt
		usage.CompletionTokens += tokens.completion
		usage.CachedTokens += tokens.cached
	}
	return usage
}

// consume reads one NDJSON line. Every line reaches the trace — the trace is
// the flight recorder and a filtered recorder answers the question it was
// filtered for and no other — and only the few that mean something reach a
// person.
func (r *sweRun) consume(line string) {
	r.trace.note(strings.TrimRight(line, "\n"))
	var event sweEvent
	if json.Unmarshal([]byte(line), &event) != nil {
		return
	}
	switch event.Type {
	case "terminal":
		captured := event
		r.terminal = &captured
	case "stage", "supervisor":
		r.stage(event)
	case "message.updated":
		r.countTokens(event)
	}
}

// stage turns one engine stage into within-node visibility. Progress is
// replaceable and may say anything; Share is a message to the rest of the job
// and is spent only on milestones — docs/SUBHARNESSES.md's "one mouth" is the
// whole reason this is two channels and not one.
func (r *sweRun) stage(event sweEvent) {
	name := event.Stage
	if name == "" {
		name = event.Type
	}
	r.noteFit(event)
	r.record(name + " " + event.Status)
	done, latest := sweLatest(event)
	r.task.progress(swePhase(name), done, 0, latest)
	if milestone := sweMilestone(event); milestone != "" && r.task.Share != nil {
		_ = r.task.Share(milestone)
	}
}

// noteFit collects the engine's own judgements about the size of this job as
// they go past. It reads three stages and changes nothing: the pipeline had
// already decided all of this to run the job, and the only new thing here is
// that the decisions survive the run instead of scrolling away in the trace.
func (r *sweRun) noteFit(event sweEvent) {
	switch event.Stage {
	case "root-cut":
		if band, ok := event.Data["band"].(string); ok {
			r.fit.band = strings.TrimSpace(band)
		}
		// "selected" is the arm where the engine ran the goal as a single coder
		// leaf rather than planning it into a DAG.
		r.fit.rootCut = event.Status == "selected"
	case "classifier":
		switch event.Status {
		case "trivial", "focused", "vague":
			r.fit.class = event.Status
		}
	case "audit":
		if cycle := event.count("cycle"); cycle > 0 {
			r.fit.auditCycle = cycle
		}
		if ceiling := event.count("max_cycles"); ceiling > 0 {
			r.fit.auditMax = ceiling
		}
	}
}

// record keeps the bounded tail of what the engine actually did, in the shape
// Outcome.Ran is read in everywhere else: the last forty things, in order.
func (r *sweRun) record(line string) {
	r.outcome.Ran = append(r.outcome.Ran, strings.TrimSpace(line))
	if len(r.outcome.Ran) > ranLimit {
		r.outcome.Ran = r.outcome.Ran[len(r.outcome.Ran)-ranLimit:]
	}
}

// countTokens reads the assistant message's own accounting. The engine's cost
// is authoritative and comes from the terminal line; this is the token half,
// which nothing else in the stream carries.
func (r *sweRun) countTokens(event sweEvent) {
	if len(event.Properties) == 0 {
		return
	}
	var payload struct {
		Info struct {
			ID     string `json:"id"`
			Role   string `json:"role"`
			Tokens struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
				Cache  struct {
					Read float64 `json:"read"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"info"`
	}
	if json.Unmarshal(event.Properties, &payload) != nil {
		return
	}
	info := payload.Info
	if info.Role != "assistant" || info.ID == "" {
		return
	}
	if r.tokens == nil {
		r.tokens = map[string]sweTokens{}
	}
	if _, seen := r.tokens[info.ID]; !seen {
		r.turns++
	}
	r.tokens[info.ID] = sweTokens{
		prompt:     int(info.Tokens.Input),
		completion: int(info.Tokens.Output),
		cached:     int(info.Tokens.Cache.Read),
	}
}

// poll reads control and steering at an event boundary. It answers with the
// action and the line to write into the trace, so the caller owns the killing
// and this owns the reading.
func (r *sweRun) poll() (ControlAction, string) {
	if r.polled.After(time.Now().Add(-swePollFloor)) {
		return ControlNone, ""
	}
	r.polled = time.Now()
	r.drainSteering()
	if r.task.Control == nil {
		return ControlNone, ""
	}
	switch r.task.Control() {
	case ControlCancel:
		return ControlCancel, "cancel requested — stopping the engine"
	case ControlPause:
		return ControlPause, "pause requested — stopping the engine; its checkpoint makes resuming cheap"
	}
	return ControlNone, ""
}

// swePollFloor keeps the store query rate sane on a chatty stream.
const swePollFloor = 500 * time.Millisecond

// drainSteering empties the mailbox so nothing the user said is lost, even
// though v1 cannot put it in front of the engine mid-run.
//
// The dishonest version of this function is the one that does not exist: a
// mailbox nobody drains looks, from the store's side, exactly like a mailbox
// whose contents were delivered. Reading the lines, writing them to the trace,
// and carrying them into the Outcome's tail is the difference between "we could
// not act on this" and silence.
func (r *sweRun) drainSteering() {
	if r.task.Steer == nil {
		return
	}
	for _, guidance := range r.task.Steer() {
		if guidance = strings.TrimSpace(guidance); guidance == "" {
			continue
		}
		r.trace.note("steered (received, not injectable mid-run): " + guidance)
		r.steered = append(r.steered, guidance)
	}
}

// text is the deliverable: what the engine said it did, then one line of
// arithmetic, then anything the user said that this run could not act on.
func (r *sweRun) text(stop StopReason, terminal *sweEvent) string {
	var block strings.Builder
	if terminal != nil {
		if message := strings.TrimSpace(terminal.Message); message != "" {
			block.WriteString(message)
		}
	}
	if block.Len() == 0 {
		block.WriteString(sweEndingText(stop))
	}
	status := string(stop)
	cycles, cost := 0, 0.0
	if terminal != nil {
		status = terminal.Status
		cycles, cost = terminal.count("cycle"), terminal.cost()
	}
	fmt.Fprintf(&block, "\n\nswe: %s after %d cycles, $%.4f", status, cycles, cost)
	if len(r.steered) > 0 {
		block.WriteString("\n\nsteering received late: " + strings.Join(r.steered, " · ") +
			" — this worker cannot take guidance mid-run, so none of it was applied.")
	}
	return block.String()
}

// sweEndingText is what a run says when the engine said nothing. Each of these
// is a real ending with no message of its own attached to it.
func sweEndingText(stop StopReason) string {
	switch stop {
	case StopCancelled:
		return "The coding run was cancelled. Its checkpoint is in the workspace, so the same node can pick it up."
	case StopPaused:
		return "The coding run is paused. Its checkpoint is in the workspace, so resuming continues rather than restarts."
	case StopDeadline:
		return "The coding run ran out of wall clock. Its checkpoint is in the workspace."
	}
	return "The coding run ended without saying how it went."
}

// swePhase is the engine's stage vocabulary in the user's. The engine names its
// own machinery — root-cut, fix-generator, exit-guard — and those names are
// exactly right in the trace and exactly wrong in a progress row somebody is
// reading to find out whether their bug is fixed yet.
func swePhase(stage string) string {
	switch stage {
	case "bootstrap":
		return "preparing the repository"
	case "resume":
		return "picking up where it stopped"
	case "root-cut", "classifier", "entry-agent", "pre-gates":
		return "reading the issue"
	case "product", "architecture", "planner", "issue-writer", "plan-apply":
		return "planning the change"
	case "scheduler", "root-orchestrator", "stale-reaper":
		return "writing the code"
	case "verification":
		return "running the repository's own checks"
	case "audit", "fix-generator":
		return "auditing the result"
	case "exit-guard":
		return "recovering unfinished work"
	case "pr-ready":
		return "readying the change for review"
	case "auto-resume":
		return "resuming after a stumble"
	}
	return stage
}

// sweLatest is the short right-hand side of a progress row, and the count that
// goes with it when the engine offered one.
func sweLatest(event sweEvent) (int, string) {
	switch event.Stage {
	case "scheduler":
		switch event.Status {
		case "cycle":
			return event.count("cycle"), fmt.Sprintf("cycle %d · %d dispatched",
				event.count("cycle"), event.count("dispatched"))
		case "cycle-complete":
			return event.count("cycle"), fmt.Sprintf("cycle %d done · %d still open",
				event.count("cycle"), event.count("open"))
		}
	case "plan-apply":
		if event.Status == "completed" {
			return event.count("tasks"), fmt.Sprintf("%d tasks, %d edges",
				event.count("tasks"), event.count("edges"))
		}
	case "audit":
		return event.count("cycle"), event.Status
	}
	if event.Status == "" {
		return 0, ""
	}
	return 0, event.Status
}

// sweMilestone is the whole of what siblings and the thread hear. Everything
// else in the stream is progress, which is replaceable, or trace, which is a
// file. A milestone is a line somebody will still be reading tomorrow, so this
// list is short on purpose.
func sweMilestone(event sweEvent) string {
	switch event.Stage {
	case "classifier":
		switch event.Status {
		case "trivial", "focused", "vague":
			return "the coding pipeline read this issue as " + event.Status
		}
	case "plan-apply":
		if event.Status == "completed" {
			return fmt.Sprintf("coding plan ready — %d tasks, %d edges",
				event.count("tasks"), event.count("edges"))
		}
	case "scheduler":
		if event.Status == "cycle" && event.count("dispatched") > 0 {
			return fmt.Sprintf("coding cycle %d — %d leaves dispatched",
				event.count("cycle"), event.count("dispatched"))
		}
	case "verification":
		switch event.Status {
		case "pass":
			return "the repository's own checks passed"
		case "fail":
			return "the repository's own checks failed — " + firstSweLine(event.Message)
		}
	case "audit":
		switch event.Status {
		case "pass", "fail", "escalated":
			return fmt.Sprintf("audit verdict %s at cycle %d", event.Status, event.count("cycle"))
		}
	case "pr-ready":
		if event.Status == "completed" {
			return "the change is readied for review"
		}
	case "auto-resume":
		return "the pipeline resumed itself: " + event.Status
	}
	return ""
}

func firstSweLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return snip(line, 160)
}

// ── stderr, kept just in case ────────────────────────────────────────────────

// tailWriter keeps the last few lines of a stream. It exists for exactly one
// moment: a child that died without a terminal event, where the only account of
// what happened is whatever it managed to complain about on the way out.
type tailWriter struct {
	mutex   sync.Mutex
	limit   int
	partial string
	lines   []string
}

func (w *tailWriter) Write(payload []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.partial += string(payload)
	for {
		line, rest, found := strings.Cut(w.partial, "\n")
		if !found {
			break
		}
		w.partial = rest
		if strings.TrimSpace(line) != "" {
			w.lines = append(w.lines, strings.TrimSpace(line))
		}
		if len(w.lines) > w.limit {
			w.lines = w.lines[len(w.lines)-w.limit:]
		}
	}
	return len(payload), nil
}

func (w *tailWriter) String() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	lines := append([]string(nil), w.lines...)
	if trailing := strings.TrimSpace(w.partial); trailing != "" {
		lines = append(lines, trailing)
	}
	if len(lines) > w.limit {
		lines = lines[len(lines)-w.limit:]
	}
	return strings.Join(lines, "\n")
}

// traceTee sends the child's stderr to the flight recorder and to the tail at
// once. The engine talks to stderr about retries, catalog refreshes and
// control-plane notices — none of it structured, all of it the thing you want
// when a run made no sense.
type traceTee struct {
	trace  *tracer
	prefix string
	also   *tailWriter
	rest   string
}

func (t *traceTee) Write(payload []byte) (int, error) {
	if t.also != nil {
		_, _ = t.also.Write(payload)
	}
	t.rest += string(payload)
	for {
		line, remainder, found := strings.Cut(t.rest, "\n")
		if !found {
			break
		}
		t.rest = remainder
		if strings.TrimSpace(line) != "" {
			t.trace.note(t.prefix + line)
		}
	}
	return len(payload), nil
}
