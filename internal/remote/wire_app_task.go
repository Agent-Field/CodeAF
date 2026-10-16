package remote

import (
	"context"
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
	// MethodAppTaskRouting reads one task's models and MethodAppTaskRoutingAct
	// changes them ([session.Agent.TaskRouting], [session.Agent.TaskRoutingAct]).
	MethodAppTaskRouting    = "App.TaskRouting"
	MethodAppTaskRoutingAct = "App.TaskRoutingAct"
	// MethodAppTaskRoutingOptions previews what each change would do.
	MethodAppTaskRoutingOptions = "App.TaskRoutingOptions"
)

// appRoutingDoor is the conversation whose tasks' models can be read and moved.
type appRoutingDoor interface {
	TaskRouting(id string, changes int) (session.TaskRoutingView, error)
	TaskRoutingAct(ctx context.Context, id string, act session.TaskRoutingAction) (session.TaskRoutingResult, error)
	TaskRoutingOptions(id string) (session.TaskRoutingOptions, error)
}

// AppTaskRoutingArgs is one task's routing read or change.
type AppTaskRoutingArgs struct {
	ID      string                     `json:"id"`
	Changes int                        `json:"changes,omitempty"`
	Act     *session.TaskRoutingAction `json:"act,omitempty"`
}

// appRoutingActResult carries a typed refusal across the socket intact.
type appRoutingActResult struct {
	Options *session.TaskRoutingOptions `json:",omitempty"`
	Result  session.TaskRoutingResult
	Refusal *session.TaskRoutingRefusal `json:",omitempty"`
}

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
	case MethodAppTaskRouting, MethodAppTaskRoutingAct, MethodAppTaskRoutingOptions:
		routing, ok := agent.(appRoutingDoor)
		if !ok {
			return nil, errors.New("engine: task models are unavailable")
		}
		args, err := arg[AppTaskRoutingArgs](call)
		if err != nil {
			return nil, err
		}
		if call.Method == MethodAppTaskRoutingOptions {
			options, err := routing.TaskRoutingOptions(args.ID)
			if err != nil {
				return appRoutingFailure(err)
			}
			return json.Marshal(appRoutingActResult{Options: &options})
		}
		if call.Method == MethodAppTaskRouting {
			view, err := routing.TaskRouting(args.ID, args.Changes)
			if err != nil {
				return appRoutingFailure(err)
			}
			return json.Marshal(appRoutingActResult{Result: session.TaskRoutingResult{View: view}})
		}
		if args.Act == nil {
			return nil, errors.New("engine: a task models change needs an action")
		}
		result, err := routing.TaskRoutingAct(context.Background(), args.ID, *args.Act)
		if err != nil {
			return appRoutingFailure(err)
		}
		return json.Marshal(appRoutingActResult{Result: result})
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

// appRoutingFailure answers a typed refusal as a value, any other error as one.
func appRoutingFailure(err error) (json.RawMessage, error) {
	var refusal *session.TaskRoutingRefusal
	if errors.As(err, &refusal) {
		return json.Marshal(appRoutingActResult{Refusal: refusal})
	}
	return nil, err
}

func (a *Agent) routingCall(method string, args AppTaskRoutingArgs) (session.TaskRoutingResult, error) {
	raw, err := a.c.call(nil, method, args)
	if err != nil {
		return session.TaskRoutingResult{}, err
	}
	var result appRoutingActResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return session.TaskRoutingResult{}, err
	}
	if result.Refusal != nil {
		return session.TaskRoutingResult{}, result.Refusal
	}
	return result.Result, nil
}

// TaskRouting reads one task's models.
func (a *Agent) TaskRouting(id string, changes int) (session.TaskRoutingView, error) {
	result, err := a.routingCall(MethodAppTaskRouting, AppTaskRoutingArgs{ID: id, Changes: changes})
	return result.View, err
}

// TaskRoutingAct is a person's change to one task's models.
func (a *Agent) TaskRoutingAct(_ context.Context, id string, act session.TaskRoutingAction) (session.TaskRoutingResult, error) {
	return a.routingCall(MethodAppTaskRoutingAct, AppTaskRoutingArgs{ID: id, Act: &act})
}

// TaskRoutingOptions previews what each change would do to one task.
func (a *Agent) TaskRoutingOptions(id string) (session.TaskRoutingOptions, error) {
	raw, err := a.c.call(nil, MethodAppTaskRoutingOptions, AppTaskRoutingArgs{ID: id})
	if err != nil {
		return session.TaskRoutingOptions{}, err
	}
	var result appRoutingActResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return session.TaskRoutingOptions{}, err
	}
	if result.Refusal != nil {
		return session.TaskRoutingOptions{}, result.Refusal
	}
	if result.Options == nil {
		return session.TaskRoutingOptions{}, errors.New("engine: no task models options came back")
	}
	return *result.Options, nil
}
