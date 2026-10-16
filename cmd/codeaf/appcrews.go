package main

// ── codeaf app: CREWS ARE TEAMS ─────────────────────────────────────────────
//
// A crew IS a codeaf team (internal/teams), read from and written to the same
// store the terminal's Teams page uses: the teams file, each team's Traffic
// log, the decision packets and the usage ledger. Nothing here keeps a second
// account of a team. What the design asks for and teams do not keep is the
// app layer's (appcrewlayer.go), and is never written into the teams file.
//
// THE APP DOES WHAT THE TERMINAL WINDOW DOES FOR THE TEAMS IT HOLDS
// (internal/tui3's teamtraffic.go): a manager's `start` line is a new
// conversation it opens in the crew's folder and adds under the manager's
// handle, and a manager's `stop` line is the member's own stop. It acts only on
// lines that arrive while it watches, never on history, and only while it
// holds the manager. Every member of an open crew stays held while the crew is
// open, so a member woken by its manager is a turn on the wire
// (appwakes.go) and never a conversation nobody is watching.
//
// A decision packet waiting on the person is also an ask on the manager that
// brought it to them, so it reaches the dock and the Inbox like any question;
// answering either decides the packet through the store's own door.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// The ids a crew's asks carry: a decision packet, and an agent's proposal.
const (
	appCrewAskPrefix     = "crew:"
	appProposalAskPrefix = "crewprop:"
)

// appTrafficStart is a cursor before a log's first line.
var appTrafficStart = strings.Repeat("0", 12)

// appCrewLogPage bounds one read of a crew's log.
const appCrewLogPage = 200

// The named tints and the hue each stands for, in OKLCH degrees.
var appTints = []struct {
	name string
	hue  float64
}{{"rose", 15}, {"sand", 75}, {"sage", 145}, {"tide", 215}, {"iris", 285}}

// appTintGraphite is the root crew's tint, and an uncoloured crew's.
const appTintGraphite = "graphite"

// appTintOf is the named tint nearest a hue.
func appTintOf(t teamstore.Team) string {
	if t.Root || !t.Hued() {
		return appTintGraphite
	}
	best, gap := appTintGraphite, math.Inf(1)
	for _, tint := range appTints {
		if g := teamstore.HueGap(t.Hue, tint.hue); g < gap {
			best, gap = tint.name, g
		}
	}
	return best
}

// appTintHue is a named tint's hue.
func appTintHue(name string) (float64, bool) {
	for _, tint := range appTints {
		if tint.name == name {
			return tint.hue, true
		}
	}
	return 0, false
}

// appCrewRun is the app's watch over the crews: what it last said of each,
// where it is in each log, the packets waiting on the person, the asks they
// are, and which agents an open crew keeps held.
type appCrewRun struct {
	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	watch   teamstore.Watch
	prints  map[string]string
	members map[string]string
	cursors map[string]string
	acted   map[string]bool
	packets map[string]teamstore.Packet
	asks    map[string]appwire.Ask
	held    map[string][]string
	opening map[string]bool
}

func (c *appCrewRun) locked(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
}

// crewProfile is the profile whose teams the app reads.
func (b *appBackend) crewProfile() string {
	if b.profileDir != "" {
		return b.profileDir
	}
	return config.ProfileDir()
}

// crews starts the watch the first time anything asks for a crew, and answers it.
func (b *appBackend) crews() *appCrewRun {
	var run *appCrewRun
	start := false
	b.locked(func() {
		if b.crewRun == nil && !b.closing {
			b.crewRun = &appCrewRun{
				stop: make(chan struct{}), done: make(chan struct{}),
				prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{},
				acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{},
				held: map[string][]string{}, opening: map[string]bool{},
			}
			start = true
		}
		run = b.crewRun
	})
	if start {
		b.crewTick(run, true)
		b.reopenProposals(run)
		guard.Go("app/crews", func() { b.crewLoop(run) })
	}
	return run
}

// reopenProposals asks again about every crew proposal the person had not
// answered when the app last closed: a proposal is kept in the app layer, so
// its ask outlives a restart.
func (b *appBackend) reopenProposals(run *appCrewRun) {
	var kept []appwire.Proposal
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for _, p := range f.Proposals {
			if p != nil {
				kept = append(kept, *p)
			}
		}
	})
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	for _, p := range kept {
		ask := appProposalAskOf(p)
		run.locked(func() { run.asks[ask.ID] = ask })
		b.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	}
}

// crewLoop looks at the crews every beat until the app closes.
func (b *appBackend) crewLoop(run *appCrewRun) {
	defer close(run.done)
	ticker := time.NewTicker(appCrewPoll)
	defer ticker.Stop()
	for {
		select {
		case <-run.stop:
			return
		case <-ticker.C:
			b.crewTick(run, false)
		}
	}
}

// crewStop ends the watch.
func (run *appCrewRun) close() {
	close(run.stop)
	<-run.done
}

// appCrewRead is one look at the store: the teams, the defaults, the layer,
// and the packets waiting on the person.
type appCrewRead struct {
	file     *teamstore.File
	defaults teamstore.Defaults
	layer    appCrewLayerFile
	waiting  []teamstore.Packet
}

func (b *appBackend) crewRead() (appCrewRead, error) {
	profile := b.crewProfile()
	file, err := teamstore.Load(profile)
	if err != nil {
		return appCrewRead{}, err
	}
	read := appCrewRead{file: file, defaults: teamstore.DefaultsAt(profile)}
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		raw, _ := json.Marshal(f)
		_ = json.Unmarshal(raw, &read.layer)
	})
	read.waiting, _, _ = teamstore.OpenPackets(profile, teamstore.Person)
	return read, nil
}

// crewTick is one look: every crew said again where it changed, every new log
// line said and acted on, every packet on the person made an ask or closed,
// and every member of an open crew held.
func (b *appBackend) crewTick(run *appCrewRun, first bool) {
	read, err := b.crewRead()
	if err != nil {
		return
	}
	views := map[string]appwire.Crew{}
	held := map[string][]string{}
	for _, t := range read.file.Teams {
		view := b.crewView(read, t)
		views[t.ID] = view
		if !t.Closed() {
			for _, m := range view.Members {
				held[m.Agent] = append(held[m.Agent], t.ID)
			}
		}
	}
	var changed []appwire.Crew
	var removed []string
	var members []appwire.CrewMember
	var moved []string
	run.locked(func() {
		for id, view := range views {
			raw, _ := json.Marshal(view)
			if run.prints[id] != string(raw) {
				run.prints[id] = string(raw)
				if !first {
					changed = append(changed, view)
				}
			}
			for _, m := range view.Members {
				raw, _ := json.Marshal(m)
				key := appMemberKey(id, m.Agent)
				if run.members[key] != string(raw) {
					run.members[key] = string(raw)
					if !first {
						members = append(members, m)
					}
				}
			}
		}
		for id := range run.prints {
			if _, still := views[id]; !still {
				delete(run.prints, id)
				removed = append(removed, id)
			}
		}
		if !first {
			moved = appMembershipMoved(run.held, held)
		}
		run.held = held
	})
	sort.Slice(changed, func(i, j int) bool { return changed[i].ID < changed[j].ID })
	for _, view := range changed {
		b.emit.Emit(appwire.EventCrewChanged, appwire.CrewChanged{Crew: view})
	}
	for _, m := range members {
		b.emit.Emit(appwire.EventMemberChanged, appwire.MemberChanged{Member: m})
	}
	for _, id := range removed {
		b.emit.Emit(appwire.EventCrewRemoved, appwire.CrewRemoved{ID: id})
	}
	// AN AGENT WHOSE CREWS CHANGED IS SAID AGAIN, so its row's crews are
	// never older than the crews' own.
	for _, id := range moved {
		var a *appAgent
		b.locked(func() { a = b.agents[id] })
		if a != nil {
			b.sayAgent(a)
		}
	}
	for _, t := range read.file.Teams {
		if !t.Closed() {
			b.crewTraffic(run, read, t)
		}
	}
	b.crewPackets(run, read, first)
	b.holdMembers(run, held)
}

// appMembershipMoved is every agent whose open crews differ between two looks.
func appMembershipMoved(before, after map[string][]string) []string {
	var moved []string
	for id, crews := range after {
		if strings.Join(crews, ",") != strings.Join(before[id], ",") {
			moved = append(moved, id)
		}
	}
	for id := range before {
		if _, still := after[id]; !still {
			moved = append(moved, id)
		}
	}
	sort.Strings(moved)
	return moved
}

// ── the view ────────────────────────────────────────────────────────────────

