package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// Native managers write closing packets independently of HTTP. Sampling those
// packets makes report availability durable and publishes each report once.
func (s *Server) RefreshCrewWork(ctx context.Context) error {
	a := s.options.Agents
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return nil
	}
	var ready []string
	var asks []Ask
	err := a.disk(func(d *agentDisk) error {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		type update struct {
			revision uint64
			report   *CrewReport
			packet   string
			ask      *Ask
		}
		updates := map[string]update{}
		for _, t := range f.Teams {
			if t.Closed() || t.Manager == "" || t.Root {
				continue
			}
			meta, err := readCrewMeta(t)
			if err != nil {
				return err
			}
			work, err := readCrewWork(t)
			if err != nil {
				return err
			}
			packets, err := teams.Packets(a.home, t.ID)
			if err != nil {
				return err
			}
			u := update{revision: crewRevision(t)}
			for _, p := range packets {
				if p.Origin == t.ID && p.Kind == teams.PacketClosing && p.Report != nil && p.Waiting() && p.ID != work.ReportPacket {
					view, _, err := crewView(f, t, d, a)
					if err != nil {
						return err
					}
					report := reportFromPacket(p, packets, view.Spent)
					u.report = &report
					u.packet = p.ID
				}
			}
			if meta.EndsAt != nil && !meta.EndsAt.After(a.now()) && work.EndAsk == nil && meta.State == "active" && u.report == nil {
				u.ask = &Ask{ID: teams.NewID(), Kind: "question", Source: SourceRef{Kind: "crew", ID: t.ID}, Path: []SourceRef{{Kind: "crew", ID: t.ID}}, Title: t.Name + " reached its end date. Wrap up?", State: "waiting", Options: []AskOption{{ID: "wrap_up", Label: "Wrap up"}, {ID: "keep_going", Label: "Keep going"}}, Before: []DisplayEntry{{Kind: "notice", Text: "The crew reached its end date"}, {Kind: "notice", Text: "Finish crew keeps its report"}}}
			}
			if u.report != nil || u.ask != nil {
				updates[t.ID] = u
			}
		}
		if err = s.sampleCrewLogs(a.home, f); err != nil {
			return err
		}
		if len(updates) == 0 {
			return nil
		}
		return teams.Update(a.home, func(current *teams.File) error {
			for cid, u := range updates {
				i := teams.Index(current.Teams, cid)
				if i < 0 || current.Teams[i].Closed() {
					continue
				}
				t := &current.Teams[i]
				if crewRevision(*t) != u.revision {
					continue
				}
				meta, err := readCrewMeta(*t)
				if err != nil {
					return err
				}
				work, err := readCrewWork(*t)
				if err != nil {
					return err
				}
				if u.report != nil && work.ReportPacket != u.packet {
					meta.Report = u.report
					meta.State = "report_ready"
					work.ReportPacket = u.packet
					ready = append(ready, cid)
					if err = current.ClearWrap(cid); err != nil {
						return err
					}
				}
				if u.ask != nil && work.EndAsk == nil && meta.State == "active" && meta.EndsAt != nil && !meta.EndsAt.After(a.now()) {
					work.EndAsk = u.ask
					asks = append(asks, *u.ask)
				}
				if err = writeCrewMeta(t, meta); err != nil {
					return err
				}
				if err = writeCrewWork(t, work); err != nil {
					return err
				}
			}
			return nil
		})
	}, false)
	if err != nil {
		return err
	}
	for _, cid := range ready {
		_, _ = s.Events().Publish(EventReportReady, ReportReadyEvent{Crew: cid})
		s.crewChanged(cid, "state", "report")
	}
	for _, ask := range asks {
		_, _ = s.Events().Publish(EventAskCreated, AskEvent{Ask: ask.ID, Kind: ask.Kind})
	}
	return nil
}

// CrewWorkAsks is the read-only waiting set for the asks and Inbox lanes.
// An expired end date asks once and never silently closes a crew.
func (a *AgentRegistry) CrewWorkAsks() ([]Ask, error) {
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
		work, err := readCrewWork(t)
		if err != nil {
			return nil, err
		}
		if work.EndAsk != nil {
			out = append(out, *work.EndAsk)
		}
	}
	return out, nil
}

