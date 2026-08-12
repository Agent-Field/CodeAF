package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
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
// A bundle lays its parts side by side and they run at once. Every part of one
// splice carries that splice's creation sequence, and every part's title is
// clipped to 48 characters for the rail, so five briefs on five topics that
// open with the same words were handed one address between them. Four of the
// five deliverables were overwritten by whichever sibling finished last; the
// run's own reflection recorded the collision and nothing acted on it.
//
// Titles stay clipped — they are for reading. The address is keyed on the node
// id, which is unique by construction, so identical titles cost nothing.
//
// The rail-side half of this defect has since been fixed at its own layer:
// clipTitles keeps the divergence rather than the shared head, so a bundle no
// longer hands two parts the same name. That fix is pinned below, but it is
// not what makes the address safe, and this test must not depend on it — the
// address has to survive a collision it is never again handed. So the siblings
// here are given one byte-identical title outright.
func TestSiblingLeavesWhoseTitlesClipAlikeAreNotHandedTheSameFile(t *testing.T) {
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Real titles, from the real clip: a bundle names each part with the
	// person's own opening words, bounded to rail width. These two share a
	// 60-character stem and diverge only on the last word.
	graph := plan.Bundle("five briefs", []string{
		"Research and write a one-page brief on the history of movable type in Korea",
		"Research and write a one-page brief on the history of movable type in Mainz",
	})
	var titles []string
	for _, node := range graph.Nodes {
		if node.Kind == plan.KindWork {
			titles = append(titles, node.Title)
		}
	}
	if len(titles) != 2 {
		t.Fatalf("the bundle laid out %d parts, want 2: %q", len(titles), titles)
	}
	if titles[0] == titles[1] {
		t.Fatalf("both parts are named %q — the rail cannot tell the siblings apart", titles[0])
	}

	// The address, though, is not allowed to lean on that. Two siblings under
	// one splice, same creation sequence, same order of magnitude — and one
	// name between them.
	shared := titles[0]
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
