package verification

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goStream is a `go test -json` stream for one package: each failing test
// with an assertion, each passing test, and the package's own verdict.
func goStream(pkg string, failing, passing []string) string {
	var lines []string
	add := func(action, test, output string) {
		raw, _ := json.Marshal(struct {
			Action, Package, Test, Output string
		}{action, pkg, test, output})
		lines = append(lines, string(raw))
	}
	add("start", "", "")
	for _, name := range passing {
		add("run", name, "")
		add("output", name, "--- PASS: "+name+" (0.00s)\n")
		add("pass", name, "")
	}
	for _, name := range failing {
		add("run", name, "")
		add("output", name, "    x_test.go:9: wrong\n")
		add("output", name, "--- FAIL: "+name+" (0.00s)\n")
		add("fail", name, "")
	}
	if len(failing) > 0 {
		add("output", "", "FAIL\n")
		add("fail", "", "")
	} else {
		add("output", "", "PASS\n")
		add("pass", "", "")
	}
	return strings.Join(lines, "\n")
}

func measuredRepo(t *testing.T) (root, base string, at Snapshot) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	base = git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("the change"), 0600); err != nil {
		t.Fatal(err)
	}
	at, err = SnapshotAt(root)
	if err != nil || at.Head != base {
		t.Fatalf("snapshot %+v: %v", at, err)
	}
	return root, base, at
}

func failedGoCheck(root string, args ...string) Check {
	exit := 1
	return Check{Command: strings.Join(args, " "), Cwd: root, Args: args, Context: make([]string, len(checkContextNames)), Exit: &exit}
}

func TestTheFactoryClearsOnlyFailuresTheBaseAlreadyHas(t *testing.T) {
	const pkg = "example.com/p"
	for _, tc := range []struct {
		name          string
		mine, theirs  []string
		outcome       string
		cleared       int
		wantNew       string
		baseRunWanted bool
	}{
		{"the same test fails on the base", []string{"TestOld"}, []string{"TestOld", "TestOther"}, MeasuredOnBase, 1, "", true},
		{"one failure is new", []string{"TestOld", "TestMine"}, []string{"TestOld"}, MeasuredNew, 0, pkg + ".TestMine", true},
		{"the base passes", []string{"TestMine"}, nil, MeasuredNew, 0, pkg + ".TestMine", true},
		{"the checkout passes as it is", nil, nil, MeasuredPassed, 1, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, base, at := measuredRepo(t)
			transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
			check := failedGoCheck(root, "go", "test", "./...", "-count=1", "-run", "TestOld|TestMine")
			latest := map[string]Check{check.Key(): check}
			baseRuns := 0
			run := func(_ context.Context, dir string, _ []string, argv []string) (string, int, error) {
				if got := strings.Join(argv, " "); got != "go test -json -count=1 ./... -run TestOld|TestMine" {
					t.Errorf("ran %q", got)
				}
				failing := tc.mine
				if dir != root {
					baseRuns++
					failing = tc.theirs
					// The base worktree is the base, clean, and outside the checkout.
					if raw, err := os.ReadFile(filepath.Join(dir, "code.txt")); err != nil || string(raw) != "before" {
						t.Errorf("base worktree holds %q: %v", raw, err)
					}
					if strings.HasPrefix(dir, root+string(filepath.Separator)) {
						t.Errorf("base worktree %s is inside the job checkout", dir)
					}
				}
				exit := 0
				if len(failing) > 0 {
					exit = 1
				}
				return goStream(pkg, failing, []string{"TestFine"}), exit, nil
			}
			n, err := MeasureBase(context.Background(), transcript, root, latest, at, base, run)
			if err != nil || n != tc.cleared {
				t.Fatalf("cleared %d: %v", n, err)
			}
			m := ReadMeasured(transcript)[check.Key()]
			if m.Outcome != tc.outcome || m.Clears(at, base) != (tc.cleared == 1) || strings.Join(m.New, ",") != tc.wantNew {
				t.Fatalf("measured %+v", m)
			}
			if (baseRuns == 1) != tc.baseRunWanted {
				t.Fatalf("base ran %d times", baseRuns)
			}
			// It holds for this checkout and this base only.
			if m.Clears(Snapshot{Head: at.Head, Fingerprint: strings.Repeat("c", 40)}, base) || m.Clears(at, strings.Repeat("d", 40)) {
				t.Fatal("a measurement cleared another checkout or base")
			}
			// Measured once: a second call runs nothing.
			again := func(context.Context, string, []string, []string) (string, int, error) {
				t.Error("measured the same check on the same checkout twice")
				return "", 0, nil
			}
			if n, err := MeasureBase(context.Background(), transcript, root, latest, at, base, again); err != nil || n != tc.cleared {
				t.Fatalf("second call cleared %d: %v", n, err)
			}
			// The base worktree is gone again.
			out, _ := exec.Command("git", "-C", root, "worktree", "list").Output()
			if strings.Count(strings.TrimSpace(string(out)), "\n") != 0 {
				t.Fatalf("a base worktree was left behind:\n%s", out)
			}
		})
	}
}

