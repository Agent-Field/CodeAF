package appwire

import (
	"strings"
	"time"
)

// Workstreams v5: a standing order made from a workstream's
// People and rules sheet, and the preview it shows before it is switched on.
//
// AN ORDER MADE HERE IS SAVED AS PROPOSED, so the store's own law holds: NOTHING
// RUNS BEFORE RATIFY (internal/automation's trust.go). The sheet's switch is
// orders.ratify the first time and orders.resume and orders.pause after; its
// Cancel is orders.not_now, which takes a proposed order away.
//
// The preview is the schedule's own arithmetic, never the app's: orders.preview
// reads a when exactly as orders.change would and answers the next runs, so the
// times a person is shown before switching it on are the times the clock keeps.

// FeatureWorkstreamOrders is the hello word that says this engine makes orders
// from a workstream (orders.create) and previews a when (orders.preview).
const FeatureWorkstreamOrders = "workstream_orders"

// OrderNextRunsMost is how many of an order's next runs a preview names.
const OrderNextRunsMost = 3

// OrderCreate is what orders.create takes.
type OrderCreate struct {
	// Title is the short name; empty takes the brief's first line.
	Title string `json:"title,omitempty"`
	// When is "every 0 9 * * 1", "every 1d", "at tomorrow 09:00" or "in 2h",
	// the dialect orders.change reads.
	When string `json:"when"`
	// Does is the brief each run carries out.
	Does string `json:"does"`
	// Workstream scopes it: the workstream whose tasks each run lands in.
	Workstream      string   `json:"workstream"`
	BudgetPerRunUSD *float64 `json:"budget_per_run_usd,omitempty"`
}

// OrderPreview is what orders.preview answers: the when in words and exactly,
// and when it would next run.
type OrderPreview struct {
	When      string      `json:"when"`
	WhenExact string      `json:"when_exact"`
	NextRuns  []time.Time `json:"next_runs"`
}

// OrderMaker is an engine that makes orders from a workstream. It is asserted
// apart from [Orderer], so an engine that only keeps orders still serves them.
type OrderMaker interface {
	CreateOrder(in OrderCreate) (Order, error)
	PreviewOrder(when string) (OrderPreview, error)
}

func withWorkstreamOrdersFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(OrderMaker); !ok {
		return hello
	}
	return withFeature(hello, FeatureWorkstreamOrders)
}

func orderMakerCall[P, R any](name string, call func(OrderMaker, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		o, ok := e.(OrderMaker)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		return call(o, p)
	})
}

// orderTextMost bounds a brief, as a task's request is bounded.
const orderTextMost = 4000

func init() {
	methods["orders.create"] = orderMakerCall("orders.create", func(o OrderMaker, p OrderCreate) (*OrderResult, error) {
		p.Title, p.When, p.Does, p.Workstream = strings.TrimSpace(p.Title), strings.TrimSpace(p.When), strings.TrimSpace(p.Does), strings.TrimSpace(p.Workstream)
		switch {
		case p.Does == "":
			return nil, Errorf(CodeBadParams, "a standing order needs something to do")
		case len(p.Does) > orderTextMost:
			return nil, Errorf(CodeBadParams, "what a standing order does is at most %d characters", orderTextMost)
		case p.When == "":
			return nil, Errorf(CodeBadParams, "a standing order needs a when")
		case p.Workstream == "" || len(p.Workstream) > 200 || strings.ContainsAny(p.Workstream, "\r\n\t"):
			return nil, Errorf(CodeBadParams, "workstream is a workstream's id")
		case p.BudgetPerRunUSD != nil && *p.BudgetPerRunUSD <= 0:
			return nil, Errorf(CodeBadParams, "a budget per run is above zero")
		}
		return orderResult(o.CreateOrder(p))
	})
	methods["orders.preview"] = orderMakerCall("orders.preview", func(o OrderMaker, p struct {
		When string `json:"when"`
	}) (*OrderPreview, error) {
		when := strings.TrimSpace(p.When)
		if when == "" {
			return nil, Errorf(CodeBadParams, "orders.preview needs a when")
		}
		out, err := o.PreviewOrder(when)
		if err != nil {
			return nil, err
		}
		if out.NextRuns == nil {
			out.NextRuns = []time.Time{}
		}
		return &out, nil
	})
}
