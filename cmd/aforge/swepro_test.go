package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweproSentinelAnswersOnlyToOne(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"", false},
		{"0", false},
		{"true", false},
		{"yes", false},
		{" 1", false},
	} {
		environ := func(string) string { return testCase.value }
		if got := sweproSentinel(environ); got != testCase.want {
			t.Fatalf("sweproSentinel(%q) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
	if sweproSentinelEnv != "AFORGE_SWEPRO" {
		t.Fatalf("sentinel variable renamed to %q; the subharness and the engine's "+
			"re-exec both depend on this name", sweproSentinelEnv)
	}
}

// buildAforge builds the binary under test. The engine is only reachable
// through a process, so everything below has to go through one.
func buildAforge(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	binary := filepath.Join(t.TempDir(), "aforge")
	build := exec.Command(goTool, "build", "-o", binary, ".")
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build aforge: %v\n%s", buildErr, out)
	}
	return binary
}

// runEngine runs the binary as the engine and returns everything it said.
func runEngine(t *testing.T, binary, workspace string, env []string, argv ...string) string {
	t.Helper()
	command := exec.Command(binary, argv...)
	command.Dir = workspace
	command.Env = append(append(os.Environ(), "AFORGE_SWEPRO=1"), env...)
	out, _ := command.CombinedOutput()
	return string(out)
}

// The proof that the seam is real: build the binary, run it with the sentinel
// set and the argv the engine's auto-resume supervisor re-execs itself with,
// and read codeaf's voice coming out of aforge's binary. The same argv with
// the sentinel cleared must land back in aforge's command switch.
func TestSweproSentinelTurnsTheBinaryIntoTheEngine(t *testing.T) {
	binary := buildAforge(t)
	workspace := t.TempDir()

	// runAutoResume's child argv, verbatim in shape (internal/swepro/codeaf,
	// main.go). Embedded, the supervisor re-execs os.Executable() — this
	// binary — and passes os.Environ() through, so the sentinel rides along
	// and the grandchild is the engine too. That is why the supervisor needed
	// no patch at all, and this is the assertion that says so.
	argv := []string{
		"resume", "--dir", workspace, "--high", "vendor/high",
		"--low", "vendor/low", "--frontier", "vendor/frontier",
		"--variant", "high", "--format", "json",
	}
	out := runEngine(t, binary, workspace, []string{
		"CODEAF_CP_URL=off",
		"PLANDB_DB=" + filepath.Join(workspace, ".plandb.db"),
	}, argv...)
	if !strings.Contains(out, "[codeaf] resume: no resumable checkpoint") {
		t.Fatalf("expected codeaf's voice, got:\n%s", out)
	}

	plain := exec.Command(binary, argv...)
	plain.Dir = workspace
	plain.Env = append(os.Environ(), "AFORGE_SWEPRO=")
	plainOut, _ := plain.CombinedOutput()
	if !strings.Contains(string(plainOut), `unknown command "resume"`) {
		t.Fatalf("without the sentinel the binary must still be aforge, got:\n%s", plainOut)
	}
}

// The control-plane patch (internal/swepro/patches/0002), observed from
// outside the vendored tree — which is the only place aforge is allowed to
// look. "off" must carry the run past the gate; anything else must still be
// refused when no plane answers, because the gate is upstream's and only this
// one value was added to it.
func TestSweproControlPlaneGateIsOffOnlyForOff(t *testing.T) {
	binary := buildAforge(t)
	workspace := t.TempDir()
	argv := []string{"run", "a goal", "--dir", workspace, "--high", "vendor/high"}

	// Getting as far as the key check is the proof: it sits immediately
	// *after* the gate, and is only reached when the gate lets the run pass.
	off := runEngine(t, binary, workspace, []string{
		"CODEAF_CP_URL=off", "OPENROUTER_API_KEY=",
	}, argv...)
	if !strings.Contains(off, "OPENROUTER_API_KEY is not set") {
		t.Fatalf("CODEAF_CP_URL=off did not get past the control-plane gate:\n%s", off)
	}
	if strings.Contains(off, "requires a running AgentField control plane") {
		t.Fatalf("CODEAF_CP_URL=off still probed a control plane:\n%s", off)
	}

	// Port 1 is never listening; the gate must still refuse.
	on := runEngine(t, binary, workspace, []string{
		"CODEAF_CP_URL=http://127.0.0.1:1", "OPENROUTER_API_KEY=",
	}, argv...)
	if !strings.Contains(on, "requires a running AgentField control plane") {
		t.Fatalf("the gate stopped applying to a real URL:\n%s", on)
	}
}
