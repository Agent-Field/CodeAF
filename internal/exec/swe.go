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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/processgroup"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/swepro/enginestate"
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
	// attribution carries the operator's settings row into the one commit this
	// worker writes with a message of its own. It is the same law the generalist
	// leaf reads (linear.go's WithAttribution), reaching the one place a coding
	// worker signs anything.
	attribution bool
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

// WithAttribution admits the attribution law into the commit this worker lands
// on the shared workspace. Off is the absence of the trailer, exactly as it is
// the absence of the paragraph for a generalist leaf.
func (s *SWE) WithAttribution(on bool) *SWE {
	s.attribution = on
	return s
}

func (s *SWE) Subharness() string { return SWESubharness }

// Mutates: this worker's deliverable is the change in the tree. See [Mutator].
func (s *SWE) Mutates() bool { return true }

// Run drives one engine process from start to terminal line.
func (s *SWE) Run(ctx context.Context, task Task) (*Outcome, error) {
	started := time.Now()
	outcome := &Outcome{Stop: StopDone}
	if s.workspace == nil {
		return nil, fmt.Errorf("node %s: the swe worker was built without a workspace", task.leafKey())
	}
	if s.binary == "" {
		return nil, fmt.Errorf("node %s: the swe worker cannot find its own executable to re-exec", task.leafKey())
	}

	// The world's own account of what this leaf leaves behind, taken before the
	// engine opens its view. The derivation below is git's and is the better
	// answer wherever it can be had; this is what stands when it cannot — a
	// workspace that is not a repository, a base that was never recorded, a git
	// that refused — and it sees a file however it was written.
	s.workspace.WatchTree(task.leafKey())

	runCtx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	trace := newTracer(s.workspace, task.leafKey())
	defer trace.close()

	// Where this leaf's engine runs, and how what it writes gets back to the
	// workspace everyone else can see. The leaf's spilled output and its own
	// flight recorder both live in the workspace, and the engine audits the
	// change set it finds there. It git-excludes its own sidecars; ours get the
	// same treatment, or a 47,000-line trace of the run shows up in the diff and
	// the auditor — correctly — refuses to ship it. Both directories are named
	// because the recorder moved out of .obs and an exclusion that covered it by
	// accident would stop covering it silently. They are handed to the
	// initializer rather than written after it, because the recorder is already
	// open by now and an exclusion that arrives after the baseline commit
	// excludes nothing. See sweview.go for what a view is and why.
	view, initialized, err := sweOpen(runCtx, s.workspace, task.leafKey(), trace)
	if err != nil {
		outcome.Stop = StopError
		outcome.Text = err.Error()
		trace.note("workspace: " + err.Error())
		return s.land(ctx, task, nil, outcome, started, repoState{}), fmt.Errorf("node %s: %w", task.leafKey(), err)
	}
	// The shared root is given back the moment this run is over, however it
	// ends. A view that never took it releases nothing.
	defer view.release()
	trace.note(view.where(initialized))
	directory := view.dir
	// The repository state is read at the SHARED root rather than at the view.
	// It is the "before" half of the artifact list, and the artifact list is
	// about what this leaf added to the workspace a person will open — which a
	// diff taken inside a checkout nobody else can see cannot answer.
	before := readRepoState(runCtx, s.workspace.Root())

	goal := sweGoal(task, view)
	resuming := resumableCheckpoint(directory)
	argv := s.argv(directory, goal, resuming)
	trace.note("engine: " + s.binary + " " + strings.Join(argv[:len(argv)-2], " ") + " -- <goal>")
	if resuming {
		trace.note("engine: a resume checkpoint was found — continuing the previous run rather than starting over")
	}

	command := exec.Command(s.binary, argv...)
	command.Dir = directory
	command.Env = s.environ(view)
	// Its own process group, so a cancel reaches the engine's own children —
	// the auto-resume supervisor re-execs this binary again, and a TERM to the
	// leader alone would leave the grandchild running against a leaf nobody is
	// waiting for any more.
	processgroup.Configure(command)
	stderr := &tailWriter{limit: sweStderrTail}
	command.Stderr = &traceTee{trace: trace, prefix: "stderr: ", also: stderr}
	pipe, err := command.StdoutPipe()
	if err != nil {
		outcome.Stop = StopError
		return s.land(ctx, task, view, outcome, started, before), fmt.Errorf("node %s: swe stdout: %w", task.leafKey(), err)
	}
	if err := command.Start(); err != nil {
		outcome.Stop = StopError
		return s.land(ctx, task, view, outcome, started, before), fmt.Errorf("node %s: swe start: %w", task.leafKey(), err)
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

	state := &sweRun{task: task, trace: trace, outcome: outcome, root: directory}
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

	return s.settle(ctx, task, view, state, started, before, stopped, waitErr, stderr.String())
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
	ctx context.Context, task Task, view *sweView, state *sweRun, started time.Time,
	before repoState, stopped StopReason, waitErr error, stderrTail string,
) (*Outcome, error) {
	outcome := state.outcome
	outcome.Usage = state.usage()
	outcome.Turns = state.turns
	outcome.ToolCalls = state.turns

	// The account is complete the moment the stream is: every file the engine
	// wrote and every check it ran has already gone past. It is attached here,
	// once, ahead of every ending below — including the ones that used to leave
	// the leaf with nothing to say — because what the work did is true whatever
	// stopped it. The engine's last word joins it when there was one.
	if state.terminal != nil {
		state.account.Final = strings.TrimSpace(state.terminal.Message)
	}
	// The engine's terminal line first, the worker's own last word behind it.
	// The order is the only defensible one — a pipeline that summarises its own
	// run outranks one of its workers — but the fallback is what makes the field
	// mean anything, because the vendored engine leaves that message EMPTY on a
	// clean pass and the harness half must not depend on the vendor to be fixed.
	if strings.TrimSpace(state.account.Final) == "" {
		state.account.Final = strings.TrimSpace(state.said)
	}
	if !state.account.Empty() {
		outcome.Account = &state.account
	}

	// A user's decision outranks whatever the engine managed to say on its way
	// down: a killed run may still have flushed a terminal line, and reporting
	// that as the leaf's ending would make a cancel look like a failure.
	//
	// The SPEND is not part of that outranking. A leaf killed at its deadline
	// spent every dollar it spent, and the ending it was given says nothing
	// about the bill; the stream's own per-message accounting is the only
	// figure that survives here, and it is labelled as such.
	switch stopped {
	case StopCancelled, StopPaused, StopDeadline:
		state.estimated = outcome.Usage.Cost > 0
		outcome.Stop = stopped
		if stopped == StopDeadline && s.deliveredAtTheBell(ctx, view, state, before) {
			// The bell caught the wrap-up, not the work. Everything a finished
			// run is judged on is already true — the repository's own checks
			// passed against this tree, the engine's audit passed on top of
			// them, and the change is on disk — so reporting "the time limit
			// was reached before anything finished" would be a false statement
			// about a workspace anyone can go and read.
			outcome.Stop = StopDone
			outcome.Verdict = provider.VerdictVerifiedSuccess
			state.landing = "The coding run reached its wall clock during wrap-up, " +
				"but the change was already written, the repository's own checks had " +
				"passed against it and the engine's audit had passed on top of them — " +
				"so it is delivered as it stands rather than discarded."
			state.trace.note("deadline: the work was finished and verified before the clock ran out — " +
				"delivering it rather than failing the leaf")
			outcome.Text = state.text(StopDone, nil)
			return s.land(ctx, task, view, outcome, started, before), nil
		}
		outcome.Text = state.text(stopped, nil)
		return s.land(ctx, task, view, outcome, started, before), nil
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
		state.estimated = outcome.Usage.Cost > 0
		outcome.Text = state.text(StopError, nil)
		state.trace.note("engine: died with no terminal event — " + reason)
		return s.land(ctx, task, view, outcome, started, before),
			fmt.Errorf("node %s: the coding pipeline stopped without a verdict: %s", task.leafKey(), reason)
	}

	// The engine's own terminal figure is authoritative when it has one. When it
	// reports nothing — some endings carry no `cost_usd` at all — the streamed
	// accounting stands in rather than zeroing a run that plainly cost money,
	// and says so in the leaf's line.
	if reported := terminal.cost(); reported > 0 {
		outcome.Usage.Cost = reported
	} else {
		state.estimated = outcome.Usage.Cost > 0
	}
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
		runErr = fmt.Errorf("node %s: the coding pipeline declined this goal: %s", task.leafKey(), reason)
	case "escalated", "fail":
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictSemanticFailure
		runErr = fmt.Errorf("node %s: the coding pipeline could not finish (%s): %s",
			task.leafKey(), terminal.Status, reason)
	default:
		// "crashed", and anything a later engine adds. A crash is the harness
		// falling over rather than the model failing, so it is weather.
		outcome.Stop = StopError
		outcome.Verdict = provider.VerdictProviderFailure
		runErr = fmt.Errorf("node %s: the coding pipeline crashed: %s", task.leafKey(), reason)
	}
	outcome.Text = state.text(outcome.Stop, terminal)
	s.calibrate(outcome, state.fit, terminal.Status, time.Since(started))
	return s.land(ctx, task, view, outcome, started, before), runErr
}

