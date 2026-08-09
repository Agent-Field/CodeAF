// This file ports auditor verdict/draft/provenance persistence and git capture
// from src/session/auditor-gate.ts:437-981. All verdict readers and forensic
// breadcrumbs are fail-closed/best-effort exactly as in the source.
package auditorgate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/artifactregistry"
)

const (
	verdictRelative    = ".codeaf/auditor-verdict.json"
	draftRelative      = ".codeaf/auditor-verdict.draft.json"
	provenanceRelative = ".codeaf/auditor-verdict-meta.json"
)

func UnlinkVerdictFile(workspace string) {
	_ = os.Remove(filepath.Join(workspace, verdictRelative))
}

type AuditorVerdictDraft struct {
	StartedAt       float64 `json:"startedAt"`
	Mode            string  `json:"mode"`
	ClauseCount     float64 `json:"clauseCount"`
	MatrixCellCount float64 `json:"matrixCellCount"`
}

func WriteVerdictDraft(workspace string, draft AuditorVerdictDraft) {
	dir := filepath.Join(workspace, ".codeaf")
	if os.MkdirAll(dir, 0o777) != nil {
		return
	}
	body, err := jscompat.Stringify(draft)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(workspace, draftRelative), body, 0o666)
}

func RemoveVerdictDraft(workspace string) {
	_ = os.Remove(filepath.Join(workspace, draftRelative))
}

func ReadVerdictFile(workspace string) (*AuditorVerdict, error) {
	tried := map[string]bool{}
	tryRead := func(path string) *AuditorVerdict {
		if tried[path] {
			return nil
		}
		tried[path] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var verdict AuditorVerdict
		if json.Unmarshal(raw, &verdict) != nil || validateAuditorVerdict(verdict) != nil {
			return nil
		}
		normalized := normalizeVerdict(verdict)
		return &normalized
	}

	if verdict := tryRead(filepath.Join(workspace, verdictRelative)); verdict != nil {
		return verdict, nil
	}
	var walk func(string, int) *AuditorVerdict
	walk = func(dir string, depth int) *AuditorVerdict {
		if depth > 3 {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".git") {
				continue
			}
			full := filepath.Join(dir, entry.Name())
			if entry.IsDir() && entry.Name() == ".codeaf" {
				if verdict := tryRead(filepath.Join(full, "auditor-verdict.json")); verdict != nil {
					return verdict
				}
				continue
			}
			if !entry.IsDir() {
				continue
			}
			subEntries, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			nestedRepo := false
			for _, subEntry := range subEntries {
				if subEntry.Name() == ".git" {
					nestedRepo = true
					break
				}
			}
			if nestedRepo {
				continue
			}
			if verdict := walk(full, depth+1); verdict != nil {
				return verdict
			}
		}
		return nil
	}
	return walk(workspace, 0), nil
}

// PersistVerdict records a pipeline-adjudicated verdict atomically. This is
// the same path used by gateSession for converted inadmissible passes.
func PersistVerdict(workspace string, verdict AuditorVerdict) error {
	return writeVerdictAtomic(workspace, verdict)
}

func normalizeVerdict(verdict AuditorVerdict) AuditorVerdict {
	fields := []orderedField{field("verdict", verdict.Verdict)}
	if verdict.Commands != nil {
		fields = append(fields, field("commands", verdict.Commands))
	}
	if verdict.Notes != nil {
		fields = append(fields, field("notes", *verdict.Notes))
	}
	if verdict.Step1Goal != nil {
		fields = append(fields, field("step1_goal", *verdict.Step1Goal))
	}
	if verdict.Step2Signal != nil {
		signal := Step2Signal{
			Reproduced:          verdict.Step2Signal.Reproduced,
			Commands:            verdict.Step2Signal.Commands,
			SpecExamplesMatched: verdict.Step2Signal.SpecExamplesMatched,
			Notes:               verdict.Step2Signal.Notes,
		}
		fields = append(fields, field("step2_signal", signal))
	}
	if verdict.Step3Scope != nil {
		fields = append(fields, field("step3_scope", verdict.Step3Scope))
	}
	if verdict.Step4Structural != nil {
		fields = append(fields, field("step4_structural", verdict.Step4Structural))
	}
	if verdict.Blockers != nil {
		blockers := make([]Blocker, 0, len(verdict.Blockers))
		for _, blocker := range verdict.Blockers {
			clean := Blocker{
				File: blocker.File, Line: blocker.Line, Step: blocker.Step,
				Detail: blocker.Detail, Severity: blocker.Severity,
			}
			blockers = append(blockers, clean)
		}
		fields = append(fields, field("blockers", blockers))
	}
	if verdict.RepairHints != nil {
		fields = append(fields, field("repair_hints", verdict.RepairHints))
	}
	if verdict.ClauseCoverage != nil {
		fields = append(fields, field("clause_coverage", verdict.ClauseCoverage))
	}
	if verdict.Step2CAcceptance != nil {
		fields = append(fields, field("step2c_acceptance", verdict.Step2CAcceptance))
	}
	return newVerdictOrdered(fields...)
}

type AuditProvenance struct {
	AuditSHA   string         `json:"auditSha"`
	AuditCycle float64        `json:"auditCycle"`
	Verdict    AuditorVerdict `json:"verdict"`
}

func ReadAuditProvenance(workspace string) *AuditProvenance {
	raw, err := os.ReadFile(filepath.Join(workspace, provenanceRelative))
	if err != nil {
		return nil
	}
	var provenance AuditProvenance
	if json.Unmarshal(raw, &provenance) != nil ||
		provenance.AuditSHA == "" ||
		validateAuditorVerdict(provenance.Verdict) != nil {
		return nil
	}
	return &provenance
}

