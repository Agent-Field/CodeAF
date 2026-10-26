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
// A PROPOSAL is the agent's own: the app asks its model, beside its chat and
// never in it, for complete JSON lines naming the crew and the helpers it would
// start, and the answer becomes an ask — the crew proposal, which is all the
// person sees of it. Accepting it makes the team with the agent as
// manager and writes each helper's start on the crew's log as the manager's
// own line, its brief the manager's own words from the proposal; the app then
// performs those starts exactly as it performs any manager's.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

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
	// The brief is the person's message to the crew's new member, accepted for
	// this crew: it states the crew's exact id, as a crew message does.
	if _, err := b.send(agent.ID, brief, "", appStated(brief, appInTeam(t.ID))); err != nil {
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
	if a, err := b.find(agent); err == nil {
		if err := b.lentFence(a, "model and instructions"); err != nil {
			return err
		}
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
	// The role's instructions are words the app wrote, not the person's
	// message: they state no creation context, and stay unknown.
	_, err := b.queue(agent, "Your role in "+name+" is "+role.Name+".\n\n"+role.Instructions, "", nil)
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
	// A ROLE THAT A LENT STAGE CARRIES CANNOT CHANGE: its model and instructions
	// would be pushed onto the stage, so the edit is refused before it is saved.
	var carriers []string
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for key, m := range f.Members {
			if m != nil && m.Role == id {
				_, agent, _ := strings.Cut(key, "/")
				carriers = append(carriers, agent)
			}
		}
	})
	for _, agent := range carriers {
		if a, err := b.find(agent); err == nil {
			if err := b.lentFence(a, "model and instructions"); err != nil {
				return appwire.Role{}, err
			}
		}
	}
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

// appProposalAsk is what the app asks an agent's model for when the person
// asks for a crew (crews.propose). It is the app's instruction and never the person's
// words, so it is asked as a side ask ([appSideAsker]) and is never shown.
var appProposalAsk = fmt.Sprintf("Propose a crew for the work in this conversation: the helpers you would start and manage. "+
	"Choose the smallest crew that covers the work, with 1–3 helpers by default. "+
	"Use teams (an org) only when the work splits into separate streams that each need more than one person. "+
	"Use at most two levels below the manager: manager → team leads → members. "+
	"Handles are unique short role words, %d–%d characters using only a–z, 0–9 and hyphens, starting with a letter or digit. Never exceed %d characters. Reserve manager, you, system, everyone and room. "+
	"Reply only with NDJSON, one complete JSON object per line, in this order:\n"+
	`{"t":"shape","size":3,"teams":1,"why":"why this shape"}`+" (size counts helpers).\n"+
	`{"t":"name","name":"the crew's name","goal":"one line"}`+"\n"+
	`{"t":"team","id":"stream-id","name":"team name","lead":"lead-handle"}`+" (only for an org with 2+ teams).\n"+
	`{"t":"member","handle":"role-word","role":"short role name","brief":"whole assignment","team":"stream-id","reports_to":"manager"}`+"\n"+
	"Write members top-down, breadth first. reports_to is manager or an earlier team lead's handle; omit team for a single crew. "+
	"Do not start anyone yet; the person decides.", teamstore.HandleMin, teamstore.HandleMax, teamstore.HandleMax)

// appNoProposal is the quiet line a conversation carries when what came back
// could not be read as a proposal, and appNoAnswer leads the line when nothing
// came back at all, before the reason the ask failed.
const (
	appNoProposal   = "No crew proposal came back in a form the app can read"
	appNoAnswer     = "No crew proposal came back"
	appNoProposalID = "crewprop-none-"
)

type appStreamingSideAsker interface {
	SideAskStream(context.Context, string, func(string)) (string, error)
}

// appSideAsker asks the conversation's own model without starting a turn.
type appSideAsker interface {
	SideAsk(ctx context.Context, question string) (string, error)
}

