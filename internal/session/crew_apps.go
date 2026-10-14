package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type crewAppsBinding struct {
	turns  *crewAppTurns
	all    *svc.Services
	caller svc.Caller
}

var crewAppsBindings sync.Map

// SetCrewApps binds the server's apps to conversations using the same profile.
func SetCrewApps(profile string, all *svc.Services, caller svc.Caller) {
	if profile == "" {
		return
	}
	if all == nil {
		if previous, ok := crewAppsBindings.Load(profile); ok {
			previous.(crewAppsBinding).turns.stop("")
		}
		crewAppsBindings.Delete(profile)
		return
	}
	crewAppsBindings.Store(profile, crewAppsBinding{all: all, caller: caller, turns: &crewAppTurns{running: map[string]map[*crewAppTurn]struct{}{}}})
}

func appMemberData(member teams.Member) (crewagent.MemberData, bool) {
	raw, err := json.Marshal(member)
	if err != nil {
		return crewagent.MemberData{}, false
	}
	var data crewagent.MemberData
	if json.Unmarshal(raw, &data) != nil || data.Kind != "app_agent" || data.App == "" {
		return data, false
	}
	return data, true
}

func (a *Agent) dispatchCrewApps(ctx context.Context, team teams.Team, entry teams.Entry, id string, wakes bool) (string, bool, error) {
	targets := teamSendTargets(team, entry)
	var apps, regular []teams.Member
	for _, target := range targets {
		if _, ok := appMemberData(target); ok {
			apps = append(apps, target)
		} else {
			regular = append(regular, target)
		}
	}
	if len(apps) == 0 {
		return "", false, nil
	}
	profile := a.config.teamProfile()
	bound, ok := crewAppsBindings.Load(profile)
	if !ok {
		return "The app agent is unavailable in this crew.", true, nil
	}
	binding := bound.(crewAppsBinding)
	if binding.all.Crews.Agent == nil {
		return "The app agent is unavailable in this crew.", true, nil
	}
	for _, member := range apps {
		if err := routeCrewApp(ctx, profile, binding, team, member, entry, id); err != nil {
			return "The app agent could not answer: " + err.Error(), true, err
		}
	}
	if len(regular) > 0 && entry.Kind == teams.KindDirective {
		a.teamRouse(profile, team, wakes, regular, id)
	}
	return "The app agent answered in the crew log (" + teams.ThreadNumber(id) + ").", true, nil
}

func routeCrewApp(ctx context.Context, profile string, binding crewAppsBinding, team teams.Team, member teams.Member, entry teams.Entry, id string) error {
	ctx, finish := binding.turns.start(ctx, team.ID)
	defer finish()
	current, err := teams.Snapshot(profile)
	if err != nil {
		return err
	}
	live, ok := current.Team(team.ID)
	if !ok || live.Closed() || !live.Holds(member.Key) {
		return bridge.Errorf(bridge.CodeConflict, "the app agent has left the crew")
	}
	data, _ := appMemberData(member)
	asker := binding.caller
	if asker.Member == "" {
		asker = data.Asker
	}
	asker.Role = lesserAppRole(asker.Role, svc.Role(data.Badge.Role))
	req := svc.CrewAsk{App: data.App, Crew: team.ID, CrewName: team.Name, Asker: asker, Text: entry.Text}
	if binding.all.Crews.Links != nil {
		links, err := binding.all.Crews.Links.ForCrew(ctx, asker, bridge.LinksForCrewParams{Crew: team.ID})
		if err != nil {
			return err
		}
		for _, link := range links.Links {
			if link.App == data.App && link.State == bridge.CrewLinkOpen && link.Access == bridge.CrewLinkAccessOK {
				req.Linked = append(req.Linked, bridge.Record{Type: link.Type, ID: link.ID})
			}
		}
	}
	answer, err := binding.all.Crews.Agent.Ask(ctx, req)
	if err != nil {
		return err
	}
	reply := teams.Entry{At: time.Now().UTC(), Kind: teams.KindNote, From: member.Handle, FromKey: member.Key, To: entry.From, Member: member.Key, Answers: id, Text: crewAppAnswer(data.Badge, answer)}
	replyID, err := teams.AppendTrafficID(profile, team.ID, reply)
	if err != nil {
		return err
	}
	return crewagent.SaveReceipt(profile, team.ID, crewagent.LogReceipt{ID: replyID, Text: reply.Text, From: reply.From, At: reply.At, Member: member.Key, Badge: data.Badge, Answer: answer})
}

func lesserAppRole(asked, added svc.Role) svc.Role {
	rank := map[svc.Role]int{svc.RoleViewer: 1, svc.RoleUser: 2, svc.RoleEditor: 3}
	if rank[asked] == 0 || rank[added] == 0 {
		return svc.RoleViewer
	}
	if rank[asked] < rank[added] {
		return asked
	}
	return added
}