func WriteAuditProvenance(workspace string, provenance AuditProvenance) error {
	dir := filepath.Join(workspace, ".codeaf")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	body, err := jscompat.StringifyIndent(provenance)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(workspace, provenanceRelative), body, 0o666); err != nil {
		return err
	}
	artifactregistry.PutArtifact(workspace, "verdict", string(body))
	return nil
}

func readFrontierCallCount(workspace string) float64 {
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "frontier-calls.json"))
	if err != nil {
		return 0
	}
	var object struct {
		Count float64 `json:"count"`
	}
	if json.Unmarshal(raw, &object) != nil || mathIsNonFinite(object.Count) {
		return 0
	}
	return object.Count
}

func bumpFrontierCallCount(workspace string) float64 {
	next := readFrontierCallCount(workspace) + 1
	dir := filepath.Join(workspace, ".codeaf")
	if os.MkdirAll(dir, 0o777) == nil {
		body, _ := jscompat.Stringify(struct {
			Count float64 `json:"count"`
		}{next})
		_ = os.WriteFile(filepath.Join(dir, "frontier-calls.json"), body, 0o666)
	}
	return next
}

func mathIsNonFinite(number float64) bool {
	return number != number || number > 1.7976931348623157e308 || number < -1.7976931348623157e308
}

type ProcessResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

type ProcessRunner interface {
	Run(argv []string, cwd string) (ProcessResult, error)
}

type ProcessRunnerFunc func(argv []string, cwd string) (ProcessResult, error)

func (f ProcessRunnerFunc) Run(argv []string, cwd string) (ProcessResult, error) {
	return f(argv, cwd)
}

type ExecProcessRunner struct{}

func (ExecProcessRunner) Run(argv []string, cwd string) (ProcessResult, error) {
	if len(argv) == 0 {
		return ProcessResult{Code: 1}, errors.New("command required")
	}
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = cwd
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return ProcessResult{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return ProcessResult{
			Code: exitError.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		}, nil
	}
	return ProcessResult{Code: 1, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

func runNothrow(runner ProcessRunner, argv []string, cwd string) ProcessResult {
	if runner == nil {
		runner = ExecProcessRunner{}
	}
	result, err := runner.Run(argv, cwd)
	if err != nil {
		if result.Code == 0 {
			result.Code = 1
		}
		result.Stderr = []byte(err.Error())
	}
	return result
}

func stageAll(runner ProcessRunner, workspace string) {
	runNothrow(runner, []string{"git", "add", "-A"}, workspace)
}

func changedFilesForAudit(runner ProcessRunner, workspace string, baseSHA *string) []string {
	rangeArg := "HEAD"
	if baseSHA != nil {
		rangeArg = *baseSHA
	}
	result := runNothrow(runner,
		[]string{"git", "diff", "--cached", "--name-only", rangeArg},
		workspace,
	)
	out := []string{}
	for _, line := range splitCRLF(validUTF8OrReplacement(result.Stdout)) {
		if line = jscompat.Trim(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func captureSessionDiff(runner ProcessRunner, workspace string, baseSHA *string) string {
	stageAll(runner, workspace)
	rangeArg := "HEAD"
	if baseSHA != nil {
		rangeArg = *baseSHA
	}
	result := runNothrow(runner,
		[]string{"git", "diff", "--cached", "--no-color", rangeArg},
		workspace,
	)
	raw := validUTF8OrReplacement(result.Stdout)
	const limit = 16000
	if utf16Len(raw) <= limit {
		return raw
	}
	fullCmd := "git diff --cached " + rangeArg
	return sliceUTF16(raw, 0, limit) +
		"\n\n[diff truncated at 16000 chars — run `" + fullCmd + "` in " +
		workspace + " to see the full diff]"
}

func captureDiffStat(runner ProcessRunner, workspace string, baseSHA *string) string {
	stageAll(runner, workspace)
	rangeArg := "HEAD"
	if baseSHA != nil {
		rangeArg = *baseSHA
	}
	result := runNothrow(runner,
		[]string{"git", "diff", "--cached", "--stat", rangeArg},
		workspace,
	)
	text := validUTF8OrReplacement(result.Stdout)
	return sliceUTF16(text, 0, min(3000, utf16Len(text)))
}

func hasChanges(runner ProcessRunner, workspace string, baseSHA *string) bool {
	stageAll(runner, workspace)
	rangeArg := "HEAD"
	if baseSHA != nil {
		rangeArg = *baseSHA
	}
	result := runNothrow(runner,
		[]string{"git", "diff", "--cached", "--name-only", rangeArg},
		workspace,
	)
	return jscompat.Trim(validUTF8OrReplacement(result.Stdout)) != ""
}

func readHeadSHA(runner ProcessRunner, workspace string) *string {
	result := runNothrow(runner, []string{"git", "rev-parse", "HEAD"}, workspace)
	sha := jscompat.Trim(validUTF8OrReplacement(result.Stdout))
	if result.Code != 0 || sha == "" {
		return nil
	}
	return &sha
}

func changedFilesSinceAudit(
	runner ProcessRunner, workspace string, priorSHA, currentSHA *string,
) []string {
	if priorSHA == nil || currentSHA == nil || *priorSHA == *currentSHA {
		return []string{}
	}
	result := runNothrow(runner,
		[]string{"git", "diff", "--name-only", *priorSHA, *currentSHA},
		workspace,
	)
	return ComputeChangedSince(ChangedSinceArgs{
		PriorSHA: priorSHA, CurrentSHA: currentSHA,
		DiffOutput: validUTF8OrReplacement(result.Stdout),
	})
}

func pointer(number float64) *float64 { return &number }

func parseNumberOr(raw string, fallback float64) float64 {
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return number
}
