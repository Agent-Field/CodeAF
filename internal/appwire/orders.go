package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// Wave 6: standing orders and reminders (PROTOCOL.md). An order IS an
// automation (internal/automation): one store, one clock, the same rows the
// terminal's /automations list reads. The methods below are the app's words
// for what that store already does, plus the standing-order half the store
// keeps beside it: the ratify step, the trust ladder and where results are
// said.

// The events of wave 6.
const (
	EventOrderChanged = "order.changed"
	EventOrderRemoved = "order.removed"
	EventOrderFired   = "order.fired"
	EventOrderMissed  = "order.missed"
	EventOrderAsking  = "order.asking"
)

// The kinds of an order: a fixed line is a reminder, a brief is an order, and
// a look with a condition is a watch.
const (
	OrderKindOrder    = "order"
	OrderKindReminder = "reminder"
	OrderKindWatch    = "watch"
)

// The states of an order.
const (
	OrderProposed = "proposed"
	OrderActive   = "active"
	OrderPaused   = "paused"
	OrderStopped  = "stopped"
	OrderDone     = "done"
)

// The rungs of the trust ladder.
const (
	TrustAsksFirst = "asks_first"
	TrustEarning   = "earning"
	TrustTrusted   = "trusted"
)

// Where an order's results are said.
const (
	AnswersChat       = "chat"
	AnswersCrew       = "crew"
	AnswersEverywhere = "everywhere"
	AnswersNotHere    = "not_here"
)

// The results a run comes to, as an order's row says them.
const (
	ResultOK         = "ok"
	ResultQuiet      = "quiet"
	ResultIncomplete = "incomplete"
	ResultSkipped    = "skipped"
	ResultNotRight   = "not_right"
)

// The answers to a run that asks first; not right is [VerdictNotRight], the
// same word a task's verdict uses.
const (
	VerdictGo   = "go"
	VerdictSkip = "skip"
)

// OrderAskPrefix starts the id of a run's ask, "order:<run>", which asks.answer
// also takes with the option go, skip or not_right.
const OrderAskPrefix = "order:"

