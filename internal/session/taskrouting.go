package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// ONE TASK'S MODELS, AS THE ENGINE RECORDED THEM, AND THE ONE DOOR THAT MOVES
// THEM.
//
// A task's worker, planner and checker are its routed crew (taskcrew.go): the
// decision admitted with it, every seat that moved since and why, and what each
// seat has spent. [Agent.TaskRouting] reads exactly that — never today's
// settings, never a guess from the model names — and [Agent.TaskRoutingAct] is
// the only way a person changes it for one task:
//
//   - a task not started yet keeps the person's choice and is routed with it
//     when it starts;
//   - a running task's role moves at its NEXT CALL, never mid-call, and the
//     history says who moved it, when, and from what;
//   - a finished task is never changed: the same choices start a rerun.
//
// THE ENGINE NEVER CHOOSES A STRONGER OR DEARER MODEL ON ITS OWN. A change
// whose estimate crosses the task cap is refused with the cap it needs, and
// goes ahead only when the person names a cap at least that high for this task.
//
// A ROLE WITH NOTHING ALLOWED TO RUN ON IS A QUESTION. The task is parked
// waiting, with the router's typed refusal, and starts from where it stood when
// a model is allowed and the person says retry; nothing runs in the meantime
// and nothing is moved onto another model.

// Who moved a role, and how.
const (
	TaskCrewByEngine = "engine"
	TaskCrewByPerson = "person"

	TaskCrewChangeFallback = "fallback"
	TaskCrewChangeStronger = "stronger_worker"
	TaskCrewChangeBest     = "best"
	TaskCrewChangePick     = "pick"
	TaskCrewChangeAuto     = "auto"
	TaskCrewChangeCap      = "cap"
	TaskCrewChangeRestored = "restored"
	// TaskCrewChangeHeld is a pinned role whose route failed and that was
	// held on its model rather than moved.
	TaskCrewChangeHeld = "held"
)

// taskCrewChangesKept bounds the history one task carries; ChangeCount still
// counts every change.
const taskCrewChangesKept = 64

// taskCrewRestoresKept bounds the restore marks one task carries.
const taskCrewRestoresKept = 16

// TaskCrewChange is one change to a task's models: a role that moved, a cap a
// person raised for the task, or the crew coming back after a restart.
type TaskCrewChange struct {
	At           time.Time
	Seat         crewroute.Seat `json:",omitempty"`
	From         string         `json:",omitempty"`
	FromProvider string         `json:",omitempty"`
	FromSend     string         `json:",omitempty"`
	To           string         `json:",omitempty"`
	ToProvider   string         `json:",omitempty"`
	ToSend       string         `json:",omitempty"`
	By           string
	Kind         string
	// Why is the failure kind for a fallback; empty for a person's change.
	Why    string  `json:",omitempty"`
	CapUSD float64 `json:",omitempty"`
}

// noteChangeLocked appends one change, keeping the bound. The caller holds c.mu.
func (c *taskCrew) noteChangeLocked(change TaskCrewChange) {
	if change.At.IsZero() {
		change.At = time.Now()
	}
	c.changes = append(c.changes, change)
	if over := len(c.changes) - taskCrewChangesKept; over > 0 {
		c.changes = append([]TaskCrewChange(nil), c.changes[over:]...)
	}
	c.changeCount++
}

// heldLocked is whether the role's newest change already says it was held
// on send for the same failure. The caller holds c.mu.
func (c *taskCrew) heldLocked(seat crewroute.Seat, send, why string) bool {
	for i := len(c.changes) - 1; i >= 0; i-- {
		if change := c.changes[i]; change.Seat == seat {
			return change.Kind == TaskCrewChangeHeld && change.FromSend == send && change.Why == why
		}
	}
	return false
}

// noteRestored marks one restart of the task's crew, durably once persisted.
func (c *taskCrew) noteRestored(at time.Time, prior []time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restored = append(append([]time.Time(nil), prior...), at)
	if over := len(c.restored) - taskCrewRestoresKept; over > 0 {
		c.restored = c.restored[over:]
	}
	c.noteChangeLocked(TaskCrewChange{At: at, By: TaskCrewByEngine, Kind: TaskCrewChangeRestored})
}

// ── the read ────────────────────────────────────────────────────────────────

// The states a task's routing can be in.
const (
	// TaskRoutingRouted is a task with a routed crew.
	TaskRoutingRouted = "routed"
	// TaskRoutingPicking is a task not started yet: nothing is decided.
	TaskRoutingPicking = "picking"
	// TaskRoutingBlocked is a task waiting because a role had no model.
	TaskRoutingBlocked = "blocked"
	// TaskRoutingSingle is a task with no routing record: one recorded model,
	// and nothing known about planner or checker.
	TaskRoutingSingle = "single"
)

// The sources of a role's pick.
const (
	TaskRoleAuto     = "auto"
	TaskRolePinned   = "pinned"
	TaskRoleTaskPin  = "task_pin"
	TaskRoleOneModel = "one_model"
)

// The states of one role.
const (
	TaskRoleUsed      = "used"
	TaskRolePending   = "pending"
	TaskRoleNotRunYet = "not_run_yet"
	TaskRoleNotUsed   = "not_used"
)