// crewView is one team as a crew.
func (b *appBackend) crewView(read appCrewRead, t teamstore.Team) appwire.Crew {
	f := read.file
	view := appwire.Crew{
		ID: t.ID, Name: t.Name, Parent: t.Parent, Root: t.Root, Hue: t.Hue, Tier: t.Tier, Tint: appTintOf(t),
		ClosedWith: t.ClosedWith, Report: t.Report, Members: []appwire.CrewMember{},
		Settings: appwire.CrewSettings{QuestionsUp: t.Settings.QuestionsUp, CapUSDDay: t.Settings.CapUSDDay, DepthLimit: t.Settings.DepthLimit, SubShare: t.Settings.SubShare, Wake: t.Settings.Wake},
	}
	if !t.Made.IsZero() {
		made := t.Made
		view.MadeAt = &made
	}
	if !t.ClosedAt.IsZero() {
		closed := t.ClosedAt
		view.ClosedAt = &closed
	}
	if t.Wrap != nil {
		view.Wrap = &appwire.CrewWrap{Started: t.Wrap.Started, BoundS: int(t.Wrap.Bound.Seconds())}
	}
	e := f.Effective(t.ID, read.defaults)
	origin := func(o teamstore.Origin) appwire.CrewOrigin {
		return appwire.CrewOrigin{Kind: o.Kind, Crew: o.Team, Name: o.Name}
	}
	view.Effective = appwire.CrewEffective{
		QuestionsUp: e.QuestionsUp, QuestionsUpFrom: origin(e.QuestionsUpFrom),
		CapUSDDay: e.CapUSDDay, CapFrom: origin(e.CapFrom),
		DepthLimit: e.DepthLimit, DepthFrom: origin(e.DepthFrom),
		SubShare: e.SubShare, SubShareFrom: origin(e.SubShareFrom),
		Wake: e.Wake, WakeFrom: origin(e.WakeFrom),
	}
	spend, _ := teamstore.TeamSpend(b.crewProfile(), t.ID, teamstore.Today())
	view.Spend = appwire.CrewSpend{Day: spend.Day, USD: spend.USD, Calls: spend.Calls}
	if len(spend.ByMember) > 0 {
		view.Spend.ByMember = map[string]float64{}
	}
	keys := map[string]string{}
	for _, m := range t.Members {
		keys[m.Key] = appMemberAgent(m)
	}
	for key, usd := range spend.ByMember {
		agent := keys[key]
		if agent == "" {
			agent = appAgentID(key)
		}
		view.Spend.ByMember[agent] += usd
	}
	if m, ok := t.Member(t.Manager); ok {
		view.Manager = appMemberAgent(m)
	}
	for _, m := range t.Members {
		view.Members = append(view.Members, b.memberView(read, t, m, view.Spend.ByMember))
	}
	for _, m := range t.FormerMembers {
		view.FormerMembers = append(view.FormerMembers, appwire.CrewMember{Agent: appMemberAgent(m), Crew: t.ID, Handle: m.Handle, Title: m.Word})
	}
	if t.Parent != "" {
		view.Brief = b.subCrewBrief(t)
	}
	if extra := read.layer.Crews[t.ID]; extra != nil {
		view.Goal, view.EndsAt, view.History = extra.Goal, extra.EndsAt, extra.History
		view.Items = append(view.Items, extra.Items...)
	}
	view.State = b.crewState(read, t, view)
	if view.State == appwire.CrewIdle || view.State == appwire.CrewNeedsYou {
		view.IdleSince = b.crewIdleSince(view.Members)
	}
	return view
}

// crewIdleSince is the instant since which no member of a crew has run: the
// latest instant any member stopped. A member at rest stopped when its
// conversation last changed (the end of its last turn); a member whose turn is
// open but stopped on a question that holds the turn stopped when the first
// such question still open was put. Nil when any member may still be running
// or any member's instant is unknown.
func (b *appBackend) crewIdleSince(members []appwire.CrewMember) *time.Time {
	var last time.Time
	known := len(members) > 0
	b.locked(func() {
		for _, m := range members {
			stopped := b.stoppedLocked(b.agents[m.Agent])
			if stopped.IsZero() {
				known = false
				return
			}
			if stopped.After(last) {
				last = stopped
			}
		}
	})
	if !known {
		return nil
	}
	return &last
}

// stoppedLocked is when an agent last stopped running, and the zero time when
// it runs, may run, or is not known. Callers hold b.mu.
func (b *appBackend) stoppedLocked(a *appAgent) time.Time {
	if a == nil || a.view.State == appwire.StateWorking {
		return time.Time{}
	}
	live := a.live
	if live == nil || !live.running {
		return a.view.UpdatedAt
	}
	// A TURN IS OPEN. It has stopped only on a question that holds the turn,
	// and it stopped when the first of those still open was put; anything
	// else may be running.
	var first time.Time
	for _, q := range live.questions {
		if q.Blocking.Turn && !q.Asked.IsZero() && (first.IsZero() || q.Asked.Before(first)) {
			first = q.Asked
		}
	}
	return first
}

// subCrewBrief is the words a sub-crew's manager was started with: the
// parent's start line that named this crew.
func (b *appBackend) subCrewBrief(t teamstore.Team) string {
	entries, err := teamstore.ReadTraffic(b.crewProfile(), t.Parent, "", 0)
	if err != nil {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if e := entries[i]; e.Kind == teamstore.KindStart && e.Team == t.ID {
			return redact.Secrets(strings.TrimSpace(e.Text))
		}
	}
	return ""
}

// crewState is a crew's one word: closed, waiting on the person, working, or
// idle.
func (b *appBackend) crewState(read appCrewRead, t teamstore.Team, view appwire.Crew) string {
	if t.Closed() {
		return appwire.CrewClosed
	}
	for _, p := range read.waiting {
		if p.Origin == t.ID {
			return appwire.CrewNeedsYou
		}
	}
	working := false
	for _, m := range view.Members {
		switch m.State {
		case teamstore.StateAsking:
			return appwire.CrewNeedsYou
		case teamstore.StateRunning:
			working = true
		}
	}
	if working {
		return appwire.CrewWorking
	}
	return appwire.CrewIdle
}

// memberView is one member as the crew shows it.
func (b *appBackend) memberView(read appCrewRead, t teamstore.Team, m teamstore.Member, spent map[string]float64) appwire.CrewMember {
	agent := appMemberAgent(m)
	view := appwire.CrewMember{
		Agent: agent, Crew: t.ID, Handle: m.Handle, HandleBy: m.HandleBy, Title: m.Word, Workspace: m.Where,
		Started: m.Started, Home: m.Home, Independent: m.Independent, SpentUSD: spent[agent],
	}
	if !m.JoinedAt.IsZero() {
		joined := m.JoinedAt
		view.JoinedAt = &joined
	}
	if home, ok := read.file.Home(m.Key); ok {
		manager := ""
		if team, ok := read.file.Team(home.Team); ok {
			if mm, ok := team.Member(home.Manager); ok {
				manager = appMemberAgent(mm)
			}
		}
		view.ReportsTo = &appwire.CrewReportsTo{Crew: home.Team, Manager: manager, Distance: home.Distance}
	}
	for _, other := range read.file.Teams {
		if other.ID != t.ID && !other.Closed() && other.Holds(m.Key) {
			view.AlsoIn = append(view.AlsoIn, other.ID)
		}
		// A MEMBER THAT RUNS A CREW UNDER THIS ONE IS THAT CREW here, not a
		// plain member: it answers to this crew's manager for the crew it runs.
		if other.Parent == t.ID && !other.Closed() && other.Manager == m.Key {
			view.Leads = other.ID
		}
	}
	view.State, view.Model, view.Title, view.Line = b.memberLive(agent, view.Title)
	if usd, set := b.agentBudget(agent, b.now()); set {
		view.CapUSD = &usd
	}
	if extra := read.layer.Members[appMemberKey(t.ID, agent)]; extra != nil {
		view.Role, view.Paused, view.Was = extra.Role, extra.Paused, extra.Was
		if role := read.layer.Roles[extra.Role]; role != nil {
			view.Role = role.ID
		}
	}
	return view
}

// memberLive is a member's state, model and title as the app holds its agent:
// the engine's member words (running, asking, idle, failed).
func (b *appBackend) memberLive(agent, title string) (state, model, name, line string) {
	state, name = teamstore.StateIdle, title
	b.locked(func() {
		a := b.agents[agent]
		if a == nil {
			return
		}
		model = a.view.Model
		if a.view.Title != "" {
			name = a.view.Title
		}
		switch {
		case a.view.State == appwire.StateWorking:
			state = teamstore.StateRunning
		case a.view.State == appwire.StateNeedsYou:
			state = teamstore.StateAsking
		case a.failed != "":
			state = teamstore.StateFailed
		}
		line = agentLineLocked(a, a.view.State)
	})
	return state, model, name, line
}

