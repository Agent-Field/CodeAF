package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeStore struct {
	svc.Store
	bundle *spec.Bundle
}

func (f fakeStore) Definition(context.Context, string) (*spec.Bundle, error) { return f.bundle, nil }

// fakeRecords remembers the scope and params of every write.
type fakeRecords struct {
	svc.Records
	mu      sync.Mutex
	rows    []bridge.Record
	queries []bridge.QueryParams
	queryFn func(bridge.QueryParams) (bridge.QueryResult, error)
	creates []bridge.CreateRecordParams
	deletes []bridge.DeleteRecordParams
	updates []bridge.UpdateRecordParams
	scopes  []svc.Scope
}

func (f *fakeRecords) Query(_ context.Context, _ svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	f.queries = append(f.queries, p)
	if f.queryFn != nil {
		return f.queryFn(p)
	}
	rows := f.rows
	if rows == nil {
		rows = []bridge.Record{{ID: "ct_1", Type: "contact", Fields: map[string]any{"name": "Ada", "phone": "+1 555 0100"}}}
	}
	return bridge.QueryResult{Rows: rows, Total: len(rows)}, nil
}

func (f *fakeRecords) Create(_ context.Context, scope svc.Scope, p bridge.CreateRecordParams) (bridge.Record, error) {
	f.creates, f.scopes = append(f.creates, p), append(f.scopes, scope)
	return bridge.Record{ID: "ct_new", Type: p.Type, Fields: p.Set}, nil
}

func (f *fakeRecords) Delete(_ context.Context, scope svc.Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	f.deletes, f.scopes = append(f.deletes, p), append(f.scopes, scope)
	ids := p.IDs
	if p.ID != "" {
		ids = []string{p.ID}
	}
	return bridge.DeleteRecordResult{IDs: ids}, nil
}

func (f *fakeRecords) Get(_ context.Context, _ svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	return bridge.Record{ID: p.ID, Type: p.Type, Fields: map[string]any{"name": "Hooli"}}, nil
}

func (f *fakeRecords) Update(_ context.Context, scope svc.Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates, f.scopes = append(f.updates, p), append(f.scopes, scope)
	return bridge.UpdateRecordResult{Rows: []bridge.Record{{ID: p.ID, Type: p.Type, Fields: p.Set, AI: []string{"stage"}}}}, nil
}

type fakeApprovals struct {
	svc.Approvals
	opened []bridge.Approval
}

func (f *fakeApprovals) Open(_ context.Context, _ svc.Scope, a bridge.Approval) (bridge.Approval, error) {
	a.ID = "ap_1"
	f.opened = append(f.opened, a)
	return a, nil
}

type fakeConnectors struct{ called []string }

func (f *fakeConnectors) Outbound(tool string) bool { return strings.HasSuffix(tool, ".send") }
func (f *fakeConnectors) Call(_ context.Context, _ svc.Scope, tool string, _ map[string]any) (any, error) {
	f.called = append(f.called, tool)
	return map[string]any{"ok": true}, nil
}

// scriptedModel answers each call with the next scripted reply and keeps the
// calls it saw.
type scriptedModel struct {
	replies []string
	calls   []svc.ModelCall
}

func (m *scriptedModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.calls = append(m.calls, call)
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return svc.ModelReply{Text: reply, Cost: 0.001}, nil
}

type rig struct {
	svc       *Service
	records   *fakeRecords
	approvals *fakeApprovals
	conns     *fakeConnectors
	model     *scriptedModel
	root      string
}

func newRig(t *testing.T, replies ...string) *rig {
	t.Helper()
	files, _ := spec.GoldenCRM()
	bundle, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{records: &fakeRecords{}, approvals: &fakeApprovals{}, conns: &fakeConnectors{},
		model: &scriptedModel{replies: replies}, root: t.TempDir()}
	cfg := svc.Config{Root: r.root, Ctx: context.Background()}
	all := &svc.Services{Store: fakeStore{bundle: bundle}, Records: r.records, Approvals: r.approvals,
		Connectors: r.conns, Model: r.model}
	all.Hub = realtime.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	r.svc = New(cfg, all)
	return r
}

var scope = svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}

// ask sends one message and collects the turn's events up to done or error.
func (r *rig) ask(t *testing.T, text string) []bridge.AgentEvent {
	t.Helper()
	return r.askWithin(t, text, 5*time.Second)
}