// TaskRoutingView is one task's models as recorded.
type TaskRoutingView struct {
	Task    string
	State   string
	Mode    crewroute.Mode
	Running bool
	Settled bool
	// Restored is every time the crew came back from its checkpoint.
	Restored []time.Time
	Roles    []TaskRoutingRole
	// Model is the one recorded model of a [TaskRoutingSingle] task.
	Model string
	// EstimateUSD is the router's estimate for the crew as it stands; nil
	// when a seat's price is unknown.
	EstimateUSD *float64
	// SpentUSD is what answered calls with a known cost spent, helpers
	// included; Calls and UnknownCalls count the answered calls and those
	// whose cost nobody could say, which SpentUSD does not include.
	SpentUSD            float64
	Calls, UnknownCalls int
	HelperUSD           float64
	TaskCapUSD          float64
	Class, Why, Effort  string
	Note, Stopped       string
	Considered          int
	Warnings            []crewroute.SeatWarning
	// Changes are the newest changes, oldest first; ChangeCount is every one.
	Changes     []TaskCrewChange
	ChangeCount int
	Blocked     *TaskRoutingBlock
	// Requested is a not-started task's per-role choice; Effort "best" there
	// is a not-started task asked for the best crew.
	Requested map[crewroute.Seat]string
}

// TaskRoutingRole is one role of a routed task.
type TaskRoutingRole struct {
	Seat                crewroute.Seat
	Model, Provider     string
	Send                string
	Kind                crewroute.RouteKind
	Source              string
	Status              string
	Quality             float64
	EstimateUSD         *float64
	SpentUSD            float64
	Calls, UnknownCalls int
	Alternatives        []TaskRoutingOption
	Rejected            []TaskRoutingRejection
}

// TaskRoutingOption is one model the router lined up behind a role.
type TaskRoutingOption struct {
	Model, Provider string
	Kind            crewroute.RouteKind
	Quality         float64
	EstimateUSD     *float64
}

// TaskRoutingRejection is one route that failed this task's role.
type TaskRoutingRejection struct {
	Model, Provider string
	Reason          string
	Until           time.Time
}

// TaskRoutingBlock is why a task waits: the role with no model, and why each
// considered model was turned away.
type TaskRoutingBlock struct {
	Seat     crewroute.Seat
	Mode     crewroute.Mode
	Rejected []crewroute.Rejection
	Omitted  int
	Action   string
	Message  string
	At       time.Time
}

// taskRoutingAlternatives bounds the options one role lists.
const taskRoutingAlternatives = 5

// TaskRouting reads one task's models. changes bounds the history returned
// (1..64; 0 is 20).
func (a *Agent) TaskRouting(id string, changes int) (TaskRoutingView, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil || n == 0 {
		return TaskRoutingView{}, &TaskRoutingRefusal{Code: TaskRoutingNotFound, Message: "no such task"}
	}
	if changes <= 0 {
		changes = 20
	}
	if changes > taskCrewChangesKept {
		changes = taskCrewChangesKept
	}
	if block, ok := a.taskRouting.parkedBlock(n); ok {
		return TaskRoutingView{Task: id, State: TaskRoutingBlocked, Mode: block.Mode, Blocked: &block, Requested: a.taskRouting.requested(n)}, nil
	}
	a.mu.Lock()
	_, proposed := a.taskAnswers[n]
	a.mu.Unlock()
	if proposed {
		view := TaskRoutingView{Task: id, State: TaskRoutingPicking, Requested: a.taskRouting.requested(n)}
		if wish, ok := a.taskRouting.wish(n); ok {
			view.Effort = string(wish.effort)
		}
		return view, nil
	}
	if row, ok := a.appRunRow(n); ok {
		var record *TaskCrewRecord
		if crew := a.crews.get(n); crew != nil {
			crew.persistMu.Lock()
			record = crew.record()
			crew.persistMu.Unlock()
		} else if row.CrewState != nil {
			record = row.CrewState
		} else if row.Crew != nil {
			record = &TaskCrewRecord{Version: 1, Routed: true, Decision: *row.Crew}
		}
		settled := row.State.settled()
		if record == nil || !record.Routed {
			return TaskRoutingView{Task: id, State: TaskRoutingSingle, Model: row.Model, Running: !settled, Settled: settled}, nil
		}
		return taskRoutingOf(id, record, settled, changes), nil
	}
	if node := a.taskNode(n); node != nil {
		notice := node.notice()
		settled := notice.State.settled()
		return TaskRoutingView{Task: id, State: TaskRoutingSingle, Model: notice.Model, Running: !settled, Settled: settled}, nil
	}
	return TaskRoutingView{}, &TaskRoutingRefusal{Code: TaskRoutingNotFound, Message: "no such task"}
}

