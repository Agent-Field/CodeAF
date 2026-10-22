package run

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestChangeIncludesCommittedStagedUnstagedAndUntrackedFiles(t *testing.T) {
	dir := gitRig(t)
	base := changeBase(dir, factory.Item{Kind: factory.KindIssue})
	mustGit(t, dir, "checkout", "-q", "-b", "factory/change")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("committed.go", "one\ntwo\n")
	mustGit(t, dir, "add", "committed.go")
	mustGit(t, dir, "commit", "-q", "-m", "a step committed")
	write("staged.go", "staged\n")
	mustGit(t, dir, "add", "staged.go")
	write("ledger.go", "package ledger\n// unstaged\n")
	write(" new\tfile\n.go", "one\ntwo")
	write("binary", "\x00\x01\x02")
	write(".gitignore", "ignored\n")
	write("ignored", "not part of the change\n")
	change, err := readChange(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	want := &factory.Change{Branch: "factory/change", Stat: &factory.ChangeStat{Files: 6, Added: 7}, Files: []factory.ChangedFile{
		{Path: " new\tfile\n.go", Added: 2, Status: "added"},
		{Path: ".gitignore", Added: 1, Status: "added"},
		{Path: "binary", Status: "added"},
		{Path: "committed.go", Added: 2, Status: "added"},
		{Path: "ledger.go", Added: 1, Status: "modified"},
		{Path: "staged.go", Added: 1, Status: "added"},
	}}
	if !strings.Contains(change.Patch, "// unstaged") || !strings.Contains(change.Patch, "committed.go") {
		t.Fatal("patch preview lost changes")
	}
	want.Patch = change.Patch
	if !reflect.DeepEqual(change, want) {
		t.Fatalf("change = %+v, files = %+v; want %+v", change, change.Files, want)
	}
	if err := validateChange(change); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ledger.go")); err != nil {
		t.Fatal(err)
	}
	change, err = readChange(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range change.Files {
		if f.Path == "ledger.go" && (f.Status != "removed" || f.Removed != 1 || f.Added != 0) {
			t.Fatalf("removed file = %+v", f)
		}
	}
}

func TestChangeForPRIncludesItsOriginalDiff(t *testing.T) {
	checkout, _ := prRig(t)
	w := NewWorkdirs(filepath.Join(t.TempDir(), "work"), ExecGit{})
	it := prItem()
	dir, err := w.For(it, checkout)
	if err != nil {
		t.Fatal(err)
	}
	change, err := readChange(dir, changeBase(dir, it))
	if err != nil {
		t.Fatal(err)
	}
	if change.Stat == nil || change.Stat.Files != 1 || change.Files[0].Path != "ledger.go" || change.Stat.Added != 2 {
		t.Fatalf("PR change = %+v", change)
	}
}

func TestRunnerReportsIssueChangeAndEachChecksLatestCounts(t *testing.T) {
	dir := gitRig(t)
	mustGit(t, dir, "checkout", "-q", "-b", "factory/issue")
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("new\n"), 0o600); err != nil {
				return factory.StageResult{}, err
			}
			mustGit(t, dir, "add", "new.go")
			mustGit(t, dir, "commit", "-q", "-m", "write step")
			return factory.StageResult{Done: true}, nil
		}),
		factory.StageCheck: NewCheckExecutor(CheckOptions{}),
	}, func(o *Options) { o.RepoDir = func(string) string { return dir } })
	id := g.add("issue change", factory.Stage{Name: "write", Kind: factory.StageChat},
		factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "printf '%s\n' '--- PASS: TestOne (0.00s)' '--- SKIP: TestTwo (0.00s)'"},
		factory.Stage{Name: "build", Kind: factory.StageCheck, Ask: "true"})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateShipped)
	if it.Change == nil || it.Change.Branch != "factory/issue" || it.Change.Stat == nil || it.Change.Stat.Files != 1 || len(it.Change.Tests) != 1 || it.Change.Tests[0].Step != "test" || it.Change.Tests[0].Passed != 1 || it.Change.Tests[0].Skipped != 1 {
		t.Fatalf("issue change = %+v", it.Change)
	}
	before := factory.Item{ID: id, State: factory.StateRunning, Stages: it.Stages, Stream: &factory.Stream{}}
	report, ok := Diff(before, it)
	if !ok || report.Change == nil || report.ChangeBase == "" {
		t.Fatalf("worker report = %+v", report)
	}
	if err := ApplyReport(&before, report, g.r.now()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Change, it.Change) || before.ChangeBase != it.ChangeBase {
		t.Fatalf("server lost change: %+v", before.Change)
	}
	// Re-running the same check with no counts must remove its old totals.
	job := Job{Dir: dir, Item: it, Stage: factory.Stage{Name: "test", Kind: factory.StageCheck}}
	c := &loopCtl{id: id, ctx: context.Background()}
	g.r.loop().recordChange(c, job, factory.StageResult{Done: true})
	got, _ := g.st.Get(id)
	if len(got.Change.Tests) != 0 || len(it.Change.Tests) != 1 {
		t.Fatalf("counts were stale or the prior snapshot was mutated: %+v", got.Change)
	}
}

