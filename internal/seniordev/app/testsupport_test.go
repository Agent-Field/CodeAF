//go:build !windows

package app

// Shared fixtures for the pipeline tests.

import (
	"context"
	"fmt"

	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/sessioncore"
)

// testAgentPrompt stands in for a baked agent document in tests that drive
// the engine directly: a turn must carry an agent prompt to be composed.
const testAgentPrompt = "<Role>test agent</Role>"

// backendFunc is the stub model backend the pipeline tests run against.
type backendFunc func(context.Context, turn) (turnResult, error)

func (f backendFunc) Run(ctx context.Context, request turn) (turnResult, error) {
	return f(ctx, request)
}

// guardWorkspace is a git repository with one commit, returning the workspace
// and its base SHA. Tests of the submission protocol need a real base to diff
// against, not a bare temp directory.
func guardWorkspace(t *testing.T) (string, string) {
	t.Helper()
	workspace := gitWorkspace(t, map[string]string{"README.md": "base\n"})
	return workspace, strings.TrimSpace(
		gitOutput(context.Background(), workspace, "rev-parse", "HEAD"),
	)
}

// testRepoWithEntrypoints is a git repository that also has runnable build and
// test entrypoints, for tests that let verification actually execute something.
func testRepoWithEntrypoints(t *testing.T) string {
	t.Helper()
	return gitWorkspace(t, map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	})
}

func gitWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.name", "senior-dev-test"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.email", "senior-dev@example.test"); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name, content := range files {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := gitRun(workspace, append([]string{"add"}, names...)...); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// verifiedTestPipeline is a pipeline over a workspace whose verification the test
// supplies directly, for cases that assert on how a verification RESULT is
// interpreted rather than on running one.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// testTurn is the request shape the engine tests drive a turn with. It is the
// subset of turn a caller has to supply; runTestTurn fills in the rest exactly
// as soloTurn does, so a test measures the engine the run actually uses.
type testTurn struct {
	// SessionID drives the turn against an existing session instead of
	// creating one. Tests of the durable transcript need a second runtime to
	// land in the first one's session; production never needs this, because a
	// run holds one session for its whole life.
	SessionID       string
	Agent           string
	ProviderID      string
	ModelID         string
	Prompt          string
	Workspace       string
	SessionTitle    string
	ParentSessionID string
}

// runTestTurn creates a session and runs one turn through the same seams
// soloTurn uses. The engine tests need an entry point that is not soloTurn
// itself, which builds its prompt from the checklist and the pinned command
// and so cannot be pointed at an arbitrary agent or model.
func runTestTurn(
	t *testing.T, runtime *runtimeAdapter, input testTurn,
) (turnResult, error) {
	t.Helper()
	ctx := context.Background()
	markdown, _ := baked.GetBakedAgent(input.Agent)
	providerID, modelID := normalizeModelRef(input.ProviderID, input.ModelID)
	sessionID := input.SessionID
	if sessionID == "" {
		info, err := runtime.createSession(ctx, sessioncore.CreateInput{
			ParentID: input.ParentSessionID, Title: input.SessionTitle,
			Agent: input.Agent, Directory: input.Workspace,
			Model: sessionModel(providerID, modelID, ""),
		})
		if err != nil {
			return turnResult{}, err
		}
		sessionID = info.ID
	}
	configured, err := runtime.configureTurn(turn{
		SessionID: sessionID, ParentSessionID: input.ParentSessionID,
		SessionTitle: input.SessionTitle, Agent: input.Agent,
		AgentMarkdown: markdown, Workspace: input.Workspace,
		ProviderID: providerID, ModelID: modelID, Prompt: input.Prompt,
	})
	if err != nil {
		return turnResult{}, err
	}
	configured.ManageScratch = true
	configured.SystemInstructions = runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runtime.registry.SystemInstructions
	configured.Tools = runtime.definitionsFor(
		configured.ProviderID, configured.ModelID, input.Agent, nil,
	)
	configured.Execute = runtime.registry.Execute
	configured.AfterAssistant = runtime.registry.ClearInstructionClaims
	result, err := runtime.runTurn(ctx, configured)
	runtime.addCost(result.CostUSD)
	if result.SessionID == "" {
		result.SessionID = sessionID
	}
	return result, err
}

type coderOnlyBackend struct {
	mu sync.Mutex

	onCoder func(call int, request turn) error

	calls      []turn
	coderCalls int
}

func (backend *coderOnlyBackend) Run(
	_ context.Context, request turn,
) (turnResult, error) {
	backend.mu.Lock()
	backend.calls = append(backend.calls, request)
	backend.mu.Unlock()

	switch request.Agent {
	case "coder":
		backend.mu.Lock()
		backend.coderCalls++
		call := backend.coderCalls
		backend.mu.Unlock()
		if backend.onCoder != nil {
			if err := backend.onCoder(call, request); err != nil {
				return turnResult{}, err
			}
		}
		return turnResult{Text: "coder completed"}, nil
	default:
		return turnResult{}, fmt.Errorf(
			"test backend has no script for agent %q", request.Agent,
		)
	}
}

func (backend *coderOnlyBackend) count(agent string) int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	total := 0
	for _, call := range backend.calls {
		if call.Agent == agent {
			total++
		}
	}
	return total
}

func newRuntime(workspace string, client backend) *runtimeAdapter {
	return newConfiguredRuntime(workspace, client, nil)
}

// Create opens a session for agent under parentID. Tests of the durable
// transcript use it to hold a root session open across runtimes.
func (runtime *runtimeAdapter) Create(
	ctx context.Context, parentID string, agent string,
) (string, error) {
	info, err := runtime.createSession(ctx, sessioncore.CreateInput{
		ParentID: parentID, Agent: agent, Title: agent, Directory: runtime.workspace,
	})
	return info.ID, err
}