// ── the log, and the manager's starts and stops ─────────────────────────────

// appLogLine is one Traffic line as the app says it.
func appLogLine(t teamstore.Team, e teamstore.Entry) appwire.LogLine {
	line := appwire.LogLine{
		ID: e.ID, Crew: t.ID, At: e.At, Kind: e.Kind, From: e.From, To: e.To, Text: redact.Secrets(e.Text),
		Handles: e.Handles, Answers: e.Answers, Files: e.Files, Member: e.Member, State: e.State,
		Packet: e.Packet, Reply: e.Reply, Team: e.Team,
	}
	key := e.Member
	if key == "" && e.From != "" {
		if m, ok := t.ByHandle(e.From); ok {
			key = m.Key
		}
	}
	if key != "" {
		if m, ok := t.Member(key); ok {
			line.Agent = appMemberAgent(m)
		} else {
			line.Agent = appAgentID(key)
		}
	}
	// THE LOG NAMES PEOPLE, NOT SEATS. A line is from and to a member by its
	// handle, the person ("you"), or everyone; the manager is the member that
	// holds the seat. A line codeaf wrote about a member is that member's.
	manager := teamstore.FromManager
	if m, ok := t.Member(t.Manager); ok && m.Handle != "" {
		manager = m.Handle
	}
	line.From = appLogParty(t, e.From, e.Member, manager)
	line.To = appLogParty(t, e.To, "", manager)
	// The manager's mark in a line codeaf wrote is the manager's handle.
	if e.Kind == teamstore.KindEvent || e.Kind == teamstore.KindPacket {
		line.Text = strings.ReplaceAll(line.Text, "◆", "@"+manager)
	}
	return line
}

// appLogParty is one end of a log line as the log names it.
func appLogParty(t teamstore.Team, word, member, manager string) string {
	switch word {
	case teamstore.FromManager:
		return manager
	case teamstore.ToRoom:
		return teamstore.ToEveryone
	case teamstore.FromSystem:
		if m, ok := t.Member(member); ok && m.Handle != "" {
			return m.Handle
		}
		return "codeaf"
	}
	return word
}

// crewTraffic says a crew's new log lines and acts on its manager's starts and
// stops. The first look at a crew only finds where its log ends.
func (b *appBackend) crewTraffic(run *appCrewRun, read appCrewRead, t teamstore.Team) {
	profile := b.crewProfile()
	var cursor string
	known := false
	run.locked(func() { cursor, known = run.cursors[t.ID] })
	if !known {
		tail, _, err := run.watch.Traffic(profile, t.ID, "", 1)
		if err != nil {
			return
		}
		cursor = appTrafficStart
		if len(tail) > 0 {
			cursor = tail[len(tail)-1].ID
		}
		run.locked(func() { run.cursors[t.ID] = cursor })
		return
	}
	entries, _, err := run.watch.Traffic(profile, t.ID, cursor, appCrewLogPage)
	if err != nil || len(entries) == 0 {
		return
	}
	run.locked(func() { run.cursors[t.ID] = entries[len(entries)-1].ID })
	for _, e := range entries {
		b.emit.Emit(appwire.EventCrewLog, appwire.CrewLogEvent{Line: appLogLine(t, e)})
		b.crewAct(run, read, t, e)
	}
}

// crewAct performs a manager's start or stop once, when the app holds the
// manager.
func (b *appBackend) crewAct(run *appCrewRun, read appCrewRead, t teamstore.Team, e teamstore.Entry) {
	if e.Kind != teamstore.KindStop && e.Kind != teamstore.KindStart {
		return
	}
	done := t.ID + "/" + e.ID
	acted := false
	run.locked(func() {
		acted = run.acted[done]
		run.acted[done] = true
	})
	if acted {
		return
	}
	switch e.Kind {
	case teamstore.KindStop:
		key := e.Member
		if key == "" || !t.Holds(key) {
			if m, ok := t.ByHandle(e.To); ok {
				key = m.Key
			}
		}
		m, ok := t.Member(key)
		if !ok || m.Independent {
			return
		}
		if home, ok := read.file.Home(key); ok && home.Team != t.ID {
			return
		}
		agent := appMemberAgent(m)
		if b.isHeld(agent) {
			_ = b.Stop(agent)
		}
	case teamstore.KindStart:
		manager, ok := t.Member(t.Manager)
		if !ok || !b.isHeld(appMemberAgent(manager)) {
			return
		}
		// ANOTHER WINDOW HOLDING THE SAME MANAGER — a terminal the app joined —
		// CARRIES IT OUT INSTEAD when it claims the line first (internal/teams'
		// startclaim.go). A member under the handle that joined after the line
		// is a start by a window from before the claim.
		if m, ok := t.ByHandle(e.To); ok && !m.JoinedAt.Before(e.At) {
			return
		}
		claimed := false
		if err := teamstore.Update(b.crewProfile(), func(f *teamstore.File) error {
			claimed = f.ClaimStart(t.ID, e.ID)
			return nil
		}); err != nil || !claimed {
			return
		}
		guard.Go("app/crew-start", func() { b.crewStart(t, manager, e) })
	}
}

// isHeld says the app holds the agent's conversation.
func (b *appBackend) isHeld(agent string) bool {
	held := false
	b.locked(func() { held = b.agents[agent] != nil && b.agents[agent].live != nil })
	return held
}

// appPostureDoor is a conversation whose approval posture can be read or set.
type appPostureDoor interface {
	ResolvedApprovalPosture() string
	SetApprovalPosture(string) error
}

// crewStart is a manager's start: a new conversation in the crew's folder,
// added under the manager's handle for it; the conversation reads its brief
// off the start line itself and takes its first turn.
func (b *appBackend) crewStart(t teamstore.Team, manager teamstore.Member, e teamstore.Entry) {
	handle := strings.TrimSpace(e.To)
	var pending appPendingStart
	_ = b.crewLayer().read(func(f *appCrewLayerFile) { pending = f.Pending[appPendingKey(t.ID, handle)] })
	where := appCrewWhere(t)
	agent, err := b.CreateAgent(appwire.CreateAsk{Workspace: where, Model: pending.Model})
	if err != nil {
		b.crewNote(manager, "could not start @"+handle+": "+err.Error())
		return
	}
	a, live, err := b.heldLive(agent.ID)
	if err != nil {
		b.crewNote(manager, "could not start @"+handle+": "+err.Error())
		return
	}
	// A STARTED MEMBER IS NAMED BY ITS BRIEF, the way a conversation a person
	// opens is named by its first message: the brief is its first message. The
	// conversation names its own record the same way when its turn reads the
	// brief; this is the name the list shows from the first moment.
	if name := session.FirstMessageName(e.Text); name != "" {
		b.locked(func() {
			if strings.TrimSpace(a.view.Title) == "" {
				a.view.Title = name
			}
		})
		b.sayAgent(a)
	}
	posture := strings.TrimSpace(e.Approval)
	if posture == "" {
		if _, mlive, err := b.heldLive(appMemberAgent(manager)); err == nil {
			if door, ok := mlive.conn.Conv.(appPostureDoor); ok {
				posture = door.ResolvedApprovalPosture()
			}
		}
	}
	if posture != "" {
		if door, ok := live.conn.Conv.(appPostureDoor); ok {
			_ = door.SetApprovalPosture(posture)
		}
	}
	member, err := b.agentMember(agent.ID)
	if err != nil {
		b.crewNote(manager, "could not start @"+handle+": "+err.Error())
		return
	}
	// A MEMBER A MANAGER STARTED HAS NO JOINING MOMENT: its brief is the start
	// line written before it existed, and a joining moment after that line
	// would hide the brief from it (the session reads nothing older than a
	// member's joining). The terminal's start writes none either.
	member.Handle, member.JoinedAt, member.Started = handle, time.Time{}, true
	// THE MEMBERSHIP KEEPS THE ID OF THE LINE THAT MADE IT, so its brief is
	// that line by identity and survives a rename or a handle reused later.
	// Only a manager's start of this crew is recorded; the terminal's start
	// writes the same field (internal/tui3/teamtraffic.go).
	if e.Kind == teamstore.KindStart && e.From == teamstore.FromManager && (e.Team == "" || e.Team == t.ID) {
		member.StartEntry = e.ID
	}
	err = teamstore.Update(b.crewProfile(), func(f *teamstore.File) error {
		if err := f.AddMember(t.ID, member); err != nil {
			return err
		}
		if team, ok := f.Team(t.ID); ok {
			if got, _ := team.Member(member.Key); got.Handle != handle && teamstore.ValidHandle(handle) == nil {
				_ = f.SetHandle(t.ID, member.Key, handle)
			}
		}
		return nil
	})
	if err != nil {
		b.crewNote(manager, "@"+handle+" opened but did not join "+t.Name+": "+err.Error())
		return
	}
	if pending.Role != "" {
		_ = b.crewLayer().write(func(f *appCrewLayerFile) error {
			f.member(t.ID, agent.ID).Role = pending.Role
			delete(f.Pending, appPendingKey(t.ID, handle))
			b.roleCopyLocked(f, pending.Role)
			return nil
		})
	}
}

