package run

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// changeBase fixes the comparison before any round can commit its edits.
// A PR includes its existing change; other jobs start at their first HEAD.
func changeBase(dir string, it factory.Item) string {
	if it.ChangeBase != "" {
		return it.ChangeBase
	}
	git := ExecGit{}
	if isPull(it) {
		if base, err := git.Run(dir, "merge-base", "HEAD", PullBase(it)); err == nil {
			return base
		}
	}
	base, _ := git.Run(dir, "rev-parse", "HEAD")
	return base
}

// readChange includes committed, staged, unstaged and untracked work. Git's
// NUL-separated output keeps tabs and newlines in a filename unambiguous.
func readChange(dir, base string) (*factory.Change, error) {
	if dir == "" || base == "" {
		return nil, fmt.Errorf("change worktree or base is unknown")
	}
	git := changeGit{}
	branch, err := git.Run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		branch = ""
	}
	stats, err := git.Run(dir, "diff", "--no-ext-diff", "--no-renames", "--numstat", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	statuses, err := git.Run(dir, "diff", "--no-ext-diff", "--no-renames", "--name-status", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	files := map[string]factory.ChangedFile{}
	parts := strings.Split(statuses, "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		status := map[string]string{"A": "added", "D": "removed", "M": "modified", "T": "modified"}[parts[i]]
		if status == "" {
			return nil, fmt.Errorf("cannot read file status %q", parts[i])
		}
		path := parts[i+1]
		files[path] = factory.ChangedFile{Path: path, Status: status}
	}
	for _, row := range strings.Split(stats, "\x00") {
		cols := strings.SplitN(row, "\t", 3)
		if len(cols) != 3 {
			continue
		}
		f := files[cols[2]]
		f.Added, _ = strconv.Atoi(cols[0])
		f.Removed, _ = strconv.Atoi(cols[1])
		files[f.Path] = f
	}
	untracked, err := git.Run(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path == "" {
			continue
		}
		f := factory.ChangedFile{Path: path, Status: "added"}
		full := filepath.Join(dir, path)
		info, err := os.Lstat(full)
		if err != nil {
			return nil, err
		}
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return nil, err
			}
			content = []byte(target)
		} else {
			content, err = os.ReadFile(full)
			if err != nil {
				return nil, err
			}
		}
		if len(content) > 0 && !bytes.ContainsRune(content, 0) {
			f.Added = bytes.Count(content, []byte{'\n'})
			if content[len(content)-1] != '\n' {
				f.Added++
			}
		}
		files[path] = f
	}
	change := &factory.Change{Branch: strings.TrimSpace(branch)}
	if patch, e := git.Run(dir, "diff", "--no-ext-diff", "--no-renames", "--no-color", base, "--"); e == nil {
		if len(patch) > 512000 {
			patch = patch[:512000]
			change.PatchTruncated = true
		}
		change.Patch = strings.ToValidUTF8(patch, "")
	}
	if len(files) > 0 {
		change.Stat = &factory.ChangeStat{Files: len(files)}
		for _, f := range files {
			change.Files = append(change.Files, f)
			change.Stat.Added += f.Added
			change.Stat.Removed += f.Removed
		}
		sort.Slice(change.Files, func(i, j int) bool { return change.Files[i].Path < change.Files[j].Path })
	}
	return change, nil
}

// recordChange runs on the holding machine after every completed round,
// including a failed check. A failed read retains the last readable snapshot.
func (lp *floorLoop) recordChange(c *loopCtl, job Job, res factory.StageResult) {
	head, headErr := (ExecGit{}).Run(job.Dir, "rev-parse", "HEAD")
	change, err := readChange(job.Dir, job.Item.ChangeBase)
	if err != nil {
		change = nil
	}
	_ = lp.write(c, func(it *factory.Item) error {
		if headErr == nil && head != "" {
			it.HeadCommit = strings.TrimSpace(head)
		}
		var tests []factory.StepTests
		if it.Change != nil {
			copy := *it.Change
			it.Change = &copy
			tests = append(tests, it.Change.Tests...)
		}
		if kindOf(job.Stage) == factory.StageCheck {
			// A later count-free run replaces earlier counts for this step.
			for i := 0; i < len(tests); i++ {
				if tests[i].Step == job.Stage.Name {
					tests = append(tests[:i], tests[i+1:]...)
					break
				}
			}
			if res.Tests != nil {
				tests = append(tests, factory.StepTests{Step: job.Stage.Name, TestCounts: *res.Tests})
			}
		}
		if change != nil {
			it.Change = change
		}
		if it.Change == nil && len(tests) > 0 {
			it.Change = &factory.Change{}
		}
		if it.Change != nil {
			it.Change.Tests = tests
		}
		return nil
	})
}

// validateChange keeps malformed machine reports out of the saved job.
func validateChange(change *factory.Change) error {
	stat := factory.ChangeStat{Files: len(change.Files)}
	seen := map[string]bool{}
	for _, f := range change.Files {
		if f.Path == "" || seen[f.Path] || f.Added < 0 || f.Removed < 0 {
			return fmt.Errorf("change files require unique paths and nonnegative counts")
		}
		seen[f.Path] = true
		switch f.Status {
		case "added", "modified", "removed":
		default:
			return fmt.Errorf("unknown change file status")
		}
		stat.Added += f.Added
		stat.Removed += f.Removed
	}
	if change.Stat != nil && *change.Stat != stat {
		return fmt.Errorf("change stat does not match files")
	}
	seen = map[string]bool{}
	for _, check := range change.Tests {
		if check.Step == "" || seen[check.Step] || check.Passed < 0 || check.Failed < 0 || check.Skipped < 0 || check.Passed+check.Failed+check.Skipped == 0 {
			return fmt.Errorf("change tests require unique steps and positive outcome counts")
		}
		seen[check.Step] = true
	}
	return nil
}

// changeGit keeps raw output because trimming can change a filename that
// starts with whitespace. Git is never allowed to prompt on this path.
type changeGit struct{}

func (changeGit) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	raw, err := cmd.Output()
	return string(raw), err
}
