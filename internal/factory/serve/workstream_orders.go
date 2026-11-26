package serve

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// ── AN ORGANIZATION WORKSTREAM'S STANDING ORDERS, KEPT AND RUN HERE ────────
//
// factory/workstream_order.go says what an order is. This file is its routes
// and its clock:
//
//	GET    /v1/workstreams/{id}/orders          everyone who sees the workstream
//	POST   /v1/workstreams/{id}/orders          the person who looks after it
//	PATCH  /v1/workstreams/{id}/orders/{oid}    the same: {"on": bool}, If-Match: rev
//	DELETE /v1/workstreams/{id}/orders/{oid}    the same
//
// THE LIST SAYS WHETHER THIS SERVER RUNS THEM (`runs`). Orders run as the
// workstream's agent here, so a server that cannot run Afi (no runner, no
// model key) refuses to make one and the desk offers none: a capability that
// cannot work is absent.
//
// WHEN ONE COMES DUE, the server's own timer makes its run ONE TASK, made as
// the person who added the order and owned by the owner of incoming work:
//
//   - When "Start without asking" covers what one run may spend, the task
//     starts at once in its own seat (runTaskTurn), with the workstream's
//     tools and nothing else.
//   - Otherwise it waits on its owner with "Run … now?" (Go, Skip this one,
//     Not right). Go starts it in that same write, as whoever said go; Skip
//     this one and Not right leave it Stopped with why.
//
// A running order's task stops by the task's own Stop, as any Afi task does.
// A slot missed while the server was down is run ONCE, late.

// orderTimerFloor is the soonest the timer looks again at a slot already
// past, so a run it could not make is not retried a thousand times a second.
const orderTimerFloor = 2 * time.Second

var (
	errOrdersNotRun = errors.New("this organization's server cannot run Afi, so it keeps no standing orders: the operator adds a model key first")
	errOrdersFull   = workstreamInvalid("a workstream keeps at most " + strconv.Itoa(factory.WorkstreamOrdersMost) + " standing orders")
)

// The words a run's Ask and its ends say. ONE SOURCE: the desk's wsOrders.ts
// spells the same Ask for a Personal order's run.
const (
	orderAskGo       = "go"
	orderAskSkip     = "skip"
	orderAskNotRight = "not_right"
	orderSkipped     = "Skipped this run"
	orderNotRight    = "Not right: this run did not go ahead"
)

func (h *Handler) workstreamOrderRoutes() {
	h.route("/v1/workstreams/{id}/orders", map[string]http.HandlerFunc{http.MethodGet: h.listWorkstreamOrders, http.MethodPost: h.createWorkstreamOrder})
	h.route("/v1/workstreams/{id}/orders/{oid}", map[string]http.HandlerFunc{http.MethodPatch: h.patchWorkstreamOrder, http.MethodDelete: h.deleteWorkstreamOrder})
}

// orderView is one order as a client reads it: the order, and its schedule
// in words.
func orderView(o factory.WorkstreamOrder, now time.Time) api.WorkstreamOrder {
	return api.WorkstreamOrder{WorkstreamOrder: o, When: o.When(now)}
}

func (h *Handler) listWorkstreamOrders(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	all, err := h.config.Store.WorkstreamOrders(ws.ID)
	if h.failed(w, err) {
		return
	}
	now := h.clock()
	out := api.WorkstreamOrders{Runs: h.serverAgentServed(), Orders: make([]api.WorkstreamOrder, 0, len(all))}
	for _, o := range all {
		out.Orders = append(out.Orders, orderView(o, now))
	}
	h.write(w, http.StatusOK, out)
}

// stewardOf is ws when v looks after it and may still change it; otherwise
// the refusal is written and ok is false.
func (h *Handler) orderSteward(w http.ResponseWriter, r *http.Request) (factory.OrgWorkstream, Viewer, bool) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return ws, v, false
	}
	if !v.canWrite(ws) || !v.stewards(ws) {
		h.fail(w, http.StatusForbidden, "only the person who looks after this workstream adds, switches or removes its standing orders")
		return ws, v, false
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, "this workstream is archived")
		return ws, v, false
	}
	return ws, v, true
}

