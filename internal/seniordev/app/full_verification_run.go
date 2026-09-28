//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/fullverification"
)

type projectVerificationRun struct {
	runner *pipeline
	ctx    context.Context
	plan   fullverification.Plan
	result projectVerificationResult
	lines  []string
	issues []string

	currentFingerprint  string
	haveFingerprint     bool
	resolvedFingerprint bool
}

type verificationObservation struct {
	entrypoint fullverification.Entrypoint
	memoKey    string
	exitCode   int
	timedOut   bool
	tail       string
	noTests    bool
	evidence   map[string]any
	// suiteDead marks a failure whose output shows the suite aborted before
	// running at all (verification_deadtree.go).
	suiteDead bool
	// safetyRegression also includes suite-local parser aborts (notably Jest),
	// which are unsafe to accept even when unrelated suites still ran.
	safetyRegression bool
}

func newProjectVerificationRun(runner *pipeline, ctx context.Context) *projectVerificationRun {
	return &projectVerificationRun{
		runner: runner,
		ctx:    ctx,
		plan:   fullverification.Discover(runner.workspace),
		result: projectVerificationResult{Commands: []any{}},
		lines: []string{
			"# Independent full project verification",
			"senior-dev independently discovered and ran the standard project entrypoints",
			"below in fresh Bash subprocesses. These are process-derived command/exit",
			"observations, not the model's claims. Consult them, but still run and cite",
			"your own fresh verification commands.",
		},
		issues: []string{},
	}
}

func (run *projectVerificationRun) run() projectVerificationResult {
	// THE CHECK SAYS IT HAS STARTED, so a reader following the run knows
	// senior-dev is running the project's build and tests itself before the
	// first of them has finished. It reports; it decides nothing.
	run.runner.events.stage("verification", "running", map[string]any{
		"commands": len(run.plan.Entrypoints),
	})
	for _, entrypoint := range run.plan.Entrypoints {
		observation := run.observe(entrypoint)
		run.record(observation)
	}
	run.recordMissingEntrypoints()
	vacuous := run.recordVacuousVerification()
	return run.finish(vacuous)
}

// fingerprint resolves the tree fingerprint lazily and at most once per pass.
// A run where nothing hangs must not pay for a scan.
func (run *projectVerificationRun) fingerprint() (string, bool) {
	if !run.resolvedFingerprint {
		run.currentFingerprint, run.haveFingerprint = run.runner.worktreeFingerprint(run.ctx)
		run.resolvedFingerprint = true
	}
	return run.currentFingerprint, run.haveFingerprint
}

func (run *projectVerificationRun) observe(
	entrypoint fullverification.Entrypoint,
) verificationObservation {
	observation := verificationObservation{
		entrypoint: entrypoint,
		memoKey:    verificationMemoKey(entrypoint),
		exitCode:   -1,
	}
	if run.replayPriorTimeout(&observation) {
		observation.evidence = run.commandEvidence(observation)
		return observation
	}
	run.execute(&observation)
	run.updateTimeoutMemo(observation)
	observation.evidence = run.commandEvidence(observation)
	return observation
}

func (run *projectVerificationRun) replayPriorTimeout(
	observation *verificationObservation,
) bool {
	prior, ok := run.runner.verificationTimeouts[observation.memoKey]
	if !ok || !prior.HaveFinger {
		return false
	}
	current, ok := run.fingerprint()
	if !ok || current != prior.Fingerprint {
		return false
	}
	observation.timedOut = true
	observation.tail = prior.Tail
	run.runner.note(fmt.Sprintf(
		"[senior-dev] full verification %s: %s — replaying recorded timeout "+
			"(tree unchanged since it hung; not paying the %ds ceiling again)\n",
		observation.entrypoint.Kind, observation.entrypoint.Command,
		fullVerificationTimeoutMS/1000,
	))
	return true
}

