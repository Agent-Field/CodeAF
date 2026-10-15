package main

// ── codeaf app: A CREW'S MEMBERS, ROLES AND PROPOSALS ───────────────────────
//
// The member verbs are the team store's own (add, remove, handle, home, and a
// stop on the crew's log the member's engine reads) beside the few the app
// layer keeps (a role, a pause, a budget, what a member was). Where the layer
// has to make a field true it does so through doors the engine already has:
// a role's instructions are a message in the member's own conversation, its
// model the conversation's model, a member's budget the agent's own spending
// limit, and a pause the crew's own stop with every turn the conversation
// starts by itself stopped until the person resumes it (appwakes.go).
//
// A PROPOSAL is the agent's own: the app asks it, in its chat where the person
// can read the question, for one JSON block naming the crew and the helpers it
// would start, and the answer becomes an ask. Accepting it makes the team with
// the agent as manager and writes each helper's start on the crew's log as the
// manager's own line, its brief the manager's own words from the proposal;
// the app then performs those starts exactly as it performs any manager's.

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// Member answers one member verb with the member as it now stands.
func (b *appBackend) Member(verb string, ask appwire.MemberAsk) (appwire.CrewMember, error) {
	t, _, err := b.crewTeam(ask.Crew)
	if err != nil {
		return appwire.CrewMember{}, err
	}
	b.crews()
	crew := ask.Crew
	agent := strings.TrimSpace(ask.Agent)
	switch verb {
	case "add":
		agent, err = b.memberAdd(t, agent, ask)
	case "start":
		agent, err = b.memberStart(t, ask)
	default:
		m, ok := b.crewMemberOf(t, agent)
		if !ok {
			return appwire.CrewMember{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
		}
		switch verb {
		case "stop":
			err = b.memberStop(t, m, agent, ask.Reason)
		case "pause":
			if err = b.memberStop(t, m, agent, "paused"); err == nil {
				err = b.layerMember(crew, agent, func(x *appMemberExtra) { x.Paused = true })
			}
		case "resume":
			err = b.layerMember(crew, agent, func(x *appMemberExtra) { x.Paused = false })
		case "remove":
			if m.Key == t.Manager {
				return appwire.CrewMember{}, appwire.Errorf(appwire.CodeBadParams, "the manager is not removed; make another member manager first")
			}
			err = b.crewEdit(func(f *teamstore.File) error { return f.RemoveMember(crew, m.Key) })
			if err == nil {
				view := appwire.CrewMember{Agent: agent, Crew: crew, Handle: m.Handle}
				return view, nil
			}
		case "handle":
			err = b.crewEdit(func(f *teamstore.File) error { return f.SetHandle(crew, m.Key, strings.TrimSpace(ask.Handle)) })
		case "home":
			err = b.crewEdit(func(f *teamstore.File) error { return f.SetHome(m.Key, crew) })
		case "cap":
			err = b.memberCap(agent, ask.CapUSD)
		case "role":
			err = b.memberRole(crew, agent, ask.Role, ask.Brief, ask.Model)
		case "move":
			crew, err = b.memberMove(t, m, agent, ask.To)
		default:
			return appwire.CrewMember{}, appwire.Errorf(appwire.CodeUnknownMethod, "members.%s is not a member verb", verb)
		}
	}
	if err != nil {
		return appwire.CrewMember{}, err
	}
	return b.memberNow(crew, agent)
}

// memberNow is a member as its crew shows it now.
func (b *appBackend) memberNow(crew, agent string) (appwire.CrewMember, error) {
	view, err := b.CrewGet(crew)
	if err != nil {
		return appwire.CrewMember{}, err
	}
	for _, m := range view.Members {
		if m.Agent == agent {
			return m, nil
		}
	}
	return appwire.CrewMember{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, view.Name)
}

// layerMember changes a member's app-layer record.
func (b *appBackend) layerMember(crew, agent string, fn func(*appMemberExtra)) error {
	if err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		fn(f.member(crew, agent))
		return nil
	}); err != nil {
		return appEngineError(err)
	}
	return nil
}

