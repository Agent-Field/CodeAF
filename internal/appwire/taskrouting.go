package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// V80: TASK MODELS ON A TASK — which model plans, works and checks one task.
//
// These are facts the engine recorded for the task: the routed picks, how each
// was chosen, what each role spent, every change and who made it. Nothing is
// read from today's settings and nothing here is wording: the app draws its own
// words from these values. A task never changes models by being read.
//
// tasks.routing.get is one task, bounded: at most [TaskRoutingChangesMax]
// changes and [TaskRoutingAlternativesMax] alternatives per role. The task
// list carries only the small [TaskModelsChip] on each row.

// The routing states of a task.
const (
	TaskRoutingRouted  = "routed"
	TaskRoutingPicking = "picking"
	TaskRoutingBlocked = "blocked"
	TaskRoutingSingle  = "single"
)

// The actions tasks.routing.act takes.
const (
	TaskRoutingStronger = "stronger_worker"
	TaskRoutingBest     = "best"
	TaskRoutingPick     = "pick"
	TaskRoutingAuto     = "auto"
	TaskRoutingSame     = "same"
	TaskRoutingRetry    = "retry"
)

// The bounds of one read.
const (
	TaskRoutingChangesDefault  = 20
	TaskRoutingChangesMax      = 64
	TaskRoutingAlternativesMax = 5
)

// TaskRouter is an engine that serves a task's models. It is asserted, so an
// engine without it answers unknown_method.
type TaskRouter interface {
	TaskRouting(id string, changes int) (TaskRouting, error)
	ActOnTaskRouting(p TaskRoutingActParams) (TaskRoutingActResult, error)
}

// TaskRouting is one task's models as the engine recorded them.
type TaskRouting struct {
	TaskID string `json:"task_id"`
	State  string `json:"state" enum:"routed,picking,blocked,single"`
	// Mode is how the roles were chosen at admission; empty before routing.
	Mode    string `json:"mode,omitempty" enum:"auto,one"`
	Running bool   `json:"running"`
	Settled bool   `json:"settled"`
	// Restored is every time the task's picks came back after a restart.
	Restored []time.Time       `json:"restored"`
	Roles    []TaskRoutingRole `json:"roles"`
	// Model is a single-state task's one recorded model; nothing is known
	// about its planner or checker.
	Model *ModelRef `json:"model,omitempty"`
	// EstimateUSD is the router's own estimate for the crew as it stands, null
	// when a role's price is unknown.
	EstimateUSD *float64 `json:"estimate_usd"`
	// SpentUSD is what calls with a known cost spent; UnknownCalls is how many
	// answered calls had no known cost and are NOT in SpentUSD.
	SpentUSD     float64  `json:"spent_usd"`
	Calls        int      `json:"calls"`
	UnknownCalls int      `json:"unknown_calls"`
	HelperUSD    float64  `json:"helper_usd"`
	TaskCapUSD   *float64 `json:"task_cap_usd"`
	// Class is what the router read the task as.
	Class    string               `json:"class,omitempty"`
	Effort   string               `json:"effort,omitempty" enum:"best,cheap"`
	Warnings []TaskRoutingWarning `json:"warnings"`
	// StoppedAction is the one action a task stopped on when a role had
	// nowhere left to go (add credit, reconnect).
	StoppedAction string              `json:"stopped_action,omitempty"`
	Changes       []TaskRoutingChange `json:"changes"`
	ChangeCount   int                 `json:"change_count"`
	Blocked       *TaskRoutingWait    `json:"blocked,omitempty"`
	// Requested is a not-started or waiting task's own per-role choice.
	Requested *TaskRoutingPicks `json:"requested,omitempty"`
}

// TaskRoutingRole is one role of a routed task.
type TaskRoutingRole struct {
	Role    string   `json:"role" enum:"worker,planner,checker"`
	Model   ModelRef `json:"model"`
	Billing string   `json:"billing,omitempty" enum:"metered,plan,local,free"`
	// Source is how the role got its model: Auto, a saved pin, a pin for this
	// task only, or one model for every role.
	Source string `json:"source" enum:"auto,pinned,task_pin,one_model"`
	// Status is whether the role has answered: pending is a person's change
	// not yet answered at the next step.
	Status       string   `json:"status" enum:"used,pending,not_run_yet,not_used"`
	Quality      float64  `json:"quality"`
	EstimateUSD  *float64 `json:"estimate_usd"`
	SpentUSD     float64  `json:"spent_usd"`
	Calls        int      `json:"calls"`
	UnknownCalls int      `json:"unknown_calls"`
	// Alternatives are the models the router lined up behind this role, in
	// order; Rejected are routes that failed on this task.
	Alternatives []TaskRoutingOption    `json:"alternatives"`
	Rejected     []TaskRoutingRejection `json:"rejected"`
}

// TaskRoutingOption is one model lined up behind a role.
type TaskRoutingOption struct {
	Model       ModelRef `json:"model"`
	Billing     string   `json:"billing,omitempty" enum:"metered,plan,local,free"`
	Quality     float64  `json:"quality"`
	EstimateUSD *float64 `json:"estimate_usd"`
}

