package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── WHERE AN ITEM'S WORK LIVES: A WORKTREE AND A BRANCH PER ITEM ───────────
//
// The hand run of the first item left its fix uncommitted on the person's
// own `main`, with no branch, and four benches shared that one checkout, so
// two items running at once would have written over each other. So an item
// never works in the person's checkout. It works in a git worktree of its
// own, `<store>/work/<repo>-<number>`, on a branch of its own,
// `factory/<number>-<slug>`, made from the checkout the first time a round
// needs a folder and reused by every round after.
//
// THE PERSON'S CHECKOUT IS NEVER TOUCHED. `git worktree add` writes only the
// new folder and the repository's shared refs; whatever is uncommitted in the
// checkout stays exactly where it was, and the item starts from a commit, not
// from that mess. THE WORKTREE AND THE BRANCH OUTLIVE THE ITEM: a stop, a
// ship and a send back all keep them, and nothing deletes either yet.

// Git runs one git command in dir and answers what it printed. The real one
// is [ExecGit]; a test may hand in its own.
type Git interface {
	Run(dir string, args ...string) (string, error)
}

// ExecGit is [Git] over the git binary on the PATH. NO SHELL: every argument
// reaches git as itself, so a branch or a folder name can never be read as a
// command. Git is told never to prompt, because nobody is at a keyboard to
// answer a password question, and a prompt would hold the round forever.
type ExecGit struct{}

// Run runs `git -C dir args...`. A failure's error is gits last line.
func (ExecGit) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if line := lastLine(text); line != "" {
			return text, errors.New(line)
		}
		return text, err
	}
	return text, nil
}

// lastLine is the last line of s that says something.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// Workdirs makes and finds the items' worktrees under one root.
type Workdirs struct {
	root string
	git  Git
	// Log, when set, is told the line `branch: <name>` once, when an item's
	// worktree is made: that line is how the post stage's `pr` knows which
	// branch to push. The wire sets it to [StoreLog].
	Log func(it factory.Item, line string)

	// mu holds one `git worktree add` at a time. Two benches making their
	// worktrees from one checkout at the same moment would race on the
	// repository's own lock files, and one of them would fail for nothing.
	mu sync.Mutex
}

// NewWorkdirs makes the items' worktrees under root, which the wire sets to
// `<store dir>/work` (`~/.codeaf/v3/factory/work`).
func NewWorkdirs(root string, git Git) *Workdirs {
	if git == nil {
		git = ExecGit{}
	}
	return &Workdirs{root: root, git: git}
}

// Dir is where its worktree lives: `<root>/<repo-short>-<number>`.
func (w *Workdirs) Dir(it factory.Item) string {
	repo := strings.TrimSpace(it.Repo)
	repo = repo[strings.LastIndex(repo, "/")+1:]
	if repo == "" {
		repo = "item"
	}
	return filepath.Join(w.root, repo+"-"+itemDigits(it))
}

// Branch is the branch its work goes on: `factory/<number>-<slug>`, the slug
// from the title, at most forty characters.
func (w *Workdirs) Branch(it factory.Item) string { return itemBranch(it) }

// For answers its worktree, making it from checkout the first time. A
// worktree git already lists at that folder is used as it is, on whatever
// branch it has; a branch that exists without a worktree is checked out into
// a new one; otherwise the branch is made from the default branch's head.
func (w *Workdirs) For(it factory.Item, checkout string) (string, error) {
	checkout = strings.TrimSpace(checkout)
	if checkout == "" {
		return "", fmt.Errorf("codeaf does not know where %s is checked out", strings.TrimSpace(it.Repo))
	}
	dir := w.Dir(it)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.listed(checkout, dir) {
		if isPull(it) {
			w.toHead(it, checkout, dir)
		}
		return dir, nil
	}
	fail := func(err error) (string, error) {
		return "", fmt.Errorf("could not make a worktree for %s: %s", it.Ref(), lastLine(err.Error()))
	}
	if err := os.MkdirAll(w.root, 0o700); err != nil {
		return fail(err)
	}
	// A worktree whose folder was deleted by hand still holds its branch in
	// gits books until it is pruned, and the add below would be refused.
	_, _ = w.git.Run(checkout, "worktree", "prune")
	branch := w.Branch(it)
	start := w.base(checkout)
	if isPull(it) {
		head, ok, err := w.fetchPull(it, checkout)
		if err != nil {
			return "", fmt.Errorf("could not fetch the head of %s: %s", it.Ref(), lastLine(err.Error()))
		}
		if ok {
			start = head
		}
	}
	var err error
	existing := false
	if _, verr := w.git.Run(checkout, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); verr == nil {
		existing = true
		_, err = w.git.Run(checkout, "worktree", "add", dir, branch)
	} else {
		_, err = w.git.Run(checkout, "worktree", "add", "--no-track", "-b", branch, dir, start)
	}
	if err != nil {
		return fail(err)
	}
	if w.Log != nil {
		w.Log(it, "branch: "+branch)
	}
	if existing && isPull(it) {
		w.toHead(it, checkout, dir)
	}
	return dir, nil
}

