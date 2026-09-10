package remote

import (
	"encoding/json"
	"errors"
)

// The host advertises the complete setup door so older engines remain explicit.
type taskSetupDoor interface {
	RetargetTask(uint64, string) error
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
func (a *Agent) RetargetTask(id uint64, model string) error {
	_, err := a.taskSetupCall(MethodTaskModel, id, model)
	return err
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
