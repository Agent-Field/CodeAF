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
