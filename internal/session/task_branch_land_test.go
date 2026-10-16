package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const landBranch = "task/paint-abc123"

// landRepo is a temp repository on main whose recorded task branch changes
// b.txt; main has moved on with c.txt, so the land is a merge commit. Every
// repository here is the test's own, never a person's.
func landRepo(t *testing.T) (repo string, place Place) {
	t.Helper()
	repo = t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "base")
	gitT(t, repo, "checkout", "-q", "-b", landBranch)
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("task\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("mine\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "mine")
	place = newPlace(t, false)
	writeRuns(t, place, []string{landBranch})
	return repo, place
}

func landCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func headOf(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
}

func planLand(t *testing.T, place Place, repo string) TaskBranchLandPlan {
	t.Helper()
	plan, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

func wantRefusal(t *testing.T, err error, reason string) *LandRefusal {
	t.Helper()
	var refusal *LandRefusal
	if !errors.As(err, &refusal) || refusal.Reason != reason {
		t.Fatalf("want refusal %q, got %v", reason, err)
	}
	return refusal
}

// untouched asserts the checkout is exactly where it was: HEAD, a clean
// status and no merge in progress.
func untouched(t *testing.T, repo, head string) {
	t.Helper()
	if got := headOf(t, repo); got != head {
		t.Fatalf("HEAD moved: %s -> %s", head, got)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("a merge was left in progress")
	}
}

func TestALandMergesThePlannedCommitAndProvesIt(t *testing.T) {
	repo, place := landRepo(t)
	head := headOf(t, repo)
	plan := planLand(t, place, repo)
	if plan.Target != "main" || plan.Head != head || plan.FastForward || plan.Task != "1" || plan.Files != 1 || !plan.CountsKnown || len(plan.Hooks) != 0 || plan.Revision == "" {
		t.Fatalf("plan %+v", plan)
	}
	landed, err := LandTaskBranch(landCtx(t), place, repo, plan)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	parents := strings.Fields(gitOut(t, repo, "rev-list", "--parents", "-n", "1", "HEAD"))
	if landed.HeadBefore != head || landed.HeadAfter != parents[0] || len(parents) != 3 || parents[1] != head || parents[2] != plan.BranchHead {
		t.Fatalf("landed %+v parents %v", landed, parents)
	}
	if _, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch); err == nil {
		t.Fatal("a landed branch planned again")
	} else {
		wantRefusal(t, err, LandMerged)
	}
}

func TestAFastForwardLandsAsOne(t *testing.T) {
	repo, place := landRepo(t)
	gitT(t, repo, "reset", "-q", "--hard", "HEAD~1")
	plan := planLand(t, place, repo)
	if !plan.FastForward {
		t.Fatalf("plan %+v", plan)
	}
	landed, err := LandTaskBranch(landCtx(t), place, repo, plan)
	if err != nil || landed.HeadAfter != plan.BranchHead || headOf(t, repo) != plan.BranchHead {
		t.Fatalf("landed %+v %v", landed, err)
	}
}

func TestOnlyARecordedBranchIsPlanned(t *testing.T) {
	repo, place := landRepo(t)
	gitT(t, repo, "branch", "task/stranger-222222", landBranch)
	for _, name := range []string{"task/stranger-222222", "main", "task/../main", ""} {
		if _, err := PlanTaskBranchLand(landCtx(t), place, repo, name); !errors.Is(err, ErrLandNotOwned) {
			t.Fatalf("%q: %v", name, err)
		}
	}
}

func TestADirtyCheckoutIsRefusedAndLeftAlone(t *testing.T) {
	for name, dirty := range map[string]func(repo string){
		"untracked": func(repo string) { os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644) },
		"modified":  func(repo string) { os.WriteFile(filepath.Join(repo, "a.txt"), []byte("edit\n"), 0o644) },
		"staged": func(repo string) {
			os.WriteFile(filepath.Join(repo, "a.txt"), []byte("edit\n"), 0o644)
			gitT(t, repo, "add", "a.txt")
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, place := landRepo(t)
			head := headOf(t, repo)
			dirty(repo)
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			wantRefusal(t, err, LandDirty)
			untouched(t, repo, head)
		})
	}
}

func TestAConflictIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	repo, place := landRepo(t)
	gitT(t, repo, "checkout", "-q", landBranch)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("theirs\n"), 0o644)
	gitT(t, repo, "commit", "-q", "-am", "theirs")
	gitT(t, repo, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("mine\n"), 0o644)
	gitT(t, repo, "commit", "-q", "-am", "mine again")
	head := headOf(t, repo)
	before := gitOut(t, repo, "status", "--porcelain=v2", "--untracked-files=all")
	_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
	refusal := wantRefusal(t, err, LandConflict)
	if len(refusal.Paths) != 1 || refusal.Paths[0] != "a.txt" {
		t.Fatalf("paths %v", refusal.Paths)
	}
	untouched(t, repo, head)
	if after := gitOut(t, repo, "status", "--porcelain=v2", "--untracked-files=all"); after != before {
		t.Fatalf("status moved: %q -> %q", before, after)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(data) != "mine\n" {
		t.Fatalf("a.txt is %q", data)
	}
}

func TestAnIgnoredFileWhereTheBranchWritesIsRefused(t *testing.T) {
	repo, place := landRepo(t)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("build/\n"), 0o644)
	gitT(t, repo, "add", ".gitignore")
	gitT(t, repo, "commit", "-q", "-m", "ignore")
	gitT(t, repo, "checkout", "-q", landBranch)
	os.MkdirAll(filepath.Join(repo, "build"), 0o755)
	os.WriteFile(filepath.Join(repo, "build", "out"), []byte("task's\n"), 0o644)
	gitT(t, repo, "add", "-f", "build/out")
	gitT(t, repo, "commit", "-q", "-m", "out")
	gitT(t, repo, "checkout", "-q", "main")
	os.MkdirAll(filepath.Join(repo, "build"), 0o755)
	os.WriteFile(filepath.Join(repo, "build", "out"), []byte("yours\n"), 0o644)
	head := headOf(t, repo)
	_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
	refusal := wantRefusal(t, err, LandIgnoredInWay)
	if len(refusal.Paths) != 1 || refusal.Paths[0] != "build/out" {
		t.Fatalf("paths %v", refusal.Paths)
	}
	untouched(t, repo, head)
	if data, _ := os.ReadFile(filepath.Join(repo, "build", "out")); string(data) != "yours\n" {
		t.Fatalf("build/out is %q", data)
	}
}

func TestAnythingThatMovesAfterThePlanRefusesTheLand(t *testing.T) {
	for name, move := range map[string]func(t *testing.T, repo string){
		"branch advanced": func(t *testing.T, repo string) {
			gitT(t, repo, "checkout", "-q", landBranch)
			os.WriteFile(filepath.Join(repo, "d.txt"), []byte("late\n"), 0o644)
			gitT(t, repo, "add", ".")
			gitT(t, repo, "commit", "-q", "-m", "late")
			gitT(t, repo, "checkout", "-q", "main")
		},
		"target advanced": func(t *testing.T, repo string) {
			gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "more")
		},
		"checkout moved": func(t *testing.T, repo string) { gitT(t, repo, "checkout", "-q", "-b", "elsewhere") },
		"hook installed": func(t *testing.T, repo string) {
			os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-merge"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
		},
		"config changed": func(t *testing.T, repo string) { gitT(t, repo, "config", "merge.renames", "false") },
	} {
		t.Run(name, func(t *testing.T) {
			repo, place := landRepo(t)
			plan := planLand(t, place, repo)
			move(t, repo)
			head := headOf(t, repo)
			_, err := LandTaskBranch(landCtx(t), place, repo, plan)
			wantRefusal(t, err, LandChanged)
			untouched(t, repo, head)
		})
	}
}