// CrewPropose asks the agent for a crew proposal; its answer arrives as an ask.
//
// THE QUESTION IS ASKED BESIDE THE CHAT AND NEVER IN IT. It was once sent as a
// turn, and the person then read the app's instruction as a message of their
// own and the model's JSON as its answer — live, on every reread and after a
// relaunch — and the model carried the exchange into every turn after it. A
// turn hidden after the fact has to be hidden from the stream, the listing,
// the title, a retry and every other reader of the journal; a side ask writes
// nothing to hide, starts no turn and leaves the model's context as it was. So
// the only thing the person sees is the proposal's ask, or one quiet line
// when no proposal could be read.
//
// One ask runs per agent at a time: a second press while the first is out is
// answered by the first's card.
func (b *appBackend) CrewPropose(agent string) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return err
	}
	if err := b.workAllowed(a); err != nil {
		return err
	}
	asker, ok := live.conn.Conv.(appSideAsker)
	if !ok {
		return appwire.Errorf(appwire.CodeEngine, "this agent cannot be asked for helpers outside its chat; update codeaf")
	}
	run := b.crews()
	if run == nil {
		return appwire.Errorf(appwire.CodeClosed, "the app is closing")
	}
	// THE DAY CAP IS CHECKED BEFORE THE MODEL IS ASKED, as it is before a turn
	// (startLocked): a side ask is paid for like one. At the cap the press is
	// answered in the chat's one quiet line, which is also what tells a window
	// waiting on the press that nothing more is coming, and the person is asked
	// about the cap (unless that ask is already open). A LIMIT NOBODY CAN READ
	// REFUSES THE ASK, as it refuses a turn: the press ends in the same quiet
	// line, saying why, and the model is not asked.
	now := b.now()
	limit, err := b.dayCap(now)
	spent := 0.0
	if err == nil && limit > 0 {
		spent, err = b.spentTodayChecked(now)
	}
	if err != nil {
		b.noProposal(agent, appNoAnswer+" · "+appDetail(redact.Secrets(err.Error())))
		return nil
	}
	if limit > 0 && spent >= limit {
		b.noProposal(agent, appNoAnswer+" · Day cap reached · "+appUSD(spent)+" of "+appUSD(limit))
		b.askDay(a, live, spent, limit, "")
		return nil
	}
	first := false
	run.locked(func() {
		if run.proposing == nil {
			run.proposing = map[string]bool{}
		}
		first = !run.proposing[agent]
		run.proposing[agent] = true
	})
	if !first {
		return nil
	}
	guard.Go("app/crew-propose", func() {
		// The press is free again the moment the model has answered, before
		// the card is drawn, so a press made after the card is a new ask; the
		// deferred call is for a road that never reaches that moment.
		answered := sync.OnceFunc(func() { run.locked(func() { delete(run.proposing, agent) }) })
		defer answered()
		ctx, cancel := context.WithTimeout(context.Background(), session.SideAskPatience)
		defer cancel()
		var answer string
		var err error
		var stream *appProposalStream
		if streaming, ok := live.conn.Conv.(appStreamingSideAsker); ok {
			stream = &appProposalStream{emit: func(seq int, line json.RawMessage) {
				b.emit.Emit(appwire.EventCrewProposalDelta, appwire.CrewProposalDelta{Agent: agent, Seq: seq, Line: line})
			}}
			answer, err = streaming.SideAskStream(ctx, appProposalAsk, stream.write)
			if stream.received == 0 {
				stream.write(answer)
			}
			stream.finish(err == nil)
		} else {
			answer, err = asker.SideAsk(ctx, appProposalAsk)
		}
		answered()
		// The conversation was in use for as long as its model was being asked,
		// so the idle sweep counts from the answer and not from the press.
		b.locked(func() { live.lastUse = b.now() })
		if stream != nil && len(stream.proposal.Helpers) > 0 {
			if err != nil || stream.incomplete {
				word := "helpers"
				if len(stream.proposal.Helpers) == 1 {
					word = "helper"
				}
				stream.proposal.Note = fmt.Sprintf("Stopped after %d %s", len(stream.proposal.Helpers), word)
			}
			if stream.proposal.Name == "" {
				stream.proposal.Name = "Proposed crew"
			}
			b.openProposal(agent, stream.proposal)
			return
		}
		if err != nil {
			b.noProposal(agent, appNoAnswer+" · "+appDetail(redact.Secrets(err.Error())))
			return
		}
		b.proposalAnswered(agent, answer)
	})
	return nil
}

// noProposal says, quietly and in the agent's own conversation, that no
// proposal came of a press. Its id opens with appNoProposalID, which is how a
// window knows the press it is waiting on has been answered.
func (b *appBackend) noProposal(agent, words string) {
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: appNoProposalID + b.now().Format("150405"), Agent: agent, Kind: appwire.EntryNotice, At: b.now(),
		Text: words, Quiet: true,
	}})
}

// proposalAnswered reads the agent's answer as a proposal and asks the person.
func (b *appBackend) proposalAnswered(agent, answer string) {
	p, ok := appParseProposal(answer)
	if !ok {
		b.noProposal(agent, appNoProposal)
		return
	}
	b.openProposal(agent, p)
}

