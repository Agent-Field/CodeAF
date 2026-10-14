package appapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var memberContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members", Request: "CreateMemberRequest", Response: "Member"}, handle: (*Server).memberHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/crews/{cid}/members/{mid}", Request: "PatchMemberRequest", Response: "Member"}, handle: (*Server).memberHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/pause", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/resume", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/remove", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/move", Request: "MoveMemberRequest", Response: "Member"}, handle: (*Server).memberHandler},
}

func (s *Server) memberHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.crewAppsAroundMembers(s.serveMember))(w, r)
}
func (s *Server) serveMember(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	cid, mid := r.PathValue("cid"), r.PathValue("mid")
	var add CreateMemberRequest
	var patch PatchMemberRequest
	var move MoveMemberRequest
	var action ExpectRequest
	verb := ""
	if mid != "" && r.Method == "POST" {
		parts := strings.Split(r.URL.Path, "/")
		verb = parts[len(parts)-1]
	}
	var body any = &action
	var expect *AgentExpect
	switch {
	case mid == "":
		body = &add
	case r.Method == "PATCH":
		body = &patch
	case verb == "move":
		body = &move
	}
	if !decodeCrew(w, r, body) {
		return
	}
	switch {
	case mid == "":
		expect = add.Expect
	case r.Method == "PATCH":
		expect = patch.Expect
	case verb == "move":
		expect = move.Expect
	default:
		expect = action.Expect
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	var result Member
	var destination string
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
			if err = crewExpect(*t, meta, expect); err != nil {
				return err
			}
			if t.Closed() {
				return agentError(409, "conflict", "reopen the crew before changing its members")
			}
			if mid == "" {
				if add.ReportsTo == "" {
					add.ReportsTo = crewAgentID(d, t.Manager)
				}
				result, err = s.addCrewMember(r.Context(), a, d, f, t, &meta, add, "")
				if err != nil {
					return err
				}
			} else {
				record, ok := d.Records[mid]
				if !ok || !t.Holds(record.SessionFile) {
					return agentError(404, "not_found", "no such member in this crew")
				}
				member := meta.Members[mid]
				manager := t.Manager == record.SessionFile
				if manager && (verb == "remove" || verb == "move") {
					return agentError(409, "conflict", "a crew must have a manager; use replace-manager")
				}
				switch {
				case r.Method == "PATCH":
					if patch.Cap != nil {
						cap, err := crewCents(*patch.Cap)
						if err != nil {
							return err
						}
						budget, err := crewCents(meta.Budget)
						if err != nil {
							return err
						}
						if cap > budget {
							return crewBad("member caps cannot exceed the crew budget")
						}
						member.Cap = patch.Cap
						member.BudgetAsk = nil
						if member.State == "stopped" {
							member.State = "idle"
						}
					}
					if patch.ReportsTo != nil {
						if manager && *patch.ReportsTo != "" {
							return crewBad("the manager reports through its parent crew")
						}
						if !manager && *patch.ReportsTo != crewAgentID(d, t.Manager) {
							return crewBad("members report to this crew's manager")
						}
						member.ReportsTo = *patch.ReportsTo
					}
					if patch.Instructions != nil {
						if len(*patch.Instructions) > 16000 || strings.TrimSpace(*patch.Instructions) == "" {
							return crewBad("instructions need 1 to 16000 bytes")
						}
						member.Role.Instructions = *patch.Instructions
					}
					if patch.Cap != nil {
						live, err := a.connect(r.Context(), record)
						if err != nil {
							return err
						}
						usd, _ := strconv.ParseFloat(patch.Cap.USD, 64)
						if member.StartSpent != nil {
							start, _ := strconv.ParseFloat(member.StartSpent.USD, 64)
							usd += start
						}
						if err = live.conn.Engine.SetSpendRail(usd); err != nil {
							return err
						}
					}
				case verb == "pause" || verb == "remove" || verb == "move":
					if verb == "move" {
						if move.CrewID == cid {
							return crewBad("choose another crew")
						}
						j := teams.Index(f.Teams, move.CrewID)
						if j < 0 {
							return crewMissing()
						}
						target := &f.Teams[j]
						if target.Closed() {
							return agentError(409, "conflict", "reopen the destination crew first")
						}
						if target.Holds(record.SessionFile) {
							return agentError(409, "conflict", "already in "+target.Name)
						}
						dst, err := readCrewMeta(*target)
						if err != nil {
							return err
						}
						if member.Cap != nil {
							cap, _ := crewCents(*member.Cap)
							budget, _ := crewCents(dst.Budget)
							if cap > budget {
								return crewBad("member caps cannot exceed the destination crew budget")
							}
						}
						for _, existing := range dst.Members {
							if member.RoleID != "" && existing.RoleID == member.RoleID {
								return agentError(409, "conflict", "already in "+target.Name)
							}
						}
						member.ReportsTo = crewAgentID(d, target.Manager)
						member.StartSpent = a.view(record).Spent
						dst.Members[mid] = member
						dst.Version++
						if err = f.AddMember(target.ID, teams.Member{Key: record.SessionFile, File: record.SessionFile, Where: record.Workspace, Word: record.Agent.Name, JoinedAt: a.now().UTC()}); err != nil {
							return err
						}
						if err = writeCrewMeta(target, dst); err != nil {
							return err
						}
						destination = target.ID
					}
					live, err := a.connect(r.Context(), record)
					if err != nil {
						return err
					}
					if err = live.conn.Engine.StopWork(); err != nil {
						return err
					}
					live.running = false
					live.generation++
					record.Agent.State = "idle"
					if verb == "pause" {
						member.State = "paused"
					} else {
						if err = f.RemoveMember(cid, record.SessionFile); err != nil {
							return err
						}
						retire := meta.Members[mid]
						current := a.view(record).Spent
						if current != nil {
							spent, _ := crewCents(*current)
							if retire.StartSpent != nil {
								start, _ := crewCents(*retire.StartSpent)
								spent -= start
							}
							prior := int64(0)
							if meta.RetiredSpent != nil {
								prior, _ = crewCents(*meta.RetiredSpent)
							}
							if spent > 0 {
								meta.RetiredSpent = crewPoolMoney(prior + spent)
							}
						}
						delete(meta.Members, mid)
						record.Agent.CrewID = destination
						if destination == "" {
							record.Agent.RoleID = ""
						}
					}
				case verb == "resume":
					if member.State == "stopped" {
						return agentError(409, "at_cap", "raise the member cap before resuming")
					}
					member.State = "idle"
				default:
					return crewBad("unknown member action")
				}
				if verb != "remove" && verb != "move" {
					meta.Members[mid] = member
				}
				record.Agent.Version++
				d.Records[mid] = record
				result = Member{CrewID: cid, AgentID: mid, RoleID: member.RoleID, IsManager: manager, ReportsTo: member.ReportsTo, Cap: member.Cap, State: member.State, AlsoIn: []string{}}
				if verb == "move" {
					result.CrewID = destination
				}
			}
			meta.Version++
			return writeCrewMeta(t, meta)
		})
	}, true)
	if err != nil {
		crewFailure(w, err)
		return
	}
	s.crewChanged(cid, "members")
	if destination != "" {
		s.crewChanged(destination, "members")
	}
	_, _ = s.Events().Publish(EventAgentChanged, AgentEvent{Agent: result.AgentID, Fields: []string{"crew_id", "role_id", "state"}})
	text := "Member changed"
	if verb != "" {
		text = "Member " + verb
	}
	at := a.now().UTC()
	receipt := DisplayEntry{Kind: "receipt", Text: text, By: "you", At: &at, Link: &SourceRef{Kind: "crew", ID: cid}}
	_, _ = s.Events().Publish(EventTurnEntry, TurnEntryEvent{Agent: result.AgentID, Entry: receipt})
	if patch.Instructions != nil {
		s.crewDirectiveMember(a, cid, result.AgentID, *patch.Instructions)
	}
	if mid == "" {
		write(w, 201, result)
	} else if r.Method == "PATCH" || verb == "move" {
		write(w, 200, result)
	} else {
		write(w, 200, ActionResponse{OK: true, Receipt: &receipt})
	}
}
func (s *Server) validateCrewMember(ctx context.Context, f *teams.File, meta crewMeta, b CreateMemberRequest) error {
	if b.RoleID != "" && b.NewRole != nil {
		return crewBad("choose role_id or new_role")
	}
	if b.RoleID == "" && b.NewRole == nil && b.AgentID == "" {
		return crewBad("choose a role or an existing agent")
	}
	if b.RoleID != "" {
		role, err := s.crewRole(ctx, f, b.RoleID)
		if err != nil {
			return err
		}
		b.NewRole = &NewRole{Name: role.Name, Instructions: role.Instructions, Model: role.Model, Effort: role.Effort}
	}
	if b.NewRole != nil {
		if !crewName(b.NewRole.Name) || len(b.NewRole.Instructions) > 16000 || b.NewRole.Effort != "" && b.NewRole.Effort != "low" && b.NewRole.Effort != "medium" && b.NewRole.Effort != "high" {
			return crewBad("role needs a name of 1 to 40 characters, instructions up to 16000 bytes and low, medium or high effort")
		}
	}
	if b.Cap != nil {
		cap, err := crewCents(*b.Cap)
		if err != nil {
			return err
		}
		budget, err := crewCents(meta.Budget)
		if err != nil {
			return err
		}
		if cap > budget {
			return crewBad("member caps cannot exceed the crew budget")
		}
	}
	return nil
}
func (s *Server) addCrewMember(ctx context.Context, a *AgentRegistry, d *agentDisk, f *teams.File, t *teams.Team, meta *crewMeta, b CreateMemberRequest, roleID string) (Member, error) {
	if len(t.Members) >= 64 {
		return Member{}, crewBad("a crew may have at most 64 members")
	}
	if err := s.validateCrewMember(ctx, f, *meta, b); err != nil {
		return Member{}, err
	}
	if b.RoleID != "" {
		role, err := s.crewRole(ctx, f, b.RoleID)
		if err != nil {
			return Member{}, err
		}
		roleID = role.ID
		b.NewRole = &NewRole{Name: role.Name, Instructions: role.Instructions, Model: role.Model, Effort: role.Effort}
	}
	if b.ReportsTo != "" && b.ReportsTo != meta.ManagerID {
		return Member{}, crewBad("members report to this crew's manager")
	}
	var record agentRecord
	if b.AgentID != "" {
		var ok bool
		record, ok = d.Records[b.AgentID]
		if !ok {
			return Member{}, errAgentMissing
		}
		if t.Holds(record.SessionFile) {
			return Member{}, agentError(409, "conflict", "already in "+t.Name)
		}
		if record.Agent.State == "closed" || record.Agent.State == "archived" {
			return Member{}, agentError(409, "conflict", "reopen the agent before adding it to a crew")
		}
	}
	role := NewRole{}
	if b.NewRole != nil {
		role = *b.NewRole
	} else {
		role = NewRole{Name: record.Agent.Name, Model: record.Agent.Model, Effort: record.Agent.Effort, Instructions: "Continue the role of " + record.Agent.Name + " for this crew."}
		roleID = record.Agent.RoleID
	}
	if role.Effort == "" {
		role.Effort = "medium"
	}
	if roleID == "" {
		roleID = teams.NewID()
	}
	for _, member := range meta.Members {
		if member.RoleID == roleID {
			return Member{}, agentError(409, "conflict", "already in "+t.Name)
		}
	}
	// A live copy in another crew gets a new conversation and keeps the shared role.
	if b.AgentID == "" || record.Agent.CrewID != "" {
		mutation := agentMutation{Start: "empty", Effort: &role.Effort, Budget: b.Cap}
		if role.Model != "" {
			mutation.Model = &role.Model
		}
		if err := s.fillAgentDefaults(&mutation); err != nil {
			return Member{}, err
		}
		agent, _, err := a.create(ctx, d, mutation)
		if err != nil {
			return Member{}, err
		}
		record = d.Records[agent.ID]
		record.Agent.Name = role.Name
		if err = a.live[agent.ID].conn.Engine.Rename(role.Name); err != nil {
			return Member{}, err
		}
	}
	if err := f.AddMember(t.ID, teams.Member{Key: record.SessionFile, File: record.SessionFile, Where: record.Workspace, Word: record.Agent.Name, JoinedAt: a.now().UTC()}); err != nil {
		return Member{}, err
	}
	record.Agent.CrewID = t.ID
	record.Agent.RoleID = roleID
	record.Agent.Version++
	d.Records[record.Agent.ID] = record
	state := "idle"
	member := crewMemberMeta{RoleID: roleID, Role: role, Cap: b.Cap, ReportsTo: b.ReportsTo, State: state, StartSpent: a.view(record).Spent}
	meta.Members[record.Agent.ID] = member
	return Member{CrewID: t.ID, AgentID: record.Agent.ID, RoleID: roleID, Cap: b.Cap, ReportsTo: b.ReportsTo, State: state, AlsoIn: []string{}}, nil
}
func (s *Server) crewDirectiveMember(a *AgentRegistry, cid, mid, text string) {
	var key string
	_ = a.disk(func(d *agentDisk) error { key = d.Records[mid].SessionFile; return nil }, false)
	if key != "" {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return
		}
		t, ok := f.Team(cid)
		if !ok {
			return
		}
		m, ok := t.Member(key)
		if !ok {
			return
		}
		_ = teams.AppendTraffic(a.home, cid, teams.Entry{Kind: teams.KindDirective, From: teams.FromYou, To: m.Handle, ToKey: key, Text: text})
	}
}

