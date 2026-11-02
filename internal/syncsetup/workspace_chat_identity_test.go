package syncsetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// The accepted public takeover transfers a chat that has never created a task.
// Its sealed chat identity must be adopted before the receiver opens any agent.
func TestTasklessTakePreservesConversationWorkspaceIdentity(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	var providerRequests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerRequests.Add(1)
		http.Error(w, "no model allowed in takeover proof", 503)
	}))
	defer provider.Close()
	h := newTwoHomes(t)
	t.Setenv(home.EnvVar, h.homeA)
	// The relay-only fixture writes a synthetic future-version header. Let the
	// actual public session create this fresh fixture's current journal instead.
	if err := os.Remove(transcriptOf(h.cell)); err != nil {
		t.Fatal(err)
	}
	source, err := session.New(session.Config{Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), Model: "fixture/no-model-call", APIKey: "fixture-no-provider-key", BaseURL: provider.URL, System: "Mock chat without tasks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	source.SettleWrites()
	meta, err := session.LoadMeta(h.cell.Root)
	if err != nil || meta.ChatURI == "" {
		t.Fatalf("source identity: %+v %v", meta, err)
	}
	expected := meta.ChatURI
	a := h.openA()
	a.mustSay("MOCK_TASKLESS_CHAT")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	took, err := h.continuerB().Take(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(took.TaskCopies) != 0 {
		t.Fatal("taskless chat fixture restored task copies")
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	bound, err := registry.Resolve(expected)
	if err != nil || bound.Role != workspaceidentity.Chat || bound.Path != took.Taken.Cell.Root {
		t.Fatalf("accepted taskless take did not adopt sealed chat identity: %+v %v", bound, err)
	}
	received, err := session.LoadMeta(took.Taken.Cell.Root)
	if err != nil || received.ChatURI != expected {
		t.Fatal("received metadata changed chat identity")
	}
	workspace := workspaceOf(took.Taken.Cell.Root)
	receiver, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: took.Taken.Cell.Root, Workspace: workspace, Owned: received.Owned}, SessionFile: transcriptOf(took.Taken.Cell), Model: "fixture/no-model-call", APIKey: "fixture-no-provider-key", BaseURL: provider.URL, System: "Mock received chat without tasks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := receiver.Close(); err != nil {
		t.Fatal(err)
	}
	receiver.SettleWrites()
	reopened, err := session.LoadMeta(took.Taken.Cell.Root)
	if err != nil || reopened.ChatURI != expected {
		t.Fatal("opening receiver reminted chat identity")
	}
	if providerRequests.Load() != 0 {
		t.Fatal("taskless takeover unexpectedly called a provider")
	}
}