// deliveredAtTheBell reports whether a run stopped by the wall clock had
// already finished the work it was stopped in the middle of.
//
// Three things must all be true, and each rules out a different way of being
// wrong. The engine's machine verification passed most recently — the
// repository's own build and tests, run as processes, against this tree. Its
// audit passed on top of that, which is the gate that reads the change against
// the goal. And the workspace differs from where the leaf found it, because a
// green suite over an unchanged repository is a green suite over nothing at
// all: the loudest false positive available here, and the one a repository
// whose tests already passed hands out for free.
//
// It reads the repository rather than the outcome's artifact list because the
// list is assembled later, in land, and this decides what land is landing.
func (s *SWE) deliveredAtTheBell(ctx context.Context, view *sweView, state *sweRun, before repoState) bool {
	if !state.state.deliverable() {
		return false
	}
	// The leaf's own context is gone by now — that is what a deadline is — so
	// the read is made against the caller's, with a short ceiling of its own.
	// A git call that cannot answer leaves the ending exactly as it was.
	//
	// The read is of the VIEW rather than of the shared root: an isolated leaf's
	// work is still on its own branch at this point — landing it is what this
	// answer decides — and the shared root would truthfully report that nothing
	// had happened.
	read, cancel := context.WithTimeout(ctx, sweBellRead)
	defer cancel()
	after := readRepoState(read, view.dir)
	if after.top == "" {
		return false
	}
	if before.head != "" && after.head != "" && before.head != after.head {
		return true
	}
	for path := range after.dirty {
		if !before.dirty[path] && !sweSidecar(path) {
			return true
		}
	}
	return false
}

// sweBellRead bounds the one git read taken after the clock has already run
// out. It is short on purpose: nothing downstream is waiting on a better
// answer than "the tree changed" or "we could not tell".
const sweBellRead = 10 * time.Second

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
func (s *SWE) land(ctx context.Context, task Task, view *sweView, outcome *Outcome, started time.Time, before repoState) *Outcome {
	// The work comes home before it is counted. Everything below reads the
	// shared workspace — the artifact list, the sizes, the paths a person will
	// open — and for an isolated leaf none of it is true until its branch has
	// been squashed back in.
	landed := sweLanding{}
	if view != nil {
		landing := view.land(ctx, outcome.Stop == StopDone,
			sweLandingMessage(task, outcome.Text, s.attribution))
		landed = landing
		if landing.before.top != "" {
			// The change arrived as one commit, and this is the state of the
			// workspace immediately underneath it. Reading the artifact list
			// against the state an hour and three siblings ago would credit this
			// leaf with every file they landed in between.
			before = landing.before
		}
		if landing.refusal != "" {
			outcome.Text = strings.TrimSpace(outcome.Text) + "\n\n" + landing.refusal
		}
		// Damage from a run before these defences, said once where the person
		// reading the leaf's answer will see it. It is not this change and it is
		// not this leaf's to fix; it is theirs to know about.
		if view.tracked != "" {
			outcome.Text = strings.TrimSpace(outcome.Text) + "\n\n" + view.tracked
		}
	}
	// The substrate account, and it is deliberately taken AFTER the landing: the
	// change set of a node is what its work put in the tree, and until the
	// branch is squashed home the tree does not have it. See [SWE.substrate].
	paths := s.substrate(ctx, task, view, outcome, before, landed.head)
	if extra := s.recordArtifacts(task, paths); extra > 0 {
		outcome.Text = strings.TrimSpace(outcome.Text) +
			fmt.Sprintf("\n\n(%d further changed files are named in the run's trace rather than here)", extra)
	}
	// And the world's own account, read after the landing for the same reason
	// the substrate account is: until the branch is squashed home the shared
	// tree does not hold the work.
	s.workspace.RecordChanges(task.leafKey())
	outcome.Artifacts = s.workspace.Artifacts(task.leafKey())
	outcome.Elapsed = time.Since(started)
	outcome.Verdict = verdictFor(outcome)
	provider.Report(ctx, outcome.Verdict)
	return outcome
}

// substrate replaces this node's account of what it changed with the one git
// can prove, writes the change's own text somewhere a reader can open it, and
// hands back the paths for the artifact list.
//
// It exists because the two accounts of a coding leaf's change set were
// different accounts and only one of them was true across a node's whole life.
//
// The narration — the file rows [sweRun.noteFiles] cuts out of the engine's tool
// metadata as the stream goes past — is per PASS. It has to be: it is what this
// process watched happen. So a node judged, failed on the wording of its
// deliverable and run a second time reported the second run's change set, which
// on a tree where the fix had already landed was nothing at all. That is
// measured, and it is why a delivered account carried a verification story and
// zero file rows.
//
// The derivation here is per NODE, because its base is a git ref the node owns
// (see [sweView.anchor]) rather than a value this process remembers. `base..HEAD`
// is the whole of what the node changed on pass one and on pass six alike, and
// nothing about it depends on the process that computes it having been present
// for the earlier ones.
//
// Both channels stay. The narration is live progress — it is what the record
// shows while the engine is still working, when there is no commit to diff —
// and this is the account, replacing it the moment there is a repository answer.
// There is exactly one derived list: the artifact registry, the account rows and
// the patch are three renderings of it, which is what stops a reader being told
// two different change sets by two surfaces of the same run.
//
// before is the pre-run repository state and is the fallback, unchanged, for
// every case the derivation cannot reach: a workspace that is not a repository,
// a base that was never recorded, a git that refused. A leaf must still be able
// to say what it wrote.
func (s *SWE) substrate(ctx context.Context, task Task, view *sweView, outcome *Outcome,
	before repoState, passHead string) []string {
	dir, passBase, live := view.measure(before)
	if dir == "" {
		dir = s.workspace.Root()
	}
	// This pass's own range, filed in the repository under the node's name. It
	// is written before the account is read, so the read that follows includes
	// the pass that just finished.
	//
	// passHead is the landing's own observation of where the shared workspace
	// ended up, taken while the lease was still held; empty means there was no
	// landing to observe, which is every leaf that worked in the shared tree
	// directly (it still holds the lease here, so reading HEAD now is the same
	// observation) and every leaf whose work is still on its own branch.
	swePassRef(ctx, dir, task.leafKey(), passBase, passHead)
	changed, span, ok := sweChangeSet(ctx, dir, task.leafKey(), view.nodeBase(), live)
	if !ok {
		// No derivation, so the narration stands as the only account there is,
		// and the artifact list is read the way it was read before any of this:
		// what the shared workspace holds now against what it held then.
		return sweChangedPaths(ctx, s.workspace.Root(), before)
	}
	account := outcome.Account
	if account == nil && len(changed) > 0 {
		// A run that ended before settle could attach one — a start failure, a
		// death — still changed what it changed, and the tree is where that is
		// written down.
		account = &Account{}
		outcome.Account = account
	}
	if account != nil {
		account.SetFiles(changed)
		account.Range = span
		account.Patch = s.recordPatch(ctx, task, dir, live)
	}
	// Artifacts name files in the shared workspace, so only a change that
	// reached it may be recorded: work still sitting on an undelivered branch is
	// real, is in the account, and is not in a directory the rest of the job can
	// open. The refusal sentence is what says where it is.
	if dir != s.workspace.Root() {
		return nil
	}
	paths := make([]string, 0, len(changed))
	for _, file := range changed {
		paths = append(paths, file.Path)
	}
	return paths
}