// CrewQuestionPath is the asks lane's read-only door. It never creates a second
// question or answers one; irreversible and production decisions retain the
// same source id and reach the person through this full visible path.
func (a *AgentRegistry) CrewQuestionPath(agentID string) ([]SourceRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var path []SourceRef
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[agentID]
		if !ok {
			return errAgentMissing
		}
		path = []SourceRef{{Kind: "member", ID: agentID}}
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		cid := record.Agent.CrewID
		for count := 0; cid != ""; count++ {
			if count >= len(f.Teams) {
				return fmt.Errorf("crew parent cycle")
			}
			t, ok := f.Team(cid)
			if !ok {
				return crewMissing()
			}
			path = append(path, SourceRef{Kind: "crew", ID: cid})
			cid = t.Parent
		}
		return nil
	}, false)
	return path, err
}

// CrewAllowsMessage is the turn lane's admission door. A paused or capped
// member cannot start another turn until its saved state or cap changes.
func (a *AgentRegistry) CrewAllowsMessage(agentID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.disk(func(d *agentDisk) error {
		record, ok := d.Records[agentID]
		if !ok {
			return errAgentMissing
		}
		if record.Agent.CrewID == "" {
			return nil
		}
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		t, ok := f.Team(record.Agent.CrewID)
		if !ok {
			return crewMissing()
		}
		m, err := readCrewMeta(t)
		if err != nil {
			return err
		}
		if t.Closed() {
			return agentError(409, "conflict", "reopen the crew before messaging")
		}
		for _, parent := range f.Ancestors(t.ID) {
			pm, err := readCrewMeta(parent)
			if err != nil {
				return err
			}
			spent, err := crewPoolSpent(f, parent, d, a)
			if err != nil {
				return err
			}
			cap, _ := crewCents(pm.Budget)
			if parent.Closed() || spent >= cap {
				return agentError(409, "at_cap", "the parent crew is at its budget")
			}
		}
		member := m.Members[agentID]
		if member.State == "paused" {
			return agentError(409, "conflict", "Paused · Resume to message")
		}
		if member.State == "stopped" {
			return agentError(409, "at_cap", "raise the member cap before messaging")
		}
		_, rows, err := crewView(f, t, d, a)
		if err != nil {
			return err
		}
		total := int64(0)
		for _, row := range rows {
			spent := int64(0)
			if row.Spent != nil {
				spent, _ = crewCents(*row.Spent)
			}
			total += spent
			if row.AgentID == agentID && row.Cap != nil {
				cap, _ := crewCents(*row.Cap)
				if spent >= cap {
					return agentError(409, "at_cap", "the member is at its cap")
				}
			}
		}
		total, err = crewPoolSpent(f, t, d, a)
		if err != nil {
			return err
		}
		budget, _ := crewCents(m.Budget)
		if total >= budget {
			return agentError(409, "at_cap", "the crew is at its budget")
		}
		return nil
	}, false)
}

