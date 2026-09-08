package session

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	aforgeGitName  = "aforge"
	aforgeGitEmail = "aforge@localhost"
)

// aforgeGitIdentity is THE ONE SPELLING of the identity every commit and merge
// the harness writes carries. The moved-tip guard compares against the same
// email, so a writer and the policy that recognizes its work cannot drift.
func aforgeGitIdentity() []string {
	return []string{"-c", "user.name=" + aforgeGitName, "-c", "user.email=" + aforgeGitEmail}
}

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
// Full ref names keep a same-named tag from changing a branch's spelling to
// heads/name, which would disguise protected names from the landing guard.
func currentBranch(root string) string {
	out, err := git(root, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/")
}

// branchCommit reads the world a named branch points at. Empty is ordinary:
// a detached checkout has no name to read, and a failed observation must not
// turn into a landing refusal later.
func branchCommit(root, branch string) string {
	branch = strings.TrimSpace(branch)
	if strings.TrimSpace(root) == "" || branch == "" {
		return ""
	}
	out, err := git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
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
	if root == "" {
		return false
	}
	// A GROUND INSIDE THE ROOT IS STILL THE PERSON'S REPOSITORY. This used to
	// be `root != ground` and nothing more, which answered false for a ground
	// one directory inside the checkout — every protection below skipped, and
	// the landing merged onto whatever branch the person was standing on.
	//
	// Nothing reaches that today: [Agent.ReferPlace] snaps a referred path to
	// the repository root and so does groundRoot on the task ladder, so the two
	// are equal by the time either road builds a tree. But that is an invariant
	// held in other files, and this is the guard whose failure costs somebody
	// their working tree — so it does not rest on somebody else remembering.
	// The exemptions below are unchanged and still decide the borrowed cases.
	if ground != "" && ground != root && !withinDir(root, ground) {
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
	case branchMovedByPerson(t.root, current, t.homeSha):
		return "its branch " + t.branch + " was kept: " + current + " has moved on since the work was cut — merge it where you want it"
	}
	return ""
}

// branchMovedByPerson reports that the named branch no longer points at the
// recorded world and that the movement was not made solely by aforge's own
// landings. A rewrite is always the person's movement; a forward move is theirs
// when any commit in the new range carries a different committer identity.
//
// A failed git read answers false because an observation failure is not grounds
// to keep finished work away from the branch it was meant for. Exit status one
// from merge-base is its documented "not an ancestor" answer rather than a
// failed read, and is the rewrite case this policy must catch.
func branchMovedByPerson(root, branch, recorded string) bool {
	branch, recorded = strings.TrimSpace(branch), strings.TrimSpace(recorded)
	if strings.TrimSpace(root) == "" || branch == "" || recorded == "" {
		return false
	}
	tip := branchCommit(root, branch)
	if tip == "" || tip == recorded {
		return false
	}
	if _, err := git(root, "merge-base", "--is-ancestor", recorded, "refs/heads/"+branch); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) && exited.ExitCode() == 1 {
			return true
		}
		return false
	}
	committers, err := git(root, "log", "--no-show-signature", "--format=%ce", recorded+"..refs/heads/"+branch)
	if err != nil {
		return false
	}
	for _, email := range strings.Fields(committers) {
		if email != aforgeGitEmail {
			return true
		}
	}
	return false
}
