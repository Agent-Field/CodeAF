package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/home"
)

// identityPlace is a conversation folder that already holds a conversation,
// the way the app leaves one it has created.
func identityPlace(t *testing.T, id string) Place {
	t.Helper()
	place := Place{Dir: filepath.Join(t.TempDir(), id)}
	if err := SaveMeta(place.Dir, Meta{ID: id, Workspace: t.TempDir(), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return place
}

func openIdentityAgent(t *testing.T, place Place, completer Completer) *Agent {
	t.Helper()
	if completer == nil {
		completer = &scriptedCompleter{}
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Place = place })
	return agent
}

func TestAgentDIDIsMintedOnceAndSurvivesReopening(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "0123456789abcdef")
	first := openIdentityAgent(t, place, nil)
	did := first.AgentDID()
	if !agentdid.Valid(did) {
		t.Fatalf("a conversation opened without a valid DID: %q", did)
	}
	if opened, _ := LoadMeta(place.Dir); opened.AgentIdentity != nil {
		t.Fatal("opening a conversation wrote to its files")
	}
	first.stampMeta()
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AgentDID() != did {
		t.Fatalf("the DID was not kept with the conversation: disk %q, agent %q", meta.AgentDID(), did)
	}
	_ = first.Close()

	again := openIdentityAgent(t, place, nil)
	if got := again.AgentDID(); got != did {
		t.Fatalf("reopening changed the DID: %q, then %q", did, got)
	}
}

func TestAgentDIDIsUniquePerConversation(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	one := openIdentityAgent(t, identityPlace(t, "1111111111111111"), nil).AgentDID()
	two := openIdentityAgent(t, identityPlace(t, "2222222222222222"), nil).AgentDID()
	if one == "" || two == "" || one == two {
		t.Fatalf("two conversations share an identity: %q and %q", one, two)
	}
}

// A copy or branch carries the old files under a new id; it must not keep the
// original's identity.
func TestAgentDIDIsNotCarriedToANewID(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "3333333333333333")
	agent := openIdentityAgent(t, place, nil)
	did := agent.AgentDID()
	agent.stampMeta()
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AgentDID() != did {
		t.Fatalf("the DID was not written: %q", meta.AgentDID())
	}
	meta.ID = "4444444444444444"
	if err := SaveMeta(place.Dir, meta); err != nil {
		t.Fatal(err)
	}
	copied, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if copied.AgentDID() != "" {
		t.Fatalf("a copy under a new id kept the original DID %q", did)
	}
}

// A save that brings another identity never replaces the one on disk.
func TestAgentDIDIsNeverReplaced(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "5555555555555555")
	agent := openIdentityAgent(t, place, nil)
	did := agent.AgentDID()
	agent.stampMeta()
	other, err := agentdid.New()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentIdentity = &AgentIdentity{DID: other, Session: meta.ID}
	if err := SaveMeta(place.Dir, meta); err != nil {
		t.Fatal(err)
	}
	if kept, _ := LoadMeta(place.Dir); kept.AgentDID() != did {
		t.Fatalf("a save replaced the DID: %q became %q", did, kept.AgentDID())
	}
}

// A folder with no conversation in it yet is not written at open; the first
// identity write carries the DID the agent already uses.
func TestAgentDIDOfANewFolderIsWrittenWithItsFirstIdentity(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := Place{Dir: filepath.Join(t.TempDir(), "6666666666666666")}
	agent := openIdentityAgent(t, place, nil)
	did := agent.AgentDID()
	if !agentdid.Valid(did) {
		t.Fatalf("a new conversation has no DID: %q", did)
	}
	agent.stampMeta()
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AgentDID() != did {
		t.Fatalf("the first identity write did not record the DID: disk %q, agent %q", meta.AgentDID(), did)
	}
}

func bashWritesAgentDID(file string) *scriptedCompleter {
	return &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("did", "bash", `{"command":"printf '%s' \"$CODEAF_AGENT_DID\" > `+file+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
}

func TestToolCommandsSeeTheAgentDID(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "7777777777777777")
	agent, workspace := newTestAgent(t, bashWritesAgentDID("did.txt"), func(config *Config) { config.Place = place })
	drainTurn(t, agent, "Write your identity")
	raw, err := os.ReadFile(filepath.Join(workspace, "did.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != agent.AgentDID() || !agentdid.Valid(string(raw)) {
		t.Fatalf("the command saw %q, the agent is %q", raw, agent.AgentDID())
	}
}

func TestTaskWorkersActAsTheAgentWithTheirTaskNumber(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "8888888888888888")
	agent := openIdentityAgent(t, place, nil)
	did := agent.AgentDID()
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Identity", brief: "Write your identity", acceptance: "identity written", model: "test/model", depth: 1})
	node := graph.node(id)
	dir := t.TempDir()
	worker, err := agent.newTaskAgent(context.Background(), dir, node, "")
	if err != nil {
		t.Fatalf("worker construction: %v", err)
	}
	defer worker.Close()
	want := did + "#w" + strconv.FormatUint(id, 10)
	if got := worker.AgentDID(); got != want {
		t.Fatalf("worker DID = %q, want %q", got, want)
	}
	if !strings.HasPrefix(want, did+"#w") || agentdid.Valid(want) {
		t.Fatalf("a worker DID must hang from the agent's: %q", want)
	}
}

