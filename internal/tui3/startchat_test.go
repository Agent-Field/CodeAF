package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"path/filepath"
	"testing"
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
	fresh := &postureTestAgent{fakeAgent: fakeAgent{model: "deepseek/deepseek-v4.1-flash"}}
	a.start = func(where string) (Conversation, error) {
		return Conversation{Agent: fresh, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"), Workspace: where}, nil
	}
	request := &session.ChatStart{ID: "request-one", Workspace: a.workspace, Model: "moonshotai/kimi-k3", Brief: "Report ready", Approval: session.PostureAllow}
	drive(t, a, runCmd(a.startRequestedChat(request))...)
	if fresh.Model() != request.Model {
		t.Fatalf("new chat model = %s", fresh.Model())
	}
	if a.frontTabKey() != front {
		t.Fatal("start stole focus")
	}
	if fresh.posture != session.PostureAllow {
		t.Fatal("start lost approvals")
	}
	if a.startRequestedChat(request) != nil {
		t.Fatal("duplicate request started another chat")
	}
}