// ── A PULL REQUEST IS REVIEWED AT ITS OWN HEAD ─────────────────────────────
//
// THE STEP MUST NEVER NEED THE NETWORK OR GIT PLUMBING TO SEE WHAT IT IS
// REVIEWING. On the 2026-10-09 run of a pull request the read step's
// worktree was cut from the default branch, so the change it was asked to
// read was not in it; its `git remote` and `git worktree` were refused (an
// unattended run reads no other branch's books) and it fell back to fetching
// the pull request's diff page off the web. So a pull request's worktree is
// cut at the pull request's head: `refs/pull/<n>/head`, which the base
// repository serves for a fork's pull request too, kept at
// `refs/factory/pull/<n>`, with the base branch fetched beside it so
// `git diff origin/<base>...HEAD` is the change.

// isPull says whether it is a pull request with a number to fetch.
func isPull(it factory.Item) bool { return it.Kind == factory.KindPR && it.Num > 0 }

// pullRef is where a pull request's head is kept in the checkout's refs.
func pullRef(it factory.Item) string { return "refs/factory/pull/" + strconv.Itoa(it.Num) }

// PullBase is the ref a pull request's change is read against in its
// worktree: `origin/<base>` when the item knows its base branch, else
// `origin/HEAD`, the remote's default branch.
func PullBase(it factory.Item) string {
	if b := strings.TrimSpace(it.Base); b != "" {
		return "origin/" + b
	}
	return "origin/HEAD"
}

// fetchPull fetches the pull request's head into [pullRef], and its base
// branch beside it when the item names one, and answers the ref. ok is false
// with no error when the checkout has no `origin`, a repository made on this
// machine whose pull requests have nowhere to be fetched from: its worktree
// is then cut the way an issue's is.
func (w *Workdirs) fetchPull(it factory.Item, checkout string) (string, bool, error) {
	if _, err := w.git.Run(checkout, "remote", "get-url", "origin"); err != nil {
		return "", false, nil
	}
	ref := pullRef(it)
	specs := []string{"+refs/pull/" + strconv.Itoa(it.Num) + "/head:" + ref}
	if b := strings.TrimSpace(it.Base); b != "" && !strings.ContainsAny(b, " :~^?*[\\") {
		specs = append(specs, "+refs/heads/"+b+":refs/remotes/origin/"+b)
	}
	if _, err := w.git.Run(checkout, append([]string{"fetch", "--no-tags", "--quiet", "origin"}, specs...)...); err != nil {
		return "", false, err
	}
	return ref, true, nil
}

// toHead brings a pull request's existing worktree to its head, when it has
// not been fetched yet or the head moved since, and when that loses nothing:
// the worktree has no commit of its own (every commit on it is on the head or
// on a remote branch) and nothing uncommitted. A worktree a step already wrote
// into stays where it is. It is best effort: a fetch that fails leaves the
// worktree as it was, and the round reads what is there.
func (w *Workdirs) toHead(it factory.Item, checkout, dir string) {
	ref := pullRef(it)
	have, err := w.git.Run(checkout, "rev-parse", "--verify", "--quiet", ref)
	have = strings.TrimSpace(have)
	if err == nil && (it.HeadSHA == "" || strings.EqualFold(have, it.HeadSHA)) {
		if at, herr := w.git.Run(dir, "rev-parse", "HEAD"); herr == nil && strings.TrimSpace(at) == have {
			return
		}
	} else {
		if _, ok, ferr := w.fetchPull(it, checkout); ferr != nil || !ok {
			return
		}
	}
	if own, err := w.git.Run(dir, "rev-list", "--count", "HEAD", "--not", ref, "--remotes=origin"); err != nil || strings.TrimSpace(own) != "0" {
		return
	}
	if dirty, err := w.git.Run(dir, "status", "--porcelain"); err != nil || strings.TrimSpace(dirty) != "" {
		return
	}
	if _, err := w.git.Run(dir, "reset", "--keep", ref); err != nil {
		return
	}
	if w.Log != nil {
		short, _ := w.git.Run(dir, "rev-parse", "--short", "HEAD")
		w.Log(it, "at the head of "+it.Ref()+": "+strings.TrimSpace(short))
	}
}

