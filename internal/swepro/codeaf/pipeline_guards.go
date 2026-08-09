package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/session/auditconvergence"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/hygiene"
	"github.com/Agent-Field/swe-pro-go/internal/session/specidentifiers"
	"github.com/Agent-Field/swe-pro-go/internal/session/tamperchecks"
)

func (runner *pipeline) changedFiles(
	ctx context.Context, baseSHA string, excludeDeleted bool,
) []string {
	args := []string{"diff", "--name-only", "--no-renames"}
	if excludeDeleted {
		args = append(args, "--diff-filter=d")
	}
	if baseSHA != "" {
		args = append(args, baseSHA)
	}
	raw := gitOutput(ctx, runner.workspace, args...)
	if raw == "" {
		return []string{}
	}
	files := []string{}
	for _, file := range strings.Split(raw, "\n") {
		if file = strings.TrimSpace(file); file != "" {
			files = append(files, file)
		}
	}
	return files
}

func (runner *pipeline) applyAuditGuards(
	ctx context.Context,
	goal string,
	baseSHA string,
	audit auditorgate.GateResult,
) auditorgate.GateResult {
	changed := runner.changedFiles(ctx, baseSHA, false)
	if os.Getenv("CODEAF_TAMPER") != "0" {
		hunks := map[string]string{}
		for _, file := range changed {
			base := strings.ToLower(filepath.Base(file))
			if base != "pyproject.toml" && base != "setup.cfg" && base != "package.json" {
				continue
			}
			args := []string{"diff"}
			if baseSHA != "" {
				args = append(args, baseSHA)
			}
			args = append(args, "--", file)
			hunks[file] = gitOutput(ctx, runner.workspace, args...)
		}
		verdict := tampercheck.CheckTamper(tampercheck.TamperInput{
			ChangedFiles: changed, TaskText: goal, Hunks: hunks,
		})
		if !verdict.Clean {
			files := make([]string, 0, len(verdict.Findings))
			for _, finding := range verdict.Findings {
				files = append(files, finding.File)
			}
			runner.note(
				"[codeaf] verification-config tamper detected (files the task did not ask " +
					"to touch): " + strings.Join(files, ", ") + "\n",
			)
			audit = appendCorrectnessGuard(
				audit,
				tampercheck.TamperBlockerDetails(verdict),
				"verification-config tampering",
				[]string{
					"Revert the verification-config change(s) listed above (or, if the task genuinely requires them, make that intent explicit). Weakening a test/CI/coverage gate to make verification pass is never acceptable.",
				},
			)
		}
	}

	if os.Getenv("CODEAF_SPEC_IDS") != "0" {
		identifiers := specidentifiers.EnforceableIdentifiers(
			specidentifiers.ExtractSpecIdentifiers(goal),
		)
		identifiers = append(identifiers, runner.predictedIdentifiers...)
		if len(identifiers) > 0 {
			const fileCap = 200_000
			const totalCap = 2_000_000
			parts := []string{}
			total := 0
			for _, file := range changed {
				if total >= totalCap {
					break
				}
				path := filepath.Join(runner.workspace, file)
				stat, err := os.Stat(path)
				if err != nil || !stat.Mode().IsRegular() || stat.Size() > fileCap {
					continue
				}
				body, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				content := string(body)
				parts = append(parts, content)
				total += len(utf16.Encode([]rune(content)))
			}
			result := specidentifiers.CheckIdentifiersInTree(
				specidentifiers.CheckIdentifiersInput{
					Identifiers: identifiers, SearchText: strings.Join(parts, "\n"),
					ChangedFiles: &changed,
				},
			)
			if len(result.Missing) > 0 {
				values := make([]string, 0, len(result.Missing))
				for _, missing := range result.Missing {
					values = append(values, missing.Value)
				}
				runner.note(
					"[codeaf] spec identifiers missing from changed files (paraphrased?): " +
						strings.Join(values, ", ") + "\n",
				)
				audit = appendCorrectnessGuard(
					audit,
					specidentifiers.IdentifierBlockerDetails(result.Missing),
					"spec identifiers paraphrased",
					[]string{
						"Use the spec's exact identifiers verbatim (export names, file paths, option keys/values, marker strings, display names). Never paraphrase or abbreviate a name the spec states; when the spec abbreviates a user-facing name, follow the repo's sibling-component convention for the full spelling.",
					},
				)
			}
		}
	}
	return audit
}

