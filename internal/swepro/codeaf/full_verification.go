package codeaf

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/fullverification"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/tool"
)

const fullVerificationTimeoutMS = 600_000

// The registry may be configured with another shell. POSIX-like shells with
// pipefail honor this strict mode; shells without it reject the preamble and
// therefore fail verification closed instead of trusting a masked pipeline.
const strictVerificationPreamble = "set -euo pipefail\n"

type projectVerificationResult struct {
	Commands []any
	Prompt   string
	Failed   *fullverification.Entrypoint
	Failure  string
	// TimedOut is set when at least one entrypoint was killed at the
	// fullVerificationTimeoutMS ceiling without ever producing an exit status.
	// A hung suite is an INCOMPLETE observation, not a red one.
	TimedOut bool
}

// timedOutEntrypoint records an entrypoint that exhausted the harness ceiling,
// together with the worktree fingerprint it hung against.
type timedOutEntrypoint struct {
	Tail        string
	Fingerprint string
	HaveFinger  bool
}

func verificationMemoKey(entrypoint fullverification.Entrypoint) string {
	return entrypoint.Workdir + "\x00" + entrypoint.Command
}

func projectVerificationPassVerdict(
	result projectVerificationResult,
) auditorgate.AuditorVerdict {
	reproduced := true
	notes := "Harness-discovered project build/test entrypoints exited 0."
	return auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictPass,
		Step2Signal: &auditorgate.Step2Signal{
			Reproduced: &reproduced, Commands: result.Commands, Notes: &notes,
		},
		Blockers: []auditorgate.Blocker{},
	}
}

