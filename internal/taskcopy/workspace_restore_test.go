package taskcopy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func workspaceReceiver(t *testing.T, c cell.Cell, plan WorkspacePlan) cell.Cell {
	t.Helper()
	receiver := cell.Cell{ID: "receiver", Root: filepath.Join(t.TempDir(), "chat")}
	if err := os.MkdirAll(filepath.Join(receiver.Root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, c.Root, "cp", "-R", workspaceCarrierRoot(c), workspaceCarrierRoot(receiver))
	writeWorkspaceCheckpoint(t, receiver, plan)
	return receiver
}
func TestWorkspaceRestoreReopensSameWholeLayout(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	first, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	stages, _ := filepath.Glob(filepath.Join(receiver.Root, ".codeaf", "workspace-stage-*"))
	if len(stages) != 1 {
		t.Fatalf("stages: %v", stages)
	}
	second, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.Promote(); err != nil {
		t.Fatal(err)
	}
	third, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = third.Promote(); err != nil {
		t.Fatal(err)
	}
	if err = third.Acknowledge(); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Promote(); err != nil {
		t.Fatal(err)
	}
	stages, _ = filepath.Glob(filepath.Join(receiver.Root, ".codeaf", "workspace-stage-*"))
	if len(stages) != 0 {
		t.Fatalf("duplicate stages: %v", stages)
	}
}
func TestWorkspaceRestoreRejectsDamagedClosureBeforeMutations(t *testing.T) {
	for _, kind := range []string{"missing-root", "wrong-checkpoint", "traversal-record", "native-deletion", "native-mode", "native-mode-alias", "native-deletion-alias", "native-overlay", "symlink-carrier", "occupied-destination"} {
		t.Run(kind, func(t *testing.T) {
			c, plan := workspaceFixture(t)
			if err := (Carry{}).Compose(c); err != nil {
				t.Fatal(err)
			}
			receiver := workspaceReceiver(t, c, plan)
			manifest, err := readWorkspaceManifest(receiver)
			if err != nil {
				t.Fatal(err)
			}
			from := filepath.Join(workspaceCarrierRoot(receiver), manifest.Generation, manifest.Roots[1].Token)
			switch kind {
			case "missing-root":
				if err = os.RemoveAll(from); err != nil {
					t.Fatal(err)
				}
			case "wrong-checkpoint":
				write(t, filepath.Join(receiver.Root, cell.StateDir, "tasks.json"), `{"type":"tasks","version":2}`)
			case "traversal-record":
				rec, err := readRecord(from)
				if err != nil {
					t.Fatal(err)
				}
				rec.Deleted = []string{"../../outside"}
				if err = writeRecord(from, rec); err != nil {
					t.Fatal(err)
				}
			case "native-deletion", "native-mode", "native-mode-alias", "native-deletion-alias", "native-overlay":
				rec, err := readRecord(from)
				if err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "native-deletion":
					rec.Deleted = []string{".furrow/workspace-id"}
				case "native-mode-alias":
					rec.Modes = map[string]os.FileMode{"./.furrow/workspace-id": 0600}
				case "native-deletion-alias":
					rec.Deleted = []string{"x/../.furrow/workspace-id"}
				case "native-mode":
					rec.Modes = map[string]os.FileMode{".furrow/workspace-id": 0600}
				case "native-overlay":
					write(t, filepath.Join(from, filesDir, ".furrow", "workspace-id"), "forged native identity\n")
				}
				if err = writeRecord(from, rec); err != nil {
					t.Fatal(err)
				}
			case "symlink-carrier":
				replacement := t.TempDir()
				if err = os.Rename(from, filepath.Join(replacement, "root")); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(filepath.Join(replacement, "root"), from); err != nil {
					t.Fatal(err)
				}
			case "occupied-destination":
				write(t, filepath.Join(liveRoot(receiver), "keep"), "unrelated")
			}
			prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
			if kind == "occupied-destination" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = prepared.Promote(); err == nil {
					t.Fatal("occupied destination accepted")
				}
				got, _ := os.ReadFile(filepath.Join(liveRoot(receiver), "keep"))
				if string(got) != "unrelated" {
					t.Fatal("existing root changed")
				}
				return
			}
			if err == nil {
				t.Fatal("damaged carrier accepted")
			}
			if _, err = os.Stat(filepath.Join(receiver.Root, ".codeaf")); !os.IsNotExist(err) {
				t.Fatalf("validation mutated private restore state: %v", err)
			}
			if _, err = os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
				t.Fatalf("validation exposed partial roots: %v", err)
			}
		})
	}
}
func TestWorkspacePromotionRecoversRenameBeforeReceipt(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the filesystem state after successful rename and before the next
	// durable receipt write; reopening must adopt that exact layout once.
	stage, err := prepared.stage()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(stage, liveRoot(receiver)); err != nil {
		t.Fatal(err)
	}
	reopened, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := reopened.Promote()
	if err != nil || len(bindings) != 2 {
		t.Fatalf("recovery: %v %v", bindings, err)
	}
}
func TestWorkspaceCarrierRejectsInvalidPlansPreservingPublishedGeneration(t *testing.T) {
	for _, kind := range []string{"missing-source", "case-alias", "cycle", "required-incomplete", "layout-escape", "wrong-owner"} {
		t.Run(kind, func(t *testing.T) {
			c, plan := workspaceFixture(t)
			if err := (Carry{}).Compose(c); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(workspaceCarrierRoot(c), "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-source":
				if err = os.RemoveAll(filepath.Join(liveRoot(c), "7", "docs")); err != nil {
					t.Fatal(err)
				}
			case "case-alias":
				plan.Groups[0].Entries[1].Layout = "7/MAIN"
			case "cycle":
				plan.Groups[0].Entries[0].GroundURI = plan.Groups[0].Entries[1].CopyURI
				plan.Groups[0].Entries[1].GroundURI = plan.Groups[0].Entries[0].CopyURI
			case "required-incomplete":
				plan.Groups[0].Incomplete = true
			case "layout-escape":
				plan.Groups[0].Entries[1].Layout = "7/../../escape"
			case "wrong-owner":
				plan.Groups[0].Entries[1].ChatURI = "other"
			}
			writeWorkspaceCheckpoint(t, c, plan)
			if err := (Carry{}).Compose(c); err == nil {
				t.Fatal("invalid plan accepted")
			}
			after, err := os.ReadFile(filepath.Join(workspaceCarrierRoot(c), "manifest.json"))
			if err != nil || string(before) != string(after) {
				t.Fatal("prior carrier replaced")
			}
		})
	}
}
func TestWorkspaceCarrierPreservesCooperatorsAndHistoricalIntent(t *testing.T) {
	c, plan := workspaceFixture(t)
	plan.Groups = append(plan.Groups, WorkspaceGroup{TaskID: 8, Required: true, Primary: "main", Entries: append([]WorkspaceEntry(nil), plan.Groups[0].Entries...)})
	intent := WorkspaceEntry{ChatURI: plan.ChatURI, RootURI: plan.Groups[0].Entries[0].RootURI, Layout: "9/not-carved", Access: "read"}
	plan.Groups = append(plan.Groups, WorkspaceGroup{TaskID: 9, Incomplete: true, Primary: "not-carved", Entries: []WorkspaceEntry{intent}})
	writeWorkspaceCheckpoint(t, c, plan)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspaceCarrierRoot(c), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest workspaceManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Roots) != 2 || len(manifest.Plan.Groups) != 3 {
		t.Fatalf("closure duplicated or intent discarded: %+v", manifest)
	}
	receiver := workspaceReceiver(t, c, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := prepared.Promote()
	if err != nil || len(bindings) != 2 {
		t.Fatalf("cooperator closure: %v %v", bindings, err)
	}
}

