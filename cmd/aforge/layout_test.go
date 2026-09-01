package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The two surfaces that run leaves both send the harness's own files out of the
// directory the leaf works in, and they send them somewhere a debugger can find.
//
// This is the surface half of the layout law that internal/exec pins from below
// (TestTheLeafsWorkingDirectoryHoldsNoMachinery). It used to hold for exactly
// one caller — the errand that edits a person's project — on the reading that a
// directory the harness made for a job is the harness's to litter. What it
// littered it with was the worker's own transcript, in the one directory the
// worker was told to work in, and a measured leaf spent five of eleven turns
// reading it back.
func TestBothSurfacesSendTheirMachineryOutOfTheLeafsDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)

	// The resident surface: jobs work under <store>-workspace/<job>, machinery
	// goes to <store>-scratch, and neither is under the other.
	database := filepath.Join(state, "chat.db")
	workspaceRoot := home.StoreDir(database, "workspace")
	scratchRoot := home.StoreDir(database, "scratch")
	if scratchRoot == workspaceRoot {
		t.Fatal("the resident surface's machinery shares a root with its work")
	}
	jobDir := filepath.Join(workspaceRoot, "task-1")
	if within(scratchRoot, jobDir) || within(jobDir, scratchRoot) {
		t.Fatalf("a job directory (%s) and the scratch home (%s) contain one another", jobDir, scratchRoot)
	}

	// The headless surface: the workspace may be a person's repository, so the
	// machinery goes to aforge's own state root rather than to a sibling of it.
	headless := home.Join("scratch", "aforge-abc123")
	if !within(state, headless) {
		t.Fatalf("the headless scratch home %s is outside aforge's state root %s", headless, state)
	}
	repository := filepath.Join(t.TempDir(), "someones-project")
	if within(repository, headless) {
		t.Fatalf("the headless surface put its machinery inside the person's workspace %s", repository)
	}
}

// The surface's own note to a node — that an older graph named a worker this
// build does not have — has to land in the same file the recorder is about to
// open, and the recorder is in the scratch home now. It has been out of step with the recorder
// once before, one directory earlier, which is why it is seated rather than
// spelled here.
func TestTheDegradationNoteFollowsTheRecorderIntoTheScratchHome(t *testing.T) {
	session := "chat-scratch"
	graph := seedErrandGraph(t, session, "retired-worker")
	workspace, scratch := t.TempDir(), t.TempDir()
	seatLeafWorkerNotes(workspace, scratch, graph)
	t.Cleanup(func() { seatLeafWorkerNotes("", "", nil) })

	node, found, err := graph.Node("task-1-leaf")
	if err != nil || !found {
		t.Fatalf("read the leaf: found=%t err=%v", found, err)
	}
	leafSubharness(node)

	body, err := os.ReadFile(exec.TraceFile(scratch, node.ID))
	if err != nil {
		t.Fatalf("the note did not follow the recorder into the scratch home: %v", err)
	}
	if want := `note: "retired-worker" is not a worker`; !strings.Contains(string(body), want) {
		t.Fatalf("the recorder does not carry the note: %q", string(body))
	}
	// And nothing at all was written beside the person's work.
	if entries, err := os.ReadDir(filepath.Join(workspace, "task-1")); err == nil && len(entries) > 0 {
		t.Fatalf("the job directory holds machinery: %v", entries)
	}
}

// within reports that child sits inside parent, which is the whole question the
// layout law asks of two directories.
func within(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