func (h *Handler) createWorkstreamOrder(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.orderSteward(w, r)
	if !ok {
		return
	}
	if !h.serverAgentServed() {
		h.fail(w, http.StatusConflict, errOrdersNotRun.Error())
		return
	}
	var body factory.WorkstreamOrderNew
	if !h.decode(w, r, &body, false) {
		return
	}
	now := h.clock()
	made, err := factory.NewWorkstreamOrder(ws.ID, h.newTaskID("or_")(), body, factory.PersonRef{ID: v.Handle, Name: h.personNames()(v.Handle)}, now)
	if h.taskRefused(w, err) {
		return
	}
	// Who wrote what each run will ask (factory.WorkstreamOrder.WordsBy).
	made.WordsBy = factory.WordsAuthor(wordsKind(r), body.WordsBy)
	err = h.config.Store.EditWorkstreamOrders(func(ow store.OrderWrite) error {
		all, err := ow.Orders(ws.ID)
		if err != nil {
			return err
		}
		if len(all) >= factory.WorkstreamOrdersMost {
			return errOrdersFull
		}
		return ow.Put(made, 0)
	})
	if h.orderRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "added standing order", made.ID)
	h.publishWorkstreamPart(ws, api.PartOrders, made.ID, h.orgActor(r), made.Rev, false)
	h.scheduleOrderTimer()
	h.write(w, http.StatusCreated, orderView(made, now))
}

func (h *Handler) patchWorkstreamOrder(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.orderSteward(w, r)
	if !ok {
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	var body factory.WorkstreamOrderPatch
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.On == nil {
		h.fail(w, http.StatusUnprocessableEntity, `say what changes: {"on": true} or {"on": false}`)
		return
	}
	if *body.On && !h.serverAgentServed() {
		h.fail(w, http.StatusConflict, errOrdersNotRun.Error())
		return
	}
	now := h.clock()
	var out factory.WorkstreamOrder
	err := h.config.Store.EditWorkstreamOrders(func(ow store.OrderWrite) error {
		cur, err := ow.Order(ws.ID, r.PathValue("oid"))
		if err != nil {
			return err
		}
		if cur.Rev != rev {
			return store.ErrWorkstreamOrderChanged
		}
		out = cur.Switch(*body.On, now)
		return ow.Put(out, cur.Rev)
	})
	if h.orderRefused(w, err) {
		return
	}
	verb := "switched off standing order"
	if out.On {
		verb = "switched on standing order"
	}
	h.recordWorkstream(r, ws.ID, verb, out.ID)
	h.publishWorkstreamPart(ws, api.PartOrders, out.ID, h.orgActor(r), out.Rev, false)
	h.scheduleOrderTimer()
	h.write(w, http.StatusOK, orderView(out, now))
}

func (h *Handler) deleteWorkstreamOrder(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.orderSteward(w, r)
	if !ok {
		return
	}
	id := r.PathValue("oid")
	err := h.config.Store.EditWorkstreamOrders(func(ow store.OrderWrite) error { return ow.Delete(ws.ID, id) })
	if h.orderRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "removed standing order", id)
	h.publishWorkstreamPart(ws, api.PartOrders, id, h.orgActor(r), 0, true)
	h.scheduleOrderTimer()
	w.WriteHeader(http.StatusNoContent)
}

// orderRefused writes err as a person reads it; false when err is nil.
func (h *Handler) orderRefused(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrWorkstreamOrderGone):
		h.fail(w, http.StatusNotFound, "that standing order is not in this workstream")
	case errors.Is(err, store.ErrWorkstreamOrderChanged):
		h.fail(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, errOrdersFull):
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.failed(w, err)
	}
	return true
}

// ── the clock ──

// scheduleOrderTimer sets the timer for the soonest order due, or stops it
// when none is. Every change to an order and every run calls it.
func (h *Handler) scheduleOrderTimer() {
	if h.config.Store == nil {
		return
	}
	next, err := h.config.Store.NextWorkstreamOrder()
	if err != nil {
		return
	}
	h.ordersMu.Lock()
	defer h.ordersMu.Unlock()
	if h.ordersClosed {
		return
	}
	if h.ordersTimer != nil {
		h.ordersTimer.Stop()
		h.ordersTimer = nil
	}
	if next.IsZero() {
		return
	}
	wait := next.Sub(h.clock())
	if wait < orderTimerFloor {
		wait = orderTimerFloor
	}
	h.ordersTimer = time.AfterFunc(wait, func() {
		func() {
			defer guard.Recover("factory standing orders")
			h.runDueOrders(h.clock())
		}()
		h.scheduleOrderTimer()
	})
}

// closeOrders stops the timer for good.
func (h *Handler) closeOrders() {
	h.ordersMu.Lock()
	defer h.ordersMu.Unlock()
	h.ordersClosed = true
	if h.ordersTimer != nil {
		h.ordersTimer.Stop()
	}
	if h.chaseTimer != nil {
		h.chaseTimer.Stop()
	}
}

// runDueOrders makes one run of every order due at now. On a server that
// cannot run Afi it makes none, and the orders wait: the first look after a
// key arrives runs each one once, late.
func (h *Handler) runDueOrders(now time.Time) {
	if h.config.Store == nil || !h.serverAgentServed() {
		return
	}
	due, err := h.config.Store.DueWorkstreamOrders(now)
	if err != nil {
		return
	}
	for _, o := range due {
		h.runOrder(o, now)
	}
}

