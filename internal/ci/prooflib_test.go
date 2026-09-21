package ci

// THE PROOF LIBRARY'S OWN FAILING ARM.
//
// `scripts/proof.sh` exists because three proof steps were written in a shape
// that could not report failure, and every test here runs its function TWICE:
// once where the thing it checks is sound, and once where it is broken.
//
// THE SECOND ARM IS THE ACCEPTANCE. A guard against invisible failure that has
// only ever been seen passing is the thing it was written to prevent, wearing
// its own name. The first arm only shows the function runs.
//
// Each broken arm also pins WHAT THE PLAIN TOOL SAYS about the same situation,
// because that is the whole argument for the function existing: in all three
// cases the obvious command reports success.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sourcing runs one line of shell with the library sourced, in the directory
// given, with its logs kept inside the test's own directory so two tests cannot
// read each other's. The directory matters for `run_tests`, which asks `go test`
// about a package and must therefore stand in that package's own module.
func sourcing(t *testing.T, dir, line string) (string, int) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("finding the repository root: %v", err)
	}
	cmd := exec.Command("bash", "-c", `source "`+filepath.Join(root, "scripts", "proof.sh")+`"; `+line)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PROOF_LOG_DIR="+t.TempDir(), "PROOF_TEST_TIMEOUT=2m")
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running the library: %v", err)
	}
	return string(out), code
}

// aPackageOfTwoTests is a throwaway module holding exactly TestAlpha and
// TestBeta, which is the smallest thing a filter can drift away from.
func aPackageOfTwoTests(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	write("go.mod", "module proofprobe\n\ngo 1.21\n")
	write("probe_test.go", "package probe\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {}\n\nfunc TestBeta(t *testing.T) {}\n")
	return dir
}

// A FILTER THAT HAS DRIFTED FROM THE FILE IS CAUGHT, AND THE PLAIN COMMAND SAYS
// `ok`. This is the acceptance of the whole change: `go test` answers a pattern
// matching nothing with success, because from its side nothing failed, so a
// named test that never ran reads exactly like one that passed.
func TestRunTestsCatchesANameThatReportedNothing(t *testing.T) {
	dir := aPackageOfTwoTests(t)

	// What the obvious command says about the same situation.
	plain := exec.Command("go", "test", "-count=1", "-run", "^(TestAlpha|TestGamma)$", ".")
	plain.Dir = dir
	if out, err := plain.CombinedOutput(); err != nil {
		t.Fatalf("the plain command failed, so this test is no longer about a silent pass: %v\n%s", err, out)
	} else if !strings.Contains(string(out), "ok") {
		t.Fatalf("the plain command did not report ok on a filter naming a test that does not exist:\n%s", out)
	}

	out, code := sourcing(t, dir, "run_tests . TestAlpha TestGamma")
	if code == 0 {
		t.Fatalf("the library agreed with the plain command and reported success:\n%s", out)
	}
	if !strings.Contains(out, "asked=2  reported=1") {
		t.Fatalf("the failure does not say the count on both sides:\n%s", out)
	}
	if !strings.Contains(out, "TestGamma") {
		t.Fatalf("the failure does not name the test that reported nothing, so nobody can act on it:\n%s", out)
	}
	// AND IT DOES NOT NAME THE ONE THAT DID RUN, which is what keeps the list
	// readable when a chain asks for thirty names and one has gone.
	if strings.Contains(out, "  TestAlpha") {
		t.Fatalf("the failure lists a test that ran as one that did not:\n%s", out)
	}
}

// AND IT PASSES WHEN EVERY NAME RAN, including when one of them is a table with
// subtests, whose indented lines must not be counted as names of their own.
func TestRunTestsPassesWhenEveryNameRan(t *testing.T) {
	dir := aPackageOfTwoTests(t)
	out, code := sourcing(t, dir, "run_tests . TestAlpha TestBeta")
	if code != 0 {
		t.Fatalf("two names that both ran were reported as drift:\n%s", out)
	}
	if !strings.Contains(out, "asked=2  reported=2") {
		t.Fatalf("the passing line does not say both counts:\n%s", out)
	}
}

// A FILE GOFMT WOULD REWRITE IS CAUGHT, AND THE PLAIN COMMAND EXITS 0. `gofmt
// -l` prints the files it would reformat and its exit code says nothing about
// whether it printed any.
func TestFmtCheckFailsOnAFileGofmtWouldRewrite(t *testing.T) {
	dir := t.TempDir()
	bent := filepath.Join(dir, "bent.go")
	if err := os.WriteFile(bent, []byte("package p\n\nfunc F()  {\n\tx :=1\n\t_ = x\n}\n"), 0o644); err != nil {
		t.Fatalf("writing the unformatted file: %v", err)
	}

	plain := exec.Command("gofmt", "-l", dir)
	out, err := plain.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt itself failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "bent.go") {
		t.Fatalf("gofmt does not consider the file unformatted, so this test proves nothing:\n%s", out)
	}
	// The exit code, which is the thing a step must not read: it is 0 here.

	got, code := sourcing(t, dir, "fmt_check .")
	if code == 0 {
		t.Fatalf("an unformatted tree was reported clean:\n%s", got)
	}
	if !strings.Contains(got, "bent.go") || !strings.Contains(got, "gofmt=DIRTY") {
		t.Fatalf("the failure does not print the list gofmt found:\n%s", got)
	}
}

func TestFmtCheckPassesOnAFormattedTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fine.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("writing the formatted file: %v", err)
	}
	out, code := sourcing(t, dir, "fmt_check .")
	if code != 0 || !strings.Contains(out, "gofmt=clean") {
		t.Fatalf("a formatted tree was reported dirty (%d):\n%s", code, out)
	}
}

// A STEP REPORTS ITS OWN COMMAND'S VERDICT AND NOT A READER'S. The shape this
// replaces ends in a pipe, and a pipe hands back the last command's status: the
// reader of a failure, which succeeds at reading it.
func TestStepReportsTheCommandsOwnStatusAndNotAPipesStatus(t *testing.T) {
	// The situation, in one line: a command that fails loudly and is then read.
	pipe := exec.Command("bash", "-c", `bash -c 'echo "--- FAIL: TestX"; exit 3' 2>&1 | tail -5; echo "exit=$?"`)
	out, err := pipe.CombinedOutput()
	if err != nil {
		t.Fatalf("the pipeline itself failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "exit=0") {
		t.Fatalf("the pipeline no longer reports the reader's status, so this test is about nothing:\n%s", out)
	}

	got, code := sourcing(t, t.TempDir(), `step probe bash -c 'echo "--- FAIL: TestX"; exit 3'`)
	if code != 3 {
		t.Fatalf("step answered %d, not the command's own 3:\n%s", code, got)
	}
	if !strings.Contains(got, "probe_exit=3") {
		t.Fatalf("the line a person reads does not carry the command's status:\n%s", got)
	}
	// AND THE FAILURES ARE COUNTED FROM THE OUTPUT, which is the witness that
	// catches the case where the status itself is wrong.
	if !strings.Contains(got, "probe_FAIL_lines=1") {
		t.Fatalf("the failures in the output were not counted:\n%s", got)
	}
}