// taskRoutingOf is a routed task's view from its crew record.
func taskRoutingOf(id string, r *TaskCrewRecord, settled bool, changes int) TaskRoutingView {
	d := r.Decision
	mode := d.Mode
	if mode == "" {
		mode = crewroute.ModeAuto
	}
	view := TaskRoutingView{
		Task: id, State: TaskRoutingRouted, Mode: mode, Running: !settled, Settled: settled,
		Restored: append([]time.Time(nil), r.Restored...), HelperUSD: r.HelperUSD,
		Class: string(d.Class), Why: d.Why, Effort: string(d.Effort), Note: d.Note, Stopped: d.Stopped,
		Considered: d.Considered, Warnings: append([]crewroute.SeatWarning(nil), d.Warnings...), ChangeCount: r.ChangeCount,
	}
	if from := len(r.Changes) - changes; from > 0 {
		view.Changes = append([]TaskCrewChange(nil), r.Changes[from:]...)
	} else {
		view.Changes = append([]TaskCrewChange(nil), r.Changes...)
	}
	g := r.Guard
	if g != nil {
		view.TaskCapUSD = g.TaskCap
		view.SpentUSD = g.TaskSpent
		view.Calls, view.UnknownCalls = g.TaskCalls, g.TaskUnknown
	}
	view.SpentUSD += r.HelperUSD
	known, total := true, 0.0
	for _, seat := range crewroute.Seats {
		pick := d.Seat(seat)
		role := TaskRoutingRole{Seat: seat, Model: pick.Model, Provider: pick.Provider, Send: pick.Send, Kind: pick.Kind, Quality: pick.Quality, EstimateUSD: pickEstimate(pick)}
		switch {
		case mode == crewroute.ModeOne:
			role.Source = TaskRoleOneModel
		case r.TaskPins[seat]:
			role.Source = TaskRoleTaskPin
		case pick.Pinned:
			role.Source = TaskRolePinned
		default:
			role.Source = TaskRoleAuto
		}
		if role.EstimateUSD == nil {
			known = false
		} else {
			total += *role.EstimateUSD
		}
		if g != nil {
			role.SpentUSD = g.SeatSpent[seat]
			role.Calls, role.UnknownCalls = g.SeatCalls[seat], g.SeatUnknown[seat]
		}
		used := role.Calls > 0 || role.SpentUSD > 0 || (pick.Send != "" && r.Started[pick.Send])
		switch {
		case r.Pending[seat]:
			role.Status = TaskRolePending
		case used:
			role.Status = TaskRoleUsed
		case settled:
			role.Status = TaskRoleNotUsed
		default:
			role.Status = TaskRoleNotRunYet
		}
		for _, rung := range r.Ladders[seat] {
			if len(role.Alternatives) == taskRoutingAlternatives {
				break
			}
			if rung.Send == pick.Send {
				continue
			}
			role.Alternatives = append(role.Alternatives, TaskRoutingOption{Model: rung.Model, Provider: rung.Provider, Kind: rung.Kind, Quality: rung.Quality, EstimateUSD: pickEstimate(rung)})
		}
		sends := map[string]bool{r.Original[seat]: true}
		for _, change := range r.Changes {
			if change.Seat == seat {
				sends[change.FromSend], sends[change.ToSend] = true, true
			}
		}
		for _, send := range crewKeys(sends) {
			if failed, ok := r.Failed[send]; ok && send != "" {
				model, provider := send, ""
				for _, change := range r.Changes {
					if change.FromSend == send {
						model, provider = change.From, change.FromProvider
					}
				}
				role.Rejected = append(role.Rejected, TaskRoutingRejection{Model: model, Provider: provider, Reason: string(failed.Kind), Until: failed.Until})
			}
		}
		view.Roles = append(view.Roles, role)
	}
	if known {
		if d.EstUSD > 0 && d.CostFactor > 0 {
			total = d.EstUSD
		}
		view.EstimateUSD = &total
	}
	return view
}

// pickEstimate is one pick's expected cost: nothing on a plan, a local model
// or a free pool, the router's figure on a priced route, and unknown (nil)
// on a metered route nobody priced.
func pickEstimate(pick crewroute.Pick) *float64 {
	zero := 0.0
	switch pick.Kind {
	case crewroute.Plan, crewroute.Local, crewroute.Free:
		return &zero
	}
	switch {
	case pick.EstUSD > 0:
		v := pick.EstUSD
		return &v
	case pick.CostUSD > 0 && !math.IsInf(pick.CostUSD, 0):
		v := pick.CostUSD
		return &v
	}
	return nil
}

// ── the act ─────────────────────────────────────────────────────────────────

// The kinds of change a person can ask for.
const (
	TaskRoutingStronger = "stronger_worker"
	TaskRoutingBest     = "best"
	TaskRoutingPick     = "pick"
	TaskRoutingAuto     = "auto"
	TaskRoutingSame     = "same"
	TaskRoutingRetry    = "retry"
)

// The refusal codes.
const (
	TaskRoutingNotFound    = "not_found"
	TaskRoutingBadAction   = "bad_action"
	TaskRoutingOverCap     = "over_cap"
	TaskRoutingDayCap      = "day_cap"
	TaskRoutingNoEligible  = "no_eligible"
	TaskRoutingNotAllowed  = "not_allowed"
	TaskRoutingRouteFailed = "route_failed"
	TaskRoutingUnchanged   = "unchanged"
	TaskRoutingStrongest   = "strongest"
	TaskRoutingSettled     = "settled"
	TaskRoutingBusy        = "busy"
	TaskRoutingStale       = "stale"
	TaskRoutingUnavailable = "unavailable"
	TaskRoutingNotRouted   = "not_routed"
)

