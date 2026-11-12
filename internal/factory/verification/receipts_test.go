package verification

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fakeChecks(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go", "pytest", "npm", "pnpm", "yarn", "python", "python3", "pip-audit", "trivy", "cargo", "govulncheck", "golangci-lint"} {
		script := "#!/bin/sh\nprintf '%s\\n' '--- PASS: TestA (0s)' PASS\nexit \"${CHECK_EXIT:-0}\"\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root, "export PATH=" + quote(bin) + ":\"$PATH\"; "
}

func runObserved(t *testing.T, transcript, cwd, command string) (string, int) {
	t.Helper()
	wrapped, err := Prepare(transcript, command)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return string(output), e.ExitCode()
	}
	t.Fatal(err)
	return "", -1
}

func TestCheckExitIsIndependentOfReportingPipeline(t *testing.T) {
	cases := []struct {
		name, command        string
		shellExit, checkExit int
	}{
		{"zero failures", "go test -v ./... | grep -c -e '--- FAIL:'", 1, 0},
		{"broken reporter", "go test -v ./... | (cat >/dev/null; exit 2)", 2, 0},
		{"real failure hidden by tail", "CHECK_EXIT=7 go test -v ./... | tail -2", 7, 7},
		{"failure hidden by explicit shell success", "CHECK_EXIT=7 go test -v ./... || true", 0, 7},
		{"quiet vet", "go vet ./... >/dev/null", 0, 0},
		{"pytest", "pytest", 0, 0},
		{"python pytest", "python3 -m pytest tests", 0, 0},
		{"npm run test", "npm run test -- --runInBand", 0, 0},
		{"nested bash", "bash -c 'go test ./...'", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, prefix := fakeChecks(t)
			transcript := filepath.Join(root, "session.jsonl")
			out, exit := runObserved(t, transcript, root, prefix+tc.command)
			if exit != tc.shellExit {
				t.Fatalf("shell exit %d want %d: %s", exit, tc.shellExit, out)
			}
			checks, err := Read(transcript)
			if err != nil || len(checks) != 1 {
				t.Fatalf("checks=%+v err=%v", checks, err)
			}
			if checks[0].Exit == nil || *checks[0].Exit != tc.checkExit {
				t.Fatalf("check exit %+v", checks[0])
			}
		})
	}
}

func TestCheckIdentityPreservesScopeAndRerunOrder(t *testing.T) {
	root, prefix := fakeChecks(t)
	transcript := filepath.Join(root, "session.jsonl")
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	commands := []string{
		"CHECK_EXIT=7 go test -v ./... | tail -1",
		"go test -v -run TestA ./...",
		"cd nested && go test -v ./...",
		"GOOS=linux go test -v ./...",
		"go test -v ./... | grep -c -e '--- FAIL:'",
	}
	for _, command := range commands {
		runObserved(t, transcript, root, prefix+command)
	}
	checks, err := Read(transcript)
	if err != nil || len(checks) != 5 {
		t.Fatalf("checks=%+v err=%v", checks, err)
	}
	first := checks[0].Key()
	for _, i := range []int{1, 2, 3} {
		if checks[i].Key() == first {
			t.Fatalf("different scope %d cleared full check", i)
		}
	}
	if checks[4].Key() != first || *checks[0].Exit != 7 || *checks[4].Exit != 0 {
		t.Fatalf("corrected check did not preserve identity/order: %+v", checks)
	}
}

func TestIncompleteCheckCannotBecomePassAndLiteralIsNotACheck(t *testing.T) {
	root, prefix := fakeChecks(t)
	transcript := filepath.Join(root, "session.jsonl")
	runObserved(t, transcript, root, prefix+"printf '%s' 'go test ./...'")
	checks, err := Read(transcript)
	if err != nil || len(checks) != 0 {
		t.Fatalf("literal became a check: %+v %v", checks, err)
	}
	runObserved(t, transcript, root, prefix+"go test ./...")
	files, err := filepath.Glob(transcript + ".checks/call-*/check-*.exit")
	if err != nil || len(files) != 1 {
		t.Fatalf("status files: %v %v", files, err)
	}
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}
	checks, err = Read(transcript)
	if err != nil || len(checks) != 1 || checks[0].Exit != nil {
		t.Fatalf("missing exit became pass: %+v %v", checks, err)
	}
}

