package tui3

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

type observedRequestedChat struct {
	*session.Agent
	done chan struct{}
}

func (a *observedRequestedChat) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	events, err := a.Agent.Submit(ctx, text)
	if err != nil {
		cancel()
		return nil, err
	}
	forward := make(chan session.Event, 128)
	go func() {
		defer cancel()
		defer close(a.done)
		defer close(forward)
		for event := range events {
			select {
			case forward <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return forward, nil
}

// C1, C4, C5: a temporary parent's configured model is saved in an inherited
// chat. That child reconciles availability before sending and reopens on its
// saved choice; an explicit-model request retains its persistent behavior.
func TestRequestedChatSavesConfiguredModelBeforeItsOwnAvailabilityReconcile(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited", true: "explicit"}[explicit], func(t *testing.T) {
			server := sourcestub.New("replacement", "explicit")
			defer server.Close()
			service := testDirectService(server.URL())
			sources := modelsource.NewSet(testDefaultService(""), service)
			a, _, _ := tabApp(t)
			parent := &modelForNowAgent{fakeAgent: &fakeAgent{model: "deepseek-direct/configured"}, configured: "deepseek-direct/configured"}
			if err := parent.UseModelForNow("deepseek-direct/replacement"); err != nil {
				t.Fatal(err)
			}
			a.agent = parent
			owner := &claimingChatAgent{Agent: parent, claimed: &sync.Map{}}
			a.sources, a.requireListedModel = sources, true
			a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "replacement"}, {ID: "explicit"}} }
			childDir := t.TempDir()
			child, err := session.New(session.Config{Workspace: childDir, SessionFile: filepath.Join(childDir, "transcript.jsonl"), Place: session.Place{Dir: childDir}, Model: "deepseek-direct/configured", Sources: sources, DeterministicNames: true})
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			observed := &observedRequestedChat{Agent: child, done: make(chan struct{})}
			a.start = func(where string) (Conversation, error) {
				return Conversation{Agent: observed, SessionFile: filepath.Join(childDir, "transcript.jsonl"), Workspace: where}, nil
			}
			model, wantRequest := parent.ConfiguredModel(), "replacement"
			if explicit {
				model, wantRequest = "deepseek-direct/explicit", "explicit"
			}
			request := &session.ChatStart{ID: "new-chat", Workspace: childDir, Model: model, Brief: "answer briefly"}
			spend(t, a, a.startRequestedChat(request, owner))
			select {
			case <-observed.done:
			case <-time.After(5 * time.Second):
				t.Fatal("requested chat did not finish")
			}
			if err := child.Close(); err != nil {
				t.Fatal(err)
			}
			child.SettleWrites()
			meta, err := session.LoadMeta(childDir)
			if err != nil || meta.Model != model || meta.ModelIdentity == nil || meta.ModelIdentity.Selected != model {
				t.Fatalf("child metadata=%+v, %v", meta, err)
			}
			requests := completionRequests(server)
			if len(requests) != 1 || completionModel(t, requests[0]) != wantRequest {
				t.Fatalf("child requests=%+v", requests)
			}
			reopened, err := session.New(session.Config{Workspace: childDir, SessionFile: filepath.Join(childDir, "transcript.jsonl"), Place: session.Place{Dir: childDir}, Model: meta.Model, Sources: sources, DeterministicNames: true})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if reopened.ConfiguredModel() != model || reopened.Model() != model || parent.ConfiguredModel() != "deepseek-direct/configured" {
				t.Fatal("child reopen or parent choice inherited the temporary model")
			}
		})
	}
}

func TestStartedTeammateUsesNamedModelBeforeWaking(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	var fresh *postureTestAgent
	a.start = func(where string) (Conversation, error) {
		fresh = &postureTestAgent{fakeAgent: fakeAgent{model: "deepseek/deepseek-v4.1-flash"}}
		return Conversation{Agent: fresh, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"), Workspace: where}, nil
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "scout", Text: "report ready", Model: "moonshotai/kimi-k3", Approval: session.PostureAllow})
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if fresh == nil || fresh.Model() != "moonshotai/kimi-k3" {
		t.Fatalf("new chat did not take named model: %+v", fresh)
	}
}

func TestRequestedChatKeepsFocusAndUsesNamedModel(t *testing.T) {
	a, _, _ := tabApp(t)
	front := a.frontTabKey()
	owner := &claimingChatAgent{Agent: a.agent, claimed: &sync.Map{}}
	fresh := &postureTestAgent{fakeAgent: fakeAgent{model: "deepseek/deepseek-v4.1-flash"}}
	a.start = func(where string) (Conversation, error) {
		return Conversation{Agent: fresh, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"), Workspace: where}, nil
	}
	request := &session.ChatStart{ID: "request-one", Workspace: a.workspace, Model: "moonshotai/kimi-k3", Brief: "Report ready", Approval: session.PostureAllow}
	drive(t, a, runCmd(a.startRequestedChat(request, owner))...)
	if fresh.Model() != request.Model {
		t.Fatalf("new chat model = %s", fresh.Model())
	}
	if a.frontTabKey() != front {
		t.Fatal("start stole focus")
	}
	if fresh.posture != session.PostureAllow {
		t.Fatal("start lost approvals")
	}
	if a.startRequestedChat(request, owner) != nil {
		t.Fatal("duplicate request started another chat")
	}
}

// The session owns this claim; multiple terminal adapters share the same door.
type claimingChatAgent struct {
	Agent
	claimed *sync.Map
}

func (a *claimingChatAgent) ClaimChatStart(id string) (bool, error) {
	_, loaded := a.claimed.LoadOrStore(id, true)
	return !loaded, nil
}

func TestTwoWindowsOpenOnlyOneRequestedChat(t *testing.T) {
	a, _, _ := tabApp(t)
	b, _, _ := tabApp(t)
	replay, _, _ := tabApp(t)
	owner := &claimingChatAgent{Agent: a.agent, claimed: &sync.Map{}}
	started := 0
	start := func(where string) (Conversation, error) {
		started++
		return Conversation{Agent: &fakeAgent{}, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"), Workspace: where}, nil
	}
	request := &session.ChatStart{ID: "shared-request", Workspace: a.workspace, Model: "moonshotai/kimi-k3", Brief: "Report ready"}
	for _, window := range []*app{a, b, replay} {
		window.start = start
		drive(t, window, runCmd(window.startRequestedChat(request, owner))...)
	}
	if started != 1 {
		t.Fatalf("one request opened %d chats", started)
	}
}
