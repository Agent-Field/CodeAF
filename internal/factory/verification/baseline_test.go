package verification

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func goBaselineOutput(assertion string) string {
	const pkg = "example.com/project/api"
	var lines []string
	add := func(action, test, output string) {
		raw, _ := json.Marshal(struct {
			Action, Package, Test, Output string
		}{action, pkg, test, output})
		lines = append(lines, string(raw))
	}
	add("start", "", "")
	add("run", "TestAPI", "")
	add("output", "TestAPI", "=== RUN   TestAPI\n")
	add("run", "TestAPI/existing", "")
	add("output", "TestAPI/existing", "=== RUN   TestAPI/existing\n")
	add("output", "TestAPI/existing", "    api_test.go:18: "+assertion+"\n")
	add("output", "TestAPI/existing", "    --- FAIL: TestAPI/existing (0.00s)\n")
	add("fail", "TestAPI/existing", "")
	add("output", "TestAPI", "--- FAIL: TestAPI (0.01s)\n")
	add("fail", "TestAPI", "")
	add("output", "", "FAIL\n")
	add("output", "", "FAIL\texample.com/project/api\t0.021s\n")
	add("fail", "", "")
	return strings.Join(lines, "\n")
}

func TestBaselineMatchesOnlyTheSameVerifiedFailure(t *testing.T) {
	base := strings.Repeat("a", 40)
	current := Snapshot{Head: base, Fingerprint: strings.Repeat("b", 40)}
	pinned := Snapshot{Head: base, Fingerprint: emptyGitBlobSHA1}
	root := t.TempDir()
	worktree, baseline := filepath.Join(root, "work"), filepath.Join(root, "base")
	common := filepath.Join(root, "repo", ".git")
	exit := 1
	args := []string{"go", "test", "-json", "-count=1", "-run", "^TestAPI$", "./..."}
	check := Check{
		Command: "go test -json -count=1 -run '^TestAPI$' ./...",
		Cwd:     filepath.Join(worktree, "api"), GitRoot: worktree, GitCommonDir: common,
		Args: args, Context: make([]string, len(checkContextNames)), Exit: &exit,
		Before: current, After: current, CallID: "candidate", Source: "transcript",
		OwnerRoot: worktree, order: 1,
	}
	old := check
	old.Command = "cd '" + filepath.Join(baseline, "api") + "' && " + check.Command
	old.Cwd, old.GitRoot, old.Diagnostic = filepath.Join(baseline, "api"), baseline, true
	old.Before, old.After, old.CallID, old.order = pinned, pinned, "base", 2
	old.CleanDetachedBase = true

	for _, tc := range []struct {
		name   string
		change func(*Check, *Check, *[]Check, map[string]string)
		want   bool
	}{
		{name: "same assertion", want: true},
		{name: "fenced module cache permissions", want: true, change: func(c, b *Check, _ *[]Check, _ map[string]string) {
			c.Context[2] = "-modcacherw"
			b.Context[2] = "-modcacherw"
		}},
		{name: "module cache permissions with overlay", change: func(c, b *Check, _ *[]Check, _ map[string]string) {
			c.Context[2] = "-modcacherw -overlay=other.json"
			b.Context[2] = c.Context[2]
		}},
		{name: "module cache permissions with module override", change: func(c, b *Check, _ *[]Check, _ map[string]string) {
			c.Context[2] = "-modcacherw -modfile=other.mod"
			b.Context[2] = c.Context[2]
		}},
		{name: "changed assertion", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			o["candidate"] = goBaselineOutput("different reason")
		}},
		{name: "other subtest", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			o["candidate"] = strings.ReplaceAll(o["candidate"], "TestAPI/existing", "TestAPI/new")
		}},
		{name: "base has an extra existing failure", want: true, change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			extra := strings.ReplaceAll(goBaselineOutput("already fixed in candidate"), "example.com/project/api", "example.com/project/other")
			o["base"] += "\n" + extra
		}},
		{name: "candidate has an extra failure", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			extra := strings.ReplaceAll(goBaselineOutput("new failure"), "example.com/project/api", "example.com/project/other")
			o["candidate"] += "\n" + extra
		}},
		{name: "package failure without test event", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			lines := strings.Split(o["candidate"], "\n")
			o["candidate"] = strings.Join(append(lines[:1], lines[len(lines)-3:]...), "\n")
		}},
		{name: "mixed test and build failure", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			o["candidate"] += "\n" + `{"ImportPath":"example.com/project/other","Action":"build-fail"}`
		}},
		{name: "incomplete output", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			o["base"] = strings.Split(o["base"], "\n")[0]
		}},
		{name: "no assertion diagnostic", change: func(_, _ *Check, _ *[]Check, o map[string]string) {
			for _, id := range []string{"base", "candidate"} {
				o[id] = strings.ReplaceAll(o[id], "    api_test.go:18: existing reason", "")
			}
		}},
		{name: "dirty base", change: func(_, b *Check, _ *[]Check, _ map[string]string) { b.Before, b.After = current, current }},
		{name: "different base", change: func(_, b *Check, _ *[]Check, _ map[string]string) {
			b.Before.Head = strings.Repeat("c", 40)
			b.After = b.Before
		}},
		{name: "different Git repository", change: func(_, b *Check, _ *[]Check, _ map[string]string) { b.GitCommonDir += "-unrelated" }},
		{name: "different checkout-relative directory", change: func(_, b *Check, _ *[]Check, _ map[string]string) { b.Cwd = b.GitRoot }},
		{name: "different flags", change: func(_, b *Check, _ *[]Check, _ map[string]string) {
			b.Args = append(append([]string{}, b.Args...), "-race")
			b.Command += " -race"
		}},
		{name: "different context", change: func(_, b *Check, _ *[]Check, _ map[string]string) {
			b.Context = make([]string, len(checkContextNames))
			b.Context[0] = "linux"
		}},
		{name: "stale candidate", change: func(c, _ *Check, _ *[]Check, _ map[string]string) { c.After = pinned }},
		{name: "old transcript", change: func(_, b *Check, _ *[]Check, _ map[string]string) { b.Source += "-old" }},
		{name: "inherited base", change: func(_, b *Check, _ *[]Check, _ map[string]string) { b.Inherited = true }},
		{name: "shell wrapper", change: func(c, _ *Check, _ *[]Check, _ map[string]string) { c.Command += " | tail -20" }},
		{name: "trailing background", change: func(c, _ *Check, _ *[]Check, _ map[string]string) { c.Command += " &" }},
		{name: "environment override", change: func(c, _ *Check, _ *[]Check, _ map[string]string) { c.Command = "FOO=bar " + c.Command }},
		{name: "later conflicting baseline", change: func(_, b *Check, extra *[]Check, _ map[string]string) {
			later := *b
			zero := 0
			later.Exit, later.order, later.CallID = &zero, b.order+1, "later"
			*extra = append(*extra, later)
		}},
		{name: "ambiguous order", change: func(_, b *Check, extra *[]Check, _ map[string]string) {
			later := *b
			later.CallID = "also-base"
			*extra = append(*extra, later)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, baseCheck := check, old
			candidate.Context = append([]string(nil), check.Context...)
			baseCheck.Context = append([]string(nil), old.Context...)
			output := goBaselineOutput("existing reason")
			outputs := map[string]string{"candidate": output, "base": output}
			var extra []Check
			if tc.change != nil {
				tc.change(&candidate, &baseCheck, &extra, outputs)
			}
			key := candidate.Key()
			got := BaselineMatches(append([]Check{candidate, baseCheck}, extra...), map[string]Check{key: candidate}, current, base, outputs)
			if match, ok := got[key]; ok != tc.want || ok && match.CallID != old.CallID {
				t.Fatalf("matched %q, want match=%v", match.CallID, tc.want)
			}
		})
	}
}

func TestBaselineMatchesObservedLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("requires the Go test runner")
	}
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	candidate, baseline := filepath.Join(root, "candidate"), filepath.Join(root, "baseline")
	if err := os.MkdirAll(filepath.Join(candidate, "api"), 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = candidate
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, file, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write(candidate, ".gitignore", "ignored.tmp\n")
	write(candidate, "go.mod", "module example.com/baselinefixture\n\ngo 1.24\n")
	write(candidate, "api/api_test.go", `package api

import "testing"

func TestAPI(t *testing.T) {
	t.Run("existing", func(t *testing.T) { t.Fatal("existing failure") })
	t.Run("passing", func(t *testing.T) {})
}
`)
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	git("worktree", "add", "--detach", baseline, base)
	write(candidate, "unrelated.txt", "candidate change\n")

	file := filepath.Join(root, "stage.jsonl")
	outputs := map[string]string{}
	observe := func(id, dir string) {
		t.Helper()
		command := "cd " + quote(filepath.Join(dir, "api")) + " && go test -json -count=1 ."
		wrapped, err := PrepareCallIn(file, candidate, command, id)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "bash", "-c", wrapped)
		cmd.Dir = candidate
		out, err := cmd.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("go test in %s: %v\n%s", id, err, out)
		}
		outputs[id] = string(out)
	}
	observe("candidate", candidate)
	observe("base", baseline)

	checkMatches := func(want bool) {
		t.Helper()
		checks, err := ReadIn(file, candidate)
		if err != nil {
			t.Fatal(err)
		}
		at, err := SnapshotAt(candidate)
		if err != nil {
			t.Fatal(err)
		}
		latest, _ := RequiredChecks(checks, at, true)
		if len(latest) != 1 {
			t.Fatalf("required checks = %d, want 1", len(latest))
		}
		for key, candidate := range latest {
			match, found := BaselineMatches(checks, latest, at, base, outputs)[key]
			if found != want {
				t.Fatalf("match = %v, want %v\nchecks: %+v", found, want, checks)
			}
			if found && (!match.Diagnostic || match.Before.Head != base || *candidate.Exit != 1 || *match.Exit != 1) {
				t.Fatalf("matching changed recorded evidence: candidate=%+v baseline=%+v", candidate, match)
			}
		}
	}
	checkMatches(true)

	// An ignored fixture makes the later reading unsafe even though the
	// checkout fingerprint and the assertion text are still identical.
	write(baseline, "ignored.tmp", "local setup\n")
	observe("base-with-ignored-file", baseline)
	checkMatches(false)
}
