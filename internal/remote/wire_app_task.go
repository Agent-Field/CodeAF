package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

// App task calls preserve engine refusals; a failed read never becomes an empty tree.
const (
	MethodAppTasks           = "App.Tasks"
	MethodAppTask            = "App.Task"
	MethodAppTaskAction      = "App.TaskAction"
	MethodAppQuickTasks      = "App.QuickTasks"
	MethodAppQuickTaskAction = "App.QuickTaskAction"
	// MethodAppMoveToTask asks that the running reply move to a task at its
	// next step boundary ([session.Agent.MoveTurnToTask]).
	MethodAppMoveToTask = "App.MoveToTask"
	// MethodAppResumeAfterQuit carries on a reply the app was closed in the
	// middle of ([session.Agent.ResumeAfterQuit]).
	MethodAppResumeAfterQuit = "App.ResumeAfterQuit"
	// MethodAppRename gives the conversation the name a person typed
	// ([session.Agent.RenameByPerson]).
	MethodAppRename = "App.Rename"
)

// appRenameDoor is the conversation a person can rename.
type appRenameDoor interface{ RenameByPerson(title string) error }

// AppRenameArgs is the name a person typed.
type AppRenameArgs struct {
	Title string `json:"title"`
}

// appQuitDoor is the conversation that can carry on after the app's quit.
type appQuitDoor interface{ ResumeAfterQuit() bool }

type appTaskPageResult struct {
	Task  session.AppTaskView
	Found bool
}
type appQuickActionResult struct{ Undone, Changed []string }
type appTaskDoor interface {
	AppTasks() ([]session.AppTaskView, error)
	AppTask(string) (session.AppTaskView, bool, error)
	AppTaskAction(session.AppTaskIntent) error
	AppQuickTasks() ([]session.AppQuickTaskView, error)
	AppQuickTaskAction(session.AppTaskIntent) ([]string, []string, error)
	MoveTurnToTask() error
}

func dispatchAppTask(agent any, call Frame) (json.RawMessage, error) {
	door, ok := agent.(appTaskDoor)
	if !ok {
		return nil, errors.New("engine: app tasks are unavailable")
	}
	switch call.Method {
	case MethodAppRename:
		rename, ok := agent.(appRenameDoor)
		if !ok {
			return nil, errors.New("engine: this conversation cannot be renamed")
		}
		args, err := arg[AppRenameArgs](call)
		if err != nil {
			return nil, err
		}
		return nil, rename.RenameByPerson(args.Title)
	case MethodAppResumeAfterQuit:
		quit, ok := agent.(appQuitDoor)
		if !ok {
			return nil, errors.New("engine: this conversation cannot carry on by itself")
		}
		return json.Marshal(quit.ResumeAfterQuit())
	case MethodAppMoveToTask:
		return nil, door.MoveTurnToTask()
	case MethodAppTasks:
		rows, err := door.AppTasks()
		if err != nil {
			return nil, err
		}
		return json.Marshal(rows)
	case MethodAppQuickTasks:
		rows, err := door.AppQuickTasks()
		if err != nil {
			return nil, err
		}
		return json.Marshal(rows)
	case MethodAppTask:
		args, err := arg[PlanTaskArgs](call)
		if err != nil {
			return nil, err
		}
		task, found, err := door.AppTask(args.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(appTaskPageResult{task, found})
	case MethodAppTaskAction, MethodAppQuickTaskAction:
		intent, err := arg[session.AppTaskIntent](call)
		if err != nil {
			return nil, err
		}
		if call.Method == MethodAppTaskAction {
			return nil, door.AppTaskAction(intent)
		}
		undone, changed, err := door.AppQuickTaskAction(intent)
		if err != nil {
			return nil, err
		}
		return json.Marshal(appQuickActionResult{undone, changed})
	}
	return nil, errors.New("unknown app task method")
}
func (a *Agent) AppTasks() ([]session.AppTaskView, error) {
	raw, err := a.c.call(nil, MethodAppTasks, nil)
	if err != nil {
		return nil, err
	}
	var rows []session.AppTaskView
	err = json.Unmarshal(raw, &rows)
	return rows, err
}
func (a *Agent) AppTask(id string) (session.AppTaskView, bool, error) {
	raw, err := a.c.call(nil, MethodAppTask, PlanTaskArgs{ID: id})
	if err != nil {
		return session.AppTaskView{}, false, err
	}
	var result appTaskPageResult
	err = json.Unmarshal(raw, &result)
	return result.Task, result.Found, err
}
func (a *Agent) AppTaskAction(intent session.AppTaskIntent) error {
	_, err := a.c.call(nil, MethodAppTaskAction, intent)
	return err
}
func (a *Agent) AppQuickTasks() ([]session.AppQuickTaskView, error) {
	raw, err := a.c.call(nil, MethodAppQuickTasks, nil)
	if err != nil {
		return nil, err
	}
	var rows []session.AppQuickTaskView
	err = json.Unmarshal(raw, &rows)
	return rows, err
}
func (a *Agent) AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error) {
	raw, err := a.c.call(nil, MethodAppQuickTaskAction, intent)
	if err != nil {
		return nil, nil, err
	}
	var result appQuickActionResult
	err = json.Unmarshal(raw, &result)
	return result.Undone, result.Changed, err
}

// MoveTurnToTask asks the engine to move the running reply to a task.
func (a *Agent) MoveTurnToTask() error {
	_, err := a.c.call(nil, MethodAppMoveToTask, nil)
	return err
}

// ResumeAfterQuit asks the engine to carry on the reply the app was closed in
// the middle of, and answers whether a turn was queued.
func (a *Agent) ResumeAfterQuit() (bool, error) {
	raw, err := a.c.call(nil, MethodAppResumeAfterQuit, nil)
	if err != nil {
		return false, err
	}
	var queued bool
	err = json.Unmarshal(raw, &queued)
	return queued, err
}

// RenameByPerson gives the conversation the name a person typed.
func (a *Agent) RenameByPerson(title string) error {
	_, err := a.c.call(nil, MethodAppRename, AppRenameArgs{Title: title})
	return err
}
