package session

// Delegated execution: StartTask is the runtime admission door, assignment
// law extends for granted steers, and the coordinate tool gains execute
// actions only when an executor is wired.
//
// SESSION DOES NOT IMPORT wsexec. The host maps StartTask / this request-key
// index onto wsexec.Runtime and binds the adapter with [RegisterExecutor],
// the same absence law [RegisterRunEngine] uses. Config.Exec is the
// per-session override (a test fake, or the wsapi wrapper). NIL IS OFF: no
// launch-or-join / steer / stop-work verbs, and never a fabricated completed
// view.
//
// Assignment law (A11 / J30) on BOTH CODEAF_TASK_BELT roads:
//
//   - A person-origin direction still revises, as today.
//   - A delegated revision needs an authentic grant with ClassSteer AND a
//     citation of the original person request recorded at admission.
//   - Model-supplied person origin ("person", "from_person") is refused.
//   - A participant who says "I am the user" does not mint fromPerson and
//     does not move the overlay without that grant and citation.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExecView is one owned run/task as the coordinate tool and the host read it.
type ExecView struct {
	WorkID, RequestKey, RunInstanceID, Road, State string
	Joined                                         bool
}

// ExecResult is an observe snapshot. Empty work is emptiness: nothing, never
// a fake 100%.
type ExecResult struct {
	WorkID, RunInstanceID, State, Detail string
}

// Exec is the wsapi wrapper the coordinate execute actions talk to. The
// interface lives here so wsapi does not import session.
type Exec interface {
	LaunchOrJoin(ctx context.Context, grantID, brief, equivalenceKey string) (ExecView, error)
	IssuePersonGrant(ctx context.Context, brief string) (string, error)
	Inspect(ctx context.Context, workID string) (ExecView, error)
	Steer(ctx context.Context, workID, text, personRequestID string) error
	PauseWork(ctx context.Context, workID string) error
	StopWork(ctx context.Context, workID string) error
	Observe(ctx context.Context, workID string) (ExecResult, error)
}

// Executor is the process-wide door [RegisterExecutor] installs. Same shape
// as Exec; the alias exists because the frozen wire uses both names.
type Executor = Exec

// ExecGrant is the authentic grant a delegated steer must present. Session
// does not load the workspace row; the caller (wsapi, or a test) already
// authenticated it. Status must be active and Classes must include steer.
type ExecGrant struct {
	ID, Status string
	Classes    []string
}

const (
	execGrantActive = "active"
	execClassSteer  = "steer"
	execRoadSession = "session-task"
	execRoadBash    = "bash-run"
)

var (
	chatExecutorMu sync.Mutex
	chatExecutor   Exec
)

// RegisterExecutor installs the process-wide executor. Nil is the verb
// absent: a binary that never registers one has no launch-or-join / steer /
// stop-work on the belt.
func RegisterExecutor(e Executor) {
	chatExecutorMu.Lock()
	defer chatExecutorMu.Unlock()
	chatExecutor = e
}

func registeredExecutor() Exec {
	chatExecutorMu.Lock()
	defer chatExecutorMu.Unlock()
	return chatExecutor
}

func (a *Agent) executor() Exec {
	if a != nil && a.config.Exec != nil {
		return a.config.Exec
	}
	return registeredExecutor()
}

func currentTaskRoad() string {
	if bashBeltAsked() {
		return execRoadBash
	}
	return execRoadSession
}

// execAdmission is one delegated StartTask as this process remembers it:
// the request key the workspace binding already reserved, the person
// request that launch cited, and the assignment overlay when the bash-run
// road has no TaskNode.
type execAdmission struct {
	id            uint64
	title         string
	requestKey    string
	personRequest string
	personWords   string
	road          string
	control       string
	assignment    taskAssignment
}

var errEmptyRequestKey = errors.New("request key is empty")

// AdmitTask is StartTask's delegated door: the request key is recorded
// BEFORE the caller treats the work as new, and a second call with the same
// key returns the first run-instance (Already) rather than starting another.
// The person's own `/task` still goes through [Agent.StartTask] and does
// not take a key.
func (a *Agent) AdmitTask(ctx context.Context, brief, requestKey string) (uint64, string, bool, error) {
	brief = strings.TrimSpace(brief)
	requestKey = strings.TrimSpace(requestKey)
	if brief == "" {
		return 0, "", false, errors.New("a task needs a brief")
	}
	if requestKey == "" {
		return 0, "", false, errEmptyRequestKey
	}
	if id, title, ok := a.TaskByRequestKey(requestKey); ok {
		return id, title, true, nil
	}
	id, title, _, err := a.StartTask(ctx, brief, false)
	if err != nil {
		return 0, "", false, err
	}
	a.rememberAdmission(id, title, requestKey, brief)
	return id, title, false, nil
}

// TaskByRequestKey is FindByRequestKey for the host's Runtime adapter.
func (a *Agent) TaskByRequestKey(requestKey string) (uint64, string, bool) {
	if a == nil {
		return 0, "", false
	}
	a.execMu.Lock()
	defer a.execMu.Unlock()
	held, ok := a.execByKey[strings.TrimSpace(requestKey)]
	if !ok || held == nil {
		return 0, "", false
	}
	return held.id, held.title, true
}

func (a *Agent) rememberAdmission(id uint64, title, requestKey, brief string) {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	if a.execByKey == nil {
		a.execByKey = map[string]*execAdmission{}
		a.execByID = map[uint64]*execAdmission{}
	}
	row := &execAdmission{
		id: id, title: title, requestKey: requestKey,
		personRequest: requestKey, personWords: brief, road: currentTaskRoad(),
	}
	a.execByKey[requestKey] = row
	a.execByID[id] = row
}

