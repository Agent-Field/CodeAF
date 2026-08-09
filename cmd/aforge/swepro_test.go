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
		t.Fatalf("sentinel variable renamed to %q; the harness and the engine's "+
			"re-exec both depend on this name", sweproSentinelEnv)
	}
}

// The proof that the seam is real: build the binary, run it with the sentinel
// set and the argv the engine's auto-resume supervisor re-execs itself with,
// and read codeaf's voice coming out of aforge's binary. The same argv with
// the sentinel cleared must land back in aforge's command switch.
func TestSweproSentinelTurnsTheBinaryIntoTheEngine(t *testing.T) {
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

	workspace := t.TempDir()
	// runAutoResume's child argv, verbatim in shape. CODEAF_CP_URL=off keeps
	// the run off any control plane (the aforge-embed patch); no checkpoint
	// exists in a fresh directory, so the engine says so and exits 0.
	argv := []string{
		"resume", "--dir", workspace, "--high", "vendor/high",
		"--low", "vendor/low", "--frontier", "vendor/frontier",
		"--variant", "high", "--format", "json",
	}
	environ := append(os.Environ(),
		"AFORGE_SWEPRO=1", "CODEAF_CP_URL=off",
		"PLANDB_DB="+filepath.Join(workspace, ".plandb.db"),
	)

	engine := exec.Command(binary, argv...)
	engine.Env = environ
	engine.Dir = workspace
	engineOut, engineErr := engine.CombinedOutput()
	if engineErr != nil {
		t.Fatalf("sentinel run failed: %v\n%s", engineErr, engineOut)
	}
	if !strings.Contains(string(engineOut), "[codeaf] resume: no resumable checkpoint") {
		t.Fatalf("expected codeaf's voice, got:\n%s", engineOut)
	}

	plain := exec.Command(binary, argv...)
	plain.Env = append(os.Environ(), "AFORGE_SWEPRO=")
	plain.Dir = workspace
	plainOut, _ := plain.CombinedOutput()
	if !strings.Contains(string(plainOut), `unknown command "resume"`) {
		t.Fatalf("without the sentinel the binary must still be aforge, got:\n%s", plainOut)
	}
}