// TaskRoutingAction is one change a person asked for on one task.
type TaskRoutingAction struct {
	Kind string
	// Picks are per-role models for Kind pick; an empty model is Auto for
	// that role.
	Picks map[crewroute.Seat]string
	// Rerun starts a finished task again with this choice.
	Rerun bool
	// RaiseCapUSD is a task cap the person explicitly allows for this task.
	RaiseCapUSD float64
	// ChangeCount, when set, is the history count the person saw; a task that
	// changed since is refused as stale.
	ChangeCount *int
}

// TaskRoutingResult is what an action did.
type TaskRoutingResult struct {
	View    TaskRoutingView
	Applied []crewroute.Seat
	// Rerun is the id of the task a rerun started.
	Rerun uint64
}

// TaskRoutingRefusal is an action the engine did not take, typed.
type TaskRoutingRefusal struct {
	Code    string
	Message string
	Seat    crewroute.Seat
	// RequiredUSD is the cap the change needs and CapUSD the cap it met.
	RequiredUSD, CapUSD float64
	Blocked             *TaskRoutingBlock
}

func (e *TaskRoutingRefusal) Error() string { return e.Message }

func refuse(code, format string, args ...any) error {
	return &TaskRoutingRefusal{Code: code, Message: fmt.Sprintf(format, args...)}
}

// TaskRoutingAct is a person's change to one task's models.
func (a *Agent) TaskRoutingAct(ctx context.Context, id string, act TaskRoutingAction) (TaskRoutingResult, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil || n == 0 {
		return TaskRoutingResult{}, refuse(TaskRoutingNotFound, "no such task")
	}
	switch act.Kind {
	case TaskRoutingStronger, TaskRoutingBest, TaskRoutingAuto, TaskRoutingSame, TaskRoutingRetry:
	case TaskRoutingPick:
		if len(act.Picks) == 0 {
			return TaskRoutingResult{}, refuse(TaskRoutingBadAction, "pick needs at least one role")
		}
		for seat := range act.Picks {
			if seat != crewroute.Worker && seat != crewroute.Planner && seat != crewroute.Checker {
				return TaskRoutingResult{}, refuse(TaskRoutingBadAction, "%q is not a role · worker, planner or checker", seat)
			}
		}
	default:
		return TaskRoutingResult{}, refuse(TaskRoutingBadAction, "%q is not a change · stronger_worker, best, pick, auto, same or retry", act.Kind)
	}
	if act.RaiseCapUSD < 0 || math.IsNaN(act.RaiseCapUSD) || math.IsInf(act.RaiseCapUSD, 0) {
		return TaskRoutingResult{}, refuse(TaskRoutingBadAction, "a raised cap is a dollar figure above zero")
	}
	var applied []crewroute.Seat
	var rerun uint64
	switch {
	case a.taskRouting.isParked(n):
		err = a.actOnParked(ctx, n, act)
	case a.isProposed(n):
		err = a.actOnProposal(n, act)
	default:
		row, ok := a.appRunRow(n)
		if !ok {
			return TaskRoutingResult{}, refuse(TaskRoutingNotFound, "no such task")
		}
		crew := a.crews.get(n)
		switch {
		case act.Rerun || row.State.settled():
			if !act.Rerun {
				return TaskRoutingResult{}, refuse(TaskRoutingSettled, "this task has finished · its models are kept as they ran; rerun it to choose again")
			}
			if !row.State.settled() {
				return TaskRoutingResult{}, refuse(TaskRoutingBusy, "this task is still running · stop it first, then rerun it")
			}
			rerun, err = a.rerunWithRouting(ctx, row, crew, act)
		case crew == nil:
			return TaskRoutingResult{}, refuse(TaskRoutingNotRouted, "this task runs on one recorded model and has no roles to change")
		default:
			applied, err = a.actOnLiveCrew(n, crew, act)
		}
	}
	if err != nil {
		return TaskRoutingResult{}, err
	}
	view, err := a.TaskRouting(id, 0)
	if err != nil {
		return TaskRoutingResult{}, err
	}
	return TaskRoutingResult{View: view, Applied: applied, Rerun: rerun}, nil
}

func (a *Agent) isProposed(n uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.taskAnswers[n]
	return ok
}

