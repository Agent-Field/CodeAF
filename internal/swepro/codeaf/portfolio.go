// This file ports src/cli/cmd/portfolio.ts:1-542 from swe-pro commit 3b25a1a.
package codeaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/luby"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/mergeexecution"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/portfolio"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
)

const maxPortfolioAttempts = 8

var writeFailureTokens = []string{"ENOSPC", "disk is full", "no space left on device"}

// cliExitError carries process.exit(code) through the testable runCLI seam.
// Its empty message prevents main from adding output the TypeScript command
// did not print.
type cliExitError struct{ code int }

func (err *cliExitError) Error() string { return "" }

type cloneResult struct {
	OK     bool
	CoW    bool
	Reason string
}

type portfolioChildRequest struct {
	Args      []string
	Workspace string
	Tag       string
	LogPath   string
	Env       map[string]*string
}

type portfolioDeps struct {
	NowMillis  func() float64
	IsGitRepo  func(context.Context, string) bool
	CheckDisk  func(string) resourceguard.DiskEnvelope
	Clone      func(context.Context, string, string) cloneResult
	ResetClean func(context.Context, string)
	SpawnChild func(context.Context, portfolioChildRequest) (int, error)
	Sleep      mergeexecution.Sleep
	Rand       func() float64
}

func defaultPortfolioDeps() portfolioDeps {
	return portfolioDeps{
		NowMillis: func() float64 { return float64(time.Now().UnixMilli()) },
		IsGitRepo: func(ctx context.Context, directory string) bool {
			code, _ := runPortfolioProcess(ctx,
				[]string{"git", "rev-parse", "--is-inside-work-tree"}, directory,
			)
			return code == 0
		},
		CheckDisk: func(path string) resourceguard.DiskEnvelope {
			return resourceguard.CheckDiskEnvelope(path)
		},
		Clone: clonePortfolioWorkspace,
		ResetClean: func(ctx context.Context, clone string) {
			_, _ = runPortfolioProcess(ctx, []string{"git", "reset", "--hard"}, clone)
			_, _ = runPortfolioProcess(ctx,
				[]string{"git", "clean", "-fdq", "-e", "node_modules", "-e", ".venv", "-e", "target"},
				clone,
			)
		},
		SpawnChild: spawnPortfolioChild,
		Sleep:      mergeexecution.RealSleep,
		Rand:       rand.Float64,
	}
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (writer *synchronizedWriter) WriteString(value string) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	_, _ = io.WriteString(writer.w, value)
}

