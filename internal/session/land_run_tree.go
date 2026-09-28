package session

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
)

// LandRunTree commits a run's own working copy onto the branch it is checked
// out on, and it is the landing half of the run engine ([internal/run]) stated
// here, where the commit road lives.
//
// THE RUN'S WORK IS WHAT ITS TREE SAYS. Every worker of a run edits through
// bash ([NewBeltWorker]), and a shell worker fills no write ledger, so the
// working copy's status and commits since base record what the run made — the same
// road a bash-belt node's landing takes ([commitTaskWork] on its belt arm),
// reached by name rather than from inside a task tree.
//
// IT ANSWERS FOUR THINGS AND ONLY THE FIRST THREE ARE THE LANDING. The branch
// is the one the copy stands on once the commit has landed. The paths are the
// net change since base, including a branch the worker merged in; when the net
// tree is unchanged but commits moved HEAD, they are the paths those commits
// touched. The refusal is the
// sentence a person reads when the landing had
// nothing to do or the tree would not take the work, and it is empty on every
// other road. The error is non-nil when the working copy or its history cannot
// be read or landed, rather than for an ordinary refusal to bring work home.
//
// THE SWITCH IS THE BELT'S, read here for the reason [NewBeltWorker] reads it:
// the belt road stages the tree's own status minus what the harness itself
// writes, which is not what the default landing does, so a landing that ran
// with CODEAF_TASK_BELT naming the node belt would change a landing for one nobody
// composed. With the flag off this refuses and touches nothing.
//
// ATTRIBUTION IS ON UNLESS CONTRIBUTING FORBIDS IT. The same two trailer
// lines are added to worker commits and the landing commit. model is the one
// the `Assisted-by` line names; empty leaves it bare because this door does
// not know which model each worker used.
func LandRunTree(dir, base, title, model string) (branch string, changed []string, refusal string, err error) {
	if !bashBeltAsked() {
		return "", nil, "", errors.New("the bash belt is off: CODEAF_TASK_BELT names the node belt")
	}
	sign := gitSignature{named: model != "", model: model}
	// A plumbing failure leaves the original branch intact. The work must still
	// come home, so report the signing problem and continue the landing.
	if _, err := signRunCommits(dir, base, sign); err != nil {
		log.Printf("codeaf: could not sign the run's commits: %v", err)
	}
	saved, problem, why := commitTaskWork(dir, title, nil, sign, true)
	if problem != "" {
		if why == refusedByTheTree {
			// The place itself would not take the write, and asking again would
			// get the same answer: that is the fault arm, never a landing.
			return "", nil, "", errors.New(problem)
		}
		return "", nil, problem, nil
	}
	headMoved := false
	if base != "" {
		out, err := git(dir, "diff", "--name-only", "-z", base, "HEAD")
		if err != nil {
			return "", nil, "", err
		}
		changed = append(changed, gitNULPaths(out)...)
		head := runTreeHead(dir)
		if head == "" {
			return "", nil, "", errors.New("read the run's branch head after landing")
		}
		headMoved = head != base
		if headMoved && len(changed) == 0 {
			// The net path list measures this tree since base, not the
			// rewrite's private range; include an outside merged branch too.
			changed, err = runTouchedPaths(dir, base, head)
			if err != nil {
				return "", nil, "", err
			}
		}
	} else {
		changed = saved
	}
	if len(changed) == 0 && !headMoved {
		return "", nil, runNothingToLand, nil
	}
	if branch = currentBranch(dir); branch == "" {
		return "", nil, "", errors.New("the run's working copy stands on no branch, so there is nothing to land")
	}
	return branch, changed, "", nil
}

// runTouchedPaths names files in non-merge commits when committed work later
// cancels itself out. It is sorted and unique, while an empty-commit run still
// lands on its branch with a count of zero files.
func runTouchedPaths(dir, base, head string) ([]string, error) {
	rangeArg := head
	if base != "" {
		rangeArg = base + ".." + head
	}
	out, err := git(dir, "rev-list", "--no-merges", rangeArg)
	if err != nil {
		return nil, fmt.Errorf("list run's touched commits: %w: %s", err, strings.TrimSpace(out))
	}
	seen := make(map[string]bool)
	for _, sha := range strings.Fields(out) {
		if !runObjectID.MatchString(sha) {
			return nil, fmt.Errorf("list run's touched commits: invalid object id %q", sha)
		}
		paths, err := git(dir, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "-z", sha)
		if err != nil {
			return nil, fmt.Errorf("read run commit paths %s: %w", sha, err)
		}
		for _, path := range gitNULPaths(paths) {
			seen[path] = true
		}
	}
	changed := make([]string, 0, len(seen))
	for path := range seen {
		changed = append(changed, path)
	}
	sort.Strings(changed)
	return changed, nil
}

// runNothingToLand is the refusal a run's landing answers when the working copy
// holds no change the run made: the branch would carry what it always carried.
const runNothingToLand = "nothing to land: the run's working copy holds no change"
