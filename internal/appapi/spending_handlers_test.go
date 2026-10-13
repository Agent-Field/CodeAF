package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func writeResourceLedger(t *testing.T, home string, lines []session.UsageLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "v3"), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(home, "v3", session.UsageLedgerName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if err := json.NewEncoder(f).Encode(line); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSpendingLedgerKeepsSubCentsAndFamilyAttribution(t *testing.T) {
	home := t.TempDir()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, loc)
	s := resourceServer(t, home, &now, nil)
	lines := []session.UsageLine{
		{At: now.Add(-24 * time.Hour), USD: 4, Session: "old", Model: "m-old"},
		{At: now.Add(24 * time.Hour), USD: 99, Session: "future", Model: "m-future"},
	}
	for i := range 10 {
		lines = append(lines, session.UsageLine{At: now.Add(-time.Duration(i) * time.Minute), USD: .001, Session: "child", Root: "a-root", Task: "t-child", Model: "m-worker"})
	}
	writeResourceLedger(t, home, lines)
	w := resourceRequest(s, "GET", "/app/v1/spending", "", "")
	var result SpendingResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Body.String())
	}
	if result.Today.Cap.USD != "20.00" || result.Today.Spent.USD != "0.01" || len(result.Rows) != 10 || len(result.ByAgent) != 1 || result.ByAgent[0].Agent != "a-root" || result.ByAgent[0].Spent.USD != "0.01" {
		t.Fatalf("ledger: %+v", result)
	}
	w = resourceRequest(s, "GET", "/app/v1/spending?when=week&by=model", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Rows) != 11 || result.Today.Spent.USD != "0.01" {
		t.Fatalf("week: %d %s", w.Code, w.Body.String())
	}
	w = resourceRequest(s, "GET", "/app/v1/spending/today", "", "")
	var today SpendingToday
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &today) != nil || today != result.Today {
		t.Fatalf("today: %d %s", w.Code, w.Body.String())
	}
}
func TestSpendingDayRaiseIsDurableAndIdempotent(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	s := resourceServer(t, home, &now, nil)
	body := `{"scope":"day","to":{"usd":"30.00"},"for":"for_good"}`
	first := resourceRequest(s, "POST", "/app/v1/caps/raise", body, "raise")
	if first.Code != 200 {
		t.Fatalf("raise: %d %s", first.Code, first.Body.String())
	}
	again := resourceRequest(s, "POST", "/app/v1/caps/raise", body, "raise")
	if again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Fatal("raise was not replayed")
	}
	changed := strings.ReplaceAll(body, "30.00", "40.00")
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", changed, "raise"); w.Code != 409 {
		t.Fatal(w.Body.String())
	}
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", body, "new"); w.Code != 409 {
		t.Fatal("non-raise accepted")
	}
	second := resourceServer(t, home, &now, nil)
	w := resourceRequest(second, "GET", "/app/v1/spending/today", "", "")
	var today SpendingToday
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &today) != nil || today.Cap.USD != "30.00" || today.Spent.USD != "0.00" {
		t.Fatal(w.Body.String())
	}
	if value, err := config.DailyBudgetUSDAt(home); err != nil || value != 30 {
		t.Fatalf("engine day cap: %v %v", value, err)
	}
}
func TestSpendingAgentRaiseUsesExecutionOwnerAndExpect(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	engine := &fakeAgentEngine{}
	registry := NewAgentRegistry(home, nil)
	registry.now = func() time.Time { return now }
	registry.live["a-one"] = &agentLive{conn: AgentConnection{Engine: engine}}
	registry.mu.Lock()
	err := registry.disk(func(d *agentDisk) error {
		d.Records["a-one"] = agentRecord{Agent: Agent{ID: "a-one", Budget: Money{USD: "5.00"}, State: "idle", Version: 3}}
		return nil
	}, true)
	registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s := New("test-resource-token", Options{ProfileDir: home, Agents: registry})
	t.Cleanup(s.Close)
	stale := `{"scope":"agent","id":"a-one","to":{"usd":"8.00"},"for":"for_good","expect":{"version":2}}`
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", stale, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "stale") {
		t.Fatal(w.Body.String())
	}
	good := strings.ReplaceAll(stale, `"version":2`, `"version":3`)
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", good, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if engine.budget != 8 {
		t.Fatalf("live engine budget: %v", engine.budget)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if err := registry.disk(func(d *agentDisk) error {
		if d.Records["a-one"].Agent.Budget.USD != "8.00" {
			t.Fatal("raise not saved")
		}
		return nil
	}, false); err != nil {
		t.Fatal(err)
	}
}
func TestSpendingRefusalsDoNotChangeCaps(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	s := resourceServer(t, home, &now, nil)
	cases := []struct {
		body   string
		status int
	}{
		{`null`, 422}, {`{} {}`, 422}, {`{"scope":"day","to":{"usd":"25"},"for":"for_good"}`, 422},
		{`{"scope":"day","id":"a","to":{"usd":"25.00"},"for":"for_good"}`, 422},
		{`{"scope":"unknown","to":{"usd":"25.00"},"for":"for_good"}`, 422},
		{`{"scope":"agent","to":{"usd":"25.00"},"for":"for_good"}`, 422},
		{`{"scope":"day","to":{"usd":"0.00"},"for":"for_good"}`, 422},
		{`{"scope":"day","to":{"usd":"19.00"},"for":"for_good"}`, 409},

		{`{"scope":"task","id":"t","to":{"usd":"25.00"},"for":"for_good"}`, 404},
		{`{"scope":"crew","id":"c","to":{"usd":"25.00"},"for":"for_good"}`, 409},
	}
	for _, tc := range cases {
		if w := resourceRequest(s, "POST", "/app/v1/caps/raise", tc.body, ""); w.Code != tc.status {
			t.Errorf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"when=tomorrow", "by=unknown"} {
		if w := resourceRequest(s, "GET", "/app/v1/spending?"+query, "", ""); w.Code != 422 {
			t.Fatal(w.Body.String())
		}
	}
	w := resourceRequest(s, "GET", "/app/v1/spending/today", "", "")
	if !strings.Contains(w.Body.String(), `"usd":"20.00"`) {
		t.Fatal(w.Body.String())
	}
}

type fakeResourceSpending struct {
	cap    Money
	raised RaiseCapRequest
	now    time.Time
	rows   []MeasuredSpendingRow
}

func (f *fakeResourceSpending) SpendingRows(context.Context) ([]MeasuredSpendingRow, error) {
	return f.rows, nil
}
func (f *fakeResourceSpending) SpendingCap(context.Context, string, string) (Money, error) {
	return f.cap, nil
}
func (f *fakeResourceSpending) RaiseCap(_ context.Context, body RaiseCapRequest, now time.Time) error {
	f.raised, f.now, f.cap = body, now, body.To
	return nil
}
func TestSpendingUsesCapResourceInterfaces(t *testing.T) {
	now := time.Now()
	backend := &fakeResourceSpending{}
	s := resourceServer(t, t.TempDir(), &now, WithSpendingBackend(context.Background(), backend))
	for _, scope := range []string{"day", "agent", "member", "crew", "task"} {
		backend.cap = Money{USD: "2.00"}
		id := "owner"
		if scope == "day" {
			id = ""
		}
		body := fmt.Sprintf(`{"scope":%q,"id":%q,"to":{"usd":"3.00"},"for":"today","expect":{"version":7}}`, scope, id)
		w := resourceRequest(s, "POST", "/app/v1/caps/raise", body, "")
		if w.Code != 200 || backend.raised.Scope != scope || backend.raised.ID != id || backend.raised.For != "today" || *backend.raised.Expect.Version != 7 || !backend.now.Equal(now) {
			t.Fatalf("%s forwarding: %d %+v", scope, w.Code, backend.raised)
		}
	}
}
func TestSpendChangedThrottlesWithoutLosingLatestRead(t *testing.T) {
	now := time.Now()
	backend := &fakeResourceSpending{cap: Money{USD: "20.00"}}
	s := resourceServer(t, t.TempDir(), &now, WithSpendingBackend(context.Background(), backend))
	if err := s.PublishSpending(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	backend.rows = []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "agent", ID: "a"}}, USD: 1.2}}
	now = now.Add(999 * time.Millisecond)
	w := resourceRequest(s, "GET", "/app/v1/spending/today", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"usd":"1.20"`) || s.events.seq != 1 {
		t.Fatalf("throttle: %d %s seq %d", w.Code, w.Body.String(), s.events.seq)
	}
	now = now.Add(time.Millisecond)
	if err := s.PublishSpending(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if s.events.seq != 2 || !strings.Contains(string(s.events.ring[1].data), `"agent":"a"`) {
		t.Fatal("latest spend was not published at one second")
	}
}
func TestResourceRoutesRequireBearerAndMethods(t *testing.T) {
	now := time.Now()
	s := resourceServer(t, t.TempDir(), &now, nil)
	for _, path := range []string{"/app/v1/spending", "/app/v1/spending/today", "/app/v1/caps/raise", "/app/v1/memory", "/app/v1/memory/m/forget", "/app/v1/memory/m/restore"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:53021"+path, nil))
		if w.Code != 401 {
			t.Fatalf("%s auth: %d", path, w.Code)
		}
		if w := resourceRequest(s, "DELETE", path, "{}", ""); w.Code != 405 || w.Header().Get("Allow") == "" {
			t.Fatalf("%s method: %d %s", path, w.Code, w.Body.String())
		}
	}
}

type fakeRuleCaps struct {
	fakeResourceSpending
	caps map[string]Money
}

func (f *fakeRuleCaps) SpendingCap(_ context.Context, scope, _ string) (Money, error) {
	return f.caps[scope], nil
}
func TestSpendingAdmissionUsesDayAgentAndTaskRules(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		rows   []MeasuredSpendingRow
		capped string
	}{
		{"day", []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "agent", ID: "other"}}, USD: 20}}, "day"},
		{"agent", []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "agent", ID: "a"}}, USD: 5}}, "agent"},
		{"task lifetime", []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now.AddDate(0, 0, -1), Who: SpendingWho{Kind: "agent", ID: "a"}, What: "task:t"}, USD: 2}}, "task"},
		{"agent resets daily", []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now.AddDate(0, 0, -1), Who: SpendingWho{Kind: "agent", ID: "a"}}, USD: 5}}, ""},
		{"task ids belong to agent", []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "agent", ID: "other"}, What: "task:t"}, USD: 2}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeRuleCaps{caps: map[string]Money{"day": usdMoney(DefaultDayCapUSD), "agent": usdMoney(config.AppDefaultAgentBudget), "task": usdMoney(DefaultTaskCapUSD)}}
			backend.rows = tc.rows
			s := resourceServer(t, t.TempDir(), &now, WithSpendingBackend(context.Background(), backend))
			err := s.CheckSpending(context.Background(), "a", "t")
			if tc.capped == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.capped+" cap reached") {
				t.Fatalf("wrong cap: %v", err)
			}
		})
	}
}

func TestSpendingTemporaryDayRaiseExpiresInHTTPAndEngine(t *testing.T) {
	home := t.TempDir()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 0, 30, 0, 0, loc)
	s := resourceServer(t, home, &now, nil)
	body := `{"scope":"day","to":{"usd":"30.00"},"for":"today"}`
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", body, "raise-today"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	now = time.Date(2026, 11, 1, 23, 59, 59, 0, loc)
	second := resourceServer(t, home, &now, nil)
	if w := resourceRequest(second, "GET", "/app/v1/spending/today", "", ""); !strings.Contains(w.Body.String(), `"usd":"30.00"`) {
		t.Fatal(w.Body.String())
	}
	now = now.Add(time.Second)
	if w := resourceRequest(second, "GET", "/app/v1/spending/today", "", ""); !strings.Contains(w.Body.String(), `"usd":"20.00"`) {
		t.Fatal(w.Body.String())
	}
	if cap, err := config.AppCapAt(home, "day", "", 20, now); err != nil || cap != 20 {
		t.Fatalf("engine cap %v %v", cap, err)
	}
}

func TestSpendingTaskRaiseKeepsAgentOwnershipAndReachesEngine(t *testing.T) {
	home, now := t.TempDir(), time.Now()
	registry := NewAgentRegistry(home, nil)
	registry.now = func() time.Time { return now }
	registry.live["a"] = &agentLive{conn: AgentConnection{Engine: &fakeAgentEngine{tasks: []session.PlanTaskRow{{ID: "1", Status: "running"}}}}}
	registry.mu.Lock()
	err := registry.disk(func(d *agentDisk) error {
		d.Records["a"] = agentRecord{Agent: Agent{ID: "a", Budget: usdMoney(5), State: "idle", Version: 1}}
		return nil
	}, true)
	registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s := New("test-resource-token", Options{ProfileDir: home, Agents: registry})
	t.Cleanup(s.Close)
	body := `{"scope":"task","id":"a:1","to":{"usd":"3.00"},"for":"for_good"}`
	if w := resourceRequest(s, "POST", "/app/v1/caps/raise", body, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if cap, err := config.AppCapAt(home, "task", config.AppTaskCapID("a", "1"), 2, now); err != nil || cap != 3 {
		t.Fatalf("task cap %v %v", cap, err)
	}
	if cap, err := config.AppCapAt(home, "task", config.AppTaskCapID("other", "1"), 2, now); err != nil || cap != 2 {
		t.Fatalf("other agent task cap %v %v", cap, err)
	}
}

func TestSpendingBudgetQuestionsResolveOnlyActualWaitingItems(t *testing.T) {
	home, now := t.TempDir(), time.Now()
	backend := &fakeResourceSpending{cap: usdMoney(20), rows: []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "system"}}, USD: 20}}}
	s := resourceServer(t, home, &now, WithSpendingBackend(context.Background(), backend))
	if err := s.RefreshSpending(context.Background()); err != nil {
		t.Fatal(err)
	}
	asks, err := s.SpendingBudgetAsks()
	if err != nil || len(asks) != 1 || asks[0].Kind != "budget" {
		t.Fatalf("asks %v %v", asks, err)
	}
	seq := s.events.seq
	if err := s.RefreshSpending(context.Background()); err != nil || s.events.seq != seq {
		t.Fatalf("duplicate question: %d %v", s.events.seq, err)
	}
	w := resourceRequest(s, "POST", "/app/v1/caps/raise", `{"scope":"day","to":{"usd":"30.00"},"for":"today"}`, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	asks, err = s.SpendingBudgetAsks()
	if err != nil || len(asks) != 0 {
		t.Fatalf("raised ask %v %v", asks, err)
	}
	found := false
	for i := uint64(0); i < s.events.seq; i++ {
		if row := s.events.ring[i]; row.kind == EventInboxResolved && strings.Contains(string(row.data), `"item":"budget:`) {
			found = true
		}
	}
	if !found {
		t.Fatal("waiting budget question was not resolved")
	}
}