// Remove takes its worktree away and KEEPS ITS BRANCH, so the work is still
// in the repository. NOTHING CALLS IT YET: a stop, a ship and a send back all
// keep the worktree, and the manual says nothing deletes one.
func (w *Workdirs) Remove(it factory.Item, checkout string) error {
	checkout = strings.TrimSpace(checkout)
	if checkout == "" {
		return fmt.Errorf("codeaf does not know where %s is checked out", strings.TrimSpace(it.Repo))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.git.Run(checkout, "worktree", "remove", "--force", w.Dir(it)); err != nil {
		return fmt.Errorf("could not remove the worktree for %s: %s", it.Ref(), lastLine(err.Error()))
	}
	return nil
}

// listed says whether git already has a worktree at dir. Both sides go
// through EvalSymlinks, because git prints the resolved path and a temp
// folder is often reached through a link.
func (w *Workdirs) listed(checkout, dir string) bool {
	out, err := w.git.Run(checkout, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	want := samePath(dir)
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok && samePath(p) == want {
			return true
		}
	}
	return false
}

func samePath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// base is the commit a new item branch starts from: the checkout's
// `origin/HEAD` target when git knows it (the remote's default branch, so the
// person's own unpushed commits never ride along into a pull request), else
// the branch the checkout is on, else its HEAD.
func (w *Workdirs) base(checkout string) string {
	if ref, err := w.git.Run(checkout, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		if ref = strings.TrimSpace(ref); ref != "" {
			return ref
		}
	}
	if cur, err := w.git.Run(checkout, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if cur = strings.TrimSpace(cur); cur != "" {
			return cur
		}
	}
	return "HEAD"
}

// jobDir is the folder a round of it runs in: [Options.Workdir]'s when the
// runner has one, else [Options.RepoDir]'s. loop.go's job() calls it in place
// of reading RepoDir itself, and keeps the error on the Job for [runIn].
func (lp *floorLoop) jobDir(it factory.Item) (string, error) {
	if wd := lp.r.opts.Workdir; wd != nil {
		return wd(it)
	}
	if lp.r.opts.RepoDir != nil {
		return lp.r.opts.RepoDir(it.Repo), nil
	}
	return "", nil
}

// runIn runs one round of job, unless its folder could not be made: then the
// round fails with that sentence and the executor is never called, because a
// stage run anywhere else would be run in the person's checkout.
func runIn(ctx context.Context, exec Executor, job Job) (factory.StageResult, error) {
	if job.dirErr != nil {
		return factory.StageResult{}, job.dirErr
	}
	return exec.Run(ctx, job)
}

// ErrNoRemote is a push from a repository with no `origin`.
var ErrNoRemote = errors.New("no remote to push to")

// GitPush is the post stage's push over git: `git push -u origin <branch>`
// from dir, after checking there is an `origin` at all. NEVER A FORCE-PUSH:
// the refspec is the bare branch, so a remote branch that moved refuses the
// push and the person decides.
func GitPush(git Git) func(dir, branch string) error {
	if git == nil {
		git = ExecGit{}
	}
	return func(dir, branch string) error {
		if _, err := git.Run(dir, "remote", "get-url", "origin"); err != nil {
			return ErrNoRemote
		}
		_, err := git.Run(dir, "push", "-u", "origin", branch)
		return err
	}
}

// StoreLog is [Workdirs.Log] over st: the line is appended to the item's
// stream the way the loop appends its own, through Annotate, so the line
// does not make the row read as just changed.
func StoreLog(st *store.Store, clock func() time.Time) func(it factory.Item, line string) {
	if clock == nil {
		clock = time.Now
	}
	return func(it factory.Item, line string) {
		if st == nil || it.ID <= 0 {
			return
		}
		_ = st.Annotate(it.ID, func(x *factory.Item) error {
			if x.Stream == nil {
				x.Stream = &factory.Stream{}
			}
			loopSay(x, clock(), "thought", line)
			return nil
		})
	}
}

// itemDigits is the item's number as a folder and a branch spell it: the
// digits of its ref, or for an item with no number (a CI run) its ref and its
// id, so two of those never share a worktree.
func itemDigits(it factory.Item) string {
	ref := it.Ref()
	if d, ok := strings.CutPrefix(ref, "#"); ok {
		return d
	}
	return fmt.Sprintf("%s-%d", ref, it.ID)
}

// itemBranch is `factory/<number>-<slug>`, the slug the title lowercased
// with every run of other characters one dash, at most forty characters.
func itemBranch(it factory.Item) string {
	var slug []rune
	dash := false
	for _, r := range strings.ToLower(it.Title) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			slug = append(slug, r)
			dash = false
		} else if !dash && len(slug) > 0 {
			slug = append(slug, '-')
			dash = true
		}
		if len(slug) >= 40 {
			break
		}
	}
	s := strings.Trim(string(slug), "-")
	if s == "" {
		return "factory/" + itemDigits(it)
	}
	return "factory/" + itemDigits(it) + "-" + s
}
