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
	"bufio"
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
	// settings is the configuration the digest in config was taken of.
	settings landConfigFacts
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

// landQuiet is put in front of every git a land runs. fsmonitor would start
// the repository's own watcher, and automatic maintenance would start `gc
// --auto` after a merge: a pre-auto-gc hook the plan never named, and a gc that
// detaches into a session of its own where no group kill reaches it. The
// settings ride GIT_CONFIG_PARAMETERS into every git those gits start.
var landQuiet = []string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}

// landCapture keeps what git writes up to max, and past it stops the git: the
// write fails and the read's own context is cancelled.
type landCapture struct {
	data   []byte
	max    int
	over   bool
	cancel context.CancelFunc
}

var errLandOver = errors.New("git said more than a land reads")

func (c *landCapture) Write(p []byte) (int, error) {
	if len(c.data)+len(p) > c.max {
		c.over = true
		c.cancel()
		return 0, errLandOver
	}
	c.data = append(c.data, p...)
	return len(p), nil
}

// landRead runs one read-only git question under ctx and deliverGitCallMax
// and answers stdout and git's exit code.
//
// IT IS BOUNDED BY MORE THAN ITS DEADLINE. A status that looks into
// submodules starts gits of its own, and one of them holding the output open
// would hold the read open with it. So git and everything it starts are one
// process group, killed together when the time runs out; WaitDelay ends the
// wait on any pipe a survivor still holds; and stderr is not a pipe at all.
func landRead(ctx context.Context, dir string, max int, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	command := landCommand(ctx, dir, append(append([]string{"--no-optional-locks"}, landQuiet...), args...)...)
	capture := &landCapture{max: max, cancel: cancel}
	command.Stdout, command.Stderr = capture, nil
	processgroup.Configure(command)
	command.Cancel = func() error { return processgroup.Kill(command.Process.Pid) }
	command.WaitDelay = time.Second
	err := command.Run()
	code, ran := gitExit(err)
	switch {
	case capture.over:
		return nil, -1, refuseLand(LandTooLarge, "git said more about this repository than a land reads")
	case ctx.Err() != nil, errors.Is(err, exec.ErrWaitDelay):
		return nil, -1, ErrLandBusy
	case ran:
		return capture.data, code, nil
	case err != nil:
		return nil, -1, err
	}
	return capture.data, 0, nil
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
	for _, phase := range []func(context.Context, *TaskBranchLandPlan) error{
		func(ctx context.Context, plan *TaskBranchLandPlan) error { return landOwner(ctx, place, branch, plan) },
		func(ctx context.Context, plan *TaskBranchLandPlan) error { return landRepository(ctx, workspace, plan) },
		landState,
		landTarget,
		landHistory,
		landWork,
	} {
		if err := phase(ctx, &plan); err != nil {
			return plan, err
		}
	}
	plan.Revision = plan.digest()
	return plan, nil
}

// landOwner finds the branch in the conversation's own task records, which
// is the only authority a land has over it.
func landOwner(ctx context.Context, place Place, branch string, plan *TaskBranchLandPlan) error {
	if strings.TrimSpace(place.Dir) == "" || !validTaskBranch(branch) {
		return ErrLandNotOwned
	}
	names, err := ownedTaskBranches(ctx, place)
	if errors.Is(err, errTaskBranchTime) {
		return ErrLandBusy
	}
	if err != nil {
		return err
	}
	for _, named := range names {
		if named.branch == branch {
			plan.Branch, plan.Task, plan.Title = named.branch, named.task, named.title
			return nil
		}
	}
	return ErrLandNotOwned
}

// landRepository resolves the repository the conversation's folder is in,
// through the one asker of that question ([repositoryRoot]), and binds it by
// its git directories too, so another clone or worktree at the same path is a
// different repository.
func landRepository(ctx context.Context, workspace string, plan *TaskBranchLandPlan) error {
	if info, err := os.Stat(workspace); strings.TrimSpace(workspace) == "" || err != nil || !info.IsDir() {
		return ErrLandNoRepo
	}
	// A BOUNDED QUESTION FIRST: a git that does not answer, or no checkout at
	// all, is found here under the request's clock.
	if err := landGitNewEnough(ctx, workspace); err != nil {
		return err
	}
	if inside, err := landLine(ctx, workspace, "rev-parse", "--is-inside-work-tree"); err != nil || inside != "true" {
		return landReadFailed(err, ErrLandNoRepo)
	}
	root, err := landRoot(ctx, workspace)
	if err != nil {
		return err
	}
	plan.Root = root
	if plan.GitDir, err = landLine(ctx, root, "rev-parse", "--absolute-git-dir"); err != nil {
		return landReadFailed(err, ErrLandNoRepo)
	}
	if plan.CommonDir, err = landLine(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir"); err != nil {
		return landReadFailed(err, ErrLandNoRepo)
	}
	plan.GitDir, plan.CommonDir = canonicalPath(plan.GitDir), canonicalPath(plan.CommonDir)
	return nil
}

