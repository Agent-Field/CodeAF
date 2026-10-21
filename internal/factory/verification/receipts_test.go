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
	for _, name := range []string{"go", "pytest", "npm", "python3"} {
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