func (run *projectVerificationRun) execute(observation *verificationObservation) {
	entrypoint := observation.entrypoint
	bashInput := map[string]any{
		"command":    strictVerificationPreamble + entrypoint.Command,
		"timeout_ms": fullVerificationTimeoutMS,
	}
	if entrypoint.Workdir != "" {
		bashInput["workdir"] = entrypoint.Workdir
	}
	input, _ := json.Marshal(bashInput)
	toolResult, err := run.runner.runtime.registry.Execute(
		run.ctx,
		steploop.ToolCall{
			ID: run.runner.runtime.nextID("verification"), Name: "bash", Input: input,
			SessionID: run.runner.sessionID, Agent: "coder",
		},
	)
	if err == nil {
		observation.exitCode, observation.timedOut = verificationExit(toolResult.Metadata.Raw())
	}
	// A COMMAND CUT BY THE RUN'S OWN ENDING HAS NO EXIT STATUS. When the run's
	// context ends while the project's commands run — codeaf's stop, or the
	// wall clock — the command is killed half way, and what it left reads as a
	// failure it never reported. It is recorded the way a hang is: an
	// incomplete observation, never a red one, so neither is a submitted
	// candidate failed nor an unsubmitted tree restored on its account. A
	// command that had already exited clean before the stop keeps its pass.
	if run.ctx.Err() != nil && observation.exitCode != 0 {
		observation.exitCode, observation.timedOut = -1, true
	}
	output := toolResult.Output
	if err != nil {
		output = err.Error()
	}
	observation.tail = verificationOutputTail(output, 600)
	if entrypoint.Kind == fullverification.KindTest && !observation.timedOut {
		observation.noTests = noTestsReported(output)
	}
	// Suite-abort detection for the unsubmitted-tree finalizer
	// (verification_deadtree.go).
	if observation.exitCode != 0 && !observation.timedOut && suiteDeadOutput(output) {
		observation.suiteDead = true
	}
	if observation.exitCode != 0 && !observation.timedOut && safetyRegressionOutput(output) {
		observation.safetyRegression = true
	}
}

func verificationExit(raw json.RawMessage) (int, bool) {
	var metadata struct {
		ExitCode *int `json:"exitCode"`
	}
	if json.Unmarshal(raw, &metadata) == nil && metadata.ExitCode != nil {
		return *metadata.ExitCode, false
	}
	// bash.go omits exitCode on exactly one successful registry path: the
	// timeout branch, where it kills the process group after the ceiling.
	return -1, true
}

func (run *projectVerificationRun) updateTimeoutMemo(observation verificationObservation) {
	if !observation.timedOut {
		delete(run.runner.verificationTimeouts, observation.memoKey)
		return
	}
	if run.runner.verificationTimeouts == nil {
		run.runner.verificationTimeouts = map[string]timedOutEntrypoint{}
	}
	recorded, ok := run.fingerprint()
	run.runner.verificationTimeouts[observation.memoKey] = timedOutEntrypoint{
		Tail: observation.tail, Fingerprint: recorded, HaveFinger: ok,
	}
}

func (run *projectVerificationRun) commandEvidence(
	observation verificationObservation,
) map[string]any {
	entrypoint := observation.entrypoint
	evidence := map[string]any{
		"cmd": entrypoint.Command, "exit": float64(observation.exitCode),
		"tail": observation.tail, "source": entrypoint.Source,
		"kind": string(entrypoint.Kind), "buildExpected": run.plan.BuildExpected,
		"testExpected": run.plan.TestExpected,
	}
	if entrypoint.Workdir != "" {
		evidence["workdir"] = entrypoint.Workdir
	}
	if observation.timedOut {
		evidence["timedOut"] = true
	}
	if observation.suiteDead {
		evidence["suite_dead"] = true
	}
	if observation.noTests {
		evidence["no_tests"] = true
	}
	if observation.safetyRegression {
		evidence["safety_regression"] = true
	}
	return evidence
}

func (run *projectVerificationRun) record(observation verificationObservation) {
	run.result.Commands = append(run.result.Commands, observation.evidence)
	run.recordCommandLine(observation)
	entrypoint := observation.entrypoint
	// EACH COMMAND IS A STEP OF ITS OWN, reported after it ran and judged
	// exactly as before: a command that hung, or that the run's own ending cut,
	// has no exit to report.
	var exit *int
	if !observation.timedOut {
		code := observation.exitCode
		exit = &code
	}
	run.runner.events.verifyStep(entrypoint.Command, observation.tail, exit)
	run.runner.note(fmt.Sprintf(
		"[senior-dev] full verification %s: %s (exit=%d, source=%s)\n",
		entrypoint.Kind, entrypoint.Command, observation.exitCode, entrypoint.Source,
	))
	if observation.exitCode == 0 && !observation.noTests {
		return
	}
	if observation.noTests {
		run.result.NoTests = true
		run.result.NoTestsCommand = entrypoint.Command
	}
	// Every non-zero exit is a failure, full stop. Excusing a red command as
	// "pre-existing" on the strength of a pre-edit baseline probe would let a
	// red baseline route every later red into the excused path, and the run
	// would ship claiming it had verified. Whether an untouched test was
	// already red is a question for the implement loop, on demand, at the
	// moment of failure -- never a standing authority to ignore a failing
	// command at ship time.
	run.recordNewFailure(observation)
}