func TestWorkspaceReopenRejectsChangedStagedBaseline(t *testing.T) {
	c, plan := workspaceFixture(t)
	for _, entry := range plan.Groups[0].Entries {
		tree := filepath.Join(liveRoot(c), filepath.FromSlash(entry.Layout))
		write(t, filepath.Join(tree, "stable.txt"), "stable baseline\n")
		run(t, tree, "git", "add", "stable.txt")
		run(t, tree, "git", "commit", "-qm", "stable")
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := prepared.stage()
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stage, "7", "main", "stable.txt"), "changed after prepare\n")
	if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
		t.Fatal("changed staged baseline accepted")
	}
	if _, err = os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
		t.Fatal("damaged preparation exposed")
	}
}

func TestWorkspaceWorkerAdmissionRequiresWholeRestoreReceipt(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	if err := WorkspaceReady(c); err != nil {
		t.Fatalf("local source blocked: %v", err)
	}
	receiver := workspaceReceiver(t, c, plan)
	if err := ValidateWorkspace(receiver, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(receiver.Root, ".codeaf")); !os.IsNotExist(err) {
		t.Fatal("preflight wrote receiver state")
	}
	if err := WorkspaceReady(receiver); err == nil {
		t.Fatal("incoming unadopted roots runnable")
	}
	restored, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = WorkspaceReady(receiver); err == nil {
		t.Fatal("prepared roots runnable")
	}
	if _, err = restored.Promote(); err != nil {
		t.Fatal(err)
	}
	if err = WorkspaceReady(receiver); err == nil {
		t.Fatal("promoted but unadopted roots runnable")
	}
	if err = restored.Acknowledge(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(liveRoot(receiver), "7", "main", "base.txt"), "legitimate resumed edit\n")
	write(t, filepath.Join(receiver.Root, cell.StateDir, "tasks.json"), `{"type":"tasks","version":2,"receiver_rebound":true}`)
	if err = WorkspaceReady(receiver); err != nil {
		t.Fatalf("adopted receiver blocked after local updates: %v", err)
	}
}