// actOnProposal keeps a not-started task's choice for the moment it is
// routed. Nothing is decided yet, so there is nothing to be stronger than.
func (a *Agent) actOnProposal(n uint64, act TaskRoutingAction) error {
	if act.Rerun {
		return refuse(TaskRoutingBadAction, "this task has not run · there is nothing to rerun")
	}
	switch act.Kind {
	case TaskRoutingStronger, TaskRoutingSame, TaskRoutingRetry:
		return refuse(TaskRoutingBadAction, "this task has not started · choose its models with pick, best or auto")
	}
	a.taskRouting.change(n, func(w *crewWish) {
		switch act.Kind {
		case TaskRoutingAuto:
			w.sends, w.effort = nil, ""
		case TaskRoutingBest:
			w.sends, w.effort = nil, crewroute.EffortBest
		case TaskRoutingPick:
			if w.sends == nil {
				w.sends = map[crewroute.Seat]string{}
			}
			for seat, model := range act.Picks {
				if model = strings.TrimSpace(model); model == "" {
					delete(w.sends, seat)
				} else {
					w.sends[seat] = model
				}
			}
		}
		if act.RaiseCapUSD > 0 {
			w.taskCap = act.RaiseCapUSD
		}
	})
	return nil
}

// crewTaskText is the words a crew was routed on.
func crewTaskText(title, brief string) crewroute.Task {
	return crewroute.Task{Text: strings.TrimSpace(title + "\n\n" + brief)}
}

// routeRefusal is a router error as an action's typed refusal.
func routeRefusal(err error) error {
	var none *crewroute.NoEligibleError
	switch {
	case errors.As(err, &none):
		block := blockOf(none, time.Now())
		return &TaskRoutingRefusal{Code: TaskRoutingNoEligible, Message: none.Error(), Seat: none.Seat, Blocked: &block}
	case errors.Is(err, config.ErrCrewAtCap):
		return refuse(TaskRoutingDayCap, "today's spend has reached the daily cap · nothing was changed")
	case errors.Is(err, crewroute.ErrStrongest):
		return refuse(TaskRoutingStrongest, "this is already the strongest model the allowed models make for that role")
	}
	return refuse(TaskRoutingUnavailable, "the models could not be chosen: %v", err)
}

func blockOf(none *crewroute.NoEligibleError, at time.Time) TaskRoutingBlock {
	return TaskRoutingBlock{Seat: none.Seat, Mode: none.Mode, Rejected: append([]crewroute.Rejection(nil), none.Rejected...),
		Omitted: none.Omitted, Action: none.Action, Message: none.Error(), At: at}
}

