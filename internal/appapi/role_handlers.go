package appapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var roleContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/roles", Request: "", Response: "RolesResponse"}, handle: (*Server).roleHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/roles", Request: "CreateRoleRequest", Response: "CrewRole"}, handle: (*Server).roleHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/roles/{rid}", Request: "PatchRoleRequest", Response: "CrewRole"}, handle: (*Server).roleHandler},
}

// Role edits save a shared definition; native directives apply instructions at
// each copy's next boundary without changing its memory or current work.
func (s *Server) roleHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.serveRole)(w, r)
}
func (s *Server) serveRole(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	var create CreateRoleRequest
	var patch PatchRoleRequest
	if r.Method == "POST" && !decodeCrew(w, r, &create) {
		return
	}
	if r.Method == "PATCH" && !decodeCrew(w, r, &patch) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	var result CrewRole
	var version uint64
	var copies []RoleCopy
	err := a.disk(func(d *agentDisk) error {
		f, err := teams.Snapshot(a.home)
		if err != nil {
			return err
		}
		roles, err := readCrewRoles(a.home)
		if err != nil {
			return err
		}
		// Inline roles made before this store remain editable, without migration.
		for _, t := range f.Teams {
			meta, err := readCrewMeta(t)
			if err != nil {
				return err
			}
			for _, m := range meta.Members {
				if m.RoleID != "" {
					if _, ok := roles[m.RoleID]; !ok {
						roles[m.RoleID] = savedCrewRole{CrewRole: CrewRole{ID: m.RoleID, Name: m.Role.Name, Instructions: m.Role.Instructions, Model: m.Role.Model, Effort: m.Role.Effort, MadeBy: "you"}, Version: 1}
					}
				}
			}
		}
		if r.Method == "GET" {
			rows := []CrewRole{}
			for id, role := range roles {
				role.Copies, err = roleCopies(f, id, d)
				if err != nil {
					return err
				}
				if strings.Contains(strings.ToLower(role.Name+" "+role.Instructions), strings.ToLower(r.URL.Query().Get("q"))) {
					rows = append(rows, role.CrewRole)
				}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
			write(w, 200, RolesResponse{Roles: rows})
			return nil
		}
		return updateCrewRoles(a.home, func(current map[string]savedCrewRole) error {
			role := savedCrewRole{CrewRole: CrewRole{ID: teams.NewID(), Name: create.Name, Instructions: create.Instructions, Model: create.Model, Effort: create.Effort, MadeBy: "you"}, Version: 1}
			expect := create.Expect
			if r.Method == "PATCH" {
				id := r.PathValue("rid")
				var ok bool
				role, ok = current[id]
				if !ok {
					role, ok = roles[id]
				}
				if !ok {
					return agentError(404, "not_found", "no such role")
				}
				expect = patch.Expect
				if patch.Name != nil {
					role.Name = *patch.Name
				}
				if patch.Instructions != nil {
					role.Instructions = *patch.Instructions
				}
				if patch.Model != nil {
					role.Model = *patch.Model
				}
				if patch.Effort != nil {
					role.Effort = *patch.Effort
				}
			}
			if expect != nil && (expect.State != nil || expect.Version != nil && (r.Method == "POST" || *expect.Version != role.Version)) {
				return agentError(409, "stale", "the role changed; read it again before acting")
			}
			role.Name = strings.TrimSpace(role.Name)
			if role.Effort == "" {
				role.Effort = "medium"
			}
			if !crewName(role.Name) || len(role.Instructions) > 16000 || strings.TrimSpace(role.Instructions) == "" || strings.TrimSpace(role.Model) == "" || len(role.Model) > 256 || role.Effort != "low" && role.Effort != "medium" && role.Effort != "high" {
				return crewBad("role needs a name of 1 to 40 characters, instructions of 1 to 16000 bytes, a model and low, medium or high effort")
			}
			if r.Method == "PATCH" {
				role.Version++
			}
			copies, err = roleCopies(f, role.ID, d)
			if err != nil {
				return err
			}
			if r.Method == "PATCH" {
				if err = teams.Update(a.home, func(currentTeams *teams.File) error {
					for _, copy := range copies {
						i := teams.Index(currentTeams.Teams, copy.CrewID)
						if i < 0 || currentTeams.Teams[i].Closed() {
							continue
						}
						t := &currentTeams.Teams[i]
						meta, err := readCrewMeta(*t)
						if err != nil {
							return err
						}
						member, ok := meta.Members[copy.MemberID]
						if !ok || member.RoleID != role.ID {
							continue
						}
						member.Role = NewRole{Name: role.Name, Instructions: role.Instructions, Model: role.Model, Effort: role.Effort}
						meta.Members[copy.MemberID] = member
						meta.Version++
						if err = writeCrewMeta(t, meta); err != nil {
							return err
						}
						record, ok := d.Records[copy.MemberID]
						if !ok {
							continue
						}
						live, err := a.connect(r.Context(), record)
						if err != nil {
							return err
						}
						live.conn.Engine.SetModel(role.Model)
						live.conn.Engine.SetReasoningFor(role.Model, role.Effort)
						if err = live.conn.Engine.Rename(role.Name); err != nil {
							return err
						}
						record.Agent.Name = role.Name
						record.Agent.Model = role.Model
						record.Agent.Effort = role.Effort
						record.Agent.Version++
						d.Records[copy.MemberID] = record
					}
					return nil
				}); err != nil {
					return err
				}
			}
			role.Copies = nil
			current[role.ID] = role
			result, version = role.CrewRole, role.Version
			return nil
		})
	}, r.Method != "GET")
	if err != nil {
		crewFailure(w, err)
		return
	}
	if r.Method == "GET" {
		return
	}
	for _, copy := range copies {
		var key string
		if err := a.disk(func(d *agentDisk) error { key = d.Records[copy.MemberID].SessionFile; return nil }, false); err != nil {
			crewFailure(w, err)
			return
		}
		if key != "" {
			f, err := teams.Snapshot(a.home)
			if err != nil {
				crewFailure(w, err)
				return
			}
			t, ok := f.Team(copy.CrewID)
			if !ok || t.Closed() {
				continue
			}
			member, ok := t.Member(key)
			if !ok {
				continue
			}
			if err := teams.AppendTraffic(a.home, copy.CrewID, teams.Entry{Kind: teams.KindDirective, From: teams.FromYou, To: member.Handle, ToKey: key, Text: "Role updated: " + result.Name + "\n" + result.Instructions, At: a.now().UTC()}); err != nil {
				crewFailure(w, err)
				return
			}
		}
		s.crewChanged(copy.CrewID, "members")
		_, _ = s.Events().Publish(EventAgentChanged, AgentEvent{Agent: copy.MemberID, Fields: []string{"name", "model", "effort"}})
	}
	result.Copies = copies
	_, _ = s.Events().Publish(EventRoleChanged, IDChangedEvent{ID: result.ID})
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", version))
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	write(w, status, result)
}