// memberAdd puts an existing agent in the crew, as the manager's team_add does.
func (b *appBackend) memberAdd(t teamstore.Team, agent string, ask appwire.MemberAsk) (string, error) {
	if agent == "" {
		return "", appwire.Errorf(appwire.CodeBadParams, "members.add needs an agent")
	}
	m, err := b.agentMember(agent)
	if err != nil {
		return "", err
	}
	handle := strings.TrimSpace(ask.Handle)
	err = b.crewEdit(func(f *teamstore.File) error {
		if err := f.AddMember(t.ID, m); err != nil {
			return err
		}
		if handle != "" {
			return f.SetHandle(t.ID, m.Key, handle)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ask.Role) != "" {
		if err := b.memberRole(t.ID, agent, ask.Role, ask.Brief, ask.Model); err != nil {
			return "", err
		}
	}
	return agent, nil
}

// memberStart opens a new conversation in the crew's folder whose first
// message is the person's brief, and puts it in the crew under the handle.
func (b *appBackend) memberStart(t teamstore.Team, ask appwire.MemberAsk) (string, error) {
	handle, brief := strings.TrimSpace(ask.Handle), strings.TrimSpace(ask.Brief)
	if err := teamstore.ValidHandle(handle); err != nil {
		return "", appwire.Errorf(appwire.CodeBadParams, "a handle is 2 to 12 lowercase letters or digits")
	}
	if _, taken := t.ByHandle(handle); taken {
		return "", appwire.Errorf(appwire.CodeBadParams, "@%s is already in %s", handle, t.Name)
	}
	if brief == "" {
		return "", appwire.Errorf(appwire.CodeBadParams, "members.start needs a brief")
	}
	model := strings.TrimSpace(ask.Model)
	var role *appwire.Role
	if ask.Role != "" {
		role = b.roleNamed(ask.Role)
		if role != nil && model == "" {
			model = role.Model
		}
	}
	agent, err := b.CreateAgent(appwire.CreateAsk{Workspace: appCrewWhere(t), Model: model})
	if err != nil {
		return "", err
	}
	if _, _, err := b.heldLive(agent.ID); err != nil {
		return "", err
	}
	m, err := b.agentMember(agent.ID)
	if err != nil {
		return "", err
	}
	m.Handle = handle
	if err := b.crewEdit(func(f *teamstore.File) error {
		if err := f.AddMember(t.ID, m); err != nil {
			return err
		}
		return f.SetHandle(t.ID, m.Key, handle)
	}); err != nil {
		return "", err
	}
	if ask.Role != "" {
		if err := b.memberRole(t.ID, agent.ID, ask.Role, "", ""); err != nil {
			return "", err
		}
	}
	if _, err := b.Send(agent.ID, brief, ""); err != nil {
		return "", err
	}
	return agent.ID, nil
}

// memberStop is the crew's own stop: a line on its log the member's engine
// reads, and the member's running turn stopped.
func (b *appBackend) memberStop(t teamstore.Team, m teamstore.Member, agent, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "stopped by you"
	}
	if err := teamstore.AppendTraffic(b.crewProfile(), t.ID, teamstore.Entry{
		Kind: teamstore.KindStop, From: teamstore.FromYou, To: m.Handle, ToKey: m.Key, Member: m.Key, Text: reason,
	}); err != nil {
		return appEngineError(err)
	}
	if b.isHeld(agent) {
		return b.Stop(agent)
	}
	return nil
}

// memberCap is a member's budget: the agent's own spending limit.
func (b *appBackend) memberCap(agent string, usd *float64) error {
	if usd == nil || *usd <= 0 {
		return appwire.Errorf(appwire.CodeBadParams, "a member's budget is more than $0")
	}
	return b.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent, ToUSD: *usd, For: appwire.RaiseGood})
}

