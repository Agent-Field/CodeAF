package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// The ledger includes child engine processes, so its producer is observed at
// the home boundary. The synchronous method below is also the test clock door.
func (s *Server) startSpendingWatch() {
	if s.options.Agents == nil {
		return
	}
	s.SetInboxSource("spending", spendingInboxSource{s: s})
	if s.options.Agents.open == nil {
		return
	}
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			s.events.mu.Lock()
			changed, closed := s.events.changed, s.events.closed
			s.events.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-changed:
				continue
			case <-ticker.C:
				_ = s.RefreshSpending(context.Background())
			}
		}
	}()
}

type spendingAsk struct {
	At    time.Time `json:"at"`
	Ask   Ask       `json:"ask"`
	Scope string    `json:"scope"`
	Day   string    `json:"day"`
	Cap   Money     `json:"cap"`
	Owner string    `json:"owner"`
}

type spendingAskState struct {
	Next  uint64                 `json:"next"`
	Items map[string]spendingAsk `json:"items"`
}

// Early app launches stored a bare map; reading it does not discard waiting ids.
func readSpendingAskState(raw []byte) (spendingAskState, error) {
	var state spendingAskState
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Items == nil {
		state.Items = map[string]spendingAsk{}
		if err := json.Unmarshal(raw, &state.Items); err != nil {
			return state, err
		}
	}
	return state, nil
}

// SpendingBudgetAsks is the typed resource boundary for asks and Inbox. The
// waiting set is restart-safe; consumers never need this resource's disk layout.
func (s *Server) SpendingBudgetAsks() ([]Ask, error) {
	rows, err := s.spendingAskRows()
	result := []Ask{}
	for _, row := range rows {
		result = append(result, row.Ask)
	}
	return result, err
}
func (s *Server) spendingAskRows() ([]spendingAsk, error) {
	result := []spendingAsk{}
	err := resourceDisk(s.resourceHome(), "spending-asks", map[string]spendingAsk{}, func(raw []byte) (any, error) {
		held, err := readSpendingAskState(raw)
		if err != nil {
			return nil, err
		}
		for _, row := range held.Items {
			result = append(result, row)
		}
		return held, nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Ask.ID < result[j].Ask.ID })
	return result, err
}

// RefreshSpending publishes measured changes and maintains one budget question
// per reached scope. Raising a cap or crossing midnight resolves the actual ask.
func (s *Server) RefreshSpending(ctx context.Context) error {
	backend := s.spendingBackend()
	rows, err := backend.SpendingRows(ctx)
	if err != nil {
		return err
	}
	now := s.resourceNow()
	day := now.Format("2006-01-02")
	totals := map[string]float64{"day:": 0}
	owners := map[string]spendingAsk{"day:": {Scope: "day", Ask: Ask{Source: SourceRef{Kind: "spending", ID: day}}}}
	for _, row := range rows {
		cost, err := measuredCost(row)
		if err != nil {
			return err
		}
		if row.At.After(now) {
			continue
		}
		if row.At.In(now.Location()).Format("2006-01-02") == day {
			totals["day:"] += cost
			if row.Who.Kind == "agent" && row.Who.ID != "" {
				key := "agent:" + row.Who.ID
				totals[key] += cost
				owners[key] = spendingAsk{Scope: "agent", Owner: row.Who.ID, Ask: Ask{Source: SourceRef{Kind: "agent", ID: row.Who.ID}}}
			}
		}
		if row.Who.Kind == "agent" && row.Who.ID != "" && strings.HasPrefix(row.What, "task:") {
			id := config.AppTaskCapID(row.Who.ID, strings.TrimPrefix(row.What, "task:"))
			key := "task:" + id
			totals[key] += cost
			owners[key] = spendingAsk{Scope: "task", Owner: id, Ask: Ask{Source: SourceRef{Kind: "task", ID: id}}}
		}
	}
	active := map[string]spendingAsk{}
	for key, total := range totals {
		row := owners[key]
		cap, err := backend.SpendingCap(ctx, row.Scope, row.Owner)
		if err != nil {
			// A measured historical agent may no longer be in this registry.
			if row.Scope != "day" {
				continue
			}
			return err
		}
		usd, err := moneyValue(cap)
		if err != nil {
			return err
		}
		if usd <= 0 || total < usd {
			continue
		}
		row.Day, row.Cap = day, cap
		if row.Scope == "task" {
			row.Day = ""
		}
		row.Ask = Ask{Kind: "budget", Source: row.Ask.Source, Path: []SourceRef{row.Ask.Source}, State: "waiting", Title: fmt.Sprintf("%s cap reached · $%.2f spent of $%s", row.Scope, total, cap.USD), Options: []AskOption{{ID: "raise", Label: "Raise the cap"}, {ID: "stop", Label: "Stop"}}, Before: []DisplayEntry{{Kind: "notice", Text: "Work paused at its cap"}, {Kind: "notice", Text: "Raise the cap before continuing"}}}
		active[key] = row
	}
	err = resourceDisk(s.resourceHome(), "spending-asks", map[string]spendingAsk{}, func(raw []byte) (any, error) {
		state, err := readSpendingAskState(raw)
		if err != nil {
			return nil, err
		}
		old := state.Items
		for key, row := range active {
			if prev, ok := old[key]; ok && prev.Day == row.Day && prev.Cap == row.Cap {
				row.Ask.ID, row.At = prev.Ask.ID, prev.At
			} else {
				state.Next++
				row.At = now
				row.Ask.ID = fmt.Sprintf("budget:%d:%s", state.Next, key)
			}
			active[key] = row
		}
		for key, row := range old {
			if next, ok := active[key]; !ok || next.Ask.ID != row.Ask.ID {
				if s.inbox == nil {
					_, _ = s.events.Publish(EventInboxResolved, InboxResolvedEvent{Item: row.Ask.ID, How: "in_place", By: "you", At: now})
				}
				_, _ = s.events.Publish(EventAskResolved, AskEvent{Ask: row.Ask.ID, Kind: "budget", How: "raised_or_reset"})
			}
		}
		for key, row := range active {
			if prev, ok := old[key]; ok && prev.Ask.ID == row.Ask.ID {
				continue
			}
			_, _ = s.events.Publish(EventAskCreated, AskEvent{Ask: row.Ask.ID, Kind: "budget", Agent: row.Owner})
			if s.inbox == nil {
				_, _ = s.events.Publish(EventInboxAdded, InboxAddedEvent{Item: row.Ask.ID, Kind: "budget"})
			}
		}
		state.Items = active
		return state, nil
	})
	if err != nil {
		return err
	}
	return s.PublishSpending(ctx, "")
}

// TaskCapID is shared with task resources because native task numbers are local.
func TaskCapID(agent, task string) string { return config.AppTaskCapID(agent, task) }