func crewAppAnswer(badge bridge.Badge, answer svc.CrewAnswer) string {
	var out strings.Builder
	out.WriteString("from " + badge.AppName)
	if badge.Can != "" {
		out.WriteString(" · " + badge.Can)
	}
	out.WriteString("\n\n" + answer.Text)
	if len(answer.Changed) > 0 {
		out.WriteString("\n\nChanged in " + badge.AppName + " through the " + badge.AgentName + "\n")
		for _, record := range answer.Changed {
			fmt.Fprintf(&out, "%s · %s · %s v%d\n", record.Label, record.What, badge.AppName, record.Version)
		}
	}
	if len(answer.Approvals) > 0 {
		out.WriteString("\nWaiting on you:\n")
		for _, approval := range answer.Approvals {
			out.WriteString(approval.Title + " · " + approval.Summary + " · " + approval.ID + "\n")
		}
	}
	if answer.ShapeThis != "" {
		out.WriteString("\nThat changes the app · Shape this\n" + answer.ShapeThis)
	}
	if answer.Cost > 0 {
		fmt.Fprintf(&out, "\n$%.2f · %s budget", answer.Cost, badge.AppName)
	}
	return out.String()
}

// Both member posts and manager sends enter through this one registration hook.
func (a *Agent) crewAppsTools(tools []bare.Tool) []bare.Tool {
	for i, tool := range tools {
		if tool.Name != teamSendToolName && tool.Name != teamPostToolName {
			continue
		}
		tools[i].Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			reply, failed, err, handled := a.crewAppsTool(ctx, tool.Name, args)
			if handled {
				return reply, failed, err
			}
			return tool.Execute(ctx, args)
		}
	}
	return tools
}

func (a *Agent) crewAppsTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error, bool) {
	var parsed struct{ To, Text, Kind, Thread, Team string }
	if decodeToolArguments(args, &parsed) != nil || strings.TrimSpace(parsed.Text) == "" {
		return "", false, nil, false
	}
	manager := name == teamSendToolName
	if manager && parsed.Thread != "" || !manager && parsed.Kind != "" {
		return "", false, nil, false
	}
	team, role, refusal := a.teamTarget(parsed.Team, manager)
	if refusal != "" {
		return "", false, nil, false
	}
	entry, refusal := a.crewAppAddress(team, role, manager, parsed.To, parsed.Kind)
	if refusal != "" {
		return "", false, nil, false
	}
	targets := teamSendTargets(team, entry)
	hasApp := false
	for _, target := range targets {
		if _, ok := appMemberData(target); ok {
			hasApp = true
		}
	}
	if !hasApp {
		return "", false, nil, false
	}
	if parsed.Thread != "" {
		thread, ok := teams.ThreadID(parsed.Thread)
		if !ok {
			return "", false, nil, false
		}
		entry.Answers = thread
	}
	entry.Text = strings.TrimSpace(parsed.Text)
	id, err := teams.AppendTrafficID(a.config.teamProfile(), team.ID, entry)
	if err != nil {
		return "The crew message could not be written: " + err.Error(), true, err, true
	}
	reply, handled, err := a.dispatchCrewApps(ctx, team, entry, id, role.wakes)
	return reply, err != nil, err, handled
}

func (a *Agent) crewAppAddress(team teams.Team, role teamRole, manager bool, to, kind string) (teams.Entry, string) {
	if !manager {
		handle := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(to)), "@")
		member, ok := teamMemberByHandle(team, handle)
		if !ok || member.Handle == role.handle || role.handle == "" {
			return teams.Entry{}, "not addressed to a teammate"
		}
		return teams.Entry{Kind: teams.KindNote, From: role.handle, To: handle, Member: member.Key}, ""
	}
	if kind == "" {
		kind = teams.KindNote
	}
	if kind != teams.KindDirective && kind != teams.KindNote {
		return teams.Entry{}, "invalid message kind"
	}
	file, _, _, _ := a.teamSnapshot(a.config.teamProfile())
	entry, refusal := teamSendAddress(file, team, to, kind)
	if refusal != "" {
		return entry, refusal
	}
	if entry.To == teams.ToEveryone && kind == teams.KindDirective {
		own, shared := splitByHome(file, team)
		if len(shared) > 0 {
			if len(own) == 0 {
				return entry, "none report here"
			}
			entry = teamEntryFor(own)
		}
	}
	entry.Kind, entry.From = kind, teams.FromManager
	return entry, ""
}

type crewAppTurn struct{ cancel context.CancelFunc }
type crewAppTurns struct {
	mu      sync.Mutex
	running map[string]map[*crewAppTurn]struct{}
}

func (t *crewAppTurns) start(ctx context.Context, crew string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	turn := &crewAppTurn{cancel: cancel}
	t.mu.Lock()
	if t.running[crew] == nil {
		t.running[crew] = map[*crewAppTurn]struct{}{}
	}
	t.running[crew][turn] = struct{}{}
	t.mu.Unlock()
	return ctx, func() { cancel(); t.mu.Lock(); defer t.mu.Unlock(); delete(t.running[crew], turn) }
}
func (t *crewAppTurns) stop(crew string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, turns := range t.running {
		if crew != "" && id != crew {
			continue
		}
		for turn := range turns {
			turn.cancel()
		}
		delete(t.running, id)
	}
}

// StopCrewAppAgents ends the crew copies while the app's own agent remains available.
func StopCrewAppAgents(profile, crew string) {
	if binding, ok := crewAppsBindings.Load(profile); ok {
		binding.(crewAppsBinding).turns.stop(crew)
	}
}