func TestSpendingASecondCapQuestionGetsAFreshID(t *testing.T) {
	now := time.Now()
	backend := &fakeResourceSpending{cap: usdMoney(20), rows: []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "system"}}, USD: 20}}}
	s := resourceServer(t, t.TempDir(), &now, WithSpendingBackend(context.Background(), backend))
	if err := s.RefreshSpending(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := s.SpendingBudgetAsks()
	if err != nil || len(first) != 1 {
		t.Fatalf("first %v %v", first, err)
	}
	backend.cap = usdMoney(30)
	if err := s.RefreshSpending(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend.rows[0].USD = 30
	if err := s.RefreshSpending(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := s.SpendingBudgetAsks()
	if err != nil || len(second) != 1 || first[0].ID == second[0].ID {
		t.Fatalf("reused question id: %v %v %v", first, second, err)
	}
}

func TestSpendingAdmissionUsesTheAgentWhenTaskNumbersRepeat(t *testing.T) {
	home, now := t.TempDir(), time.Now()
	registry := NewAgentRegistry(home, nil)
	registry.now = func() time.Time { return now }
	for _, id := range []string{"a", "b"} {
		registry.live[id] = &agentLive{conn: AgentConnection{Engine: &fakeAgentEngine{tasks: []session.PlanTaskRow{{ID: "1", Status: "running"}}}}}
	}
	registry.mu.Lock()
	err := registry.disk(func(d *agentDisk) error {
		for _, id := range []string{"a", "b"} {
			d.Records[id] = agentRecord{Agent: Agent{ID: id, State: "idle", Budget: usdMoney(5), Version: 1}}
		}
		return nil
	}, true)
	registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	writeResourceLedger(t, home, []session.UsageLine{{At: now, Session: "a", Task: "1", USD: 2}})
	s := New("test-resource-token", Options{ProfileDir: home, Agents: registry})
	t.Cleanup(s.Close)
	if err := s.CheckSpending(context.Background(), "a", "1"); err == nil || !strings.Contains(err.Error(), "task cap reached") {
		t.Fatal(err)
	}
	if err := s.CheckSpending(context.Background(), "b", "b:1"); err != nil {
		t.Fatal(err)
	}
	w := resourceRequest(s, "POST", "/app/v1/caps/raise", `{"scope":"task","id":"1","to":{"usd":"3.00"},"for":"for_good"}`, "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "needs its agent id") {
		t.Fatal(w.Body.String())
	}
}
