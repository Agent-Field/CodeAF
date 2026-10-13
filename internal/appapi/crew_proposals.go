package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// Proposal work goes through the source agent's normal engine, routing and
// ledger. Nothing in the app chooses a model or calls a provider directly.
func (s *Server) proposeHelpers(w http.ResponseWriter, r *http.Request, a *AgentRegistry) {
	var body ExpectRequest
	if !decodeCrew(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var record agentRecord
	var live *agentLive
	var stream <-chan session.Event
	var generation uint64
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	err := a.disk(func(d *agentDisk) error {
		var ok bool
		record, ok = d.Records[id]
		if !ok {
			return errAgentMissing
		}
		agent := a.view(record)
		if err := crewAgentExpect(agent, body.Expect); err != nil {
			return err
		}
		if agent.CrewID != "" {
			return agentError(409, "conflict", "this agent is already a crew member")
		}
		if agent.State != "idle" {
			return agentError(409, "conflict", "wait until the agent is idle before bringing in helpers")
		}
		var err error
		live, err = a.connect(ctx, record)
		if err != nil {
			return err
		}
		prompt := `Propose helpers for our current work. Do not make a crew or start helpers. Return only a JSON object with name (1 to 40 characters), tint (iris, sage, sand, tide, rose or graphite), helpers (1 to 16 objects with role, model and budget:{usd:"2.00"}), budget:{usd:"20.00"}, optional ends_at (RFC3339). Every cap must fit the crew budget. Keep the proposal editable; I may keep this as one agent.`
		stream, err = live.conn.Engine.Submit(ctx, prompt)
		if err != nil {
			return agentError(409, "conflict", "the helper proposal was not accepted")
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
	if err != nil {
		crewFailure(w, err)
		return
	}
	var text strings.Builder
	complete := false
	for !complete {
		select {
		case <-ctx.Done():
			_ = live.conn.Engine.StopWork()
			_ = a.transitionState(id, "idle", live, generation)
			fail(w, 409, "conflict", "the helper proposal stopped before it was ready")
			return
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
			case session.EventConsentRequest, session.EventQuestion:
				_ = live.conn.Engine.StopWork()
				_ = a.transitionState(id, "needs_you", live, generation)
				fail(w, 409, "conflict", "answer the agent's question before bringing in helpers")
				return
			case session.EventError:
				_ = a.transitionState(id, "failed", live, generation)
				fail(w, 502, "not_allowed", "the helper proposal could not be made")
				return
			}
			if text.Len() > 32<<10 {
				_ = live.conn.Engine.StopWork()
				_ = a.transitionState(id, "idle", live, generation)
				fail(w, 502, "bad_input", "the helper proposal was too large")
				return
			}
		}
	}
	_ = a.transitionState(id, "idle", live, generation)
	var proposal HelperProposal
	raw := strings.TrimSpace(text.String())
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	if json.Unmarshal([]byte(raw), &proposal) != nil || !validHelperProposal(proposal, a.now()) {
		fail(w, 502, "bad_input", "the agent did not return a valid helper proposal")
		return
	}
	proposal.ID = teams.NewID()
	a.mu.Lock()
	err = a.disk(func(d *agentDisk) error {
		record = d.Records[id]
		if record.Agent.CrewID != "" || record.Agent.State == "closed" || record.Agent.State == "archived" {
			return agentError(409, "stale", "the agent changed while proposing helpers")
		}
		return nil
	}, false)
	a.mu.Unlock()
	if err == nil {
		err = crewProposals(a.home, func(records map[string]crewProposalRecord) error {
			for key, p := range records {
				if a.now().Sub(p.Made) > 24*time.Hour {
					delete(records, key)
				}
			}
			if len(records) >= 128 {
				return agentError(409, "conflict", "too many helper proposals are waiting")
			}
			records[proposal.ID] = crewProposalRecord{Proposal: proposal, AgentID: id, Goal: record.Agent.Name, Version: record.Agent.Version, Made: a.now().UTC()}
			return nil
		}, true)
	}
	if err != nil {
		crewFailure(w, err)
		return
	}
	_, _ = s.Events().Publish(EventAskCreated, AskEvent{Ask: proposal.ID, Agent: id, Kind: "proposal"})
	write(w, 200, proposal)
}
func validHelperProposal(p HelperProposal, now time.Time) bool {
	if !crewName(p.Name) || crewTintIndex(p.Tint) < 0 || len(p.Helpers) == 0 || len(p.Helpers) > 16 {
		return false
	}
	budget, err := crewCents(p.Budget)
	if err != nil {
		return false
	}
	for _, h := range p.Helpers {
		cap, err := crewCents(h.Budget)
		if err != nil || cap > budget || !crewName(h.Role) || strings.TrimSpace(h.Model) == "" {
			return false
		}
	}
	return p.EndsAt == nil || p.EndsAt.After(now)
}
