package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func ownedLegacyNode(t *testing.T) (*Agent, *TaskNode, taskTree) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "example")}
	project := t.TempDir()
	path := filepath.Join(place.Trees(), "1")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(path, "proof.txt"), "retained task bytes\n")
	tree, err := registerOwnedWorkspace(taskTree{dir: path, ground: project, mode: TaskModeMirror, merge: mergeInPlace, place: place}, place)
	if err != nil {
		t.Fatal(err)
	}
	agent, node := unverifiedNode(t, func(config *Config) { config.Workspace = project; config.Place = place })
	node.setTree(tree)
	return agent, node, tree
}

func TestLegacyOwnedTaskPublishesWorkspaceIdentity(t *testing.T) {
	agent, node, tree := ownedLegacyNode(t)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	select {
	case event := <-lane:
		if event.Task == nil || event.Task.ID != node.id {
			t.Fatal("missing public task notice")
		}
		raw, err := json.Marshal(event.Task)
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]any
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record["WorkspaceURI"] != tree.uri || record["ChatURI"] != tree.chatURI || record["ProjectURI"] != tree.projectURI {
			t.Fatalf("public legacy task notice dropped its workspace identity: %s", raw)
		}
		if event.Task.Copy != nil {
			t.Fatal("legacy identity must not enable run-copy UI semantics")
		}
	case <-time.After(time.Second):
		t.Fatal("public task roster did not arrive")
	}
}

func TestLegacyOwnedTaskCheckpointReopensReboundCopy(t *testing.T) {
	agent, node, tree := ownedLegacyNode(t)
	raw, err := json.Marshal(node.graph.document())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint taskDocument
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.Nodes) != 1 {
		t.Fatal("missing checkpoint task")
	}
	moved := filepath.Join(agent.config.Place.Trees(), "rebound")
	if err := os.Rename(tree.dir, moved); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Bind(tree.uri, moved); err != nil {
		t.Fatal(err)
	}
	restored := restoreNode(node.graph, checkpoint.Nodes[0])
	reopened, err := restored.workingCopy(agent.config.Place, agent.config.Workspace)
	if err != nil || reopened.dir != moved || reopened.uri != tree.uri {
		t.Fatalf("checkpoint reopening lost identity binding: %+v %v", reopened, err)
	}
	if raw, err := os.ReadFile(filepath.Join(reopened.dir, "proof.txt")); err != nil || string(raw) != "retained task bytes\n" {
		t.Fatal("checkpoint reopened different task bytes")
	}
}

func TestLegacyOwnedTaskUnknownIdentityRefusesFreshWorld(t *testing.T) {
	agent, node, tree := ownedLegacyNode(t)
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	// A checkpoint carrying an unknown identity must not use its still-existing
	// old directory, nor prepare a replacement from the project.
	raw, err := json.Marshal(node.graph.document())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	nodes := document["nodes"].([]any)
	record := nodes[0].(map[string]any)
	record["workspace_uri"] = "codeaf-workspace://foreign/unknown"
	record["chat_uri"] = tree.chatURI
	record["project_uri"] = tree.projectURI
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint taskDocument
	if err = json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	restored := restoreNode(node.graph, checkpoint.Nodes[0])
	if _, err := restored.workingCopy(agent.config.Place, agent.config.Workspace); err == nil {
		t.Fatal("unknown task URI silently fell back to existing path")
	}
	if _, opened := agent.openTaskWorld(context.Background(), restored, io.Discard); opened {
		t.Fatal("unknown task URI prepared a replacement world")
	}
	if raw, err := os.ReadFile(filepath.Join(tree.dir, "proof.txt")); err != nil || string(raw) != "retained task bytes\n" {
		t.Fatal("refused task identity changed retained bytes")
	}
	if _, err := registry.Resolve(tree.uri); err != nil {
		t.Fatal("unknown identity damaged existing workspace registration")
	}
}