// actOnLiveCrew moves a running task's roles at their next call.
func (a *Agent) actOnLiveCrew(n uint64, crew *taskCrew, act TaskRoutingAction) ([]crewroute.Seat, error) {
	route := a.config.RouteCrew
	if route == nil {
		return nil, refuse(TaskRoutingUnavailable, "this conversation has no model router")
	}
	if act.Kind == TaskRoutingSame || act.Kind == TaskRoutingRetry {
		return nil, refuse(TaskRoutingBadAction, "%s is for a rerun or a waiting task", act.Kind)
	}
	crew.mu.Lock()
	cur, count := crew.decision, crew.changeCount
	pinned := map[crewroute.Seat]bool{}
	for seat := range crew.taskPins {
		pinned[seat] = true
	}
	for seat := range crew.seatSends {
		pinned[seat] = true
	}
	crew.mu.Unlock()
	if act.ChangeCount != nil && *act.ChangeCount != count {
		return nil, refuse(TaskRoutingStale, "this task's models changed · open it again")
	}
	ask := config.CrewAsk{Task: crewTaskText(crew.title, crew.brief), Repo: crew.repo, ChatModel: a.Model(), Sends: map[crewroute.Seat]string{}}
	keep := func(seats ...crewroute.Seat) {
		for _, seat := range seats {
			if send := cur.Seat(seat).Send; send != "" {
				ask.Sends[seat] = send
			}
		}
	}
	var targets []crewroute.Seat
	switch act.Kind {
	case TaskRoutingStronger:
		keep(crewroute.Planner, crewroute.Checker)
		ask.Stronger = &cur
		targets = []crewroute.Seat{crewroute.Worker}
	case TaskRoutingBest:
		ask.Effort = crewroute.EffortBest
		targets = crewroute.Seats
	case TaskRoutingPick:
		for _, seat := range crewroute.Seats {
			model, named := act.Picks[seat]
			switch {
			case !named:
				keep(seat)
			case strings.TrimSpace(model) != "":
				ask.Sends[seat] = strings.TrimSpace(model)
				targets = append(targets, seat)
			default:
				targets = append(targets, seat)
			}
		}
	case TaskRoutingAuto:
		for _, seat := range crewroute.Seats {
			if pinned[seat] {
				targets = append(targets, seat)
			} else {
				keep(seat)
			}
		}
		if len(targets) == 0 {
			return nil, refuse(TaskRoutingUnchanged, "no role of this task was chosen for it · it is already on Auto")
		}
	}
	if len(ask.Sends) == 0 {
		ask.Sends = nil
	}
	decision, err := route(ask)
	if err != nil {
		return nil, routeRefusal(err)
	}
	next := cur
	next.Crew = append([]crewroute.Pick(nil), cur.Crew...)
	var changed []crewroute.Seat
	for _, seat := range targets {
		pick := decision.Seat(seat)
		if pick.Send == "" {
			continue
		}
		if act.Kind == TaskRoutingPick {
			if want := strings.TrimSpace(act.Picks[seat]); want != "" {
				if pick.Send != want && pick.Model != want {
					return nil, &TaskRoutingRefusal{Code: TaskRoutingNotAllowed, Seat: seat,
						Message: fmt.Sprintf("%s cannot take the %s role here · %s", want, seat, warningFor(decision, seat))}
				}
				pick.Pinned = true
			}
		}
		pick.Seat = seat
		if pick.Send == cur.Seat(seat).Send && pick.Pinned == cur.Seat(seat).Pinned {
			if act.Kind != TaskRoutingAuto && act.Kind != TaskRoutingPick {
				continue
			}
		}
		if _, failed := crew.failedHere(pick.Send); failed {
			return nil, &TaskRoutingRefusal{Code: TaskRoutingRouteFailed, Seat: seat,
				Message: fmt.Sprintf("%s already failed to start on this task", pick.Model)}
		}
		for i := range next.Crew {
			if next.Crew[i].Seat == seat {
				next.Crew[i] = pick
			}
		}
		changed = append(changed, seat)
	}
	if len(changed) == 0 {
		if act.Kind == TaskRoutingStronger {
			return nil, refuse(TaskRoutingStrongest, "this is already the strongest worker the allowed models make")
		}
		return nil, refuse(TaskRoutingUnchanged, "the models this asks for are the ones the task runs on")
	}
	estimate, known := crewEstimate(next)
	capUSD := 0.0
	if crew.guard != nil {
		crew.guard.mu.Lock()
		capUSD = crew.guard.TaskCap
		crew.guard.mu.Unlock()
	}
	raise := 0.0
	if known && capUSD > 0 && estimate > capUSD+1e-9 {
		if act.RaiseCapUSD+1e-9 < estimate {
			return nil, &TaskRoutingRefusal{Code: TaskRoutingOverCap, RequiredUSD: estimate, CapUSD: capUSD,
				Message: fmt.Sprintf("this needs more than the %s task cap · it needs %s", crewroute.Money(capUSD), crewroute.Money(estimate))}
		}
		raise = act.RaiseCapUSD
	}
	at := a.taskClockNow()
	kind := act.Kind
	crew.mu.Lock()
	if crew.changeCount != count {
		crew.mu.Unlock()
		return nil, refuse(TaskRoutingStale, "this task's models changed · open it again")
	}
	for _, seat := range changed {
		was, pick := crew.decision.Seat(seat), next.Seat(seat)
		if crew.seatSends == nil {
			crew.seatSends = map[crewroute.Seat]string{}
		}
		crew.seatSends[seat] = pick.Send
		if crew.ladders == nil {
			crew.ladders = map[crewroute.Seat][]crewroute.Pick{}
		}
		if pick.Pinned {
			delete(crew.ladders, seat)
		} else {
			crew.ladders[seat] = decision.Ladder[seat]
		}
		if crew.pending == nil {
			crew.pending = map[crewroute.Seat]bool{}
		}
		if !crew.started[pick.Send] {
			crew.pending[seat] = true
		} else {
			delete(crew.pending, seat)
		}
		if kind == TaskRoutingPick && strings.TrimSpace(act.Picks[seat]) != "" {
			if crew.taskPins == nil {
				crew.taskPins = map[crewroute.Seat]bool{}
			}
			crew.taskPins[seat] = true
		} else {
			delete(crew.taskPins, seat)
		}
		crew.noteChangeLocked(TaskCrewChange{At: at, Seat: seat, From: was.Model, FromProvider: was.Provider, FromSend: was.Send,
			To: pick.Model, ToProvider: pick.Provider, ToSend: pick.Send, By: TaskCrewByPerson, Kind: kind})
	}
	next.Retried, next.Ladder = crew.decision.Retried, crew.ladders
	if known {
		next.EstUSD = estimate
	}
	crew.decision = next
	if raise > 0 {
		crew.noteChangeLocked(TaskCrewChange{At: at, By: TaskCrewByPerson, Kind: TaskCrewChangeCap, CapUSD: raise})
	}
	crew.mu.Unlock()
	if raise > 0 && crew.guard != nil {
		crew.guard.mu.Lock()
		crew.guard.TaskCap = raise
		crew.guard.mu.Unlock()
	}
	if crew.persist != nil {
		if err := crew.persist(); err != nil {
			return changed, err
		}
	}
	var lines []string
	for _, seat := range changed {
		lines = append(lines, fmt.Sprintf("%s changed %s → %s", seat, cur.Seat(seat).Model, next.Seat(seat).Model))
	}
	a.republishCrewRow(n, next, "you changed this task's models · "+strings.Join(lines, " · ")+" · from its next step")
	return changed, nil
}

// warningFor is what the router said about a role it did not seat as asked.
func warningFor(d crewroute.Decision, seat crewroute.Seat) string {
	for _, w := range d.Warnings {
		if w.Seat == seat {
			return string(w.Kind)
		}
	}
	if d.Mode == crewroute.ModeOne {
		return "one model runs every role"
	}
	return "not allowed"
}

