package session

// task_branch_land.go is the person's own landing of one of the conversation's
// task branches into their checkout, in two steps: a plan that reads and
// binds every fact the merge depends on, and a land that reads them all again
// under the repository's lock and merges only when nothing has moved.
//
// ── WHO MAY ASK ──
//
// Only a branch the conversation's own task records name ([ownedTaskBranches])
// is ever planned, and only into the repository the conversation's recorded
// workspace resolves to. A `task/` ref somebody else made, a path, a root and a
// target are never taken from the caller: the branch name is a key into the
// records and nothing more.
//
// ── WHAT IT WILL NOT DO ──
//
// IT NEVER MERGES INTO A CHECKOUT THAT HAS ANYTHING OF THE PERSON'S IN IT. The
// task runtime's landing carries the person's uncommitted work aside and puts
// it back (groundcarry.go); this one refuses instead, because nobody is
// watching a stash or a `reset --hard` that a button started. So a dirty
// checkout, an ignored file where the branch would write one, and a merge
// git's own merge-tree says would conflict are all refused BEFORE anything is
// written, and a merge that fails anyway is never aborted, reset or replayed:
// the facts as they stand are what the person is told.
//
// IT RUNS NO PROGRAM IT CANNOT NAME. A configured merge driver or filter is a
// program whose identity cannot be bound, so a repository with either is
// refused rather than merged around. The hooks git will run are bound by their
// bytes and shown before the person confirms; a hook that changed since the
// plan makes the land refuse.
//
// IT MERGES THE COMMIT THE PLAN NAMED, not whatever the branch name points at
// by the time git runs, so a branch that moves between the plan and the merge
// cannot land commits nobody confirmed.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// The reasons a land is refused, as the wire spells them.
const (
	LandChanged       = "changed"
	LandMerged        = "merged"
	LandGone          = "gone"
	LandDetached      = "detached"
	LandOperation     = "operation"
	LandDirty         = "dirty"
	LandIgnoredInWay  = "ignored_in_way"
	LandConflict      = "conflict"
	LandHeld          = "held"
	LandTaskCopyDirty = "task_copy_dirty"
	LandCustomMerge   = "custom_merge"
	LandFilter        = "filter"
	LandSparse        = "sparse"
	LandSubmodule     = "submodule"
	LandSigning       = "signing"
	LandFastForward   = "ff_only"
	LandMergeOptions  = "merge_options"
	LandHookUnbounded = "hook_unbounded"
	LandUnrelated     = "unrelated"
	LandOutside       = "outside"
	LandGitOld        = "git_old"
	LandTooLarge      = "too_large"
	LandRefused       = "refused"
)

const (
	// landReadMax bounds what one read of git's answers the plan keeps.
	landReadMax = 1 << 20
	// landPathsShown is the most paths a refusal names.
	landPathsShown = 50
	// landDiffMax is the most files a land will read the branch's change of.
	landDiffMax = 20000
	// landHookMax is the largest hook whose bytes a plan binds.
	landHookMax = 1 << 20
	// landOutputMax is the most of the merge's own output that is kept.
	landOutputMax = 64 << 10
)

// landMergeMax is the merge's own ceiling. It is not the request's: a merge
// killed halfway through writing a checkout is the worst state a person can be
// left in, so the request ending does not stop it — only this does. A variable
// so a test can shorten it.
var landMergeMax = 60 * time.Second

// landLockPoll is how often a land asks for the repository's lock again.
var landLockPoll = 25 * time.Millisecond

// landHooks are the hooks a merge can run, by name: a fast-forward runs the
// last three, a merge commit all six.
var landHooks = []string{"pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-merge", "reference-transaction", "post-index-change"}

// LandHook is one hook git will find when the merge runs, bound by its bytes.
type LandHook struct {
	Name       string
	SHA256     string
	Size       int64
	Executable bool
}

// TaskBranchLandPlan is everything a land is bound to. Revision is the digest
// of all of it; the land refuses when a fresh reading digests differently.
type TaskBranchLandPlan struct {
	Revision    string
	Task        string
	Title       string
	Branch      string
	BranchHead  string
	Base        string
	Target      string
	Head        string
	Root        string
	GitDir      string
	CommonDir   string
	FastForward bool
	// Tree is the tree git's merge-tree says the merge makes.
	Tree         string
	Files        int
	Added        int
	Removed      int
	CountsKnown  bool
	CountsCapped bool
	Hooks        []LandHook
	// config and copies are bound by the revision and never shown.
	config string
	copies string
}

