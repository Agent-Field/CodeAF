package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Who may leave a file in the person's directory is a structural question, and
// the structure already answers it: the delivery gate and the announcement both
// read "job root" as "this is what the person gets". The file invitation now
// reads it the same way.
//
// The run this fixes left 07-pr-482-code-review.md, 52-write-complete-review.md,
// 70-read-diff.md, 144-low-findings.md, 144-synthesis.md and
// 216-assemble-review.md in a working directory. Five of the six were the
// working notes of nodes whose whole output was consumed downstream.
func TestOnlyTheDeliverableOwnerIsInvitedIntoTheWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	scratch := filepath.Join(t.TempDir(), "scratch")
	space, err := exec.NewWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(scratch)

	owner := store.Node{ID: "job", Parent: store.RootID, CreatedSeq: 7, Title: "pr 482 code review"}
	hint, intermediate := leafOutputHint(owner, owner.Title, space)
	if intermediate {
		t.Fatal("the job root was treated as a handoff")
	}
	if hint != "07-pr-482-code-review.md" {
		t.Fatalf("the owner's hint = %q, want the workspace path it always had", hint)
	}

	for _, node := range []store.Node{
		{ID: "job-2", Parent: "job", CreatedSeq: 70, Title: "read diff"},
		{ID: "job-3", Parent: "job", CreatedSeq: 144, Title: "synthesis"},
	} {
		hint, intermediate := leafOutputHint(node, node.Title, space)
		if !intermediate {
			t.Fatalf("%s is consumed downstream and was still treated as a deliverable owner", node.ID)
		}
		if !strings.HasPrefix(hint, scratch+string(filepath.Separator)) {
			t.Fatalf("%s was pointed at %q, which is not under the run's scratch %q", node.ID, hint, scratch)
		}
		if strings.HasPrefix(hint, workspaceRoot) {
			t.Fatalf("%s can still land a working file in the person's directory: %q", node.ID, hint)
		}
	}
}

// A chat window's workspace is the run's own directory, so scratch and
// workspace are the same place and there is nowhere else to point. The
// distinction that survives is the one that matters: an intermediate leaf is
// still told its result is a handoff rather than a document.
func TestAnIntermediateLeafIsAHandoffEvenWhenScratchIsTheWorkspace(t *testing.T) {
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	node := store.Node{ID: "job-2", Parent: "job", CreatedSeq: 52, Title: "write complete review"}
	hint, intermediate := leafOutputHint(node, node.Title, space)
	if !intermediate {
		t.Fatal("a node under a job root is not the deliverable owner")
	}
	if hint != "52-write-complete-review.md" {
		t.Fatalf("hint = %q, want the relative scratch spelling", hint)
	}
}