func (run *projectVerificationRun) recordCommandLine(observation verificationObservation) {
	entrypoint := observation.entrypoint
	if observation.timedOut {
		run.lines = append(run.lines, fmt.Sprintf(
			"- [%s] `%s` (source: %s) HUNG — killed at the %ds verification ceiling with no exit status%s",
			entrypoint.Kind, entrypoint.Command, entrypoint.Source,
			fullVerificationTimeoutMS/1000, verificationTailSuffix(observation.tail),
		))
		return
	}
	run.lines = append(run.lines, fmt.Sprintf(
		"- [%s] `%s` (source: %s) exit=%d%s",
		entrypoint.Kind, entrypoint.Command, entrypoint.Source, observation.exitCode,
		verificationTailSuffix(observation.tail),
	))
}

func (run *projectVerificationRun) recordNewFailure(observation verificationObservation) {
	entrypoint := observation.entrypoint
	if run.result.Failed == nil {
		failed := entrypoint
		run.result.Failed = &failed
	}
	run.result.NewFailures++
	issue := fmt.Sprintf(
		"project %s verification failed: `%s` exited %d",
		entrypoint.Kind, entrypoint.Command, observation.exitCode,
	)
	if observation.noTests {
		issue = fmt.Sprintf("no tests were found by `%s`", entrypoint.Command)
	}
	if observation.timedOut {
		run.result.TimedOut = true
		issue = fmt.Sprintf(
			"project %s verification did not complete: `%s` was killed after %ds "+
				"(the verification ceiling) without producing an exit status — the suite "+
				"hung, it did not report failures",
			entrypoint.Kind, entrypoint.Command, fullVerificationTimeoutMS/1000,
		)
	}
	if observation.tail != "" {
		issue += ": " + observation.tail
	}
	run.issues = append(run.issues, issue)
}

func (run *projectVerificationRun) recordMissingEntrypoints() {
	// Only demand entrypoints the discovered ecosystem is expected to have.
	run.recordMissingEntrypoint(
		run.plan.BuildExpected, fullverification.KindBuild,
		"(project build/typecheck entrypoint not found)",
		"project build/typecheck verification failed: no standard build/typecheck entrypoint was discoverable",
		"- [build] no standard project build/typecheck entrypoint discovered",
	)
	run.recordMissingEntrypoint(
		run.plan.TestExpected, fullverification.KindTest,
		"(project test entrypoint not found)",
		"project test verification failed: no standard test entrypoint was discoverable",
		"- [test] no standard project test entrypoint discovered",
	)
}

func (run *projectVerificationRun) recordMissingEntrypoint(
	expected bool,
	kind fullverification.EntrypointKind,
	command string,
	issue string,
	line string,
) {
	if !expected || planHasKind(run.plan, kind) {
		return
	}
	missing := fullverification.Entrypoint{
		Kind: kind, Command: command, Source: "manifest/CI/documentation discovery",
	}
	if run.result.Failed == nil {
		run.result.Failed = &missing
	}
	run.result.NewFailures++
	run.issues = append(run.issues, issue)
	run.lines = append(run.lines, line)
}

func (run *projectVerificationRun) recordVacuousVerification() bool {
	vacuous := len(run.plan.Entrypoints) == 0 &&
		!run.plan.BuildExpected && !run.plan.TestExpected
	if !vacuous {
		return false
	}
	run.lines = append(run.lines,
		"- [none] no project build/typecheck or test entrypoint exists to discover:",
		"  this workspace carries no language manifest, build system, or test suite.",
		"  Full-project verification is VACUOUS here — it proves nothing.")
	run.runner.note("[senior-dev] full project verification found nothing to run " +
		"(no language manifest, build system, or test suite) — vacuous pass\n")
	return true
}

func (run *projectVerificationRun) finish(vacuous bool) projectVerificationResult {
	if len(run.issues) == 1 {
		run.result.Failure = run.issues[0]
	} else if len(run.issues) > 1 {
		run.result.Failure = "project verification failed: " + strings.Join(run.issues, "; ")
	}
	run.result.Prompt = strings.Join(run.lines, "\n")
	status := "pass"
	data := map[string]any{"commands": run.result.Commands}
	if vacuous {
		data["vacuous"] = true
	}
	if run.result.Failed != nil {
		status = "fail"
		data["reason"] = run.result.Failure
	}
	run.runner.events.stage("verification", status, data)
	// The result is remembered against the tree it measured, so finalize can
	// consult the last verdict on an unchanged tree without re-verifying — the
	// runs that need the dead-tree check end with no wall left to verify.
	run.runner.rememberVerifiedTree(run.result)
	return run.result
}