// TaskBranchLanded is a land that happened.
type TaskBranchLanded struct {
	Branch      string
	Target      string
	HeadBefore  string
	HeadAfter   string
	FastForward bool
}

// LandRefusal is a land that did not start, or a merge git refused with
// nothing moved. Paths are the files it is about, at most landPathsShown.
type LandRefusal struct {
	Reason string
	Said   string
	Paths  []string
}

func (r *LandRefusal) Error() string { return r.Said }

func refuseLand(reason, said string, paths ...string) *LandRefusal {
	if len(paths) > landPathsShown {
		paths = paths[:landPathsShown]
	}
	return &LandRefusal{Reason: reason, Said: said, Paths: paths}
}

// LandFacts is what a land that may have moved something found afterwards.
// An empty string or a nil pointer is a fact that could not be read.
type LandFacts struct {
	Head            string
	Target          string
	MergeInProgress *bool
	IndexLocked     *bool
	Clean           *bool
	Conflicted      []string
}

// LandUncertain is a land whose outcome cannot be proved either way. It is
// never retried: the person looks at the checkout first.
type LandUncertain struct {
	Said  string
	Facts LandFacts
}

func (u *LandUncertain) Error() string { return u.Said }

var (
	// ErrLandNotOwned is a branch the conversation's records do not name.
	ErrLandNotOwned = errors.New("that branch is not one of this conversation's task branches")
	// ErrLandBusy is a land that could not have the repository to itself, or
	// git that did not answer in time.
	ErrLandBusy = errors.New("the repository is busy; try again")
	// ErrLandNoRepo is a workspace that is not a checkout.
	ErrLandNoRepo = errors.New("this conversation's folder is not a git checkout")
)

// landCommand is the one door this file's git goes through: no GIT_*
// variable of this process's own reaches it, so the repository is the one the
// directory says and no external diff, index or object store is borrowed. A
// variable so a test can put a slow git behind it.
var landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = landEnv()
	return command
}

func landEnv() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "GPG_TTY=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0", "GPG_TTY=", "LC_ALL=C", "GIT_LITERAL_PATHSPECS=1")
}

// landRead runs one read-only git question under ctx and deliverGitCallMax
// with fsmonitor off, and answers stdout when git exited zero within max.
func landRead(ctx context.Context, dir string, max int, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	command := landCommand(ctx, dir, append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)...)
	command.Stderr = io.Discard
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, -1, err
	}
	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			return nil, -1, ErrLandBusy
		}
		return nil, -1, err
	}
	data, readErr := io.ReadAll(io.LimitReader(out, int64(max)+1))
	_ = out.Close()
	waitErr := command.Wait()
	switch {
	case ctx.Err() != nil:
		return nil, -1, ErrLandBusy
	case len(data) > max:
		return nil, -1, refuseLand(LandTooLarge, "git said more about this repository than a land reads")
	case readErr != nil:
		return nil, -1, readErr
	}
	if code, ran := gitExit(waitErr); ran {
		return data, code, nil
	}
	if waitErr != nil {
		return nil, -1, waitErr
	}
	return data, 0, nil
}

// landLine is a read whose answer is one line that must exit zero.
func landLine(ctx context.Context, dir string, args ...string) (string, error) {
	out, code, err := landRead(ctx, dir, 64<<10, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s exited %d", args[0], code)
	}
	return strings.TrimSpace(string(out)), nil
}

func isOid(s string) bool { return len(s) >= 40 && isHex(s) }

// PlanTaskBranchLand reads and binds what landing branch into the
// conversation's checkout would do. It writes nothing but the objects
// merge-tree adds to the object store, which nothing refers to.
func PlanTaskBranchLand(ctx context.Context, place Place, workspace, branch string) (TaskBranchLandPlan, error) {
	return readLandPlan(ctx, place, workspace, branch)
}

