package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// ── THE CHECK SEES WHAT NOTHING ADDED ───────────────────────────────────────

// THE INCIDENT, PINNED. A run was checked against `git diff --cached` and the
// files it turned on were UNTRACKED — nothing had added them, no diff showed
// them, and the reading judged the whole change against the tracked half of it.
// The check reads the ground's full manifest now, and this is the assertion that
// says so: an untracked file is IN the evidence, named, and labelled as
// something no diff will show.
func TestTheChecksEvidenceIncludesUntrackedFiles(t *testing.T) {
	repo := newTestRepo(t)
	// One tracked file changed and staged, one file nothing has ever added.
	writeFile(t, filepath.Join(repo, "shared.txt"), "the changed line\n")
	writeFile(t, filepath.Join(repo, "topbar.go"), "package tui3\n")
	mustGit(t, repo, "add", "--", "shared.txt")

	node := loneTestNode(t, "redraw the top bar")
	node.graph.mu.Lock()
	node.spec.acceptance = "the top bar draws"
	node.graph.mu.Unlock()

	question := auditQuestion(node, taskTree{root: repo}, auditGround{dir: repo}, auditDoor{},
		[]string{"shared.txt"}, "it draws")

	if !strings.Contains(question, groundManifestHeading) {
		t.Fatalf("the check was given no manifest of the ground it stands in:\n%s", question)
	}
	// THE UNTRACKED FILE IS THE WHOLE POINT. It is not in `Files it wrote`, it is
	// not in any diff, and before the manifest there was no line of the packet it
	// could have appeared on.
	if !strings.Contains(question, "?? topbar.go") {
		t.Fatalf("the untracked file is not in the evidence:\n%s", question)
	}
	if !strings.Contains(question, groundManifestUntracked) {
		t.Fatalf("the manifest does not say what `??` means:\n%s", question)
	}
	// AND THE TRACKED HALF IS STILL THERE, so the manifest is a survey of the
	// whole ground rather than a listing of the leftovers.
	if !strings.Contains(question, "shared.txt") {
		t.Fatalf("the staged change is not in the manifest:\n%s", question)
	}
}

// A GROUND WITH NOTHING IN IT SAYS SO. "The tree is clean" and "nobody looked"
// are the same blank page, and only one of them is a finding.
func TestAGroundWithNoChangeSaysSoRatherThanNothing(t *testing.T) {
	repo := newTestRepo(t)
	if got := groundManifest(repo); !strings.Contains(got, groundManifestClean) {
		t.Fatalf("a clean tree drew no manifest at all: %q", got)
	}
}

// AND A DIRECTORY THAT IS NOT A REPOSITORY DRAWS NOTHING. There is no manifest
// to give, the packet already says the workspace is not one, and an empty string
// renders as nothing — the emptiness law, said about a block of evidence.
func TestAGroundThatIsNotARepositoryDrawsNoManifest(t *testing.T) {
	plain := t.TempDir()
	if got := groundManifest(plain); got != "" {
		t.Fatalf("a plain directory was given a manifest: %q", got)
	}
	node := loneTestNode(t, "write the notes")
	question := auditQuestion(node, taskTree{}, auditGround{dir: plain}, auditDoor{}, nil, "")
	if strings.Contains(question, groundManifestHeading) {
		t.Fatalf("the packet carries a manifest of a directory git knows nothing about:\n%s", question)
	}
}

// THE PRIVATE METADATA DIRECTORY IS NOT EVIDENCE. A job's own log is this
// program's droppings, and a manifest that reported them would be the harness
// asking a reader to judge work by the files the harness left.
func TestTheManifestLeavesThisProgramsOwnDroppingsOut(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, aforgeDroppings, "jobs", "1.log"), "building\n")
	writeFile(t, filepath.Join(repo, "topbar.go"), "package tui3\n")

	manifest := groundManifest(repo)
	if strings.Contains(manifest, aforgeDroppings) {
		t.Fatalf("the manifest reports this program's own droppings:\n%s", manifest)
	}
	if !strings.Contains(manifest, "topbar.go") {
		t.Fatalf("the manifest lost the work's own untracked file:\n%s", manifest)
	}
}