func TestHooksAreBoundByTheirBytesWhereGitLooks(t *testing.T) {
	repo, place := landRepo(t)
	hooks := t.TempDir()
	os.WriteFile(filepath.Join(hooks, "post-merge"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	gitT(t, repo, "config", "core.hooksPath", hooks)
	plan := planLand(t, place, repo)
	if len(plan.Hooks) != 1 || plan.Hooks[0].Name != "post-merge" || !plan.Hooks[0].Executable || plan.Hooks[0].Size != 17 || len(plan.Hooks[0].SHA256) != 64 {
		t.Fatalf("hooks %+v", plan.Hooks)
	}
	os.WriteFile(filepath.Join(hooks, "post-merge"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	_, err := LandTaskBranch(landCtx(t), place, repo, plan)
	wantRefusal(t, err, LandChanged)
}

func TestConfigurationALandWillNotMergeUnder(t *testing.T) {
	for name, c := range map[string]struct {
		set    [][2]string
		reason string
	}{
		"merge driver": {[][2]string{{"merge.mine.driver", "cat %A"}}, LandCustomMerge},
		"filter":       {[][2]string{{"filter.lfs.smudge", "cat"}}, LandFilter},
		"ff only":      {[][2]string{{"merge.ff", "only"}}, LandFastForward},
		"signing":      {[][2]string{{"commit.gpgSign", "true"}}, LandSigning},
		"options":      {[][2]string{{"branch.main.mergeOptions", "--no-commit"}}, LandMergeOptions},
		"sparse":       {[][2]string{{"core.sparseCheckout", "true"}}, LandSparse},
	} {
		t.Run(name, func(t *testing.T) {
			repo, place := landRepo(t)
			for _, kv := range c.set {
				gitT(t, repo, "config", kv[0], kv[1])
			}
			_, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch)
			wantRefusal(t, err, c.reason)
		})
	}
}

func TestTheStrategyAndStashArePinned(t *testing.T) {
	repo, place := landRepo(t)
	gitT(t, repo, "config", "pull.twohead", "resolve")
	gitT(t, repo, "config", "merge.autoStash", "true")
	plan := planLand(t, place, repo)
	if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
		t.Fatalf("land: %v", err)
	}
	if out := gitOut(t, repo, "stash", "list"); strings.TrimSpace(out) != "" {
		t.Fatalf("a stash was made: %q", out)
	}
}

func TestALandThatCannotHaveTheLockIsBusyAndTouchesNothing(t *testing.T) {
	repo, place := landRepo(t)
	plan := planLand(t, place, repo)
	held := openGitRootLock(place, plan.Root)
	if held == nil {
		t.Fatal("no lock file")
	}
	defer held.Close()
	if err := filelock.Lock(held, true, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	head := headOf(t, repo)
	if _, err := LandTaskBranch(ctx, place, repo, plan); !errors.Is(err, ErrLandBusy) {
		t.Fatalf("held lock: %v", err)
	}
	untouched(t, repo, head)
	if _, err := LandTaskBranch(landCtx(t), Place{}, repo, plan); !errors.Is(err, ErrLandBusy) {
		t.Fatalf("no place: %v", err)
	}
	untouched(t, repo, head)
}

// A hook that never ends is killed with its children at the merge's ceiling,
// and what the checkout then holds is reported, not claimed.
func TestAHookThatHangsEndsUncertainWithinTheCeiling(t *testing.T) {
	repo, place := landRepo(t)
	hook := "#!/bin/sh\nhead -c 1000000 /dev/zero\nsleep 600 &\nwait\n"
	os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-merge"), []byte(hook), 0o755)
	plan := planLand(t, place, repo)
	old := landMergeMax
	landMergeMax = 2 * time.Second
	t.Cleanup(func() { landMergeMax = old })
	started := time.Now()
	_, err := LandTaskBranch(landCtx(t), place, repo, plan)
	var uncertain *LandUncertain
	if !errors.As(err, &uncertain) {
		t.Fatalf("want uncertain, got %v", err)
	}
	if took := time.Since(started); took > 15*time.Second {
		t.Fatalf("took %s", took)
	}
	if uncertain.Facts.Head == "" || uncertain.Facts.Target != "main" || uncertain.Facts.MergeInProgress == nil {
		t.Fatalf("facts %+v", uncertain.Facts)
	}
}

// A write that races in after the last check is never aborted or reset: git
// refuses, and what stands is reported.
func TestARaceAfterTheChecksIsReportedNotUndone(t *testing.T) {
	repo, place := landRepo(t)
	plan := planLand(t, place, repo)
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		for _, arg := range args {
			if arg == "merge" {
				os.WriteFile(filepath.Join(repo, "b.txt"), []byte("raced\n"), 0o644)
				break
			}
		}
		return real(ctx, dir, args...)
	}
	head := headOf(t, repo)
	_, err := LandTaskBranch(landCtx(t), place, repo, plan)
	var uncertain *LandUncertain
	if !errors.As(err, &uncertain) || uncertain.Facts.Head != head || uncertain.Facts.Clean == nil || *uncertain.Facts.Clean {
		t.Fatalf("want uncertain with facts, got %v %+v", err, uncertain)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "b.txt")); string(data) != "raced\n" {
		t.Fatalf("the raced file was touched: %q", data)
	}
}

func TestEveryBoundFactMovesTheRevision(t *testing.T) {
	base := TaskBranchLandPlan{Task: "1", Branch: "b", BranchHead: "h", Base: "m", Target: "t", Head: "x", Root: "r", GitDir: "g", CommonDir: "c", Tree: "tr", config: "cf", copies: "cp", Hooks: []LandHook{{Name: "post-merge", SHA256: "s", Size: 1}}}
	seen := map[string]string{base.digest(): "base"}
	for name, change := range map[string]func(p *TaskBranchLandPlan){
		"task": func(p *TaskBranchLandPlan) { p.Task = "2" }, "branch": func(p *TaskBranchLandPlan) { p.Branch = "c" },
		"branch head": func(p *TaskBranchLandPlan) { p.BranchHead = "i" }, "base": func(p *TaskBranchLandPlan) { p.Base = "n" },
		"target": func(p *TaskBranchLandPlan) { p.Target = "u" }, "head": func(p *TaskBranchLandPlan) { p.Head = "y" },
		"root": func(p *TaskBranchLandPlan) { p.Root = "s" }, "git dir": func(p *TaskBranchLandPlan) { p.GitDir = "h" },
		"common dir": func(p *TaskBranchLandPlan) { p.CommonDir = "d" }, "ff": func(p *TaskBranchLandPlan) { p.FastForward = true },
		"tree": func(p *TaskBranchLandPlan) { p.Tree = "ts" }, "config": func(p *TaskBranchLandPlan) { p.config = "cg" },
		"copies":     func(p *TaskBranchLandPlan) { p.copies = "cq" },
		"hook bytes": func(p *TaskBranchLandPlan) { p.Hooks = []LandHook{{Name: "post-merge", SHA256: "t", Size: 1}} },
		"hook mode": func(p *TaskBranchLandPlan) {
			p.Hooks = []LandHook{{Name: "post-merge", SHA256: "s", Size: 1, Executable: true}}
		},
		"no hook": func(p *TaskBranchLandPlan) { p.Hooks = nil },
	} {
		p := base
		change(&p)
		if other, dup := seen[p.digest()]; dup {
			t.Fatalf("%s digests the same as %s", name, other)
		}
		seen[p.digest()] = name
	}
}

// A merge git refuses is said as far as it was seen: never "nothing was
// changed", and never a refusal at all when a hook was there to run.
func TestARefusedMergeIsSaidOnlyAsFarAsItWasSeen(t *testing.T) {
	refuseMerge := func(t *testing.T) {
		real := landCommand
		t.Cleanup(func() { landCommand = real })
		landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
			for _, arg := range args {
				if arg == "merge" {
					return exec.CommandContext(ctx, "git", "merge", "--no-such-option")
				}
			}
			return real(ctx, dir, args...)
		}
	}
	t.Run("no hook", func(t *testing.T) {
		repo, place := landRepo(t)
		plan := planLand(t, place, repo)
		refuseMerge(t)
		head := headOf(t, repo)
		_, err := LandTaskBranch(landCtx(t), place, repo, plan)
		refusal := wantRefusal(t, err, LandRefused)
		if strings.Contains(refusal.Said, "nothing was changed") || !strings.Contains(refusal.Said, "ignore") {
			t.Fatalf("said %q", refusal.Said)
		}
		untouched(t, repo, head)
	})
	t.Run("hook present", func(t *testing.T) {
		repo, place := landRepo(t)
		os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-merge"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
		plan := planLand(t, place, repo)
		refuseMerge(t)
		_, err := LandTaskBranch(landCtx(t), place, repo, plan)
		var uncertain *LandUncertain
		if !errors.As(err, &uncertain) {
			t.Fatalf("want uncertain with a hook present, got %v", err)
		}
	})
}