func readLandPlan(ctx context.Context, place Place, workspace, branch string) (TaskBranchLandPlan, error) {
	var plan TaskBranchLandPlan
	if strings.TrimSpace(place.Dir) == "" || !validTaskBranch(branch) {
		return plan, ErrLandNotOwned
	}
	names, err := ownedTaskBranches(ctx, place)
	if errors.Is(err, errTaskBranchTime) {
		return plan, ErrLandBusy
	}
	if err != nil {
		return plan, err
	}
	for _, named := range names {
		if named.branch == branch {
			plan.Branch, plan.Task, plan.Title = named.branch, named.task, named.title
			break
		}
	}
	if plan.Branch == "" {
		return plan, ErrLandNotOwned
	}
	if info, err := os.Stat(workspace); strings.TrimSpace(workspace) == "" || err != nil || !info.IsDir() {
		return plan, ErrLandNoRepo
	}
	if err := landGitNewEnough(ctx, workspace); err != nil {
		return plan, err
	}
	top, err := landLine(ctx, workspace, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return plan, landReadFailed(err, ErrLandNoRepo)
	}
	if climbsOutOfScratch(workspace, top) {
		return plan, refuseLand(LandOutside, "this conversation's folder is inside a scratch directory, not a checkout of its own")
	}
	root := canonicalPath(top)
	plan.Root = root
	if plan.GitDir, err = landLine(ctx, root, "rev-parse", "--absolute-git-dir"); err != nil {
		return plan, landReadFailed(err, ErrLandNoRepo)
	}
	if plan.CommonDir, err = landLine(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir"); err != nil {
		return plan, landReadFailed(err, ErrLandNoRepo)
	}
	plan.GitDir, plan.CommonDir = canonicalPath(plan.GitDir), canonicalPath(plan.CommonDir)

	config, err := landConfig(ctx, root)
	if err != nil {
		return plan, err
	}
	plan.config = config.digest
	if refusal := config.refusal(); refusal != nil {
		return plan, refusal
	}
	if refusal := landOperation(plan.GitDir); refusal != nil {
		return plan, refusal
	}
	if hold, busy := programHoldNear(root, ""); busy {
		return plan, refuseLand(LandHeld, hold.holder+" is working in "+hold.where(root)+"; land this when that run has ended")
	}

	ref, code, err := landRead(ctx, root, 4096, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return plan, landReadFailed(err, ErrTaskBranchGit)
	}
	target := strings.TrimSpace(string(ref))
	if code != 0 || !strings.HasPrefix(target, "refs/heads/") {
		return plan, refuseLand(LandDetached, "your checkout is not on a branch, so there is nowhere to land this")
	}
	plan.Target = strings.TrimPrefix(target, "refs/heads/")
	if refusal := config.targetRefusal(plan.Target); refusal != nil {
		return plan, refusal
	}
	if plan.Head, err = landCommit(ctx, root, "HEAD"); err != nil || plan.Head == "" {
		return plan, refuseLand(LandDetached, "your branch "+plan.Target+" has no commit yet")
	}
	if plan.BranchHead, err = landCommit(ctx, root, "refs/heads/"+plan.Branch); err != nil {
		return plan, landReadFailed(err, ErrTaskBranchGit)
	}
	if plan.BranchHead == "" {
		return plan, refuseLand(LandGone, "the branch "+plan.Branch+" is not in this repository any more")
	}
	if ancestor, err := landAncestor(ctx, root, plan.BranchHead, plan.Head); err != nil {
		return plan, landReadFailed(err, ErrTaskBranchGit)
	} else if ancestor {
		return plan, refuseLand(LandMerged, plan.Branch+" is already in "+plan.Target)
	}
	base, code, err := landRead(ctx, root, 256, "merge-base", plan.Head, plan.BranchHead)
	if err != nil {
		return plan, landReadFailed(err, ErrTaskBranchGit)
	}
	if plan.Base = strings.TrimSpace(string(base)); code != 0 || !isOid(plan.Base) {
		return plan, refuseLand(LandUnrelated, plan.Branch+" shares no history with "+plan.Target)
	}
	ff, err := landAncestor(ctx, root, plan.Head, plan.BranchHead)
	if err != nil {
		return plan, landReadFailed(err, ErrTaskBranchGit)
	}
	plan.FastForward = ff && config.get("merge.ff") != "false"
	if !plan.FastForward && config.get("merge.ff") == "only" {
		return plan, refuseLand(LandFastForward, "this repository only takes fast-forward merges, and "+plan.Branch+" is not one")
	}
	if !plan.FastForward && config.on("commit.gpgsign") {
		return plan, refuseLand(LandSigning, "this repository signs its commits, and a land cannot ask for your key")
	}

	if paths, err := landStatus(ctx, root); err != nil {
		return plan, err
	} else if len(paths) > 0 {
		return plan, refuseLand(LandDirty, "your checkout has changes of its own; commit or put them away before landing", paths...)
	}
	if plan.copies, err = landTaskCopies(ctx, root, plan.Branch, plan.BranchHead); err != nil {
		return plan, err
	}
	if err := landBranchChange(ctx, root, &plan); err != nil {
		return plan, err
	}
	if plan.Tree, err = landPreflight(ctx, root, plan.Head, plan.BranchHead); err != nil {
		return plan, err
	}
	if plan.FastForward {
		if plan.Tree, err = landLine(ctx, root, "rev-parse", "--verify", "--quiet", plan.BranchHead+"^{tree}"); err != nil {
			return plan, landReadFailed(err, ErrTaskBranchGit)
		}
	}
	files, added, removed, capped, err := countBranchDiff(ctx, root, plan.Base, plan.BranchHead)
	if err == nil {
		plan.Files, plan.Added, plan.Removed, plan.CountsCapped, plan.CountsKnown = files, added, removed, capped, true
	}
	if plan.Hooks, err = landHookFacts(ctx, root); err != nil {
		return plan, err
	}
	plan.Revision = plan.digest()
	return plan, nil
}

