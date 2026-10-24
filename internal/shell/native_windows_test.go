//go:build windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// N2 proves a real Windows process hands Git Bash one parse, including multiline
// text and every character that the WSL launcher used to expand prematurely.
func TestWindowsSingleParse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bash, err := Bash()
	if err != nil {
		t.Skipf("Git Bash is required for native shell tests: %v", err)
	}
	literal := "double=\" single=' backslash=\\ percent=%PATH% substitution=$(echo WRONG) héllo ✓"
	command := "export PATH='/c/Program Files (x86)/test':$PATH\nx=inner; echo \"x=[$x]\"\nprintf 'second line\\n'\ncat <<'CODEAF_LITERAL'\n" + literal + "\nCODEAF_LITERAL\n"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-lc", command)
	cmd.Dir = t.TempDir()
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != "x=[inner]\nsecond line\n"+literal+"\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// N1 proves an explicit bad choice refuses and changing the override
// invalidates the cached answer, rather than silently falling through to PATH.
func TestWindowsOverrideCacheAndRefusal(t *testing.T) {
	bash, err := Bash()
	if err != nil {
		t.Skipf("Git Bash is required for cache reset proof: %v", err)
	}
	t.Setenv(env.Legacy("CODEAF_BASH"), bash)
	t.Setenv("CODEAF_BASH", "")
	if got, err := Bash(); got != bash || err != nil {
		t.Fatalf("owned-variable fallback: %q %v", got, err)
	}
	t.Setenv("CODEAF_BASH", bash)
	if got, err := Bash(); err != nil || got != bash {
		t.Fatalf("explicit bash: %q %v", got, err)
	}
	missing := filepath.Join(t.TempDir(), "missing bash.exe")
	t.Setenv("CODEAF_BASH", missing)
	if got, err := Bash(); got != "" || err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "CODEAF_BASH") {
		t.Fatalf("missing override: %q %v", got, err)
	}
	t.Setenv("CODEAF_BASH", filepath.Join(os.Getenv("SystemRoot"), "System32", "bash.exe"))
	if got, err := Bash(); got != "" || err == nil || !strings.Contains(err.Error(), "CODEAF_BASH") {
		t.Fatalf("WSL override: %q %v", got, err)
	}
	t.Setenv("CODEAF_BASH", bash)
	if got, err := Bash(); err != nil || got != bash {
		t.Fatalf("restored bash: %q %v", got, err)
	}
	// R4 keeps both route remedies in the Windows-only shell fact.
	if fact := PromptFact(); !strings.Contains(fact, bash) || !strings.Contains(fact, "/c/…") || !strings.HasSuffix(fact, PathConversionFact) {
		t.Fatalf("C8 shell fact: %q", fact)
	}
}

// N2 keeps cwd, long output and exit status through the real native bash door.
func TestWindowsCwdLongOutputAndExit(t *testing.T) {
	bash, err := Bash()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("cwd is right"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", "cat marker; printf '\\n'; for ((n=0;n<12000;n++)); do printf 'long output %05d\\n' \"$n\"; done; exit 23")
	cmd.Dir = dir
	body, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 23 || err == nil || !strings.HasPrefix(string(body), "cwd is right\n") || !strings.HasSuffix(string(body), "long output 11999\n") || strings.Count(string(body), "long output ") != 12000 {
		t.Fatalf("exit/cwd/output: bytes=%d, state=%v, err=%v", len(body), cmd.ProcessState, err)
	}
}