func runPortfolio(
	ctx context.Context,
	args cliArgs,
	stdout io.Writer,
	stderr io.Writer,
	deps portfolioDeps,
) error {
	out := &synchronizedWriter{w: stdout}
	notes := &synchronizedWriter{w: stderr}
	message := strings.TrimSpace(args.Message)
	if message == "" {
		notes.WriteString("[codeaf] missing message — pass a prompt as positional arg\n")
		return &cliExitError{code: 1}
	}

	directory := args.Directory
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	var err error
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	numberOfAttempts := portfolioAttemptNumber(args.Attempts)
	n := portfolioAttemptCount(numberOfAttempts)
	if !deps.IsGitRepo(ctx, directory) {
		notes.WriteString("[codeaf] --dir is not a git repository: " + directory + "\n")
		return &cliExitError{code: 1}
	}

	budgets := luby.LubyBudgets(numberOfAttempts)
	budgetText := make([]string, len(budgets))
	for index, budget := range budgets {
		budgetText[index] = jscompat.FormatNumber(budget)
	}
	mode := "ladder (cheapest-first escalation)"
	if args.Blast {
		mode = "blast (all concurrent)"
	}
	notes.WriteString(
		"[codeaf] portfolio: up to " + jscompat.FormatNumber(numberOfAttempts) +
			" attempts, mode=" + mode + ", luby budgets [" +
			strings.Join(budgetText, ",") + "], dir=" + directory + "\n",
	)

	portfolioRoot := directory + ".portfolio"
	tags := make([]string, n)
	for index := range tags {
		tags[index] = string(rune('a' + index))
	}

	launchAttempt := func(tag string, hard bool) (portfolio.AttemptResult, error) {
		clone := filepath.Join(portfolioRoot, tag)
		started := deps.NowMillis()
		envelope := deps.CheckDisk(directory)
		if !envelope.OK {
			notes.WriteString(
				"[codeaf] portfolio: attempt " + tag + " SKIPPED — disk below floor before clone " +
					"(free=" + jscompat.ToFixed(float64(envelope.FreeGB), 2) + "GB floor=" +
					jscompat.FormatNumber(float64(envelope.FloorGB)) + "GB)\n",
			)
			wall := deps.NowMillis() - started
			return portfolio.AttemptResult{
				Tag: tag, Workspace: clone, GateStatus: "unknown", WallMS: &wall,
			}, nil
		}
		cloned := deps.Clone(ctx, directory, clone)
		if !cloned.OK {
			notes.WriteString("[codeaf] portfolio: attempt " + tag + " clone FAILED: " + cloned.Reason + "\n")
			wall := deps.NowMillis() - started
			return portfolio.AttemptResult{
				Tag: tag, Workspace: clone, GateStatus: "unknown", WallMS: &wall,
			}, nil
		}
		deps.ResetClean(ctx, clone)
		kind := "deep-copy"
		if cloned.CoW {
			kind = "CoW"
		}
		modeLabel := "[cheap]"
		if hard {
			modeLabel = "[hard]"
		}
		notes.WriteString(
			"[codeaf] portfolio: attempt " + tag + " launching " + modeLabel +
				" (" + kind + " clone at " + clone + ")\n",
		)

		childArgs := []string{
			"--dir", clone,
			"--high", args.High,
			"--low", args.Low,
			"--variant", args.Variant,
			"--format", args.Format,
		}
		if hard {
			childArgs = append(childArgs, "--hard")
		}
		if args.EntryAgent != "" {
			childArgs = append(childArgs, "--entry-agent", args.EntryAgent)
		}
		if args.PRReady {
			childArgs = append(childArgs, "--pr-ready")
		}
		childArgs = append(childArgs, message)
		seed := tag
		environment := map[string]*string{"CODEAF_PORTFOLIO_SEED": &seed, "CODEAF_HARD": nil}
		if hard {
			hardValue := "1"
			environment["CODEAF_HARD"] = &hardValue
		}
		logPath := filepath.Join(clone, ".codeaf", "portfolio-attempt.log")
		code, spawnErr := deps.SpawnChild(ctx, portfolioChildRequest{
			Args: childArgs, Workspace: clone, Tag: tag,
			LogPath: logPath, Env: environment,
		})
		if spawnErr != nil {
			return portfolio.AttemptResult{}, spawnErr
		}
		wall := deps.NowMillis() - started
		notes.WriteString(
			"[codeaf] portfolio: attempt " + tag + " exited code=" +
				jscompat.FormatNumber(float64(code)) + " in " + jscompat.ToFixed(wall/1000, 1) + "s\n",
		)
		return readPortfolioAttempt(tag, clone, wall), nil
	}

	launch := func(tag string, index int) (portfolio.AttemptResult, error) {
		return launchAttempt(tag, index > 0)
	}
	stop := func(attempt portfolio.AttemptResult) bool {
		if attempt.GateStatus != "pass" || attempt.Verdict == nil {
			return false
		}
		verdict, ok := asAuditorVerdict(attempt.Verdict)
		return ok && auditorgate.IsAdmissibleVerdict(verdict).Admissible
	}
	var results []portfolio.AttemptResult
	if args.Blast {
		staggered := mergeexecution.WithStagger(
			func(tag string, index float64) (portfolio.AttemptResult, error) {
				return launchAttempt(tag, true)
			},
			mergeexecution.StaggerDeps{Sleep: deps.Sleep, Rand: deps.Rand},
		)
		results, err = portfolio.RunBlast(tags, func(tag string, index int) (portfolio.AttemptResult, error) {
			return staggered(tag, float64(index))
		})
	} else {
		results, err = portfolio.RunLadder(tags, launch, stop,
			func(previous portfolio.AttemptResult, nextTag string) {
				notes.WriteString(
					"[codeaf] portfolio: attempt " + previous.Tag + " did not pass " +
						"(gate=" + previous.GateStatus + ", blockers=" +
						jscompat.FormatNumber(float64(portfolioBlockerCount(previous.Verdict))) +
						") — escalating to attempt " + nextTag + "\n",
				)
			},
		)
	}
	if err != nil {
		return err
	}

	best := portfolio.SelectBest(results)
	out.WriteString("\n" + renderPortfolioTable(results) + "\n\n")
	warning := portfolio.BuildSuspectWarning(results)
	if warning != "" {
		out.WriteString(warning + "\n")
		notes.WriteString(warning)
	}
	if best == nil {
		notes.WriteString("[codeaf] portfolio: no attempts produced a result\n")
		return &cliExitError{code: 1}
	}
	out.WriteString("Winning attempt: " + best.Tag + " (gate: " + best.GateStatus + ")\n")
	out.WriteString("Winning workspace: " + best.Workspace + "\n\n")
	if len(results) > 1 {
		others := make([]portfolio.AttemptResult, 0, len(results)-1)
		for _, result := range results {
			if result.Tag != best.Tag {
				others = append(others, result)
			}
		}
		out.WriteString(portfolio.BuildMergeBrief(*best, others) + "\n")
	} else {
		notes.WriteString(
			"[codeaf] portfolio: stopped after 1 attempt (" + best.GateStatus +
				") — no escalation needed, nothing to merge\n",
		)
	}

	mergeAttempts := make([]mergeexecution.AttemptResult, len(results))
	for index, result := range results {
		mergeAttempts[index] = toMergeAttempt(result)
	}
	mergeBest := toMergeAttempt(*best)
	outcome, err := mergeexecution.RunReconciliation(mergeexecution.RunOptions{
		Attempts: mergeAttempts, Best: &mergeBest, NoReconcile: !args.Reconcile,
		Deps: mergeexecution.ReconcileDeps{
			CheckDisk: func(path string) (mergeexecution.DiskEnvelope, error) {
				envelope := deps.CheckDisk(path)
				return mergeexecution.DiskEnvelope{
					FreeBytes: float64(envelope.FreeBytes), FreeGB: float64(envelope.FreeGB),
					FloorGB: float64(envelope.FloorGB), OK: envelope.OK,
				}, nil
			},
			SpawnChild: func(winnerWorkspace, reconcileMessage string) (int, error) {
				childArgs := []string{
					"--dir", winnerWorkspace, "--hard",
					"--high", args.High, "--low", args.Low,
					"--variant", args.Variant, "--format", args.Format,
				}
				if args.EntryAgent != "" {
					childArgs = append(childArgs, "--entry-agent", args.EntryAgent)
				}
				if args.PRReady {
					childArgs = append(childArgs, "--pr-ready")
				}
				childArgs = append(childArgs, reconcileMessage)
				hard, seed := "1", "reconcile"
				return deps.SpawnChild(ctx, portfolioChildRequest{
					Args: childArgs, Workspace: winnerWorkspace, Tag: "reconcile",
					LogPath: filepath.Join(winnerWorkspace, ".codeaf",
						"portfolio-reconcile-"+jscompat.FormatNumber(deps.NowMillis())+".log"),
					Env: map[string]*string{"CODEAF_HARD": &hard, "CODEAF_PORTFOLIO_SEED": &seed},
				})
			},
			ReadVerdict: func(workspace string) (any, error) {
				verdict, _ := auditorgate.ReadVerdictFile(workspace)
				return verdict, nil
			},
			Log: func(line string) { notes.WriteString(line + "\n") },
		},
	})
	if err != nil {
		return err
	}
	if outcome.Ran {
		reconciled := readPortfolioAttempt("reconciled", best.Workspace, 0)
		withReconciled := append(append([]portfolio.AttemptResult{}, results...), reconciled)
		out.WriteString("\n" + renderPortfolioTable(withReconciled) + "\n")
		status := "gate=" + outcome.PostGateStatus
		if outcome.PostGateStatus == "pass" {
			status = "PASSED"
		}
		out.WriteString(
			"\n[codeaf] portfolio: reconciliation " + status + " on winner " + best.Workspace + "\n\n",
		)
	}

	anyPass := false
	for _, result := range results {
		if result.GateStatus == "pass" {
			anyPass = true
			break
		}
	}
	if outcome.Ran && outcome.PostGateStatus == "pass" {
		anyPass = true
	}
	summary := "[codeaf] portfolio: " + jscompat.FormatNumber(float64(len(results))) + " attempt(s) launched"
	if outcome.Ran {
		summary += " + 1 reconciliation (post-gate=" + outcome.PostGateStatus + ")"
	}
	if anyPass {
		summary += "; at least one PASSED\n"
	} else {
		summary += "; no attempt passed\n"
	}
	notes.WriteString(summary)
	if !anyPass {
		return &cliExitError{code: 1}
	}
	return nil
}