// runOrder counts one run of o and makes it a task: waiting on its owner's
// Go, or started at once when "Start without asking" covers it. The count is
// kept first, so a run that cannot be made is passed over, never retried.
func (h *Handler) runOrder(o factory.WorkstreamOrder, now time.Time) {
	var took factory.WorkstreamOrder
	if err := h.config.Store.EditWorkstreamOrders(func(ow store.OrderWrite) error {
		cur, err := ow.Order(o.WS, o.ID)
		if err != nil {
			return err
		}
		if !cur.On || cur.NextAt().After(now) {
			return store.ErrWorkstreamOrderChanged
		}
		took = cur.Took(now)
		return ow.Put(took, cur.Rev)
	}); err != nil {
		return
	}
	ws, found, err := h.config.Store.Workstream(o.WS)
	if err != nil || !found || ws.ArchivedAt != nil {
		return
	}
	starter := factory.NormalizeHandle(took.CreatedBy.ID)
	alone := took.StartsAlone(ws.RulesOrDefault())
	var made factory.WorkstreamTask
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		kept, err := tw.Tasks(ws.ID)
		if err != nil {
			return err
		}
		if store.CountWorkstreamTasks(kept) >= factory.TasksMost {
			return factory.TaskInvalid("the workstream keeps as many tasks as it may")
		}
		made, err = factory.NewTask(ws.ID, h.newTaskID("tk_")(), orderTaskOf(took), took.CreatedBy, taskDefaultOwner(ws), kept, now)
		if err != nil {
			return err
		}
		// The run asks the order's words, written by whoever wrote the order.
		factory.StampWords(&made, took.WordsBy)
		if alone {
			made.State = factory.TaskRunning
		} else {
			made.State, made.Ask = factory.TaskNeeds, orderRunAsk(took, now)
		}
		if err := tw.Put(made, 0); err != nil {
			return err
		}
		if alone {
			// Last, with the write still open, as a person's Start does: a
			// run Afi cannot take keeps no task that says running.
			return h.runTaskTurn(ws, made, starter, taskWords(made))
		}
		return nil
	})
	if err != nil {
		if alone {
			h.stopTaskTurn(ws.ID, made.ID)
		}
		return
	}
	h.recordAs(context.Background(), starter, record.WorkstreamChanged, "standing order ran", orgObjectWorkstream+ws.ID, took.Title)
	h.publishTasks(ws, starter, made)
	h.publishWorkstreamPart(ws, api.PartOrders, took.ID, starter, took.Rev, false)
}

// orderTaskOf is the task one run of o adds: Came from the order, done by
// Afi, in the order's group, its run's number naming it.
func orderTaskOf(o factory.WorkstreamOrder) factory.TaskCreate {
	return factory.TaskCreate{Title: o.Title, Request: o.Does, DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi},
		CameFrom: &factory.TaskCameFrom{Kind: factory.TaskByOrder, OrderID: o.ID, Name: o.Title, Run: o.Runs},
		Group:    &factory.TaskGroup{ID: factory.OrderGroupPrefix + o.ID, Name: o.Title, Kind: factory.TaskByOrder}}
}

// orderRunAsk is a run's "Run … now?".
func orderRunAsk(o factory.WorkstreamOrder, now time.Time) *factory.TaskAsk {
	return &factory.TaskAsk{ID: factory.OrderAskPrefix + strconv.FormatInt(o.Runs, 10), Title: "Run " + o.Title + " now?",
		Detail: o.Does, AskedAt: now.UTC().Format(time.RFC3339Nano),
		Options: []factory.TaskAskOption{{ID: orderAskGo, Label: "Go", Recommended: true}, {ID: orderAskSkip, Label: "Skip this one"}, {ID: orderAskNotRight, Label: "Not right"}}}
}

// orderAnswer reads an answer to a run's own Ask on a task an order made
// HERE: Go wakes Afi (wake), Skip this one and Not right stop the task with
// why. Any other answer, or an order's task a desk made, is left as it is.
func orderAnswer(t *factory.WorkstreamTask, askID, option string) (wake bool) {
	if t.CameFrom.Kind != factory.TaskByOrder || t.DoneBy.Kind != factory.TaskByAfi || !strings.HasPrefix(askID, factory.OrderAskPrefix) ||
		strings.TrimPrefix(askID, factory.OrderAskPrefix) != strconv.FormatInt(t.CameFrom.Run, 10) || !strings.HasPrefix(t.CameFrom.OrderID, "or_") {
		return false
	}
	switch option {
	case orderAskGo:
		return t.State == factory.TaskRunning
	case orderAskSkip:
		t.State, t.StateReason = factory.TaskStopped, orderSkipped
	case orderAskNotRight:
		t.State, t.StateReason = factory.TaskStopped, orderNotRight
	}
	return false
}