// swePassRef files one pass's range in the repository, under the node's name.
//
// The synthetic commit is the whole trick and it is why this needs no storage of
// its own. A commit whose TREE is the state the pass ended at and whose PARENT is
// the state it started from is a git object that means exactly "this range" —
// `C^..C` is the pass's diff, forever, whatever lands afterwards. One sha per
// pass, in a ref, in the repository the work is in.
//
// The range is per pass and the account is per node because the accounts are
// UNIONED (see [sweNodeChanges]). Neither half works alone: a single wide range
// from the node's first base to the current HEAD would swallow every sibling
// that landed in between, and a single pass's range forgets everything the
// node's earlier passes did — which is the defect this whole file is about,
// since a repair round over landed work reported a change set of nothing.
//
// Everything about it is best-effort. A pass that cannot be filed leaves the
// account with one pass fewer, which is a smaller claim rather than a false one.
func swePassRef(ctx context.Context, dir, leaf, base, head string) {
	base, head = strings.TrimSpace(base), strings.TrimSpace(head)
	if strings.TrimSpace(dir) == "" || base == "" {
		return
	}
	heads := []string{head}
	if head == "" {
		heads = gitLines(ctx, dir, "rev-parse", "HEAD")
	}
	if len(heads) == 0 || heads[0] == base {
		// Nothing was committed in this pass. Whatever it left uncommitted is
		// read live off the working tree and needs no range of its own.
		return
	}
	if gitQuiet(ctx, dir, "diff", "--quiet", base, heads[0]) == nil {
		// A range whose two ends have the same tree is not a change; a
		// re-landing of work somebody else already applied reaches here.
		return
	}
	namespace := swePassRefs(leaf)
	// The pass number is the count of what is already filed, so a restarted
	// process numbers from the repository rather than from a memory it does not
	// have. Padded because refs sort as strings and a reader listing them should
	// see them in the order they happened.
	next := len(gitLines(ctx, dir, "for-each-ref", "--format=%(refname)", namespace))
	commit := gitLines(ctx, dir,
		"-c", "user.name=aforge",
		"-c", "user.email=agentfield-bot@users.noreply.github.com",
		"commit-tree", heads[0]+"^{tree}", "-p", base,
		"-m", "aforge: what node "+leaf+" changed in pass "+strconv.Itoa(next+1))
	if len(commit) == 0 {
		return
	}
	_ = gitQuiet(ctx, dir, "update-ref", fmt.Sprintf("%s/%04d", namespace, next), commit[0])
}

// sweChangeSet is the one derivation: every range this node has recorded, plus
// whatever is still uncommitted on top, as account rows.
//
// The uncommitted half is not belt-and-braces. The engine commits as it merges
// its own judged worktrees, so a finished run usually has a clean tree — but a
// run that was cancelled, deadlined or died has whatever it was in the middle of
// writing, and a leaf whose account said "nothing" over a tree full of its own
// edits is the original defect wearing a different hat.
//
// base is the node's durable first base and is used for the RANGE the account
// reports, not for the diff: it is where this node's history starts, which is
// what a reader needs to place the change in the repository.
func sweChangeSet(ctx context.Context, dir, leaf, base string, live bool) ([]FileChange, Range, bool) {
	if strings.TrimSpace(dir) == "" {
		return nil, Range{}, false
	}
	tops := gitLines(ctx, dir, "rev-parse", "--show-toplevel")
	heads := gitLines(ctx, dir, "rev-parse", "HEAD")
	if len(tops) == 0 || len(heads) == 0 {
		return nil, Range{}, false
	}
	span := Range{Base: strings.TrimSpace(base), Head: heads[0]}
	if span.Base == "" {
		span.Base = heads[0]
	}
	place := sweRelocate(tops[0], dir)
	// An Account is the accumulator rather than a map because the merge law for
	// a path touched twice — strongest word for the change, summed line counts —
	// is already written there, once, for both channels. It is also exactly the
	// law the union across passes needs: a file this node edited twice is one
	// row at the sum of its two edits.
	var rows Account
	passes := sweNodePasses(ctx, dir, leaf)
	for _, pass := range passes {
		sweNoteDiff(ctx, dir, &rows, place, pass+"^", pass)
	}
	if live {
		// Tracked edits nobody committed, measured against HEAD so they are
		// counted once whether or not any range above was recorded.
		sweNoteDiff(ctx, dir, &rows, place, span.Head)
		// And files git has never seen, which no diff can reach. They carry no
		// line counts because none were measured, and a count nobody measured is
		// not a number this may invent.
		for _, line := range gitLines(ctx, dir, "status", "--porcelain") {
			if !strings.HasPrefix(line, "??") {
				continue
			}
			if path, ok := place(porcelainPath(line)); ok {
				rows.Note(path, ChangeAdded, 0, 0)
			}
		}
	}
	if len(rows.Files) == 0 && len(passes) == 0 {
		// No range was ever filed for this node and the tree is clean. There is
		// no evidence here that the repository was read successfully rather than
		// read as empty, so the caller keeps whatever it had.
		return nil, Range{}, false
	}
	sort.Slice(rows.Files, func(i, j int) bool { return rows.Files[i].Path < rows.Files[j].Path })
	return rows.Files, span, true
}

// sweNodePasses lists the ranges this node has recorded, oldest first.
func sweNodePasses(ctx context.Context, dir, leaf string) []string {
	return gitLines(ctx, dir, "for-each-ref", "--sort=refname", "--format=%(objectname)", swePassRefs(leaf))
}

// sweNoteDiff reads one `git diff` into the account: the sizes from --numstat
// and the kind of change from --name-status, both NUL-separated so a path is
// whatever git says it is rather than whatever survives a split on newlines.
func sweNoteDiff(ctx context.Context, dir string, rows *Account, place func(string) (string, bool), revisions ...string) bool {
	numstat := append([]string{"diff", "-z", "--numstat", "-M"}, revisions...)
	body, ok := gitText(ctx, dir, numstat...)
	if !ok {
		return false
	}
	for _, record := range sweDiffRecords(body, 3) {
		added, _ := strconv.Atoi(record[0])
		removed, _ := strconv.Atoi(record[1])
		if path, ok := place(record[2]); ok {
			rows.Note(path, ChangeChanged, added, removed)
		}
	}
	status := append([]string{"diff", "-z", "--name-status", "-M"}, revisions...)
	if body, ok := gitText(ctx, dir, status...); ok {
		for _, record := range sweDiffRecords(body, 2) {
			if path, ok := place(record[1]); ok {
				rows.Note(path, sweStatusKind(record[0]), 0, 0)
			}
		}
	}
	return true
}