func portfolioAttemptNumber(value float64) float64 {
	return math.Max(1, math.Min(maxPortfolioAttempts, math.Floor(value)))
}

func portfolioAttemptCount(value float64) int {
	if math.IsNaN(value) {
		return 0
	}
	return int(value)
}

func runPortfolioProcess(ctx context.Context, argv []string, cwd string) (int, string) {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = cwd
	var stderr bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stderr.String()
	}
	return -1, err.Error()
}

func clonePortfolioWorkspace(ctx context.Context, source, destination string) cloneResult {
	_ = os.RemoveAll(destination)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o777); err != nil {
		return cloneResult{Reason: err.Error()}
	}
	if code, _ := runPortfolioProcess(ctx,
		[]string{"cp", "-c", "-R", source, destination}, parent,
	); code == 0 {
		return cloneResult{OK: true, CoW: true}
	}
	_ = os.RemoveAll(destination)
	code, stderr := runPortfolioProcess(ctx,
		[]string{"cp", "-R", source, destination}, parent,
	)
	if code == 0 {
		return cloneResult{OK: true}
	}
	if len(stderr) > 200 {
		stderr = stderr[:200]
	}
	return cloneResult{Reason: stderr}
}

func spawnPortfolioChild(ctx context.Context, request portfolioChildRequest) (int, error) {
	if err := os.MkdirAll(filepath.Dir(request.LogPath), 0o777); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(request.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	executable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	command := exec.CommandContext(ctx, executable, request.Args...)
	command.Env = mergePortfolioEnv(os.Environ(), request.Env)
	command.Stdout = logFile
	command.Stderr = logFile
	command.Stdin = nil
	err = command.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}

func mergePortfolioEnv(base []string, changes map[string]*string) []string {
	out := make([]string, 0, len(base)+len(changes))
	seen := map[string]bool{}
	for _, pair := range base {
		key, _, _ := strings.Cut(pair, "=")
		if value, changed := changes[key]; changed {
			if !seen[key] && value != nil {
				out = append(out, key+"="+*value)
			}
			seen[key] = true
			continue
		}
		out = append(out, pair)
	}
	for key, value := range changes {
		if !seen[key] && value != nil {
			out = append(out, key+"="+*value)
		}
	}
	return out
}

func readPortfolioAttempt(tag, clone string, wall float64) portfolio.AttemptResult {
	verdict, gateStatus, coverage, parsed := readPortfolioVerdict(clone)
	cost := derivePortfolioCost(clone)
	suspect := portfolio.DetectTelemetrySuspect(portfolio.TelemetryInput{
		GateStatus: gateStatus, VerdictParsed: parsed, ClauseCoverage: coverage,
		LogWriteFailure: portfolioLogHasWriteFailure(clone),
	})
	draft := mergeexecution.DraftPresenceSuspect(mergeexecution.DraftPresenceInput{
		VerdictParsed: parsed, DraftExists: fileExists(mergeexecution.AuditorDraftPath(clone)),
	})
	reasons := []string{}
	if suspect.Reason != "" {
		reasons = append(reasons, suspect.Reason)
	}
	if draft.Reason != "" {
		reasons = append(reasons, draft.Reason)
	}
	result := portfolio.AttemptResult{
		Tag: tag, Workspace: clone, GateStatus: gateStatus, Verdict: verdict,
		ClauseCoverage: coverage, CostUSD: cost, WallMS: &wall,
		TelemetrySuspect: suspect.Suspect || draft.Suspect,
	}
	if len(reasons) > 0 {
		reason := strings.Join(reasons, "; ")
		result.TelemetrySuspectReason = &reason
	}
	return result
}

func readPortfolioVerdict(workspace string) (any, string, []portfolio.ClauseCoverage, bool) {
	if verdict, _ := auditorgate.ReadVerdictFile(workspace); verdict != nil {
		coverage := make([]portfolio.ClauseCoverage, len(verdict.ClauseCoverage))
		for index, entry := range verdict.ClauseCoverage {
			coverage[index] = portfolio.ClauseCoverage{Clause: entry.Clause, Evidence: entry.Evidence}
		}
		return verdict, string(verdict.Verdict), coverage, true
	}
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "auditor-verdict-meta.json"))
	if err != nil {
		return nil, "unknown", nil, false
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(raw, &meta) != nil || len(meta["verdict"]) == 0 {
		return nil, "unknown", nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(meta["verdict"]))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		return nil, "unknown", nil, false
	}
	status, parsed := object["verdict"].(string)
	if !parsed {
		status = "unknown"
	}
	return object, status, coverageFromObject(object), parsed
}

