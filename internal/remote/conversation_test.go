package remote

import (
	"testing"
)

type commandAgent struct {
	*fakeAgent
	point  int
	undone string
}

func (a *commandAgent) RewindKeep(p int) (string, error) { a.point = p; return "token", nil }
func (a *commandAgent) UndoRewind(token string) error    { a.undone = token; return nil }
func (a *commandAgent) DebugConversation() (string, string, error) {
	return "/far/record", "recording", nil
}

func TestConversationCommandsCrossTheOwningEngine(t *testing.T) {
	agent := &commandAgent{fakeAgent: &fakeAgent{model: "m"}}
	deleted := false
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: agent, ProfileDir: t.TempDir(), SessionFile: "/far/chat/transcript.jsonl", DeleteConversation: func(file string, choices map[string]string, affected map[string][]string) error {
			if file != "/far/chat/transcript.jsonl" {
				t.Errorf("file: %s", file)
			}
			deleted = true
			return nil
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	remoteAgent := loop.Client.Agent()
	token, err := remoteAgent.RewindKeep(3)
	if err != nil || token != "token" || agent.point != 3 {
		t.Fatalf("rewind: %q %v", token, err)
	}
	if err := remoteAgent.UndoRewind(token); err != nil || agent.undone != token {
		t.Fatalf("undo: %v", err)
	}
	path, message, err := remoteAgent.DebugConversation()
	if err != nil || path != "/far/record" || message != "recording" {
		t.Fatalf("debug: %q %q %v", path, message, err)
	}
	if err := remoteAgent.DeleteOwnConversation(); err != nil || !deleted {
		t.Fatalf("delete: %v", err)
	}
}