func TestWorkspaceForkKeepsOnlyChangedOutputsFromEachRoot(t *testing.T) {
	ambient := t.TempDir()
	run(t, ambient, "git", "init", "-q")
	write(t, filepath.Join(ambient, "borrowed-sentinel.txt"), "unrelated working directory\n")
	t.Chdir(ambient)
	c, plan := workspaceFixture(t)
	selections := []WorkspaceResultSelection{}
	for _, entry := range plan.Groups[0].Entries {
		tree := filepath.Join(liveRoot(c), filepath.FromSlash(entry.Layout))
		write(t, filepath.Join(tree, "input-dir/large-unchanged.bin"), strings.Repeat("unchanged", 1<<17))
		run(t, tree, "git", "add", "input-dir/large-unchanged.bin")
		run(t, tree, "git", "commit", "-qm", "input")
		base := run(t, tree, "git", "rev-parse", "HEAD")
		write(t, filepath.Join(tree, "committed.txt"), "committed "+entry.Layout+"\n")
		run(t, tree, "git", "add", "committed.txt")
		run(t, tree, "git", "commit", "-qm", "result")
		write(t, filepath.Join(tree, "output-dir", "nested.txt"), "nested output\n")
		if err := os.Chmod(filepath.Join(tree, "output-dir"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(tree, "output-dir", "empty"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(tree, "only-empty-output"), 0750); err != nil {
			t.Fatal(err)
		}
		selections = append(selections, WorkspaceResultSelection{CopyURI: entry.CopyURI, BaseCommit: base})
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "history")
	outputs, err := RestoreCompletedWorkspaceResults(c, plan, destination, selections)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 {
		t.Fatalf("outputs: %v", outputs)
	}
	for _, path := range outputs {
		for relative, mode := range map[string]os.FileMode{"output-dir": 0750, "output-dir/empty": 0700, "only-empty-output": 0750} {
			info, err := os.Stat(filepath.Join(path, relative))
			if err != nil || !info.IsDir() || info.Mode().Perm() != mode {
				t.Fatalf("historical directory %s missing or changed: %v %v", relative, info, err)
			}
		}
		for _, name := range []string{"committed.txt", "base.txt", "new.txt"} {
			if _, err = os.Stat(filepath.Join(path, name)); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{".git", "input-dir", "deleted.txt"} {
			if _, err = os.Stat(filepath.Join(path, name)); !os.IsNotExist(err) {
				t.Fatalf("non-output %s retained", name)
			}
		}
	}
	if _, err = os.Stat(liveRoot(c)); err != nil {
		t.Fatal("source roots changed")
	}
	if got, err := os.ReadFile(filepath.Join(ambient, "borrowed-sentinel.txt")); err != nil || string(got) != "unrelated working directory\n" {
		t.Fatal("unrelated working directory changed")
	}
	if _, err = RestoreCompletedWorkspaceResults(c, plan, destination, selections); err != nil {
		t.Fatalf("result retry: %v", err)
	}
	selections[1].BaseCommit = strings.Repeat("0", 40)
	untouched := filepath.Join(t.TempDir(), "refused")
	if _, err = RestoreCompletedWorkspaceResults(c, plan, untouched, selections); err == nil {
		t.Fatal("missing base accepted")
	}
	if _, err = os.Stat(untouched); !os.IsNotExist(err) {
		t.Fatal("partial history published")
	}
}

func TestWorkspaceAdoptedReceiverCanRewriteAndPublishAgain(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := prepared.Promote()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Adopt(workspaceidentity.Record{URI: plan.ChatURI, Role: workspaceidentity.Chat}, receiver.Root, workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		entry := binding.Entry
		project := t.TempDir()
		if _, err = registry.Adopt(workspaceidentity.Record{URI: entry.ProjectURI, Role: workspaceidentity.Project}, project, workspaceidentity.Links{}); err != nil {
			t.Fatal(err)
		}
		if _, err = registry.Adopt(workspaceidentity.Record{URI: entry.CopyURI, Role: workspaceidentity.Run}, binding.Path, workspaceidentity.Links{OwnerURI: entry.ChatURI, ProjectURI: entry.ProjectURI}); err != nil {
			t.Fatal(err)
		}
	}
	if err = WorkspaceReady(receiver); err == nil {
		t.Fatal("registry adoption bypassed missing acknowledgement")
	}
	if err = prepared.Acknowledge(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"type": "tasks", "version": 2, "workspace_transfer": plan, "local_paths_rebound": true})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(receiver.Root, cell.StateDir, "tasks.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(liveRoot(receiver), "7", "main", "new.txt"), "legitimate resumed edit\n")
	if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err != nil {
		t.Fatalf("adopted retry rejected local checkpoint: %v", err)
	}
	if err = (Carry{}).Compose(receiver); err != nil {
		t.Fatal(err)
	}
	if err = WorkspaceReady(receiver); err != nil {
		t.Fatalf("next publication blocked resumed source: %v", err)
	}
}

func TestWorkspaceSecondRootFailurePreservesStageAndStartsNothing(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	manifest, err := readWorkspaceManifest(receiver)
	if err != nil {
		t.Fatal(err)
	}
	second := manifest.Roots[1]
	from := filepath.Join(workspaceCarrierRoot(receiver), manifest.Generation, second.Token)
	rec, err := readWorkspaceRecord(from)
	if err != nil {
		t.Fatal(err)
	}
	// The carried overlay has new.txt as a regular file. A mode request below
	// that file fails only when the staged second checkout is inspected.
	rec.Modes["new.txt/child"] = 0644
	if err = writeRecord(from, rec); err != nil {
		t.Fatal(err)
	}
	if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
		t.Fatal("inconsistent second-root path accepted")
	}
	if _, err = os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
		t.Fatal("partial roots became runnable")
	}
	stages, _ := filepath.Glob(filepath.Join(receiver.Root, ".codeaf", "workspace-stage-*"))
	if len(stages) != 1 {
		t.Fatalf("private stage lost: %v", stages)
	}
	if _, err = os.Stat(filepath.Join(stages[0], "7", "main", "base.txt")); err != nil {
		t.Fatal("first root preparation was lost")
	}
	delete(rec.Modes, "new.txt/child")
	if err = writeRecord(from, rec); err != nil {
		t.Fatal(err)
	}
	retried, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = retried.Promote(); err != nil {
		t.Fatal(err)
	}
	if err = WorkspaceReady(receiver); err == nil {
		t.Fatal("unadopted retry runnable")
	}
}

// Directory metadata refusals are exercised through public carrier preparation.

func TestWorkspaceDirectoryMetadataRefusesBeforePreparation(t *testing.T) {
	for _, kind := range []string{"legacy", "nil-map", "traversal", "git-private", "native-private", "special-mode", "owner-permission", "missing-parent", "file-parent", "case-alias", "unicode-alias"} {
		t.Run(kind, func(t *testing.T) {
			c, plan := workspaceFixture(t)
			if err := (Carry{}).Compose(c); err != nil {
				t.Fatal(err)
			}
			receiver := workspaceReceiver(t, c, plan)
			manifest, err := readWorkspaceManifest(receiver)
			if err != nil {
				t.Fatal(err)
			}
			root := &manifest.Roots[0]
			switch kind {
			case "legacy":
				root.DirectoryVersion = 0
				root.Directories = nil
			case "nil-map":
				root.Directories = nil
			case "traversal":
				root.Directories = map[string]os.FileMode{"../outside": 0750}
			case "git-private":
				root.Directories = map[string]os.FileMode{".git": 0700}
			case "native-private":
				root.Directories = map[string]os.FileMode{".furrow": 0700}
			case "special-mode":
				root.Directories = map[string]os.FileMode{"unsafe": os.ModeSymlink | 0750}
			case "owner-permission":
				root.Directories = map[string]os.FileMode{"unreadable": 0500}
			case "missing-parent":
				root.Directories = map[string]os.FileMode{"parent/child": 0750}
			case "file-parent":
				root.Directories = map[string]os.FileMode{"base.txt": 0750, "base.txt/child": 0750}
			case "case-alias":
				root.Directories = map[string]os.FileMode{"output": 0750, "OUTPUT": 0750}
			case "unicode-alias":
				root.Directories = map[string]os.FileMode{"caf\u00e9": 0750, "cafe\u0301": 0750}
			}
			if err = writeWorkspaceJSON(filepath.Join(workspaceCarrierRoot(receiver), "manifest.json"), manifest); err != nil {
				t.Fatal(err)
			}
			if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
				t.Fatalf("%s metadata accepted", kind)
			}
			if _, err = os.Stat(filepath.Join(receiver.Root, ".codeaf")); !os.IsNotExist(err) {
				t.Fatalf("%s mutated preparation state", kind)
			}
		})
	}
}