// memberMove moves a member to another crew under the same handle, and keeps
// where it was.
func (b *appBackend) memberMove(t teamstore.Team, m teamstore.Member, agent, to string) (string, error) {
	if strings.TrimSpace(to) == "" || to == t.ID {
		return "", appwire.Errorf(appwire.CodeBadParams, "members.move needs another crew in to")
	}
	if m.Key == t.Manager {
		return "", appwire.Errorf(appwire.CodeBadParams, "the manager does not move; make another member manager first")
	}
	err := b.crewEdit(func(f *teamstore.File) error {
		moved := m
		moved.Home, moved.Started = false, false
		if err := f.AddMember(to, moved); err != nil {
			return err
		}
		if moved.Handle != "" {
			_ = f.SetHandle(to, moved.Key, moved.Handle)
		}
		return f.RemoveMember(t.ID, m.Key)
	})
	if err != nil {
		return "", err
	}
	err = b.crewLayer().write(func(f *appCrewLayerFile) error {
		old := f.member(t.ID, agent)
		next := f.member(to, agent)
		next.Role, next.Paused = old.Role, old.Paused
		next.Was = &appwire.MemberWas{Crew: t.ID, Role: old.Role, At: b.now()}
		delete(f.Members, appMemberKey(t.ID, agent))
		return nil
	})
	if err != nil {
		return "", appEngineError(err)
	}
	return to, nil
}

// ── roles ───────────────────────────────────────────────────────────────────

// roleNamed is a role by id or by name, or nil.
func (b *appBackend) roleNamed(idOrName string) *appwire.Role {
	var found *appwire.Role
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for _, role := range f.Roles {
			if role != nil && (role.ID == idOrName || strings.EqualFold(role.Name, idOrName)) {
				copied := *role
				found = &copied
			}
		}
	})
	return found
}

// memberRole gives a member a role, making the role when it is new: its
// instructions go to the member as a message in its own conversation and its
// model becomes the member's.
func (b *appBackend) memberRole(crew, agent, role, instructions, model string) error {
	role, instructions, model = strings.TrimSpace(role), strings.TrimSpace(instructions), strings.TrimSpace(model)
	if role == "" {
		return appwire.Errorf(appwire.CodeBadParams, "name the role")
	}
	var given appwire.Role
	err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		var found *appwire.Role
		for _, r := range f.Roles {
			if r != nil && (r.ID == role || strings.EqualFold(r.Name, role)) {
				found = r
			}
		}
		if found == nil {
			if f.Roles == nil {
				f.Roles = map[string]*appwire.Role{}
			}
			found = &appwire.Role{ID: f.next("role"), Name: role, Instructions: instructions, Model: model, MadeAt: b.now()}
			f.Roles[found.ID] = found
		}
		m := f.member(crew, agent)
		if m.Role != found.ID {
			if m.Role != "" {
				m.Was = &appwire.MemberWas{Role: m.Role, At: b.now()}
			}
			m.Role = found.ID
		}
		given = *found
		return nil
	})
	if err != nil {
		return appEngineError(err)
	}
	return b.tellRole(agent, crew, given)
}

// tellRole puts a role's instructions in a member's conversation and moves it
// to the role's model.
func (b *appBackend) tellRole(agent, crew string, role appwire.Role) error {
	if role.Model != "" {
		if _, err := b.SetModel(agent, role.Model); err != nil {
			return err
		}
	}
	if strings.TrimSpace(role.Instructions) == "" {
		return nil
	}
	name := crew
	if t, _, err := b.crewTeam(crew); err == nil {
		name = t.Name
	}
	_, err := b.Queue(agent, "Your role in "+name+" is "+role.Name+".\n\n"+role.Instructions, "")
	return err
}

// Roles is every role, with the members that carry it.
func (b *appBackend) Roles() ([]appwire.Role, error) {
	var out []appwire.Role
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for _, role := range f.Roles {
			if role == nil {
				continue
			}
			copied := *role
			copied.Copies = []appwire.RoleCopy{}
			for key, m := range f.Members {
				if m != nil && m.Role == role.ID {
					crew, agent, _ := strings.Cut(key, "/")
					copied.Copies = append(copied.Copies, appwire.RoleCopy{Crew: crew, Agent: agent})
				}
			}
			out = append(out, copied)
		}
	})
	return out, nil
}

