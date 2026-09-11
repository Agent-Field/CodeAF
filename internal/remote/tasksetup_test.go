package remote

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"testing"
)

type setupAgent struct {
	*fakeAgent
	id          uint64
	model, rung string
}

func (a *setupAgent) RetargetTask(id uint64, value string) error {
	a.id, a.model = id, value
	return nil
}
func (a *setupAgent) SetTaskEffort(id uint64, value string) error {
	a.id, a.rung = id, value
	return nil
}
func (a *setupAgent) TaskEffort(id uint64) string { return a.rung }

func TestTaskSetupCrossesTheProductionHostConnection(t *testing.T) {
	far := &setupAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/task-session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	if !agent.TaskSetupSupported() {
		t.Fatal("host hid task setup")
	}
	if err := agent.RetargetTask(17, "chosen-model"); err != nil {
		t.Fatal(err)
	}
	if far.id != 17 || far.model != "chosen-model" {
		t.Fatal("wrong task or model")
	}
	if err := agent.SetTaskEffort(17, "high"); err != nil {
		t.Fatal(err)
	}
	if agent.TaskEffort(17) != "high" {
		t.Fatal("thinking setting did not cross the host")
	}
	_, err = loop.Client.call(nil, MethodTaskModel, TaskSetupArgs{ID: 17, Session: "/another/session.jsonl", Value: "wrong"})
	if err == nil || far.model != "chosen-model" {
		t.Fatal("setup escaped its conversation")
	}
}

func TestTaskSetupCapabilityAndWatcherRefusal(t *testing.T) {
	for _, watch := range []bool{false, true} {
		far := &setupAgent{fakeAgent: &fakeAgent{}}
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
		if !watch && loop.Client.Agent().TaskSetupSupported() {
			t.Fatal("unsupported engine advertised setup")
		}
		if err := loop.Client.Agent().RetargetTask(17, "wrong"); err == nil {
			t.Fatal("read-only setup accepted")
		}
		if far.model != "" {
			t.Fatal("read-only setup changed task")
		}
		loop.Close()
	}
}

// The actual engine, rather than only a fixture, must expose the host seam.
var _ taskSetupDoor = (*session.Agent)(nil)