func TestWorkspaceDirectoryMetadataCannotOmitPayloadParents(t *testing.T) {
	c, plan := workspaceFixture(t)
	for _, entry := range plan.Groups[0].Entries {
		write(t, filepath.Join(liveRoot(c), filepath.FromSlash(entry.Layout), "nested", "output.txt"), "nested output\n")
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	manifest, err := readWorkspaceManifest(receiver)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Roots[0].Directories = map[string]os.FileMode{}
	if err = writeWorkspaceJSON(filepath.Join(workspaceCarrierRoot(receiver), "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
		t.Fatal("incomplete directory set was accepted")
	}
	if _, err = os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
		t.Fatal("incomplete metadata exposed runnable roots")
	}
}

func TestWorkspaceDirectoryMetadataCannotTraverseCheckoutSymlink(t *testing.T) {
	c, plan := workspaceFixture(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep"), "unchanged\n")
	for _, entry := range plan.Groups[0].Entries {
		tree := filepath.Join(liveRoot(c), filepath.FromSlash(entry.Layout))
		if err := os.Symlink(outside, filepath.Join(tree, "redirect")); err != nil {
			t.Fatal(err)
		}
		run(t, tree, "git", "add", "redirect")
		run(t, tree, "git", "commit", "-qm", "symlink baseline")
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	manifest, err := readWorkspaceManifest(receiver)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Roots[0].Directories["redirect"] = 0700
	manifest.Roots[0].Directories["redirect/child"] = 0700
	if err = writeWorkspaceJSON(filepath.Join(workspaceCarrierRoot(receiver), "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err = (Carry{}).PrepareWorkspace(receiver, plan); err == nil {
		t.Fatal("checkout symlink granted directory authority")
	}
	got, err := os.ReadFile(filepath.Join(outside, "keep"))
	if err != nil || string(got) != "unchanged\n" {
		t.Fatal("outside file changed")
	}
	if _, err = os.Stat(filepath.Join(outside, "child")); !os.IsNotExist(err) {
		t.Fatal("outside directory created")
	}
	if _, err = os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
		t.Fatal("symlink plan exposed runnable roots")
	}
}
