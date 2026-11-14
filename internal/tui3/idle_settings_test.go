package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type refusedSettingAgent struct {
	*effortAgent
	refusal error
}

func (a *refusedSettingAgent) SetConversationEffort(string) bool { return false }
func (a *refusedSettingAgent) SetModel(string)                   {}
func (a *refusedSettingAgent) SettingError() error               { return a.refusal }

func TestIdleRelease_R5_1_TerminalKeepsRefusedSettingWords(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B"}
	far := &refusedSettingAgent{effortAgent: &effortAgent{fakeAgent: &fakeAgent{model: "old/model"}, conversation: effort.Low}, refusal: want}
	a := newTestApp(far)
	a.slash("/effort high")
	if !strings.Contains(a.input.String(), "/effort high") || far.ConversationEffort() != "low" {
		t.Fatal("refused effort choice was lost or applied")
	}
	a.slash("/model new/model")
	if !strings.Contains(a.input.String(), "/model new/model") || far.Model() != "old/model" {
		t.Fatal("refused model choice was lost or applied")
	}
}