func TestLegacyOwnedWorkerUsesItsAdmittedRuntime(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("runtime", "bash", `{"command":"printf '%s\\n' \"$CODEAF_WORKSPACE_URI\" \"$TMPDIR\" \"$XDG_CACHE_HOME\" > runtime.txt; mktemp >> runtime.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "active-worker")}
	project := t.TempDir()
	agent, _ := newTestAgent(t, script, func(config *Config) { config.Place = place; config.Workspace = project })
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Workspace runtime", brief: "Record the workspace environment", acceptance: "runtime recorded", model: "test/model", depth: 1})
	node := graph.node(id)
	path := filepath.Join(place.Trees(), "1")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	tree, err := registerOwnedWorkspace(taskTree{dir: path, ground: project, mode: TaskModeMirror, merge: mergeInPlace, place: place}, place)
	if err != nil {
		t.Fatal(err)
	}
	node.setTree(tree)
	worker, err := agent.newTaskAgent(context.Background(), tree.dir, node, "")
	if err != nil {
		t.Fatalf("legacy worker construction: %v", err)
	}
	defer worker.Close()
	drainTurn(t, worker, "Record the workspace environment")
	raw, err := os.ReadFile(filepath.Join(tree.dir, "runtime.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 || lines[0] != tree.uri || filepath.Dir(lines[3]) != lines[1] {
		t.Fatalf("legacy worker did not receive admitted workspace and temporary directory: %q", raw)
	}
	for _, path := range lines[1:3] {
		if !withinDir(home.Dir(), path) || withinDir(tree.dir, path) {
			t.Fatalf("legacy worker scratch/cache path outside owned runtime: %s", path)
		}
	}
	if _, err := os.Stat(lines[3]); err != nil {
		t.Fatal("legacy worker did not create its actual temporary file")
	}
}

func TestLegacyNestedTaskKeepsItsParentGroundIdentity(t *testing.T) {
	agent, node, parent := ownedLegacyNode(t)
	childPath := filepath.Join(agent.config.Place.Trees(), "child")
	if err := os.MkdirAll(childPath, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(childPath, "proof.txt"), "child bytes\n")
	child, err := registerOwnedWorkspace(taskTree{dir: childPath, root: parent.dir, ground: parent.dir, branch: "task/child", mode: TaskModeWorktree}, agent.config.Place)
	if err != nil {
		t.Fatal(err)
	}
	node.setTree(child)
	raw, err := json.Marshal(node.graph.document())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint taskDocument
	if err = json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(agent.config.Place.Trees(), "parent-rebound")
	if err := os.Rename(parent.dir, moved); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Bind(parent.uri, moved); err != nil {
		t.Fatal(err)
	}
	restored := restoreNode(node.graph, checkpoint.Nodes[0])
	copy, err := restored.workingCopy(agent.config.Place, agent.config.Workspace)
	if err != nil || copy.ground != moved || copy.root != moved || copy.projectURI != parent.projectURI || copy.groundURI != parent.uri {
		t.Fatalf("nested task lost parent landing identity after checkpoint: %+v %v", copy, err)
	}
}

func TestLegacyOwnedWorkersCanShareTheirConversationJournalDirectory(t *testing.T) {
	agent, node, first := ownedLegacyNode(t)
	worker, err := agent.newTaskAgent(context.Background(), first.dir, node, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Second workspace", brief: "second task", acceptance: "done", model: "test/model", depth: 1})
	second := graph.node(id)
	path := filepath.Join(agent.config.Place.Trees(), "second")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	tree, err := registerOwnedWorkspace(taskTree{dir: path, ground: first.ground, mode: TaskModeMirror, merge: mergeInPlace, place: agent.config.Place}, agent.config.Place)
	if err != nil {
		t.Fatal(err)
	}
	second.setTree(tree)
	next, err := agent.newTaskAgent(context.Background(), tree.dir, second, "")
	if err != nil {
		t.Fatalf("second legacy task cannot use its conversation's shared journal directory: %v", err)
	}
	defer next.Close()
	if filepath.Dir(agent.TaskJournal(node.id)) != filepath.Dir(agent.TaskJournal(second.id)) {
		t.Fatal("legacy task journal placement changed")
	}
}