// RoleUpdate edits a role; every member that carries it is told the new
// instructions and moved to the new model.
func (b *appBackend) RoleUpdate(id string, name, instructions, model *string) (appwire.Role, error) {
	var role appwire.Role
	var copies []appwire.RoleCopy
	told, moved := false, false
	err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		r := f.Roles[id]
		if r == nil {
			return appwire.Errorf(appwire.CodeNotFound, "there is no role %s", id)
		}
		if name != nil && strings.TrimSpace(*name) != "" {
			r.Name = strings.TrimSpace(*name)
		}
		if instructions != nil && strings.TrimSpace(*instructions) != r.Instructions {
			r.Instructions, told = strings.TrimSpace(*instructions), true
		}
		if model != nil && strings.TrimSpace(*model) != r.Model {
			r.Model, moved = strings.TrimSpace(*model), true
		}
		role = *r
		for key, m := range f.Members {
			if m != nil && m.Role == id {
				crew, agent, _ := strings.Cut(key, "/")
				copies = append(copies, appwire.RoleCopy{Crew: crew, Agent: agent})
			}
		}
		return nil
	})
	if err != nil {
		var wire *appwire.Error
		if ok := asWire(err, &wire); ok {
			return appwire.Role{}, wire
		}
		return appwire.Role{}, appEngineError(err)
	}
	for _, c := range copies {
		send := role
		if !told {
			send.Instructions = ""
		}
		if !moved {
			send.Model = ""
		}
		if err := b.tellRole(c.Agent, c.Crew, send); err != nil {
			return appwire.Role{}, err
		}
	}
	role.Copies = append([]appwire.RoleCopy{}, copies...)
	return role, nil
}

// asWire unwraps a wire error.
func asWire(err error, wire **appwire.Error) bool {
	w, ok := err.(*appwire.Error)
	if ok {
		*wire = w
	}
	return ok
}

// ── proposals ───────────────────────────────────────────────────────────────

// appProposalAsk is what the app asks an agent for when the person asks it to
// propose a crew. It is said in the agent's own chat, so the person reads it.
const appProposalAsk = "Propose a crew for the work in this conversation: the helpers you would start and manage. " +
	"Reply with one JSON object and nothing else, shaped " +
	`{"name": "the crew's name", "goal": "one line", "helpers": [{"handle": "2 to 12 lowercase letters or digits", "role": "a short role name", "brief": "the helper's whole assignment"}]}. ` +
	"Do not start anyone yet; the person decides."

// CrewPropose asks the agent for a crew proposal; its answer arrives as an ask.
func (b *appBackend) CrewPropose(agent string) error {
	a, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	b.crews()
	if _, err := b.Send(agent, appProposalAsk, ""); err != nil {
		return err
	}
	var done chan struct{}
	b.locked(func() { done = live.pumpDone })
	guard.Go("app/crew-propose", func() {
		if done != nil {
			<-done
		}
		b.proposalAnswered(a, agent)
	})
	return nil
}

// proposalAnswered reads the agent's answer as a proposal and asks the person.
func (b *appBackend) proposalAnswered(a *appAgent, agent string) {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	answer := ""
	for _, entry := range appEntriesOf(agent, appRecordItems(session.ReadTranscript(transcript)), b.now(), nil) {
		if entry.Kind == appwire.EntryAnswer {
			answer = entry.Text
		}
	}
	p, ok := appParseProposal(answer)
	if !ok {
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: "crewprop-none-" + b.now().Format("150405"), Agent: agent, Kind: appwire.EntryNotice, At: b.now(),
			Text: "No crew proposal came back in a form the app can read", Quiet: true,
		}})
		return
	}
	p.Agent, p.At = agent, b.now()
	if err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		p.ID = f.next("prop")
		if f.Proposals == nil {
			f.Proposals = map[string]*appwire.Proposal{}
		}
		kept := p
		f.Proposals[p.ID] = &kept
		return nil
	}); err != nil {
		return
	}
	ask := appProposalAskOf(p)
	run := b.crews()
	run.locked(func() { run.asks[ask.ID] = ask })
	b.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
}

