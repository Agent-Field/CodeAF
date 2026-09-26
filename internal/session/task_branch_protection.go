package session

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
)

const (
	codeafGitName  = gitidentity.Name
	codeafGitEmail = gitidentity.Email

	// Legacy identities codeaf's task commits were once authored with. Both stay
	// recognised by taskCommitIdentity so older work still lands as the task
	// system's own.
	legacyCodeafGitName  = "aforge"           // legacy-name
	legacyCodeafGitEmail = "aforge@localhost" // legacy-name
	legacyBotGitName     = "codeaf"           // legacy-identity: the pre-bot address
	legacyBotGitEmail    = "codeaf@localhost" // legacy-identity: the pre-bot address
)

// codeafGitIdentity marks commits the task system creates so sibling landings
// can distinguish its own forward progress from a person's intervening work.
// The author is the bot account — the same identity the Co-Authored-By trailer
// names — and the two older local addresses are recognised as legacy.
func codeafGitIdentity() []string {
	return []string{"-c", "user.name=" + codeafGitName, "-c", "user.email=" + codeafGitEmail}
}

// protectedBranchNames is THE ONE LIST of branch names automatic task landing leaves unchanged.
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
// repositories codeaf owns as working material. It is deliberately not
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
	// repository's `.codeaf/` (task_run.go's [taskOwnFolder]), where no Trees()
	// prefix can name them; a mirror opened there sits on git's default branch,
	// and reading that as the person's trunk would keep every part of the family
	// off the tree its parent is waiting to merge.
	for _, dropping := range taskDroppingNames() {
		if strings.Contains(root, string(filepath.Separator)+dropping+string(filepath.Separator)) {
			return false
		}
	}
	// A LEGACY SESSION HAS NO Place, but its family trees still live below the
	// old .codeaf/tasks path. Those repositories are codeaf's working
	// material too; treating git's default branch there as the person's would
	// keep every part out of its parent and break the family landing.
	for _, dropping := range taskDroppingNames() {
		marker := string(filepath.Separator) + filepath.Join(dropping, "tasks") + string(filepath.Separator)
		if strings.Contains(root+string(filepath.Separator), marker) {
			return false
		}
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
		return "its branch " + t.branch + " was kept: your checkout is not on a branch — inspect the retained task branch without changing this checkout"
	case t.home != "" && current != t.home:
		return "its branch " + t.branch + " was kept: your checkout has moved from " + t.home + " to " + current + " since the work was cut — inspect the retained task branch before choosing a destination"
	case protectedBranch(t.root, current):
		return "its branch " + t.branch + " was kept: your checkout is on " + current + ", which tasks do not merge into automatically"
	case branchMovedByPerson(t.root, current, t.homeSha):
		return "its branch " + t.branch + " was kept: " + current + " has moved on since the work was cut — inspect the retained task branch before choosing a destination"
	}
	return ""
}

// branchMovedByPerson reports that the named branch no longer points at the
// recorded world and that the movement was not made solely by codeaf's own
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
	committers, err := git(root, "log", "--no-show-signature", "--format=%cn%x09%ce", recorded+"..refs/heads/"+branch)
	if err != nil {
		return false
	}
	// AN EMPTY RANGE IS NOT A MOVEMENT. The early returns above mean the range
	// normally holds at least one commit, but a reading that comes back with no
	// rows at all has observed nothing — and the reading-failure case two lines
	// up already rules that "not the person", because an observation failure is
	// no grounds to keep finished work away from the branch it was meant for.
	rows := strings.TrimSpace(committers)
	if rows == "" {
		return false
	}
	for _, line := range strings.Split(rows, "\n") {
		name, email, ok := strings.Cut(line, "\t")
		if !ok || !taskCommitIdentity(name, email) {
			return true
		}
	}
	return false
}

func taskCommitIdentity(name, email string) bool {
	return (name == codeafGitName && email == codeafGitEmail) ||
		(name == legacyCodeafGitName && email == legacyCodeafGitEmail) ||
		(name == legacyBotGitName && email == legacyBotGitEmail)
}
