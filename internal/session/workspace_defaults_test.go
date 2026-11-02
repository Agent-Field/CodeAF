package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func ordinaryTaskFixture(t *testing.T) (*Agent, *beltRunDouble, Place, cell.Cell) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	project := newTestRepo(t)
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: c.Root, Workspace: project}
	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), Workspace: project}); err != nil {
		t.Fatal(err)
	}
	engine := newBeltRunDouble("done")
	engine.real = true
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place, c.SessionFile = project, place, place.Transcript()
	})
	if _, _, _, err := agent.StartTask(context.Background(), "write result.txt", true); err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	t.Cleanup(func() {
		select {
		case <-engine.release:
		default:
			endBeltRun(t, agent, engine)
		}
	})
	return agent, engine, place, c
}

func TestOrdinaryDelegatedEditComesHomeWithoutACommand(t *testing.T) {
	agent, engine, place, _ := ordinaryTaskFixture(t)
	engine.mu.Lock()
	spec := engine.spec
	engine.mu.Unlock()
	if spec.WorkspaceSet != nil {
		t.Fatal("ordinary task received a private workspace set")
	}
	writeFile(t, filepath.Join(spec.Workspace, "result.txt"), "delivered\n")
	endBeltRun(t, agent, engine)
	got, err := os.ReadFile(filepath.Join(place.Workspace, "result.txt"))
	if err != nil || string(got) != "delivered\n" {
		t.Fatalf("task did not come home: %q, %v", got, err)
	}
	g := agent.graph()
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	if len(rows) != 1 || rows[0].Merge != mergeMerged || rows[0].Branch != currentBranch(place.Workspace) {
		t.Fatalf("landing receipt: %+v", rows)
	}
	meta, err := LoadMeta(place.Dir)
	if err != nil || meta.WorkspacePolicy != nil || meta.ChatURI != "" {
		t.Fatalf("ordinary chat gained ownership: %+v, %v", meta, err)
	}
}

func TestOrdinaryTaskCheckpointAndCarrierRemainReadableByOlderBuilds(t *testing.T) {
	_, engine, place, c := ordinaryTaskFixture(t)
	engine.mu.Lock()
	workspace := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(workspace, "result.txt"), "carried\n")
	if err := (taskcopy.Carry{Project: place.Workspace}).Compose(c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(place.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.Version != 1 {
		t.Fatalf("older reader requires version 1: %s", raw)
	}
	records, err := filepath.Glob(filepath.Join(c.Root, ".cell", "trees", "*", "tree.json"))
	if err != nil || len(records) != 1 {
		t.Fatalf("older reader cannot find task carrier: %v, %v", records, err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, ".cell", "workspaces-v2")); !os.IsNotExist(err) {
		t.Fatalf("ordinary task created a workspace carrier: %v", err)
	}
}

type ordinarySealTransport struct{}

func (ordinarySealTransport) Do(_ context.Context, _ cellstore.Target, op cellstore.Op) ([]byte, error) {
	if op.Verb() == "capture-plan" {
		return []byte(`{"token":"ordinary-seal","full":true,"paths":[]}`), nil
	}
	return []byte(`{"snapshot":"` + strings.Repeat("a", 64) + `","durable":true}`), nil
}

func TestOrdinaryFinishedTaskSealsCreateNoWorkspaceGeneration(t *testing.T) {
	agent, engine, place, c := ordinaryTaskFixture(t)
	endBeltRun(t, agent, engine)
	store := cellstore.Engine{DataRoot: t.TempDir(), Workspace: place.Workspace, Compose: taskcopy.Carry{}, Transport: ordinarySealTransport{}}
	for range 2 {
		if _, err := store.Seal(context.Background(), c, cellstore.TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(c.Root, ".cell", "workspaces-v2")); !os.IsNotExist(err) {
			t.Fatalf("idle seal created workspace carrier work: %v", err)
		}
	}
}

func TestPlatformWithoutApplyKeepsOwnershipOff(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := Place{Dir: t.TempDir()}
	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
		t.Fatal(err)
	}
	if workspaceOwnershipEnabled(place, false) {
		t.Fatal("unsupported platform selected private placement for a persisted policy")
	}
	if !workspaceOwnershipEnabled(place, true) {
		t.Fatal("supported platform lost the persisted policy")
	}
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	if !workspaceOwnershipEnabled(place, true) {
		t.Fatal("persisted policy depends on the receiver's switch")
	}
}