// looseObjects writes n small blobs straight into the object store, one git.
func looseObjects(t *testing.T, repo string, n int) {
	t.Helper()
	dir := t.TempDir()
	var paths strings.Builder
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprint(i))
		os.WriteFile(path, []byte(fmt.Sprint("blob ", i)), 0o644)
		paths.WriteString(path + "\n")
	}
	cmd := exec.Command("git", "hash-object", "-w", "--stdin-paths")
	cmd.Dir, cmd.Stdin = repo, strings.NewReader(paths.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hash-object: %v %s", err, out)
	}
}

// gcRepo is landRepo with automatic gc due after any merge, and a pre-auto-gc
// hook that leaves a marker outside the repository and then declines the gc,
// so no gc ever runs or detaches here.
func gcRepo(t *testing.T) (repo string, place Place, marker string) {
	t.Helper()
	repo, place = landRepo(t)
	marker = filepath.Join(t.TempDir(), "pre-auto-gc-ran")
	hook := "#!/bin/sh\necho ran > '" + marker + "'\nexit 1\n"
	os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-auto-gc"), []byte(hook), 0o755)
	gitT(t, repo, "config", "gc.auto", "1")
	gitT(t, repo, "config", "gc.autoDetach", "false")
	looseObjects(t, repo, 2000)
	return repo, place, marker
}

