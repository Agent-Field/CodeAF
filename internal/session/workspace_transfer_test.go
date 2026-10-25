package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// This module fixture supplies the closed source plan DB explicitly. Production
// relay derives its plan index separately; this is not a relay acceptance test.
func TestWorkspaceSetTransferKeepsAllRootsWithAndWithoutDerivedPlanDatabase(t *testing.T) {
	for _, withPlanDB := range []bool{false, true} {
		name := "portable-state-only"
		if withPlanDB {
			name = "explicit-plan-database-module-fixture"
		}
		t.Run(name, func(t *testing.T) { testWorkspaceSetTransfer(t, withPlanDB, false, false) })
	}
}

func TestWorkspaceSetTransferFinalizationRetriesDeniedCheckpointWrite(t *testing.T) {
	testWorkspaceSetTransfer(t, false, true, false)
}

func TestWorkspaceSetTransferRefusesKnownForeignCopyBeforeMutation(t *testing.T) {
	testWorkspaceSetTransfer(t, false, false, true)
}

func testWorkspaceSetTransfer(t *testing.T, withPlanDB, denyWrite, foreignCopy bool) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	app, library := t.TempDir(), newTestRepo(t)
	writeFile(t, filepath.Join(app, "app.txt"), "../library/input.txt\n")
	writeFile(t, filepath.Join(library, "input.txt"), "dependency\n")
	source := Place{Dir: t.TempDir(), Workspace: app}
	if err := os.Mkdir(filepath.Join(source.Dir, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	engine := newBeltRunDouble("done")
	engine.honoursStop = true
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place = app, source; c.SessionFile = source.Transcript() })
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "write both", WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: app, Slot: "app", Access: WorkspaceWrite, Landing: app}, {Source: library, Slot: "library", Access: WorkspaceRead}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	engine.mu.Lock()
	cwd := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(cwd, "unfinished.txt"), "retained bytes\n")
	if done := agent.cutBeltRun(); done != nil {
		<-done
	}
	agent.Close()
	if err := (taskcopy.Carry{}).Compose(cell.Cell{Root: source.Dir}); err != nil {
		t.Fatal(err)
	}
	plan, _, err := taskcopy.ReadWorkspacePlan(cell.Cell{Root: source.Dir})
	if err != nil {
		t.Fatal(err)
	}
	receiver := Place{Dir: t.TempDir(), Workspace: t.TempDir()}
	if err := os.CopyFS(filepath.Join(receiver.Dir, cell.StateDir), os.DirFS(filepath.Join(source.Dir, cell.StateDir))); err != nil {
		t.Fatal(err)
	}
	if withPlanDB {
		planDB, err := os.ReadFile(filepath.Join(source.Dir, planStoreFilename))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(receiver.Dir, planStoreFilename), planDB, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// An older portable chat may not have stamped its chat URI into metadata.
	if truth := layoutOf(receiver.Dir).metaTruth(receiver.Dir); truth != "" {
		if raw, err := os.ReadFile(truth); err == nil {
			var metadata map[string]any
			if err := json.Unmarshal(raw, &metadata); err != nil {
				t.Fatal(err)
			}
			delete(metadata, "chat_uri")
			updated, _ := json.Marshal(metadata)
			if err := os.WriteFile(truth, updated, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv(home.EnvVar, t.TempDir())
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]string{}
	canonical := map[string]string{}
	for _, entry := range plan.Groups[0].Entries {
		local := t.TempDir()
		rec, err := registry.Register(workspaceidentity.Project, local)
		if err != nil {
			t.Fatal(err)
		}
		bindings[entry.ProjectURI] = local
		canonical[local] = rec.URI
	}
	if foreignCopy {
		incoming := plan.Groups[0].Entries[0]
		if _, err := registry.Adopt(workspaceidentity.Record{URI: incoming.ProjectURI, Role: workspaceidentity.Project}, bindings[incoming.ProjectURI], workspaceidentity.Links{}); err != nil {
			t.Fatal(err)
		}
		wrongChat, err := registry.Register(workspaceidentity.Chat, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Adopt(workspaceidentity.Record{URI: incoming.CopyURI, Role: workspaceidentity.Run, OwnerURI: wrongChat.URI, ProjectURI: incoming.ProjectURI}, t.TempDir(), workspaceidentity.Links{OwnerURI: wrongChat.URI, ProjectURI: incoming.ProjectURI}); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(filepath.Join(home.Join("workspace-identities"), "registry.sqlite"))
		if err := ValidateTaskWorkspaceSetBindings(receiver, bindings); err == nil {
			t.Fatal("known foreign copy ownership accepted")
		}
		if _, err := RestoreTaskWorkspaceSets(context.Background(), receiver, bindings); err == nil {
			t.Fatal("known foreign copy was promoted before owner refusal")
		}
		if _, err := os.Stat(receiver.Trees()); !os.IsNotExist(err) {
			t.Fatal("identity preflight exposed private roots")
		}
		after, _ := os.ReadFile(filepath.Join(home.Join("workspace-identities"), "registry.sqlite"))
		if string(before) != string(after) {
			t.Fatal("identity preflight mutated registry")
		}
		return
	}
	if err := ValidateTaskWorkspaceSetBindings(receiver, nil); err == nil {
		t.Fatal("missing all-root bindings were admitted")
	}
	if _, err := os.Stat(receiver.Trees()); !os.IsNotExist(err) {
		t.Fatal("failed binding preflight created receiver copies")
	}
	raw, err := os.ReadFile(receiver.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var tampered taskDocument
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.WorkspaceTransfer.Groups[0].Entries[0].Access = "read"
	mutated, _ := json.Marshal(tampered)
	if err := os.WriteFile(receiver.Tasks(), mutated, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskWorkspaceTransferPlan(receiver); err == nil {
		t.Fatal("tampered projection accepted")
	}
	if err := os.WriteFile(receiver.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskWorkspaceSetBindings(receiver, bindings); err != nil {
		t.Fatal(err)
	}
	if denyWrite {
		stateDir := filepath.Join(receiver.Dir, cell.StateDir)
		if err := os.Chmod(stateDir, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(stateDir, 0700)
		if probe, err := os.CreateTemp(stateDir, ".permission-probe-*"); err == nil {
			probe.Close()
			os.Remove(probe.Name())
			t.Skip("platform cannot deny checkpoint directory writes")
		}
		if _, err := RestoreTaskWorkspaceSets(context.Background(), receiver, bindings); err == nil {
			t.Fatal("denied checkpoint write reported successful transfer")
		}
		if err := workspaceFinalizationPending(receiver); err == nil {
			t.Fatal("failed checkpoint replacement did not fence execution")
		}
		original, err := readWorkspaceCheckpointBytes(receiver.Tasks())
		if err != nil {
			t.Fatal(err)
		}
		if err := (&taskStore{path: receiver.Tasks()}).writeLocked(taskDocument{Type: taskDocumentType, Version: 1}); err == nil {
			t.Fatal("normal writer overwrote pending transfer")
		}
		unchanged, _ := readWorkspaceCheckpointBytes(receiver.Tasks())
		if string(original) != string(unchanged) {
			t.Fatal("normal writer changed pending checkpoint")
		}
		if err := os.Chmod(stateDir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := RestoreTaskWorkspaceSets(context.Background(), receiver, bindings)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMeta(receiver.Dir)
	if err != nil || meta.ChatURI != plan.ChatURI {
		t.Fatal("receiver metadata lost admitted chat identity")
	}
	if err := workspaceFinalizationPending(receiver); err != nil {
		t.Fatal(err)
	}
	finalRaw, err := readWorkspaceCheckpointBytes(receiver.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	finalDocument, err := decodeTasks(finalRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range finalDocument.Runs {
		if row.WorkspaceSet != nil {
			for _, use := range row.WorkspaceSet.Roots {
				if use.Landing != "" {
					t.Fatal("source landing authority travelled to receiver")
				}
			}
		}
	}
	again, err := RestoreTaskWorkspaceSets(context.Background(), receiver, bindings)
	if err != nil || !reflect.DeepEqual(restored, again) {
		t.Fatalf("repeated completed restore was not idempotent: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored %d roots", len(restored))
	}
	for local, uri := range canonical {
		record, err := registry.Lookup(workspaceidentity.Project, local)
		if err != nil || record.URI != uri {
			t.Fatal("receiver canonical project identity changed")
		}
	}
	freshEngine := newBeltRunDouble("done")
	registerBeltRunEngine(t, freshEngine)
	fresh, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = receiver.Workspace, receiver
		c.SessionFile = receiver.Transcript()
	})
	row, ok := runRowOf(fresh.graph(), id)
	if !ok || row.WorkspaceSet == nil {
		t.Fatal("transferred run metadata lost")
	}
	if !withPlanDB {
		freshEngine.mu.Lock()
		started := freshEngine.spec.Workspace != ""
		freshEngine.mu.Unlock()
		if started || row.State != TaskInterrupted {
			t.Fatal("portable-state-only fixture unexpectedly reconstructed active execution")
		}
		if readFile(t, filepath.Join(row.Copy.Dir, "unfinished.txt")) != "retained bytes\n" || readFile(t, filepath.Join(row.Copy.Dir, "..", "library", "input.txt")) != "dependency\n" {
			t.Fatal("portable-state-only restore lost root bytes")
		}
		fresh.Close()
		return
	}
	select {
	case <-freshEngine.entered:
	case <-time.After(5 * time.Second):
		row, _ := runRowOf(fresh.graph(), id)
		t.Fatalf("restored required set did not automatically reopen: state=%s report=%s", row.State, row.Report)
	}

	freshEngine.mu.Lock()
	actual := freshEngine.spec.Workspace
	freshEngine.mu.Unlock()
	if readFile(t, filepath.Join(actual, "unfinished.txt")) != "retained bytes\n" || readFile(t, filepath.Join(actual, "..", "library", "input.txt")) != "dependency\n" {
		t.Fatal("reopen lost sibling output or input")
	}
	endBeltRun(t, fresh, freshEngine)
}

func TestWorkspaceSetForkHistoryDropsEveryExecutionAuthority(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := Place{Dir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(place.Dir, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	document := taskDocument{Type: taskDocumentType, Version: 2, Seq: 1, Runs: []runRecord{{ID: 1, State: TaskRunning, WorkspaceSet: &WorkspaceSet{Version: 1, Primary: "app", Roots: []WorkspaceRootUse{{Slot: "app", Access: WorkspaceWrite, Landing: "source-authority"}}}}}}
	raw, _ := json.Marshal(document)
	if err := os.WriteFile(place.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ForkTaskHistory(place); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(place.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTasks(after)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != 1 || decoded.WorkspaceTransfer != nil || len(decoded.Runs) != 1 || decoded.Runs[0].WorkspaceSet != nil || decoded.Runs[0].Copy != nil {
		t.Fatal("independent history retained execution authority")
	}
	original, err := os.ReadFile(filepath.Join(ForkHistoryDir(place), "tasks.json"))
	if err != nil || string(original) != string(raw) {
		t.Fatal("fork failed to retain exact original metadata")
	}
}

func TestWorkspaceSetMixedLegacyCopiesRefuseWithoutReplacingCheckpoint(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := Place{Dir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(place.Dir, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"type":"tasks","version":1,"seq":0}`)
	if err := os.WriteFile(place.Tasks(), original, 0600); err != nil {
		t.Fatal(err)
	}
	document := taskDocument{Type: taskDocumentType, Version: 2, Seq: 2, Runs: []runRecord{{ID: 1, State: TaskDone, Copy: &TaskCopyRecord{URI: "legacy-copy"}}, {ID: 2, State: TaskRunning, WorkspaceSet: &WorkspaceSet{Version: 1, Primary: "app", Roots: []WorkspaceRootUse{{Slot: "app", Access: WorkspaceWrite, Copy: &TaskCopyRecord{URI: "set-copy"}}}}}}}
	if err := (&taskStore{path: place.Tasks()}).writeLocked(document); err == nil || !strings.Contains(err.Error(), "mixed legacy") {
		t.Fatalf("mixed legacy copies were not explicitly refused: %v", err)
	}
	after, _ := os.ReadFile(place.Tasks())
	if string(after) != string(original) {
		t.Fatal("mixed refusal replaced original checkpoint")
	}
	if _, err := workspaceTransferProjectionFor(document, place, "incoming-chat"); err == nil {
		t.Fatal("receiver projection silently omitted mixed legacy copies")
	}
}