// runProjectVerification executes the discovered project-wide entrypoints
// through the live Bash registry. The gate deliberately disables its test memo
// while retaining the registry's process-derived exitCode metadata.
func (runner *pipeline) runProjectVerification(
	ctx context.Context,
) projectVerificationResult {
	plan := fullverification.Discover(runner.workspace)
	result := projectVerificationResult{Commands: []any{}}
	lines := []string{
		"# Harness-executed full project verification",
		"The harness independently discovered and ran the standard project entrypoints",
		"below in fresh Bash subprocesses. These are process-derived command/exit",
		"observations, not worker claims. Consult them, but still run and cite your own",
		"fresh verification commands as required by the auditor protocol.",
	}
	issues := []string{}
	// Re-running a command that already hung cannot yield a different answer
	// while the files it ran against are byte identical, and each attempt costs
	// the full 600s ceiling. werkzeug-3146 spent 1800s of a 3600s budget doing
	// exactly that, three times, producing byte-identical output.
	//
	// Resolved lazily and at most once per pass: a run where nothing ever hangs
	// must not pay for a worktree scan, and a failing audit is required not to
	// fingerprint the tree at all (pipeline_prready_test.go).
	var currentFingerprint string
	var haveFingerprint, resolvedFingerprint bool
	fingerprint := func() (string, bool) {
		if !resolvedFingerprint {
			currentFingerprint, haveFingerprint = runner.worktreeFingerprint(ctx)
			resolvedFingerprint = true
		}
		return currentFingerprint, haveFingerprint
	}
	for _, entrypoint := range plan.Entrypoints {
		memoKey := verificationMemoKey(entrypoint)
		exitCode := -1
		timedOut := false
		var tail string
		prior, hasPrior := runner.verificationTimeouts[memoKey]
		priorStillApplies := false
		if hasPrior && prior.HaveFinger {
			if current, ok := fingerprint(); ok && current == prior.Fingerprint {
				priorStillApplies = true
			}
		}
		if priorStillApplies {
			timedOut = true
			tail = prior.Tail
			runner.note(fmt.Sprintf(
				"[codeaf] full verification %s: %s — replaying recorded timeout "+
					"(tree unchanged since it hung; not paying the %ds ceiling again)\n",
				entrypoint.Kind, entrypoint.Command, fullVerificationTimeoutMS/1000,
			))
		} else {
			bashInput := map[string]any{
				"command":    strictVerificationPreamble + entrypoint.Command,
				"timeout_ms": fullVerificationTimeoutMS,
			}
			if entrypoint.Workdir != "" {
				bashInput["workdir"] = entrypoint.Workdir
			}
			input, _ := json.Marshal(bashInput)
			toolResult, err := runner.runtime.registry.Execute(tool.WithTestMemoDisabled(ctx), steploop.ToolCall{
				ID: runner.runtime.nextID("verification"), Name: "bash", Input: input,
				SessionID: runner.sessionID, Agent: "auditor",
			})
			if err == nil {
				var metadata struct {
					ExitCode *int `json:"exitCode"`
				}
				if json.Unmarshal(toolResult.Metadata.Raw(), &metadata) == nil && metadata.ExitCode != nil {
					exitCode = *metadata.ExitCode
				} else {
					// bash.go omits exitCode on exactly one path: the timeout
					// branch, where it kills the process group after the
					// ceiling. Every other non-exit path returns an error.
					timedOut = true
				}
			}
			output := toolResult.Output
			if err != nil {
				output = err.Error()
			}
			tail = verificationOutputTail(output, 600)
			if timedOut {
				if runner.verificationTimeouts == nil {
					runner.verificationTimeouts = map[string]timedOutEntrypoint{}
				}
				recorded, ok := fingerprint()
				runner.verificationTimeouts[memoKey] = timedOutEntrypoint{
					Tail: tail, Fingerprint: recorded, HaveFinger: ok,
				}
			} else {
				delete(runner.verificationTimeouts, memoKey)
			}
		}
		commandEvidence := map[string]any{
			"cmd": entrypoint.Command, "exit": float64(exitCode), "tail": tail,
			"source": entrypoint.Source, "kind": string(entrypoint.Kind),
			"buildExpected": plan.BuildExpected,
			"testExpected":  plan.TestExpected,
		}
		if entrypoint.Workdir != "" {
			commandEvidence["workdir"] = entrypoint.Workdir
		}
		if timedOut {
			commandEvidence["timedOut"] = true
		}
		result.Commands = append(result.Commands, commandEvidence)
		if timedOut {
			lines = append(lines, fmt.Sprintf(
				"- [%s] `%s` (source: %s) HUNG — killed at the %ds harness ceiling with no exit status%s",
				entrypoint.Kind, entrypoint.Command, entrypoint.Source,
				fullVerificationTimeoutMS/1000, verificationTailSuffix(tail),
			))
		} else {
			lines = append(lines, fmt.Sprintf(
				"- [%s] `%s` (source: %s) exit=%d%s",
				entrypoint.Kind, entrypoint.Command, entrypoint.Source, exitCode,
				verificationTailSuffix(tail),
			))
		}
		runner.note(fmt.Sprintf(
			"[codeaf] full verification %s: %s (exit=%d, source=%s)\n",
			entrypoint.Kind, entrypoint.Command, exitCode, entrypoint.Source,
		))
		if exitCode != 0 {
			if result.Failed == nil {
				failed := entrypoint
				result.Failed = &failed
			}
			issue := fmt.Sprintf(
				"project %s verification failed: `%s` exited %d",
				entrypoint.Kind, entrypoint.Command, exitCode,
			)
			if timedOut {
				result.TimedOut = true
				issue = fmt.Sprintf(
					"project %s verification did not complete: `%s` was killed after %ds "+
						"(the harness ceiling) without producing an exit status — the suite "+
						"hung, it did not report failures",
					entrypoint.Kind, entrypoint.Command, fullVerificationTimeoutMS/1000,
				)
			}
			if tail != "" {
				issue += ": " + tail
			}
			issues = append(issues, issue)
		}
	}
	// Only demand a build/typecheck when the ecosystem actually has one.
	// Requiring it unconditionally would fail every plain-Python repo, which
	// has tests to run but nothing to compile.
	if plan.BuildExpected && !planHasKind(plan, fullverification.KindBuild) {
		missing := fullverification.Entrypoint{
			Kind: fullverification.KindBuild, Command: "(project build/typecheck entrypoint not found)",
			Source: "manifest/CI/documentation discovery",
		}
		if result.Failed == nil {
			result.Failed = &missing
		}
		issues = append(issues, "project build/typecheck verification failed: no standard build/typecheck entrypoint was discoverable")
		lines = append(lines, "- [build] no standard project build/typecheck entrypoint discovered")
	}
	// Symmetric with the build gate above. Demanding a test entrypoint from
	// every workspace failed the one kind that can never supply it: a
	// workspace with no project in it. That failure is unrepairable, so the
	// audit-fix loop reran forever against a deliverable that was already done.
	if plan.TestExpected && !planHasKind(plan, fullverification.KindTest) {
		missing := fullverification.Entrypoint{
			Kind: fullverification.KindTest, Command: "(project test entrypoint not found)",
			Source: "manifest/CI/documentation discovery",
		}
		if result.Failed == nil {
			result.Failed = &missing
		}
		issues = append(issues, "project test verification failed: no standard test entrypoint was discoverable")
		lines = append(lines, "- [test] no standard project test entrypoint discovered")
	}
	// Neither gate demands anything and discovery found nothing to run. Say so
	// explicitly: the audit stage reads this prompt, and a verification section
	// that simply listed no commands would read as "everything passed" when the
	// truth is "there was nothing here to run".
	vacuous := len(plan.Entrypoints) == 0 && !plan.BuildExpected && !plan.TestExpected
	if vacuous {
		lines = append(lines,
			"- [none] no project build/typecheck or test entrypoint exists to discover:",
			"  this workspace carries no language manifest, build system, or test suite.",
			"  Full-project verification is VACUOUS here — it proves nothing. The",
			"  acceptance contract is the effective gate for this task.")
		runner.note("[codeaf] full project verification found nothing to run " +
			"(no language manifest, build system, or test suite) — vacuous pass, " +
			"the acceptance contract is the effective gate\n")
	}
	if len(issues) == 1 {
		result.Failure = issues[0]
	} else if len(issues) > 1 {
		result.Failure = "project verification failed: " + strings.Join(issues, "; ")
	}
	result.Prompt = strings.Join(lines, "\n")
	eventStatus := "pass"
	eventData := map[string]any{"commands": result.Commands}
	if vacuous {
		eventData["vacuous"] = true
	}
	if result.Failed != nil {
		eventStatus = "fail"
		eventData["reason"] = result.Failure
	}
	runner.events.stage("verification", eventStatus, eventData)
	return result
}