// crewEstimate is a crew's estimate, and false when a seat's price is unknown.
func crewEstimate(d crewroute.Decision) (float64, bool) {
	total := 0.0
	for _, seat := range crewroute.Seats {
		pick := d.Seat(seat)
		if pick.Send == "" {
			continue
		}
		est := pickEstimate(pick)
		if est == nil {
			return 0, false
		}
		total += *est
	}
	if d.CostFactor > 1 {
		total *= d.CostFactor
	}
	return total, true
}

// republishCrewRow puts a crew on its row and says the change on the plan.
func (a *Agent) republishCrewRow(row uint64, decision crewroute.Decision, note string) {
	g := a.graph()
	if g == nil {
		return
	}
	for _, kept := range g.runRows(row) {
		if kept.ID != row {
			continue
		}
		kept.Crew = &decision
		kept.Model = decision.Seat(crewroute.Worker).Model
		a.publishRunRow(g, kept)
		break
	}
	g.planNote(note)
}

// rerunWithRouting starts a finished task again with the person's choice. The
// finished task's record is never changed.
func (a *Agent) rerunWithRouting(ctx context.Context, row TaskNotice, crew *taskCrew, act TaskRoutingAction) (uint64, error) {
	var prior *crewroute.Decision
	brief, title := "", row.Title
	switch {
	case crew != nil:
		d := crew.current()
		prior, brief, title = &d, crew.brief, crew.title
	case row.CrewState != nil && row.CrewState.Routed:
		d := row.CrewState.Decision
		prior, brief, title = &d, row.CrewState.Brief, row.CrewState.Title
	}
	if brief == "" {
		brief = row.Brief
	}
	if strings.TrimSpace(brief) == "" {
		return 0, refuse(TaskRoutingNotRouted, "this task's brief was not kept · it cannot be rerun from here")
	}
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	if live {
		return 0, refuse(TaskRoutingBusy, "other work is still running · rerun this task when it has finished")
	}
	wish := crewWish{}
	sendsOf := func(seats ...crewroute.Seat) map[crewroute.Seat]string {
		out := map[crewroute.Seat]string{}
		for _, seat := range seats {
			if prior != nil && prior.Seat(seat).Send != "" {
				out[seat] = prior.Seat(seat).Send
			}
		}
		return out
	}
	switch act.Kind {
	case TaskRoutingSame:
		if prior == nil {
			return 0, refuse(TaskRoutingNotRouted, "this task has no recorded roles to run again")
		}
		wish.sends = sendsOf(crewroute.Seats...)
	case TaskRoutingStronger:
		if prior == nil {
			return 0, refuse(TaskRoutingNotRouted, "this task has no recorded worker to be stronger than")
		}
		wish.sends, wish.stronger = sendsOf(crewroute.Planner, crewroute.Checker), prior
	case TaskRoutingBest:
		wish.effort = crewroute.EffortBest
	case TaskRoutingPick:
		wish.sends = map[crewroute.Seat]string{}
		for seat, model := range act.Picks {
			if model = strings.TrimSpace(model); model != "" {
				wish.sends[seat] = model
			}
		}
	case TaskRoutingRetry:
		return 0, refuse(TaskRoutingBadAction, "retry is for a waiting task")
	}
	if route := a.config.RouteCrew; route != nil {
		// THE RERUN'S ESTIMATE IS READ BEFORE ANYTHING STARTS, so a choice
		// over the task cap is refused with the cap it needs.
		ask := config.CrewAsk{Task: crewTaskText(title, brief), Effort: wish.effort, Stronger: wish.stronger, Sends: wish.sends,
			Repo: canonicalPath(a.config.Workspace), ChatModel: a.Model()}
		if len(ask.Sends) == 0 {
			ask.Sends = nil
		}
		decision, err := route(ask)
		if err != nil {
			return 0, routeRefusal(err)
		}
		capUSD, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
		if estimate, known := crewEstimate(decision); known && capUSD > 0 && estimate > capUSD+1e-9 {
			if act.RaiseCapUSD+1e-9 < estimate {
				return 0, &TaskRoutingRefusal{Code: TaskRoutingOverCap, RequiredUSD: estimate, CapUSD: capUSD,
					Message: fmt.Sprintf("this needs more than the %s task cap · it needs %s", crewroute.Money(capUSD), crewroute.Money(estimate))}
			}
			wish.taskCap = act.RaiseCapUSD
		}
	}
	id, _, _, err := a.startTaskRun(withCrewWish(ctx, wish), brief, true, "")
	if err != nil {
		var none *crewroute.NoEligibleError
		if errors.As(err, &none) || errors.Is(err, config.ErrCrewAtCap) || errors.Is(err, crewroute.ErrStrongest) {
			return 0, routeRefusal(err)
		}
		return 0, err
	}
	return id, nil
}

// ── choices kept before a task starts, and tasks waiting on a model ─────────

// TaskWaitingNoModel is a task parked because a role had no model allowed.
const TaskWaitingNoModel = "no model allowed"

// taskRoutingBook is the conversation's per-task choices for tasks not
// started yet, and its parked hand-offs.
type taskRoutingBook struct {
	mu     sync.Mutex
	wishes map[uint64]crewWish
	parked map[uint64]*parkedHandoff
}