func (r *rig) askWithin(t *testing.T, text string, ceiling time.Duration) []bridge.AgentEvent {
	t.Helper()
	hub := r.svc.all.Hub
	events, cancel := hub.Subscribe("app/crm/agent/*")
	defer cancel()
	res, err := r.svc.Send(context.Background(), scope, bridge.AgentSendParams{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	var got []bridge.AgentEvent
	timeout := time.After(ceiling)
	for {
		select {
		case ev := <-events:
			e := ev.Data.(bridge.AgentEvent)
			if e.Turn != res.Turn {
				continue
			}
			got = append(got, e)
			if e.Kind == bridge.AgentDone || e.Kind == bridge.AgentError {
				return got
			}
		case <-timeout:
			t.Fatalf("turn did not finish: %+v", got)
		}
	}
}

func kinds(events []bridge.AgentEvent) []bridge.AgentEventKind {
	var out []bridge.AgentEventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func TestRecordsEditThroughTheCapabilityChecker(t *testing.T) {
	r := newRig(t,
		`{"tool":"records.update","args":{"type":"deal","id":"d_1","set":{"stage":"Won"}}}`,
		`{"say":"Marked the deal Won."}`)
	events := r.ask(t, "mark d_1 won")
	want := []bridge.AgentEventKind{bridge.AgentTool, bridge.AgentDelta, bridge.AgentDone}
	if got := kinds(events); strings.Join(toStrings(got), ",") != strings.Join(toStrings(want), ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if len(r.records.updates) != 1 || r.records.updates[0].Set["stage"] != "Won" {
		t.Fatalf("updates = %+v", r.records.updates)
	}
	if !r.records.scopes[0].Caller.Agent {
		t.Fatal("agent writes must carry Caller.Agent so records labels them AI (rule 09)")
	}
	if events[2].Cost == nil || events[2].Cost.Spent != 0.002 {
		t.Fatalf("cost = %+v", events[2].Cost)
	}
}

func TestToolsPassTheCheckerAndMaskedValuesStayHidden(t *testing.T) {
	tests := []struct {
		name, tool, wantInFeedback string
	}{
		{"unlisted tool", `{"tool":"shell","args":{}}`, "no tool shell"},
		{"definition write", `{"tool":"apps.keep","args":{}}`, "no tool apps.keep"},
		{"unknown record type", `{"tool":"records.query","args":{"type":"ghost"}}`, "cannot read records of type ghost"},
		{"masked field", `{"tool":"records.query","args":{"type":"contact"}}`, `"name":"Ada"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.tool, `{"say":"ok"}`)
			r.ask(t, "go")
			feedback := r.model.calls[1].Messages[len(r.model.calls[1].Messages)-1]
			if feedback.Role != "user" || !strings.Contains(feedback.Text, tc.wantInFeedback) {
				t.Fatalf("feedback = %+v", feedback)
			}
			if strings.Contains(feedback.Text, "555") {
				t.Fatalf("masked phone leaked: %s", feedback.Text)
			}
		})
	}
}

func TestChangeRequestAnswersThatChangesTheAppWithShapeThis(t *testing.T) {
	r := newRig(t, `{"shape_this":true,"say":"sure, adding it"}`)
	request := "add a Region column to deals"
	events := r.ask(t, request)
	if len(events) != 2 || events[0].Kind != bridge.AgentShapeThis {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Text != "That changes the app" || events[0].Request != request {
		t.Fatalf("shape_this = %+v", events[0])
	}
	if len(r.records.updates) != 0 || len(r.model.calls) != 1 {
		t.Fatal("a change request must not touch content or loop on")
	}
}

func TestOutboundActionWaitsForApproval(t *testing.T) {
	r := newRig(t,
		`{"tool":"gmail.draft","args":{"to":"ada@example.com","body":"Hi"}}`,
		`{"tool":"gmail.send","args":{"to":"ada@example.com","body":"Hi"}}`,
		`{"say":"The email waits for your approval."}`)
	events := r.ask(t, "email Ada")
	if got := strings.Join(toStrings(kinds(events)), ","); got != "tool,approval,tool,approval,delta,done" {
		t.Fatalf("events = %s", got)
	}
	if len(r.conns.called) != 0 {
		t.Fatalf("connector calls = %v: connector actions must wait for approval", r.conns.called)
	}
	if len(r.approvals.opened) != 2 || r.approvals.opened[1].Kind != "send" || r.approvals.opened[1].Tool != "gmail.send" {
		t.Fatalf("approvals = %+v", r.approvals.opened)
	}
	if events[3].Approval == nil || events[3].Approval.ID != "ap_1" {
		t.Fatalf("approval event = %+v", events[3])
	}
}

func TestThreadsPersistInTheAppDirAndContinue(t *testing.T) {
	r := newRig(t, `{"say":"one"}`, `{"say":"two"}`)
	events, cancel := r.svc.all.Hub.Subscribe("app/crm/agent/*")
	defer cancel()
	first, _ := r.svc.Send(context.Background(), scope, bridge.AgentSendParams{Text: "hi", Record: "deal/d_1", Screen: "pipeline"})
	waitDone(t, events, first.Turn)
	second, _ := r.svc.Send(context.Background(), scope, bridge.AgentSendParams{Thread: first.Thread, Text: "again"})
	waitDone(t, events, second.Turn)

	raw, err := os.ReadFile(filepath.Join(r.root, "crm", "threads", first.Thread+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved thread
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 4 || !strings.Contains(saved.Messages[0].Text, "[record in view: deal/d_1]") {
		t.Fatalf("saved = %+v", saved.Messages)
	}
	if len(r.model.calls[1].Messages) != 3 {
		t.Fatalf("second call saw %d messages, want the thread so far", len(r.model.calls[1].Messages))
	}
}

func TestSendRejectsEmptyTextAndBadThread(t *testing.T) {
	r := newRig(t)
	for _, p := range []bridge.AgentSendParams{{Text: " "}, {Text: "hi", Thread: "../x"}} {
		if _, err := r.svc.Send(context.Background(), scope, p); err == nil {
			t.Fatalf("Send(%+v) succeeded", p)
		}
	}
}

func TestPromptNamesOnlyTheRealTools(t *testing.T) {
	r := newRig(t, `{"say":"ok"}`)
	r.ask(t, "hi")
	system := r.model.calls[0].System
	for _, want := range []string{"You are the CRM agent", "records.update", "gmail.send", "masked, hidden"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if r.model.calls[0].Model != "deepseek/deepseek-v4.1-flash" || r.model.calls[0].App != "crm" {
		t.Fatalf("call = %+v", r.model.calls[0])
	}
}

func waitDone(t *testing.T, events <-chan svc.Event, turn string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if e := ev.Data.(bridge.AgentEvent); e.Turn == turn && (e.Kind == bridge.AgentDone || e.Kind == bridge.AgentError) {
				return
			}
		case <-timeout:
			t.Fatal("turn did not finish")
		}
	}
}

func toStrings(kinds []bridge.AgentEventKind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}

// Results go back as user turns: the model answers in JSON text, so there is
// no tool_call_id for a "tool" message, and the API refuses one without it.
func TestToolResultsGoBackAsUserTurnsNamingTheTool(t *testing.T) {
	r := newRig(t,
		`{"tool":"records.query","args":{"type":"contact"}}`,
		`{"tool":"records.get","args":{"type":"contact","id":"ct_1"}}`,
		`{"say":"Done."}`)
	r.ask(t, "look around")
	final := r.model.calls[2].Messages
	var results []string
	for _, m := range final {
		if m.Role == "tool" {
			t.Fatalf("tool message in the request: %+v", m)
		}
		if strings.HasPrefix(m.Text, "Result of ") {
			if m.Role != "user" {
				t.Fatalf("result role = %s", m.Role)
			}
			results = append(results, strings.SplitN(m.Text, ":", 2)[0])
		}
	}
	if got := strings.Join(results, ","); got != "Result of records.query,Result of records.get" {
		t.Fatalf("results = %s", got)
	}
}

func TestErrorsAreLabelledAsTheAppAgent(t *testing.T) {
	r := newRig(t, `{"say":"ok"}`)
	r.ask(t, "hi")
	if got := r.model.calls[0].Role; got != "app agent" {
		t.Fatalf("role = %q", got)
	}
}

func TestTheStreamHasPlainSummariesNotCallJSON(t *testing.T) {
	r := newRig(t,
		`{"tool":"records.query","args":{"type":"contact","limit":50,"query":"sort:-amount"}}`,
		"**Ada** is your biggest contact.")
	events := r.ask(t, "who is biggest?")
	if events[0].Kind != bridge.AgentTool || events[0].Summary != "Looked up 1 contact" {
		t.Fatalf("tool event = %+v", events[0])
	}
	for _, e := range events {
		if strings.ContainsAny(e.Text+e.Summary, "{}") {
			t.Fatalf("raw JSON reached the person: %+v", e)
		}
	}
	if events[1].Kind != bridge.AgentDelta || events[1].Text != "**Ada** is your biggest contact." {
		t.Fatalf("answer = %+v", events[1])
	}
}

func TestRefFieldsComeWithTheTitleOfWhatTheyPointAt(t *testing.T) {
	r := newRig(t, `{"tool":"records.query","args":{"type":"contact"}}`, `{"say":"ok"}`)
	r.records.rows = []bridge.Record{{ID: "ct_1", Type: "contact", Fields: map[string]any{"name": "Ada", "company": "co_hooli"}}}
	r.ask(t, "who works where?")
	msgs := r.model.calls[1].Messages
	if feedback := msgs[len(msgs)-1].Text; !strings.Contains(feedback, `"company_title":"Hooli"`) {
		t.Fatalf("feedback = %s", feedback)
	}
}

func TestThreadsSavedWithToolMessagesLoadAsUserTurns(t *testing.T) {
	store := newThreadStore(t.TempDir())
	old := &thread{ID: "t1", Messages: []svc.ModelMessage{{Role: "user", Text: "hi"}, {Role: "tool", Text: "{}"}}}
	if err := store.save("crm", old); err != nil {
		t.Fatal(err)
	}
	got, err := store.load("crm", "t1")
	if err != nil || got.Messages[1].Role != "user" {
		t.Fatalf("loaded %+v, %v", got, err)
	}
}

func TestAReplyWithSeveralJSONObjectsActsOnTheFirst(t *testing.T) {
	a := parseAction(`{"tool":"records.query","args":{"type":"deal"}}` + "\n\nNote: waiting.\n" + `{"say":"Waiting."}`)
	if a.Tool != "records.query" || a.Say != "" {
		t.Fatalf("action = %+v", a)
	}
}