// TaskRoutingRejection is one route that failed a role on this task; Reason
// is the failure kind (payment, auth, limit, unavailable, ...).
type TaskRoutingRejection struct {
	Model  ModelRef   `json:"model"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until,omitempty"`
}

// TaskRoutingWarning is what was wrong with a role's saved pin at admission.
type TaskRoutingWarning struct {
	Role  string   `json:"role" enum:"worker,planner,checker"`
	Kind  string   `json:"kind" enum:"pin_not_allowed,provider_unavailable"`
	Model ModelRef `json:"model"`
}

// TaskRoutingChange is one change to a task's models.
type TaskRoutingChange struct {
	At     time.Time `json:"at"`
	Role   string    `json:"role,omitempty" enum:"worker,planner,checker"`
	From   *ModelRef `json:"from,omitempty"`
	To     *ModelRef `json:"to,omitempty"`
	By     string    `json:"by" enum:"engine,person"`
	Kind   string    `json:"kind" enum:"fallback,held,stronger_worker,best,pick,auto,cap,restored"`
	Reason string    `json:"reason,omitempty"`
	CapUSD *float64  `json:"cap_usd,omitempty"`
}

// TaskRoutingWait is why a task waits: a role with no model allowed.
type TaskRoutingWait struct {
	Role     string              `json:"role" enum:"worker,planner,checker"`
	Mode     string              `json:"mode" enum:"auto,one"`
	Rejected []TaskModelRejected `json:"rejected"`
	Omitted  int                 `json:"omitted"`
	Action   string              `json:"action,omitempty"`
	At       time.Time           `json:"at"`
}

// TaskModelRejected is one model turned away from a role at admission.
type TaskModelRejected struct {
	Model  ModelRef `json:"model"`
	Reason string   `json:"reason" enum:"not_allowed,no_price,provider_unavailable,route_unavailable,cannot_do_role"`
}

// TaskRoutingPicks is a per-role choice; a role left out is unchanged, and a
// role sent as null is Auto.
type TaskRoutingPicks struct {
	Worker  *ModelRef `json:"worker,omitempty" nullable:"true"`
	Planner *ModelRef `json:"planner,omitempty" nullable:"true"`
	Checker *ModelRef `json:"checker,omitempty" nullable:"true"`
}

// TaskRoutingActParams is tasks.routing.act's params as the engine reads them.
type TaskRoutingActParams struct {
	ID     string
	Action string
	// Picks holds a role only when the call names it; nil is Auto.
	Picks       map[string]*ModelRef
	Rerun       bool
	RaiseCapUSD float64
	ChangeCount *int
}

// TaskRoutingActWire is tasks.routing.act's params as they travel.
type TaskRoutingActWire struct {
	ID          string            `json:"id"`
	Action      string            `json:"action" enum:"stronger_worker,best,pick,auto,same,retry"`
	Picks       *TaskRoutingPicks `json:"picks,omitempty"`
	Rerun       bool              `json:"rerun,omitempty"`
	RaiseCapUSD *float64          `json:"raise_cap_usd,omitempty"`
	ChangeCount *int              `json:"change_count,omitempty"`
}

// TaskRoutingRefused is an action the engine did not take, as an answer
// rather than an error: the app offers what it can do next from Code.
type TaskRoutingRefused struct {
	Code    string `json:"code" enum:"over_cap,day_cap,no_eligible,not_allowed,route_failed,unchanged,strongest,settled,busy,stale,not_routed"`
	Message string `json:"message"`
	Role    string `json:"role,omitempty" enum:"worker,planner,checker"`
	// RequiredCapUSD is the task cap the change needs; CapUSD the cap it met.
	RequiredCapUSD *float64         `json:"required_cap_usd,omitempty"`
	CapUSD         *float64         `json:"cap_usd,omitempty"`
	Blocked        *TaskRoutingWait `json:"blocked,omitempty"`
}

// TaskRoutingActResult is tasks.routing.act's answer: the task as it now
// stands, the roles that move at their next step, or the refusal.
type TaskRoutingActResult struct {
	Routing     TaskRouting         `json:"routing"`
	Applied     []string            `json:"applied"`
	RerunTaskID string              `json:"rerun_task_id,omitempty"`
	Refused     *TaskRoutingRefused `json:"refused,omitempty"`
}

// TaskRoutingResult is tasks.routing.get's answer.
type TaskRoutingResult struct {
	Routing TaskRouting `json:"routing"`
}

// TaskModelsChip is the task list's small summary of a task's models: its
// state, mode and each role's model, read from the row the engine published.
type TaskModelsChip struct {
	State string           `json:"state" enum:"routed,picking,blocked,single"`
	Mode  string           `json:"mode,omitempty" enum:"auto,one"`
	Roles TaskRoutingPicks `json:"roles"`
	// Pinned counts roles a person pinned; Distinct the distinct models.
	Pinned   int `json:"pinned"`
	Distinct int `json:"distinct"`
}

