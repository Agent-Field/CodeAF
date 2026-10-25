package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// API-level protocol acceptance: fictional task metadata is explicitly seeded.
// Real native seals, public OpenBranch, and a second public Take prove transport;
// this is not evidence that a model or /task created the seeded frontier.
func TestIndependentBranchResultsSurviveSecondPublicTake(t *testing.T) {
	testIndependentBranchResultsSurviveSecondPublicTake(t, false)
}

func TestNestedPlainBranchResultsSurviveSecondPublicTake(t *testing.T) {
	testIndependentBranchResultsSurviveSecondPublicTake(t, true)
}

func testIndependentBranchResultsSurviveSecondPublicTake(t *testing.T, plainNested bool) {
	ctx := context.Background()
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(h.work, "keep.txt"), "baseline\n")
	write(filepath.Join(h.work, "large-input.dat"), strings.Repeat("unchanged baseline\n", 1<<17))
	if !plainNested {
		git(t, h.work, "init", "-q", "-b", "main")
		git(t, h.work, "add", ".")
		git(t, h.work, "commit", "-qm", "baseline")
	}
	a := h.openA()
	a.mustSay("initial shared turn")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := first.Taken.Cell
	work := workspaceOf(at.Root)
	t.Setenv(home.EnvVar, h.homeB)
	var inheritedChatURI string
	if plainNested {
		copies := seedNestedPlainProtocolFixture(t, at, work)
		inheritedChatURI = copies["1"].ChatURI
	} else {
		registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
		if err != nil {
			t.Fatal(err)
		}
		parent, err := registry.Register(workspaceidentity.Chat, at.Root)
		inheritedChatURI = parent.URI
		if err != nil {
			t.Fatal(err)
		}
		meta, err := session.LoadMeta(at.Root)
		if err != nil {
			t.Fatal(err)
		}
		meta.ChatURI = parent.URI
		if err := session.SaveMeta(at.Root, meta); err != nil {
			t.Fatal(err)
		}
		tree := filepath.Join(at.Root, cell.TreesDir, "1")
		git(t, work, "worktree", "add", "-qb", "task/completed", tree)
		write(filepath.Join(tree, "committed.txt"), "completed committed result\n")
		git(t, tree, "add", "committed.txt")
		git(t, tree, "commit", "-qm", "completed output")
		write(filepath.Join(tree, "dirty.txt"), "completed dirty result\n")
		if err := os.Remove(filepath.Join(tree, "keep.txt")); err != nil {
			t.Fatal(err)
		}
		active := filepath.Join(at.Root, cell.TreesDir, "2")
		git(t, work, "worktree", "add", "-qb", "task/active", active)
		write(filepath.Join(active, "active.txt"), "unfinished active output\n")
		checkpoint := `{"type":"tasks","version":1,"seq":2,"runs":[{"id":1,"title":"finished fixture","state":"done","report":"completed report","result":"completed answer","costUsd":0.125,"changed":["committed.txt","dirty.txt","keep.txt"],"copy":{"dir":"foreign-completed"}},{"id":2,"title":"active fixture","state":"running","report":"partial report","pendingRun":{},"copy":{"dir":"foreign-active"}}]}`
		write(session.Place{Dir: at.Root}.Tasks(), checkpoint)
	}
	appendTo(t, transcriptOf(at), "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"unpublished task results\"}\n")
	eng := h.engB
	eng.Workspace = work
	if _, err := eng.Seal(ctx, at, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeA)
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Taken.Kept == "" {
		t.Fatal("unpublished task snapshot was not preserved as a branch")
	}
	t.Setenv(home.EnvVar, h.homeA)
	originalMain, err := os.ReadFile(transcriptOf(h.cell))
	if err != nil {
		t.Fatal(err)
	}
	branchRoot := filepath.Join(t.TempDir(), again.Taken.Kept)
	opener := h.a.Continuer(h.engine, TakeOptions{RootFor: func(string) string { return branchRoot }})
	fork, err := opener.OpenBranch(ctx, again.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	forkMeta, err := session.LoadMeta(fork.Taken.Cell.Root)
	if err != nil || forkMeta.ChatURI == "" || forkMeta.ChatURI == inheritedChatURI {
		t.Fatalf("fork identity not independent: %+v %v", forkMeta, err)
	}
	rows := session.SealedArtifacts(fork.Taken.Cell.Root)
	expectedResults := 2
	if plainNested {
		expectedResults = 4
	}
	if len(rows) != expectedResults {
		t.Fatalf("completed outputs not discoverable, or baseline duplicated: %+v", rows)
	}
	assertForkResultArtifacts(t, rows)
	if entries, _ := os.ReadDir(filepath.Join(fork.Taken.Cell.Root, cell.TreesDir)); len(entries) != 0 {
		t.Fatal("fork instantiated inherited task work")
	}

	// The next normal seal and takeover must carry the historical files and
	// resolve their ledger paths under the new device's chat folder.
	forkChat := h.openOn(h.a, h.engine, fork.Taken.Cell, workspaceOf(fork.Taken.Cell.Root), nameA)
	forkChat.mustSay("new independent turn")
	executor.Settle(ctx, forkChat.seat)
	forkChat.drive.Idle()
	h.durable(fork.Taken.Cell.ID, fork.Taken.Cell)
	if err := forkChat.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	second, err := h.continuerB().Take(ctx, fork.Taken.Cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Taken.Cell.Root == fork.Taken.Cell.Root {
		t.Fatal("second transfer did not relocate the chat")
	}
	secondMeta, err := session.LoadMeta(second.Taken.Cell.Root)
	if err != nil || secondMeta.ChatURI != forkMeta.ChatURI {
		t.Fatalf("second transfer changed independent identity: %+v %v", secondMeta, err)
	}
	rows = session.SealedArtifacts(second.Taken.Cell.Root)
	if len(rows) != expectedResults {
		t.Fatalf("second seal/Take lost historical result ledger: %+v", rows)
	}
	assertForkResultArtifacts(t, rows)
	for _, row := range rows {
		relative, err := filepath.Rel(second.Taken.Cell.Root, row.Path)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatalf("artifact retained source path: %s", row.Path)
		}
	}
	saved, err := os.ReadFile(session.Place{Dir: second.Taken.Cell.Root}.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var taskDoc struct {
		Nodes []json.RawMessage
		Runs  []struct {
			Copy, PendingRun json.RawMessage
			Report           string
		}
	}
	if err := json.Unmarshal(saved, &taskDoc); err != nil {
		t.Fatal(err)
	}
	for _, row := range taskDoc.Runs {
		if len(row.Copy) > 0 || len(row.PendingRun) > 0 {
			t.Fatal("second transfer restored inherited execution authority")
		}
	}
	if raw, err := os.ReadFile(transcriptOf(h.cell)); err != nil || !bytes.Equal(raw, originalMain) {
		t.Fatal("opening or relaying fork changed main conversation")
	}
}

func assertForkResultArtifacts(t *testing.T, rows []session.Artifact) {
	t.Helper()
	want := map[string]string{"committed.txt": "completed committed result\n", "dirty.txt": "completed dirty result\n", "child-committed.txt": "nested committed output\n", "child-dirty.txt": "nested dirty output\n"}
	for _, row := range rows {
		body, err := os.ReadFile(row.Path)
		if err != nil || string(body) != want[filepath.Base(row.Path)] {
			t.Fatalf("historical output %s=%q %v", row.Path, body, err)
		}
	}
}