// sweDiffRecords splits git's NUL-separated porcelain into fixed-width records.
//
// The one wrinkle is renames, and it is the reason this is not a plain chunker.
// `--numstat -z` writes a rename as "adds\tdels\t" with an EMPTY third column
// and then two more fields, the old name and the new; `--name-status -z` writes
// "R100" and then two fields. Either way the record grows by one and the name
// that matters is the last of them, which is what this returns.
func sweDiffRecords(body string, width int) [][]string {
	fields := strings.Split(body, "\x00")
	var records [][]string
	for index := 0; index < len(fields); {
		if strings.TrimSpace(fields[index]) == "" {
			index++
			continue
		}
		head := strings.Split(fields[index], "\t")
		record := make([]string, 0, width)
		record = append(record, head...)
		index++
		// A tab-joined head short of the record's width, or one whose last
		// column is empty, is a rename: the names follow as their own fields.
		for len(record) < width || strings.TrimSpace(record[width-1]) == "" {
			if index >= len(fields) {
				return records
			}
			if len(record) < width {
				record = append(record, fields[index])
			} else {
				record[width-1] = fields[index]
			}
			index++
		}
		records = append(records, record[:width])
	}
	return records
}

// sweStatusKind is git's letter in the account's vocabulary. A rename is a move
// and a copy is an addition; the percentage git appends to both is not part of
// the answer.
func sweStatusKind(letter string) string {
	if letter == "" {
		return ChangeChanged
	}
	switch letter[0] {
	case 'A', 'C':
		return ChangeAdded
	case 'D':
		return ChangeDeleted
	case 'R':
		return ChangeMoved
	}
	return ChangeChanged
}

// sweRelocate maps a path git printed into the spelling a reader of this job
// recognises, and refuses the ones that are not this leaf's to claim.
//
// git speaks in paths relative to the repository's top, and the repository may
// be an ancestor of the working directory — a leaf working in one directory of a
// monorepo must not claim its siblings' files. The top may also be spelled
// differently from the directory: on macOS /var is a symlink to /private/var, so
// the same directory has two honest names and a naive Rel between them escapes.
func sweRelocate(top, directory string) func(string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(top)
	if err != nil {
		resolved = top
	}
	real, err := filepath.EvalSymlinks(directory)
	if err != nil {
		real = directory
	}
	return func(path string) (string, bool) {
		if strings.TrimSpace(path) == "" {
			return "", false
		}
		inside, err := filepath.Rel(real, filepath.Join(resolved, path))
		if err != nil || strings.HasPrefix(inside, "..") || sweSidecar(inside) {
			return "", false
		}
		return accountSpelling(inside), true
	}
}

// recordPatch writes the change's own text where a reader can open it and
// returns the handle.
//
// The text is the half of the record nothing downstream ever had. A gate handed
// a list of paths can settle whether a file exists; it cannot settle whether the
// deliverable's account of WHY those lines changed is true, and a method writer
// handed paths alone was left to infer the reason — which is exactly how a
// contract's illustrative example of a root cause was shipped verbatim as a real
// one. A path to the diff costs one line in every context and carries the whole
// change to any reader willing to open it.
//
// It goes under the harness's own directory for this job rather than into the
// workspace, because that directory is git-excluded before the engine's first
// stage runs: a patch file written beside the work would be part of the next
// diff, and the engine's own auditor would — correctly — refuse to ship it.
func (s *SWE) recordPatch(ctx context.Context, task Task, dir string, live bool) string {
	body := ""
	// Pass by pass, in the order they happened, because that is what this node
	// did and a single wide diff would be a different claim: it would carry
	// whatever siblings landed in between as though this node had written it.
	for _, pass := range sweNodePasses(ctx, dir, task.leafKey()) {
		if text, ok := gitText(ctx, dir, "diff", "-M", pass+"^", pass); ok {
			body += text
		}
	}
	if live {
		// Only where the working tree is ours to read; see [sweView.measure].
		if uncommitted, ok := gitText(ctx, dir, "diff", "-M", "HEAD"); ok {
			body += uncommitted
		}
	}
	if strings.TrimSpace(body) == "" {
		return ""
	}
	full, _, err := s.workspace.ScratchPath(patchName(task.leafKey()))
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return ""
	}
	// Internal: it is evidence about the deliverable and never the deliverable,
	// so it must not turn up in the list of files the work produced.
	s.workspace.RecordInternal(task.leafKey(), full)
	return full
}

// recordArtifacts files the derived change set as this leaf's artifacts and
// returns how many were left out of the bounded list. It is a consumer of the
// one derivation above and computes nothing of its own: two lists of "what this
// leaf changed", derived two ways, is two answers to one question.
func (s *SWE) recordArtifacts(task Task, paths []string) int {
	directory := s.workspace.Root()
	sort.Strings(paths)
	overflow := 0
	if len(paths) > sweArtifactLimit {
		overflow = len(paths) - sweArtifactLimit
		paths = paths[:sweArtifactLimit]
	}
	for _, path := range paths {
		s.workspace.Record(task.leafKey(), filepath.Join(directory, path))
	}
	return overflow
}

