package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

var spendingContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/spending", Response: "SpendingResponse"}, handle: (*Server).spendingHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/spending/today", Response: "SpendingToday"}, handle: (*Server).spendingHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/caps/raise", Request: "RaiseCapRequest", Response: "ActionResponse"}, handle: (*Server).spendingHandler},
}

// SpendingBackend is the resource boundary for measured costs and live caps.
// RaiseCap must check expect and the old cap atomically before changing a limit.
// A temporary raise must expire in the execution owner, not just in its HTTP view.
type SpendingBackend interface {
	SpendingRows(context.Context) ([]MeasuredSpendingRow, error)
	SpendingCap(context.Context, string, string) (Money, error)
	RaiseCap(context.Context, RaiseCapRequest, time.Time) error
}

// MeasuredSpendingRow keeps sub-cent provider receipts until totals are summed.
// Money is rounded only at the wire boundary; rounding each call loses real cost.
type MeasuredSpendingRow struct {
	SpendingRow
	USD float64
}

type spendingBackendKey struct{}

// WithSpendingBackend lets task and crew owners provide their caps through their
// own interfaces. The default adapter never edits another lane's state files.
func WithSpendingBackend(ctx context.Context, backend SpendingBackend) context.Context {
	return context.WithValue(ctx, spendingBackendKey{}, backend)
}
func (s *Server) spendingBackend() SpendingBackend {
	if s.options.Context != nil {
		if backend, ok := s.options.Context.Value(spendingBackendKey{}).(SpendingBackend); ok {
			return backend
		}
	}
	return nativeSpending{s: s}
}
func usdMoney(value float64) Money { return Money{USD: fmt.Sprintf("%.2f", value)} }
func measuredCost(row MeasuredSpendingRow) (float64, error) {
	if row.USD < 0 || math.IsNaN(row.USD) || math.IsInf(row.USD, 0) {
		return 0, agentError(500, "conflict", "the ledger contains an invalid measured cost")
	}
	return row.USD, nil
}
func moneyValue(value Money) (float64, error) {
	if !agentMoney.MatchString(value.USD) {
		return 0, agentError(422, "bad_input", "money needs USD with two decimal places")
	}
	amount, err := strconv.ParseFloat(value.USD, 64)
	return amount, err
}
func (s *Server) spendingHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.spendingAction)(w, r)
}
func (s *Server) spendingAction(w http.ResponseWriter, r *http.Request) {
	backend, now := s.spendingBackend(), s.resourceNow()
	if r.Method == "POST" {
		var body RaiseCapRequest
		if !decodeResourceObject(w, r, &body) {
			return
		}
		amount, err := moneyValue(body.To)
		if err != nil {
			resourceFailure(w, err)
			return
		}
		if amount <= 0 || (body.For != "today" && body.For != "for_good") || !strings.Contains(" day agent member crew task ", " "+body.Scope+" ") || (body.Scope == "day" && body.ID != "") || (body.Scope != "day" && strings.TrimSpace(body.ID) == "") {
			fail(w, 422, "bad_input", "raise needs a scope, its id, positive money and today or for_good")
			return
		}
		old, err := backend.SpendingCap(r.Context(), body.Scope, body.ID)
		if err != nil {
			resourceFailure(w, err)
			return
		}
		current, err := moneyValue(old)
		if err != nil {
			resourceFailure(w, err)
			return
		}
		if current == 0 || amount <= current {
			fail(w, 409, "conflict", "the new cap must be higher than the current cap")
			return
		}
		if err := backend.RaiseCap(r.Context(), body, now); err != nil {
			resourceFailure(w, err)
			return
		}
		// A cap change alone does not resolve a waiting ask. Its owner does that
		// when it accepts the raised limit, so the stream never invents an Inbox id.
		_ = s.PublishSpending(r.Context(), body.ID)
		write(w, 200, ActionResponse{OK: true})
		return
	}
	when, by := r.URL.Query().Get("when"), r.URL.Query().Get("by")
	if when == "" {
		when = "today"
	}
	if by == "" {
		by = "agent"
	}
	if when != "today" && when != "week" && when != "all" {
		fail(w, 422, "bad_input", "when must be today, week or all")
		return
	}
	if by != "agent" && by != "crew" && by != "job" && by != "model" {
		fail(w, 422, "bad_input", "by must be agent, crew, job or model")
		return
	}
	rows, err := backend.SpendingRows(r.Context())
	if err != nil {
		resourceFailure(w, err)
		return
	}
	cap, err := backend.SpendingCap(r.Context(), "day", "")
	if err != nil {
		resourceFailure(w, err)
		return
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	floor := start
	if when == "week" {
		floor = start.AddDate(0, 0, -6)
	}
	result := SpendingResponse{Today: SpendingToday{Cap: cap}, Rows: []SpendingRow{}, ByAgent: []AgentSpending{}}
	totals := map[string]float64{}
	var today float64
	for _, row := range rows {
		cost, err := measuredCost(row)
		if err != nil {
			resourceFailure(w, err)
			return
		}
		if !row.At.Before(start) && row.At.Before(end) {
			today += cost
		}
		if !row.At.Before(end) || (when != "all" && row.At.Before(floor)) {
			continue
		}
		row.Cost = usdMoney(cost)
		result.Rows = append(result.Rows, row.SpendingRow)
		if row.Who.Kind == "agent" && row.Who.ID != "" {
			totals[row.Who.ID] += cost
		}
	}
	result.Today.Spent = usdMoney(today)
	for id, spent := range totals {
		result.ByAgent = append(result.ByAgent, AgentSpending{Agent: id, Spent: usdMoney(spent)})
	}
	sort.Slice(result.ByAgent, func(i, j int) bool { return result.ByAgent[i].Agent < result.ByAgent[j].Agent })
	sort.SliceStable(result.Rows, func(i, j int) bool { return result.Rows[i].At.After(result.Rows[j].At) })
	if by != "agent" {
		sort.SliceStable(result.Rows, func(i, j int) bool {
			a, b := result.Rows[i], result.Rows[j]
			if by == "model" {
				return a.Model < b.Model
			}
			return a.Who.Kind+":"+a.Who.ID < b.Who.Kind+":"+b.Who.ID
		})
	}
	if strings.HasSuffix(r.URL.Path, "/today") {
		write(w, 200, result.Today)
	} else {
		write(w, 200, result)
	}
	_ = s.PublishSpending(r.Context(), "")
}

// PublishSpending is the call-completion hook for resource producers. The
// receipt throttles publications across windows to at most one per second.
// Producers call again on the next measured change; reads also refresh it.
func (s *Server) PublishSpending(ctx context.Context, agent string) error {
	backend := s.spendingBackend()
	rows, err := backend.SpendingRows(ctx)
	if err != nil {
		return err
	}
	cap, err := backend.SpendingCap(ctx, "day", "")
	if err != nil {
		return err
	}
	now := s.resourceNow()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	var total float64
	for _, row := range rows {
		if row.At.Before(start) || !row.At.Before(end) {
			continue
		}
		amount, err := measuredCost(row)
		if err != nil {
			return err
		}
		total += amount
	}
	event := SpendChangedEvent{Today: usdMoney(total), Cap: cap, Agent: agent}
	type receipt struct {
		Epoch string
		At    time.Time
		Today Money
		Cap   Money
	}
	return resourceDisk(s.resourceHome(), "spending-event", receipt{}, func(raw []byte) (any, error) {
		var old receipt
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		if old.Epoch == s.options.Epoch && ((old.Today == event.Today && old.Cap == event.Cap) || now.Sub(old.At) < time.Second) {
			return old, nil
		}
		if _, err := s.events.Publish(EventSpendChanged, event); err != nil {
			return nil, err
		}
		return receipt{Epoch: s.options.Epoch, At: now, Today: event.Today, Cap: cap}, nil
	})
}

type nativeSpending struct{ s *Server }

func (n nativeSpending) SpendingRows(context.Context) ([]MeasuredSpendingRow, error) {
	lines, err := session.ReadUsage(filepath.Join(n.s.resourceHome(), "v3", session.UsageLedgerName), time.Time{})
	if err != nil {
		return nil, err
	}
	result := []MeasuredSpendingRow{}
	for _, line := range lines {
		if line.USD <= 0 || math.IsNaN(line.USD) || math.IsInf(line.USD, 0) {
			continue
		}
		who := SpendingWho{Kind: "agent", ID: line.Session}
		if line.Root != "" {
			who.ID = line.Root
		}
		if who.ID == "" {
			who.Kind, who.Purpose = "system", line.Role
		}
		what := line.Role
		if line.Task != "" {
			what = "task:" + line.Task
		}
		if line.Standing != "" {
			what = "order:" + line.Standing
		}
		result = append(result, MeasuredSpendingRow{SpendingRow: SpendingRow{At: line.At, Who: who, What: what, Model: line.Model}, USD: line.USD})
	}
	return result, nil
}
func (n nativeSpending) SpendingCap(_ context.Context, scope, id string) (Money, error) {
	if scope == "day" || scope == "task" {
		values, _, err := config.AppSettingsAt(n.s.options.ProfileDir)
		if err != nil {
			return Money{}, err
		}
		key := "spending.day_cap"
		if scope == "task" {
			key = "spending.per_task"
		}
		return Money{USD: values[key].(map[string]string)["usd"]}, nil
	}
	if scope == "agent" && n.s.options.Agents != nil {
		a := n.s.options.Agents
		a.mu.Lock()
		defer a.mu.Unlock()
		var cap Money
		err := a.disk(func(d *agentDisk) error {
			row, ok := d.Records[id]
			if !ok {
				return agentError(404, "not_found", "no such agent")
			}
			cap = row.Agent.Budget
			return nil
		}, false)
		return cap, err
	}
	return Money{}, agentError(409, "not_allowed", "this cap needs its resource's execution owner")
}
func (n nativeSpending) RaiseCap(ctx context.Context, body RaiseCapRequest, _ time.Time) error {
	if body.For == "today" {
		return agentError(409, "not_allowed", "this execution owner cannot expire a cap raise at midnight")
	}
	if body.Scope == "day" {
		if body.Expect != nil {
			return agentError(422, "bad_input", "day cap expectations use the settings route and its ETag")
		}
		values, version, err := config.AppSettingsAt(n.s.options.ProfileDir)
		if err != nil {
			return err
		}
		current := values["spending.day_cap"].(map[string]string)["usd"]
		old, _ := strconv.ParseFloat(current, 64)
		amount, _ := moneyValue(body.To)
		if old == 0 || amount <= old {
			return agentError(409, "conflict", "the new cap must be higher than the current cap")
		}
		raw, _ := json.Marshal(body.To)
		patch := map[string]json.RawMessage{"spending.day_cap": raw}
		if err := config.ApplyAppSettings(n.s.options.ProfileDir, patch, version); err != nil {
			if err == config.ErrAppStale {
				return agentError(409, "stale", "the cap changed; read it again before acting")
			}
			return err
		}
		n.s.settingsChanged(patch)
		return nil
	}
	if body.Scope == "agent" && n.s.options.Agents != nil {
		a := n.s.options.Agents
		a.mu.Lock()
		defer a.mu.Unlock()
		var changed Agent
		err := a.disk(func(d *agentDisk) error {
			row, ok := d.Records[body.ID]
			if !ok {
				return agentError(404, "not_found", "no such agent")
			}
			old, _ := moneyValue(row.Agent.Budget)
			amount, _ := moneyValue(body.To)
			if amount <= old {
				return agentError(409, "conflict", "the new cap must be higher than the current cap")
			}
			var err error
			changed, _, err = a.mutate(ctx, d, body.ID, "", agentMutation{Budget: &body.To, Expect: body.Expect})
			return err
		}, true)
		if err == nil {
			a.publish(EventAgentChanged, changed, "budget")
		}
		return err
	}
	return agentError(409, "not_allowed", "this cap needs its resource's execution owner")
}

// CheckSpending is the admission door for app AI and turn owners. It checks
// measured totals before work begins, using a local calendar day per agent and
// the task's whole history. Callers must also reserve in-flight provider costs
// in their existing SpendGuard; this reading does not allocate a second ledger.
func (s *Server) CheckSpending(ctx context.Context, agent, task string) error {
	backend := s.spendingBackend()
	rows, err := backend.SpendingRows(ctx)
	if err != nil {
		return err
	}
	now := s.resourceNow()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	var daySpent, agentSpent, taskSpent float64
	for _, row := range rows {
		cost, err := measuredCost(row)
		if err != nil {
			return err
		}
		if !row.At.Before(start) && row.At.Before(end) {
			daySpent += cost
			if row.Who.Kind == "agent" && row.Who.ID == agent {
				agentSpent += cost
			}
		}
		if task != "" && row.Who.Kind == "agent" && row.Who.ID == agent && row.What == "task:"+task && !row.At.After(now) {
			taskSpent += cost
		}
	}
	checks := []struct {
		scope, id string
		spent     float64
	}{{"day", "", daySpent}}
	if agent != "" {
		checks = append(checks, struct {
			scope, id string
			spent     float64
		}{"agent", agent, agentSpent})
	}
	if task != "" {
		checks = append(checks, struct {
			scope, id string
			spent     float64
		}{"task", task, taskSpent})
	}
	for _, check := range checks {
		cap, err := backend.SpendingCap(ctx, check.scope, check.id)
		if err != nil {
			return err
		}
		amount, err := moneyValue(cap)
		if err != nil {
			return err
		}
		if amount > 0 && check.spent >= amount {
			return agentError(409, "at_cap", fmt.Sprintf("%s cap reached · $%.2f spent of $%s · raise the cap before continuing", check.scope, check.spent, cap.USD))
		}
	}
	return nil
}