func init() {
	methods["tasks.routing.get"] = typed(func(e Engine, p struct {
		ID      string `json:"id"`
		Changes int    `json:"changes"`
	}) (*TaskRoutingResult, error) {
		r, err := taskRouter(e, "tasks.routing.get")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		if p.Changes < 0 || p.Changes > TaskRoutingChangesMax {
			return nil, Errorf(CodeBadParams, "changes is 1 to %d", TaskRoutingChangesMax)
		}
		if p.Changes == 0 {
			p.Changes = TaskRoutingChangesDefault
		}
		got, err := r.TaskRouting(id, p.Changes)
		if err != nil {
			return nil, err
		}
		return &TaskRoutingResult{normalTaskRouting(got)}, nil
	})
	methods["tasks.routing.act"] = shaped[TaskRoutingActWire](func(e Engine, raw json.RawMessage) (*TaskRoutingActResult, error) {
		r, err := taskRouter(e, "tasks.routing.act")
		if err != nil {
			return nil, err
		}
		p, err := decodeTaskRoutingAct(raw)
		if err != nil {
			return nil, err
		}
		got, err := r.ActOnTaskRouting(p)
		if err != nil {
			return nil, err
		}
		got.Routing = normalTaskRouting(got.Routing)
		got.Applied = nonNil(got.Applied)
		return &got, nil
	})
}

func taskRouter(e Engine, name string) (TaskRouter, error) {
	r, ok := e.(TaskRouter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return r, nil
}

// normalTaskRouting answers empty lists, never null.
func normalTaskRouting(t TaskRouting) TaskRouting {
	t.Restored, t.Roles, t.Warnings, t.Changes = nonNil(t.Restored), nonNil(t.Roles), nonNil(t.Warnings), nonNil(t.Changes)
	for i := range t.Roles {
		t.Roles[i].Alternatives, t.Roles[i].Rejected = nonNil(t.Roles[i].Alternatives), nonNil(t.Roles[i].Rejected)
	}
	if t.Blocked != nil {
		t.Blocked.Rejected = nonNil(t.Blocked.Rejected)
	}
	return t
}

// decodeTaskRoutingAct reads tasks.routing.act's params: an id, one action,
// picks only for pick, nothing it does not know.
func decodeTaskRoutingAct(raw json.RawMessage) (TaskRoutingActParams, error) {
	var fields map[string]json.RawMessage
	if err := decode(raw, &fields); err != nil {
		return TaskRoutingActParams{}, err
	}
	var p TaskRoutingActParams
	for name, value := range fields {
		switch name {
		case "id":
			if json.Unmarshal(value, &p.ID) != nil {
				return p, Errorf(CodeBadParams, "id is a task id")
			}
		case "action":
			if json.Unmarshal(value, &p.Action) != nil {
				return p, Errorf(CodeBadParams, "action is a word")
			}
		case "picks":
			var picks map[string]json.RawMessage
			if json.Unmarshal(value, &picks) != nil || picks == nil {
				return p, Errorf(CodeBadParams, "picks is {worker?, planner?, checker?}")
			}
			p.Picks = map[string]*ModelRef{}
			for role, pick := range picks {
				if !taskModelRole(role) {
					return p, Errorf(CodeBadParams, "picks: %q is not a role · worker, planner or checker", role)
				}
				if string(pick) == "null" {
					p.Picks[role] = nil
					continue
				}
				var ref ModelRef
				if json.Unmarshal(pick, &ref) != nil {
					return p, Errorf(CodeBadParams, "picks.%s is {id, provider?} or null", role)
				}
				ref, err := checkModelRef("picks."+role, ref)
				if err != nil {
					return p, err
				}
				p.Picks[role] = &ref
			}
		case "rerun":
			if json.Unmarshal(value, &p.Rerun) != nil {
				return p, Errorf(CodeBadParams, "rerun is true or false")
			}
		case "raise_cap_usd":
			if json.Unmarshal(value, &p.RaiseCapUSD) != nil || p.RaiseCapUSD <= 0 || p.RaiseCapUSD > 1e6 {
				return p, Errorf(CodeBadParams, "raise_cap_usd is a dollar figure above zero")
			}
		case "change_count":
			var n int
			if json.Unmarshal(value, &n) != nil || n < 0 {
				return p, Errorf(CodeBadParams, "change_count is the count tasks.routing.get answered")
			}
			p.ChangeCount = &n
		default:
			return p, Errorf(CodeBadParams, "tasks.routing.act has no %s", name)
		}
	}
	if p.ID = strings.TrimSpace(p.ID); p.ID == "" {
		return p, Errorf(CodeBadParams, "tasks.routing.act needs an id")
	}
	switch p.Action {
	case TaskRoutingStronger, TaskRoutingBest, TaskRoutingAuto, TaskRoutingSame, TaskRoutingRetry:
		if p.Picks != nil {
			return p, Errorf(CodeBadParams, "picks go with action pick")
		}
	case TaskRoutingPick:
		if len(p.Picks) == 0 {
			return p, Errorf(CodeBadParams, "pick needs picks for at least one role")
		}
	default:
		return p, Errorf(CodeBadParams, "action is stronger_worker, best, pick, auto, same or retry")
	}
	return p, nil
}
