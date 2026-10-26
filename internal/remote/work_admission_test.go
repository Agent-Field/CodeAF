package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

type viewerAdmissionAgent struct {
	*fakeAgent
	refusal *executor.WorkRefusal
}

func (a *viewerAdmissionAgent) CheckWork() error { return a.refusal }

// C1: the real remote transport keeps the refusal's facts and sentence. The
// real-session/real-drive companion is TestC1C2AppViewerRefusesWorkBeforeSubmission.
func TestC1RemoteWorkAdmissionKeepsOwnershipFacts(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat; this window now only shows it", HolderID: "device", HolderName: "laptop"}
	far := &viewerAdmissionAgent{fakeAgent: &fakeAgent{}, refusal: want}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if !loop.Client.Welcome().WorkAdmission {
		t.Fatal("the session admission door was not advertised")
	}
	err = loop.Client.Agent().CheckWork()
	var got *executor.WorkRefusal
	if !errors.As(err, &got) || *got != *want || err.Error() != want.Line {
		t.Fatalf("remote ownership refusal = %+v, %v", got, err)
	}
}

// C6: no advertisement means no extra call, even without a live connection.
func TestC6OldRemoteEngineNeedsNoAdmissionCall(t *testing.T) {
	agent := &Agent{c: &Client{}}
	if err := agent.CheckWork(); err != nil {
		t.Fatal(err)
	}
}