// landReadFailed turns a read's failure into the answer a land gives: time
// running out is busy, a refusal stays itself, and anything else is fallback.
func landReadFailed(err, fallback error) error {
	var refusal *LandRefusal
	switch {
	case errors.Is(err, ErrLandBusy), errors.As(err, &refusal):
		return err
	case err == nil:
		return fallback
	}
	return fmt.Errorf("%w: %v", fallback, err)
}

// landGitNewEnough refuses a git with no `merge-tree --write-tree` (2.38):
// without it there is no way to know a merge conflicts without making it.
func landGitNewEnough(ctx context.Context, dir string) error {
	out, err := landLine(ctx, dir, "version")
	if err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	fields := strings.Fields(out)
	if len(fields) >= 3 {
		parts := strings.SplitN(fields[2], ".", 3)
		if len(parts) >= 2 {
			major, _ := strconv.Atoi(parts[0])
			minor, _ := strconv.Atoi(parts[1])
			if major > 2 || major == 2 && minor >= 38 {
				return nil
			}
		}
	}
	return refuseLand(LandGitOld, "this git cannot check a merge for conflicts without making it; git 2.38 or newer can")
}

func landCommit(ctx context.Context, root, name string) (string, error) {
	out, code, err := landRead(ctx, root, 256, "rev-parse", "--verify", "--quiet", name+"^{commit}")
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(out))
	if code != 0 {
		return "", nil
	}
	if !isOid(oid) {
		return "", fmt.Errorf("%s answered something that is not a commit id", name)
	}
	return oid, nil
}

func landAncestor(ctx context.Context, root, older, newer string) (bool, error) {
	_, code, err := landRead(ctx, root, 64, "merge-base", "--is-ancestor", older, newer)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == 1:
		return false, nil
	}
	return false, fmt.Errorf("merge-base --is-ancestor exited %d", code)
}

// landConfigFacts is the repository's whole configuration as git reads it,
// keyed in git's own lower-case spelling, and its digest.
type landConfigFacts struct {
	values map[string][]string
	digest string
}

func landConfig(ctx context.Context, root string) (landConfigFacts, error) {
	out, code, err := landRead(ctx, root, landReadMax, "config", "-z", "--list")
	if err != nil {
		return landConfigFacts{}, landReadFailed(err, ErrTaskBranchGit)
	}
	if code != 0 {
		return landConfigFacts{}, fmt.Errorf("%w: git config exited %d", ErrTaskBranchGit, code)
	}
	sum := sha256.Sum256(out)
	facts := landConfigFacts{values: map[string][]string{}, digest: hex.EncodeToString(sum[:])}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		key, value, _ := strings.Cut(string(record), "\n")
		facts.values[key] = append(facts.values[key], value)
	}
	return facts, nil
}

func (c landConfigFacts) get(key string) string {
	values := c.values[key]
	if len(values) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(values[len(values)-1]))
}

func (c landConfigFacts) on(key string) bool {
	switch c.get(key) {
	case "true", "yes", "on", "1":
		return true
	}
	return len(c.values[key]) > 0 && c.get(key) == ""
}

// refusal is the configuration a land will not merge under: a merge driver or
// a filter (a program whose identity cannot be bound) and a sparse checkout.
func (c landConfigFacts) refusal() *LandRefusal {
	keys := make([]string, 0, len(c.values))
	for key := range c.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		section, rest, ok := strings.Cut(key, ".")
		if !ok {
			continue
		}
		dot := strings.LastIndex(rest, ".")
		if dot < 0 {
			continue
		}
		variable := rest[dot+1:]
		switch {
		case section == "merge" && variable == "driver":
			return refuseLand(LandCustomMerge, "this repository names its own merge program, and a land runs only git's built-in merges")
		case section == "filter" && (variable == "clean" || variable == "smudge" || variable == "process"):
			return refuseLand(LandFilter, "this repository runs a filter program on its files, and a land cannot vouch for it")
		}
	}
	if c.on("core.sparsecheckout") {
		return refuseLand(LandSparse, "this checkout is sparse, and a land only merges into a whole one")
	}
	return nil
}

