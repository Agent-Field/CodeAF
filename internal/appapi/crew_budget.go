package appapi

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// The monitor enforces lifetime crew pools independently of the terminal's
// daily pools. Its producer method is synchronous so tests need no real sleeps.
func (s *Server) startCrewBudgetWatch() {
	if s.options.Agents == nil {
		return
	}
	ctx := s.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
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
			case <-ctx.Done():
				return
			case <-changed:
			case <-ticker.C:
			}
			s.options.Agents.mu.Lock()
			stopped := s.options.Agents.stopped
			s.options.Agents.mu.Unlock()
			if stopped {
				return
			}
			_ = s.EnforceCrewBudgets(ctx)
		}
	}()
}

// EnforceCrewBudgets stops at a cap and saves one question in the native
// document before publication. Repeated samples and concurrent app launches
// reuse the saved question; neither can produce a second card for this cap.
func (s *Server) EnforceCrewBudgets(ctx context.Context) error {
	a := s.options.Agents
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return nil
	}
	type crossing struct {
		cid, mid string
		spent    int64
		cap      Money
	}
	var hits []crossing
	var asks []Ask
	err := a.disk(func(d *agentDisk) error {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		for _, t := range f.Teams {
			meta, err := readCrewMeta(t)
			if err != nil {
				return err
			}
			if t.Closed() {
				continue
			}
			_, members, err := crewView(f, t, d, a)
			if err != nil {
				return err
			}
			total := int64(0)
			for _, m := range members {
				if m.Spent != nil {
					n, _ := crewCents(*m.Spent)
					total += n
				}
			}
			budget, _ := crewCents(meta.Budget)
			if total >= budget && budget > 0 {
				if meta.BudgetAsk == nil {
					hits = append(hits, crossing{cid: t.ID, spent: total, cap: meta.Budget})
				}
				continue
			}
			for _, m := range members {
				if m.Cap == nil || m.Spent == nil || meta.Members[m.AgentID].BudgetAsk != nil {
					continue
				}
				spent, _ := crewCents(*m.Spent)
				cap, _ := crewCents(*m.Cap)
				if spent >= cap {
					hits = append(hits, crossing{cid: t.ID, mid: m.AgentID, spent: spent, cap: *m.Cap})
				}
			}
		}
		if len(hits) == 0 {
			return nil
		}
		return teams.Update(a.home, func(current *teams.File) error {
			for _, hit := range hits {
				i := teams.Index(current.Teams, hit.cid)
				if i < 0 {
					continue
				}
				t := &current.Teams[i]
				if t.Closed() {
					continue
				}
				meta, err := readCrewMeta(*t)
				if err != nil {
					return err
				}
				if hit.mid == "" && meta.BudgetAsk != nil || hit.mid != "" && meta.Members[hit.mid].BudgetAsk != nil {
					continue
				}
				// A concurrent raise wins over a sample made before its write.
				cap := meta.Budget
				if hit.mid != "" {
					m := meta.Members[hit.mid]
					if m.Cap == nil {
						continue
					}
					cap = *m.Cap
				}
				if cap != hit.cap {
					continue
				}
				for _, native := range t.Members {
					id := crewAgentID(d, native.Key)
					if hit.mid != "" && id != hit.mid {
						continue
					}
					record, ok := d.Records[id]
					if !ok {
						continue
					}
					live, err := a.connect(ctx, record)
					if err != nil {
						return err
					}
					if err = live.conn.Engine.StopWork(); err != nil {
						return err
					}
					live.running = false
					live.generation++
					member := meta.Members[id]
					member.State = "stopped"
					meta.Members[id] = member
				}
				source := SourceRef{Kind: "crew", ID: hit.cid}
				name := t.Name
				if hit.mid != "" {
					source = SourceRef{Kind: "member", ID: hit.mid}
					name = d.Records[hit.mid].Agent.Name
				}
				path := []SourceRef{source}
				if source.Kind == "member" {
					path = append(path, SourceRef{Kind: "crew", ID: t.ID})
				}
				for _, parent := range current.Ancestors(t.ID) {
					path = append(path, SourceRef{Kind: "crew", ID: parent.ID})
				}
				ask := Ask{ID: teams.NewID(), Kind: "budget", Source: source, Path: path, Title: fmt.Sprintf("%s reached its $%s cap · $%d.%02d spent", name, hit.cap.USD, hit.spent/100, hit.spent%100), State: "waiting", Options: []AskOption{{ID: "raise", Label: "Raise the cap"}, {ID: "stop", Label: "Stop"}}, Before: []DisplayEntry{{Kind: "notice", Text: "Work stopped at its cap"}, {Kind: "notice", Text: "Your manager is asking what to do next"}}}
				if hit.mid == "" {
					meta.BudgetAsk = &ask
				} else {
					member := meta.Members[hit.mid]
					member.BudgetAsk = &ask
					meta.Members[hit.mid] = member
				}
				meta.Version++
				if err = writeCrewMeta(t, meta); err != nil {
					return err
				}
				asks = append(asks, ask)
			}
			return nil
		})
	}, false)
	if err != nil {
		return err
	}
	for _, ask := range asks {
		_, _ = s.Events().Publish(EventAskCreated, AskEvent{Ask: ask.ID, Kind: "budget"})
		cid := ask.Source.ID
		if ask.Source.Kind == "member" {
			for _, hit := range hits {
				if hit.mid == ask.Source.ID {
					cid = hit.cid
					break
				}
			}
		}
		s.crewChanged(cid, "members", "budget")
	}
	return nil
}

// CrewBudgetAsks gives asks and Inbox their typed waiting set without borrowing
// either resource's handler or inventing another question identity.
func (a *AgentRegistry) CrewBudgetAsks() ([]Ask, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Ask{}
	f, err := teams.Snapshot(a.home)
	if err != nil {
		return nil, err
	}
	for _, t := range f.Teams {
		if t.Closed() {
			continue
		}
		m, err := readCrewMeta(t)
		if err != nil {
			return nil, err
		}
		if m.BudgetAsk != nil {
			out = append(out, *m.BudgetAsk)
		}
		for _, member := range m.Members {
			if member.BudgetAsk != nil {
				out = append(out, *member.BudgetAsk)
			}
		}
	}
	return out, nil
}
