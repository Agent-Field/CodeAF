//go:build linux || darwin

package executor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// A FENCED CHECK WRITES ITS RECEIPT. Task A of the third live run stalled at
// Test: every `go test` its step ran was wrapped to record a receipt beside
// the step's transcript, which lives in codeaf's home, outside the job's
// checkout, and the fence refused the receipt's mktemp ("Operation not
// permitted", exit 125) — so no check ever ran and none was ever green. It
// passed in the round before only because that home sat under a folder the
// fence keeps writable anyway. The receipt folder is the step's own and is
// writable inside the fence; the rest of codeaf's home is not.
func TestAFencedCheckWritesItsReceiptOutsideTheCheckout(t *testing.T) {
	if executor.FenceLimitLine() == executor.FenceDegradedLine {
		t.Skip("this system cannot confine commands")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	base, err := os.MkdirTemp(".", ".fence-receipts-")
	if err != nil {
		t.Fatal(err)
	}
	if base, err = filepath.Abs(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	root := filepath.Join(base, "work", "repo")
	codeafHome := filepath.Join(base, "home", "codeaf")
	transcript := filepath.Join(codeafHome, "projects", "job-75", ".cell", "transcript.jsonl")
	for _, dir := range []string{root, filepath.Dir(transcript)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"go.mod": "module example.com/repo\n\ngo 1.21\n", "main.go": "package main\n\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fence, err := executor.NewFence(executor.FenceOptions{Root: root, Home: filepath.Join(base, "home"), CodeafHome: codeafHome, StateDir: filepath.Join(base, "state"),
		Receipts: []string{verification.ReceiptRoot(transcript)}})
	if err != nil {
		t.Fatal(err)
	}
	command, err := verification.PrepareCallIn(transcript, root, "go vet .", "call-1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := executor.FencedSeat(fence).In(root).Exec(context.Background(), executor.ExecRequest{Argv: []string{"/bin/bash", "-c", command}, Env: os.Environ()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Exit != 0 {
		t.Fatalf("the fenced check could not run (exit %d): %s", res.Exit, res.Stderr)
	}
	receipts, _ := filepath.Glob(filepath.Join(verification.ReceiptRoot(transcript), "call-*", "check-*"))
	if len(receipts) == 0 {
		t.Fatalf("the fenced check left no receipt: %s %s", res.Stdout, res.Stderr)
	}
	// The receipt folder is the only part of codeaf's home the step may write.
	probe := filepath.Join(codeafHome, "projects", "outside")
	if res, _ := executor.FencedSeat(fence).In(root).Exec(context.Background(), executor.ExecRequest{Argv: []string{"/bin/sh", "-c", "echo x > " + probe}, Env: os.Environ()}, nil); res.Exit == 0 {
		t.Fatal("the fence let a step write elsewhere in codeaf's home")
	}
}