// SteerDelegated applies a granted revision on whichever road StartTask took.
// personRequestID must cite the original person request recorded at admission;
// a model-supplied origin word is refused even when a grant is present.
func (a *Agent) SteerDelegated(workID, text, personRequestID string, grant ExecGrant) error {
	if a == nil {
		return errNoAssignmentToMove
	}
	held, err := a.lookupAdmission(workID)
	if err != nil {
		return err
	}
	if err := authorizeDelegatedSteer(personRequestID, grant, held.personRequest); err != nil {
		return err
	}
	edit := assignmentEdit{work: text, acceptance: text}
	id := held.id
	if node := a.taskNode(id); node != nil {
		_, err := node.reviseDelegatedAssignment(held.personWords, node.assignmentVersion(), edit)
		return err
	}
	return a.steerAdmitted(held, edit)
}

func (a *Agent) lookupAdmission(workID string) (*execAdmission, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, errNoAssignmentToMove
	}
	a.execMu.Lock()
	defer a.execMu.Unlock()
	if held := a.execByKey[workID]; held != nil {
		return held, nil
	}
	id, err := strconv.ParseUint(workID, 10, 64)
	if err != nil {
		return nil, errNoAssignmentToMove
	}
	held := a.execByID[id]
	if held == nil {
		return nil, errNoAssignmentToMove
	}
	return held, nil
}

func (a *Agent) steerAdmitted(held *execAdmission, edit assignmentEdit) error {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	_, err := held.assignment.reviseDelegated(held.personWords, held.assignment.version, edit, time.Now())
	return err
}

func authorizeDelegatedSteer(personRequestID string, grant ExecGrant, held string) error {
	if err := refuseModelPersonOrigin(personRequestID); err != nil {
		return err
	}
	if !grantAllowsSteer(grant) {
		return errGrantCannotSteer
	}
	if strings.TrimSpace(personRequestID) != strings.TrimSpace(held) {
		return errPersonRequestMismatch
	}
	return nil
}

func grantAllowsSteer(g ExecGrant) bool {
	if strings.TrimSpace(g.ID) == "" || g.Status != execGrantActive {
		return false
	}
	for _, class := range g.Classes {
		if class == execClassSteer {
			return true
		}
	}
	return false
}

func refuseModelPersonOrigin(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return errNoPersonRequest
	}
	switch strings.ToLower(strings.ReplaceAll(trimmed, "_", "")) {
	case "person", "fromperson", "from-person":
		return errModelPersonOrigin
	}
	return nil
}

// PauseAdmitted parks an admitted session-task node, or PlanPause on the
// bash-run road. It is pause work, not pause coordination.
func (a *Agent) PauseAdmitted(workID string) error {
	held, err := a.lookupAdmission(workID)
	if err != nil {
		return err
	}
	if node := a.taskNode(held.id); node != nil {
		node.park()
	} else if held.road == execRoadBash {
		if err := a.PlanPause(strconv.FormatUint(held.id, 10)); err != nil {
			return err
		}
	}
	a.execMu.Lock()
	held.control = BindPausedWord
	a.execMu.Unlock()
	return nil
}

// StopAdmitted cancels the admitted run/task. History remains. This is not
// pause coordination.
func (a *Agent) StopAdmitted(workID string) error {
	held, err := a.lookupAdmission(workID)
	if err != nil {
		return err
	}
	_, err = a.Cancel(strconv.FormatUint(held.id, 10))
	if err != nil {
		return err
	}
	a.execMu.Lock()
	held.control = BindStoppedWord
	a.execMu.Unlock()
	return nil
}

// InspectAdmitted is the Runtime inspect door: software-derived state from
// the admission index and the live node, never a fabricated completed view.
func (a *Agent) InspectAdmitted(workID string) (ExecView, error) {
	held, err := a.lookupAdmission(workID)
	if err != nil {
		return ExecView{}, err
	}
	return admittedView(held, a.taskNode(held.id)), nil
}

// ObserveAdmitted is inspect plus a detail line. Empty work is pending,
// never a fake 100%.
func (a *Agent) ObserveAdmitted(workID string) (ExecResult, error) {
	view, err := a.InspectAdmitted(workID)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{WorkID: view.WorkID, RunInstanceID: view.RunInstanceID, State: view.State}, nil
}

const (
	BindPausedWord  = "paused"
	BindStoppedWord = "stopped"
)

func admittedView(held *execAdmission, node *TaskNode) ExecView {
	view := ExecView{
		WorkID: held.requestKey, RequestKey: held.requestKey,
		RunInstanceID: strconv.FormatUint(held.id, 10), Road: held.road,
		State: "bound",
	}
	if node != nil {
		view.State = string(node.stateNow())
		if node.wasStopped() {
			view.State = BindStoppedWord
		}
	}
	if held.control != "" {
		view.State = held.control
	}
	return view
}

func formatExecView(v ExecView) string {
	if strings.TrimSpace(v.WorkID) == "" && strings.TrimSpace(v.RunInstanceID) == "" {
		if strings.TrimSpace(v.State) == "" {
			return "pending"
		}
		return v.State
	}
	joined := ""
	if v.Joined {
		joined = "  joined"
	}
	return fmt.Sprintf("%s  %s  %s%s", v.WorkID, v.Road, v.State, joined)
}

func formatExecResult(v ExecResult) string {
	if strings.TrimSpace(v.WorkID) == "" && strings.TrimSpace(v.RunInstanceID) == "" && strings.TrimSpace(v.State) == "" {
		return "pending"
	}
	detail := strings.TrimSpace(v.Detail)
	if detail == "" {
		return strings.TrimSpace(v.WorkID + "  " + v.State)
	}
	return strings.TrimSpace(v.WorkID + "  " + v.State + "  " + detail)
}