func TestQuotedPathsAndArgumentsStayData(t *testing.T) {
	root, prefix := fakeChecks(t)
	transcript := filepath.Join(root, "a 'quoted' session.jsonl")
	arg := "Test space | $(echo unsafe)"
	runObserved(t, transcript, root, prefix+"go test -run "+quote(arg)+" ./...")
	checks, err := Read(transcript)
	if err != nil || len(checks) != 1 || checks[0].Args[3] != arg {
		t.Fatalf("arguments: %+v %v", checks, err)
	}
	if !strings.Contains(checks[0].Label(), "Test space") {
		t.Fatal(checks[0].Label())
	}
}

func TestOnlyOutputPresentationMayDiscountShellFailure(t *testing.T) {
	for _, command := range []string{"go test ./...", "cd nested && GOPROXY=direct go test -v ./... 2>&1 | grep -c -e '--- FAIL:'", "go vet ./... | tail -20", "export PATH='/a path':\"$PATH\"; go test ./... | tail -2"} {
		if !PresentationOnly(command) {
			t.Errorf("ordinary check/formatter refused: %q", command)
		}
	}
	for _, command := range []string{"go test ./... && npm run build", "go test ./... && git diff --check", "go test ./... | tail -2; false", "go test ./... || true", "go test ./... | bash", "go test ./... | grep x && go vet ./...", "go test $(cat packages)", "printf 'go test ./...'", "go test ./... | (cat; exit 2)"} {
		if PresentationOnly(command) {
			t.Errorf("compound script treated as reporting: %q", command)
		}
	}
}

func TestVerboseOutputDoesNotChangeCheckIdentityOrEraseScope(t *testing.T) {
	check := Check{Cwd: "/repo", Args: []string{"go", "test", "./...", "-count=1", "-v"}}
	plain := check
	plain.Args = []string{"go", "test", "./...", "-count=1"}
	if check.Key() != plain.Key() {
		t.Fatal("output verbosity changed check identity")
	}
	for _, args := range [][]string{
		{"go", "test", "./...", "-count=1", "-race"},
		{"go", "test", "./...", "-count=1", "-run", "TestA"},
		{"go", "test", "./nested", "-count=1"},
		{"go", "test", "./...", "-count=1", "-args", "-v"},
	} {
		other := check
		other.Args = args
		if other.Key() == check.Key() {
			t.Fatalf("different scope cleared check: %v", args)
		}
	}
	selector := Check{Cwd: "/repo", Args: []string{"go", "test", "-run", "-v", "./..."}}
	other := selector
	other.Args = []string{"go", "test", "-run", "./..."}
	if selector.Key() == other.Key() {
		t.Fatal("a regex value was mistaken for a verbosity flag")
	}
}

// Exercise the non-Bash branch even on macOS, whose /bin/sh may itself be Bash.
func TestObserverPreservesShFallbackWithoutExportedFunctions(t *testing.T) {
	for _, want := range []int{0, 7} {
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			root, prefix := fakeChecks(t)
			transcript := filepath.Join(root, "session.jsonl")
			wrapped, err := Prepare(transcript, prefix+"CHECK_EXIT="+strconv.Itoa(want)+" go test ./...")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-c", "unset BASH_VERSION; "+wrapped)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			got := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					got = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if got != want || !strings.Contains(string(output), "PASS") {
				t.Fatalf("sh command changed: exit %d want %d, output %s", got, want, output)
			}
			checks, err := Read(transcript)
			if err != nil || len(checks) != 0 {
				t.Fatalf("sh invented observed checks: %+v, %v", checks, err)
			}
		})
	}
}

