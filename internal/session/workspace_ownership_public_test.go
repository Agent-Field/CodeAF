package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func TestOwnershipStartTaskUsesPrivateWorkspaceSetWithoutPrototypeFlag(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "input.txt"), []byte("input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: t.TempDir(), Workspace: project}
	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), Workspace: project, WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
		t.Fatal(err)
	}
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = project, place
		c.SessionFile = place.Transcript()
	})
	if _, _, _, err := agent.StartTask(context.Background(), "write result.txt", true); err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	ended := false
	t.Cleanup(func() {
		if !ended {
			endBeltRun(t, agent, engine)
		}
	})
	engine.mu.Lock()
	spec := engine.spec
	engine.mu.Unlock()
	if spec.WorkspaceSet == nil || len(spec.WorkspaceSet.Roots) != 1 {
		t.Fatal("ordinary task did not receive an owned workspace set")
	}
	root := spec.WorkspaceSet.Roots[0]
	if root.Access != WorkspaceWrite || root.Landing != "" || root.Copy == nil || root.Copy.Dir == project {
		t.Fatalf("ordinary task did not retain writes privately: %+v", root)
	}
	if _, _, _, err := agent.StartTask(context.Background(), "also write another.txt", true); err != nil {
		t.Fatalf("cooperator: %v", err)
	}
	plan, _, err := taskcopy.ReadWorkspacePlan(cell.Cell{Root: place.Dir})
	if err != nil || plan == nil {
		t.Fatalf("ordinary tasks have no transferable closure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spec.Workspace, "result.txt"), []byte("private result\n"), 0600); err != nil {
		t.Fatal(err)
	}
	endBeltRun(t, agent, engine)
	ended = true
	if _, err := os.Stat(filepath.Join(project, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("ordinary task landed without permission: %v", err)
	}
	next := newBeltRunDouble("done")
	registerBeltRunEngine(t, next)
	other := t.TempDir()
	request := WorkspaceSetRequest{Primary: "app", Roots: []WorkspaceRootRequest{{Source: project, Slot: "app", Access: WorkspaceWrite}, {Source: other, Slot: "library", Access: WorkspaceRead}}}
	if _, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "use both roots", request); err != nil {
		t.Fatal(err)
	}
	<-next.entered
	t.Cleanup(func() { endBeltRun(t, agent, next) })
	if _, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "cooperate on exact roots", request); err != nil {
		t.Fatal(err)
	}
	request.Roots[1].Access = WorkspaceWrite
	if _, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "broaden library access", request); err == nil {
		t.Fatal("cooperator broadened read authority")
	}
	plan, _, err = taskcopy.ReadWorkspacePlan(cell.Cell{Root: place.Dir})
	if err != nil || plan == nil || len(plan.Groups) < 2 {
		t.Fatalf("ordinary and multiroot history cannot share a checkpoint: %+v %v", plan, err)
	}
}

func TestOwnershipStartTaskReopensPendingWorkspaceSet(t *testing.T) {
	littleMemoryHost(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	project := t.TempDir()
	place := Place{Dir: t.TempDir(), Workspace: project}
	engine := newBeltRunDouble("must remain queued")
	registerBeltRunEngine(t, engine)
	profile := t.TempDir()
	open := func() *Agent {
		agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace, c.Place, c.SessionFile = project, place, place.Transcript()
			c.ProfileDir, c.TaskMinFreeMB = profile, 1<<40
		})
		return agent
	}
	first := open()
	id, _, _, err := first.StartTask(context.Background(), "queued private output", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := open()
	defer second.Close()
	raw, err := os.ReadFile(place.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint struct {
		Runs []struct {
			ID      uint64 `json:"id"`
			Pending *struct {
				WorkspaceSet *WorkspaceSetRequest `json:"workspaceSet"`
			} `json:"pendingRun"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range checkpoint.Runs {
		if row.ID == id && row.Pending != nil && row.Pending.WorkspaceSet != nil {
			set := row.Pending.WorkspaceSet
			found = len(set.Roots) == 1 && set.Primary == "work" && set.Roots[0].SourceURI != "" && set.Roots[0].Landing == "" && set.Roots[0].LandingURI == ""
		}
	}
	if !found {
		t.Fatalf("reopened ordinary task lost pending owned layout: %s", raw)
	}
	if engine.didRun() {
		t.Fatal("held task ran before admission")
	}
	entries, err := os.ReadDir(place.Trees())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("held task carved its copy before admission")
	}
}

func TestOwnershipStartTaskPreservesExplicitInPlaceAndHomeScratch(t *testing.T) {
	for _, choice := range []string{"in place", "home"} {
		t.Run(choice, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			t.Setenv("CODEAF_TASK_BELT", "bash")
			project := t.TempDir()
			if choice == "home" {
				t.Setenv("HOME", project)
			}
			if err := os.WriteFile(filepath.Join(project, "personal.txt"), []byte("borrowed\n"), 0600); err != nil {
				t.Fatal(err)
			}
			place := Place{Dir: t.TempDir(), Workspace: project}
			engine := newBeltRunDouble("done")
			registerBeltRunEngine(t, engine)
			agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place = project, place })
			if choice == "in place" {
				if _, err := agent.ReferPlace(project, PlaceSaid); err != nil {
					t.Fatal(err)
				}
				if err := agent.SetPlaceMode(project, "in place"); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, _, err := agent.StartTask(context.Background(), "write output.txt", true); err != nil {
				t.Fatal(err)
			}
			<-engine.entered
			t.Cleanup(func() { endBeltRun(t, agent, engine) })
			engine.mu.Lock()
			spec := engine.spec
			engine.mu.Unlock()
			if spec.WorkspaceSet != nil {
				t.Fatal("explicit in-place or home reference was converted into a snapshot")
			}
			if choice == "in place" && spec.Workspace != project {
				t.Fatalf("explicit placement moved to %s", spec.Workspace)
			}
			if choice == "home" {
				if spec.Workspace == project {
					t.Fatal("home task works in the person's home")
				}
				if _, err := os.Stat(filepath.Join(spec.Workspace, "personal.txt")); !os.IsNotExist(err) {
					t.Fatalf("home was copied into task scratch: %v", err)
				}
			}
		})
	}
}
