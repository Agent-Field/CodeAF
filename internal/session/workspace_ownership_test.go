package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A model's folder argument names the project, not permission to edit it while
// the task is running. The prepared brief must reach the worker's own copy.
func TestOwnershipProposalCopiesPlainProjectBeforeWorkerWrites(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "BRIEF.md"), "prepared input\n")
	place := Place{Dir: t.TempDir(), Workspace: project}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = t.TempDir()
		c.Place = place
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	world, release := heldTaskWorld(t, a, "result.txt", "worker output\n")
	args, _ := json.Marshal(taskArguments{Title: "copy the project", Summary: "use the prepared input",
		Ground: project, Where: project, Brief: "Read BRIEF.md and write result.txt",
		Deliverable: "result.txt", Acceptance: "result.txt has the result"})
	answer, bad, err := a.proposeTask(context.Background(), args)
	if err != nil || bad {
		t.Fatalf("proposal: %s, %v", answer, err)
	}
	tree := <-world
	if !withinDir(place.Trees(), tree.dir) || tree.mode != TaskModeMirror {
		t.Fatalf("worker did not get an owned mirror: %+v", tree)
	}
	if got := readFile(t, filepath.Join(tree.dir, "BRIEF.md")); got != "prepared input\n" {
		t.Fatalf("worker input = %q", got)
	}
	if _, err := os.Stat(filepath.Join(project, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("worker wrote original while running: %v", err)
	}
	if tree.uri == "" || tree.chatURI == "" || tree.projectURI == "" {
		t.Fatalf("task copy has no durable ownership identity: %+v", runCopyOf(tree))
	}
	release()
	waitDoneNode(t, a.graph().node(1))
}

func TestOwnershipCooperatingTasksShareTheAdmittedRunIdentity(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "BRIEF.md"), "shared project\n")
	place := Place{Dir: t.TempDir(), Workspace: project}
	a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place = project, place
	})
	stand := a.resolveTaskGround(taskSpec{ground: project, where: project, deliverable: "result.txt"})
	if err := a.startKnownTaskRun(context.Background(), 1, "shared work", "brief", nil, stand, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	t.Cleanup(func() { endBeltRun(t, a, double) })
	if err := a.startKnownTaskRun(context.Background(), 2, "cooperating work", "brief", nil, stand, ""); err != nil {
		t.Fatal(err)
	}
	root, ok := runRowOf(a.graph(), 1)
	child, joined := runRowOf(a.graph(), 2)
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if !ok || root.Copy == nil || root.Copy.URI == "" || spec.WorkspaceURI != root.Copy.URI {
		t.Fatalf("worker did not receive the recorded run identity: %#v / %q", root.Copy, spec.WorkspaceURI)
	}
	if !joined || child.Parent != 1 || spec.Workspace != root.Copy.Dir {
		t.Fatalf("cooperating task did not join the existing workspace: %#v", child)
	}
}

func TestOwnershipExplicitPersonInPlaceChoiceIsStillHonoured(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = t.TempDir()
		c.Place = Place{Dir: t.TempDir()}
	})
	if _, err := a.ReferPlace(project, PlaceSaid); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPlaceMode(project, "in place"); err != nil {
		t.Fatal(err)
	}
	stand := a.resolveTaskGround(taskSpec{ground: project, where: project, deliverable: "result.txt"})
	if stand.mode != TaskModeInPlace || stand.dir != canonicalPath(project) {
		t.Fatalf("person's explicit placement was ignored: %+v", stand)
	}
	tree, err := prepareTaskTreeOn(context.Background(), a.config.Place, a.config.Workspace, a.journalID(), 1, "in place", stand)
	if err != nil || tree.dir != canonicalPath(project) || tree.uri != "" {
		t.Fatalf("borrowed project was mislabeled an owned run: %+v, %v", tree, err)
	}
}

func TestOwnershipTaskFromHomeDoesNotCopyUnrelatedPersonalFiles(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	personHome := t.TempDir()
	t.Setenv("HOME", personHome)
	writeFile(t, filepath.Join(personHome, "unrelated.txt"), "mock unrelated material\n")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = personHome
		c.Place = Place{Dir: t.TempDir()}
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	world, release := heldTaskWorld(t, a, "reply.txt", "mock reply\n")
	args, _ := json.Marshal(taskArguments{Title: "write a short reply", Summary: "compose a reply",
		Brief: "Write reply.txt with a fictional short reply", Deliverable: "reply.txt", Acceptance: "the reply exists"})
	answer, bad, err := a.proposeTask(context.Background(), args)
	if err != nil || bad {
		t.Fatalf("proposal: %s, %v", answer, err)
	}
	tree := <-world
	if _, err := os.Stat(filepath.Join(tree.dir, "unrelated.txt")); !os.IsNotExist(err) {
		t.Fatalf("home-only task copied unrelated personal material: %v", err)
	}
	release()
	waitDoneNode(t, a.graph().node(1))
}