func TestTakenConversationWithoutPolicyKeepsLegacyPlacement(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	source := Place{Dir: t.TempDir(), Workspace: t.TempDir()}
	if err := os.Mkdir(filepath.Join(source.Dir, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(source.Dir, Meta{ID: source.ID(), Workspace: source.Workspace}); err != nil {
		t.Fatal(err)
	}
	receiver := Place{Dir: t.TempDir(), Workspace: t.TempDir()}
	if err := os.CopyFS(filepath.Join(receiver.Dir, ".cell"), os.DirFS(filepath.Join(source.Dir, ".cell"))); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := AdoptChatWorkspaceIdentity(receiver); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place, c.Workspace, c.SessionFile = receiver, receiver.Workspace, receiver.Transcript()
	})
	if WorkspaceOwnershipEnabled(receiver) || agent.config.WorkspaceURI != "" {
		t.Fatal("taking an ordinary chat enabled private placement")
	}
	stand := taskStand{dir: receiver.Workspace, mode: TaskModeMirror}
	got, err := agent.ownedRunPlacement(context.Background(), stand)
	if err != nil || got.workspaceSet != nil {
		t.Fatalf("receiver changed task placement: %+v, %v", got, err)
	}
}

func TestOrdinaryChatReviewAndApplyExplainPrivateCopies(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Place = Place{Dir: t.TempDir()} })
	_, reviewErr := agent.ReviewTaskWorkspaceRoot(context.Background(), 1, "work")
	_, applyErr := agent.ApplyTaskWorkspaceRoot(context.Background(), 1, "work")
	for _, err := range []error{reviewErr, applyErr} {
		if err == nil || err.Error() != "review and apply are for private task copies; this chat doesn't have them" {
			t.Fatalf("ordinary chat explanation: %v", err)
		}
	}
}

func TestOrdinaryChatKeepsItsTemporaryAndCacheEnvironment(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	scratch, cache := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Setenv("XDG_CACHE_HOME", cache)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Place = Place{Dir: t.TempDir()} })
	result, err := agent.config.seat().In(agent.config.Workspace).Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", `printf '%s\n' "$TMPDIR" "$XDG_CACHE_HOME"`}}, nil)
	if err != nil || result.Exit != 0 || string(result.Stdout) != scratch+"\n"+cache+"\n" {
		t.Fatalf("ordinary environment changed: %+v, %v", result, err)
	}
}

func TestOrdinaryChatOmitsAndRefusesModelWorkspaceRoots(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Place = Place{Dir: t.TempDir()} })
	tools := agent.taskTools()
	if len(tools) != 1 || strings.Contains(string(tools[0].Schema), "workspace_set") {
		t.Fatal("unavailable workspace roots were advertised")
	}
	args := json.RawMessage(`{"title":"Use roots","summary":"Use private roots","brief":"Write output","deliverable":"output","acceptance":"output exists","workspace_set":{"primary":"work","roots":[{"source":"` + filepath.ToSlash(t.TempDir()) + `","slot":"work","access":"write"}]}}`)
	line, refused, err := bare.RunStaged(context.Background(), agent.stageTask(context.Background(), args))
	if err != nil || !refused || !strings.Contains(line, "requires private task placement") {
		t.Fatalf("unavailable roots were not refused: %q, %v, %v", line, refused, err)
	}
}
