package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// This protocol fixture intentionally seeds mock structured metadata. The live
// acceptance test separately requires the real model/task door to create it.
func TestPublicTakeRestoresEveryWorkspaceSetRoot(t *testing.T) {
	testPublicWorkspaceTakeAndFork(t, false)
}

func TestPublicForkKeepsLiteralLinksWithoutCitingTheirTargets(t *testing.T) {
	testPublicWorkspaceTakeAndFork(t, true)
}

func testPublicWorkspaceTakeAndFork(t *testing.T, literalLinks bool) {
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, h.homeA)
	chat := h.openA()
	chat.mustSay("mock protocol baseline")
	if err := chat.drive.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := registry.Register(workspaceidentity.Chat, h.cell.Root)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(h.cell.Root)
	if err != nil {
		t.Fatal(err)
	}
	meta.ChatURI = owner.URI
	if err := session.SaveMeta(h.cell.Root, meta); err != nil {
		t.Fatal(err)
	}
	set := &session.WorkspaceSet{Version: 1, Primary: "app"}
	plan := &taskcopy.WorkspacePlan{Version: 2, ChatURI: owner.URI}
	group := taskcopy.WorkspaceGroup{TaskID: 1, Primary: "app"}
	receiverBindings := map[string]string{}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if literalLinks {
		if err := os.WriteFile(outside, []byte("mock outside bytes must not become a deliverable\n"), 0000); err != nil {
			t.Fatal(err)
		}
	}

	for _, slot := range []string{"app", "library"} {
		source := t.TempDir()
		project, err := registry.Register(workspaceidentity.Project, source)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(h.cell.Root, cell.TreesDir, "1", slot)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "baseline.txt"), []byte("input "+slot+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "init", "-q", "-b", "task/"+slot)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-qm", "mock input")
		base, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		run, err := registry.Register(workspaceidentity.Run, dir, workspaceidentity.Links{OwnerURI: owner.URI, ProjectURI: project.URI})
		if err != nil {
			t.Fatal(err)
		}
		copy := &session.TaskCopyRecord{URI: run.URI, ChatURI: owner.URI, ProjectURI: project.URI, GroundURI: project.URI, Dir: "session:trees/1/" + slot, Mode: session.TaskModeMirror, Branch: "task/" + slot, CheckBase: strings.TrimSpace(string(base))}
		set.Roots = append(set.Roots, session.WorkspaceRootUse{Slot: slot, SourceURI: project.URI, Access: session.WorkspaceWrite, Copy: copy})
		group.Entries = append(group.Entries, taskcopy.WorkspaceEntry{CopyURI: run.URI, ChatURI: owner.URI, ProjectURI: project.URI, RootURI: project.URI, GroundURI: project.URI, Layout: "1/" + slot, Access: "write"})
		if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte("private output "+slot+"\n"), 0750); err != nil {
			t.Fatal(err)
		}
		if literalLinks {
			for name, target := range map[string]string{"dangling-link": "../missing-fixture-target", "outside-link": outside} {
				if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		receiverBindings[project.URI] = t.TempDir()
	}
	plan.Groups = append(plan.Groups, group)
	document := map[string]any{"type": "tasks", "version": 2, "seq": 1, "workspace_transfer": plan,
		"runs": []any{map[string]any{"id": 1, "title": "mock set", "state": "done", "workspaceSet": set, "copy": set.Roots[0].Copy}}}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile((session.Place{Dir: h.cell.Root}).Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Seal(context.Background(), h.cell, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	view, err := h.a.Dir.Cell(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	engine := h.engine.Sync(cellstore.SyncKeys{CellKey: h.a.Identity.CellKey(), Dedup: h.a.Identity.DedupSecret()}, h.a.Ledger)
	publisher := &cellsync.Publisher{Engine: engine, Store: h.a.Store, Dir: h.a.Dir}
	driving := cellsync.Driving{Cell: h.cell, Fence: view.Cell.Lease.Fence, Head: view.Cell.Head, Frames: view.Cell.Frames}
	if err := publisher.Publish(context.Background(), &driving, headOf(t, h.cell), cellsync.PublishInfo{}); err != nil {
		t.Fatal(err)
	}
	// Drive the public takeover, not the internal carrier, so hook ordering and
	// receiver authority are part of this regression's observable behavior.
	t.Setenv(home.EnvVar, h.homeB)
	unmapped := h.b.Continuer(h.engB, TakeOptions{RootFor: func(id string) string { return filepath.Join(h.rootsB, id) }})
	before, err := h.b.Dir.Cell(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, missingErr := unmapped.Take(context.Background(), h.cell.ID)
	var missing *session.WorkspaceBindingsRequired
	if !errors.As(missingErr, &missing) || len(missing.Roots) != 2 {
		t.Fatalf("takeover did not offer both missing roots: %v", missingErr)
	}
	for _, root := range missing.Roots {
		if receiverBindings[root.URI] == "" || !strings.HasPrefix(root.Label, "1/") {
			t.Fatalf("root choice lost source identity or declared layout: %+v", root)
		}
	}
	after, err := h.b.Dir.Cell(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Cell.Lease != after.Cell.Lease {
		t.Fatal("asking for root mappings displaced the existing holder")
	}
	// The fetched tree lives beside the final root during admission. A binding
	// to the final root must fail before the lease is acquired.
	overlap := make(map[string]string, len(receiverBindings))
	for uri, path := range receiverBindings {
		overlap[uri] = path
	}
	receiverRoot := filepath.Join(h.rootsB, h.cell.ID)
	scope := h.b.scope(h.cell.ID)
	if err := unmapped.taker(scope).Fetch.Fetch(context.Background(), cell.Cell{ID: h.cell.ID, Root: receiverRoot}, before.Cell.Head); err != nil {
		t.Fatal(err)
	}
	scope.Settle()
	overlap[missing.Roots[0].URI] = receiverRoot
	if _, err := unmapped.TakeSeenWithWorkspaceRoots(context.Background(), h.cell.ID, &directory.LeaseExpectation{Device: before.Cell.Lease.Device, Fence: before.Cell.Lease.Fence}, overlap); err == nil || !strings.Contains(err.Error(), "overlaps private receiver storage") {
		t.Fatalf("takeover did not reject overlapping final private storage: %v", err)
	}
	afterOverlap, err := h.b.Dir.Cell(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Cell.Lease != afterOverlap.Cell.Lease {
		t.Fatal("rejecting final-root overlap displaced the existing holder")
	}
	wrong := &directory.LeaseExpectation{Device: "changed-holder", Fence: before.Cell.Lease.Fence}
	if _, err := unmapped.TakeSeenWithWorkspaceRoots(context.Background(), h.cell.ID, wrong, receiverBindings); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("mapping retry ignored changed holder: %v", err)
	}
	seen := &directory.LeaseExpectation{Device: before.Cell.Lease.Device, Fence: before.Cell.Lease.Fence}
	got, err := unmapped.TakeSeenWithWorkspaceRoots(context.Background(), h.cell.ID, seen, receiverBindings)
	if err != nil {
		t.Fatalf("public Take lost a declared working root: %v", err)
	}
	for _, slot := range []string{"app", "library"} {
		file := filepath.Join(got.Taken.Cell.Root, cell.TreesDir, "1", slot, "result.txt")
		raw, err := os.ReadFile(file)
		if err != nil || string(raw) != "private output "+slot+"\n" {
			t.Fatalf("%s root output: %q, %v", slot, raw, err)
		}
	}
	for _, source := range receiverBindings {
		if entries, err := os.ReadDir(source); err != nil || len(entries) != 0 {
			t.Fatalf("receiver association was used as a landing destination: %v, %v", entries, err)
		}
	}
	t.Setenv(home.EnvVar, h.homeA)
	brancher := cellsync.Brancher{Dir: h.a.Dir, Publisher: publisher, NewID: func() (string, error) { return cell.NewID(h.clock.Now()) }}
	branch, err := brancher.Branch(context.Background(), &driving, headOf(t, h.cell), 0, cellsync.PublishInfo{Title: "mock independent results"})
	if err != nil {
		t.Fatal(err)
	}
	forkBase := t.TempDir()
	opener := h.a.Continuer(h.engine, TakeOptions{RootFor: func(id string) string { return filepath.Join(forkBase, id) }})
	fork, err := opener.OpenBranch(context.Background(), branch)
	if err != nil {
		t.Fatalf("public fork could not preserve settled multi-root results: %v", err)
	}
	history := session.ForkHistoryDir(session.Place{Dir: fork.Taken.Cell.Root})
	for _, slot := range []string{"app", "library"} {
		raw, err := os.ReadFile(filepath.Join(history, "results", "1", slot, "result.txt"))
		if err != nil || string(raw) != "private output "+slot+"\n" {
			t.Fatalf("fork lost %s output: %q, %v", slot, raw, err)
		}
		if _, err := os.Stat(filepath.Join(history, "results", "1", slot, "baseline.txt")); !os.IsNotExist(err) {
			t.Fatalf("fork copied unchanged input as output: %v", err)
		}
		if literalLinks {
			for name, want := range map[string]string{"dangling-link": "../missing-fixture-target", "outside-link": outside} {
				got, err := os.Readlink(filepath.Join(history, "results", "1", slot, name))
				if err != nil || got != want {
					t.Fatalf("fork lost literal result %s: %q %v", name, got, err)
				}
			}
		}
	}
	if literalLinks {
		ledger, err := os.ReadFile((session.Place{Dir: fork.Taken.Cell.Root}).ArtifactLedger())
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ledger, []byte("dangling-link")) || bytes.Contains(ledger, []byte("outside-link")) {
			t.Fatal("literal link was registered as a regular document citation")
		}
		for _, artifact := range session.SealedArtifacts(fork.Taken.Cell.Root) {
			info, err := os.Lstat(artifact.Path)
			if err != nil || !info.Mode().IsRegular() {
				t.Fatal("artifact registry exposed a link target")
			}
		}
		if info, err := os.Stat(filepath.Join(history, filepath.Base(taskcopy.WorkspaceCarrierPath))); err != nil || !info.IsDir() {
			t.Fatal("fork did not retain its complete historical carrier")
		}
	}

}