func (s *Server) ensureCrewReport(ctx context.Context, cid string, expect *AgentExpect) (uint64, error) {
	a := s.options.Agents
	if err := s.RefreshCrewWork(ctx); err != nil {
		return 0, err
	}
	var version uint64
	var record agentRecord
	var live *agentLive
	var stream <-chan session.Event
	var generation uint64
	var hasReport bool
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	a.mu.Lock()
	err := a.disk(func(d *agentDisk) error {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		t, ok := f.Team(cid)
		if !ok {
			return crewMissing()
		}
		meta, err := readCrewMeta(t)
		if err != nil {
			return err
		}
		if err = crewExpect(t, meta, expect); err != nil {
			return err
		}
		if t.Closed() {
			return agentError(409, "conflict", "the crew is already closed")
		}
		if err = s.checkCrewOpenItems(ctx, f, t, d, a); err != nil {
			return err
		}
		version = crewRevision(t)
		if meta.Report != nil {
			hasReport = true
			return nil
		}
		id := crewAgentID(d, t.Manager)
		record, ok = d.Records[id]
		if !ok {
			return errAgentMissing
		}
		if a.view(record).State != "idle" {
			return agentError(409, "conflict", "wait for the manager to be idle or wrap up first")
		}
		live, err = a.connect(ctx, record)
		if err != nil {
			return err
		}
		prompt := `Write the final crew report from the crew log, your work and decisions. Do not start new work or close the crew. Return only JSON: {"shipped":"what shipped","decisions":["decisions the person made"],"files":[{"path":"file","state":"changed","added":0,"removed":0}],"left_open":[{"text":"remaining work","hand_off":{"kind":"task","id":"id"}}]}. Do not invent facts or hand-offs. The binary supplies the cost and Markdown.`
		stream, err = live.conn.Engine.Submit(ctx, prompt)
		if err != nil {
			return err
		}
		live.running = true
		live.generation++
		generation = live.generation
		record.Agent.State = "running"
		record.Agent.Version++
		d.Records[id] = record
		return nil
	}, true)
	a.mu.Unlock()
	if err != nil || hasReport {
		return version, err
	}
	var text strings.Builder
	complete := false
	for !complete {
		select {
		case <-ctx.Done():
			_ = live.conn.Engine.StopWork()
			_ = a.transitionState(record.Agent.ID, "idle", live, generation)
			return 0, agentError(409, "conflict", "the manager stopped before its report was ready")
		case event, ok := <-stream:
			if !ok {
				complete = true
				break
			}
			switch event.Kind {
			case session.EventTextDelta:
				text.WriteString(event.Text)
			case session.EventRetrying:
				text.Reset()
			case session.EventTurnDone:
				complete = true
			case session.EventError:
				_ = live.conn.Engine.StopWork()
				_ = a.transitionState(record.Agent.ID, "failed", live, generation)
				return 0, agentError(502, "not_allowed", "the manager could not write its report")
			case session.EventQuestion, session.EventConsentRequest:
				_ = live.conn.Engine.StopWork()
				_ = a.transitionState(record.Agent.ID, "needs_you", live, generation)
				return 0, agentError(409, "conflict", "answer the manager's question before finishing")
			}
			if text.Len() > 128<<10 {
				_ = live.conn.Engine.StopWork()
				_ = a.transitionState(record.Agent.ID, "idle", live, generation)
				return 0, agentError(502, "bad_input", "the manager's report was too large")
			}
		}
	}
	_ = a.transitionState(record.Agent.ID, "idle", live, generation)
	raw := strings.TrimSpace(text.String())
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var report CrewReport
	if json.Unmarshal([]byte(raw), &report) != nil || strings.TrimSpace(report.Shipped) == "" {
		return 0, agentError(502, "bad_input", "the manager did not return a valid report")
	}
	if report.Decisions == nil {
		report.Decisions = []string{}
	}
	if report.Files == nil {
		report.Files = []FileChange{}
	}
	if report.LeftOpen == nil {
		report.LeftOpen = []ReportOpenItem{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	err = a.disk(func(d *agentDisk) error {
		return teams.Update(a.home, func(f *teams.File) error {
			i := teams.Index(f.Teams, cid)
			if i < 0 {
				return crewMissing()
			}
			t := &f.Teams[i]
			if t.Closed() || crewRevision(*t) != version {
				return agentError(409, "stale", "the crew changed while its report was written; read it again")
			}
			if a.live[record.Agent.ID] != live || live.generation != generation {
				return agentError(409, "stale", "the manager changed while its report was written")
			}
			meta, err := readCrewMeta(*t)
			if err != nil {
				return err
			}
			view, _, err := crewView(f, *t, d, a)
			if err != nil {
				return err
			}
			report.Cost = Money{USD: "0.00"}
			if view.Spent != nil {
				report.Cost = *view.Spent
			}
			report.Markdown = crewReportMarkdown(report)
			meta.Report = &report
			meta.State = "report_ready"
			if err = f.ClearWrap(cid); err != nil {
				return err
			}
			if err = writeCrewMeta(t, meta); err != nil {
				return err
			}
			version = crewRevision(*t)
			return nil
		})
	}, false)
	if err == nil {
		_, _ = s.Events().Publish(EventReportReady, ReportReadyEvent{Crew: cid})
		s.crewChanged(cid, "state", "report")
	}
	return version, err
}

// Pool spending includes each conversation once across descendants. A manager
// retained in its parent's roster must not be counted twice after delegation.
func crewPoolSpent(f *teams.File, t teams.Team, d *agentDisk, a *AgentRegistry) (int64, error) {
	total := int64(0)
	seen := map[string]bool{}
	for _, crew := range append([]teams.Team{t}, f.Descendants(t.ID)...) {
		meta, err := readCrewMeta(crew)
		if err != nil {
			return 0, err
		}
		if meta.RetiredSpent != nil {
			n, _ := crewCents(*meta.RetiredSpent)
			total += n
		}
		for _, m := range crew.Members {
			id := crewAgentID(d, m.Key)
			if seen[id] {
				continue
			}
			seen[id] = true
			record, ok := d.Records[id]
			if !ok {
				continue
			}
			spent := a.view(record).Spent
			if meta.Members[id].EndSpent != nil {
				spent = meta.Members[id].EndSpent
			}
			if spent == nil {
				continue
			}
			n, _ := crewCents(*spent)
			if start := meta.Members[id].StartSpent; start != nil {
				offset, _ := crewCents(*start)
				n -= offset
			}
			if n > 0 {
				total += n
			}
		}
	}
	return total, nil
}
func crewPoolMoney(cents int64) *Money {
	if cents <= 0 {
		return nil
	}
	return &Money{USD: fmt.Sprintf("%d.%02d", cents/100, cents%100)}
}