func TestCheckCountsComeFromFullOutputAndOmitUnknownCounts(t *testing.T) {
	cases := []struct {
		name, output string
		want         *factory.TestCounts
	}{
		{"Go verbose", "--- PASS: TestOne (0.00s)\n--- FAIL: TestTwo (0.00s)\n--- SKIP: TestThree (0.00s)\n", &factory.TestCounts{Passed: 1, Failed: 1, Skipped: 1}},
		{"Go JSON", "{\"Action\":\"pass\",\"Test\":\"TestOne\"}\n{\"Action\":\"skip\",\"Test\":\"TestTwo\"}\n{\"Action\":\"pass\",\"Package\":\"example\"}", &factory.TestCounts{Passed: 1, Skipped: 1}},
		{"TAP", "TAP version 13\n1..3\nok 1 - works\nnot ok 2 - fails\nok 3 - unavailable # SKIP no key", &factory.TestCounts{Passed: 1, Failed: 1, Skipped: 1}},
		{"pytest", "================ 2 failed, 10 passed, 3 skipped in 0.5s ================", &factory.TestCounts{Passed: 10, Failed: 2, Skipped: 3}},
		{"package summary", "ok  example.com/pkg 0.01s\nPASS\nexit 0\n", nil},
		{"no tests", "================ no tests ran in 0.5s ================", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testCounts(tc.output); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("counts = %+v; want %+v", got, tc.want)
			}
		})
	}
	var logs logSink
	job := checkJob(t, "printf '%s\n' '--- PASS: TestOne (0.00s)'; for i in {1..60}; do echo output; done; exit 1", &logs)
	res, err := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if res.Exit != 1 || res.Tests == nil || res.Tests.Passed != 1 {
		t.Fatalf("full output counts = %+v", res)
	}
}

func TestApprovedCommitMessageStaysInsidePublicBoundary(t *testing.T) {
	dir := gitRig(t)
	if err := os.WriteFile(filepath.Join(dir, "fix.go"), []byte("package fix\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Kind: factory.KindIssue, Repo: "api", Product: "acme", Num: 7, Title: "Fix /Users/private-person/notes handling"}
	if err := commitApproved(ExecGit{}, dir, it, "Co-authored-by: codeaf <codeaf@users.noreply.github.com>"); err != nil {
		t.Fatal(err)
	}
	msg := mustGit(t, dir, "log", "-1", "--format=%B")
	if strings.Contains(msg, "private-person") || !strings.Contains(msg, "[local home]/notes") || !strings.Contains(msg, "codeaf@users.noreply.github.com") {
		t.Fatalf("commit message: %q", msg)
	}
}