func (b *appBackend) openProposal(agent string, p appwire.Proposal) {
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
	if run == nil {
		return
	}
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
	if p.Note != "" {
		detail = append(detail, p.Note)
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

// appParseProposal reads NDJSON or an older one-object proposal.
func appParseProposal(answer string) (appwire.Proposal, bool) {
	stream := &appProposalStream{}
	stream.write(answer)
	stream.finish(true)
	if len(stream.proposal.Helpers) > 0 {
		if stream.proposal.Name == "" {
			stream.proposal.Name = "Proposed crew"
		}
		return stream.proposal, true
	}
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
	if run == nil {
		return
	}
	var ask appwire.Ask
	run.locked(func() {
		ask = run.asks[appProposalAskPrefix+id]
		delete(run.asks, appProposalAskPrefix+id)
	})
	if ask.ID == "" {
		return
	}
	at := b.now()
	b.emit.Emit(appwire.EventAskClosed, appwire.AskClosed{Agent: ask.Agent, ID: ask.ID, By: appwire.ClosedByApp, Option: option, At: at})
	// A MADE CREW'S RECEIPT IS ITS FORMATION FACT, said once by crewReceipt
	// under rcrew:<crew> and read back the same after a restart; a second
	// "Made the crew" here drew the card's end twice. A kept agent's receipt
	// says so in its own words.
	if option == appwire.ProposalCreate {
		return
	}
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "r" + ask.ID, Agent: ask.Agent, Kind: appwire.EntryReceipt, At: at, Text: appKeptOneAgent + " · you · " + at.Format("15:04"),
	}})
}

// appKeptOneAgent is the receipt a proposal leaves when the person keeps the
// agent alone.
const appKeptOneAgent = "Kept as one agent"