// roleCopyLocked is nothing to do: a role's copies are read from the members
// that carry it. It is named so the place a copy is made reads as one.
func (b *appBackend) roleCopyLocked(*appCrewLayerFile, string) {}

// crewNote says something beside the manager, in its conversation's own lane.
func (b *appBackend) crewNote(manager teamstore.Member, text string) {
	agent := appMemberAgent(manager)
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "crew-note-" + strconv.FormatInt(b.now().UnixNano(), 36), Agent: agent, Kind: appwire.EntryNotice,
		At: b.now(), Text: redact.Secrets(text), Quiet: true,
	}})
}

// appCrewWhere is the folder a crew's new members open in: the manager's, else
// the one every member shares.
func appCrewWhere(t teamstore.Team) string {
	if m, ok := t.Member(t.Manager); ok && strings.TrimSpace(m.Where) != "" {
		return m.Where
	}
	where := ""
	for _, m := range t.Members {
		w := strings.TrimSpace(m.Where)
		if w == "" {
			continue
		}
		if where == "" {
			where = filepath.Clean(w)
		} else if filepath.Clean(w) != where {
			return ""
		}
	}
	return where
}

// agentMember is an agent as a team member: its conversation key and file,
// its folder and title.
func (b *appBackend) agentMember(agent string) (teamstore.Member, error) {
	a, err := b.find(agent)
	if err != nil {
		return teamstore.Member{}, err
	}
	var transcript, where, title string
	b.locked(func() { transcript, where, title = a.transcript, a.view.Workspace, a.view.Title })
	if strings.TrimSpace(transcript) == "" {
		return teamstore.Member{}, appwire.Errorf(appwire.CodeNotFound, "the conversation of %s has no file yet", agent)
	}
	key := filepath.Clean(transcript)
	if real, err := filepath.EvalSymlinks(key); err == nil {
		key = filepath.Clean(real)
	}
	return teamstore.Member{Key: key, File: transcript, Where: where, Word: title, JoinedAt: b.now()}, nil
}

// holdMembers keeps every member of an open crew held, opening one that is
// not, one at a time.
func (b *appBackend) holdMembers(run *appCrewRun, held map[string][]string) {
	for agent := range held {
		if b.isHeld(agent) {
			continue
		}
		start := false
		run.locked(func() {
			if !run.opening[agent] {
				run.opening[agent], start = true, true
			}
		})
		if !start {
			continue
		}
		guard.Go("app/crew-hold", func() {
			defer run.locked(func() { delete(run.opening, agent) })
			a, err := b.find(agent)
			if err != nil {
				return
			}
			// ONE agent.changed FOR AN AGENT THIS OPENED, as heldLive says it:
			// a list the app read while the agent was closed is otherwise
			// never told. A conversation already held opens nothing.
			if view, opened, err := b.ensure(a); err == nil && opened {
				b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
			}
		})
	}
}

// crewKeeps says an open crew keeps this agent held.
func (b *appBackend) crewKeeps(agent string) bool {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run == nil {
		return false
	}
	keeps := false
	run.locked(func() { keeps = len(run.held[agent]) > 0 })
	return keeps
}

// crewsOf is the open crews an agent is a member of.
func (b *appBackend) crewsOf(agent string) []string {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run == nil {
		return nil
	}
	var out []string
	run.locked(func() { out = append(out, run.held[agent]...) })
	sort.Strings(out)
	return out
}

// ── decisions ───────────────────────────────────────────────────────────────

// appDecisionOf is a packet as a decision, its path named by the crews it
// climbed through.
func appDecisionOf(f *teamstore.File, p teamstore.Packet) appwire.Decision {
	name := func(id string) string {
		if id == teamstore.Person {
			return "you"
		}
		if t, ok := f.Team(id); ok {
			return t.Name
		}
		return id
	}
	d := appwire.Decision{
		ID: p.ID, Crew: p.Origin, WaitsOn: p.Team, Kind: p.Kind, RaisedBy: p.RaisedBy, Question: redact.Secrets(p.Question),
		State: p.State, DecidedBy: p.DecidedBy, Decision: p.Decision, Reason: p.Reason, Raised: p.Raised, At: p.At,
	}
	for _, party := range p.Parties {
		dp := appwire.DecisionParty{Handle: party.Handle, Crew: party.Team, Context: redact.Secrets(party.Context)}
		if party.Key != "" {
			dp.Agent = appAgentID(party.Key)
		}
		d.Parties = append(d.Parties, dp)
	}
	for _, o := range p.Options {
		d.Options = append(d.Options, appwire.DecisionOption{ID: o.ID, Label: o.Label, Consequence: o.Consequence})
	}
	if p.Recommendation != nil {
		d.Recommendation = &appwire.DecisionPick{Option: p.Recommendation.Option, Reason: p.Recommendation.Reason}
	}
	if r := p.Report; r != nil {
		d.Report = &appwire.CrewReport{Done: r.Done, Left: r.Left, Files: r.Files, SpendUSD: r.SpendUSD, Incomplete: r.Incomplete}
	}
	if c := p.Cap; c != nil {
		d.Cap = &appwire.CrewCap{Crew: c.Team, Day: c.Day, CapUSD: c.CapUSD, SpentUSD: c.SpentUSD, RaiseTo: c.RaiseTo}
	}
	d.Path = []string{name(p.Origin)}
	for _, hop := range p.Trail {
		d.Trail = append(d.Trail, appwire.DecisionHop{From: hop.From, To: hop.To, By: hop.By, Reason: hop.Reason, At: hop.At})
		d.Path = append(d.Path, name(hop.To))
	}
	if d.Path[len(d.Path)-1] != name(p.Team) {
		d.Path = append(d.Path, name(p.Team))
	}
	return d
}

// decisionAgent is the agent a decision on the person is asked on: the
// manager of the crew that brought it up, else the crew it was raised in.
func decisionAgent(f *teamstore.File, p teamstore.Packet) string {
	from := p.Origin
	if n := len(p.Trail); n > 0 {
		from = p.Trail[n-1].From
	}
	for _, id := range []string{from, p.Origin} {
		if t, ok := f.Team(id); ok {
			if m, ok := t.Member(t.Manager); ok {
				return appMemberAgent(m)
			}
			if len(t.Members) > 0 {
				return appMemberAgent(t.Members[0])
			}
		}
	}
	return ""
}

// appDecisionAsk is a decision on the person as an ask.
func appDecisionAsk(f *teamstore.File, p teamstore.Packet) appwire.Ask {
	d := appDecisionOf(f, p)
	ask := appwire.Ask{
		ID: appCrewAskPrefix + p.ID, Agent: decisionAgent(f, p), Kind: appwire.AskQuestion, Subkind: appwire.SubkindCrewDecision,
		Title: d.Question, Path: d.Path, SayWhy: true, DecisionKind: d.Kind,
		Subject: &appwire.AskSubject{Kind: appwire.SubjectCrewDecision, Ref: p.ID},
	}
	if d.Recommendation != nil {
		ask.Reason = d.Recommendation.Reason
	}
	for _, o := range d.Options {
		ask.Options = append(ask.Options, appwire.AskOption{ID: o.ID, Label: o.Label})
		choice := appwire.AskChoice{ID: o.ID, Label: o.Label, Detail: o.Consequence}
		if d.Recommendation != nil && d.Recommendation.Option == o.ID {
			choice.Suggested = true
		}
		ask.Choices = append(ask.Choices, choice)
	}
	return ask
}

