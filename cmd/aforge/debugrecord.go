package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

// openDebugRecord is what every door calls the moment it has read the switch:
// it mints this invocation's run id and, when the record is on, writes the run's
// own header into a folder named after it (internal/trace's [trace.OpenRun]).
//
// IT IS ONE FUNCTION AND NOT THREE COPIES because the three doors differ only in
// the word they call themselves and where they were pointed. Everything else —
// what "the workspace" means when nobody named one, which build this is, when
// the run started — is the same answer at every door, and three copies of it is
// three places for it to drift.
//
// The context it returns is the run's, and the caller threads it into whatever
// it opens. A door that cannot thread one loses nothing: the run is also this
// process's own (see [trace.Begin]).
func openDebugRecord(command, model, workspace string) context.Context {
	ctx := trace.Begin(context.Background())
	trace.OpenRun(ctx, trace.RunHeader{
		Command:   command,
		Model:     strings.TrimSpace(model),
		Build:     buildinfo.String(),
		Workspace: debugRecordWorkspace(workspace),
		Started:   time.Now(),
	})
	return ctx
}

// debugRecordWorkspace is the folder a run was pointed at, as an absolute path.
// A door that was given no folder was pointed at the one it was started in,
// which is what every door already does with the flag left off — so the header
// says the same thing the run does rather than leaving the field empty and
// making a reader guess.
func debugRecordWorkspace(named string) string {
	named = strings.TrimSpace(named)
	if named == "" {
		named = "."
	}
	if absolute, err := filepath.Abs(named); err == nil {
		return absolute
	}
	if here, err := os.Getwd(); err == nil {
		return here
	}
	return named
}
