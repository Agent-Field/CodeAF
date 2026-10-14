package appapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var crewContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews", Request: "", Response: "CrewsResponse"}, handle: (*Server).crewHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}", Request: "", Response: "CrewResponse"}, handle: (*Server).crewHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/bring-in-helpers", Request: "ExpectRequest", Response: "HelperProposal"}, handle: (*Server).crewHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews", Request: "CreateCrewRequest", Response: "Crew"}, handle: (*Server).crewHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/crews/{cid}", Request: "PatchCrewRequest", Response: "Crew"}, handle: (*Server).crewHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/replace-manager", Request: "ReplaceManagerRequest", Response: "Crew"}, handle: (*Server).crewHandler},
}

// Crew reads and writes share the native team store and its cross-process lock.
func (s *Server) crewHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.serveCrew)(w, r)
}

func (s *Server) serveCrew(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	if r.PathValue("id") != "" {
		s.proposeHelpers(w, r, a)
		return
	}
	cid := r.PathValue("cid")
	if r.Method == "GET" {
		if err := s.RefreshCrewWork(r.Context()); err != nil {
			crewFailure(w, err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		err := a.disk(func(d *agentDisk) error {
			f, err := teams.Snapshot(a.home)
			if err != nil {
				return err
			}
			if cid == "" {
				rows := []Crew{}
				for _, t := range f.Teams {
					if t.Manager == "" || t.Root {
						continue
					}
					c, _, err := crewView(f, t, d, a)
					if err != nil {
						return err
					}
					if strings.Contains(strings.ToLower(c.Name+" "+c.Goal), strings.ToLower(r.URL.Query().Get("q"))) {
						rows = append(rows, c)
					}
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
				write(w, 200, CrewsResponse{rows})
				return nil
			}
			t, ok := f.Team(cid)
			if !ok {
				return crewMissing()
			}
			c, members, err := crewView(f, t, d, a)
			if err != nil {
				return err
			}
			w.Header().Set("ETag", fmt.Sprintf("\"%d\"", crewRevision(t)))
			work, err := readCrewWork(t)
			if err != nil {
				return err
			}
			plan := []Task{}
			if manager, ok := d.Records[c.ManagerID]; ok {
				plan = a.turnSnapshot(manager).Tasks.Tasks
			}
			write(w, 200, CrewResponse{Crew: c, Tree: members, Plan: plan, Items: work.Items})
			return nil
		}, false)
		if err != nil {
			crewFailure(w, err)
		}
		return
	}
	if cid == "" {
		s.createCrew(w, r, a)
		return
	}
	var patch PatchCrewRequest
	var replace ReplaceManagerRequest
	replacing := strings.HasSuffix(r.URL.Path, "/replace-manager")
	if replacing {
		if !decodeCrew(w, r, &replace) {
			return
		}
	} else {
		if !decodeCrew(w, r, &patch) {
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	var result Crew
	err := a.disk(func(d *agentDisk) error {
		return teams.Update(a.home, func(f *teams.File) error {
			i := teams.Index(f.Teams, cid)
			if i < 0 {
				return crewMissing()
			}
			t := &f.Teams[i]
			meta, err := readCrewMeta(*t)
			if err != nil {
				return err
			}
			expect := patch.Expect
			if replacing {
				expect = replace.Expect
			}
			if err := crewExpect(*t, meta, expect); err != nil {
				return err
			}
			if t.Closed() {
				return agentError(409, "conflict", "reopen the crew before changing it")
			}
			if replacing {
				if (replace.MemberID == "") == (replace.Role == "") {
					return crewBad("choose a member_id or a role for the manager")
				}
				mid := replace.MemberID
				if replace.Role != "" {
					role, err := s.crewRole(r.Context(), f, replace.Role)
					if err != nil {
						return err
					}
					for id, member := range meta.Members {
						if member.RoleID == role.ID {
							mid = id
							break
						}
					}
					if mid == "" {
						m, err := s.addCrewMember(r.Context(), a, d, f, t, &meta, CreateMemberRequest{NewRole: &NewRole{Name: role.Name, Instructions: role.Instructions, Model: role.Model, Effort: role.Effort}}, role.ID)
						if err != nil {
							return err
						}
						mid = m.AgentID
					}
				}
				record, ok := d.Records[mid]
				if !ok || !t.Holds(record.SessionFile) {
					return agentError(404, "not_found", "the new manager is not a member")
				}
				old := t.Manager
				if err := f.SetManager(cid, record.SessionFile); err != nil {
					return agentError(409, "conflict", err.Error())
				}
				for id, m := range meta.Members {
					if m.ReportsTo == crewAgentID(d, old) || id == mid {
						m.ReportsTo = mid
						if id == mid {
							m.ReportsTo = ""
						}
						meta.Members[id] = m
					}
				}
				meta.ManagerID = mid
			} else {
				if err := applyCrewPatch(t, &meta, patch); err != nil {
					return err
				}
				if err := checkCrewTreeBudgets(f, *t, meta); err != nil {
					return err
				}
			}
			meta.Version++
			if err := writeCrewMeta(t, meta); err != nil {
				return err
			}
			result, _, err = crewView(f, *t, d, a)
			return err
		})
	}, true)
	if err != nil {
		crewFailure(w, err)
		return
	}
	s.crewChanged(result.ID, "manager_id", "name", "tint", "goal", "budget", "ends_at")
	if replacing {
		s.crewDirective(a, result.ID, result.ManagerID, "You now manage this crew. Read the previous manager’s notes and the crew log before continuing.")
	}
	write(w, 200, result)
}

func decodeCrew(w http.ResponseWriter, r *http.Request, body any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return false
	}
	for _, value := range object {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(w, 422, "bad_input", "omit unset fields instead of sending null")
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(body) != nil {
		fail(w, 422, "bad_input", "invalid action fields")
		return false
	}
	return true
}

func crewBad(message string) error { return agentError(422, "bad_input", message) }
func crewMissing() error           { return agentError(404, "not_found", "no such crew") }
func crewFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, errAgentMissing) {
		fail(w, 404, "not_found", "no such agent")
		return
	}
	var reply *agentReplyError
	if errors.As(err, &reply) {
		fail(w, reply.status, reply.reason, reply.sentence)
		return
	}
	if errors.Is(err, teams.ErrBusy) {
		fail(w, 409, "conflict", "the crew is being changed; try again")
		return
	}
	fail(w, 500, "not_allowed", "the crew action could not be saved")
}
func crewCents(m Money) (int64, error) {
	if !agentMoney.MatchString(m.USD) || m.USD == "0.00" {
		return 0, crewBad("budget and caps need positive USD with two decimal places")
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(m.USD, ".", ""), 10, 64)
	return n, err
}

var crewTints = spec.Tints

func crewTintIndex(tint string) int {
	for i, t := range crewTints {
		if t == tint {
			return i
		}
	}
	return -1
}
func crewName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 40 && !strings.ContainsAny(name, "\r\n\x00")
}
func applyCrewPatch(t *teams.Team, m *crewMeta, p PatchCrewRequest) error {
	if p.Name != nil {
		if !crewName(*p.Name) {
			return crewBad("crew name must be one line of 1 to 40 characters")
		}
		t.Name = strings.TrimSpace(*p.Name)
	}
	if p.Tint != nil {
		i := crewTintIndex(*p.Tint)
		if i < 0 {
			return crewBad("choose one of the six crew tints")
		}
		m.Tint = *p.Tint
		t.SetHue(teams.HueSpec{Hue: float64(i) * 60})
	}
	if p.Goal != nil {
		if len(*p.Goal) > 8000 {
			return crewBad("goal must be at most 8000 bytes")
		}
		m.Goal = *p.Goal
	}
	if p.Budget != nil {
		cents, err := crewCents(*p.Budget)
		if err != nil {
			return err
		}
		for _, member := range m.Members {
			if member.Cap != nil {
				cap, err := crewCents(*member.Cap)
				if err != nil {
					return err
				}
				if cap > cents {
					return crewBad("member caps cannot exceed the crew budget")
				}
			}
		}
		m.Budget = *p.Budget
		m.BudgetAsk = nil
		for id, member := range m.Members {
			if member.State == "stopped" {
				member.State = "idle"
			}
			m.Members[id] = member
		}
	}
	if p.EndsAt != nil {
		if !p.EndsAt.After(t.Made) {
			return crewBad("end date must be after the crew was made")
		}
		m.EndsAt = p.EndsAt
	}
	return nil
}

// The complete native document participates in stale checks, including TUI edits.
func crewRevision(t teams.Team) uint64 {
	raw, _ := json.Marshal(t)
	hash := sha256.Sum256(raw)
	return binary.BigEndian.Uint64(hash[:8])
}
func crewExpect(t teams.Team, m crewMeta, e *AgentExpect) error {
	if e == nil {
		return nil
	}
	state := m.State
	if state == "" {
		state = "active"
	}
	if t.Closed() && state != "archived" {
		state = "closed"
	}
	if e.Version != nil && *e.Version != crewRevision(t) || e.State != nil && *e.State != state {
		return agentError(409, "stale", "the crew changed; read it again before acting")
	}
	return nil
}
func (s *Server) crewChanged(cid string, fields ...string) {
	_, _ = s.Events().Publish(EventCrewChanged, CrewChangedEvent{cid, fields})
}

// Role storage belongs to A10B; its reader can be supplied without coupling handlers.
type CrewRoleSource interface {
	ReadCrewRole(context.Context, string) (CrewRole, error)
}

func (s *Server) crewRole(ctx context.Context, f *teams.File, id string) (CrewRole, error) {
	if s.options.CrewRoles != nil {
		return s.options.CrewRoles.ReadCrewRole(ctx, id)
	}
	if s.options.Agents != nil {
		role, err := (storedCrewRoles{home: s.options.Agents.home}).ReadCrewRole(ctx, id)
		if err == nil {
			return role, nil
		}
		var reply *agentReplyError
		if !errors.As(err, &reply) || reply.status != 404 {
			return CrewRole{}, err
		}
	}
	for _, t := range f.Teams {
		meta, err := readCrewMeta(t)
		if err != nil {
			return CrewRole{}, err
		}
		for _, m := range meta.Members {
			if m.RoleID == id {
				return CrewRole{ID: id, Name: m.Role.Name, Instructions: m.Role.Instructions, Model: m.Role.Model, Effort: m.Role.Effort}, nil
			}
		}
	}
	return CrewRole{}, agentError(404, "not_found", "no such role")
}

// New agents keep their own transcript; team membership is a reference, never a merge.
func (s *Server) createCrew(w http.ResponseWriter, r *http.Request, a *AgentRegistry) {
	var body CreateCrewRequest
	if !decodeCrew(w, r, &body) {
		return
	}
	if body.From != "agents" && body.From != "proposal" {
		crewFailure(w, crewBad("from must be proposal or agents"))
		return
	}
	var proposed crewProposalRecord
	if body.From == "proposal" {
		if len(body.AgentIDs) > 0 || body.Manager != "" || len(body.Members) > 0 || body.Name != "" || body.Goal != "" {
			crewFailure(w, crewBad("edit a helper proposal through edits"))
			return
		}
		var err error
		proposed, err = loadCrewProposal(a.home, body.ProposalID)
		if err != nil {
			crewFailure(w, err)
			return
		}
		body.AgentIDs = []string{proposed.AgentID}
		body.Manager = proposed.AgentID
		body.Name = proposed.Proposal.Name
		body.Goal = proposed.Goal
		for _, h := range proposed.Proposal.Helpers {
			cap := h.Budget
			body.Members = append(body.Members, CreateMemberRequest{NewRole: &NewRole{Name: h.Role, Instructions: h.Role, Model: h.Model, Effort: config.AppDefaultEffort}, Cap: &cap})
		}
	} else {
		if len(body.AgentIDs) < 2 {
			crewFailure(w, crewBad("choose at least two agents"))
			return
		}
		if body.Manager == "" {
			crewFailure(w, crewBad("choose a new manager or one of the selected agents"))
			return
		}
	}
	if !crewName(body.Name) || strings.TrimSpace(body.Goal) == "" || len(body.Goal) > 8000 {
		crewFailure(w, crewBad("give the crew a name of 1 to 40 characters and a goal of at most 8000 bytes"))
		return
	}
	if len(body.AgentIDs)+len(body.Members) > 64 {
		crewFailure(w, crewBad("a crew may have at most 64 members"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	var result Crew
	var handover string
	err := a.disk(func(d *agentDisk) error {
		return teams.Update(a.home, func(f *teams.File) error {
			if body.From == "proposal" {
				for _, t := range f.Teams {
					m, err := readCrewMeta(t)
					if err != nil {
						return err
					}
					if m.ProposalID == body.ProposalID {
						result, _, err = crewView(f, t, d, a)
						return err
					}
				}
			}
			if proposed.ParentID != "" {
				parent, ok := f.Team(proposed.ParentID)
				if !ok {
					return crewMissing()
				}
				pm, err := readCrewMeta(parent)
				if err != nil {
					return err
				}
				if parent.Closed() || pm.State != "active" || crewRevision(parent) != proposed.ParentVersion {
					return agentError(409, "stale", "the parent crew changed; ask for a new helper proposal")
				}
				if len(f.Ancestors(parent.ID))+1 >= config.DefaultTeamsDepthLimit {
					return agentError(409, "conflict", "Crews nest 3 deep at most.")
				}
				source, ok := d.Records[proposed.AgentID]
				if !ok || source.Agent.CrewID != parent.ID || !parent.Holds(source.SessionFile) || source.SessionFile == parent.Manager {
					return agentError(409, "stale", "the proposed manager left its parent crew")
				}
			}
			seen := map[string]bool{}
			for _, id := range body.AgentIDs {
				if seen[id] {
					return crewBad("select each agent once")
				}
				seen[id] = true
				record, ok := d.Records[id]
				if !ok {
					return errAgentMissing
				}
				if body.From == "proposal" && (record.Agent.Version != proposed.Version || a.now().Sub(proposed.Made) > 24*time.Hour) {
					return agentError(409, "stale", "the helper proposal changed; ask for a new one")
				}
				if err := crewAgentExpect(a.view(record), body.Expect); err != nil {
					return err
				}
				if record.Agent.State == "closed" || record.Agent.State == "archived" {
					return agentError(409, "conflict", "reopen the agent before adding it to a crew")
				}
			}
			if body.Manager != "new" && !seen[body.Manager] {
				return crewBad("the manager must be one of the selected agents or new")
			}
			tint := crewTints[len(f.Teams)%len(crewTints)]
			used := map[string]bool{}
			for _, t := range f.Teams {
				m, err := readCrewMeta(t)
				if err != nil {
					return err
				}
				used[m.Tint] = true
			}
			for _, word := range crewTints {
				if !used[word] {
					tint = word
					break
				}
			}
			meta := crewMeta{Crew: Crew{Tint: tint, Goal: body.Goal, Budget: defaultCrewBudget(), Share: config.DefaultTeamsSubSharePct, State: "active", CreatedFrom: body.From}, Members: map[string]crewMemberMeta{}, Version: 1, ProposalID: body.ProposalID}
			if body.From == "proposal" {
				meta.CreatedFrom = "agent"
				meta.Tint = proposed.Proposal.Tint
				meta.Budget = proposed.Proposal.Budget
				meta.EndsAt = proposed.Proposal.EndsAt
			}
			up := true
			t := teams.Team{ID: teams.NewID(), Parent: proposed.ParentID, Name: body.Name, Made: a.now().UTC(), Settings: teams.Settings{QuestionsUp: &up}}
			t.SetHue(teams.HueSpec{Hue: float64(crewTintIndex(meta.Tint)) * 60})
			if body.Edits != nil {
				if body.Edits.Expect != nil {
					return crewBad("use the top-level expect for creation")
				}
				if err := applyCrewPatch(&t, &meta, *body.Edits); err != nil {
					return err
				}
			}
			if proposed.ParentID != "" {
				parent, _ := f.Team(proposed.ParentID)
				pm, err := readCrewMeta(parent)
				if err != nil {
					return err
				}
				pool, _ := crewCents(pm.Budget)
				budget, err := crewCents(meta.Budget)
				if err != nil {
					return err
				}
				if budget > pool*int64(config.DefaultTeamsSubSharePct)/100 {
					return crewBad("sub-crew budget cannot exceed its 50% parent share")
				}
			}
			// Resolve and validate every requested member before an engine is opened.
			for _, member := range body.Members {
				if err := s.validateCrewMember(r.Context(), f, meta, member); err != nil {
					return err
				}
				if member.ReportsTo != "" && member.ReportsTo != body.Manager {
					return crewBad("members report to this crew's manager")
				}
				if member.AgentID != "" {
					if seen[member.AgentID] {
						return agentError(409, "conflict", "an agent can join a crew only once")
					}
					seen[member.AgentID] = true
					record, ok := d.Records[member.AgentID]
					if !ok {
						return errAgentMissing
					}
					if record.Agent.State == "closed" || record.Agent.State == "archived" {
						return agentError(409, "conflict", "reopen the agent before adding it to a crew")
					}
				}
			}
			f.Teams = append(f.Teams, t)
			target := &f.Teams[len(f.Teams)-1]
			for _, id := range body.AgentIDs {
				record := d.Records[id]
				if record.Agent.CrewID != "" && proposed.ParentID == "" {
					role := NewRole{Name: record.Agent.Name, Model: record.Agent.Model, Effort: record.Agent.Effort, Instructions: "Continue the role of " + record.Agent.Name + " for this crew."}
					m, err := s.addCrewMember(r.Context(), a, d, f, target, &meta, CreateMemberRequest{NewRole: &role}, record.Agent.RoleID)
					if err != nil {
						return err
					}
					if id == body.Manager {
						meta.ManagerID = m.AgentID
					}
				} else {
					if err := f.AddMember(target.ID, teams.Member{Key: record.SessionFile, File: record.SessionFile, Where: record.Workspace, Word: record.Agent.Name, JoinedAt: a.now().UTC()}); err != nil {
						return err
					}
					meta.Members[id] = crewMemberMeta{ReportsTo: "", StartSpent: a.view(record).Spent, RoleID: record.Agent.RoleID, Role: NewRole{Name: record.Agent.Name, Model: record.Agent.Model, Effort: record.Agent.Effort}, State: "idle"}
					m := meta.Members[id]
					if m.RoleID == "" {
						m.RoleID = teams.NewID()
						meta.Members[id] = m
					}
					if id == body.Manager {
						meta.ManagerID = id
					}
				}
			}
			if body.Manager == "new" {
				role := NewRole{Name: body.Name, Instructions: body.Goal, Effort: config.AppDefaultEffort}
				m, err := s.addCrewMember(r.Context(), a, d, f, target, &meta, CreateMemberRequest{NewRole: &role}, "")
				if err != nil {
					return err
				}
				meta.ManagerID = m.AgentID
			}
			manager := d.Records[meta.ManagerID]
			if err := f.SetManager(target.ID, manager.SessionFile); err != nil {
				return agentError(409, "conflict", err.Error())
			}
			for _, member := range body.Members {
				if member.ReportsTo == "" {
					member.ReportsTo = meta.ManagerID
				}
				if _, err := s.addCrewMember(r.Context(), a, d, f, target, &meta, member, ""); err != nil {
					return err
				}
			}
			for id, m := range meta.Members {
				if id != meta.ManagerID && m.ReportsTo == "" {
					m.ReportsTo = meta.ManagerID
				}
				meta.Members[id] = m
				record := d.Records[id]
				record.Agent.CrewID = target.ID
				record.Agent.RoleID = m.RoleID
				record.Agent.Version++
				d.Records[id] = record
			}
			if err := writeCrewMeta(target, meta); err != nil {
				return err
			}
			var viewErr error
			result, _, viewErr = crewView(f, *target, d, a)
			// Only short handover notes cross chats. The manager writes its own summary.
			var notes strings.Builder
			fmt.Fprintf(&notes, "Manage crew %s. Goal: %s. Start by summarizing each member's handover; do not merge histories.\n", target.Name, meta.Goal)
			for _, id := range body.AgentIDs {
				record := d.Records[id]
				fmt.Fprintf(&notes, "Member %s (%s): %s\n", id, record.Agent.Name, crewHandover(record))
			}
			handover = notes.String()
			return viewErr
		})
	}, true)
	if err != nil {
		crewFailure(w, err)
		return
	}
	_, _ = s.Events().Publish(EventCrewCreated, CrewChangedEvent{result.ID, []string{"members", "manager_id"}})
	s.publishCrewAgents(a, result.ID)
	s.deliverCrewInstructions(a, result.ID)
	if handover != "" {
		s.crewDirective(a, result.ID, result.ManagerID, handover)
	}
	write(w, 201, result)
}

func crewAgentExpect(agent Agent, e *AgentExpect) error {
	if e != nil && (e.State != nil && *e.State != agent.State || e.Version != nil && *e.Version != agent.Version) {
		return agentError(409, "stale", "the agent changed; read it again before acting")
	}
	return nil
}
func crewHandover(record agentRecord) string {
	rows := session.ReadTranscript(record.SessionFile).Entries
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == "assistant" && rows[i].Text != "" {
			text := []rune(rows[i].Text)
			return string(text[:min(len(text), 1200)])
		}
	}
	return record.Agent.Name
}
func (s *Server) publishCrewAgents(a *AgentRegistry, cid string) {
	_ = a.disk(func(d *agentDisk) error {
		for _, r := range d.Records {
			if r.Agent.CrewID == cid {
				a.events = s.events
				a.publish(EventAgentChanged, r.Agent, "crew_id", "role_id")
			}
		}
		return nil
	}, false)
}
func (s *Server) deliverCrewInstructions(a *AgentRegistry, cid string) {
	f, err := teams.Snapshot(a.home)
	if err != nil {
		return
	}
	t, ok := f.Team(cid)
	if !ok {
		return
	}
	meta, err := readCrewMeta(t)
	if err != nil {
		return
	}
	for id, m := range meta.Members {
		if id != meta.ManagerID && m.Role.Instructions != "" {
			s.crewDirectiveMember(a, cid, id, m.Role.Instructions)
		}
	}
}
func (s *Server) crewDirective(a *AgentRegistry, cid, mid, text string) {
	// Native directives are read by the session at its next step and wake its manager.
	var file string
	_ = a.disk(func(d *agentDisk) error { file = d.Records[mid].SessionFile; return nil }, false)
	if file != "" {
		_ = teams.AppendTraffic(a.home, cid, teams.Entry{Kind: teams.KindDirective, From: teams.FromYou, To: teams.ToManager, ToKey: file, Text: text})
	}
}