func coverageFromObject(object map[string]any) []portfolio.ClauseCoverage {
	raw, ok := object["clause_coverage"].([]any)
	if !ok {
		return nil
	}
	out := make([]portfolio.ClauseCoverage, len(raw))
	for index, value := range raw {
		entry, _ := value.(map[string]any)
		out[index].Clause, _ = entry["clause"].(string)
		out[index].Evidence, _ = entry["evidence"].(string)
	}
	return out
}

func derivePortfolioCost(workspace string) *float64 {
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "outcomes.jsonl"))
	if err != nil {
		return nil
	}
	total := 0.0
	saw := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var object map[string]any
		if json.Unmarshal([]byte(line), &object) != nil {
			continue
		}
		cost, ok := object["costUsd"].(float64)
		if ok && !math.IsNaN(cost) && !math.IsInf(cost, 0) {
			total += cost
			saw = true
		}
	}
	if !saw {
		return nil
	}
	return &total
}

func portfolioLogHasWriteFailure(workspace string) bool {
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "portfolio-attempt.log"))
	if err != nil {
		return false
	}
	units := utf16.Encode([]rune(strings.ToValidUTF8(string(raw), "�")))
	if len(units) > 65536 {
		units = units[len(units)-65536:]
	}
	lower := strings.ToLower(string(utf16.Decode(units)))
	for _, token := range writeFailureTokens {
		if strings.Contains(lower, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func asAuditorVerdict(value any) (auditorgate.AuditorVerdict, bool) {
	if verdict, ok := value.(*auditorgate.AuditorVerdict); ok && verdict != nil {
		return *verdict, true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return auditorgate.AuditorVerdict{}, false
	}
	var verdict auditorgate.AuditorVerdict
	if json.Unmarshal(raw, &verdict) != nil {
		return auditorgate.AuditorVerdict{}, false
	}
	return verdict, verdict.Verdict != ""
}

func portfolioBlockerCount(value any) int {
	if value == nil {
		return 0
	}
	if object, ok := value.(map[string]any); ok {
		if blockers, ok := object["blockers"].([]any); ok {
			return len(blockers)
		}
	}
	if verdict, ok := asAuditorVerdict(value); ok {
		return len(verdict.Blockers)
	}
	return 0
}

func renderPortfolioTable(attempts []portfolio.AttemptResult) string {
	header := []string{"attempt", "gate", "coverage", "cost", "wall", "suspect"}
	rows := make([][]string, len(attempts))
	for index, attempt := range attempts {
		cost := "n/a"
		if attempt.CostUSD != nil {
			cost = "$" + jscompat.ToFixed(*attempt.CostUSD, 3)
		}
		wall := "n/a"
		if attempt.WallMS != nil {
			wall = jscompat.ToFixed(*attempt.WallMS/1000, 1) + "s"
		}
		suspect := "-"
		if attempt.TelemetrySuspect {
			suspect = "SUSPECT"
		}
		rows[index] = []string{
			attempt.Tag, attempt.GateStatus,
			jscompat.FormatNumber(float64(len(attempt.ClauseCoverage))),
			cost, wall, suspect,
		}
	}
	widths := make([]int, len(header))
	for column, cell := range header {
		widths[column] = len(cell)
		for _, row := range rows {
			if len(row[column]) > widths[column] {
				widths[column] = len(row[column])
			}
		}
	}
	line := func(cells []string) string {
		padded := make([]string, len(cells))
		for index, cell := range cells {
			padded[index] = cell + strings.Repeat(" ", widths[index]-len(cell))
		}
		return strings.Join(padded, "  ")
	}
	dashes := make([]string, len(widths))
	for index, width := range widths {
		dashes[index] = strings.Repeat("-", width)
	}
	lines := []string{line(header), line(dashes)}
	for _, row := range rows {
		lines = append(lines, line(row))
	}
	return strings.Join(lines, "\n")
}

func toMergeAttempt(attempt portfolio.AttemptResult) mergeexecution.AttemptResult {
	coverage := make([]mergeexecution.ClauseCoverage, len(attempt.ClauseCoverage))
	for index, entry := range attempt.ClauseCoverage {
		coverage[index] = mergeexecution.ClauseCoverage{Clause: entry.Clause, Evidence: entry.Evidence}
	}
	return mergeexecution.AttemptResult{
		Tag: attempt.Tag, Workspace: attempt.Workspace, GateStatus: attempt.GateStatus,
		Verdict: attempt.Verdict, ClauseCoverage: coverage, CostUSD: attempt.CostUSD,
		WallMS: attempt.WallMS, TelemetrySuspect: attempt.TelemetrySuspect,
		TelemetrySuspectReason: attempt.TelemetrySuspectReason,
	}
}
