package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// ONE AGENT'S OWN BUDGET, said as the engine holds it.
//
// settings.budget_usd is the profile's DEFAULT conversation limit, the figure
// a conversation with nothing of its own is built with. It is not any one
// agent's cap: an agent made with a budget, or raised through caps.raise, has
// its own figure, kept by the engine beside the conversation and bound again
// every time the conversation opens. [AgentBudget] is that figure, and where it
// came from, so a client never reads the default as an agent's cap.
//
// THREE SOURCES AND THREE LIMITS, NEVER CONFLATED. An agent's cap is its own
// (set), explicitly none (unlimited), or nothing of its own (inherited) — and
// what is in force is a figure, none, or not known. An inherited cap is the
// figure the conversation's engine read when it opened, which only that engine
// can say; with nothing holding the conversation it is unknown, and a client
// says so rather than guessing from today's default.
//
// THE CAP IS THE AGENT'S AND NOTHING ELSE'S. It is measured against the
// conversation's own spend over its whole life — the journaled figure, kept
// across reopens — and checked before each turn. A task's cap (task_cap_usd),
// the day cap and a task's role models are other limits with other doors.

// The scope every [AgentBudget] is in.
const BudgetScopeAgent = "agent"

// Where an agent's budget comes from.
const (
	BudgetSet       = "set"
	BudgetUnlimited = "unlimited"
	BudgetInherited = "inherited"
)

// What is in force: a figure, none, or not known.
const (
	LimitLimited   = "limited"
	LimitUnlimited = "unlimited"
	LimitUnknown   = "unknown"
)

// Which reading Limit and USD were taken from.
const (
	BudgetFromRecord = "record"
	BudgetFromEngine = "engine"
)

// AgentBudget is one agent's own budget. USD is present only when Limit is
// limited, BaseUSD only when Source is set, and RaisedUntil only while a raise
// for today lifts USD above BaseUSD. Engine is the open conversation's own
// reading, and only agents.budget.get and agents.budget.set carry it.
type AgentBudget struct {
	Scope       string             `json:"scope" enum:"agent"`
	Source      string             `json:"source" enum:"set,unlimited,inherited"`
	Limit       string             `json:"limit" enum:"limited,unlimited,unknown"`
	USD         *float64           `json:"usd,omitempty"`
	BaseUSD     *float64           `json:"base_usd,omitempty"`
	RaisedUntil *time.Time         `json:"raised_until,omitempty"`
	From        string             `json:"from" enum:"record,engine"`
	Engine      *AgentBudgetEngine `json:"engine,omitempty"`
}

// AgentBudgetEngine is what the open conversation checks its next turn
// against, and the spend it measures that against.
type AgentBudgetEngine struct {
	Limit    string   `json:"limit" enum:"limited,unlimited"`
	USD      *float64 `json:"usd,omitempty"`
	SpentUSD float64  `json:"spent_usd"`
}

// AgentBudgetSet is agents.budget.set as the engine is handed it: a positive
// figure, or nil for explicitly none.
type AgentBudgetSet struct {
	ID  string
	USD *float64
}

// AgentBudgetSetWire is agents.budget.set's params as they travel. usd is
// required: a positive amount, or null for none. Zero is refused rather than
// read as none, because a client that meant none can say so.
type AgentBudgetSetWire struct {
	ID  string   `json:"id"`
	USD *float64 `json:"usd" nullable:"true"`
}

// AgentBudgetResult is both methods' answer.
type AgentBudgetResult struct {
	Budget AgentBudget `json:"budget"`
}

// Budgeter is the engine's half of an agent's own budget. An [Engine] that is
// not one refuses agents.budget.* as unknown_method.
type Budgeter interface {
	AgentBudget(id string) (AgentBudget, error)
	SetAgentBudget(set AgentBudgetSet) (AgentBudget, error)
}

func init() {
	for name, m := range budgetMethods {
		methods[name] = m
	}
}

func budgeter(e Engine, name string) (Budgeter, error) {
	b, ok := e.(Budgeter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return b, nil
}

// decodeBudgetSet reads agents.budget.set's params, which is the one place
// a missing usd and a null one differ.
func decodeBudgetSet(raw json.RawMessage) (AgentBudgetSet, error) {
	var fields map[string]json.RawMessage
	if err := decode(raw, &fields); err != nil {
		return AgentBudgetSet{}, err
	}
	var set AgentBudgetSet
	for name, value := range fields {
		switch name {
		case "id":
			if err := json.Unmarshal(value, &set.ID); err != nil {
				return AgentBudgetSet{}, Errorf(CodeBadParams, "id must be a string")
			}
		case "usd":
		default:
			return AgentBudgetSet{}, Errorf(CodeBadParams, "agents.budget.set takes id and usd, not %s", name)
		}
	}
	if strings.TrimSpace(set.ID) == "" {
		return AgentBudgetSet{}, Errorf(CodeBadParams, "id is required")
	}
	value, ok := fields["usd"]
	if !ok {
		return AgentBudgetSet{}, Errorf(CodeBadParams, "usd is required: a positive amount, or null for none")
	}
	if strings.TrimSpace(string(value)) == "null" {
		return set, nil
	}
	var usd float64
	if err := json.Unmarshal(value, &usd); err != nil || !money(usd) {
		return AgentBudgetSet{}, Errorf(CodeBadParams, "usd must be a positive amount, or null for none")
	}
	set.USD = &usd
	return set, nil
}

var budgetMethods = map[string]method{
	"agents.budget.get": typed(func(e Engine, p IDParams) (*AgentBudgetResult, error) {
		b, err := budgeter(e, "agents.budget.get")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		budget, err := b.AgentBudget(id)
		if err != nil {
			return nil, err
		}
		return &AgentBudgetResult{budget}, nil
	}),
	"agents.budget.set": shaped[AgentBudgetSetWire](func(e Engine, raw json.RawMessage) (*AgentBudgetResult, error) {
		b, err := budgeter(e, "agents.budget.set")
		if err != nil {
			return nil, err
		}
		set, err := decodeBudgetSet(raw)
		if err != nil {
			return nil, err
		}
		budget, err := b.SetAgentBudget(set)
		if err != nil {
			return nil, err
		}
		return &AgentBudgetResult{budget}, nil
	}),
}