func TestTaskWorkerCommandsSeeTheWorkerDID(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "9999999999999999")
	// The parent never takes a turn; its worker shares its scripted completer.
	agent := openIdentityAgent(t, place, bashWritesAgentDID("worker-did.txt"))
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Identity", brief: "Write your identity", acceptance: "identity written", model: "test/model", depth: 1})
	node := graph.node(id)
	dir := t.TempDir()
	worker, err := agent.newTaskAgent(context.Background(), dir, node, "")
	if err != nil {
		t.Fatalf("worker construction: %v", err)
	}
	defer worker.Close()
	drainTurn(t, worker, "Write your identity")
	raw, err := os.ReadFile(filepath.Join(dir, "worker-did.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := agent.AgentDID() + "#w" + strconv.FormatUint(id, 10); string(raw) != want {
		t.Fatalf("the worker's command saw %q, want %q", raw, want)
	}
}

// EVERY TASK UPDATE NAMES WHO THE TASK ACTS AS. A surface that keeps something
// under a worker's name (a writer's lease) has to know what to let go of when
// the task ends, and the name on the update is the one the worker's commands
// carry: the agent's DID with the task's fragment.
func TestTaskUpdatesNameTheWorkerTheTaskActsAs(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "6666666666666666")
	agent := openIdentityAgent(t, place, nil)
	events := watched(agent)
	agent.emitTaskUpdate(TaskNotice{ID: 4, Title: "Opening scene", State: TaskDone})
	select {
	case event := <-events:
		if event.Task == nil || event.Task.Worker != agent.AgentDID()+"#w4" {
			t.Fatalf("the update names %+v, want %q", event.Task, agent.AgentDID()+"#w4")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no task update")
	}
}

// bashWritesAgentWorkspace writes the folder the command was told is the
// agent's, after a `cd` away from it when away is set (a task worker's write
// guard refuses a redirect it cannot place, so its command stays put).
func bashWritesAgentWorkspace(file string, away bool) *scriptedCompleter {
	command := `printf '%s' \"$CODEAF_AGENT_WORKSPACE\" > ` + file
	if away {
		command = `(cd / && printf '%s' \"$CODEAF_AGENT_WORKSPACE\") > ` + file
	}
	return &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("ws", "bash", `{"command":"`+command+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
}

// THE FOLDER TRAVELS WITH THE DID. The app makes an agent a canvas member by
// the folder it lists the agent in before it has seen the agent's DID, and the
// `canvas` command admits the DID by that folder. It used to read the folder
// from the command's own directory, so a Claude model that ran `cd /tmp`
// before its write was refused on every one (connected-canvas, 2026-11-18).
// The engine now says which folder is the agent's, whatever the command does.
func TestToolCommandsSeeTheAgentsFolderWhereverTheyRun(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "7777777777777778")
	agent, workspace := newTestAgent(t, bashWritesAgentWorkspace("ws.txt", true), func(config *Config) { config.Place = place })
	drainTurn(t, agent, "Write your folder")
	raw, err := os.ReadFile(filepath.Join(workspace, "ws.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != workspace {
		t.Fatalf("the command saw folder %q, the agent works in %q", raw, workspace)
	}
	if env := agent.config.identityEnv(); len(env) != 2 || env[1] != agentdid.AgentWorkspaceEnv+"="+workspace {
		t.Fatalf("the identity a Claude Code session is handed must carry the same folder: %q", env)
	}
}

// A task worker acts as the agent, so it carries the conversation's folder,
// not the directory the task happens to work in.
func TestTaskWorkerCommandsSeeTheConversationsFolder(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	place := identityPlace(t, "9999999999999998")
	agent := openIdentityAgent(t, place, bashWritesAgentWorkspace("worker-ws.txt", false))
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Identity", brief: "Write your folder", acceptance: "folder written", model: "test/model", depth: 1})
	node := graph.node(id)
	dir := t.TempDir()
	worker, err := agent.newTaskAgent(context.Background(), dir, node, "")
	if err != nil {
		t.Fatalf("worker construction: %v", err)
	}
	defer worker.Close()
	drainTurn(t, worker, "Write your folder")
	raw, err := os.ReadFile(filepath.Join(dir, "worker-ws.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := agent.config.Workspace; string(raw) != want || want == dir {
		t.Fatalf("the worker's command saw folder %q, want the conversation's %q", raw, want)
	}
}