func (c landConfigFacts) targetRefusal(target string) *LandRefusal {
	for key := range c.values {
		if strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".mergeoptions") &&
			strings.EqualFold(key, "branch."+target+".mergeoptions") {
			return refuseLand(LandMergeOptions, "your branch "+target+" sets its own merge options, and a land merges only the way it says it will")
		}
	}
	return nil
}

// landOperation refuses a checkout git is in the middle of something in, by
// the files git itself made, looked at and never opened.
func landOperation(gitDir string) *LandRefusal {
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "REBASE_HEAD", "rebase-merge", "rebase-apply", "BISECT_LOG", "sequencer"} {
		if _, err := os.Lstat(filepath.Join(gitDir, name)); err == nil {
			return refuseLand(LandOperation, "git is in the middle of something in your checkout; finish it before landing")
		}
	}
	if _, err := os.Lstat(filepath.Join(gitDir, "index.lock")); err == nil {
		return refuseLand(LandOperation, "another git is writing your checkout's index; try again when it has finished")
	}
	return nil
}

// landStatus answers every path git reports as not clean, untracked and in
// submodules included. An empty answer is a proof of clean.
func landStatus(ctx context.Context, root string) ([]string, error) {
	out, code, err := landRead(ctx, root, landReadMax, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	var refusal *LandRefusal
	if errors.As(err, &refusal) && refusal.Reason == LandTooLarge {
		return []string{"(more changes than a land reads)"}, nil
	}
	if err != nil {
		return nil, landReadFailed(err, ErrTaskBranchGit)
	}
	if code != 0 {
		return nil, fmt.Errorf("%w: git status exited %d", ErrTaskBranchGit, code)
	}
	return statusPaths(out), nil
}

// statusPaths reads the paths out of porcelain v2 -z records; a rename's
// original path is the record after it and is skipped.
func statusPaths(out []byte) []string {
	var paths []string
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		if record == "" {
			continue
		}
		fields := 0
		switch record[0] {
		case '1':
			fields = 8
		case '2':
			fields = 9
			i++ // the original path
		case 'u':
			fields = 10
		case '?', '!':
			fields = 1
		default:
			continue
		}
		parts := strings.SplitN(record, " ", fields+1)
		if len(parts) == fields+1 {
			paths = append(paths, parts[fields])
		}
	}
	return paths
}

// landTaskCopies binds every worktree the branch is checked out in, and
// refuses one with anything uncommitted: what is not on the branch would not
// land, and a copy still being written is a task still working.
func landTaskCopies(ctx context.Context, root, branch, branchHead string) (string, error) {
	out, code, err := landRead(ctx, root, landReadMax, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", landReadFailed(err, ErrTaskBranchGit)
	}
	if code != 0 {
		return "", fmt.Errorf("%w: git worktree list exited %d", ErrTaskBranchGit, code)
	}
	var bound []string
	path, head := "", ""
	for _, line := range strings.Split(string(out), "\x00") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path, head = strings.TrimPrefix(line, "worktree "), ""
		case strings.HasPrefix(line, "HEAD "):
			head = strings.TrimPrefix(line, "HEAD ")
		case line == "branch refs/heads/"+branch:
			if head != branchHead {
				return "", refuseLand(LandTaskCopyDirty, "the task's own copy of "+branch+" is not where the branch is")
			}
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				bound = append(bound, path+"\x00missing")
				continue
			}
			paths, err := landStatus(ctx, path)
			if err != nil {
				return "", err
			}
			if len(paths) > 0 {
				return "", refuseLand(LandTaskCopyDirty, "the task's own copy of "+branch+" has work that is not on the branch", paths...)
			}
			bound = append(bound, path+"\x00clean")
		}
	}
	return strings.Join(bound, "\x01"), nil
}