func TestOriginalCommandRoundTripAndOrdinaryCommands(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	for _, original := range []string{"go test ./... | tail -20", "printf '%s\\n' 'quotes \" and spaces'\ngo vet ./...", "npm run test -- --grep='a|b'"} {
		wrapped, err := Prepare(transcript, original)
		if err != nil {
			t.Fatal(err)
		}
		if got := OriginalCommand(wrapped); got != original {
			t.Fatalf("display lost original command: %q want %q", got, original)
		}
		if !strings.HasSuffix(wrapped, original) {
			t.Fatal("observer changed executable command body")
		}
		if got := OriginalCommand(original); got != original {
			t.Fatalf("ordinary command changed: %q", got)
		}
	}
	for _, original := range []string{originalCommandPrefix + "not-base64\n" + observerShellGuard + "\n", originalCommandPrefix + "Z28gdGVzdA==", originalCommandPrefix + "Z28gdGVzdA==\nprintf 'ordinary comment'"} {
		if got := OriginalCommand(original); got != original {
			t.Fatalf("malformed metadata changed command: %q", got)
		}
	}
}

func TestAuditExitsCannotBeHiddenByLogsOrShellSuccess(t *testing.T) {
	for _, command := range []string{"npm audit --omit=dev --audit-level=high | tail -1", "pip-audit || true", "python3 -m pip_audit | cat", "pnpm audit || true", "npm run lint | tail -1"} {
		t.Run(command, func(t *testing.T) {
			root, prefix := fakeChecks(t)
			file := filepath.Join(t.TempDir(), "transcript.jsonl")
			runObserved(t, file, root, prefix+"CHECK_EXIT=7 "+command)
			checks, err := Read(file)
			if err != nil || len(checks) != 1 || checks[0].Exit == nil || *checks[0].Exit != 7 {
				t.Fatalf("lost check failure: %+v %v", checks, err)
			}
			if !CheckCommand(command) {
				t.Fatal("collector vocabulary disagrees with observer")
			}
		})
	}
	if CheckCommand("printf '%s' 'npm audit'") {
		t.Fatal("quoted mention became an executable check")
	}
}
func TestVerificationSnapshotTracksHeadTrackedAndUntrackedContentsWithoutChangingIndex(t *testing.T) {
	root, prefix := fakeChecks(t)
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return string(b)
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	runObserved(t, file, root, prefix+"npm audit")
	checks, err := Read(file)
	if err != nil || len(checks) != 1 {
		t.Fatalf("checks: %v %v", checks, err)
	}
	at, err := SnapshotAt(root)
	if err != nil || !checks[0].Fresh(at) {
		t.Fatalf("fresh receipt: %+v %+v %v", checks[0], at, err)
	}
	if e := os.WriteFile(filepath.Join(root, "new-test.txt"), []byte("first"), 0600); e != nil {
		t.Fatal(e)
	}
	at2, err := SnapshotAt(root)
	if err != nil || checks[0].Fresh(at2) {
		t.Fatal("new test did not stale verification")
	}
	before := git("diff", "--cached", "--stat")
	if before != "" {
		t.Fatal("taking a snapshot staged files")
	}
	if e := os.WriteFile(filepath.Join(root, "new-test.txt"), []byte("second"), 0600); e != nil {
		t.Fatal(e)
	}
	at3, _ := SnapshotAt(root)
	if at2 == at3 {
		t.Fatal("untracked content change was lost")
	}
	git("add", ".")
	git("-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "new head")
	at4, _ := SnapshotAt(root)
	if at4.Head == at.Head || checks[0].Fresh(at4) {
		t.Fatal("head change reused stale verification")
	}
}

func TestCheckThatChangesCheckoutCannotVerifyItsOwnEdits(t *testing.T) {
	root, prefix := fakeChecks(t)
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "bin", "go")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf changed > source.txt\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	runObserved(t, file, root, prefix+"go test ./...")
	checks, err := Read(file)
	if err != nil || len(checks) != 1 {
		t.Fatalf("checks: %v %v", checks, err)
	}
	at, err := SnapshotAt(root)
	if err != nil || checks[0].Before == checks[0].After || checks[0].Fresh(at) {
		t.Fatalf("mutating check accepted: %+v %v", checks[0], err)
	}
}