// crewPackets says each packet that began or stopped waiting on the person,
// as a decision and as an ask.
func (b *appBackend) crewPackets(run *appCrewRun, read appCrewRead, first bool) {
	now := map[string]teamstore.Packet{}
	for _, p := range read.waiting {
		now[p.ID] = p
	}
	var opened, gone []teamstore.Packet
	run.locked(func() {
		for id, p := range now {
			if _, had := run.packets[id]; !had {
				opened = append(opened, p)
			}
		}
		for id, p := range run.packets {
			if _, still := now[id]; !still {
				gone = append(gone, p)
			}
		}
		run.packets = now
	})
	for _, p := range opened {
		ask := appDecisionAsk(read.file, p)
		run.locked(func() { run.asks[ask.ID] = ask })
		if !first {
			b.emit.Emit(appwire.EventDecisionOpened, appwire.DecisionEvent{Decision: appDecisionOf(read.file, p)})
		}
		b.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	}
	for _, p := range gone {
		id := appCrewAskPrefix + p.ID
		var ask appwire.Ask
		run.locked(func() {
			ask = run.asks[id]
			delete(run.asks, id)
		})
		decided, err := teamstore.PacketByID(b.crewProfile(), p.ID)
		if err != nil {
			decided = p
		}
		b.emit.Emit(appwire.EventDecisionChanged, appwire.DecisionEvent{Decision: appDecisionOf(read.file, decided)})
		b.closeCrewAsk(ask, decided.Decision, decided.DecidedBy)
	}
}

// closeCrewAsk closes a crew's ask with its receipt, which the Inbox and the
// dock read.
func (b *appBackend) closeCrewAsk(ask appwire.Ask, option, by string) {
	if ask.ID == "" || ask.Agent == "" {
		return
	}
	at := b.now()
	who := "you"
	if by != "" && by != teamstore.Person {
		who = "@" + by
	}
	b.emit.Emit(appwire.EventAskClosed, appwire.AskClosed{Agent: ask.Agent, ID: ask.ID, By: appwire.ClosedByApp, Option: option, At: at})
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "r" + ask.ID, Agent: ask.Agent, Kind: appwire.EntryReceipt, At: at, Text: "Decided · " + who + " · " + at.Format("15:04"),
	}})
}