// appProposalAskOf is a proposal as the Bring in helpers ask.
func appProposalAskOf(p appwire.Proposal) appwire.Ask {
	var detail []string
	for _, h := range p.Helpers {
		line := "@" + h.Handle
		if h.Role != "" {
			line += " · " + h.Role
		}
		detail = append(detail, line)
	}
	return appwire.Ask{
		ID: appProposalAskPrefix + p.ID, Agent: p.Agent, Kind: appwire.AskQuestion, Subkind: appwire.SubkindCrewProposal,
		Title: "Bring in helpers: " + p.Name, Detail: strings.Join(detail, "\n"), Reason: p.Goal,
		Subject:  &appwire.AskSubject{Kind: appwire.SubjectCrewProposal, Ref: p.ID},
		Proposal: &p,
		Options: []appwire.AskOption{
			{ID: appwire.ProposalCreate, Label: "Make the crew"},
			{ID: appwire.ProposalEdit, Label: "Edit"},
			{ID: appwire.ProposalKeep, Label: "Keep it one agent"},
		},
	}
}

// appParseProposal reads the one JSON object in an answer.
func appParseProposal(answer string) (appwire.Proposal, bool) {
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end <= start {
		return appwire.Proposal{}, false
	}
	var p appwire.Proposal
	if err := json.Unmarshal([]byte(answer[start:end+1]), &p); err != nil {
		return appwire.Proposal{}, false
	}
	p.Name = strings.TrimSpace(p.Name)
	var helpers []appwire.ProposalHelper
	for _, h := range p.Helpers {
		h.Handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h.Handle), "@"))
		h.Brief = strings.TrimSpace(h.Brief)
		if teamstore.ValidHandle(h.Handle) == nil && h.Brief != "" {
			helpers = append(helpers, h)
		}
	}
	p.Helpers = helpers
	return p, p.Name != "" && len(helpers) > 0
}

// answerProposal is the person's answer to a proposal: make the crew, or keep
// the agent alone. Edit is the app's editor, and leaves the ask open.
func (b *appBackend) answerProposal(agent, id, option string) error {
	switch option {
	case appwire.ProposalCreate:
		_, err := b.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: id})
		return err
	case appwire.ProposalEdit:
		return nil
	case appwire.ProposalKeep:
		b.dropProposal(id, appwire.ProposalKeep)
		return nil
	}
	return appwire.Errorf(appwire.CodeBadParams, "a proposal is answered create, edit or keep")
}

// dropProposal forgets a proposal and closes its ask.
func (b *appBackend) dropProposal(id, option string) {
	_ = b.crewLayer().write(func(f *appCrewLayerFile) error {
		delete(f.Proposals, id)
		return nil
	})
	run := b.crews()
	var ask appwire.Ask
	run.locked(func() {
		ask = run.asks[appProposalAskPrefix+id]
		delete(run.asks, appProposalAskPrefix+id)
	})
	if ask.ID == "" {
		return
	}
	at := b.now()
	words := "Kept it one agent"
	if option == appwire.ProposalCreate {
		words = "Made the crew"
	}
	b.emit.Emit(appwire.EventAskClosed, appwire.AskClosed{Agent: ask.Agent, ID: ask.ID, By: appwire.ClosedByApp, Option: option, At: at})
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "r" + ask.ID, Agent: ask.Agent, Kind: appwire.EntryReceipt, At: at, Text: words + " · you · " + at.Format("15:04"),
	}})
}