// parkedHandoff is an approved hand-off that waits on a model.
type parkedHandoff struct {
	ctx       context.Context
	p         *stagedProposal
	spec      taskSpec
	elsewhere string
	block     TaskRoutingBlock
}

func (b *taskRoutingBook) change(id uint64, edit func(*crewWish)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wishes == nil {
		b.wishes = map[uint64]crewWish{}
	}
	w := b.wishes[id]
	edit(&w)
	b.wishes[id] = w
}

func (b *taskRoutingBook) wish(id uint64) (crewWish, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.wishes[id]
	return w, ok
}

func (b *taskRoutingBook) forget(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.wishes, id)
	delete(b.parked, id)
}

func (b *taskRoutingBook) requested(id uint64) map[crewroute.Seat]string {
	w, ok := b.wish(id)
	if !ok || len(w.sends) == 0 {
		return nil
	}
	out := map[crewroute.Seat]string{}
	for seat, send := range w.sends {
		out[seat] = send
	}
	return out
}

func (b *taskRoutingBook) isParked(id uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.parked[id] != nil
}

func (b *taskRoutingBook) parkedBlock(id uint64) (TaskRoutingBlock, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.parked[id]; p != nil {
		return p.block, true
	}
	return TaskRoutingBlock{}, false
}

func (b *taskRoutingBook) take(id uint64) *parkedHandoff {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.parked[id]
	delete(b.parked, id)
	return p
}

// proposalCrewWish is base with what the person chose for this task before
// it started.
func (a *Agent) proposalCrewWish(id uint64, base crewWish) crewWish {
	w, ok := a.taskRouting.wish(id)
	if !ok {
		return base
	}
	if w.effort != "" {
		base.effort = w.effort
	}
	if len(w.sends) > 0 {
		base.sends = w.sends
		if w.sends[crewroute.Worker] != "" {
			base.worker = ""
		}
	}
	if w.taskCap > 0 {
		base.taskCap = w.taskCap
	}
	return base
}

// parkUnroutedHandoff parks an approved hand-off a role had no model for, and
// answers the receipt. Anything else is not parked.
func (a *Agent) parkUnroutedHandoff(ctx context.Context, p *stagedProposal, spec taskSpec, elsewhere string, via *delegate.Delegate, err error) (string, bool) {
	var none *crewroute.NoEligibleError
	if err == nil {
		a.taskRouting.forget(p.id)
		return "", false
	}
	if via != nil || !errors.As(err, &none) {
		return "", false
	}
	g := a.graph()
	if g == nil {
		return "", false
	}
	block := blockOf(none, a.taskClockNow())
	a.taskRouting.mu.Lock()
	if a.taskRouting.parked == nil {
		a.taskRouting.parked = map[uint64]*parkedHandoff{}
	}
	a.taskRouting.parked[p.id] = &parkedHandoff{ctx: context.WithoutCancel(ctx), p: p, spec: spec, elsewhere: elsewhere, block: block}
	a.taskRouting.mu.Unlock()
	a.publishRunRow(g, TaskNotice{ID: p.id, Title: spec.title, Brief: spec.brief, State: TaskQueued, Waiting: TaskWaitingNoModel, Report: block.Message})
	return fmt.Sprintf("task %d is waiting: %s. It has asked the person and starts when a model is allowed for the %s role; nothing is running for it.", p.id, block.Message, none.Seat), true
}

// actOnParked answers a waiting task: a new choice, then a retry, or a retry.
func (a *Agent) actOnParked(ctx context.Context, n uint64, act TaskRoutingAction) error {
	if act.Rerun {
		return refuse(TaskRoutingBadAction, "this task has not run · there is nothing to rerun")
	}
	if act.Kind != TaskRoutingRetry {
		if err := a.actOnProposal(n, act); err != nil {
			return err
		}
	}
	return a.RetryParkedTask(n)
}

// RetryParkedTask starts a waiting task from where it stood. Still no model
// keeps it waiting, with the new reason.
func (a *Agent) RetryParkedTask(n uint64) error {
	parked := a.taskRouting.take(n)
	if parked == nil {
		return refuse(TaskRoutingNotFound, "that task is not waiting on a model")
	}
	spec := parked.spec
	ctx := withCrewWish(parked.ctx, a.proposalCrewWish(n, crewWish{effort: spec.crewEffort, worker: namedModel(spec)}))
	answer, refused, handled := a.commitProposalToRun(ctx, parked.p, spec, parked.elsewhere)
	if block, waiting := a.taskRouting.parkedBlock(n); waiting {
		return &TaskRoutingRefusal{Code: TaskRoutingNoEligible, Message: block.Message, Seat: block.Seat, Blocked: &block}
	}
	if !handled {
		return refuse(TaskRoutingUnavailable, "the task could not start here")
	}
	if refused {
		return refuse(TaskRoutingUnavailable, "%s", answer)
	}
	return nil
}

// DropParkedTask ends a waiting task as not started.
func (a *Agent) DropParkedTask(n uint64) bool {
	parked := a.taskRouting.take(n)
	if parked == nil {
		return false
	}
	a.taskRouting.forget(n)
	a.settleUnstartedHandoff(n, parked.spec.title, errors.New(parked.block.Message))
	return true
}
