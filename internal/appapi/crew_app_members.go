package appapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type appAgentMemberView struct {
	Member
	crewagent.MemberData
}

func (s *Server) crewAppsAroundMembers(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.PathValue("mid") != "" {
			next(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
		if err != nil {
			fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var probe struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &probe) != nil || probe.Kind != "app_agent" {
			next(w, r)
			return
		}
		s.addCrewAppAgent(w, r, raw)
	}
}

func (s *Server) addCrewAppAgent(w http.ResponseWriter, r *http.Request, raw []byte) {
	var body struct {
		Kind   string       `json:"kind"`
		App    string       `json:"app"`
		Expect *AgentExpect `json:"expect,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || body.App == "" {
		fail(w, 422, "bad_input", "an app agent needs app")
		return
	}
	all := s.AppsServices()
	if all == nil || all.Crews.Agent == nil {
		fail(w, 503, "not_allowed", "app agents are unavailable")
		return
	}
	if err := s.crewReady(r.PathValue("cid"), body.Expect, false); err != nil {
		crewFailure(w, err)
		return
	}
	caller := appsCaller()
	badge, err := all.Crews.Agent.Badge(r.Context(), svc.Scope{App: body.App, Caller: caller, Data: svc.DataReal})
	if err != nil {
		writeLinkError(w, err)
		return
	}
	data := crewagent.MemberData{Kind: "app_agent", App: body.App, Badge: badge, Asker: caller}
	a := s.options.Agents
	a.mu.Lock()
	var result appAgentMemberView
	err = a.disk(func(d *agentDisk) error {
		return teams.Update(a.home, func(file *teams.File) error {
			index := teams.Index(file.Teams, r.PathValue("cid"))
			if index < 0 {
				return crewMissing()
			}
			team := &file.Teams[index]
			meta, err := readCrewMeta(*team)
			if err != nil {
				return err
			}
			if err := crewExpect(*team, meta, body.Expect); err != nil {
				return err
			}
			if len(team.Members) >= 64 {
				return crewBad("a crew can have at most 64 members")
			}
			if team.Closed() {
				return agentError(409, "conflict", "reopen the crew before changing its members")
			}
			for _, member := range team.Members {
				existing, ok := crewAppData(member)
				if ok && existing.App == body.App {
					return agentError(409, "conflict", "the app agent is already in this crew")
				}
			}
			native := teams.Member{Key: "app-agent:" + team.ID + ":" + body.App, Word: badge.AgentName, Handle: body.App + "-agent", HandleBy: teams.HandleByTyped, JoinedAt: s.crewClock()}
			native, err = withCrewAppData(native, data)
			if err != nil {
				return err
			}
			if err := file.AddMember(team.ID, native); err != nil {
				return err
			}
			meta.Version++
			if err := writeCrewMeta(team, meta); err != nil {
				return err
			}
			result = appAgentMemberView{Member: Member{CrewID: team.ID, AgentID: native.Key, ReportsTo: crewAgentID(d, team.Manager), State: "idle", AlsoIn: []string{}}, MemberData: data}
			return nil
		})
	}, false)
	a.mu.Unlock()
	if err != nil {
		writeLinkError(w, err)
		return
	}
	s.crewChanged(result.CrewID, "members")
	write(w, 201, result)
}

func crewAppData(member teams.Member) (crewagent.MemberData, bool) {
	raw, err := json.Marshal(member)
	if err != nil {
		return crewagent.MemberData{}, false
	}
	var data crewagent.MemberData
	if json.Unmarshal(raw, &data) != nil || data.Kind != "app_agent" {
		return data, false
	}
	return data, true
}

func withCrewAppData(member teams.Member, data crewagent.MemberData) (teams.Member, error) {
	raw, err := json.Marshal(member)
	if err != nil {
		return member, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return member, err
	}
	raw, err = json.Marshal(data)
	if err != nil {
		return member, err
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(raw, &extra); err != nil {
		return member, err
	}
	for key, value := range extra {
		fields[key] = value
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return member, err
	}
	err = json.Unmarshal(raw, &member)
	return member, err
}

func (s *Server) crewAppMemberViews(cid string, base []Member) ([]any, error) {
	views := make([]any, 0, len(base))
	a := s.options.Agents
	if a == nil {
		return views, nil
	}
	file, err := teams.Snapshot(a.home)
	if err != nil {
		return nil, err
	}
	team, ok := file.Team(cid)
	if !ok {
		return nil, crewMissing()
	}
	for _, member := range base {
		var data crewagent.MemberData
		found := false
		for _, native := range team.Members {
			if native.Key == member.AgentID {
				data, found = crewAppData(native)
				break
			}
		}
		if found {
			views = append(views, appAgentMemberView{Member: member, MemberData: data})
		} else {
			views = append(views, member)
		}
	}
	return views, nil
}

func (s *Server) syncCrewApps(r *http.Request, verb string) error {
	if err := s.syncCrewLinks(r, verb); err != nil {
		return err
	}
	if verb != "finish" || s.options.Agents == nil {
		return nil
	}
	ids := s.crewFamily(r.PathValue("cid"))
	for _, id := range ids {
		session.StopCrewAppAgents(s.options.Agents.home, id)
	}
	a := s.options.Agents
	a.mu.Lock()
	defer a.mu.Unlock()
	return teams.Update(a.home, func(file *teams.File) error {
		for _, id := range ids {
			index := teams.Index(file.Teams, id)
			if index < 0 {
				continue
			}
			team := &file.Teams[index]
			kept := make([]teams.Member, 0, len(team.Members))
			for _, member := range team.Members {
				if _, ok := crewAppData(member); ok {
					team.FormerMembers = append(team.FormerMembers, member)
				} else {
					kept = append(kept, member)
				}
			}
			team.Members = kept
		}
		return nil
	})
}
