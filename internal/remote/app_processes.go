package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

// These additive doors carry the engine's own process shelf and retained
// commands. They introduce no client-supplied command execution route.
const MethodProcessList = "Process.List"
const MethodProcessRerun = "Process.Rerun"

type ProcessRerunArgs struct {
	ID int `json:"id"`
}

func (a *Agent) JobNotices() ([]session.JobNotice, error) {
	raw, err := a.c.call(nil, MethodProcessList, nil)
	if err != nil {
		return nil, err
	}
	var rows []session.JobNotice
	err = json.Unmarshal(raw, &rows)
	return rows, err
}
func (a *Agent) RerunProcess(id int) (session.JobNotice, error) {
	raw, err := a.c.call(nil, MethodProcessRerun, ProcessRerunArgs{ID: id})
	if err != nil {
		return session.JobNotice{}, err
	}
	var row session.JobNotice
	err = json.Unmarshal(raw, &row)
	return row, err
}
func serveAppProcesses(agent WrappedAgent, call Frame) ([]byte, error) {
	switch call.Method {
	case MethodProcessList:
		door, ok := agent.(interface {
			JobNotices() ([]session.JobNotice, error)
		})
		if !ok {
			return nil, errors.New("this engine cannot list processes")
		}
		rows, err := door.JobNotices()
		if err != nil {
			return nil, err
		}
		return json.Marshal(rows)
	case MethodProcessRerun:
		args, err := arg[ProcessRerunArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			RerunProcess(int) (session.JobNotice, error)
		})
		if !ok {
			return nil, errors.New("this engine cannot rerun processes")
		}
		row, err := door.RerunProcess(args.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(row)
	}
	return nil, errors.New("no such process action")
}