func planHasKind(plan fullverification.Plan, kind fullverification.EntrypointKind) bool {
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind == kind {
			return true
		}
	}
	return false
}

func verificationTailSuffix(tail string) string {
	if tail == "" {
		return ""
	}
	return " — " + strings.ReplaceAll(tail, "\n", " ")
}

func verificationOutputTail(output string, limit int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return suffixUTF16(output, limit)
}

func projectVerificationFailure(
	result projectVerificationResult,
) auditorgate.GateResult {
	step := 2.0
	severity := auditconvergence.SeverityCorrectness
	reproduced := false
	detail := result.Failure
	if detail == "" {
		detail = "full project verification did not produce a successful command/exit observation"
	}
	reason := detail
	repairHint := "Fix the failing project-wide verification, then rerun the exact command until it exits 0. Do not replace it with targeted package checks."
	if result.Failed != nil && strings.HasPrefix(result.Failed.Command, "(") {
		if result.Failed.Kind == fullverification.KindBuild {
			repairHint = "Declare the project's standard build/typecheck and test entrypoints in its manifest, task runner, CI, or repository instructions, then make both full entrypoints exit 0."
		} else {
			repairHint = "Declare the project's standard test entrypoint in its manifest, task runner, CI, or repository instructions, then make that full entrypoint exit 0."
		}
	} else if result.TimedOut {
		// The default hint ("rerun the exact command until it exits 0") is the
		// worst possible advice for a hang: the rerun costs the full ceiling
		// again and ends identically. Severity stays `correctness` on purpose —
		// an unverified suite must not let a run go green.
		repairHint = "The full verification entrypoint HUNG — it was killed at the harness time ceiling and never reported pass or fail. Do NOT rerun it verbatim; it will hang again and consume the same budget. Identify which test hangs (the tail above shows how far it got), then run the suite in narrower slices to isolate it. If the hang is pre-existing and unrelated to your change, say so explicitly and cite the narrower slices that do pass."
	}
	verdict := auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictFail,
		Step2Signal: &auditorgate.Step2Signal{
			Reproduced: &reproduced, Commands: result.Commands,
		},
		Blockers: []auditorgate.Blocker{{
			Step: &step, Detail: detail, Severity: &severity,
		}},
		RepairHints: []string{repairHint},
	}
	return auditorgate.GateResult{
		Status: auditorgate.StatusFail, Verdict: &verdict, Reason: &reason,
	}
}
