package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

type refusedSettingsAgent struct {
	*rungAgent
	refusal *executor.WorkRefusal
}

func (a *refusedSettingsAgent) SetModelChecked(string) error { return a.refusal }
func (a *refusedSettingsAgent) SetConversationEffortChecked(string) (bool, error) {
	return false, a.refusal
}

func TestIdleRelease_R5_1_RemoteSettingsKeepRefusalAndReplica(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B", HolderName: "laptop"}
	far := &refusedSettingsAgent{rungAgent: &rungAgent{fakeAgent: &fakeAgent{model: "old/model"}, stored: "low"}, refusal: want}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	a := loop.Client.Agent()
	for _, call := range []func() error{
		func() error { return a.SetModelChecked("new/model") },
		func() error { _, err := a.SetConversationEffortChecked("high"); return err },
		func() error { a.SetModel("new/model"); return a.SettingError() },
		func() error {
			if a.SetConversationEffort("high") {
				t.Error("refused effort acknowledged")
			}
			return a.SettingError()
		},
	} {
		var got *executor.WorkRefusal
		if err := call(); !errors.As(err, &got) || *got != *want {
			t.Fatalf("setting refusal=%+v, %v", got, err)
		}
	}
	if a.Model() != "old/model" || a.ResolvedEffort() != "low" || far.Model() != "old/model" || far.stored != "low" {
		t.Fatal("refusal changed the model or effort")
	}
}
