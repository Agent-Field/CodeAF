package remote

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
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

type viewerAnswerAgent struct {
	*viewerAdmissionAgent
	task, intake, question int
}

func (a *viewerAnswerAgent) ResolveTask(uint64, session.TaskAnswer)          { a.task++ }
func (a *viewerAnswerAgent) ResolveSubharness(uint64, bool, json.RawMessage) { a.intake++ }
func (a *viewerAnswerAgent) ResolveQuestion(session.Answer) error            { a.question++; return nil }

// C1's answer amendment: the real remote server must deliver both old void
// answers and checked answers even when CheckWork refuses new work. The
// session companion exercises real pending waits and the subsequent tool gate.
func TestC1RemoteViewerAcceptsEveryAnswerDoor(t *testing.T) {
	far := &viewerAnswerAgent{viewerAdmissionAgent: &viewerAdmissionAgent{
		fakeAgent: &fakeAgent{}, refusal: &executor.WorkRefusal{Line: "laptop continued this chat"},
	}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	for _, allow := range []bool{true, false} {
		for _, answer := range []struct {
			method string
			args   any
		}{
			{MethodConsent, ConsentArgs{ID: 1, Allow: allow}},
			{MethodConsentRemember, ConsentArgs{ID: 1, Allow: allow, Scope: session.ConsentToolSession}},
			{MethodHarness, HarnessArgs{ID: 2, Run: allow}},
			{MethodConnect, ConnectArgs{ID: "browser", Approve: allow}},
			{MethodConnectKey, ConnectArgs{ID: "key", Key: "fixture"}},
			{MethodSubharnessResolve, SubharnessResolveArgs{ID: 3, Run: allow}},
			{MethodTaskResolve, TaskResolveArgs{ID: 4, Approved: allow}},
			{MethodQuestionResolve, QuestionArgs{Answer: session.Answer{Kind: session.QuestionConsent, ID: 1, Key: "1"}}},
		} {
			if _, err := loop.Client.call(context.Background(), answer.method, answer.args); err != nil {
				t.Fatalf("%s refused an answer: %v", answer.method, err)
			}
		}
	}
	far.mu.Lock()
	defer far.mu.Unlock()
	if len(far.consents) != 4 || len(far.harnesses) != 2 || len(far.connects) != 4 || far.task != 2 || far.intake != 2 || far.question != 2 {
		t.Fatalf("answers did not reach the producer: %+v", far)
	}
}
