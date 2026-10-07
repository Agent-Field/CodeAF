package secaf

// What changed, for an audit of the changes.
//
// sec-af had a PR mode that never ran: it read the diff from the process's own
// folder rather than the repository under audit, and on the path every audit
// took it was handed nothing. This is that mode made to work in a person's
// folder: the changes are the branch's commits since it left its base AND the
// edits not yet committed AND the files not yet tracked — the work as it
// stands — and the files near them (sec-af's own "blast radius": the files
// that name a changed file's module) come along, because a change is unsafe
// where its callers meet it.
//
// IT READS GIT AND WRITES NOTHING. Every command here is a read; the audit
// promised the folder back as it found it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/secaf/diffanalysis"
	"github.com/Agent-Field/codeaf/internal/secaf/focus"
)

// Changes is the work an audit of the changes looks at.
type Changes struct {
	// Base is the commit the changes are measured from, and BaseName how the
	// person would say it (`origin/dev`, `main`, the ref they typed).
	Base, BaseName string
	// Changed is every scannable file that differs from the base; Nearby is
	// the files that name one of them; Relevant is both, sorted.
	Changed, Nearby, Relevant []string
	// Diff is the change itself, cut at [diffBytes].
	Diff string
}

const (
	// gitWall bounds one git command.
	gitWall = 60 * time.Second
	// diffBytes is how much of the diff the hunters are shown. The files are
	// theirs to read whole; the diff only says where to look.
	diffBytes = 48 << 10
	// nearbyFiles bounds the files near the change, so a change to a module
	// every file imports does not turn an audit of the changes into an audit
	// of everything.
	nearbyFiles = 120
)

// errNoChanges is an audit of the changes with nothing changed.
var errNoChanges = errors.New("nothing has changed")

// ReadChanges reads what changed in repo since base, or since the branch's own
// base when base is empty.
func ReadChanges(ctx context.Context, repo, base string) (Changes, error) {
	if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return Changes{}, errors.New("an audit of the changes needs a git repository with at least one commit; audit the whole repository instead")
	}
	changes := Changes{}
	if strings.TrimSpace(base) != "" {
		commit, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", base+"^{commit}")
		if err != nil {
			return Changes{}, fmt.Errorf("%s names no commit in this repository", base)
		}
		changes.Base, changes.BaseName = strings.TrimSpace(commit), base
	} else {
		changes.Base, changes.BaseName = findBase(ctx, repo)
	}
	tracked, err := git(ctx, repo, "diff", "--name-only", "--diff-filter=ACMR", changes.Base)
	if err != nil {
		return Changes{}, fmt.Errorf("read the changes since %s: %w", changes.BaseName, err)
	}
	untracked, _ := git(ctx, repo, "ls-files", "--others", "--exclude-standard")
	seen := map[string]bool{}
	for _, file := range append(lines(tracked), lines(untracked)...) {
		if file != "" && !seen[file] && diffanalysis.IsScannable(file) {
			seen[file] = true
			changes.Changed = append(changes.Changed, file)
		}
	}
	sort.Strings(changes.Changed)
	if len(changes.Changed) == 0 {
		return changes, fmt.Errorf("%w since %s in a file this audit reads (code, not docs or config)", errNoChanges, changes.BaseName)
	}
	changes.Nearby = nearby(ctx, repo, changes.Changed, seen)
	changes.Relevant = append(append([]string(nil), changes.Changed...), changes.Nearby...)
	sort.Strings(changes.Relevant)
	diff, _ := git(ctx, repo, append([]string{"diff", changes.Base, "--"}, changes.Changed...)...)
	if len(diff) > diffBytes {
		diff = diff[:diffBytes] + "\n[the diff goes on; read the changed files themselves for the rest]\n"
	}
	changes.Diff = diff
	return changes, nil
}

// findBase is where this branch left its trunk: the merge base of HEAD with
// the remote's default branch, else with a local trunk by its usual names, and
// HEAD itself when the branch is the trunk — whose changes are then the edits
// not yet committed.
func findBase(ctx context.Context, repo string) (string, string) {
	var candidates []string
	if ref, err := git(ctx, repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append(candidates, strings.TrimSpace(ref))
	}
	candidates = append(candidates, "origin/main", "origin/master", "origin/dev", "main", "master", "dev", "trunk")
	head, _ := git(ctx, repo, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err != nil {
			continue
		}
		base, err := git(ctx, repo, "merge-base", "HEAD", candidate)
		if err != nil || strings.TrimSpace(base) == "" {
			continue
		}
		return strings.TrimSpace(base), candidate
	}
	return head, "the last commit"
}

// nearby is the files that name a changed file's module, sec-af's own reading
// of a change's reach (diffanalysis.FileToModule), searched in the working
// tree so files not yet committed count.
func nearby(ctx context.Context, repo string, changed []string, skip map[string]bool) []string {
	found := map[string]bool{}
	for _, file := range changed {
		module := diffanalysis.FileToModule(file)
		if module == "" {
			continue
		}
		out, err := git(ctx, repo, "grep", "-l", "--untracked", "--fixed-strings", "-e", module,
			"--", "*.py", "*.ts", "*.js", "*.go", "*.java", "*.rb", "*.php", "*.cs", "*.kt", "*.rs")
		if err != nil {
			continue
		}
		for _, hit := range lines(out) {
			if hit != "" && !skip[hit] && diffanalysis.IsScannable(hit) {
				found[hit] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for file := range found {
		out = append(out, file)
	}
	sort.Strings(out)
	if len(out) > nearbyFiles {
		out = out[:nearbyFiles]
	}
	return out
}

// git runs one read-only git command in repo. A command that ran and said no
// (a grep with no match, a ref that is not there) is an error with its exit.
func git(ctx context.Context, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWall)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo, "--no-optional-locks", "-c", "core.quotePath=false"}, args...)...)
	command.Env = append(command.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return stdout.String(), fmt.Errorf("%v: %s", err, message)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// withFocus is ctx carrying the change to the hunters (internal/secaf/focus).
func withFocus(ctx context.Context, changes Changes) context.Context {
	return focus.With(ctx, focus.Focus{Base: changes.BaseName, Changed: changes.Changed, Nearby: changes.Nearby, Diff: changes.Diff})
}
