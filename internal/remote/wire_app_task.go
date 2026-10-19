package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/guard"
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
	// MethodAppResumeAfterClose carries the unexpected-close wake to its owner.
	MethodAppResumeAfterClose = "App.ResumeAfterClose"
	// MethodAppRename gives the conversation the name a person typed
	// ([session.Agent.RenameByPerson]).
	MethodAppRename = "App.Rename"
	// MethodAppTaskRouting reads one task's models and MethodAppTaskRoutingAct
	// changes them ([session.Agent.TaskRouting], [session.Agent.TaskRoutingAct]).
	MethodAppTaskRouting    = "App.TaskRouting"
	MethodAppTaskRoutingAct = "App.TaskRoutingAct"
	// MethodAppTaskRoutingOptions previews what each change would do.
	MethodAppTaskRoutingOptions = "App.TaskRoutingOptions"
	// MethodAppProgramActions reads an older page of a program's actions
	// ([session.Agent.AppProgramActions]). An engine that predates it answers
	// it as an unknown method.
	MethodAppProgramActions = "App.ProgramActions"
)

// appProgramDoor is the conversation whose programs' older actions can be paged.
type appProgramDoor interface {
	AppProgramActions(id, before string) (session.AppProgramActionsPage, bool, error)
}

// AppProgramActionsArgs is one page asked for: the task and the cursor.
type AppProgramActionsArgs struct {
	ID     string `json:"id"`
	Before string `json:"before"`
}

type appProgramActionsResult struct {
	Page  session.AppProgramActionsPage
	Found bool
}

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

// appCloseDoor is deliberately separate so older engines keep their quit door.
type appCloseDoor interface{ ResumeAfterCloseKey(string) bool }

// appCloseArgs carries the private continuation identity through the engine.
type appCloseArgs struct{ Key string }

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
	case MethodAppProgramActions:
		pager, ok := agent.(appProgramDoor)
		if !ok {
			return nil, errors.New("engine: a program's earlier actions are unavailable")
		}
		args, err := arg[AppProgramActionsArgs](call)
		if err != nil {
			return nil, err
		}
		page, found, err := pager.AppProgramActions(args.ID, args.Before)
		if err != nil {
			return nil, err
		}
		return json.Marshal(appProgramActionsResult{page, found})
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
	case MethodAppResumeAfterClose:
		closed, ok := agent.(appCloseDoor)
		if !ok {
			return nil, errors.New("engine: this conversation cannot carry on by itself")
		}
		args, err := arg[appCloseArgs](call)
		if err != nil {
			return nil, err
		}
		return json.Marshal(closed.ResumeAfterCloseKey(args.Key))
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
			return appTaskActionAnswer(agent, door, call, intent)
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

// AppProgramActions reads an older page of a program's actions on the far
// engine.
func (a *Agent) AppProgramActions(id, before string) (session.AppProgramActionsPage, bool, error) {
	raw, err := a.c.call(nil, MethodAppProgramActions, AppProgramActionsArgs{ID: id, Before: before})
	if err != nil {
		return session.AppProgramActionsPage{}, false, err
	}
	var result appProgramActionsResult
	err = json.Unmarshal(raw, &result)
	return result.Page, result.Found, err
}
func (a *Agent) AppTaskAction(intent session.AppTaskIntent) error {
	_, err := a.c.call(nil, MethodAppTaskAction, intent)
	return err
}

// appTaskReceiptDoor is an engine that types a note's outcome
// ([session.Agent.AppTaskActionReceipt]). The handler asserts it to answer a
// call that asked for the receipt. The welcome advertises the typed answer
// ([Welcome.AppTaskReceipt]); it never attributes an untyped error.
type appTaskReceiptDoor interface {
	AppTaskActionReceipt(session.AppTaskIntent) session.AppTaskReceipt
}

// appTaskActionArgs is [MethodAppTaskAction]'s frame with the one additive
// flag. The intent's own fields stay at the top level, so an engine that
// predates the flag reads the same intent and ignores it.
type appTaskActionArgs struct {
	session.AppTaskIntent
	// Receipt asks for the note's typed outcome as the answer's payload
	// ([appTaskActionResult]) instead of a bare error.
	Receipt bool `json:"receipt,omitempty"`
}

// appTaskActionResult carries the typed outcome across the socket intact.
type appTaskActionResult struct {
	Receipt *session.AppTaskReceipt `json:"receipt,omitempty"`
}