// sweChangedPaths is the before/after read the artifact list was always taken
// from, kept for the runs the substrate derivation cannot reach: no repository,
// no recorded base, a git that refused.
//
// It is a before/after read rather than a plain `git status` because the engine
// commits — it merges each judged worktree onto the branch, so at the end of a
// successful run the working tree is frequently clean and the whole change lives
// between two commits. Reading only the porcelain would have reported a finished
// refactor as having touched nothing.
func sweChangedPaths(ctx context.Context, directory string, before repoState) []string {
	after := readRepoState(ctx, directory)
	if after.top == "" {
		return nil
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
	place := sweRelocate(after.top, directory)
	paths := make([]string, 0, len(changed))
	for path := range changed {
		if inside, ok := place(path); ok {
			paths = append(paths, inside)
		}
	}
	sort.Strings(paths)
	return paths
}

// sweSidecar names bookkeeping rather than work: the engine's own, and the
// harness's own beside it. A checkpoint file listed as a deliverable is noise
// in every downstream context, and a path that reached a person's file list is
// a path they will open.
//
// The engine's half is its own declaration (internal/swepro/enginestate) rather
// than a copy. The copy that used to be here is exactly why this exists as one
// function: it named `.plandb.db` and its sqlite sidecars and had never named
// the `.plandb/` directory the engine cuts its worktrees into, so the two lists
// aforge kept disagreed about what the engine's own files were.
func sweSidecar(path string) bool {
	if enginestate.Holds(path) {
		return true
	}
	head, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	switch head {
	case obsDir, ".aforge":
		return true
	}
	return false
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
func (s *SWE) environ(view *sweView) []string {
	directory := view.dir
	pinned := map[string]string{
		sweproSentinelEnv: "1",
		// The engine refuses to run without an AgentField control plane
		// answering. Embedded, there is nobody to answer: aforge is the plane.
		// "off" is the one value the embedding patch added to that gate.
		"CODEAF_CP_URL":      "off",
		"OPENROUTER_API_KEY": s.apiKey,
		// The plandb singleton, kept beside the leaf's view rather than inside
		// it (sweStateDir). Unset, the engine puts it at <run dir>/.plandb.db —
		// which is to say inside the deliverable, held out of it by an exclude
		// file that is advisory and that dies the moment anything commits. This
		// is the one piece of the engine's state that a variable can move, and
		// moving it is worth more than the exclusion was: a file that is not in
		// the tree cannot be staged, cannot be squashed, and cannot be in
		// somebody's history. Keyed by root and leaf, so a run and its resume
		// still open the same database.
		"PLANDB_DB": view.plandb(),
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
	kept := make([]string, 0, len(environ)+len(pinned)+len(sweBuildCacheDirs())+len(s.extraEnv))
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
	return append(append(kept, sweSharedCacheEnv()...), s.extraEnv...)
}

// sweCacheRoot is where every swe leaf's toolchain caches live: one directory
// under aforge's own state root, shared by every session on the machine.
//
// It is deliberately NOT under the workspace and NOT per-session. A module
// cache is content-addressed — the same module at the same version is the same
// bytes for everybody — so a private copy per session buys no isolation and
// costs a full re-download each time. Measured (audit-notes/
// headless-regression-audit.md §10): one session pulled 329MB of
// modernc.org/sqlite on its own, concurrent sessions each pulled it again, and
// the pod's 5GB root filesystem hit 100% mid-battery. That full disk is also
// what the engine's own resource guard reads, so the waste did not merely cost
// bandwidth: it pinned the scheduler into a resource pause it could never
// leave.
//
// What stays private is what carries a run's meaning — the workspace, the git
// repository, the plandb, the checkpoint. None of those live here.
func sweCacheRoot() string { return home.Join("cache", "toolchain") }

// sweBuildCacheDirs is the toolchain-cache variable table: the environment
// variable each ecosystem reads, and the leaf directory it gets under the
// shared root. Names are the ones the engine's own shell tool would otherwise
// point at a per-session scratch directory
// (internal/swepro/internal/tool/shell_scratch.go).
func sweBuildCacheDirs() map[string]string {
	return map[string]string{
		"GOMODCACHE":       "go-mod",
		"GOCACHE":          "go-build",
		"CARGO_TARGET_DIR": "cargo",
		"npm_config_cache": "npm",
		"PIP_CACHE_DIR":    "pip",
	}
}

// sweSharedCacheEnv is the shared-cache half of the child's environment.
//
// An operator who has already said where a cache goes outranks this: a machine
// with a warm GOMODCACHE, or a CI image that mounts one, has made the same
// decision better, and setting these only when they are absent is what lets the
// child inherit that instead of ignoring it. Absence is the case that filled
// the disk — `go env GOMODCACHE` has a default, but the variable is unset, so
// the engine read "nobody chose" as "give this session its own".
func sweSharedCacheEnv() []string {
	root := sweCacheRoot()
	dirs := sweBuildCacheDirs()
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		if _, set := os.LookupEnv(name); set {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	shared := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, dirs[name])
		// A cache directory that cannot be made is not worth failing a leaf
		// over: the tool creates its own, or falls back to its default, and
		// either way the run proceeds. Silence here is the fail-open the whole
		// cache layer is written as.
		if os.MkdirAll(path, 0o755) != nil {
			continue
		}
		shared = append(shared, name+"="+path)
	}
	return shared
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
	if err := processgroup.Terminate(pid); err != nil {
		_ = command.Process.Kill()
	}
	return time.AfterFunc(sweTerminateGrace, func() {
		defer guard.Recover("exec/swe kill")
		trace.note("engine: it did not stop when asked — killing the process group")
		if err := processgroup.Kill(pid); err != nil {
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
// The one thing the view changes about the prompt is where an upstream file is.
// A dependency's artifacts are recorded relative to the shared workspace, and a
// leaf reading them from a checkout of its own would find nothing at those
// names — untracked deliverables a sibling wrote are in the workspace and not in
// this leaf's view of the repository. Spelled absolutely they resolve from
// either place, so the pointer keeps working and the in-place prompt stays
// byte-identical to what it always was.
func sweGoal(task Task, view *sweView) string {
	var block strings.Builder
	// A single-leaf splice sets Brief == Goal, and sending the same text twice
	// is not context, it is size: the doubled prompt measured a band larger and
	// made the engine refuse its own fast path (§14 DNF forensics). The goal
	// preamble earns its place only when it says something the brief does not.
	if goal := strings.TrimSpace(task.Goal); goal != "" && !strings.Contains(task.Brief, goal) {
		fmt.Fprintf(&block, "This work is part of a larger goal:\n%s\n\n", goal)
	}
	if len(task.Inputs) > 0 {
		block.WriteString("Results from earlier work, which you already have and must not gather again. " +
			"Where two of them speak to the same quantity, the LATER one stands: a corrected figure " +
			"replaces its predecessor, and reaching back past a correction to the number it corrected " +
			"is the one way to be wrong with everything you need in hand:\n")
		for _, input := range task.Inputs {
			fmt.Fprintf(&block, "\n=== from %q ===\n%s\n", input.Title, input.Result)
			if paths := sweInputPaths(input.Artifacts, view); len(paths) > 0 {
				fmt.Fprintf(&block, "(files: %s — read them if you need the full detail)\n", strings.Join(paths, ", "))
			}
		}
		block.WriteString("\n")
	}
	block.WriteString("Your work:\n")
	block.WriteString(task.Brief)
	block.WriteString(outputClause(task))
	return block.String()
}

// sweInputPaths spells an upstream's files so this leaf can open them from
// wherever it is working. In place that is the list untouched.
func sweInputPaths(artifacts []string, view *sweView) []string {
	if view == nil || !view.isolated || len(artifacts) == 0 {
		return artifacts
	}
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if trimmed := strings.TrimSpace(artifact); trimmed != "" && !filepath.IsAbs(trimmed) {
			artifact = filepath.Join(view.root, trimmed)
		}
		paths = append(paths, artifact)
	}
	return paths
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
	// One workspace per job, and the scheduler starts a job's leaves together:
	// four swe leaves reach this function on the same directory within
	// milliseconds of each other. Git's own locking answers a losing racer with
	// a fatal — `.git/index.lock: File exists` from `add`/`commit`, and
	// `cannot copy .../templates/info/exclude: File exists` from a second
	// `init` — which is exit 128, and is exactly how three of four leaves died
	// on a live run. Nothing here is unsafe once it is one-at-a-time: the
	// loser wakes, finds a HEAD, and truthfully reports an existing repository.
	bootstrap := &sweRootLocksFor(directory).bootstrap
	bootstrap.Lock()
	defer bootstrap.Unlock()

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

// gitText is gitLines' other half: the whole of what git wrote, unsplit,
// for the reads whose record separator is not a newline. Every path git can
// print inside a NUL-separated record — a name with a newline in it, a name
// with a quote in it — is a name gitLines would have torn in two.
func gitText(ctx context.Context, directory string, args ...string) (string, bool) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	out, err := command.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
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
	// root is the leaf's workspace, and is here for one job: the engine names
	// files by absolute path, from inside worktrees it made under the workspace,
	// and the account has to speak in paths a reader of this job recognises. See
	// [sweRun.accountPath].
	root string

	// account is the structured story of the work, assembled as the stream goes
	// past. Everything in it is something the engine said out loud on the wire
	// and nothing else in the system was in a position to hear.
	account Account

	terminal *sweEvent
	turns    int
	// tokens is keyed by assistant message id and holds the last figure that
	// message reported. The engine republishes a message as it streams, each
	// time with the running totals, so summing every update would count the
	// same tokens several times over.
	tokens map[string]sweTokens
	// estimated is true once [sweRun.usage] has had to price the run from the
	// stream rather than read the engine's own terminal figure. It is carried
	// into the leaf's measured line, because a number nobody labelled is a
	// number the selection prompts will read as measured truth.
	estimated bool
	// steered is every line the user sent that this run could not act on.
	steered []string
	// spoken is which parts the recorder has already been told about, keyed by
	// the part's own id and phase. The bus republishes a part on every update;
	// this is what keeps one call one row (see [sweRun.narratePart]).
	spoken map[string]bool
	// polled throttles the between-lines poll: a stream can deliver a thousand
	// deltas a second and a store query per delta is a store query too many.
	polled time.Time

	// fit is what the engine said about the size of the job it was handed, kept
	// as it streams past because the sentences that use it are written at the
	// end. None of it changes what runs — it is read once, in settle, and turned
	// into prose for the ruler that decides what reaches this worker next time.
	fit sweFit

	// said is the last thing the engine's worker finished saying out loud: the
	// most recent completed assistant text part on the wire, whole.
	//
	// It is here because it was the deliverable all along and was being thrown
	// away. The engine's terminal event carries a message field and its own
	// contract promises one (internal/swepro/EVENTS-CONTRACT.md), but the
	// pipeline leaves it empty on the ending that matters most — a clean pass —
	// so the leaf's whole account of a successful coding run collapsed to "the
	// coding run ended without a verdict of its own", every time, while the
	// worker's actual prose — the root cause it found, the reasoning behind the
	// change — went past on this channel and was written only to the trace.
	//
	// Last rather than concatenated: the intermediate texts are a working
	// monologue and the final one is the answer, which is the same law the
	// generalist leaf's deliverable follows. Uncapped, because this is the
	// deliverable and not a row in a record; the trace keeps its own clipped copy.
	said string

	// landing replaces the flat "it ended without saying how it went" on the
	// one ending that has something better to say for itself.
	landing string
	// seenBaseline dedupes the baseline sentences: verification runs once per
	// audit cycle and reports the same pre-existing reds every time.
	seenBaseline map[string]bool
	// state is what the engine's own gates last said, and it is the whole of
	// what lets a run that hit the wall clock still be delivered. See
	// [sweState].
	state sweState
}

// sweState is the engine's last word from each of its two machine gates, kept
// live as the stream goes past.
//
// It exists for one ending. A leaf that reaches its wall clock is stopped, and
// everything the engine said on the way is discarded as "the time limit was
// reached before anything finished" — which was measurably false: cli#2217
// produced a patch within three lines of the upstream fix, passed the
// repository's own suite, passed the audit, and was reported `settled:false`
// because the clock expired during the wrap-up after the work was already
// committed (audit-notes §14.4.1). The work product was in the workspace the
// whole time.
//
// Both gates must be green and the workspace must actually have changed before
// that ending is rewritten, because the failure mode on the other side —
// shipping a half-finished tree because verification happened to pass six
// cycles ago — is worse than a false negative. Both are reset by a later
// failure, so "green" means green as of the last thing the engine said.
type sweState struct {
	verified bool
	audited  bool
}

// deliverable reports whether the engine had already finished and checked its
// work when the clock ran out.
func (s sweState) deliverable() bool { return s.verified && s.audited }

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
	cost                       float64
}

// usage is what this leaf spent, from the only accounting that survives every
// ending.
//
// THE COST HALF IS NOT OPTIONAL AND IT IS NOT THE TERMINAL LINE'S ALONE. It
// used to be: settle read `cost_usd` off the engine's terminal event and that
// was the whole of it. Every ending that has no terminal event — a deadline, a
// cancel, a pause, a child that died — therefore reported $0.0000 no matter how
// long it had run. Measured (audit-notes/headless-regression-audit.md §10): a
// leaf that worked for 893 seconds reported `swe: deadline after 0 cycles,
// $0.0000` while a comparable run that reached its terminal line reported
// $0.0874, a ~35× discrepancy in dollars-per-token between two runs of the same
// worker on the same model. Those lines are the measured evidence the selection
// prompts read (selfknow), so a silently-tiny number does not merely under-count
// — it teaches the ruler that this worker is nearly free.
//
// The engine publishes each assistant message's own running `cost` on
// `message.updated`, alongside the tokens this already kept. Summing the last
// figure per message id is the same arithmetic the token half uses and is
// correct for the same reason: a republished message carries running totals,
// not deltas.
func (r *sweRun) usage() Usage {
	usage := Usage{Calls: r.turns}
	for _, tokens := range r.tokens {
		usage.PromptTokens += tokens.prompt
		usage.CompletionTokens += tokens.completion
		usage.CachedTokens += tokens.cached
		usage.Cost += tokens.cost
	}
	return usage
}

// consume reads one NDJSON line.
//
// EVERY LINE IS STILL KEPT AND NO LINE IS PROSE ANY MORE. It used to be one
// `trace.note` per line, on the reasoning that the trace is a flight recorder
// and a filtered recorder answers one question only. The recorder half of that
// is right and the FILE was wrong: the recorder is also the document the task
// room draws (internal/tui2/chat/trace.go), so a busy engine — a thousand
// deltas a second — turned a room's record into screens of
// `{"id":"evt_…","type":"message.part.delta",…}` rendered as content. Measured
// on the reporter's own profile: `9200.trace.log` is 2.1MB and 5,928 lines, of
// which about forty are sentences.
//
// So the stream forks here, once, at the only place it can fork honestly:
//
//   - the RAW line goes to the sidecar ([tracer.stream]), whole, in order, for
//     whoever is debugging the engine;
//   - what a PERSON would read is written into the recorder in the recorder's
//     own grammar ([sweRun.narrate]) — a call, its result, the model's own
//     sentences — so the record draws proper tool rows with bounded output
//     boxes instead of a JSON dump it had no rule for.
//
// A line that is not one of the engine's records at all — a stray print, a
// runtime's warning — is not machine-shaped and stays a note, because that is
// exactly what it is.
func (r *sweRun) consume(line string) {
	line = strings.TrimRight(line, "\n")
	var event sweEvent
	if json.Unmarshal([]byte(line), &event) != nil {
		r.trace.note(line)
		return
	}
	r.trace.stream(line)
	r.narrate(event)
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

// swePart is one instance-bus message part, decoded only as far as the recorder
// needs it. The engine's own model of these is `internal/swepro`'s and is not
// reachable from here by design — the two halves talk over a documented line
// format (internal/swepro/EVENTS-CONTRACT.md), not over a shared struct.
type swePart struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	State struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
		Error  string          `json:"error"`
		// Metadata is what a finished tool call reports about itself beyond its
		// printed output. For the three tools that write files it is the only
		// place the change set exists in structured form — the printed output is
		// "Edit applied successfully." — and it is decoded lazily, by the one
		// reader that wants it, because every other tool in the engine puts
		// something different in here.
		Metadata json.RawMessage `json:"metadata"`
	} `json:"state"`
	Time struct {
		End float64 `json:"end"`
	} `json:"time"`
}

// narrate writes the human half of one engine event into the recorder.
//
// It emits the SAME five shapes [tracer.turn] writes for a linear leaf, so one
// reader parses both and a swe run's record reads like every other record in
// the product rather than like a second format nobody wrote a lens for.
//
// EVERY PART IS SPOKEN ABOUT ONCE. The bus republishes a part on every update —
// a tool part appears pending, then running, then completed; a text part appears
// on every token — so the dedupe is not an optimisation but the difference
// between forty rows and forty thousand. A tool is written when it is ISSUED
// (which is when its input first exists) and again when it RETURNS; text and
// reasoning are written when the part is finished, which is what `time.end`
// says, and that is also why the enormous system prompt never lands here: a
// prompt is a part with no ending of its own.
func (r *sweRun) narrate(event sweEvent) {
	// The compact control-plane shape. `supervisor` carries no stage of its own
	// and [sweRun.stage] falls back to the type for exactly that case, so this
	// reads it the same way rather than going quiet on the one record that says
	// the engine re-exec'd itself.
	if name := stageName(event); name != "" {
		status := strings.TrimSpace(event.Status)
		if status != "" {
			status = " " + status
		}
		r.trace.note("stage: " + name + status)
		return
	}
	if event.Type != "message.part.updated" || len(event.Properties) == 0 {
		return
	}
	var payload struct {
		Part swePart `json:"part"`
	}
	if json.Unmarshal(event.Properties, &payload) != nil {
		return
	}
	r.narratePart(payload.Part)
}

// narratePart writes one part, at most twice, in the recorder's own grammar.
func (r *sweRun) narratePart(part swePart) {
	if part.ID == "" {
		return
	}
	if r.spoken == nil {
		r.spoken = make(map[string]bool, 64)
	}
	once := func(key string) bool {
		if r.spoken[key] {
			return false
		}
		r.spoken[key] = true
		return true
	}
	switch part.Type {
	case "text", "reasoning":
		text := strings.TrimSpace(part.Text)
		if text == "" || part.Time.End <= 0 || !once(part.ID) {
			return
		}
		// Only what the worker SAID. Reasoning is the same channel carrying a
		// different thing — a model thinking out loud on its way to an answer —
		// and delivering it as the answer would hand a person the working
		// instead of the result.
		if part.Type == "text" {
			r.said = text
		}
		r.trace.note("text: " + snip(text, sweSaidCap))

	case "tool":
		tool := strings.TrimSpace(part.Tool)
		if tool == "" {
			tool = "tool"
		}
		switch part.State.Status {
		case "running", "pending":
			// The input is what the row is FOR, and it is empty while the call
			// is still pending — so the row waits for the phase that has it, and
			// a call that never gets one is announced with an empty object
			// rather than not at all.
			args := strings.TrimSpace(string(part.State.Input))
			if args == "" || args == "{}" || args == "null" {
				return
			}
			if !once(part.ID + "-call") {
				return
			}
			r.trace.note("call " + tool + " " + snip(args, sweCallCap))
		case "completed", "error":
			if !once(part.ID + "-result") {
				return
			}
			// The same once-per-part guard the row is written under, because the
			// account is counting lines: a part republished four times whose
			// additions were added four times would report a change four times
			// the size of the one on disk.
			if part.State.Status == "completed" {
				r.noteFiles(tool, part)
			}
			body, mark := part.State.Output, ""
			if part.State.Status == "error" {
				mark = " ERROR"
				if strings.TrimSpace(body) == "" {
					body = part.State.Error
				}
			}
			r.trace.note(fmt.Sprintf("  → %dB%s: %s",
				len(body), mark, snip(strings.TrimSpace(body), sweResultCap)))
		}
	}
}

const (
	// sweSaidCap is how much of one thing the model said survives into the
	// recorder. It matches [tracer.turn]'s own cap for a linear leaf, so the two
	// paths produce rows of the same size.
	sweSaidCap = 600
	// sweCallCap is how much of a call's argument object is kept — the same 300
	// the linear path keeps, which is the number the room's salient-input scan
	// was written against (internal/tui2/chat/trace.go's salientArg).
	sweCallCap = 300
	// sweResultCap is how much of a result is kept: the record's bounded output
	// box is twelve rows (traceBoxRows), and this is about what twelve rows
	// hold. The whole result is in the sidecar for anyone who needs it.
	sweResultCap = 600
)

// stage turns one engine stage into within-node visibility. Progress is
// replaceable and may say anything; Share is a message to the rest of the job
// and is spent only on milestones — docs/SUBHARNESSES.md's "one mouth" is the
// whole reason this is two channels and not one.
func (r *sweRun) stage(event sweEvent) {
	name := stageName(event)
	r.noteFit(event)
	r.record(name + " " + event.Status)
	done, latest := sweLatest(event)
	r.task.progress(swePhase(name), done, 0, latest)
	if milestone := sweMilestone(event); milestone != "" && r.task.Share != nil {
		_ = r.task.Share(milestone)
	}
}

// stageName is what one control-plane record calls itself: its stage, or its
// type when it has no stage of its own (`supervisor`). It is one function
// because the recorder and the progress channel must name a stage identically —
// two spellings would put two different words on two surfaces for one event.
func stageName(event sweEvent) string {
	if name := strings.TrimSpace(event.Stage); name != "" {
		return name
	}
	switch event.Type {
	case "stage", "supervisor":
		return strings.TrimSpace(event.Type)
	}
	return ""
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
		switch event.Status {
		case "pass":
			r.state.audited = true
		case "fail", "escalated":
			r.state.audited = false
		}
	case "verification":
		switch event.Status {
		case "pass":
			r.state.verified = true
		case "fail":
			r.state.verified = false
		}
		r.notePreExisting(event)
		r.noteChecks(event)
	}
}

// notePreExisting carries the engine's baseline-delta sentences out of the
// stream and into the leaf's outcome.
//
// The engine is the only thing in the system that photographed the repository
// before the work started, and the delivery gate — two processes away, holding
// prose and a file list — is the thing that most needs to know a red suite was
// red on arrival. Between them there is one channel: this event. Nothing is
// interpreted here; the sentences travel verbatim, because a gate rewording a
// fact it cannot check is a gate inventing one.
func (r *sweRun) notePreExisting(event sweEvent) {
	raw, ok := event.Data["pre_existing"].([]any)
	if !ok {
		return
	}
	if r.seenBaseline == nil {
		r.seenBaseline = map[string]bool{}
	}
	for _, item := range raw {
		note, ok := item.(string)
		if !ok {
			continue
		}
		if note = strings.TrimSpace(note); note == "" || r.seenBaseline[note] {
			continue
		}
		r.seenBaseline[note] = true
		r.outcome.Baseline = append(r.outcome.Baseline, note)
	}
}

// ── the account ──────────────────────────────────────────────────────────────
//
// What follows is the whole of how a pipeline behind a process boundary comes
// to say what it did. Nothing here asks the engine for anything new: the change
// set is already in the metadata of the three tools that write files, and the
// verification story is already in the stage event the gate publishes. Both
// were being read past on the way to the trace, which is why a run that ended
// without a terminal line could only report that it had ended.

// noteFiles reads one finished tool call for the files it wrote.
//
// Three tools write, and each reports itself differently because each is
// answering a different question — an edit knows its own patch, a write knows
// whether the file was there before, a patch knows a whole change set at once.
// The switch is that table and nothing more; a tool that is not one of them
// wrote no files and is not silently assumed to have.
func (r *sweRun) noteFiles(tool string, part swePart) {
	if len(part.State.Metadata) == 0 {
		return
	}
	switch tool {
	case "edit":
		var meta struct {
			FileDiff struct {
				File      string `json:"file"`
				Additions int    `json:"additions"`
				Deletions int    `json:"deletions"`
			} `json:"filediff"`
		}
		if json.Unmarshal(part.State.Metadata, &meta) != nil {
			return
		}
		r.note(r.accountPath(meta.FileDiff.File), ChangeChanged,
			meta.FileDiff.Additions, meta.FileDiff.Deletions)

	case "write":
		var meta struct {
			FilePath string `json:"filepath"`
			Exists   bool   `json:"exists"`
			Diff     string `json:"diff"`
		}
		if json.Unmarshal(part.State.Metadata, &meta) != nil {
			return
		}
		// A write reports no line counts of its own, only the patch it applied.
		// Counting the patch is the same arithmetic the other two tools did
		// before publishing theirs, so the three agree rather than one of them
		// reporting a file with no size.
		change := ChangeAdded
		if meta.Exists {
			change = ChangeChanged
		}
		added, removed := patchStat(meta.Diff)
		r.note(r.accountPath(meta.FilePath), change, added, removed)

	case "apply_patch":
		var meta struct {
			Files []struct {
				FilePath     string `json:"filePath"`
				RelativePath string `json:"relativePath"`
				Type         string `json:"type"`
				MovePath     string `json:"movePath"`
				Additions    int    `json:"additions"`
				Deletions    int    `json:"deletions"`
			} `json:"files"`
		}
		if json.Unmarshal(part.State.Metadata, &meta) != nil {
			return
		}
		for _, file := range meta.Files {
			// The relative path is the engine's own spelling of where the file
			// sits in the repository, and is right even when the absolute one
			// points inside a worktree this leaf has never heard of.
			path := strings.TrimSpace(file.RelativePath)
			if path == "" {
				path = r.accountPath(file.FilePath)
			} else {
				path = accountSpelling(path)
			}
			if move := strings.TrimSpace(file.MovePath); move != "" {
				path = r.accountPath(move)
			}
			r.note(path, sweChangeKind(file.Type), file.Additions, file.Deletions)
		}
	}
}

// note is the one door every change row goes through, and the filter on it is
// the same one the artifact list has always had (recordArtifacts, sweSidecar).
//
// The two halves of what a leaf reports were being read from two places under
// two rules: the artifact list from the repository, filtered, and the account's
// rows from the engine's own tool metadata, unfiltered. So a delivered account
// named `.codeaf/contract.json` as a file the work changed — the engine writing
// its own bookkeeping, reported to a person as their change. A path is either
// the work or it is machinery, and which one it is cannot depend on which
// surface is asking.
func (r *sweRun) note(path, change string, added, removed int) {
	if strings.TrimSpace(path) == "" || sweSidecar(path) {
		return
	}
	r.account.Note(path, change, added, removed)
}

// sweChangeKind is the engine's word for a change in the account's vocabulary.
// The two lists are close enough that a translation looks like ceremony and far
// enough apart that leaving it out would put "update" and "changed" on the same
// surface for the same fact.
func sweChangeKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "add":
		return ChangeAdded
	case "delete":
		return ChangeDeleted
	case "move":
		return ChangeMoved
	}
	return ChangeChanged
}