func appendCorrectnessGuard(
	audit auditorgate.GateResult,
	details []string,
	reason string,
	hints []string,
) auditorgate.GateResult {
	severity := auditconvergence.SeverityCorrectness
	step := 0.0
	blockers := make([]auditorgate.Blocker, 0, len(details))
	for _, detail := range details {
		blockers = append(blockers, auditorgate.Blocker{
			Step: &step, Detail: detail, Severity: &severity,
		})
	}
	if audit.Status != auditorgate.StatusFail {
		verdict := auditorgate.AuditorVerdict{
			Verdict: auditorgate.VerdictFail, Blockers: blockers, RepairHints: hints,
		}
		audit.Status, audit.Verdict, audit.Reason =
			auditorgate.StatusFail, &verdict, &reason
		return audit
	}
	if audit.Verdict == nil {
		verdict := auditorgate.AuditorVerdict{Verdict: auditorgate.VerdictFail}
		audit.Verdict = &verdict
	}
	audit.Verdict.Blockers = append(audit.Verdict.Blockers, blockers...)
	return audit
}

func (runner *pipeline) runHygieneCleanup(
	ctx context.Context, baseSHA string, status auditorgate.GateStatus,
) {
	if status != auditorgate.StatusPass && status != auditorgate.StatusSkipped ||
		os.Getenv("CODEAF_HYGIENE") == "0" {
		return
	}
	changed := runner.changedFiles(ctx, baseSHA, true)
	verdict := hygiene.ClassifyLeftovers(hygiene.ClassifyInput{ChangedFiles: changed})
	block := hygiene.HygienePromptBlock(verdict)
	if block == nil {
		return
	}
	runner.note(fmt.Sprintf(
		"[codeaf] hygiene: %d leftover file(s) detected — dispatching cleanup pass\n",
		len(verdict.Scratch),
	))
	for _, finding := range verdict.Scratch {
		runner.note("  - " + finding.File + " (" + finding.Reason + ")\n")
	}
	model := agentjsonModel(firstModel(runner.pool.high))
	agent := runner.entryAgent
	if agent == "" {
		agent = runner.args.EntryAgent
	}
	if agent == "" {
		agent = "root-orchestrator"
	}
	_, err := runner.runtime.Prompt(ctx, oneShotPromptRequest{
		MessageID: runner.runtime.nextID("message"), SessionID: runner.sessionID,
		Model: oneShotPromptModel{ModelID: model.ModelID, ProviderID: model.ProviderID},
		Agent: agent, Parts: []any{oneShotTextPart{Type: "text", Text: *block}},
		Workspace: runner.workspace,
	})
	if err != nil {
		runner.note("[codeaf] hygiene cleanup prompt failed (soft warning):\n" + err.Error() + "\n")
		return
	}
	after := runner.changedFiles(ctx, baseSHA, true)
	if hygiene.IsTreeClean(after, nil) {
		runner.note("[codeaf] hygiene: tree clean after cleanup pass\n")
		return
	}
	residual := hygiene.ClassifyLeftovers(hygiene.ClassifyInput{ChangedFiles: after}).Scratch
	files := make([]string, 0, min(6, len(residual)))
	for _, finding := range residual {
		if len(files) >= 6 {
			break
		}
		files = append(files, finding.File)
	}
	runner.note(fmt.Sprintf(
		"[codeaf] hygiene: %d leftover file(s) remain after cleanup (soft warning, not blocking): %s\n",
		len(residual), strings.Join(files, ", "),
	))
}
