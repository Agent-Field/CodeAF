package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

func TestAnchoringAnOwnedSessionPersistsAndReloadsProjectInstructions(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, agentsFileName), "agent instruction\n")
	writeFile(t, filepath.Join(repo, claudeFileName), "claude instruction\n")

	dir := t.TempDir()
	place := Place{Dir: dir, Owned: true}
	place.Workspace = place.Work()
	if err := os.MkdirAll(place.Work(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(dir, Meta{ID: filepath.Base(dir), Workspace: place.Work(), Owned: true}); err != nil {
		t.Fatal(err)
	}
	agent, err := newAgent(Config{
		Workspace: place.Work(), Place: place, SessionFile: place.Transcript(), Model: "test/model",
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	var anchorTool *bare.Tool
	for index := range agent.tools {
		if agent.tools[index].Name == "workspace" {
			anchorTool = &agent.tools[index]
			break
		}
	}
	if anchorTool == nil {
		t.Fatal("an owned conversation has no workspace tool")
	}
	arguments, err := json.Marshal(map[string]string{"path": filepath.Join(repo, "subdir", "..")})
	if err != nil {
		t.Fatal(err)
	}
	result, isError, err := anchorTool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if isError || !strings.HasPrefix(result, "Workspace set to ") {
		t.Fatalf("workspace tool = %q, error=%v", result, isError)
	}
	resolved := strings.TrimPrefix(result, "Workspace set to ")
	if agent.config.Workspace != resolved || agent.config.Place.Owned {
		t.Fatalf("anchor = %q, config = %+v", resolved, agent.config.Place)
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Workspace != resolved || meta.Owned {
		t.Fatalf("persisted place = %+v", meta)
	}
	agent.mu.Lock()
	system := agent.system
	first := messageText(agent.messages[0])
	agent.mu.Unlock()
	for _, want := range []string{"Working directory: " + resolved, "agent instruction", "claude instruction"} {
		if !strings.Contains(system, want) || !strings.Contains(first, want) {
			t.Fatalf("re-anchored prompt does not contain %q:\n%s", want, system)
		}
	}
	for _, tool := range agent.tools {
		if tool.Name == "workspace" {
			t.Fatal("the one-shot workspace tool remained after anchoring")
		}
	}

	tree, err := prepareTaskTreeAt(context.Background(), agent.config.Place, agent.config.Workspace, agent.journalID(), 7, "anchored work", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(place.Trees(), "7"); tree.dir != want {
		t.Fatalf("anchored task tree = %q, want %q", tree.dir, want)
	}
	if list := gitOut(t, resolved, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("anchored repository does not register the task tree:\n%s", list)
	}
	if merge, detail := tree.comeHome("anchored work", nil); merge != mergeMerged {
		t.Fatalf("cleanup merge = %q: %s", merge, detail)
	}
}
