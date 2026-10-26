package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// THE ENGINE OUTLIVES ITS APP'S PIPES. The app quits without waiting for the
// engine's wrap-up, so the engine goes on writing after both pipes' readers
// have gone; those writes fail, and the wrap-up still finishes.
func TestAnEngineWritesOnAfterItsAppsPipesClose(t *testing.T) {
	if dir := os.Getenv("CODEAF_TEST_PIPES_CHILD"); dir != "" {
		ignoreBrokenPipes()
		time.Sleep(200 * time.Millisecond) // the parent closes its ends first
		for i := 0; i < 50; i++ {
			_, _ = os.Stdout.WriteString("an event the app will never read\n")
			_, _ = os.Stderr.WriteString("a log line no one collects\n")
		}
		_ = os.WriteFile(filepath.Join(dir, "wrapped-up"), []byte("ok"), 0o600)
		os.Exit(0)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAnEngineWritesOnAfterItsAppsPipesClose$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_PIPES_CHILD="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	errs, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	_ = errs.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the engine ended while writing to its gone app: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wrapped-up")); err != nil {
		t.Fatalf("the wrap-up did not finish: %v", err)
	}
}
