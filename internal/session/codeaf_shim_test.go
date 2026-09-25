package session

// A WORKER'S `codeaf` IS THE CODEAF THAT IS RUNNING. The bash worker is taught
// to edit with `codeaf patch` (prompts/bashworker.md), and the only PATH entry
// the harness gave it was the run's `plandb` shim — so on any install whose
// file is not named codeaf (devaf, stageaf, a `--name` word) the call resolved
// on the machine's own PATH: `command not found` on a clean machine, and on the
// fresh-install check of 2026-09-25 an OLDER codeaf that answered `error: there
// is no \`codeaf patch\`` three times. These tests stand a stale `codeaf` that
// prints WRONG first on the process PATH and drive the real harness PATH setup
// ([NewBeltWorker], the run engine's worker seat, through the belt's own bash).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// writeScript writes one executable shell script and answers its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// staleCodeafFirst puts an executable named codeaf that prints WRONG at the
// FRONT of the process PATH — the older codeaf that answered on the check
// machine — and answers its directory.
func staleCodeafFirst(t *testing.T, probeAnswers bool) string {
	t.Helper()
	dir := t.TempDir()
	body := "echo WRONG; exit 3"
	if probeAnswers {
		// A codeaf that answers the plan CLI's probe but is not this process:
		// the one shape [resolvePlanCLI] may still settle on for `plandb`.
		body = `if [ "$1" = plandb ]; then exit 0; fi` + "\necho WRONG; exit 3"
	}
	writeScript(t, dir, "codeaf", body)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// runningCodeafAs stands in for the running binary under another file name,
// registered the way cmd/codeaf registers itself ([SetRunningCLI]). It answers
// the plan CLI's probe and says RIGHT with its arguments for everything else.
func runningCodeafAs(t *testing.T, name string) string {
	t.Helper()
	path := writeScript(t, t.TempDir(), name, `if [ "$1" = plandb ]; then exit 0; fi`+"\necho RIGHT \"$@\"")
	previous := runningCLI
	SetRunningCLI(path)
	t.Cleanup(func() { runningCLI = previous })
	return path
}

// beltBash answers the belt's own bash hand on a worker.
func beltBash(t *testing.T, agent *Agent) bare.Tool {
	t.Helper()
	for _, tool := range agent.beltTools() {
		if tool.Name == "bash" {
			return tool
		}
	}
	t.Fatal("the worker's belt carries no bash hand")
	return bare.Tool{}
}

// runBeltBash runs one command through the worker's bash hand and answers its
// text whether or not the command failed.
func runBeltBash(t *testing.T, bash bare.Tool, command string) string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := bash.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("the belt's bash refused %q: %v", command, err)
	}
	return text
}

// Contract 1.1-1.4: whatever the running binary's file is called, and whatever
// else is on PATH first, a worker's `codeaf …` reaches the running binary — as
// the first word, after a `cd`, and when asked where `codeaf` is.
func TestAWorkersCodeafIsTheRunningBinaryWhateverItIsNamed(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, "")
	staleCodeafFirst(t, true)
	runningCodeafAs(t, "devaf")

	agent := newRunBeltWorker(t, effort.None)
	bash := beltBash(t, agent)

	first := runBeltBash(t, bash, "codeaf patch notes.md --old a --new b")
	if strings.Contains(first, "WRONG") || !strings.Contains(first, "RIGHT patch notes.md --old a --new b") {
		t.Fatalf("a worker's `codeaf patch` reached %q, want the running devaf", first)
	}
	chained := runBeltBash(t, bash, "mkdir -p sub && cd sub && codeaf patch ../notes.md --old a --new b")
	if strings.Contains(chained, "WRONG") || !strings.Contains(chained, "RIGHT patch ../notes.md") {
		t.Fatalf("a `codeaf` after a cd reached %q, want the running devaf", chained)
	}
	where := strings.TrimSpace(runBeltBash(t, bash, "command -v codeaf"))
	if !strings.HasSuffix(where, string(filepath.Separator)+filepath.Join("bin", "codeaf")) || strings.Contains(where, "WRONG") {
		t.Fatalf("command -v codeaf answered %q, want the run's own bin/codeaf", where)
	}
}

// Contract 1.5: when nothing names the running codeaf — a driver that answers
// only the plan CLI, or a test binary — the worker's `codeaf` refuses in one
// line and NEVER falls through to whatever codeaf the machine's PATH holds.
func TestAWorkersCodeafRefusesRatherThanReachAnotherCodeaf(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, "")
	staleCodeafFirst(t, true)
	previous := runningCLI
	runningCLI = ""
	t.Cleanup(func() { runningCLI = previous })

	agent := newRunBeltWorker(t, effort.None)
	bash := beltBash(t, agent)

	said := runBeltBash(t, bash, "codeaf patch notes.md --old a --new b; echo exit=$?")
	if strings.Contains(said, "WRONG") {
		t.Fatalf("with no running codeaf named, the worker reached the machine's own codeaf: %q", said)
	}
	if !strings.Contains(said, codeafShimRefusal) || !strings.Contains(said, "exit=127") {
		t.Fatalf("the worker's codeaf answered %q, want the refusal and exit 127", said)
	}
}

// Contract 1.x, the landing's side: the `codeaf` shim is the harness's own file,
// exactly as the `plandb` shim beside it is. A run whose store sits inside the
// working copy writes both into that copy's bin/, and a landing that counted
// bin/codeaf as the work would report a change nobody asked for — or keep a
// branch for it.
func TestTheCodeafShimIsTheHarnesssOwnFileNotTheWork(t *testing.T) {
	for _, path := range []string{"bin/" + planShimFilename, "bin/" + codeafShimFilename} {
		if !harnessWrote(path) {
			t.Fatalf("%s is counted as the run's work, want it read as the harness's own shim", path)
		}
	}
	if harnessWrote("bin/devaf") {
		t.Fatal("a bin/devaf the work wrote was taken for a harness shim")
	}
}