// A land runs no automatic maintenance: the hook it would start is one the
// plan never named, and a detached gc outlives every kill.
func TestALandStartsNoAutomaticMaintenance(t *testing.T) {
	// The control: the same repository, merged by plain git, does start it.
	control, _, fired := gcRepo(t)
	gitT(t, control, "merge", "-q", "--no-edit", landBranch)
	if _, err := os.Stat(fired); err != nil {
		t.Fatalf("the fixture never starts automatic gc, so it proves nothing: %v", err)
	}
	repo, place, marker := gcRepo(t)
	plan := planLand(t, place, repo)
	if _, err := LandTaskBranch(landCtx(t), place, repo, plan); err != nil {
		t.Fatalf("land: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pre-auto-gc ran during a land: %v", err)
	}
}

// A real refusal with a hook that ran before it: git rewrote ORIG_HEAD and the
// reference-transaction hook wrote its marker, so the answer may not say that
// nothing changed, and with a hook present it is not a refusal at all.
func TestARefusalAfterAHookRanIsNeverNothingChanged(t *testing.T) {
	repo, place := landRepo(t)
	marker := filepath.Join(t.TempDir(), "transaction")
	hook := "#!/bin/sh\necho \"$1\" >> '" + marker + "'\nexit 0\n"
	os.WriteFile(filepath.Join(repo, ".git", "hooks", "reference-transaction"), []byte(hook), 0o755)
	plan := planLand(t, place, repo)
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		for _, arg := range args {
			if arg == "merge" {
				os.WriteFile(filepath.Join(repo, "b.txt"), []byte("in the way\n"), 0o644)
				break
			}
		}
		return real(ctx, dir, args...)
	}
	head := headOf(t, repo)
	_, err := LandTaskBranch(landCtx(t), place, repo, plan)
	var uncertain *LandUncertain
	if !errors.As(err, &uncertain) || strings.Contains(uncertain.Said, "nothing was changed") || uncertain.Facts.Head != head {
		t.Fatalf("want uncertain naming the facts, got %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || len(data) == 0 {
		t.Fatalf("the hook did not run, so this proves nothing: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "b.txt")); string(data) != "in the way\n" {
		t.Fatalf("b.txt is %q", data)
	}
	untouched(t, repo, head)
}
