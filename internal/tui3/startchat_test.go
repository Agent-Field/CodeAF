package tui3

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

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
