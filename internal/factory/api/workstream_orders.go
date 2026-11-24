package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── AN ORGANIZATION WORKSTREAM'S STANDING ORDERS ON THE WIRE ───────────────
//
//	GET    /v1/workstreams/{id}/orders                       -> WorkstreamOrders
//	POST   /v1/workstreams/{id}/orders                       WorkstreamOrderNew -> WorkstreamOrder (201)
//	PATCH  /v1/workstreams/{id}/orders/{oid}  If-Match: rev  WorkstreamOrderPatch -> WorkstreamOrder, 412 stale
//	DELETE /v1/workstreams/{id}/orders/{oid}                 -> 204
//
// Reading needs view on the workstream; every write is the person who looks
// after it. Making one, or switching one on, is refused (409) on a server that
// cannot run Afi, and the list's runs is false there. Every change publishes
// workstream.changed with part "orders"; a run publishes "tasks" too.

// PartOrders is the part a standing order's change names.
const PartOrders = "orders"

// The order's own shapes.
type (
	WorkstreamOrderNew   = factory.WorkstreamOrderNew
	WorkstreamOrderPatch = factory.WorkstreamOrderPatch
)

// WorkstreamOrder is one order with its schedule in words ("every Monday at
// 09:00").
type WorkstreamOrder struct {
	factory.WorkstreamOrder
	When string `json:"when"`
}

// WorkstreamOrders is a workstream's orders, oldest first, and whether this
// server runs them.
type WorkstreamOrders struct {
	Runs   bool              `json:"runs"`
	Orders []WorkstreamOrder `json:"orders"`
}