// createFromProposal makes the proposed crew: the agent its manager, the
// agents it named as members, and each helper the manager's start.
func (b *appBackend) createFromProposal(ask appwire.CrewCreate) (appwire.Crew, error) {
	var p appwire.Proposal
	found := false
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		if got := f.Proposals[ask.Proposal]; got != nil {
			p, found = *got, true
		}
	})
	if !found {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeNotFound, "there is no proposal %s", ask.Proposal)
	}
	if e := ask.Edits; e != nil {
		if strings.TrimSpace(e.Name) != "" {
			p.Name = strings.TrimSpace(e.Name)
		}
		if e.Helpers != nil {
			p.Helpers = e.Helpers
		}
		if e.Existing != nil {
			p.Existing = e.Existing
		}
		if e.Hue != nil {
			p.Hue = e.Hue
		}
		if e.CapUSDDay != nil {
			p.CapUSDDay = e.CapUSDDay
		}
		if e.Goal != "" {
			p.Goal = e.Goal
		}
		if e.EndsAt != nil {
			p.EndsAt = e.EndsAt
		}
	}
	manager, err := b.agentMember(p.Agent)
	if err != nil {
		return appwire.Crew{}, err
	}
	members := []teamstore.Member{manager}
	for _, agent := range p.Existing {
		m, err := b.agentMember(agent)
		if err != nil {
			return appwire.Crew{}, err
		}
		members = append(members, m)
	}
	hue, err := b.askedHue(p.Hue, "")
	if err != nil {
		return appwire.Crew{}, err
	}
	id, err := b.newTeam(p.Name, "", members, manager.Key, hue, nil, p.CapUSDDay)
	if err != nil {
		return appwire.Crew{}, err
	}
	b.crewExtraOnCreate(id, p.Agent, p.Goal, p.EndsAt)
	err = b.crewLayer().write(func(f *appCrewLayerFile) error {
		for _, h := range p.Helpers {
			if h.Role == "" && h.Model == "" {
				continue
			}
			role := ""
			if h.Role != "" {
				var found *appwire.Role
				for _, r := range f.Roles {
					if r != nil && strings.EqualFold(r.Name, h.Role) {
						found = r
					}
				}
				if found == nil {
					if f.Roles == nil {
						f.Roles = map[string]*appwire.Role{}
					}
					found = &appwire.Role{ID: f.next("role"), Name: h.Role, Model: h.Model, MadeAt: b.now()}
					f.Roles[found.ID] = found
				}
				role = found.ID
			}
			if f.Pending == nil {
				f.Pending = map[string]appPendingStart{}
			}
			f.Pending[appPendingKey(id, h.Handle)] = appPendingStart{Role: role, Model: h.Model}
		}
		return nil
	})
	if err != nil {
		return appwire.Crew{}, appEngineError(err)
	}
	// The watch performs these starts as it performs any manager's: it is told
	// to read this new crew's log from its first line.
	run := b.crews()
	run.locked(func() { run.cursors[id] = appTrafficStart })
	for _, h := range p.Helpers {
		if err := teamstore.AppendTraffic(b.crewProfile(), id, teamstore.Entry{
			Kind: teamstore.KindStart, From: teamstore.FromManager, FromKey: manager.Key, To: h.Handle, Text: redact.Secrets(h.Brief),
		}); err != nil {
			return appwire.Crew{}, appEngineError(err)
		}
	}
	b.crewReceipt(p.Agent, id, p.Name)
	b.dropProposal(p.ID, appwire.ProposalCreate)
	b.crewTick(run, false)
	return b.CrewGet(id)
}

// MemberBrief is what a manager started a member with: the text of the start
// line its crew's log holds for that handle, read on demand for one member. A
// member nobody started, or one whose start line is gone, has none; nothing is
// inferred from its conversation.
func (b *appBackend) MemberBrief(crew, agent string) (appwire.MemberBrief, error) {
	t, _, err := b.crewTeam(crew)
	if err != nil {
		return appwire.MemberBrief{}, err
	}
	m, ok := b.crewMemberOf(t, agent)
	if !ok {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
	}
	if !m.Started || strings.TrimSpace(m.Handle) == "" {
		return appwire.MemberBrief{}, nil
	}
	entries, err := teamstore.ReadTraffic(b.crewProfile(), t.ID, "", 0)
	if err != nil {
		return appwire.MemberBrief{}, appEngineError(err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != teamstore.KindStart || e.From != teamstore.FromManager || e.To != m.Handle {
			continue
		}
		if e.Team != "" && e.Team != t.ID {
			continue
		}
		at := e.At
		from := ""
		if boss, ok := t.Member(e.FromKey); ok {
			from = boss.Handle
		}
		return appwire.MemberBrief{Text: redact.Secrets(strings.TrimSpace(e.Text)), From: from, At: &at}, nil
	}
	return appwire.MemberBrief{}, nil
}
