package tui3

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAnEmptyTargetIsRefusedAndNeverReachesThePlatform is the law [startOpener]
// exists to keep about NOTHING.
//
// THE BUG IT REPRODUCES. `open ""` on a Mac is not an error. The platform
// resolves the empty path to the calling process's own working directory and
// puts a Finder window on screen — so an EventConnectAuth that carried no URL
// opened a file manager on whatever folder codeaf was started in, and under
// `go test` that folder is the package's own source directory. A person saw a
// window onto internal/tui3 appear and had no way at all to connect it to a
// sign-in: nothing on the surface said anything had been opened.
//
// The refusal is asserted WITH A WORKING OPENER ON PATH, which is the whole
// point. A machine with no opener refuses for a different reason and would pass
// this test with the guard deleted.
func TestAnEmptyTargetIsRefusedAndNeverReachesThePlatform(t *testing.T) {
	name, _ := openerCommand()
	if name == "" {
		t.Skip("this platform has no opener to refuse with")
	}
	// An opener that would succeed, so the only thing that can refuse is the
	// target. It is never run: a test that starts opening things on the machine
	// running it is the fault this file is about.
	dir := t.TempDir()
	script := filepath.Join(dir, name)
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stand-in opener: %v", err)
	}
	t.Setenv("PATH", dir)

	for _, target := range []string{"", " ", "\t\n"} {
		if err := startOpener(target); err == nil {
			t.Fatalf("a handoff of %q was accepted — on a Mac that is a Finder window "+
				"on the working directory, with nothing on screen to explain it", target)
		}
	}
	// And a real target still goes through, so the guard is about nothing and
	// not about tightening what a link may be.
	if err := startOpener("https://example.invalid/"); err != nil {
		t.Fatalf("a link the platform would take was refused: %v", err)
	}
}