func TestTheFactoryMeasuresOnlyPlainGoTests(t *testing.T) {
	root, base, at := measuredRepo(t)
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	passed := failedGoCheck(root, "go", "test", "./...")
	zero := 0
	passed.Exit = &zero
	outside := failedGoCheck(t.TempDir(), "go", "test", "./...")
	withFlags := failedGoCheck(root, "go", "test", "./...")
	withFlags.Context[2] = "-mod=mod -overlay=/tmp/overlay.json"
	latest := map[string]Check{}
	for _, check := range []Check{
		passed, outside, withFlags,
		failedGoCheck(root, "cargo", "test"),
		failedGoCheck(root, "go", "vet", "./..."),
		failedGoCheck(root, "go", "test", "../other"),
		failedGoCheck(root, "go", "test", "./...", "-exec", "wrapper"),
		failedGoCheck(root, "go", "test", "./...", "-args", "-x"),
		failedGoCheck(root, "go", "test"),
	} {
		latest[check.Key()] = check
	}
	run := func(_ context.Context, _ string, _ []string, argv []string) (string, int, error) {
		t.Errorf("ran %v", argv)
		return "", 0, nil
	}
	if n, err := MeasureBase(context.Background(), transcript, root, latest, at, base, run); err != nil || n != 0 {
		t.Fatalf("cleared %d: %v", n, err)
	}
}

func TestAStreamTheFactoryCannotReadClearsNothing(t *testing.T) {
	root, base, at := measuredRepo(t)
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	check := failedGoCheck(root, "go", "test", "./...")
	latest := map[string]Check{check.Key(): check}
	for name, run := range map[string]GoTestRun{
		"a build failure": func(context.Context, string, []string, []string) (string, int, error) {
			return `{"ImportPath":"example.com/p","Action":"build-fail"}`, 1, nil
		},
		"exit 2": func(context.Context, string, []string, []string) (string, int, error) {
			return goStream("example.com/p", []string{"TestOld"}, nil), 2, nil
		},
		"a red exit with no failing test": func(context.Context, string, []string, []string) (string, int, error) {
			return goStream("example.com/p", nil, []string{"TestFine"}), 1, nil
		},
	} {
		_ = os.RemoveAll(transcript + ".checks")
		if n, err := MeasureBase(context.Background(), transcript, root, latest, at, base, run); err != nil || n != 0 {
			t.Fatalf("%s: cleared %d: %v", name, n, err)
		}
		if m := ReadMeasured(transcript)[check.Key()]; m.Outcome != MeasuredUnknown {
			t.Fatalf("%s: measured %+v", name, m)
		}
	}
}

func TestACheckoutThatMovesWhileMeasuredClearsNothing(t *testing.T) {
	root, base, at := measuredRepo(t)
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	check := failedGoCheck(root, "go", "test", "./...")
	run := func(_ context.Context, dir string, _ []string, _ []string) (string, int, error) {
		if dir == root {
			_ = os.WriteFile(filepath.Join(root, "left-behind.txt"), []byte("x"), 0600)
		}
		return goStream("example.com/p", []string{"TestOld"}, nil), 1, nil
	}
	n, err := MeasureBase(context.Background(), transcript, root, map[string]Check{check.Key(): check}, at, base, run)
	if err == nil || n != 0 || len(ReadMeasured(transcript)) != 0 {
		t.Fatalf("cleared %d, kept %v: %v", n, ReadMeasured(transcript), err)
	}
}

// The real runner on a real module: a test that fails on the base and still
// fails with the change is cleared, and one the change breaks is not.
func TestTheFactoryComparesRealGoTestRuns(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	if testing.Short() {
		t.Skip("runs go test four times")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	write("go.mod", "module example.com/m\n\ngo 1.21\n")
	write("old/old.go", "package old\n\nfunc Answer() int { return 1 }\n")
	write("old/old_test.go", "package old\n\nimport \"testing\"\n\nfunc TestAlreadyBroken(t *testing.T) {\n\tif Answer() != 2 {\n\t\tt.Fatal(\"answer is not 2\")\n\t}\n}\n")
	write("mine/mine.go", "package mine\n\nfunc Value() int { return 1 }\n")
	write("mine/mine_test.go", "package mine\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatal(\"value changed\")\n\t}\n}\n")
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	// The change: unrelated to old, and it breaks mine.
	write("mine/mine.go", "package mine\n\nfunc Value() int { return 3 }\n")
	at, err := SnapshotAt(root)
	if err != nil {
		t.Fatal(err)
	}
	old := failedGoCheck(root, "go", "test", "./old", "-count=1")
	mine := failedGoCheck(root, "go", "test", "./mine", "-count=1")
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	n, err := MeasureBase(context.Background(), transcript, root, map[string]Check{old.Key(): old, mine.Key(): mine}, at, base, nil)
	kept := ReadMeasured(transcript)
	if err != nil || n != 1 || kept[old.Key()].Outcome != MeasuredOnBase || kept[mine.Key()].Outcome != MeasuredNew {
		t.Fatalf("cleared %d: %v\nold  %+v\nmine %+v", n, err, kept[old.Key()], kept[mine.Key()])
	}
	if got := strings.Join(kept[old.Key()].Failed, ","); got != "example.com/m/old.TestAlreadyBroken" {
		t.Fatalf("old failed %q", got)
	}
}
