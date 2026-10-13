package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

type orderFixture struct {
	*agentFixture
	agent Agent
	store *standing.Store
	now   time.Time
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	base := newAgentFixture(t)
	agent := readAgent(t, base.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	store, err := standing.Open(filepath.Join(base.home, "v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	return &orderFixture{base, agent, store, base.registry.now()}
}
func (f *orderFixture) propose(t *testing.T, reminder bool) standing.Item {
	t.Helper()
	item := standing.Item{Words: "Report CI failures", Workspace: f.home, Origin: standing.Origin{SessionID: f.agent.ID, Transcript: f.opens[0].SessionFile}, When: standing.When{Kind: standing.WhenEvery, Every: "1m", Words: "each minute"}, Does: standing.Action{Kind: standing.ActionSay, Say: "CI failed"}, Rails: standing.Rails{PerRunUSD: standing.DefaultPerRunUSD, MaxPerDay: 100}, App: &standing.AppOrder{Scope: "this_chat"}}
	// The opener returns the transcript separately from the request it records.
	item.Origin.Transcript = f.registry.live[f.agent.ID].conn.SessionFile
	if reminder {
		item.When = standing.When{Kind: standing.WhenAt, At: f.now.Add(time.Hour), Words: "in one hour"}
	}
	made, err := f.store.Propose(item)
	if err != nil {
		t.Fatal(err)
	}
	return made
}
func (f *orderFixture) action(t *testing.T, path, body string, status int) {
	t.Helper()
	w := f.request(t, "POST", path, body)
	if w.Code != status {
		t.Fatalf("%s: want %d, got %d: %s", path, status, w.Code, w.Body)
	}
	if status == 200 {
		var out ActionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !out.OK || out.Receipt == nil || out.Receipt.Text == "" {
			t.Fatalf("missing receipt: %s %v", w.Body, err)
		}
	}
}
func (f *orderFixture) orders(t *testing.T, q string) OrdersResponse {
	t.Helper()
	w := f.request(t, "GET", "/app/v1/agents/"+f.agent.ID+"/orders"+q, "")
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var out OrdersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestOrderRoutesLifecycleChangeScopePauseAllAndHistory(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, false)
	prefix := "/app/v1/orders/" + item.ID
	list := f.orders(t, "")
	if len(list.Waiting) != 1 || list.Waiting[0].State != "proposed" || list.Waiting[0].BudgetPerRun.USD != "0.40" || list.Active == nil || list.Reminders == nil {
		t.Fatalf("proposal: %+v", list)
	}
	f.action(t, prefix+"/change", `{"scope":"this_crew"}`, 422)
	f.action(t, prefix+"/change", `{"scope":"not_here","title":"Nightly CI","does":"Report failures","wont":"Push","budget_per_run":{"usd":"0.75"}}`, 200)
	f.action(t, prefix+"/ratify", `{"expect":{"version":1}}`, 409)
	f.action(t, prefix+"/ratify", `{}`, 200)
	active := f.orders(t, "").Active
	if len(active) != 1 || active[0].Scope != "not_here" || active[0].Wont != "Push" || active[0].Does != "Report failures" || active[0].BudgetPerRun.USD != "0.75" {
		t.Fatalf("changed card: %+v", active)
	}
	for _, scope := range []string{"this_chat", "everywhere"} {
		f.action(t, prefix+"/change", fmt.Sprintf(`{"scope":%q}`, scope), 200)
	}
	f.registry.mu.Lock()
	err := f.registry.disk(func(d *agentDisk) error {
		record := d.Records[f.agent.ID]
		record.Agent.CrewID = "crew-fixture"
		d.Records[f.agent.ID] = record
		return nil
	}, true)
	f.registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	f.action(t, prefix+"/change", `{"scope":"this_crew"}`, 200)
	if len(f.orders(t, "?q=scope:this_crew+state:active").Active) != 1 || len(f.orders(t, "?q=scope:not_here").Active) != 0 {
		t.Fatal("scope filter")
	}
	f.action(t, prefix+"/pause", `{}`, 200)
	f.action(t, prefix+"/pause", `{}`, 409)
	if len(f.orders(t, "").Paused) != 1 {
		t.Fatal("pause missing")
	}
	f.action(t, prefix+"/resume", `{}`, 200)
	rem := f.propose(t, true)
	f.action(t, "/app/v1/orders/"+rem.ID+"/ratify", `{}`, 200)
	f.action(t, "/app/v1/agents/"+f.agent.ID+"/orders/pause-all", `{}`, 200)
	list = f.orders(t, "")
	if len(list.Paused) != 1 || len(list.Reminders) != 1 || list.Reminders[0].State != "active" {
		t.Fatalf("pause all: %+v", list)
	}
	f.action(t, prefix+"/stop", `{}`, 200)
	f.action(t, prefix+"/resume", `{}`, 409)
	if len(f.orders(t, "").Paused) != 0 {
		t.Fatal("stopped order stayed listed")
	}
	stopped, err := f.store.Get(item.ID)
	if err != nil || stopped.Status != standing.StatusRetired {
		t.Fatal("stopped history missing")
	}
	declined := f.propose(t, false)
	f.action(t, "/app/v1/orders/"+declined.ID+"/not-now", `{}`, 200)
	if len(f.orders(t, "").Waiting) != 0 {
		t.Fatal("declined proposal stayed listed")
	}
}
func TestReminderRoutesPastTimeChangeCancelAndValidation(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, true)
	prefix := "/app/v1/reminders/" + item.ID
	f.action(t, prefix+"/change", `{"at":"2020-01-01T00:00:00Z"}`, 422)
	w := f.request(t, "POST", prefix+"/change", `{"at":"2020-01-01T00:00:00Z"}`)
	if !strings.Contains(w.Body.String(), "That time has passed. Pick another.") {
		t.Fatal(w.Body)
	}
	f.action(t, prefix+"/change", `{"at":"2026-10-15T10:00:00Z"}`, 200)
	f.action(t, "/app/v1/orders/"+item.ID+"/ratify", `{}`, 200)
	f.action(t, prefix+"/cancel", `{}`, 200)
	if len(f.orders(t, "").Reminders) != 0 {
		t.Fatal("cancelled reminder stayed listed")
	}
	order := f.propose(t, false)
	f.action(t, "/app/v1/reminders/"+order.ID+"/change", `{"at":"2026-10-15T10:00:00Z"}`, 422)
	for _, body := range []string{`null`, `{"extra":true}`, `{} {}`, `{"scope":"invalid"}`, `{"budget_per_run":{"usd":"0.00"}}`, `{"title":""}`, `{"trigger":{"kind":"event","source":"ci"}}`} {
		f.action(t, "/app/v1/orders/"+order.ID+"/change", body, 422)
	}
	f.action(t, "/app/v1/orders/0000000000000000/pause", `{}`, 404)
	w = f.request(t, "GET", "/app/v1/agents/missing/orders", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestOrderRunRoutesFiveApprovalsSkipPauseNotRightAndStableAsks(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, false)
	prefix := "/app/v1/orders/" + item.ID
	f.action(t, prefix+"/ratify", `{}`, 200)
	runner := &orderSayRunner{}
	ticker := &standing.Ticker{Store: f.store, Runner: runner, Now: func() time.Time { return f.now }}
	tick := func() {
		t.Helper()
		f.now = f.now.Add(time.Minute)
		if _, err := ticker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	next := func() string {
		t.Helper()
		tick()
		current, err := f.store.Get(item.ID)
		if err != nil || current.AppWaitingRun() == nil {
			t.Fatalf("run: %+v %v", current, err)
		}
		return current.AppWaitingRun().ID
	}
	first := next()
	asks, err := f.registry.OrderAsks()
	if err != nil || len(asks) != 1 || asks[0].ID != orderAskID(item.ID, first) || len(asks[0].Before) != 2 {
		t.Fatalf("asks: %+v %v", asks, err)
	}
	f.action(t, prefix+"/runs/"+first, `{"verb":"invalid"}`, 422)
	f.action(t, prefix+"/runs/"+first, `{"verb":"skip"}`, 200)
	for i := 1; i <= 5; i++ {
		id := next()
		verb := "go"
		if i == 5 {
			verb = "run_now"
		}
		f.action(t, prefix+"/runs/"+id, fmt.Sprintf(`{"verb":%q}`, verb), 200)
		if _, err := ticker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.action(t, prefix+"/runs/"+id, `{"verb":"go"}`, 409)
	}
	got := f.orders(t, "").Active[0]
	if got.Trust != "trusted" || got.ApprovedRuns != 5 || runner.said != 5 {
		t.Fatalf("trust: %+v said=%d", got, runner.said)
	}
	f.action(t, prefix+"/runs/"+first, `{"verb":"not_right"}`, 200)
	pending := next()
	f.action(t, prefix+"/runs/"+pending, `{"verb":"pause"}`, 200)
	if f.orders(t, "").Paused[0].ApprovedRuns != 0 {
		t.Fatal("Not right did not reset")
	}
	f.action(t, prefix+"/resume", `{}`, 200)
	pending = next()
	f.action(t, prefix+"/change", `{"does":"Use the new brief"}`, 200)
	f.action(t, prefix+"/runs/"+pending, `{"verb":"go"}`, 409)
	f.action(t, prefix+"/runs/missing", `{"verb":"go"}`, 404)
	if err := f.server.PublishOrders(); err != nil {
		t.Fatal(err)
	}
}

type orderSayRunner struct{ said int }

func (r *orderSayRunner) Probe(context.Context, standing.Item) (standing.ProbeReading, error) {
	return standing.ProbeReading{}, nil
}
func (r *orderSayRunner) Say(context.Context, standing.Item, string) (standing.Outcome, error) {
	r.said++
	return standing.Outcome{Kind: "said"}, nil
}
func (r *orderSayRunner) Run(context.Context, standing.Item, string, string) (standing.Outcome, error) {
	return standing.Outcome{Kind: "landed"}, nil
}

func TestOrderNotificationsScopeRestartAndIdempotency(t *testing.T) {
	f := newOrderFixture(t)
	other := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	item := f.propose(t, false)
	item, err := f.store.ChangeApp(item.ID, func(it *standing.Item) error {
		it.App.Scope = "everywhere"
		it.App.Runs = []standing.AppRun{{ID: "run_1", At: f.now, State: "done", Report: "cart.test.ts failed"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.server.PublishOrders(); err != nil {
		t.Fatal(err)
	}
	count := func(id string) int {
		t.Helper()
		f.registry.mu.Lock()
		defer f.registry.mu.Unlock()
		n := 0
		err := f.registry.disk(func(d *agentDisk) error {
			for _, saved := range d.Records[id].Receipts {
				if saved.Entry.Link != nil && saved.Entry.Link.ID == item.ID {
					n++
				}
			}
			return nil
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(f.agent.ID) != 1 || count(other.ID) != 1 {
		t.Fatal("Everywhere missed a chat")
	}
	if err = f.server.PublishOrders(); err != nil {
		t.Fatal(err)
	}
	// Restarting the publication cursor replays events but reuses durable entries.
	f.server.orders.Lock()
	f.server.orders.seen = nil
	f.server.orders.Unlock()
	if err = f.server.PublishOrders(); err != nil {
		t.Fatal(err)
	}
	if count(f.agent.ID) != 1 || count(other.ID) != 1 {
		t.Fatal("restart duplicated the receipt")
	}
	records, _, _, _, _ := f.server.events.read(0)
	changed, fired := false, false
	for _, event := range records {
		if event.kind == EventOrderChanged {
			changed = true
		}
		if event.kind == EventOrderFired {
			fired = true
		}
	}
	if !changed || !fired {
		t.Fatal("order events missing")
	}
	headers := bearer()
	headers.Set("Idempotency-Key", "ratify-order")
	path := "/app/v1/orders/" + item.ID + "/ratify"
	first := request(t, f.server, "POST", path, `{}`, headers, "127.0.0.1:4000")
	second := request(t, f.server, "POST", path, `{}`, headers, "127.0.0.1:4000")
	if first.Code != 200 || second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatal("ratification replay changed")
	}
	mismatch := request(t, f.server, "POST", path, `{"expect":{"version":1}}`, headers, "127.0.0.1:4000")
	if mismatch.Code != 409 {
		t.Fatal("changed idempotency bytes accepted")
	}
	for _, route := range orderContractRoutes {
		path := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(route.Path, "{id}", f.agent.ID), "{oid}", item.ID), "{rid}", "run_1")
		w := request(t, f.server, route.Method, path, `{}`, nil, "127.0.0.1:4000")
		if w.Code != 401 {
			t.Fatalf("unguarded %s: %d", route.Path, w.Code)
		}
	}
}

func TestOrderBudgetAskBlocksLaterRunUntilRaise(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, false)
	prefix := "/app/v1/orders/" + item.ID
	f.action(t, prefix+"/ratify", `{}`, 200)
	_, err := f.store.ChangeApp(item.ID, func(it *standing.Item) error {
		it.NeedsPerson = "Budget per run reached · $0.40 · raise the budget before running again"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	asks, err := f.registry.OrderAsks()
	if err != nil || len(asks) != 1 || asks[0].Kind != "budget" {
		t.Fatalf("missing run-budget ask: %+v %v", asks, err)
	}
	runner := &orderSayRunner{}
	ticker := &standing.Ticker{Store: f.store, Runner: runner, Now: func() time.Time { return f.now.Add(time.Hour) }}
	pass, err := ticker.Tick(context.Background())
	if err != nil || pass.Fired != 0 || pass.NeedsYou != 1 {
		t.Fatalf("budget ran again: %+v %v", pass, err)
	}
	f.action(t, prefix+"/change", `{"budget_per_run":{"usd":"0.80"}}`, 200)
	got, err := f.store.Get(item.ID)
	if err != nil || got.NeedsPerson != "" || got.Rails.PerRunUSD != 0.8 {
		t.Fatalf("raise: %+v %v", got, err)
	}
	pass, err = ticker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err = f.store.Get(item.ID)
	if err != nil || got.AppWaitingRun() == nil || pass.Fired != 0 {
		t.Fatal("raised order did not ask for a fresh trigger")
	}
}

func TestOrderInboxUsesProducerAndHeldSourceDecision(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, false)
	clock := f.now
	var timer func()
	f.server.inbox.Lock()
	f.server.inbox.now = func() time.Time { return clock }
	f.server.inbox.after = func(_ time.Duration, run func()) *time.Timer { timer = run; return nil }
	f.server.inbox.Unlock()
	rows, err := f.server.RefreshInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := "ask:" + orderAskID(item.ID, "proposal")
	found := false
	for _, row := range rows {
		if row.ID == id && row.Kind == "order_ask" {
			found = true
		}
	}
	if !found {
		t.Fatalf("proposal missing from Inbox: %+v", rows)
	}
	w := f.request(t, "POST", "/app/v1/inbox/"+id+"/answer", `{"verb":"ratify"}`)
	if w.Code != 200 || timer == nil {
		t.Fatalf("held ratification: %d %s", w.Code, w.Body)
	}
	pending, err := f.store.Get(item.ID)
	if err != nil || pending.Status != standing.StatusProposed {
		t.Fatal("Inbox ran before its hold")
	}
	f.server.inbox.Lock()
	clock = clock.Add(5 * time.Second)
	f.server.inbox.Unlock()
	timer()
	pending, err = f.store.Get(item.ID)
	if err != nil || pending.Status != standing.StatusActive {
		t.Fatalf("Inbox ratification: %+v %v", pending, err)
	}
	runner := &orderSayRunner{}
	ticker := &standing.Ticker{Store: f.store, Runner: runner, Now: func() time.Time { return f.now.Add(time.Hour) }}
	if _, err = ticker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending, err = f.store.Get(item.ID)
	if err != nil || pending.AppWaitingRun() == nil {
		t.Fatal("no run")
	}
	rows, err = f.server.RefreshInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id = "ask:" + orderAskID(item.ID, pending.AppWaitingRun().ID)
	found = false
	for _, row := range rows {
		if row.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("run missing from Inbox: %+v", rows)
	}
	w = f.request(t, "POST", "/app/v1/inbox/"+id+"/answer", `{"verb":"go"}`)
	if w.Code != 200 {
		t.Fatalf("run answer: %d %s", w.Code, w.Body)
	}
	f.server.inbox.Lock()
	clock = clock.Add(5 * time.Second)
	f.server.inbox.Unlock()
	timer()
	pending, err = f.store.Get(item.ID)
	if err != nil || pending.AppWaitingRun().State != "authorized" || pending.App.ApprovedRuns != 1 {
		t.Fatalf("run approval: %+v %v", pending, err)
	}
}

func TestOrderRunnerDeliversScopeThroughRealAppRegistry(t *testing.T) {
	f := newOrderFixture(t)
	other := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	f.registry.mu.Lock()
	err := f.registry.disk(func(d *agentDisk) error {
		for id, record := range d.Records {
			record.Agent.CrewID = "crew-fixture"
			d.Records[id] = record
		}
		return nil
	}, true)
	f.registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	item := f.propose(t, false)
	item.App.Scope, item.App.CrewID = "this_crew", "crew-fixture"
	runner := session.NewStandingRunner(session.Config{}, f.store.Root())
	deliverer, ok := runner.(standing.Deliverer)
	if !ok {
		t.Fatal("runner has no delivery door")
	}
	if _, err = deliverer.Deliver(context.Background(), item, standing.Pending{ID: "registry-scope-note", Kind: standing.ActionSay, Text: "CI failed"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{f.agent.ID, other.ID} {
		dir := filepath.Dir(f.registry.live[id].conn.SessionFile)
		files, err := standing.Drain(dir)
		if err != nil {
			t.Fatal(err)
		}
		notes := 0
		for _, file := range files {
			notes += len(file.Notes)
			if err = file.Ack(); err != nil {
				t.Fatal(err)
			}
		}
		if notes != 1 {
			t.Fatalf("%s got %d scoped notes", id, notes)
		}
	}
}

func TestOrderReportIsReadableInInboxWhenScopeIsNotHere(t *testing.T) {
	f := newOrderFixture(t)
	item := f.propose(t, false)
	_, err := f.store.ChangeApp(item.ID, func(it *standing.Item) error {
		it.Status = standing.StatusActive
		it.App.Scope = "not_here"
		it.App.ApprovedRuns = 5
		it.App.Runs = []standing.AppRun{{ID: "run_1", At: f.now, State: "done", Report: "cart.test.ts still fails"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.server.RefreshInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := "ask:" + orderAskID(item.ID, "run_1_report")
	var report InboxItem
	for _, row := range rows {
		if row.ID == id {
			report = row
		}
	}
	if report.ID == "" || len(report.Context.Before) != 2 || report.Context.Before[1].Text != "cart.test.ts still fails" {
		t.Fatalf("report inaccessible: %+v", rows)
	}
	reply, err := (&orderInboxSource{s: f.server}).AnswerInbox(context.Background(), report, InboxAnswerRequest{Verb: "not_right"})
	if err != nil || !reply.OK {
		t.Fatalf("report feedback: %+v %v", reply, err)
	}
	got, err := f.store.Get(item.ID)
	if err != nil || got.App.ApprovedRuns != 0 || got.App.Runs[0].State != "not_right" {
		t.Fatal("report feedback did not reset trust")
	}
}
