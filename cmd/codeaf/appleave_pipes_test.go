package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// THE ENGINE OUTLIVES ITS APP'S PIPES, AND ITS CHILDREN KEEP SIGPIPE. When
// the app has closed both pipes the engine's writes fail and it goes on to the
// end of its wrap-up; a shell it starts still ends a `producer | head` pipe.
func TestAnEngineWritesOnAfterItsAppsPipesClose(t *testing.T) {
	if dir := os.Getenv("CODEAF_TEST_PIPES_CHILD"); dir != "" {
		surviveBrokenPipes()
		// The parent closes its ends, then says so: every write after this is
		// to a broken pipe, never into a buffer someone still reads.
		deadline := time.Now().Add(10 * time.Second)
		for _, err := os.Stat(filepath.Join(dir, "closed")); err != nil; _, err = os.Stat(filepath.Join(dir, "closed")) {
			if time.Now().After(deadline) {
				os.Exit(3)
			}
			time.Sleep(10 * time.Millisecond)
		}
		var outErr, errErr error
		for outErr == nil || errErr == nil {
			if time.Now().After(deadline) {
				os.Exit(4)
			}
			if outErr == nil {
				_, outErr = os.Stdout.WriteString("an event the app will never read\n")
			}
			if errErr == nil {
				_, errErr = os.Stderr.WriteString("a log line no one collects\n")
			}
		}
		if !errors.Is(outErr, syscall.EPIPE) || !errors.Is(errErr, syscall.EPIPE) {
			os.Exit(5)
		}
		// A shell this engine starts gets SIGPIPE back: its producer ends at once.
		status, err := exec.Command("bash", "-c", "yes | head -1 >/dev/null; echo ${PIPESTATUS[0]}").Output()
		if err != nil {
			os.Exit(6)
		}
		_ = os.WriteFile(filepath.Join(dir, "child-sigpipe"), status, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "wrapped-up"), []byte("ok"), 0o600)
		os.Exit(0)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to start a child shell with")
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
	if err := os.WriteFile(filepath.Join(dir, "closed"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the engine ended while writing to its gone app: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wrapped-up")); err != nil {
		t.Fatalf("the wrap-up did not finish: %v", err)
	}
	status, _ := os.ReadFile(filepath.Join(dir, "child-sigpipe"))
	if got := strings.TrimSpace(string(status)); got != "141" {
		t.Fatalf("a shell the engine starts ended its producer with %q, want 141 (SIGPIPE)", got)
	}
}