// appTaskActionAnswer is the engine's half of [MethodAppTaskAction].
//
// ONLY A CALL THAT ASKED FOR THE RECEIPT IS ANSWERED WITH ONE. A client that
// predates it reads a nil error as success and never reads the payload, so an
// unknown outcome carried as a payload to it would be a note it believed
// delivered. Without the flag every outcome is the error it always was.
func appTaskActionAnswer(agent any, door appTaskDoor, call Frame, intent session.AppTaskIntent) (json.RawMessage, error) {
	args, err := arg[appTaskActionArgs](call)
	if err != nil {
		return nil, err
	}
	if !args.Receipt {
		return nil, door.AppTaskAction(intent)
	}
	receipts, ok := agent.(appTaskReceiptDoor)
	if !ok {
		// THE CALL ASKED FOR A TYPED OUTCOME AND THIS AGENT CANNOT GIVE ONE, so
		// the plain door is never called: its error is a flattened text that may
		// follow the effect. This refusal is before any effect, but the client
		// conservatively reads its untyped error as unknown and never resends.
		return nil, errAppTaskNoReceiptDoor
	}
	receipt := appTaskReceiptGuarded(receipts, intent)
	return json.Marshal(appTaskActionResult{Receipt: &receipt})
}

// errAppTaskNoReceiptDoor refuses, before the engine's door is touched, a call
// that asked for a typed outcome the serving agent has no way to give.
var errAppTaskNoReceiptDoor = errors.New("this engine cannot answer that note with a typed outcome, so it was not sent")

// appTaskReceiptWentWrongWord is the reason for a note whose engine door
// panicked: the panic could be anywhere in the door, after the words were
// written down or handed over, so what became of them is not known.
const appTaskReceiptWentWrongWord = "the engine failed while taking that note, so it is not known whether it arrived"

// appTaskReceiptGuarded calls the receipt door and turns a panic inside it into
// a typed UNKNOWN. THE PANIC IS NOT PRE-DISPATCH: the door may already have
// written the direction or handed it to a worker, and the generic recover in
// [server.invoke] would lose the typed outcome and its client key in an
// untyped error. Both stay unknown, but this receipt preserves their identity.
// The stack still goes to the log.
func appTaskReceiptGuarded(door appTaskReceiptDoor, intent session.AppTaskIntent) (receipt session.AppTaskReceipt) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("remote/engine "+MethodAppTaskAction, recovered)
			receipt = session.AppTaskReceipt{
				Outcome:  session.AppTaskUnknown,
				Reason:   appTaskReceiptWentWrongWord,
				ClientID: session.AppNoteClientID(intent.ClientID),
			}
		}
	}()
	return door.AppTaskActionReceipt(intent)
}

// appTaskReceiptKnown asks this engine whether it types a note's outcome. An
// engine without the door answers no, which is the reading a surface must take
// from silence ([Welcome.AppTaskReceipt]).
func appTaskReceiptKnown(agent any) bool {
	_, ok := agent.(appTaskReceiptDoor)
	return ok
}

// AppTaskActionReceipt is [Agent.AppTaskAction] for a note or a brief, with
// the outcome typed across the socket ([session.Agent.AppTaskActionReceipt]).
//
// WHAT THIS END CANNOT KNOW IS UNKNOWN, NEVER REFUSED. A call nobody answered
// — a deadline, a reply that never came, a write onto a pipe that went — may
// have been carried out over there. An engine that predates the receipt says
// nothing typed about a success, and flattened failures that came after the
// effect into the same text as refusals, so both of its answers are unknown
// too. An untyped error remains unknown even when the current welcome says
// receipts are supported: it is not an occurrence-bound outcome for this call.
//
// NOTHING IS SENT AGAIN, by any door, whatever comes back.
func (a *Agent) AppTaskActionReceipt(intent session.AppTaskIntent) session.AppTaskReceipt {
	key := session.AppNoteClientID(intent.ClientID)
	payload, _, err := a.c.callAnswered(nil, MethodAppTaskAction, appTaskActionArgs{AppTaskIntent: intent, Receipt: true}, callDeadline)
	if err != nil {
		return session.AppTaskReceipt{Outcome: session.AppTaskUnknown, Reason: err.Error(), ClientID: key}
	}
	var result appTaskActionResult
	if json.Unmarshal(payload, &result) != nil || result.Receipt == nil || !appTaskOutcomeKnown(result.Receipt.Outcome) {
		return session.AppTaskReceipt{Outcome: session.AppTaskUnknown, Reason: appTaskReceiptMissingWord, ClientID: key}
	}
	return *result.Receipt
}

// appTaskReceiptMissingWord is an answer that carried no typed outcome: an
// engine too old to say, or a word this build does not know.
const appTaskReceiptMissingWord = "the engine did not say what became of that note"

// appTaskOutcomeKnown is whether this build knows the outcome's word. One it
// does not know is read as unknown rather than guessed at.
func appTaskOutcomeKnown(outcome session.AppTaskOutcome) bool {
	switch outcome {
	case session.AppTaskDelivered, session.AppTaskHeld, session.AppTaskRefused, session.AppTaskUnknown:
		return true
	}
	return false
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

// ResumeAfterClose asks for the private wake after an unexpected app close.
func (a *Agent) ResumeAfterClose() (bool, error) {
	return a.ResumeAfterCloseKey("")
}

// ResumeAfterCloseKey keeps the pending key attached to its own private note.
func (a *Agent) ResumeAfterCloseKey(key string) (bool, error) {
	raw, err := a.c.call(nil, MethodAppResumeAfterClose, appCloseArgs{Key: key})
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
