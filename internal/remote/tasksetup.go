package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The host advertises the complete setup door so older engines remain explicit.
type taskSetupDoor interface {
	RetargetTask(uint64, string) (session.ModelLanding, error)
	TaskEffort(uint64) string
	SetTaskEffort(uint64, string) error
}

func taskSetupKnown(agent any) bool { _, ok := agent.(taskSetupDoor); return ok }

func (a *Agent) TaskSetupSupported() bool { return a.c.Welcome().TaskSetup }

func (a *Agent) taskSetupCall(method string, id uint64, value string) ([]byte, error) {
	welcome := a.c.Welcome()
	if !welcome.TaskSetup {
		return nil, errors.New("task setup needs a newer engine; update the engine and reconnect")
	}
	return a.c.call(nil, method, TaskSetupArgs{ID: id, Session: welcome.SessionFile, Value: value})
}

// RetargetTask changes only the task in the conversation the surface has open.
//
// AND IT CARRIES BACK WHEN THE PICK LANDED, because the room on this side says
// it out loud (internal/tui3's roomModelTiming). An engine too old to answer
// sends nothing, and nothing unmarshals as the landing every live node has when
// no request is out — which is the safe half of the sentence to say when we
// cannot know.
func (a *Agent) RetargetTask(id uint64, model string) (session.ModelLanding, error) {
	payload, err := a.taskSetupCall(MethodTaskModel, id, model)
	if err != nil {
		return session.ModelLandsNextRequest, err
	}
	landing := session.ModelLandsNextRequest
	_ = json.Unmarshal(payload, &landing)
	if landing == "" {
		landing = session.ModelLandsNextRequest
	}
	return landing, nil
}

// TaskEffort is an explicit read; rendering uses the standing task updates.
func (a *Agent) TaskEffort(id uint64) string {
	payload, err := a.taskSetupCall(MethodTaskEffort, id, "")
	if err != nil {
		return ""
	}
	var value string
	_ = json.Unmarshal(payload, &value)
	return value
}

func (a *Agent) SetTaskEffort(id uint64, rung string) error {
	_, err := a.taskSetupCall(MethodTaskSetEffort, id, rung)
	return err
}
