package furrowbin

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/env"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestK1NativeExtractionAndProbe proves K1 without relying on executable mode
// bits: the engine has a Windows suffix, runs directly, and advertises timelines.
func TestK1NativeExtractionAndProbe(t *testing.T) {
	bin, configured, err := ConfiguredBinary()
	if err != nil || !configured {
		t.Fatalf("K1 needs the runner's native engine: %v", err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Compress(data)
	if err != nil {
		t.Fatal(err)
	}
	path, err := extractInto(t.TempDir(), "k1", archive)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".exe" || !installed(path) {
		t.Fatalf("K1 invalid extracted engine %q", path)
	}
	if !probeCells(path) {
		t.Fatal("K1 native engine did not satisfy the timeline probe")
	}
}

// TestK1ShippedWindowsBinaryExtractsItsEngine proves K1 through the built CLI,
// with no explicit engine override. The native runner stages this artifact for
// release acceptance; ordinary source checkouts have no candidate to run.
func TestK1ShippedWindowsBinaryExtractsItsEngine(t *testing.T) {
	candidate := filepath.Join("..", "..", ".wintest", "codeaf-embedded.exe")
	data, err := os.ReadFile(candidate)
	if os.IsNotExist(err) {
		t.Skip("K1 shipped-binary check needs make build's Windows artifact")
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "codeaf.exe")
	if err := os.WriteFile(executable, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "identity", "show") //codeaf:plumbing K1 exercises the shipped native capability door.
	command.Env = append(os.Environ(), "CODEAF_HOME="+dir, "CODEAF_FURROW=", env.Legacy("CODEAF_FURROW")+"=", "CODEAF_SYNC_URL=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("K1 embedded engine identity door: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "fingerprint") {
		t.Fatalf("K1 identity door did not run: %s", output)
	}
	engines, err := filepath.Glob(filepath.Join(dir, "bin", "furrow-*.exe"))
	if err != nil || len(engines) != 1 {
		t.Fatalf("K1 extracted engines: %v %v", engines, err)
	}
	if !probeCells(engines[0]) {
		t.Fatal("K1 embedded engine lacks timelines")
	}
}