// Order is one standing order or reminder.
type Order struct {
	ID        string `json:"id"`
	Agent     string `json:"agent,omitempty"`
	Crew      string `json:"crew,omitempty"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Workspace string `json:"workspace,omitempty"`
	// When is the schedule as a person says it, WhenExact the same exactly.
	When      string `json:"when"`
	WhenExact string `json:"when_exact,omitempty"`
	// Does is the brief, or the reminder's line; Wont what it must never do.
	Does      string `json:"does"`
	Wont      string `json:"wont,omitempty"`
	AnswersIn string `json:"answers_in"`
	// Trust is the ladder's rung, ApprovedRuns how many go's it has had, and
	// TrustAfter how many make it trusted.
	Trust           string     `json:"trust"`
	ApprovedRuns    int        `json:"approved_runs"`
	TrustAfter      int        `json:"trust_after"`
	BudgetPerRunUSD float64    `json:"budget_per_run_usd"`
	TimePerRunS     int        `json:"time_per_run_s"`
	State           string     `json:"state"`
	FiresAt         *time.Time `json:"fires_at,omitempty"`
	LastRun         *OrderRun  `json:"last_run,omitempty"`
	// Asking is the run waiting for the person's go, when one is.
	Asking    *OrderRun `json:"asking,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// OrderRun is one run of an order.
type OrderRun struct {
	ID int64 `json:"id"`
	// AskID is set on a run waiting for the person: the id asks.answer takes.
	AskID string    `json:"ask_id,omitempty"`
	DueAt time.Time `json:"due_at"`
	At    time.Time `json:"at,omitempty"`
	// Result is set once it is over; Late says it started late, after a slot
	// missed while the Mac was asleep or codeaf was closed.
	Result string  `json:"result,omitempty"`
	Late   bool    `json:"late,omitempty"`
	Line   string  `json:"line,omitempty"`
	USD    float64 `json:"usd,omitempty"`
	// Transcript is the folder of the conversation a piece of work ran in.
	Transcript string `json:"transcript,omitempty"`
}

// OrderEdits is what orders.change changes; an absent field stays as it was.
type OrderEdits struct {
	Title           *string  `json:"title,omitempty"`
	When            *string  `json:"when,omitempty"`
	Does            *string  `json:"does,omitempty"`
	Wont            *string  `json:"wont,omitempty"`
	AnswersIn       *string  `json:"answers_in,omitempty"`
	Crew            *string  `json:"crew,omitempty"`
	BudgetPerRunUSD *float64 `json:"budget_per_run_usd,omitempty"`
	TimePerRunS     *int     `json:"time_per_run_s,omitempty"`
	Trust           *string  `json:"trust,omitempty"`
}

// OrderChanged is order.changed and order.missed.
type OrderChanged struct {
	Order Order `json:"order"`
}

// OrderRemoved is order.removed.
type OrderRemoved struct {
	ID string `json:"id"`
}

// OrderFired is order.fired and order.asking.
type OrderFired struct {
	Order Order    `json:"order"`
	Run   OrderRun `json:"run"`
}

// Orderer is an engine that keeps standing orders. It is asserted, as Crewer
// is.
type Orderer interface {
	Orders(agent string) ([]Order, error)
	OrderRuns(id string, limit int) ([]OrderRun, error)
	RatifyOrder(id string, edits *OrderEdits) (Order, error)
	ChangeOrder(id string, edits OrderEdits) (Order, error)
	NotNowOrder(id string) error
	SetOrderState(id, state string) (Order, error)
	RunOrderNow(id string) (Order, error)
	OrderVerdict(id string, run int64, verdict string) (Order, error)
}

func init() {
	for name, m := range orderMethods {
		methods[name] = m
	}
}

// orderCall decodes a method's params into P and answers what call answers.
func orderCall[P any](name string, call func(Orderer, P) (any, error)) method {
	return func(e Engine, raw json.RawMessage) (any, error) {
		o, ok := e.(Orderer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p P
		if len(raw) > 0 && string(raw) != "null" {
			if err := decode(raw, &p); err != nil {
				return nil, err
			}
		}
		return call(o, p)
	}
}

type orderAt struct {
	ID    string      `json:"id"`
	Edits *OrderEdits `json:"edits,omitempty"`
}

func orderResult(o Order, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return struct {
		Order Order `json:"order"`
	}{o}, nil
}

// stateCall is pause, resume and stop, which differ only in the state asked for.
func stateCall(name, state string) method {
	return orderCall(name, func(o Orderer, p orderAt) (any, error) {
		if err := needID(name, p.ID); err != nil {
			return nil, err
		}
		return orderResult(o.SetOrderState(p.ID, state))
	})
}

var orderMethods = map[string]method{
	"orders.list": orderCall("orders.list", func(o Orderer, p struct {
		Agent string `json:"agent"`
	}) (any, error) {
		orders, err := o.Orders(strings.TrimSpace(p.Agent))
		if err != nil {
			return nil, err
		}
		return struct {
			Orders []Order `json:"orders"`
		}{nonNil(orders)}, nil
	}),
	"orders.runs": orderCall("orders.runs", func(o Orderer, p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}) (any, error) {
		if err := needID("orders.runs", p.ID); err != nil {
			return nil, err
		}
		if p.Limit <= 0 || p.Limit > 100 {
			p.Limit = 20
		}
		runs, err := o.OrderRuns(p.ID, p.Limit)
		if err != nil {
			return nil, err
		}
		return struct {
			Runs []OrderRun `json:"runs"`
		}{nonNil(runs)}, nil
	}),
	"orders.ratify": orderCall("orders.ratify", func(o Orderer, p orderAt) (any, error) {
		if err := needID("orders.ratify", p.ID); err != nil {
			return nil, err
		}
		return orderResult(o.RatifyOrder(p.ID, p.Edits))
	}),
	"orders.change": orderCall("orders.change", func(o Orderer, p orderAt) (any, error) {
		if err := needID("orders.change", p.ID); err != nil {
			return nil, err
		}
		if p.Edits == nil {
			return nil, Errorf(CodeBadParams, "orders.change needs edits")
		}
		return orderResult(o.ChangeOrder(p.ID, *p.Edits))
	}),
	"orders.not_now": orderCall("orders.not_now", func(o Orderer, p orderAt) (any, error) {
		if err := needID("orders.not_now", p.ID); err != nil {
			return nil, err
		}
		return struct{}{}, o.NotNowOrder(p.ID)
	}),
	"orders.pause":  stateCall("orders.pause", OrderPaused),
	"orders.resume": stateCall("orders.resume", OrderActive),
	"orders.stop":   stateCall("orders.stop", OrderStopped),
	"orders.run": orderCall("orders.run", func(o Orderer, p struct {
		ID      string `json:"id"`
		Run     int64  `json:"run"`
		Verdict string `json:"verdict"`
	}) (any, error) {
		if err := needID("orders.run", p.ID); err != nil {
			return nil, err
		}
		switch p.Verdict {
		case "":
			return orderResult(o.RunOrderNow(p.ID))
		case VerdictGo, VerdictSkip, VerdictNotRight:
			return orderResult(o.OrderVerdict(p.ID, p.Run, p.Verdict))
		}
		return nil, Errorf(CodeBadParams, "verdict is go, skip or not_right")
	}),
}