// RecordCrewMessage is called by the turn producer after accepting a member's
// message. Native traffic gives its manager the same text without merging chats.
func (a *AgentRegistry) RecordCrewMessage(agentID, text string) error {
	return a.recordCrewMessage(agentID, text, "")
}
func (a *AgentRegistry) recordCrewMessage(agentID, text, key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.disk(func(d *agentDisk) error {
		record, ok := d.Records[agentID]
		if !ok {
			return errAgentMissing
		}
		if record.Agent.CrewID == "" {
			return nil
		}
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		t, ok := f.Team(record.Agent.CrewID)
		if !ok {
			return crewMissing()
		}
		native, ok := t.Member(record.SessionFile)
		if !ok {
			return agentError(409, "stale", "this agent has left the crew")
		}
		meta, err := readCrewMeta(t)
		if err != nil {
			return err
		}
		m := meta.Members[agentID]
		if key != "" {
			for _, saved := range m.MessageKeys {
				if saved == key {
					return nil
				}
			}
		}
		if err = teams.AppendTraffic(a.home, t.ID, teams.Entry{Kind: teams.KindYou, From: teams.FromYou, To: native.Handle, ToKey: record.SessionFile, Text: text}); err != nil {
			return err
		}
		if key == "" {
			return nil
		}
		return teams.Update(a.home, func(f *teams.File) error {
			i := teams.Index(f.Teams, t.ID)
			if i < 0 {
				return crewMissing()
			}
			meta, err := readCrewMeta(f.Teams[i])
			if err != nil {
				return err
			}
			m := meta.Members[agentID]
			m.MessageKeys = append(m.MessageKeys, key)
			if len(m.MessageKeys) > 128 {
				m.MessageKeys = m.MessageKeys[len(m.MessageKeys)-128:]
			}
			meta.Members[agentID] = m
			return writeCrewMeta(&f.Teams[i], meta)
		})
	}, false)
}
