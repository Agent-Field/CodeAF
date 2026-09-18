package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/store"
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
	if hint != "job-pr-482-code-review.md" {
		t.Fatalf("the owner's hint = %q, want a workspace path keyed on the node's own id", hint)
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

// A run working in the person's own directory must not file its own notes
// there. The root leaf keeps its voice — its final message is still what the
// person reads — but the file it is offered goes to the run's scratch, beside
// its recorders and its job logs, so a note the ask never named never turns up
// as an untracked file in the tree the delivery is judged in.
func TestARootLeafInAPersonsDirectoryPutsItsNotesInScratch(t *testing.T) {
	workspaceRoot := t.TempDir()
	scratch := filepath.Join(t.TempDir(), "scratch")
	space, err := exec.NewWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(scratch).OwnedByPerson()

	owner := store.Node{ID: "task-2-x1", Parent: store.RootID, CreatedSeq: 7, Title: "toolchain checks"}
	hint, intermediate := leafOutputHint(owner, owner.Title, space)
	if intermediate {
		t.Fatal("the job root was treated as a handoff")
	}
	if !filepath.IsAbs(hint) || !strings.HasPrefix(hint, scratch+string(filepath.Separator)) {
		t.Fatalf("the root leaf's note was pointed at %q, want an absolute path under the run's scratch %q", hint, scratch)
	}

	// The invitation, followed, in the executor's own order: the tree is
	// baselined before the worker moves, and the landing read files whatever
	// changed — so a hint that ever points into the tree again fails here on
	// the observation, not only on the address. The leaf's own write makes the
	// directory first, which is what the write tool does for any offered path.
	space.WatchTree(owner.ID)
	if err := os.MkdirAll(filepath.Dir(hint), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hint, []byte("checks ran green"), 0o600); err != nil {
		t.Fatal(err)
	}
	space.Record(owner.ID, hint)
	space.RecordChanges(owner.ID)
	if got := space.Artifacts(owner.ID); len(got) != 0 {
		t.Fatalf("a note written at the run's own hint was recorded as the job's work: %v", got)
	}
	left, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("the working tree the person named was left with: %v", left)
	}
}

// Only the run's own note moved. A file that turns up in the person's tree
// while the leaf runs — at a name that happens to wear the run's own naming
// shape — is still the tree's answer to what changed, and the record the
// delivery gate reads still holds it: the two are told apart by where the run
// was invited to write, never by the spelling of a name.
func TestAPersonsOwnUntrackedFileStillJoinsTheRecord(t *testing.T) {
	workspaceRoot := t.TempDir()
	space, err := exec.NewWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(filepath.Join(t.TempDir(), "scratch")).OwnedByPerson()

	owner := store.Node{ID: "task-2-x1", Parent: store.RootID, CreatedSeq: 7, Title: "toolchain checks"}
	space.WatchTree(owner.ID)
	if err := os.WriteFile(filepath.Join(workspaceRoot, "task-9-y1-shelf-readings.md"), []byte("running notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	space.RecordChanges(owner.ID)
	got := space.Artifacts(owner.ID)
	if len(got) != 1 || got[0] != "task-9-y1-shelf-readings.md" {
		t.Fatalf("a person's untracked file fell out of the record the gate reads: %v", got)
	}
}

// A directory already sitting at the offered name still withdraws the
// invitation, now measured at the scratch spelling a personal root is handed.
func TestADirectoryAtTheRootsScratchPathWithdrawsTheInvitation(t *testing.T) {
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(filepath.Join(t.TempDir(), "scratch")).OwnedByPerson()
	owner := store.Node{ID: "job", Parent: store.RootID, CreatedSeq: 7, Title: "pr 482 code review"}
	first, _ := leafOutputHint(owner, owner.Title, space)
	if first == "" {
		t.Fatal("the owner was offered nothing before the directory existed")
	}
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	hint, intermediate := leafOutputHint(owner, owner.Title, space)
	if hint != "" {
		t.Fatalf("a directory at the offered path still earned an invitation: %q", hint)
	}
	if intermediate {
		t.Fatal("the withdrawn owner lost its deliverable shape")
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
	if hint != "job-2-write-complete-review.md" {
		t.Fatalf("hint = %q, want the relative scratch spelling", hint)
	}
}

// The fan-out defect, at the one place it was decided.
//
// A wide plan lays its leaves side by side and they run at once. Every leaf of
// one splice carries that splice's creation sequence, and every leaf's title is
// clipped for the rail, so five briefs on five topics that open with the same
// words were handed one address between them. Four of the five deliverables
// were overwritten by whichever sibling finished last; the run's own reflection
// recorded the collision and nothing acted on it.
//
// Titles stay clipped — they are for reading. The address is keyed on the node
// id, which is unique by construction, so identical titles cost nothing, and
// that is the whole of the fix: the address has to survive a collision however
// the titles came to collide. So the siblings here are given one byte-identical
// title outright rather than one produced by any particular naming pass.
func TestSiblingLeavesWhoseTitlesClipAlikeAreNotHandedTheSameFile(t *testing.T) {
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Two siblings under one splice, same creation sequence, same order of
	// magnitude — and one name between them.
	const shared = "Research and write a one-page brief on the"
	siblings := []store.Node{
		{ID: "task-12-n1", Parent: "task-12", CreatedSeq: 144, CreatedOrder: 1, Title: shared},
		{ID: "task-12-n2", Parent: "task-12", CreatedSeq: 144, CreatedOrder: 2, Title: shared},
	}

	seen := map[string]string{}
	for _, node := range siblings {
		hint, intermediate := leafOutputHint(node, node.Title, space)
		if !intermediate {
			t.Fatalf("%s is a handoff under a job root and was treated as the deliverable owner", node.ID)
		}
		if hint == "" {
			t.Fatalf("%s was offered no address at all", node.ID)
		}
		if owner, taken := seen[hint]; taken {
			t.Fatalf("%s and %s were both sent to %q — one of the two deliverables is lost", owner, node.ID, hint)
		}
		seen[hint] = node.ID
	}
}