// landRoot is [repositoryRoot] asked through [landLine]: under the request's
// clock, one process group killed whole when it runs out, and with no GIT_DIR
// or GIT_WORK_TREE of this process's own to name some other repository than
// the conversation's folder. Nothing outlives the call.
func landRoot(ctx context.Context, workspace string) (string, error) {
	var asked error
	root, ok := repositoryRoot(workspace, func(dir string, args ...string) (string, error) {
		out, err := landLine(ctx, dir, args...)
		asked = err
		return out, err
	})
	switch {
	case errors.Is(asked, ErrLandBusy):
		return "", ErrLandBusy
	case !ok:
		return "", refuseLand(LandOutside, "this conversation's folder is not a checkout of its own")
	}
	return root, nil
}

// landState refuses configuration and states a land will not merge under.
func landState(ctx context.Context, plan *TaskBranchLandPlan) error {
	config, err := landConfig(ctx, plan.Root)
	if err != nil {
		return err
	}
	plan.config, plan.settings = config.digest, config
	if refusal := config.refusal(); refusal != nil {
		return refusal
	}
	if refusal := landOperation(plan.GitDir); refusal != nil {
		return refusal
	}
	if hold, busy := programHoldNear(plan.Root, ""); busy {
		return refuseLand(LandHeld, hold.holder+" is working in "+hold.where(plan.Root)+"; land this when that run has ended")
	}
	return nil
}

// landTarget binds the branch the checkout is on, its commit and the task
// branch's commit.
func landTarget(ctx context.Context, plan *TaskBranchLandPlan) error {
	ref, code, err := landRead(ctx, plan.Root, 4096, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	target := strings.TrimSpace(string(ref))
	if code != 0 || !strings.HasPrefix(target, "refs/heads/") {
		return refuseLand(LandDetached, "your checkout is not on a branch, so there is nowhere to land this")
	}
	plan.Target = strings.TrimPrefix(target, "refs/heads/")
	if plan.Head, err = landCommit(ctx, plan.Root, "HEAD"); err != nil || plan.Head == "" {
		return refuseLand(LandDetached, "your branch "+plan.Target+" has no commit yet")
	}
	if plan.BranchHead, err = landCommit(ctx, plan.Root, "refs/heads/"+plan.Branch); err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	if plan.BranchHead == "" {
		return refuseLand(LandGone, "the branch "+plan.Branch+" is not in this repository any more")
	}
	return nil
}

// landHistory binds how the two commits meet: already in, unrelated, a
// fast-forward or a merge commit, and what this repository says about each.
func landHistory(ctx context.Context, plan *TaskBranchLandPlan) error {
	config := plan.settings
	if refusal := config.targetRefusal(plan.Target); refusal != nil {
		return refusal
	}
	if ancestor, err := landAncestor(ctx, plan.Root, plan.BranchHead, plan.Head); err != nil || ancestor {
		if err != nil {
			return landReadFailed(err, ErrTaskBranchGit)
		}
		return refuseLand(LandMerged, plan.Branch+" is already in "+plan.Target)
	}
	base, code, err := landRead(ctx, plan.Root, 256, "merge-base", plan.Head, plan.BranchHead)
	if err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	if plan.Base = strings.TrimSpace(string(base)); code != 0 || !isOid(plan.Base) {
		return refuseLand(LandUnrelated, plan.Branch+" shares no history with "+plan.Target)
	}
	ff, err := landAncestor(ctx, plan.Root, plan.Head, plan.BranchHead)
	if err != nil {
		return landReadFailed(err, ErrTaskBranchGit)
	}
	plan.FastForward = ff && config.get("merge.ff") != "false"
	if !plan.FastForward && config.get("merge.ff") == "only" {
		return refuseLand(LandFastForward, "this repository only takes fast-forward merges, and "+plan.Branch+" is not one")
	}
	if !plan.FastForward && config.on("commit.gpgsign") {
		return refuseLand(LandSigning, "this repository signs its commits, and a land cannot ask for your key")
	}
	return nil
}

// landWork binds what the merge would touch: a clean checkout, the task's
// own copies, the branch's change, merge-tree's tree, the counts and hooks.
func landWork(ctx context.Context, plan *TaskBranchLandPlan) error {
	paths, err := landStatus(ctx, plan.Root)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		return refuseLand(LandDirty, "your checkout has changes of its own; commit or put them away before landing", paths...)
	}
	if plan.copies, err = landTaskCopies(ctx, plan.Root, plan.Branch, plan.BranchHead); err != nil {
		return err
	}
	if err := landBranchChange(ctx, plan.Root, plan); err != nil {
		return err
	}
	if plan.Tree, err = landPreflight(ctx, plan.Root, plan.Head, plan.BranchHead); err != nil {
		return err
	}
	landCounts(ctx, plan)
	plan.Hooks, err = landHookFacts(ctx, plan.Root)
	return err
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
	added, err := landAddedPaths(out, plan.Branch)
	if err != nil {
		return err
	}
	var inWay []string
	for _, path := range added {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); err == nil || !os.IsNotExist(err) {
			inWay = append(inWay, path)
		}
	}
	if len(inWay) > landDiffMax/10 {
		return refuseLand(LandTooLarge, plan.Branch+" adds more files where yours stand than a land reads")
	}
	if inWay, err = landUntracked(ctx, root, inWay); err != nil {
		return err
	}
	if len(inWay) > 0 {
		return refuseLand(LandIgnoredInWay, "files of yours stand where "+plan.Branch+" would write new ones", inWay...)
	}
	return nil
}

