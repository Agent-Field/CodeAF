package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/diagprofile"
	"github.com/Agent-Field/codeaf/internal/env"
)

// startDiagnostics is main's one hook for the developer-only profile
// (internal/diagprofile). It runs before anything else so the window covers the
// process's real startup, and it is a no-op unless CODEAF_DIAGNOSTIC_PROFILE_DIR
// is set. A set-but-invalid variable ends the process with status 2 and one
// fixed sentence on STDERR: stdout belongs to the JSON protocols (`engine`,
// `app`, `do --json`) and this never writes there.
//
// The returned stop waits for the files to be complete. A process that dies by
// signal, os.Exit elsewhere or a runtime crash does not run it, and leaves the
// CPU file incomplete; that is stated in docs/DIAGNOSTIC-PROFILE.md.
func startDiagnostics() (stop func()) {
	session, err := diagprofile.Start(context.Background(), env.Lookup, func(line string) {
		fmt.Fprintln(os.Stderr, line)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "codeaf:", err)
		os.Exit(2)
	}
	return session.Stop
}