// createFromProposal makes the proposed crew: the agent its manager, the
// agents it named as members, and each helper the manager's start.
func (b *appBackend) createFromProposal(ask appwire.CrewCreate) (appwire.Crew, error) {
	run := b.crews()
	run.formation.Lock()
	defer run.formation.Unlock()
	var id string
	var completed bool
	var p appwire.Proposal
	found := false
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for crewID, extra := range f.Crews {
			if extra != nil && extra.Proposal != nil && extra.Proposal.ID == ask.Proposal {
				p, found, id, completed = *extra.Proposal, true, crewID, extra.ProposalDone
				return
			}
		}
		if got := f.Proposals[ask.Proposal]; got != nil {
			p, found = *got, true
		}
	})
	if !found {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeNotFound, "there is no proposal %s", ask.Proposal)
	}
	if completed {
		b.dropProposal(p.ID, appwire.ProposalCreate)
		return b.CrewGet(id)
	}
	a, err := b.find(p.Agent)
	if err != nil {
		return appwire.Crew{}, err
	}
	a.gate.Lock()
	admissionErr := b.workAllowed(a)
	a.gate.Unlock()
	if admissionErr != nil {
		return appwire.Crew{}, admissionErr
	}
	if e := ask.Edits; e != nil && id == "" {
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
	reserved := map[string]bool{}
	for _, helper := range p.Helpers {
		if err := teamstore.ValidHandle(helper.Handle); err != nil {
			return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "invalid helper handle %q: %s", helper.Handle, err)
		}
		if reserved[helper.Handle] {
			return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "helper @%s is proposed more than once", helper.Handle)
		}
		if strings.TrimSpace(helper.Brief) == "" {
			return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "helper @%s needs an assignment", helper.Handle)
		}
		reserved[helper.Handle] = true
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
	reservedTeam := teamstore.Team{}
	for _, helper := range p.Helpers {
		reservedTeam.Members = append(reservedTeam.Members, teamstore.Member{Key: "proposal:" + helper.Handle, Handle: helper.Handle})
	}
	for i := range members {
		member := &members[i]
		member.Handle = teamstore.DeriveHandle(member.Word)
		if teamstore.ValidHandle(member.Handle) != nil || reserved[member.Handle] {
			reservedTeam.Name = member.Word
			member.Handle = teamstore.FallbackHandle(reservedTeam, member.Key)
		}
		member.HandleBy = teamstore.HandleByWords
		reserved[member.Handle] = true
		reservedTeam.Members = append(reservedTeam.Members, *member)
	}
	manager = members[0]
	hue, err := b.askedHue(p.Hue, "")
	if err != nil {
		return appwire.Crew{}, err
	}
	if id == "" {
		id = teamstore.NewID()
		if err := b.crewLayer().write(func(f *appCrewLayerFile) error { f.crew(id).Proposal = &p; return nil }); err != nil {
			return appwire.Crew{}, appEngineError(err)
		}
	}
	if team, _, lookupErr := b.crewTeam(id); lookupErr == nil {
		for _, member := range team.Members {
			if member.Key == team.Manager {
				manager = member
			}
		}
	} else {
		if err := b.newTeamAs(id, p.Name, "", members, manager.Key, hue, nil, p.CapUSDDay); err != nil {
			if clearErr := b.crewLayer().write(func(f *appCrewLayerFile) error { delete(f.Crews, id); return nil }); clearErr != nil {
				return appwire.Crew{}, appEngineError(clearErr)
			}
			return appwire.Crew{}, err
		}
		b.crewExtraOnCreate(id, p.Agent, p.Goal, p.EndsAt)
	}
	err = b.crewLayer().write(func(f *appCrewLayerFile) error {
		for _, h := range p.Helpers {
			if h.Role == "" && h.Model == "" && h.ReportsTo == "" {
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
			f.Pending[appPendingKey(id, h.Handle)] = appPendingStart{Role: role, Model: h.Model, ReportsTo: h.ReportsTo}
		}
		return nil
	})
	if err != nil {
		return appwire.Crew{}, appEngineError(err)
	}
	run.locked(func() { run.cursors[id] = appTrafficStart })
	entries, err := teamstore.ReadTraffic(b.crewProfile(), id, "", 0)
	if err != nil {
		return appwire.Crew{}, appEngineError(err)
	}
	for _, helper := range p.Helpers {
		var entry teamstore.Entry
		for _, previous := range entries {
			if previous.Kind == teamstore.KindStart && previous.FromKey == manager.Key && previous.To == helper.Handle {
				entry = previous
				break
			}
		}
		if entry.ID == "" {
			entry = teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, FromKey: manager.Key, To: helper.Handle, Text: redact.Secrets(helper.Brief), At: b.now()}
			start, err := teamstore.AppendClaimedStart(b.crewProfile(), id, entry)
			if err != nil {
				return appwire.Crew{}, appEngineError(err)
			}
			entry.ID = start
		}
		team, _, err := b.crewTeam(id)
		if err != nil {
			return appwire.Crew{}, err
		}
		seated := ""
		for _, member := range team.Members {
			if member.StartEntry == entry.ID {
				seated = appMemberAgent(member)
				break
			}
		}
		if seated != "" {
			if err := b.crewLayer().write(func(f *appCrewLayerFile) error {
				pending := f.Pending[appPendingKey(id, helper.Handle)]
				f.member(id, seated).Role = pending.Role
				b.roleCopyLocked(f, pending.Role)
				delete(f.Pending, appPendingKey(id, helper.Handle))
				return nil
			}); err != nil {
				return appwire.Crew{}, appEngineError(err)
			}
		} else {
			if err := b.crewStart(team, manager, entry); err != nil {
				return appwire.Crew{}, appEngineError(err)
			}
		}
	}
	if err := b.crewLayer().write(func(f *appCrewLayerFile) error { f.crew(id).ProposalDone = true; return nil }); err != nil {
		return appwire.Crew{}, appEngineError(err)
	}

	b.crewReceipt(p.Agent, id, p.Name)
	b.dropProposal(p.ID, appwire.ProposalCreate)
	b.crewTick(run, false)
	return b.CrewGet(id)
}

// MemberBrief is a member's brief: the start line that made this very
// membership, read by its recorded id (teamstore.StartFor), so a renamed member
// keeps its own brief and a later member under its old handle does not receive
// it; or, when it was changed since, the newest change, with every change kept
// on the membership (teamstore.ReviseBrief). A member nobody started and
// nobody briefed has none; nothing is inferred from its conversation or from
// who manages the crew now.
func (b *appBackend) MemberBrief(crew, agent string) (appwire.MemberBrief, error) {
	t, _, err := b.crewTeam(crew)
	if err != nil {
		return appwire.MemberBrief{}, err
	}
	m, ok := b.crewMemberOf(t, agent)
	if !ok {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
	}
	return b.memberBrief(t, m)
}

