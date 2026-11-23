package verification

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runOwned(t *testing.T, file, owner, cwd, command string) int {
	t.Helper()
	wrapped, err := PrepareIn(file, owner, command)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	if x, ok := err.(*exec.ExitError); ok {
		return x.ExitCode()
	}
	t.Fatalf("%v: %s", err, out)
	return -1
}

func TestRecognizedCheckIsTheDescendantEvidenceBoundary(t *testing.T) {
	for _, exit := range []int{0, 7} {
		t.Run(string(rune('0'+exit)), func(t *testing.T) {
			root, prefix := fakeChecks(t)
			file := filepath.Join(t.TempDir(), "session.jsonl")
			// The outer runner judges a deliberately failing fixture itself.
			script := "#!/bin/bash\ngo test ./fixture >/dev/null 2>&1 || true\nexit " + string(rune('0'+exit)) + "\n"
			if err := os.WriteFile(filepath.Join(root, "bin", "npm"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			got := runOwned(t, file, root, root, prefix+"CHECK_EXIT=9 npm test")
			checks, err := Read(file)
			if err != nil || len(checks) != 1 || checks[0].Exit == nil || *checks[0].Exit != exit || got != exit {
				t.Fatalf("parent/fixture confused: exit=%d checks=%+v err=%v", got, checks, err)
			}
		})
	}
}

func TestCompiledFixtureHelper(t *testing.T) {
	if os.Getenv("CODEAF_VERIFICATION_COMPILED_HELPER") != "yes" {
		return
	}
	cmd := exec.Command("bash", "-c", "CHECK_EXIT=7 go test ./fixture")
	cmd.Dir = os.Getenv("CODEAF_VERIFICATION_FIXTURE_DIR")
	if err := cmd.Run(); err == nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestOwnerBoundaryContainsCompiledWrapperFixtureEvidence(t *testing.T) {
	root, prefix := fakeChecks(t)
	fixture := t.TempDir()
	file := filepath.Join(t.TempDir(), "session.jsonl")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := prefix + "CODEAF_VERIFICATION_COMPILED_HELPER=yes CODEAF_VERIFICATION_FIXTURE_DIR=" + quote(fixture) + " " + quote(binary) + " -test.run '^TestCompiledFixtureHelper$'"
	if exit := runOwned(t, file, root, root, command); exit != 0 {
		t.Fatal(exit)
	}
	checks, err := Read(file)
	if err != nil || len(checks) != 1 || !checks[0].Diagnostic || checks[0].Exit == nil || *checks[0].Exit != 7 {
		t.Fatalf("compiled fixture was not diagnostic: %+v %v", checks, err)
	}
	required, _ := RequiredChecks(checks, Snapshot{}, false)
	if len(required) != 0 {
		t.Fatal("outside-owner fixture became required")
	}
	// A prepared worktree is usable only through its own explicit owner boundary.
	prepared := filepath.Join(t.TempDir(), "prepared.jsonl")
	if exit := runOwned(t, prepared, fixture, root, prefix+"cd "+quote(fixture)+" && go test ./fixture"); exit != 0 {
		t.Fatal(exit)
	}
	checks, err = Read(prepared)
	if err != nil || len(checks) != 1 || checks[0].Diagnostic {
		t.Fatalf("explicit prepared owner rejected: %+v %v", checks, err)
	}
}

func TestPartialReceiptDoesNotHideIndependentCompletedChecks(t *testing.T) {
	root, prefix := fakeChecks(t)
	file := filepath.Join(t.TempDir(), "session.jsonl")
	runOwned(t, file, root, root, prefix+"go test ./...")
	place := filepath.Join(file+".checks", "call-partial")
	if err := os.Mkdir(place, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(place, "command.json"), []byte(`"go vet ./..."`), 0600)
	os.WriteFile(filepath.Join(place, "check-partial"), []byte("partial"), 0600)
	checks, err := Read(file)
	if err != nil || len(checks) != 2 {
		t.Fatalf("partial hid ledger: %+v %v", checks, err)
	}
	required, _ := RequiredChecks(checks, Snapshot{}, false)
	if len(required) != 1 {
		t.Fatal("unadmitted placeholder became a permanent obligation")
	}
	for _, c := range checks {
		if len(c.Args) == 0 && (c.Exit != nil || c.Problem != "") {
			t.Fatal("in-flight metadata became terminal", c)
		}
	}
	os.WriteFile(filepath.Join(place, "check-partial.exit"), []byte("0\n"), 0600)
	checks, err = Read(file)
	if err != nil || len(checks) != 2 {
		t.Fatal(err)
	}
	for _, c := range checks {
		if len(c.Args) == 0 && c.Problem == "" {
			t.Fatal("committed corruption became pass")
		}
	}
}

func TestInheritedChecksKeepOriginalProvenanceAndCurrentMetadata(t *testing.T) {
	root, prefix := fakeChecks(t)
	first := filepath.Join(t.TempDir(), "first.jsonl")
	runOwned(t, first, root, root, prefix+"go test ./...")
	old, err := Read(first)
	if err != nil || len(old) != 1 {
		t.Fatal(err)
	}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	if err = Inherit(next, old); err != nil {
		t.Fatal(err)
	}
	saved, err := Read(next)
	if err != nil || len(saved) != 1 || !saved[0].Inherited || saved[0].Source != first || saved[0].ID != old[0].ID || !saved[0].StartedAt.Equal(old[0].StartedAt) || !saved[0].CompletedAt.Equal(*old[0].CompletedAt) {
		t.Fatalf("provenance changed: %+v %v", saved, err)
	}
	status, err := Current(next)
	if err != nil || len(status) != 0 {
		t.Fatal("history advertised as current execution", status, err)
	}
	runOwned(t, next, root, root, prefix+"CHECK_EXIT=7 go test ./...")
	all, err := Read(next)
	if err != nil {
		t.Fatal(err)
	}
	required, _ := RequiredChecks(all, Snapshot{}, false)
	for _, c := range required {
		if c.Exit == nil || *c.Exit != 7 {
			t.Fatal("old pass erased current failure", c)
		}
	}
	status, err = Current(next)
	if err != nil || len(status) != 1 || status[0].Outcome != "failed" || strings.Contains(status[0].Purpose, root) {
		t.Fatal("bad current status", status, err)
	}
}

func TestCurrentChecksPreserveNativeCallIdentity(t *testing.T) {
	root, prefix := fakeChecks(t)
	file := filepath.Join(t.TempDir(), "session.jsonl")
	wrapped, err := PrepareCallIn(file, root, prefix+"go test ./...", "native-call-42")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	checks, err := Read(file)
	if err != nil || len(checks) != 1 || checks[0].CallID != "native-call-42" {
		t.Fatalf("lost native call provenance: %+v %v", checks, err)
	}
	current, err := Current(file)
	if err != nil || len(current) != 1 || current[0].CallID != "native-call-42" || current[0].Outcome != "passed" {
		t.Fatalf("current metadata lost call identity: %+v %v", current, err)
	}
	// Interrupted receipts retain the same provenance; the native executor,
	// not this nullable status, decides whether that call is physically live.
	if err := os.Remove(filepath.Join(file+".checks", checks[0].ID) + ".exit"); err != nil {
		t.Fatal(err)
	}
	current, err = Current(file)
	if err != nil || len(current) != 1 || current[0].CallID != "native-call-42" || current[0].Outcome != "running" {
		t.Fatalf("pending receipt lost call identity: %+v %v", current, err)
	}
	legacy := filepath.Join(t.TempDir(), "legacy.jsonl")
	runOwned(t, legacy, root, root, prefix+"go vet ./...")
	current, err = Current(legacy)
	if err != nil || len(current) != 1 || current[0].CallID != "" {
		t.Fatalf("invented native identity for a legacy receipt: %+v %v", current, err)
	}
}

func TestSnapshotTimeoutKillsItsPipeHoldingChildren(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.Mkdir(bin, 0700)
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 10 &\nwait\n"), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := snapshotReadTimeout
	snapshotReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { snapshotReadTimeout = old })
	started := time.Now()
	_, err := SnapshotAt(root)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("snapshot timeout did not bound children: %v after %s", err, time.Since(started))
	}
}

func TestWorkspacePackageManagerChecksRetainExactSelection(t *testing.T) {
	root, prefix := fakeChecks(t)
	file := filepath.Join(t.TempDir(), "session.jsonl")
	command := "pnpm --config.verify-deps-before-run=false --filter desktop test src/testing/check.test.ts"
	if !CheckCommand(command) {
		t.Fatal("workspace check unrecognized")
	}
	if got := runOwned(t, file, root, root, prefix+"CHECK_EXIT=7 "+command); got != 7 {
		t.Fatal(got)
	}
	checks, err := Read(file)
	if err != nil || len(checks) != 1 || checks[0].Exit == nil || *checks[0].Exit != 7 || checks[0].Args[2] != "--filter" || checks[0].Args[3] != "desktop" {
		t.Fatalf("selection/exits changed: %+v %v", checks, err)
	}
	if CheckCommand("pnpm --filter") || CheckCommand("pnpm --filter desktop exec echo test") {
		t.Fatal("unknown operation became a check")
	}
}