// landBranchChange reads what the branch changes from its base: a submodule
// it moves is refused, and a path it adds where anything already stands in
// the checkout — an ignored file, a directory — is refused, because the merge
// would write over or beside something that is the person's.
func landBranchChange(ctx context.Context, root string, plan *TaskBranchLandPlan) error {
	out, code, err := landRead(ctx, root, 8<<20, "diff", "--no-ext-diff", "--no-textconv", "--raw", "-z", "--no-renames", "--no-abbrev", plan.Base, plan.BranchHead, "--")
	if err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	if code != 0 {
		return fmt.Errorf("%w: git diff exited %d", ErrTaskBranchGit, code)
	}
	records := bytes.Split(out, []byte{0})
	var inWay []string
	seen := 0
	for i := 0; i+1 < len(records); i += 2 {
		meta, path := string(records[i]), string(records[i+1])
		if meta == "" {
			break
		}
		seen++
		if seen > landDiffMax {
			return refuseLand(LandTooLarge, plan.Branch+" changes more files than a land reads")
		}
		fields := strings.Fields(strings.TrimPrefix(meta, ":"))
		if len(fields) < 5 {
			return fmt.Errorf("%w: git diff answered a record it does not write", ErrTaskBranchGit)
		}
		if fields[0] == "160000" || fields[1] == "160000" {
			return refuseLand(LandSubmodule, plan.Branch+" moves a submodule, and a land does not update submodules", path)
		}
		if fields[4] != "A" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); err == nil || !os.IsNotExist(err) {
			inWay = append(inWay, path)
		}
	}
	if len(inWay) > landDiffMax/10 {
		return refuseLand(LandTooLarge, plan.Branch+" adds more files where yours stand than a land reads")
	}
	// A path the checkout already TRACKS is the merge's to reconcile (the
	// preflight has its say); only what git does not track is the person's.
	if len(inWay) > 0 {
		out, code, err := landRead(ctx, root, landReadMax, append([]string{"ls-files", "-z", "--"}, inWay...)...)
		if err != nil {
			return landReadFailed(err, ErrTaskBranchGit)
		}
		if code != 0 {
			return fmt.Errorf("%w: git ls-files exited %d", ErrTaskBranchGit, code)
		}
		tracked := map[string]bool{}
		for _, path := range strings.Split(string(out), "\x00") {
			tracked[path] = true
		}
		kept := inWay[:0]
		for _, path := range inWay {
			if !tracked[path] {
				kept = append(kept, path)
			}
		}
		inWay = kept
	}
	if len(inWay) > 0 {
		return refuseLand(LandIgnoredInWay, "files of yours stand where "+plan.Branch+" would write new ones", inWay...)
	}
	return nil
}

// landPreflight is git's own answer to whether the merge conflicts, made
// without touching the checkout or the index: the tree the merge makes, or
// the conflicted paths.
func landPreflight(ctx context.Context, root, head, branchHead string) (string, error) {
	out, code, err := landRead(ctx, root, landReadMax, "merge-tree", "--write-tree", "--name-only", "-z", "--no-messages", head, branchHead)
	if err != nil {
		return "", landReadFailed(err, ErrTaskBranchGit)
	}
	records := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	tree := records[0]
	switch {
	case code == 0 && len(records) >= 1 && isOid(tree):
		return tree, nil
	case code == 1 && isOid(tree):
		var paths []string
		for _, path := range records[1:] {
			if path != "" && (len(paths) == 0 || paths[len(paths)-1] != path) {
				paths = append(paths, path)
			}
		}
		return "", refuseLand(LandConflict, "this branch conflicts with your checkout; nothing was merged", paths...)
	}
	return "", fmt.Errorf("%w: git merge-tree exited %d", ErrTaskBranchGit, code)
}

