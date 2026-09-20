package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

type retryAgent struct {
	*fakeAgent
	id uint64
}

func (a *retryAgent) RetryTask(id uint64) error { a.id = id; return nil }

func TestTaskRetryCrossesTheHostAndKeepsItsOwner(t *testing.T) {
	far := &retryAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.TaskRetrySupported() {
		t.Fatal("missing retry capability")
	}
	if err := agent.RetryTask(17); err != nil {
		t.Fatal(err)
	}
	if far.id != 17 {
		t.Fatal("retry targeted another task")
	}
	_, err = loop.Client.call(nil, MethodTaskRetry, TaskSetupArgs{ID: 23, Session: "/another/session.jsonl"})
	if err == nil || far.id != 17 {
		t.Fatal("retry escaped its conversation")
	}
}

func TestTaskRetryRefusesWatchersAndUnsupportedEngines(t *testing.T) {
	for _, watch := range []bool{false, true} {
		far := &retryAgent{fakeAgent: &fakeAgent{}}
		var owner WrappedAgent = far
		if !watch {
			owner = far.fakeAgent
		}
		loop, err := Loopback(Hello{Version: Version, Watch: watch}, Options{Boot: func(Hello) (*Engine, error) {
			return &Engine{Agent: owner, SessionFile: "/srv/task-session.jsonl"}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !watch && loop.Client.Agent().TaskRetrySupported() {
			t.Fatal("unsupported retry advertised")
		}
		if err := loop.Client.Agent().RetryTask(17); err == nil {
			t.Fatal("read-only retry accepted")
		}
		if far.id != 0 {
			t.Fatal("read-only retry changed work")
		}
		loop.Close()
	}
}

var _ taskRetryDoor = (*session.Agent)(nil)