func (b *appBackend) memberBrief(t teamstore.Team, m teamstore.Member) (appwire.MemberBrief, error) {
	var out appwire.MemberBrief
	// A FACTORY STAGE'S BRIEF IS THE ONE THE FACTORY RECORDED when it started
	// the stage: no manager started it, so no sender is named. A stage recovered
	// from an old build may still carry edits a person made BEFORE it was
	// flagged, and the stage read them, so they are shown below like any other.
	if m.FactoryStage {
		out.FactoryStage = true
		out.Text = redact.Secrets(strings.TrimSpace(m.FactoryBrief))
	} else {
		e, ok, err := teamstore.StartFor(b.crewProfile(), t, m)
		if err != nil {
			return appwire.MemberBrief{}, appEngineError(err)
		}
		if ok {
			at := e.At
			if boss, ok := t.Member(e.FromKey); ok {
				out.From = boss.Handle
			}
			out.Text, out.At = redact.Secrets(strings.TrimSpace(e.Text)), &at
		}
	}
	for _, r := range m.BriefRevisions() {
		out.Revisions = append(out.Revisions, appwire.BriefRevision{
			Text: redact.Secrets(r.Text), Before: redact.Secrets(r.Before), By: appBriefAuthor(t, r), At: r.At, Post: r.Entry,
		})
	}
	if cur, ok := m.CurrentBrief(); ok {
		at := cur.At
		out.Text, out.At = redact.Secrets(cur.Text), &at
	}
	return out, nil
}

// appBriefAuthor names who changed a brief: "you", or the handle the changer
// has in this crew now (the manager's seat by the manager's handle).
func appBriefAuthor(t teamstore.Team, r teamstore.BriefRevision) string {
	if r.By == teamstore.FromYou {
		return "you"
	}
	if r.ByKey != "" {
		if m, ok := t.Member(r.ByKey); ok && m.Handle != "" {
			return m.Handle
		}
	}
	if r.By == teamstore.FromManager {
		if m, ok := t.Member(t.Manager); ok && m.Handle != "" {
			return m.Handle
		}
	}
	return r.By
}

// MemberBriefSet changes a member's brief in place, as the person: the member
// reads the new words at its next step. Nothing is posted to its conversation,
// nothing is restarted, and a role the member carries is left as it is: the
// brief replaces the one it was started with, never its role. base, when set,
// is the revision the person edited (its post, "" for a brief never changed);
// a brief changed since is refused as a conflict and nothing is written.
func (b *appBackend) MemberBriefSet(crew, agent, text string, base *string) (appwire.MemberBrief, error) {
	t, _, err := b.crewTeam(crew)
	if err != nil {
		return appwire.MemberBrief{}, err
	}
	// REFUSED BEFORE ANYTHING ELSE, closed team or not: the job owns the brief.
	if m, ok := b.crewMemberOf(t, agent); ok && m.FactoryStage {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeBadParams, "%s is a factory stage; its brief belongs to the job and cannot be changed here", m.Handle)
	}
	if t.Closed() {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeClosed, "%s is closed; its briefs are read-only", t.Name)
	}
	m, ok := b.crewMemberOf(t, agent)
	if !ok {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
	}
	a, err := b.find(agent)
	if err != nil {
		return appwire.MemberBrief{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.workAllowed(a); err != nil {
		return appwire.MemberBrief{}, err
	}
	if m.Key == t.Manager {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeBadParams, "%s manages %s; a manager has no brief to change here", m.Handle, t.Name)
	}
	// The words it replaces, when it was never changed: its start line.
	before := ""
	if _, has := m.CurrentBrief(); !has {
		e, ok, err := teamstore.StartFor(b.crewProfile(), t, m)
		if err != nil {
			return appwire.MemberBrief{}, appEngineError(err)
		}
		if ok {
			before = e.Text
		}
	}
	change := teamstore.BriefChange{By: teamstore.FromYou, Text: redact.Secrets(text), Before: before, Base: base, At: b.now()}
	if _, err := teamstore.ReviseBrief(b.crewProfile(), t.ID, m.Key, change); err != nil {
		switch {
		case errors.Is(err, teamstore.ErrBriefUnchanged), errors.Is(err, teamstore.ErrBriefTooLong), errors.Is(err, teamstore.ErrBriefFactoryStage):
			return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		case errors.Is(err, teamstore.ErrBriefStale):
			return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeConflict, "%s's brief was changed since you opened it; read it again", m.Handle)
		case errors.Is(err, teamstore.ErrClosed):
			return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeClosed, "%s is closed; its briefs are read-only", t.Name)
		case errors.Is(err, teamstore.ErrBriefNoMember):
			return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
		}
		return appwire.MemberBrief{}, appEngineError(err)
	}
	t, _, err = b.crewTeam(crew)
	if err != nil {
		return appwire.MemberBrief{}, err
	}
	m, ok = b.crewMemberOf(t, agent)
	if !ok {
		return appwire.MemberBrief{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", agent, t.Name)
	}
	return b.memberBrief(t, m)
}
