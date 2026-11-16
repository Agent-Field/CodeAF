package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// A STANDING ORDER IN AN ORGANIZATION'S WORKSTREAM, END TO END, WITH TWO
// PASSWORD PEOPLE ON A REAL SERVER. santosh looks after "Northlight finance"
// and abir owns its incoming work. santosh's engine makes the order from the
// workstream (proposed), his switch agrees to it, and its clock parks a run
// that asks first. His desk makes that run a task on the server — every call
// below is one the desk's run watcher makes, through his engine — and abir,
// on his own engine, sees it as his, with where it came from, and answers Go.
// santosh reads which answer was chosen and carries it to his clock.
func TestAWorkstreamOrdersRunIsAnOrganizationTaskItsOwnerAnswers(t *testing.T) {
	o := startPasswordOrg(t)
	santosh := newFactoryWire(t, testFactoryHub(t))
	store, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	santosh.backend.orderStore = func() *automation.Store { return store }
	clip := &fakeCopier{avail: true}
	santosh.backend.useClipboard(clip)
	var access appwire.FactoryAccess
	santosh.must("factory.access_sign_up", map[string]any{"link": o.operatorLink(t), "caller": "w1", "username": "santosh", "password": "violet otter campsite drizzle", "name": "Santosh"}, &access)
	org := access.Factory.ID
	var made appwire.FactoryLinkMade
	santosh.must("factory.link_create", map[string]any{"factory": org}, &made)
	abir := newFactoryWire(t, testFactoryHub(t))
	abir.must("factory.access_sign_up", map[string]any{"link": clip.calls[0], "caller": "w9", "username": "abir", "password": "a long passphrase for my friend"}, &access)
	abirOrg := access.Factory.ID

	// The workstream, abir in it, and abir the owner of incoming work.
	var ws struct {
		ID    string          `json:"id"`
		Rev   int             `json:"rev"`
		Rules json.RawMessage `json:"rules"`
	}
	santosh.must("factory.org_workstream_create", map[string]any{"connection": org, "name": "Northlight finance"}, &ws)
	var shared struct {
		Workstream struct {
			Rev int `json:"rev"`
		} `json:"workstream"`
	}
	santosh.must("factory.org_workstream_shares", map[string]any{"connection": org, "id": ws.ID, "rev": ws.Rev,
		"shares": []map[string]any{{"kind": "person", "id": "abir", "role": "edit"}}}, &shared)

	// Add a standing order…: made from the workstream, proposed, with the runs it would make.
	scope := "org:" + org + "/" + ws.ID
	var res appwire.OrderResult
	santosh.must("orders.create", map[string]any{"when": "every 0 9 1 * *", "does": "Close the month's books: the eight checklist tasks.", "workstream": scope}, &res)
	order := res.Order
	if order.State != appwire.OrderProposed || order.Workstream != scope || len(order.NextRuns) != 3 || order.NextRuns[0].Day() != 1 {
		t.Fatalf("the order = %+v", order)
	}
	// Its switch: agreed to, asking first, and named in the workstream's keeps running.
	santosh.must("orders.ratify", map[string]any{"id": order.ID, "edits": map[string]any{"trust": "asks_first"}}, &res)
	if res.Order.State != appwire.OrderActive {
		t.Fatalf("switched on = %+v", res.Order)
	}
	rules := map[string]any{"default_owner": map[string]any{"kind": "person", "id": "abir", "name": "abir"},
		"start_without_asking_usd": 0, "keeps_running": map[string]any{"factory": false, "orders": []string{order.ID}}}
	santosh.must("factory.org_workstream_update", map[string]any{"connection": org, "id": ws.ID, "rev": shared.Workstream.Rev, "rules": rules}, nil)

	// The 1st comes: the clock parks a run that asks first.
	run, err := store.QueueAsking(order.ID)
	if err != nil {
		t.Fatal(err)
	}
	var orders appwire.OrdersResult
	santosh.must("orders.list", map[string]any{}, &orders)
	if len(orders.Orders) != 1 || orders.Orders[0].Asking == nil || orders.Orders[0].Asking.ID != run.ID {
		t.Fatalf("the waiting run = %+v", orders.Orders)
	}

	// santosh's desk makes the run a task, and asks its owner.
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": org, "ws": ws.ID}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	came := map[string]any{"kind": "order", "order_id": order.ID, "name": order.Title, "run": run.ID}
	var task struct {
		ID       string              `json:"id"`
		Rev      int                 `json:"rev"`
		State    string              `json:"state"`
		Owner    struct{ ID string } `json:"owner"`
		CameFrom struct {
			Kind    string `json:"kind"`
			OrderID string `json:"order_id"`
			Run     int64  `json:"run"`
		} `json:"came_from"`
		Group struct{ Kind string } `json:"group"`
		Ask   *struct{ ID string }  `json:"ask"`
	}
	santosh.must("factory.org_workstream_task_create", c(map[string]any{"title": order.Title, "request": order.Does, "done_by": map[string]any{"kind": "afi"},
		"came_from": came, "group": map[string]any{"id": "order:" + order.ID, "name": order.Title, "kind": "order"}}), &task)
	if task.Owner.ID != "abir" || task.CameFrom.Run != run.ID || task.Group.Kind != "order" {
		t.Fatalf("the run's task = %+v", task)
	}
	askID := "order:" + strconv.FormatInt(run.ID, 10)
	santosh.must("factory.org_workstream_task_update", c(map[string]any{"id": task.ID, "rev": task.Rev, "state": "needs",
		"ask": map[string]any{"id": askID, "title": "Run " + order.Title + " now?", "detail": order.Does, "asked_at": run.Due,
			"options": []map[string]any{{"id": "go", "label": "Go", "recommended": true}, {"id": "skip", "label": "Skip this one"}, {"id": "not_right", "label": "Not right"}}}}), &task)

	// abir, on his own engine, sees it as his and answers Go.
	var list appwire.OrgTasks
	abir.must("factory.org_workstream_tasks", map[string]any{"connection": abirOrg, "ws": ws.ID}, &list)
	if len(list.Tasks) != 1 {
		t.Fatalf("abir sees %d tasks", len(list.Tasks))
	}
	if err := json.Unmarshal(list.Tasks[0], &task); err != nil || task.State != "needs" || task.Owner.ID != "abir" || task.CameFrom.Kind != "order" || task.Ask == nil || task.Ask.ID != askID {
		t.Fatalf("abir's view = %+v, %v", task, err)
	}
	santosh.mustFail("factory.org_workstream_task_answer", c(map[string]any{"id": task.ID, "option": "go"}), appwire.CodeForbidden)
	abir.must("factory.org_workstream_task_answer", map[string]any{"connection": abirOrg, "ws": ws.ID, "id": task.ID, "option": "go"}, nil)

	// santosh's desk reads the answer and carries it to the clock.
	var events appwire.OrgTaskEvents
	santosh.must("factory.org_workstream_task_events", c(map[string]any{"id": task.ID}), &events)
	var last struct{ Kind, Option string }
	if err := json.Unmarshal(events.Events[len(events.Events)-1], &last); err != nil || last.Kind != "answered" || last.Option != "go" {
		t.Fatalf("the answer santosh reads = %+v, %v", last, err)
	}
	santosh.must("orders.run", map[string]any{"id": order.ID, "verdict": last.Option}, &res)
	after, err := store.Run(run.ID)
	if err != nil || after.Phase == automation.PhaseAsking {
		t.Fatalf("the run still waits after abir's Go: %+v, %v", after, err)
	}
}