// accountPath is the file the leaf's reader would recognise.
//
// The engine names files absolutely, and the file it names is usually not in
// the workspace but in a git worktree the scheduler cut under it — the layout
// is <workspace>/.plandb/wt-<task>/<path> — which it merges onto the branch when
// the leaf's work is judged. A reader handed that path is handed the machinery
// instead of the change, and would be handed a different path for the same file
// on the next run. So the workspace prefix comes off, and the worktree prefix
// with it; a path from somewhere else entirely is left exactly as the engine
// said it, because a path we cannot place is still a fact.
func (r *sweRun) accountPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || r.root == "" || !filepath.IsAbs(path) {
		return accountSpelling(path)
	}
	// macOS spells one directory two honest ways — /var is a symlink to
	// /private/var — and a Rel between the two spellings escapes. Every
	// spelling of each side is tried against every spelling of the other,
	// because resolving is not always available here: a file the engine
	// reported and then deleted, or one written inside a worktree that has
	// since been removed, cannot be resolved at all, and one that reads as
	// outside the workspace on a technicality would be reported to a person as
	// an absolute path through machinery.
	for _, base := range spellings(r.root) {
		for _, target := range spellings(path) {
			inside, err := filepath.Rel(base, target)
			if err != nil || strings.HasPrefix(inside, "..") {
				continue
			}
			return accountSpelling(inside)
		}
	}
	return path
}

