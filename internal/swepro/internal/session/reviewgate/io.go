// This file ports the git/worktree and skip-detection shell from
// src/session/review-gate.ts:247-395 and 1578-1612.
package reviewgate

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/attribution"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/util"
)

type CommandResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

type CommandFunc func(ctx context.Context, argv []string, cwd string) CommandResult

const plumbingCommandTimeout = 5 * time.Minute

func runCommand(ctx context.Context, argv []string, cwd string) CommandResult {
	commandCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), plumbingCommandTimeout)
	defer cancel()
	result, err := util.RunProcess(commandCtx, argv, util.RunOptions{
		ProcessOptions: util.ProcessOptions{Cwd: cwd},
		NoThrow:        true,
	})
	if err != nil {
		return CommandResult{Code: 1, Stderr: []byte(err.Error())}
	}
	return CommandResult{
		Code: result.Code, Stdout: result.Stdout, Stderr: result.Stderr,
	}
}

type GateSkipResult struct {
	Skip   bool   `json:"skip"`
	Reason string `json:"reason,omitempty"`
}

func ShouldSkipReview(
	ctx context.Context,
	worktree string,
	baseSHA string,
	config Config,
	run CommandFunc,
) GateSkipResult {
	if !config.Enabled {
		return GateSkipResult{Skip: true, Reason: "disabled"}
	}
	if run == nil {
		run = runCommand
	}
	run(ctx, []string{"git", "add", "-A"}, worktree)
	diff := run(ctx,
		[]string{"git", "diff", "--cached", "--name-only", baseSHA},
		worktree,
	)
	files := nonemptyTrimmedLines(string(diff.Stdout))
	if len(files) == 0 {
		return GateSkipResult{Skip: true, Reason: "no_changes"}
	}
	allDocs := true
	for _, file := range files {
		if !MatchesAnyGlob(file, config.SkipGlobs) {
			allDocs = false
			break
		}
	}
	if allDocs {
		return GateSkipResult{Skip: true, Reason: "docs_only"}
	}
	return GateSkipResult{Skip: false}
}

func nonemptyTrimmedLines(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		if trimmed := jscompat.Trim(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type gateWorktree struct {
	Path    string
	BaseSHA string
	Branch  string
}

func (s *Service) allocateGateWorktree(
	ctx context.Context,
	workspace string,
	taskID string,
	baseRef string,
) (*gateWorktree, error) {
	branch := "plandb/" + taskID
	path := filepath.Join(workspace, ".plandb", "wt-"+taskID)
	baseRev := s.run(ctx, []string{"git", "rev-parse", "--verify", baseRef + "^{commit}"}, workspace)
	if baseRev.Code != 0 {
		return nil, fmt.Errorf(
			"rev-parse %s: code=%d %s",
			baseRef,
			baseRev.Code,
			compact(jscompat.Trim(string(baseRev.Stderr)), 300),
		)
	}
	baseSHA := jscompat.Trim(string(baseRev.Stdout))
	s.run(ctx, []string{"git", "worktree", "remove", "--force", path}, workspace)
	s.run(ctx, []string{"git", "branch", "-D", branch}, workspace)
	create := s.run(ctx,
		[]string{"git", "worktree", "add", "-b", branch, path, baseRef},
		workspace,
	)
	if create.Code != 0 {
		return nil, fmt.Errorf(
			"worktree add %s: code=%d %s",
			path,
			create.Code,
			compact(jscompat.Trim(string(create.Stderr)), 300),
		)
	}
	_, _ = util.EnsureCodeafExcluded(ctx, path)
	_ = util.SuppressCaseCollisions(ctx, path)
	return &gateWorktree{Path: path, BaseSHA: baseSHA, Branch: branch}, nil
}

func (s *Service) cleanupWorktree(
	ctx context.Context,
	workspace string,
	worktreePath string,
	branch string,
) {
	s.run(ctx,
		[]string{"git", "worktree", "remove", "--force", worktreePath},
		workspace,
	)
	s.run(ctx, []string{"git", "branch", "-D", branch}, workspace)
}

func (s *Service) captureDiff(
	ctx context.Context,
	worktree string,
	baseSHA string,
	limit int,
) string {
	result := s.run(ctx,
		[]string{"git", "diff", "--no-color", baseSHA, "HEAD"},
		worktree,
	)
	return compact(string(result.Stdout), limit)
}

var testFilePattern = regexp.MustCompile(
	`(?:_test\.go|test_[^/]+\.py|[^/]+_test\.py|\.test\.(?:js|jsx|ts|tsx)|\.spec\.(?:js|jsx|ts|tsx)|_spec\.rb|_test\.rb|Test\.java|Test\.kt)$`,
)

func (s *Service) worktreeAddsTestFile(
	ctx context.Context,
	worktree string,
	baseSHA string,
) bool {
	s.run(ctx, []string{"git", "add", "-A"}, worktree)
	result := s.run(ctx,
		[]string{
			"git", "diff", "--cached", "--name-only", "--diff-filter=A", baseSHA,
		},
		worktree,
	)
	for _, file := range nonemptyTrimmedLines(string(result.Stdout)) {
		if testFilePattern.MatchString(file) {
			return true
		}
	}
	// Deliberate parity divergence from src/session/review-gate.ts:341-353:
	// adding cases to an existing test file is test coverage even though the
	// TypeScript original only recognizes newly added test files.
	numstat := s.run(ctx,
		[]string{"git", "diff", "--cached", "--numstat", baseSHA},
		worktree,
	)
	for _, line := range nonemptyTrimmedLines(string(numstat.Stdout)) {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || !testFilePattern.MatchString(fields[2]) {
			continue
		}
		additions, err := strconv.Atoi(fields[0])
		if err == nil && additions > 0 {
			return true
		}
	}
	return false
}

func (s *Service) autoCommitImpl(
	ctx context.Context,
	worktree string,
	taskID string,
	title string,
) {
	s.run(ctx, []string{"git", "add", "-A"}, worktree)
	s.run(ctx,
		[]string{
			"git", "rm", "--cached", "-r", "--quiet", "--ignore-unmatch",
			".codeaf",
		},
		worktree,
	)
	status := s.run(ctx, []string{"git", "status", "--porcelain"}, worktree)
	if jscompat.Trim(string(status.Stdout)) == "" {
		return
	}
	s.run(ctx,
		attribution.GitArgv(
			"commit", "-m", attribution.AppendCommitTrailer(
				"leaf "+taskID+": "+compact(title, 80),
			),
		),
		worktree,
	)
}

func (s *Service) commitRepair(
	ctx context.Context,
	worktree string,
	taskID string,
	title string,
) {
	s.run(ctx, []string{"git", "add", "-A"}, worktree)
	s.run(ctx,
		[]string{
			"git", "rm", "--cached", "-r", "--quiet", "--ignore-unmatch",
			".codeaf",
		},
		worktree,
	)
	status := s.run(ctx, []string{"git", "status", "--porcelain"}, worktree)
	if jscompat.Trim(string(status.Stdout)) == "" {
		return
	}
	s.run(ctx,
		attribution.GitArgv(
			"commit", "-m", attribution.AppendCommitTrailer(
				"repair "+taskID+": "+compact(title, 80),
			),
		),
		worktree,
	)
}