// crewAsksOf is the crew asks waiting on an agent.
func (b *appBackend) crewAsksOf(agent string) []appwire.Ask {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run == nil {
		return nil
	}
	var out []appwire.Ask
	run.locked(func() {
		for _, ask := range run.asks {
			if ask.Agent == agent {
				out = append(out, ask)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// answerCrewAsk answers a crew's ask: a decision is decided through the store,
// a proposal made into a crew or let go.
func (b *appBackend) answerCrewAsk(agent, id, option, text, why string) (bool, error) {
	switch {
	case strings.HasPrefix(id, appCrewAskPrefix):
		_, err := b.CrewDecide(strings.TrimPrefix(id, appCrewAskPrefix), option, text, why)
		return true, err
	case strings.HasPrefix(id, appProposalAskPrefix):
		return true, b.answerProposal(agent, strings.TrimPrefix(id, appProposalAskPrefix), option)
	}
	return false, nil
}

// ── the methods ─────────────────────────────────────────────────────────────

// CrewList is every open crew, or every closed one.
func (b *appBackend) CrewList(closed bool) ([]appwire.Crew, error) {
	b.crews()
	read, err := b.crewRead()
	if err != nil {
		return nil, appEngineError(err)
	}
	teams := read.file.Open()
	if closed {
		teams = read.file.ClosedTeams()
	}
	out := make([]appwire.Crew, 0, len(teams))
	for _, t := range teams {
		out = append(out, b.crewView(read, t))
	}
	return out, nil
}

// ListCrews is the open crews.
func (b *appBackend) ListCrews() ([]appwire.Crew, error) { return b.CrewList(false) }

// CrewGet is one crew.
func (b *appBackend) CrewGet(id string) (appwire.Crew, error) {
	b.crews()
	read, err := b.crewRead()
	if err != nil {
		return appwire.Crew{}, appEngineError(err)
	}
	t, ok := read.file.Team(id)
	if !ok {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", id)
	}
	return b.crewView(read, t), nil
}

// crewEdit is one write to the teams store, its refusal in the app's words.
func (b *appBackend) crewEdit(fn func(*teamstore.File) error) error {
	if err := teamstore.Update(b.crewProfile(), fn); err != nil {
		var wire *appwire.Error
		if errors.As(err, &wire) {
			return wire
		}
		return appwire.Errorf(appwire.CodeBadParams, "%s", strings.TrimPrefix(err.Error(), "teams: "))
	}
	return nil
}

// CrewCreate makes a crew from agents, or from an agent's proposal.
func (b *appBackend) CrewCreate(ask appwire.CrewCreate) (appwire.Crew, error) {
	if ask.From == "proposal" {
		return b.createFromProposal(ask)
	}
	name := strings.TrimSpace(ask.Name)
	if name == "" {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "give the crew a name")
	}
	if len(ask.Agents) == 0 {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "a crew needs at least one agent")
	}
	members := make([]teamstore.Member, 0, len(ask.Agents))
	managerKey := ""
	for _, agent := range ask.Agents {
		m, err := b.agentMember(agent)
		if err != nil {
			return appwire.Crew{}, err
		}
		if agent == ask.Manager {
			managerKey = m.Key
		}
		members = append(members, m)
	}
	if ask.Manager != "" && managerKey == "" {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "the manager must be one of the crew's agents")
	}
	hue, err := b.askedHue(ask.Hue, ask.Tint)
	if err != nil {
		return appwire.Crew{}, err
	}
	id, err := b.newTeam(name, ask.Parent, members, managerKey, hue, ask.Settings, ask.CapUSDDay)
	if err != nil {
		return appwire.Crew{}, err
	}
	b.crewExtraOnCreate(id, ask.Manager, ask.Goal, ask.EndsAt)
	b.crewReceipt(ask.Manager, id, name)
	return b.CrewGet(id)
}

// askedHue is the hue a create or update asked for: a hue, or a named tint.
func (b *appBackend) askedHue(hue *float64, tint string) (*float64, error) {
	if hue != nil {
		h := math.Mod(math.Mod(*hue, 360)+360, 360)
		return &h, nil
	}
	if strings.TrimSpace(tint) != "" {
		h, ok := appTintHue(tint)
		if !ok {
			return nil, appwire.Errorf(appwire.CodeBadParams, "a tint is rose, sand, sage, tide or iris")
		}
		return &h, nil
	}
	return nil, nil
}

// newTeam writes a new team, its name unique among open teams, under its
// parent when it has one and the parent may nest.
func (b *appBackend) newTeam(name, parent string, members []teamstore.Member, managerKey string, hue *float64, settings *appwire.CrewSettings, cap *float64) (string, error) {
	id := teamstore.NewID()
	defaults := teamstore.DefaultsAt(b.crewProfile())
	err := b.crewEdit(func(f *teamstore.File) error {
		for _, t := range f.Open() {
			if strings.EqualFold(strings.TrimSpace(t.Name), name) {
				return appwire.Errorf(appwire.CodeBadParams, "a crew already uses the name %s", name)
			}
		}
		if parent != "" && !f.CanNest(parent, defaults) {
			return appwire.Errorf(appwire.CodeBadParams, "a crew cannot go under that one: it is closed or as deep as crews may go")
		}
		var used []teamstore.HueSpec
		for _, t := range f.Teams {
			if t.Hued() {
				used = append(used, t.HueSpec())
			}
		}
		team := teamstore.Team{ID: id, Name: name, Made: b.now(), Founder: managerKey}
		spec := teamstore.NextHue(used, nil)
		if hue != nil {
			spec.Hue = *hue
		}
		team.SetHue(spec)
		f.Teams = append(f.Teams, team)
		for _, m := range members {
			if err := f.AddMember(id, m); err != nil {
				return err
			}
		}
		if managerKey != "" {
			if err := f.SetManager(id, managerKey); err != nil {
				return err
			}
		}
		if parent != "" {
			if err := f.SetParent(id, parent); err != nil {
				return err
			}
		}
		if settings != nil || cap != nil {
			return f.SetSettings(id, func(s *teamstore.Settings) {
				if settings != nil {
					appApplySettings(s, *settings)
				}
				if cap != nil {
					v := *cap
					s.CapUSDDay = &v
				}
			})
		}
		return nil
	})
	return id, err
}

// appApplySettings writes the settings that were given.
func appApplySettings(s *teamstore.Settings, given appwire.CrewSettings) {
	if given.QuestionsUp != nil {
		s.QuestionsUp = given.QuestionsUp
	}
	if given.CapUSDDay != nil {
		s.CapUSDDay = given.CapUSDDay
	}
	if given.DepthLimit != nil {
		s.DepthLimit = given.DepthLimit
	}
	if given.SubShare != nil {
		s.SubShare = given.SubShare
	}
	if given.Wake != nil {
		s.Wake = given.Wake
	}
}

// crewExtraOnCreate keeps a new crew's goal, end date and the manager's tasks
// as they stood.
func (b *appBackend) crewExtraOnCreate(id, manager, goal string, ends *time.Time) {
	var history []string
	if manager != "" {
		if tasks, err := b.ListTasks(manager); err == nil {
			for _, task := range tasks {
				history = append(history, task.ID)
			}
		}
	}
	if strings.TrimSpace(goal) == "" && ends == nil && len(history) == 0 {
		return
	}
	_ = b.crewLayer().write(func(f *appCrewLayerFile) error {
		extra := f.crew(id)
		extra.Goal, extra.EndsAt, extra.History = strings.TrimSpace(goal), ends, history
		return nil
	})
}

// crewReceipt says in the founding manager's conversation that the crew was
// made. It says the fact the teams file keeps ([teamstore.Team.Founder], Made
// and Name) and no words, so a restart reads the same entry back from
// [appBackend.crewFormedEntries] under the same id.
func (b *appBackend) crewReceipt(manager, id, name string) {
	if manager == "" {
		return
	}
	for _, entry := range b.crewFormedEntries(manager) {
		if entry.ID == appCrewFormedID(id) {
			b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
			return
		}
	}
}

// appCrewFormedID is the entry id of a crew's formation fact.
func appCrewFormedID(crew string) string { return "rcrew:" + crew }

// crewFormedEntries is the crews an agent's conversation founded, each as the
// formation entry the conversation carried when it was made, oldest first.
// One read of the teams file.
func (b *appBackend) crewFormedEntries(agent string) []appwire.Entry {
	file, err := teamstore.Load(b.crewProfile())
	if err != nil {
		return nil
	}
	var out []appwire.Entry
	for _, t := range file.Teams {
		if t.Founder == "" || t.Made.IsZero() {
			continue
		}
		founder := appAgentID(t.Founder)
		for _, m := range append(append([]teamstore.Member{}, t.Members...), t.FormerMembers...) {
			if m.Key == t.Founder {
				founder = appMemberAgent(m)
				break
			}
		}
		if founder != agent {
			continue
		}
		out = append(out, appwire.Entry{
			ID: appCrewFormedID(t.ID), Agent: agent, Kind: appwire.EntryReceipt, At: t.Made,
			Op: &appwire.EntryOp{Kind: appwire.OpCrewFormed, Crew: t.ID, Name: t.Name},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// appWithFormed puts formation entries among a transcript's entries by the
// clock: before the first entry whose own instant is later. unknown is the
// instant an entry carries when the record never said, which places nothing.
func appWithFormed(all, formed []appwire.Entry, unknown time.Time, sources ...*[]int) []appwire.Entry {
	for _, f := range formed {
		at, seen := len(all), false
		for i, e := range all {
			if e.ID == f.ID {
				seen = true
				break
			}
			if at == len(all) && !e.At.Equal(unknown) && e.At.After(f.At) {
				at = i
			}
		}
		if seen {
			continue
		}
		all = append(all, appwire.Entry{})
		copy(all[at+1:], all[at:])
		all[at] = f
		if len(sources) > 0 && sources[0] != nil {
			from := *sources[0]
			from = append(from, -1)
			copy(from[at+1:], from[at:])
			from[at] = -1 // A formation fact has no transcript item to dress.
			*sources[0] = from
		}
	}
	return all
}

// CrewUpdate renames, recolours or resets a crew, and keeps its goal and end
// date in the app layer.
func (b *appBackend) CrewUpdate(ask appwire.CrewUpdate) (appwire.Crew, error) {
	tint := ""
	if ask.Tint != nil {
		tint = *ask.Tint
	}
	hue, err := b.askedHue(ask.Hue, tint)
	if err != nil {
		return appwire.Crew{}, err
	}
	clearHue := false
	for _, key := range ask.Clear {
		clearHue = clearHue || key == "hue"
	}
	if clearHue && (ask.Hue != nil || strings.TrimSpace(tint) != "") {
		return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "a crew cannot be given a colour and have it cleared in one update")
	}
	err = b.crewEdit(func(f *teamstore.File) error {
		i := teamstore.Index(f.Teams, ask.ID)
		if i < 0 {
			return appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", ask.ID)
		}
		if ask.Name != nil {
			name := strings.TrimSpace(*ask.Name)
			if name == "" {
				return appwire.Errorf(appwire.CodeBadParams, "give the crew a name")
			}
			for _, t := range f.Open() {
				if t.ID != ask.ID && strings.EqualFold(strings.TrimSpace(t.Name), name) {
					return appwire.Errorf(appwire.CodeBadParams, "a crew already uses the name %s", name)
				}
			}
			f.Teams[i].Name = name
		}
		if hue != nil {
			f.Teams[i].SetHue(teamstore.HueSpec{Hue: *hue, Tier: f.Teams[i].Tier})
		}
		if clearHue {
			f.Teams[i].ClearHue()
		}
		if ask.Settings != nil || len(ask.Clear) > 0 {
			return f.SetSettings(ask.ID, func(s *teamstore.Settings) {
				if ask.Settings != nil {
					appApplySettings(s, *ask.Settings)
				}
				for _, key := range ask.Clear {
					switch key {
					case "questions_up":
						s.QuestionsUp = nil
					case "cap_usd_day":
						s.CapUSDDay = nil
					case "depth_limit":
						s.DepthLimit = nil
					case "sub_share":
						s.SubShare = nil
					case "wake":
						s.Wake = nil
					}
				}
			})
		}
		return nil
	})
	if err != nil {
		return appwire.Crew{}, err
	}
	if ask.Goal != nil || ask.EndsAt != nil {
		err := b.crewLayer().write(func(f *appCrewLayerFile) error {
			extra := f.crew(ask.ID)
			if ask.Goal != nil {
				extra.Goal = strings.TrimSpace(*ask.Goal)
			}
			if ask.EndsAt != nil {
				ends := *ask.EndsAt
				extra.EndsAt = &ends
				if ends.IsZero() {
					extra.EndsAt = nil
				}
			}
			return nil
		})
		if err != nil {
			return appwire.Crew{}, appEngineError(err)
		}
	}
	crew, err := b.CrewGet(ask.ID)
	if err != nil {
		return crew, err
	}
	// A CAP THE PERSON SET IS SAID IN THE MANAGER'S CHAT, where the spending
	// it bounds is decided.
	cleared := false
	for _, key := range ask.Clear {
		cleared = cleared || key == "cap_usd_day"
	}
	if ask.Settings != nil && ask.Settings.CapUSDDay != nil || cleared {
		words := crew.Name + " cap cleared"
		if !cleared {
			words = fmt.Sprintf("%s cap set to $%.2f", crew.Name, *ask.Settings.CapUSDDay)
		}
		b.crewSettingReceipt(crew.Manager, crew.ID, "cap", words)
	}
	return crew, nil
}

// crewSettingReceipt writes a receipt for a crew setting the person changed
// into the manager's chat.
func (b *appBackend) crewSettingReceipt(manager, id, setting, words string) {
	if manager == "" {
		return
	}
	at := b.now()
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "rcrew:" + id + ":" + setting + ":" + strconv.FormatInt(at.UnixNano(), 36), Agent: manager,
		Kind: appwire.EntryReceipt, At: at, Text: words + " · you · " + at.Format("15:04"),
	}})
}

// crewTeam is one team as it stands, or not_found.
func (b *appBackend) crewTeam(id string) (teamstore.Team, *teamstore.File, error) {
	f, err := teamstore.Load(b.crewProfile())
	if err != nil {
		return teamstore.Team{}, nil, appEngineError(err)
	}
	t, ok := f.Team(id)
	if !ok {
		return teamstore.Team{}, nil, appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", id)
	}
	return t, f, nil
}

// crewMemberOf is an agent's membership of a crew.
func (b *appBackend) crewMemberOf(t teamstore.Team, agent string) (teamstore.Member, bool) {
	for _, m := range t.Members {
		if appMemberAgent(m) == agent {
			return m, true
		}
	}
	return teamstore.Member{}, false
}

// CrewMessage puts the person's words into a conversation of the crew: the
// manager's, or a member's, which the manager is also told of on the crew's
// log (app layer).
func (b *appBackend) CrewMessage(ask appwire.CrewMessage) (appwire.Entry, error) {
	t, _, err := b.crewTeam(ask.Crew)
	if err != nil {
		return appwire.Entry{}, err
	}
	target := ask.To
	if target == "" || target == teamstore.ToManager {
		m, ok := t.Member(t.Manager)
		if !ok {
			return appwire.Entry{}, appwire.Errorf(appwire.CodeBadParams, "%s has no manager to talk to", t.Name)
		}
		target = appMemberAgent(m)
	}
	member, ok := b.crewMemberOf(t, target)
	if !ok {
		return appwire.Entry{}, appwire.Errorf(appwire.CodeNotFound, "%s is not in %s", target, t.Name)
	}
	if len(ask.Attachments) > 0 || ask.Said != nil || len(ask.Reads) > 0 {
		return b.crewMessageWith(t, member, target, ask)
	}
	turn, err := b.Send(target, ask.Text, ask.ClientID)
	var wire *appwire.Error
	if errors.As(err, &wire) && wire.Code == appwire.CodeBusy {
		// A MEMBER IN THE MIDDLE OF A TURN IS NOT REFUSED THE PERSON'S WORDS:
		// they wait in its queue and go in when the turn in hand ends, the way
		// a message typed to a working agent does. Turn 0 says it is waiting.
		turn, err = 0, nil
		if _, qerr := b.Queue(target, ask.Text, ask.ClientID); qerr != nil {
			err = qerr
		}
	}
	if err != nil {
		return appwire.Entry{}, err
	}
	b.tellManager(t, member, ask.Text)
	return appwire.Entry{Agent: target, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: ask.Text, ClientID: ask.ClientID}, nil
}

// crewMessageWith is a crew message carrying attachments staged on its
// target, the person's own words apart from the text, or reads. A MESSAGE
// CARRYING ANY OF THEM IS NEVER QUEUED: the queue keeps words only, so
// queueing it would send the words and lose the rest. A busy target refuses it
// instead, and every chip stays staged for the person to send again when the
// turn ends.
func (b *appBackend) crewMessageWith(t teamstore.Team, member teamstore.Member, target string, ask appwire.CrewMessage) (appwire.Entry, error) {
	turn, shown, err := b.sendMessage(target, appwire.AgentMessage{Text: ask.Text, Said: ask.Said, ClientID: ask.ClientID, Attachments: ask.Attachments, Reads: ask.Reads})
	var wire *appwire.Error
	if errors.As(err, &wire) && wire.Code == appwire.CodeBusy {
		if len(ask.Attachments) > 0 {
			return appwire.Entry{}, appwire.Errorf(appwire.CodeBusy, "%s is working on a turn; the message was not sent and its attachments are still attached", member.Handle)
		}
		return appwire.Entry{}, appwire.Errorf(appwire.CodeBusy, "%s is working on a turn; the message was not sent", member.Handle)
	}
	if err != nil {
		return appwire.Entry{}, err
	}
	// The manager is told what the person said, never the expanded words the
	// member's model was sent.
	text := ask.Text
	var views []appwire.Attachment
	var reads []appwire.MessageRead
	if shown != nil {
		text, views, reads = shown.text, shown.attachments, appReadsOf(shown.reads)
	}
	logged := text
	if strings.TrimSpace(logged) == "" && len(views) > 0 {
		titles := make([]string, 0, len(views))
		for _, view := range views {
			titles = append(titles, view.Title)
		}
		logged = "Attached: " + strings.Join(titles, ", ")
	}
	if strings.TrimSpace(logged) != "" {
		b.tellManager(t, member, logged)
	}
	return appwire.Entry{Agent: target, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: text, ClientID: ask.ClientID, Attachments: views, Reads: reads}, nil
}

// tellManager puts the person's words to a member on the crew's log.
// THE MANAGER IS TOLD AS A MEMBER'S WORDS REACH IT: one line on the crew's
// log, in its digest at its next step. It does not wake it. Words to the
// manager itself are not logged; it heard them.
func (b *appBackend) tellManager(t teamstore.Team, member teamstore.Member, text string) {
	if member.Key == t.Manager || t.Manager == "" {
		return
	}
	_ = teamstore.AppendTraffic(b.crewProfile(), t.ID, teamstore.Entry{
		Kind: teamstore.KindYou, From: teamstore.FromYou, To: member.Handle, ToKey: member.Key, Member: member.Key, Text: text,
	})
}

// CrewLog is a crew's log after a cursor, oldest first.
func (b *appBackend) CrewLog(id, after string, limit int) ([]appwire.LogLine, string, error) {
	t, _, err := b.crewTeam(id)
	if err != nil {
		return nil, "", err
	}
	if limit <= 0 || limit > appCrewLogPage {
		limit = appCrewLogPage
	}
	entries, err := teamstore.ReadTraffic(b.crewProfile(), id, after, limit)
	if err != nil {
		return nil, "", appEngineError(err)
	}
	lines := make([]appwire.LogLine, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, appLogLine(t, e))
	}
	cursor := after
	if len(entries) > 0 {
		cursor = entries[len(entries)-1].ID
	}
	return lines, cursor, nil
}

// appLogAnchorAround is how many neighbours an anchor reads on each side when
// the ask names none.
const appLogAnchorAround = 2

// CrewLogAt is one crew log line a conversation's own record points at (a
// handoff's post, a delivery's post), with its neighbours. THE LINE IS FOUND
// BY FACTS, NEVER BY WORDS OR TIME: a crew the agent is a member of, with the
// name the record gave, whose log holds that very id written by or to that
// member. Two crews that both qualify are refused rather than guessed between,
// and a line rotated out of the log is not_found, never a nearby one.
func (b *appBackend) CrewLogAt(ask appwire.LogAnchorAsk) (appwire.LogAnchor, error) {
	post, ok := teamstore.ThreadID(ask.Post)
	if !ok {
		return appwire.LogAnchor{}, appwire.Errorf(appwire.CodeBadParams, "%q is not a crew log line", ask.Post)
	}
	around := ask.Around
	if around == 0 {
		around = appLogAnchorAround
	}
	profile := b.crewProfile()
	f, err := teamstore.Load(profile)
	if err != nil {
		return appwire.LogAnchor{}, appEngineError(err)
	}
	var (
		found   []appwire.LogAnchor
		inCrews int
	)
	for _, t := range f.Teams {
		if ask.CrewName != "" && t.Name != ask.CrewName {
			continue
		}
		m, ok := b.crewMemberOf(t, ask.Agent)
		if !ok {
			continue
		}
		inCrews++
		entries, ok, err := teamstore.ReadAround(profile, t.ID, post, around)
		if err != nil {
			return appwire.LogAnchor{}, appEngineError(err)
		}
		if !ok {
			continue
		}
		var mine bool
		lines := make([]appwire.LogLine, 0, len(entries))
		for _, e := range entries {
			if e.ID == post {
				mine = teamstore.Involves(e, m)
			}
			lines = append(lines, appLogLine(t, e))
		}
		if mine {
			found = append(found, appwire.LogAnchor{Crew: t.ID, Closed: t.Closed(), Lines: lines})
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		return appwire.LogAnchor{}, appwire.Errorf(appwire.CodeConflict, "line %s is in more than one of this agent's crews", teamstore.ThreadNumber(post))
	case inCrews == 0:
		return appwire.LogAnchor{}, appwire.Errorf(appwire.CodeNotFound, "this agent is no longer in a crew named %q", ask.CrewName)
	}
	return appwire.LogAnchor{}, appwire.Errorf(appwire.CodeNotFound, "line %s is no longer in the crew's log", teamstore.ThreadNumber(post))
}

// CrewDecisions is the decisions waiting (or all of them) for one crew, or
// every decision waiting on the person.
func (b *appBackend) CrewDecisions(id string, all bool) ([]appwire.Decision, error) {
	profile := b.crewProfile()
	f, err := teamstore.Load(profile)
	if err != nil {
		return nil, appEngineError(err)
	}
	var packets []teamstore.Packet
	switch {
	case id == "":
		packets, _, err = teamstore.OpenPackets(profile, teamstore.Person)
	case all:
		packets, err = teamstore.Packets(profile, id)
	default:
		var open []teamstore.Packet
		open, _, err = teamstore.OpenPackets(profile, teamstore.ScopeAll)
		for _, p := range open {
			if p.Origin == id || p.Team == id {
				packets = append(packets, p)
			}
		}
	}
	if err != nil {
		return nil, appEngineError(err)
	}
	out := make([]appwire.Decision, 0, len(packets))
	for _, p := range packets {
		out = append(out, appDecisionOf(f, p))
	}
	return out, nil
}

// CrewDecide is the person's decision on a packet; a closing report accepted
// closes the crew it reports on.
func (b *appBackend) CrewDecide(packet, option, text, reason string) (appwire.Decision, error) {
	profile := b.crewProfile()
	decision := strings.TrimSpace(option)
	if decision == "" {
		decision = strings.TrimSpace(text)
	} else if strings.TrimSpace(text) != "" && strings.TrimSpace(reason) == "" {
		reason = text
	}
	p, err := teamstore.Decide(profile, packet, teamstore.Person, decision, strings.TrimSpace(reason))
	if err != nil {
		switch {
		case errors.Is(err, teamstore.ErrNoPacket):
			return appwire.Decision{}, appwire.Errorf(appwire.CodeNotFound, "there is no decision %s", packet)
		case errors.Is(err, teamstore.ErrDecided):
			return appwire.Decision{}, appwire.Errorf(appwire.CodeBadParams, "that decision was already made")
		}
		return appwire.Decision{}, appEngineError(err)
	}
	if p.Kind == teamstore.PacketClosing && (p.Decision == teamstore.OptionClose || p.Decision == teamstore.OptionCloseNow) {
		if _, err := teamstore.AcceptClosing(profile, p); err != nil {
			return appwire.Decision{}, appEngineError(err)
		}
	}
	f, err := teamstore.Load(profile)
	if err != nil {
		return appwire.Decision{}, appEngineError(err)
	}
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run != nil {
		b.crewTick(run, false)
	}
	return appDecisionOf(f, p), nil
}

// CrewManager makes a member the crew's manager, or clears it.
func (b *appBackend) CrewManager(id, agent string) (appwire.Crew, error) {
	t, _, err := b.crewTeam(id)
	if err != nil {
		return appwire.Crew{}, err
	}
	key := ""
	if agent != "" {
		m, ok := b.crewMemberOf(t, agent)
		if !ok {
			return appwire.Crew{}, appwire.Errorf(appwire.CodeBadParams, "the manager must be a member of %s", t.Name)
		}
		key = m.Key
	}
	err = b.crewEdit(func(f *teamstore.File) error {
		if key == "" {
			return f.ClearManager(id)
		}
		return f.SetManager(id, key)
	})
	if err != nil {
		return appwire.Crew{}, err
	}
	return b.CrewGet(id)
}

// CrewMove puts crews under another crew, or at the top with parent "".
func (b *appBackend) CrewMove(ids []string, parent string) error {
	defaults := teamstore.DefaultsAt(b.crewProfile())
	return b.crewEdit(func(f *teamstore.File) error {
		target := f.MoveTarget(parent)
		// The check answers whether the move MAY go: false comes with why not.
		if block, ok := f.MoveCheck(ids, target, defaults); !ok {
			return appwire.Errorf(appwire.CodeBadParams, "%s", appMoveWords(block))
		}
		return f.Move(ids, target)
	})
}

// appMoveWords is why a move was refused, in the app's words.
func appMoveWords(block teamstore.MoveBlock) string {
	switch block.Kind {
	case "closed":
		return block.Name + " is closed"
	case "depth":
		return fmt.Sprintf("crews may go %d deep there, and this would make %d", block.Limit, block.Depth+block.Need)
	case "here":
		return "it is already there"
	case "self":
		return "a crew cannot go under itself"
	case "inside":
		return block.Name + " cannot go under a crew inside it"
	case "root":
		return "All crews stays at the top"
	case "gone":
		return "that crew is gone"
	}
	return "that move is not allowed"
}

// CrewWrapUp asks the crew's manager to wrap up and bring a closing report.
func (b *appBackend) CrewWrapUp(id, text string) error {
	t, _, err := b.crewTeam(id)
	if err != nil {
		return err
	}
	if t.Manager == "" {
		return appwire.Errorf(appwire.CodeBadParams, "%s has no manager to wrap up; close it instead", t.Name)
	}
	if err := teamstore.AppendTraffic(b.crewProfile(), id, teamstore.WrapUpRequest(text)); err != nil {
		return appEngineError(err)
	}
	return nil
}

// CrewClose closes a crew and every crew under it. Its conversations keep
// working; the app stops holding them for it.
func (b *appBackend) CrewClose(id string) error {
	t, _, err := b.crewTeam(id)
	if err != nil {
		return err
	}
	if t.Root {
		return appwire.Errorf(appwire.CodeBadParams, "All crews does not close; clear its manager instead")
	}
	if err := b.crewEdit(func(f *teamstore.File) error { return f.Disband(id, b.now(), "") }); err != nil {
		return err
	}
	_ = teamstore.AppendTraffic(b.crewProfile(), id, teamstore.Entry{Kind: teamstore.KindClose, From: teamstore.FromYou, To: teamstore.ToRoom, Text: "closed"})
	return nil
}

// CrewReopen opens a closed crew again, with the crews its close closed.
func (b *appBackend) CrewReopen(id string) (appwire.Crew, error) {
	if _, _, err := b.crewTeam(id); err != nil {
		return appwire.Crew{}, err
	}
	err := b.crewEdit(func(f *teamstore.File) error {
		if err := f.Reopen(id); err != nil {
			if errors.Is(err, teamstore.ErrParentClosed) {
				return appwire.Errorf(appwire.CodeBadParams, "its parent crew is closed; reopen that first")
			}
			return err
		}
		return nil
	})
	if err != nil {
		return appwire.Crew{}, err
	}
	_ = teamstore.AppendTraffic(b.crewProfile(), id, teamstore.Entry{Kind: teamstore.KindReopen, From: teamstore.FromYou, To: teamstore.ToRoom, Text: "reopened"})
	return b.CrewGet(id)
}

// CrewDelete forgets a closed crew and the crews under it. Their
// conversations are never deleted.
func (b *appBackend) CrewDelete(id string) ([]string, error) {
	t, _, err := b.crewTeam(id)
	if err != nil {
		return nil, err
	}
	if !t.Closed() {
		return nil, appwire.Errorf(appwire.CodeBadParams, "close %s before deleting it", t.Name)
	}
	ids, err := teamstore.Delete(b.crewProfile(), id)
	if err != nil {
		return nil, appEngineError(err)
	}
	_ = b.crewLayer().write(func(f *appCrewLayerFile) error {
		for _, gone := range ids {
			delete(f.Crews, gone)
			for key := range f.Members {
				if strings.HasPrefix(key, gone+"/") {
					delete(f.Members, key)
				}
			}
		}
		return nil
	})
	return ids, nil
}

// appDefaults is the five defaults on the wire.
func appDefaults(d teamstore.Defaults) appwire.CrewDefaults {
	return appwire.CrewDefaults{QuestionsUp: d.QuestionsUp, CapUSDDay: d.CapUSDDay, DepthLimit: d.DepthLimit, SubShare: d.SubShare, Wake: d.Wake}
}

// CrewDefaults is the profile's five crew defaults.
func (b *appBackend) CrewDefaults() (appwire.CrewDefaults, error) {
	return appDefaults(teamstore.DefaultsAt(b.crewProfile())), nil
}

// CrewSetDefault writes one default the way the terminal's settings do.
func (b *appBackend) CrewSetDefault(key, value string) (appwire.CrewDefaults, error) {
	if err := config.ApplyTeamDefault(b.crewProfile(), key, value); err != nil {
		return appwire.CrewDefaults{}, appwire.Errorf(appwire.CodeBadParams, "%s", plainWords(err.Error()))
	}
	return b.CrewDefaults()
}

// CrewItems is a crew's linked items (app layer).
func (b *appBackend) CrewItems(id string) ([]appwire.CrewItem, error) {
	if _, _, err := b.crewTeam(id); err != nil {
		return nil, err
	}
	var items []appwire.CrewItem
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		if extra := f.Crews[id]; extra != nil {
			items = append(items, extra.Items...)
		}
	})
	return items, nil
}