// spellings is a path as written and, when the filesystem will say, as
// resolved. One entry when the two are the same or the path does not exist.
func spellings(path string) []string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved == path {
		return []string{path}
	}
	return []string{path, resolved}
}

// accountSpelling strips the engine's worktree prefix and normalises the
// separator. The prefix is a two-segment fact about where a leaf was run and
// says nothing about what changed.
func accountSpelling(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	head, rest, found := strings.Cut(path, "/")
	if !found || head != sweWorktreeDir {
		return path
	}
	branch, tail, found := strings.Cut(rest, "/")
	if !found || !strings.HasPrefix(branch, sweWorktreePrefix) {
		return path
	}
	return tail
}

const (
	// sweWorktreeDir and sweWorktreePrefix are the engine's own worktree layout
	// (internal/swepro/internal/session/scheduler/worktree.go), read from
	// outside. They are named rather than pattern-matched because a directory
	// that merely looks like one is a directory somebody's repository owns.
	sweWorktreeDir    = ".plandb"
	sweWorktreePrefix = "wt-"
)

// patchStat counts a unified diff. The header lines are the two that begin with
// a tripled marker, and they are not content — counting them would report every
// written file as one line larger than it is on both sides.
func patchStat(patch string) (added, removed int) {
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// noteChecks carries the verification gate's own evidence out of the stream.
//
// The gate publishes every entrypoint it discovered and ran, with the command,
// the exit status, the tail of what the process printed, and — for a red one —
// whether the same red was already there before this work began. That is the
// verification story in full, and it is the thing a judge two processes away
// most needs and can least reconstruct: it holds prose and a file list, and
// cannot run anything.
//
// The last pass replaces the ones before it. Verification runs once per audit
// cycle over a tree that keeps changing, so a union of four passes would report
// a suite both red and green; what is true is what the last run found.
func (r *sweRun) noteChecks(event sweEvent) {
	raw, ok := event.Data["commands"].([]any)
	if !ok {
		return
	}
	checks := make([]Check, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		command := strings.TrimSpace(sweText(row["cmd"]))
		if command == "" {
			continue
		}
		exit, reported := row["exit"].(float64)
		timedOut, _ := row["timedOut"].(bool)
		known, _ := row["preExisting"].(bool)
		checks = append(checks, Check{
			Command: command,
			Kind:    strings.TrimSpace(sweText(row["kind"])),
			// A suite killed at the harness ceiling never produced an exit
			// status at all. That is an INCOMPLETE observation and not a green
			// one, so it fails here exactly as it fails inside the engine.
			Passed: reported && exit == 0 && !timedOut,
			Known:  known,
			Tail:   strings.TrimSpace(sweText(row["tail"])),
		})
	}
	if len(checks) > 0 {
		r.account.Checks = checks
	}
}

// sweText reads a string out of a decoded event field, answering empty for
// anything that is not one. Every value here came off a wire whose shape is
// documented and not enforced.
func sweText(value any) string {
	text, _ := value.(string)
	return text
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
			ID     string  `json:"id"`
			Role   string  `json:"role"`
			Cost   float64 `json:"cost"`
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
		cost:       info.Cost,
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
	// The account goes under whatever the run said for itself, never over it.
	// The engine's own final message is the deliverable when there is one; this
	// is the evidence for it, and on the endings that carry no message at all it
	// is the whole of what the leaf knows — which is the difference between a
	// node summary somebody can act on and the void sentence it replaces.
	report := r.account.Report()
	if block.Len() == 0 {
		// The worker's own last word stands in for a terminal message that was
		// never written, which on this engine is every successful run. It
		// outranks both sentences below it because they are the harness
		// describing an ending and this is the work describing itself: a leaf
		// that found a root cause and said so was delivering "the coding run
		// ended without a verdict of its own" over the top of it.
		switch {
		case strings.TrimSpace(r.said) != "":
			block.WriteString(strings.TrimSpace(r.said))
			// The landing sentence is about HOW the run ended and is still owed
			// when there is one — it is the difference between a delivered
			// change and a discarded one — so it follows rather than being
			// displaced.
			if r.landing != "" {
				block.WriteString("\n\n" + r.landing)
			}
		case r.landing != "":
			block.WriteString(r.landing)
		default:
			block.WriteString(sweEndingText(stop, report != ""))
		}
	}
	if report != "" {
		block.WriteString("\n\n" + report)
	}
	status := string(stop)
	cycles := 0
	if terminal != nil {
		status = terminal.Status
		cycles = terminal.count("cycle")
	}
	// The bill is read off the Outcome rather than off the terminal event,
	// because settle has already decided which of the two accountings is the
	// honest one and this line must not be able to disagree with the usage row
	// beside it.
	fmt.Fprintf(&block, "\n\nswe: %s after %d cycles, $%.4f", status, cycles, r.outcome.Usage.Cost)
	if r.estimated {
		block.WriteString(" (estimated from the engine's own message stream — " +
			"this run ended without a reported total)")
	}
	if len(r.steered) > 0 {
		block.WriteString("\n\nsteering received late: " + strings.Join(r.steered, " · ") +
			" — this worker cannot take guidance mid-run, so none of it was applied.")
	}
	return block.String()
}

// sweEndingText is what a run says when the engine said nothing. Each of these
// is a real ending with no message of its own attached to it.
//
// accounted is whether the leaf has the work itself to show. The default arm
// used to be "it ended without saying how it went" unconditionally, and that
// sentence was the most expensive one in the system: the delivery gate judged a
// void and reacted differently every time, and the remainder judge, seeing a
// node that reported nothing, kept adding children to re-investigate work that
// was finished and green. A run that changed files and ran its suite has an
// account whatever the process did on the way out, and the sentence says which
// of the two it is rather than collapsing them.
func sweEndingText(stop StopReason, accounted bool) string {
	switch stop {
	case StopCancelled:
		return "The coding run was cancelled. Its checkpoint is in the workspace, so the same node can pick it up."
	case StopPaused:
		return "The coding run is paused. Its checkpoint is in the workspace, so resuming continues rather than restarts."
	case StopDeadline:
		return "The coding run ran out of wall clock. Its checkpoint is in the workspace."
	}
	if accounted {
		return "The coding run ended without a verdict of its own. What it did is below, " +
			"taken from the engine's own record as the work went past."
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