// landHookFacts binds every hook a merge can run by its bytes, found where
// git itself looks (core.hooksPath honoured).
func landHookFacts(ctx context.Context, root string) ([]LandHook, error) {
	dir, err := landLine(ctx, root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return nil, landReadFailed(err, ErrTaskBranchGit)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	var hooks []LandHook
	for _, name := range landHooks {
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		}
		hook, err := bindHook(path, name)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}

func bindHook(path, name string) (LandHook, error) {
	unbounded := refuseLand(LandHookUnbounded, "the "+name+" hook in this repository cannot be read whole, so a land cannot say what it runs", name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > landHookMax {
		return LandHook{}, unbounded
	}
	file, err := os.Open(path)
	if err != nil {
		return LandHook{}, unbounded
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, landHookMax+1))
	if err != nil || len(data) > landHookMax {
		return LandHook{}, unbounded
	}
	sum := sha256.Sum256(data)
	return LandHook{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Executable: info.Mode()&0o111 != 0}, nil
}

// digest is the revision: every bound fact, each length-prefixed so no two
// different plans spell the same bytes.
func (p TaskBranchLandPlan) digest() string {
	sum := sha256.New()
	put := func(s string) { fmt.Fprintf(sum, "%d:%s;", len(s), s) }
	for _, s := range []string{"land/1", p.Task, p.Branch, p.BranchHead, p.Base, p.Target, p.Head, p.Root, p.GitDir, p.CommonDir,
		strconv.FormatBool(p.FastForward), p.Tree, p.config, p.copies} {
		put(s)
	}
	for _, hook := range p.Hooks {
		put(hook.Name)
		put(hook.SHA256)
		put(strconv.FormatInt(hook.Size, 10))
		put(strconv.FormatBool(hook.Executable))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// tryLockGitRoot is [lockGitRoot] for a person's land: it refuses rather than
// going ahead unserialised. The lock file must be the engine's own, so a place
// with no folder (whose lock would sit somewhere else) is refused too.
func tryLockGitRoot(ctx context.Context, place Place, root string) (func(), error) {
	if strings.TrimSpace(place.Dir) == "" {
		return nil, ErrLandBusy
	}
	for !gitRoot.TryLock() {
		if !landWait(ctx) {
			return nil, ErrLandBusy
		}
	}
	file := openGitRootLock(place, root)
	if file == nil {
		gitRoot.Unlock()
		return nil, ErrLandBusy
	}
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			break
		}
		if !isLockHeld(err) || !landWait(ctx) {
			_ = file.Close()
			gitRoot.Unlock()
			return nil, ErrLandBusy
		}
	}
	return func() {
		_ = filelock.Unlock(file)
		_ = file.Close()
		gitRoot.Unlock()
	}, nil
}

func landWait(ctx context.Context) bool {
	timer := time.NewTimer(landLockPoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// LandTaskBranch lands a plan the person confirmed. Under the repository's
// lock it reads every fact again and refuses LandChanged when the revision
// differs; then it merges the plan's commit, and reads the outcome back.
//
// A MERGE THAT DID NOT GO CLEANLY IS LEFT EXACTLY AS GIT LEFT IT. Nothing is
// aborted, reset or tried again; what can be proved is said, and what cannot
// is [LandUncertain] with every fact that could be read.
func LandTaskBranch(ctx context.Context, place Place, workspace string, plan TaskBranchLandPlan) (TaskBranchLanded, error) {
	if strings.TrimSpace(plan.Root) == "" || !isOid(plan.Head) || !isOid(plan.BranchHead) {
		return TaskBranchLanded{}, refuseLand(LandChanged, "this plan is not one a land can confirm; plan it again")
	}
	release, err := tryLockGitRoot(ctx, place, plan.Root)
	if err != nil {
		return TaskBranchLanded{}, err
	}
	defer release()
	fresh, err := readLandPlan(ctx, place, workspace, plan.Branch)
	if err != nil {
		var refusal *LandRefusal
		if errors.As(err, &refusal) || errors.Is(err, ErrLandNotOwned) {
			return TaskBranchLanded{}, refuseLand(LandChanged, "something changed since this land was planned: "+err.Error())
		}
		return TaskBranchLanded{}, err
	}
	if fresh.Revision != plan.Revision {
		return TaskBranchLanded{}, refuseLand(LandChanged, "something changed since this land was planned; plan it again")
	}
	return landMerge(fresh)
}

// landMerge runs the one merge, on its own ceiling, and reads what it did.
func landMerge(plan TaskBranchLandPlan) (TaskBranchLanded, error) {
	ff := "--no-ff"
	if plan.FastForward {
		ff = "--ff"
	}
	args := append([]string{"-c", "core.fsmonitor=false"}, codeafGitIdentity()...)
	args = append(args, "merge", "--no-edit", "--no-autostash", "--no-overwrite-ignore", "-s", "ort", "--commit", "--no-squash",
		"--no-verify-signatures", "--no-rerere-autoupdate", "--no-log", "--no-stat", "--no-progress", ff,
		"-m", "Merge branch '"+plan.Branch+"'", plan.BranchHead)
	ctx, cancel := context.WithTimeout(context.Background(), landMergeMax)
	defer cancel()
	command := landCommand(ctx, plan.Root, args...)
	output := &cappedText{max: landOutputMax}
	command.Stdout, command.Stderr = output, output
	// THE MERGE AND EVERY HOOK IT STARTS ARE ONE GROUP, so the ceiling kills a
	// hook's background child too, and WaitDelay stops a grandchild holding the
	// output open from holding the land open with it.
	processgroup.Configure(command)
	command.Cancel = func() error { return processgroup.Kill(command.Process.Pid) }
	command.WaitDelay = 2 * time.Second
	runErr := command.Run()
	timedOut := ctx.Err() != nil

	facts := readLandFacts(plan)
	landed := runErr == nil && !timedOut && landedFacts(plan, facts)
	if landed {
		return TaskBranchLanded{Branch: plan.Branch, Target: plan.Target, HeadBefore: plan.Head, HeadAfter: facts.Head, FastForward: plan.FastForward}, nil
	}
	if !timedOut && runErr != nil && nothingMoved(plan, facts.LandFacts) {
		said := strings.TrimSpace(string(output.data))
		if line, _, ok := strings.Cut(said, "\n"); ok {
			said = line
		}
		return TaskBranchLanded{}, refuseLand(LandRefused, "git would not merge "+plan.Branch+"; nothing was changed: "+said)
	}
	said := "the land of " + plan.Branch + " may not have finished; look at your checkout before doing anything else"
	if facts.MergeInProgress != nil && *facts.MergeInProgress {
		said = "the land of " + plan.Branch + " stopped partway; your checkout is in the middle of a merge, which `git merge --abort` undoes"
	}
	return TaskBranchLanded{}, &LandUncertain{Said: said, Facts: facts.LandFacts}
}

// readFacts is what the checkout looks like after a merge, each fact read on
// its own clock; a fact that cannot be read is left unknown.
type landAfter struct {
	LandFacts
	tree    string
	parents []string
}

func readLandFacts(plan TaskBranchLandPlan) landAfter {
	ctx := context.Background()
	var facts landAfter
	if head, err := landCommit(ctx, plan.Root, "HEAD"); err == nil {
		facts.Head = head
	}
	if ref, code, err := landRead(ctx, plan.Root, 4096, "symbolic-ref", "--quiet", "HEAD"); err == nil && code == 0 {
		facts.Target = strings.TrimPrefix(strings.TrimSpace(string(ref)), "refs/heads/")
	}
	exists := func(name string) *bool {
		_, err := os.Lstat(filepath.Join(plan.GitDir, name))
		if err != nil && !os.IsNotExist(err) {
			return nil
		}
		there := err == nil
		return &there
	}
	facts.MergeInProgress, facts.IndexLocked = exists("MERGE_HEAD"), exists("index.lock")
	if paths, err := landStatus(ctx, plan.Root); err == nil {
		clean := len(paths) == 0
		facts.Clean = &clean
	}
	if facts.MergeInProgress != nil && *facts.MergeInProgress {
		if out, code, err := landRead(ctx, plan.Root, landReadMax, "diff", "--no-ext-diff", "--name-only", "--diff-filter=U", "-z"); err == nil && code == 0 {
			for _, path := range strings.Split(string(out), "\x00") {
				if path != "" && len(facts.Conflicted) < landPathsShown {
					facts.Conflicted = append(facts.Conflicted, path)
				}
			}
		}
	}
	if facts.Head != "" {
		if tree, err := landLine(ctx, plan.Root, "rev-parse", "--verify", "--quiet", facts.Head+"^{tree}"); err == nil {
			facts.tree = tree
		}
		if out, err := landLine(ctx, plan.Root, "rev-list", "--parents", "-n", "1", facts.Head); err == nil {
			if fields := strings.Fields(out); len(fields) > 1 {
				facts.parents = fields[1:]
			}
		}
	}
	return facts
}

// landedFacts is the proof of a land: the checkout is still on the target, it
// holds the tree merge-tree predicted, and HEAD is the branch's commit (a
// fast-forward) or a merge of exactly the two commits the plan named.
func landedFacts(plan TaskBranchLandPlan, facts landAfter) bool {
	if facts.Target != plan.Target || facts.tree != plan.Tree || !isFalse(facts.MergeInProgress) || !isFalse(facts.IndexLocked) {
		return false
	}
	if plan.FastForward {
		return facts.Head == plan.BranchHead
	}
	return len(facts.parents) == 2 && facts.parents[0] == plan.Head && facts.parents[1] == plan.BranchHead
}

// nothingMoved is the proof that a refused merge changed nothing.
func nothingMoved(plan TaskBranchLandPlan, facts LandFacts) bool {
	return facts.Head == plan.Head && facts.Target == plan.Target && isFalse(facts.MergeInProgress) &&
		isFalse(facts.IndexLocked) && facts.Clean != nil && *facts.Clean
}

func isFalse(b *bool) bool { return b != nil && !*b }