// CrewLink links a job, issue, pull request or file to a crew (app layer).
func (b *appBackend) CrewLink(id, kind, ref, title string) (appwire.CrewItem, error) {
	if _, _, err := b.crewTeam(id); err != nil {
		return appwire.CrewItem{}, err
	}
	var item appwire.CrewItem
	err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		item = appwire.CrewItem{ID: f.next("i"), Crew: id, Kind: kind, Ref: strings.TrimSpace(ref), Title: strings.TrimSpace(title), At: b.now()}
		extra := f.crew(id)
		extra.Items = append(extra.Items, item)
		return nil
	})
	if err != nil {
		return appwire.CrewItem{}, appEngineError(err)
	}
	return item, nil
}

// CrewUnlink takes a linked item off a crew.
func (b *appBackend) CrewUnlink(id, item string) error {
	found := false
	err := b.crewLayer().write(func(f *appCrewLayerFile) error {
		extra := f.Crews[id]
		if extra == nil {
			return nil
		}
		kept := extra.Items[:0]
		for _, it := range extra.Items {
			if it.ID == item {
				found = true
				continue
			}
			kept = append(kept, it)
		}
		extra.Items = kept
		return nil
	})
	if err != nil {
		return appEngineError(err)
	}
	if !found {
		return appwire.Errorf(appwire.CodeNotFound, "%s has no item %s", id, item)
	}
	return nil
}