// landAddedPaths reads `diff --raw -z` records and answers the paths the
// branch adds, refusing a submodule it moves and a change too large to read.
func landAddedPaths(out []byte, branch string) ([]string, error) {
	records := bytes.Split(out, []byte{0})
	var added []string
	for i := 0; i+1 < len(records) && len(records[i]) > 0; i += 2 {
		if i/2 >= landDiffMax {
			return nil, refuseLand(LandTooLarge, branch+" changes more files than a land reads")
		}
		fields, path := strings.Fields(strings.TrimPrefix(string(records[i]), ":")), string(records[i+1])
		if len(fields) < 5 {
			return nil, fmt.Errorf("%w: git diff answered a record it does not write", ErrTaskBranchGit)
		}
		if fields[0] == "160000" || fields[1] == "160000" {
			return nil, refuseLand(LandSubmodule, branch+" moves a submodule, and a land does not update submodules", path)
		}
		if fields[4] == "A" {
			added = append(added, path)
		}
	}
	return added, nil
}

// landUntracked keeps the paths git does not track. A path the checkout
// already TRACKS is the merge's to reconcile (the preflight has its say); only
// what git does not track is the person's.
func landUntracked(ctx context.Context, root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out, code, err := landRead(ctx, root, landReadMax, append([]string{"ls-files", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, landReadFailed(err, ErrTaskBranchGit)
	}
	if code != 0 {
		return nil, fmt.Errorf("%w: git ls-files exited %d", ErrTaskBranchGit, code)
	}
	tracked := map[string]bool{}
	for _, path := range strings.Split(string(out), "\x00") {
		tracked[path] = true
	}
	var kept []string
	for _, path := range paths {
		if !tracked[path] {
			kept = append(kept, path)
		}
	}
	return kept, nil
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

// landCounts is the branch's numstat from its base, read through the land's
// own reader so it is of this repository and no other. Counts git could not
// give, or gave more of than is read, are left unknown, never zero.
func landCounts(ctx context.Context, plan *TaskBranchLandPlan) {
	out, code, err := landRead(ctx, plan.Root, taskBranchNumstatBytesMax, "diff", "--no-ext-diff", "--no-textconv", "--numstat", "-z", "--no-renames", plan.Base, plan.BranchHead, "--")
	if err != nil || code != 0 {
		return
	}
	tally, err := tallyNumstat(bufio.NewReader(bytes.NewReader(out)))
	if err == nil {
		plan.Files, plan.Added, plan.Removed, plan.CountsCapped, plan.CountsKnown = tally.files, tally.added, tally.removed, tally.capped, true
	}
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

// tryLockGitRoot is [lockGitRoot] for a person's land: it answers nil rather
// than going ahead unserialised. The lock file must be the engine's own, so a place
// with no folder (whose lock would sit somewhere else) is refused too.
func tryLockGitRoot(ctx context.Context, place Place, root string) func() {
	if strings.TrimSpace(place.Dir) == "" {
		return nil
	}
	for !gitRoot.TryLock() {
		if !landWait(ctx) {
			return nil
		}
	}
	file := openGitRootLock(place, root)
	if file == nil {
		gitRoot.Unlock()
		return nil
	}
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			break
		}
		if !isLockHeld(err) || !landWait(ctx) {
			_ = file.Close()
			gitRoot.Unlock()
			return nil
		}
	}
	return func() {
		_ = filelock.Unlock(file)
		_ = file.Close()
		gitRoot.Unlock()
	}
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
	release := tryLockGitRoot(ctx, place, plan.Root)
	if release == nil {
		return TaskBranchLanded{}, ErrLandBusy
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
	args := append(append([]string{}, landQuiet...), codeafGitIdentity()...)
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
	// A REFUSAL IS SAID ONLY AS FAR AS IT WAS SEEN. git writes ORIG_HEAD and
	// objects before it can refuse, and a hook may write anywhere, ignored files
	// included, where no status reaches. So a refusal is never "nothing was
	// changed": it is the facts read afterwards, and only when no hook could
	// have run; with one, the answer is uncertain.
	if !timedOut && runErr != nil && !plan.runsHooks() && nothingMoved(plan, facts.LandFacts) {
		said := strings.TrimSpace(string(output.data))
		if line, _, ok := strings.Cut(said, "\n"); ok {
			said = line
		}
		return TaskBranchLanded{}, refuseLand(LandRefused, "git would not merge "+plan.Branch+" ("+said+"); afterwards "+plan.Target+
			" was still at "+plan.Head+", with no merge in progress and no change git reports. Files git ignores were not looked at")
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
	facts.MergeInProgress, facts.IndexLocked = landExists(plan.GitDir, "MERGE_HEAD"), landExists(plan.GitDir, "index.lock")
	if paths, err := landStatus(ctx, plan.Root); err == nil {
		clean := len(paths) == 0
		facts.Clean = &clean
	}
	if facts.MergeInProgress != nil && *facts.MergeInProgress {
		facts.Conflicted = landConflicted(ctx, plan.Root)
	}
	if facts.Head != "" {
		facts.tree, facts.parents = landShape(ctx, plan.Root, facts.Head)
	}
	return facts
}

// landExists is whether git's own file is there, nil when that cannot be said.
func landExists(gitDir, name string) *bool {
	_, err := os.Lstat(filepath.Join(gitDir, name))
	if err != nil && !os.IsNotExist(err) {
		return nil
	}
	there := err == nil
	return &there
}

func landConflicted(ctx context.Context, root string) []string {
	out, code, err := landRead(ctx, root, landReadMax, "diff", "--no-ext-diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil || code != 0 {
		return nil
	}
	var paths []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" && len(paths) < landPathsShown {
			paths = append(paths, path)
		}
	}
	return paths
}

// landShape is a commit's tree and parents, empty where git could not say.
func landShape(ctx context.Context, root, head string) (string, []string) {
	tree, _ := landLine(ctx, root, "rev-parse", "--verify", "--quiet", head+"^{tree}")
	out, err := landLine(ctx, root, "rev-list", "--parents", "-n", "1", head)
	if fields := strings.Fields(out); err == nil && len(fields) > 1 {
		return tree, fields[1:]
	}
	return tree, nil
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

// runsHooks says a hook git would run was there when the plan was made.
func (p TaskBranchLandPlan) runsHooks() bool {
	for _, hook := range p.Hooks {
		if hook.Executable {
			return true
		}
	}
	return false
}

// nothingMoved is what can be read of a refused merge having moved nothing
// git tracks: HEAD, the target, no merge, no index lock and a clean status. It
// is not a proof about ignored files or about anything a hook did.
func nothingMoved(plan TaskBranchLandPlan, facts LandFacts) bool {
	return facts.Head == plan.Head && facts.Target == plan.Target && isFalse(facts.MergeInProgress) &&
		isFalse(facts.IndexLocked) && facts.Clean != nil && *facts.Clean
}

func isFalse(b *bool) bool { return b != nil && !*b }
