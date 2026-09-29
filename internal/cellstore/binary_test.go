package cellstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// fakeEngineBinary writes a script that answers every call with help text.
func fakeEngineBinary(t *testing.T, help string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "furrow")
	script := "#!/bin/sh\ncat <<'EOF'\n" + help + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbeAcceptsAnEngineThatKnowsTheSealFlag(t *testing.T) {
	if err := probe(fakeEngineBinary(t, "Usage: hook turn-end [--cell-dir <DIR>]")); err != nil {
		t.Fatal(err)
	}
}

func TestProbeNamesAnEngineWithoutTheSealFlag(t *testing.T) {
	err := probe(fakeEngineBinary(t, "Usage: hook turn-end"))
	if err == nil || !strings.Contains(err.Error(), sealFlag) {
		t.Fatalf("want an error naming %s, got %v", sealFlag, err)
	}
}

func TestSealingNeverFallsBackToPath(t *testing.T) {
	dir := filepath.Dir(fakeEngineBinary(t, "Usage: --cell-dir"))
	t.Setenv("PATH", dir)
	t.Setenv(furrow.BinaryEnvVar, "")
	bin, err := sealingEngine()
	if err == nil && filepath.Dir(bin) == dir {
		t.Fatalf("sealed with the PATH engine %s", bin)
	}
}

func TestSealingUsesAConfiguredEngine(t *testing.T) {
	bin := fakeEngineBinary(t, "Usage: --cell-dir")
	t.Setenv(furrow.BinaryEnvVar, bin)
	got, err := sealingEngine()
	if err != nil || got != bin {
		t.Fatalf("got %q, %v; want %q", got, err, bin)
	}
}

func TestSealingRefusesAConfiguredEngineThatCannotSeal(t *testing.T) {
	t.Setenv(furrow.BinaryEnvVar, fakeEngineBinary(t, "Usage: hook turn-end"))
	if _, err := sealingEngine(); err == nil {
		t.Fatal("a stale engine was accepted")
	}
}
