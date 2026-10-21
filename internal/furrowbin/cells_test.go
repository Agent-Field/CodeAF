package furrowbin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestC12EngineContract proves C12 by withholding each required verb and each
// changed-path sealing flag, independently of any version string.
func TestC12EngineContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("furrow runs through WSL")
	}
	for _, missing := range []string{"export", "published", "want", "import", "rebind", "materialize", "serve", "--changed", "--changed-stdin", "--cell-dir", ""} {
		t.Run("missing="+missing, func(t *testing.T) {
			program := filepath.Join(t.TempDir(), "furrow")
			help := "--changed PATH --changed-stdin --cell-dir DIR"
			if strings.HasPrefix(missing, "--") {
				help = strings.ReplaceAll(help, missing, "")
			}
			script := "#!/bin/sh\nif [ \"$1\" = \"" + missing + "\" ]; then exit 2; fi\nprintf '%s\\n' '" + help + "'\n"
			if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			if got := probeCells(program); got != (missing == "") {
				t.Fatalf("engine missing %q: available = %v", missing, got)
			}
		})
	}
}

// TestC12ProbeIsCached proves C12's once-per-process rule at the public door:
// changing the selected executable after detection cannot start another probe.
func TestC12ProbeIsCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("furrow runs through WSL")
	}
	old := cellsAvailable
	t.Cleanup(func() { cellsAvailable = old })
	program := filepath.Join(t.TempDir(), "old-furrow")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_FURROW", program)
	probes := 0
	cellsAvailable = sync.OnceValue(func() bool {
		probes++
		binary, err := cellsBinary()
		return err == nil && probeCells(binary)
	})
	if CellsAvailable() {
		t.Fatal("old engine offered moving chats")
	}
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\n' '--changed PATH --changed-stdin --cell-dir DIR'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if CellsAvailable() || probes != 1 {
		t.Fatalf("probe ran %d times or changed its answer", probes)
	}
}

// TestC12RealEngineCapabilities proves C12 against the provided engine, whose
// 0.1.0 version string must not cause a false refusal.
func TestC12RealEngineCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("furrow runs through WSL")
	}
	binary, err := cellsBinary()
	if err != nil {
		t.Skipf("no local engine: %v", err)
	}
	if !probeCells(binary) {
		t.Fatal("provided furrow lacks the cell contract")
	}
}

// TestC12ExplicitEngineNeverFallsBack proves C12 when a configured path cannot
// run, even if this test binary carries a compatible embedded engine.
func TestC12ExplicitEngineNeverFallsBack(t *testing.T) {
	for _, path := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		t.Setenv(BinaryEnvVar, path)
		if binary, err := cellsBinary(); err == nil || binary != "" {
			t.Fatalf("configured %q fell through to %q: %v", path, binary, err)
		}
	}
}
