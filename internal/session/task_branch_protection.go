package session

import (
	"path/filepath"
	"strings"
)

// protectedBranchNames is THE ONE LIST of branch names aforge never writes to.
// The manual names every entry and a structural test holds that page against
// this value, so changing the policy cannot leave the person reading an older
// list.
var protectedBranchNames = [...]string{
	"main",
	"master",
	"dev",
	"develop",
	"development",
	"staging",
	"stage",
	"trunk",
	"production",
	"prod",
	"release",
}

// currentBranch reads the branch checked out at a repository's root. Detached
// HEAD is the empty string by design: it is not a destination a landing can
// safely move, and git's quiet symbolic-ref is the direct reading of that fact.
func currentBranch(root string) string {
	out, err := git(root, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// protectedBranch answers both halves of the policy: the fixed names above and
// whatever branch a remote says is its default. Remote defaults matter even
// when a team calls theirs something this build has never heard of.
func protectedBranch(root, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, protected := range protectedBranchNames {
		if strings.EqualFold(name, protected) {
			return true
		}
	}
	remotes, err := git(root, "remote")
	if err != nil {
		return false
	}
	for _, remote := range strings.Fields(remotes) {
		ref, err := git(root, "symbolic-ref", "-q", "--short", "refs/remotes/"+remote+"/HEAD")
		if err != nil {
			continue
		}
		branch := strings.TrimPrefix(strings.TrimSpace(ref), remote+"/")
		if strings.EqualFold(name, branch) {
			return true
		}
	}
	return false
}

// landsInThePersonsRepository distinguishes the person's own checkout from
// repositories aforge owns as working material. It is deliberately not
// [standingInOwnSpace]: that question is gated on an owned conversation, while
// a referred repository is borrowed and still must be protected.
func (t taskTree) landsInThePersonsRepository() bool {
	root, ground := canonicalPath(strings.TrimSpace(t.root)), canonicalPath(strings.TrimSpace(t.ground))
	if root == "" || root != ground {
		return false
	}
	if trees := canonicalPath(strings.TrimSpace(t.place.Trees())); trees != "" && withinDir(trees, root) {
		return false
	}
	// AND THE LEGACY LAYOUT'S TASK FOLDERS ARE THE HARNESS'S TOO. A session with
	// no folder of its own puts a family's mirror and a part's worktree under the
	// repository's `.aforge-v3/` (task_run.go's [taskOwnFolder]), where no Trees()
	// prefix can name them; a mirror opened there sits on git's default branch,
	// and reading that as the person's trunk would keep every part of the family
	// off the tree its parent is waiting to merge.
	if strings.Contains(root, string(filepath.Separator)+aforgeDroppings+string(filepath.Separator)) {
		return false
	}
	// A LEGACY SESSION HAS NO Place, but its family trees still live below the
	// old .aforge-v3/tasks path. Those repositories are aforge's working
	// material too; treating git's default branch there as the person's would
	// keep every part out of its parent and break the family landing.
	marker := string(filepath.Separator) + filepath.FromSlash(tasksDirName) + string(filepath.Separator)
	if strings.Contains(root+string(filepath.Separator), marker) {
		return false
	}
	if t.place.Owned {
		for _, own := range []string{t.place.Workspace, t.place.Work()} {
			if own = canonicalPath(strings.TrimSpace(own)); own != "" && root == own {
				return false
			}
		}
	}
	return true
}

// keptLandingSentence is the one sentence a completed branch landing owes
// when the checkout is not a safe destination. It only reads the checkout; the
// caller has already put the task branch in the root repository before asking.
func (t taskTree) keptLandingSentence() string {
	current := currentBranch(t.root)
	switch {
	case current == "":
		return "its branch " + t.branch + " was kept: your checkout is not on a branch — check one out and merge it"
	case t.home != "" && current != t.home:
		return "its branch " + t.branch + " was kept: your checkout has moved from " + t.home + " to " + current + " since the work was cut — merge it where you want it"
	case protectedBranch(t.root, current):
		return "its branch " + t.branch + " was kept: your checkout is on " + current + ", which aforge never writes to — merge it when you are ready"
	}
	return ""
}
