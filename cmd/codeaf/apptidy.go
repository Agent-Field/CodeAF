package main

// ── codeaf app: CLOSING, ARCHIVING AND FINDING (wave 7) ─────────────────────
//
// A CLOSED AGENT keeps everything and leaves the rail: its conversation is let
// go, its journal stays, and opening it or sending to it opens it again. An
// ARCHIVED one is put away further, through the same mark the terminal's
// conversation list uses (session.SetArchived), and is listed only when asked
// for. When each happened is the app's to keep, in its state file; the archive
// mark itself is the conversation's.
//
// FINDING reads what is already kept: the conversations' rows for agents, the
// teams for crews, each project's task index for tasks, and the search store
// every conversation's words are indexed in for messages.

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// appRenamer is a conversation a person can rename.
type appRenamer interface {
	RenameByPerson(title string) error
}

// tidyMarks lays the app's closed and archived marks over a listed agent.
func (b *appBackend) tidyMarks(view *appwire.Agent, archived bool) {
	// A CONVERSATION AT WORK OR WAITING ON THE PERSON IS SAID AS SUCH, whatever
	// mark it carries: a terminal may be working in an agent the app closed.
	held := b.held(view.ID) || view.State == appwire.StateWorking || view.State == appwire.StateNeedsYou
	_ = b.state.read(func(f *appStateFile) {
		if at, ok := f.Closed[view.ID]; ok && !held {
			at := at
			view.ClosedAt = &at
			view.State = appwire.StateClosed
		}
		if at, ok := f.Archived[view.ID]; ok && !held {
			at := at
			view.ArchivedAt = &at
		}
	})
	if archived && !held {
		view.State = appwire.StateArchived
		if view.ArchivedAt == nil {
			view.ArchivedAt = appKnownTime(view.UpdatedAt)
		}
	}
	// LastActiveAt IS NOT FILLED FROM UpdatedAt. It is when the person last
	// spoke, and a conversation they never spoke in, a member a manager
	// started, has none; a window that wants a time to sort by falls back to
	// updated_at itself, and the wire leaves an unknown out (appblank.go).
}

// ListAgentsIn is agents.list with its state and query.
func (b *appBackend) ListAgentsIn(workspace, state, query string) ([]appwire.Agent, error) {
	all, err := b.ListAgents(workspace)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []appwire.Agent
	for _, a := range all {
		closed := a.State == appwire.StateClosed
		archived := a.State == appwire.StateArchived
		switch state {
		case "", appwire.ListOpen:
			if closed || archived {
				continue
			}
		case appwire.ListClosed:
			if !closed {
				continue
			}
		case appwire.ListArchived:
			if !archived {
				continue
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(a.Title), query) && !strings.Contains(strings.ToLower(a.Workspace), query) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// MoveToClosed lets each agent's conversation go and marks it closed.
func (b *appBackend) MoveToClosed(ids []string) error {
	now := b.now()
	for _, id := range ids {
		a, err := b.find(id)
		if err != nil {
			return err
		}
		if err := b.closeOne(a, func() error {
			if err := b.state.write(func(f *appStateFile) error {
				if f.Closed == nil {
					f.Closed = map[string]time.Time{}
				}
				f.Closed[id] = now
				return nil
			}); err != nil {
				return appEngineError(err)
			}
			return nil
		}); err != nil {
			return err
		}
		b.sayAgent(a)
	}
	return nil
}

// closeOne is the one way the app lets an agent go from a person's Close or
// Archive: what the agent kept running is fenced and stopped FIRST, and an
// agent whose process would not stop is left exactly as it was, open and
// unmarked, with the error saying which. mark runs after the conversation is
// let go and before the fence is released, so nothing starts in between.
// Only this agent's own services are touched.
func (b *appBackend) closeOne(a *appAgent, mark func() error) error {
	var agentID string
	b.locked(func() { agentID = a.view.ID })
	defer b.fenceAgentServices(agentID)()
	if err := b.stopAgentServices(agentID); err != nil {
		return err
	}
	b.closeAgent(a)
	return mark()
}

// Archive closes each agent and puts it away.
func (b *appBackend) Archive(ids []string) error {
	now := b.now()
	for _, id := range ids {
		a, err := b.find(id)
		if err != nil {
			return err
		}
		if err := b.closeOne(a, func() error {
			var transcript string
			b.locked(func() { transcript = a.transcript })
			if transcript != "" {
				if err := session.SetArchived(session.DirOf(transcript), true); err != nil {
					return appEngineError(err)
				}
			}
			if err := b.state.write(func(f *appStateFile) error {
				if f.Archived == nil {
					f.Archived = map[string]time.Time{}
				}
				f.Archived[id] = now
				return nil
			}); err != nil {
				return appEngineError(err)
			}
			return nil
		}); err != nil {
			return err
		}
		b.locked(func() { a.view.State = appwire.StateArchived })
		b.sayAgent(a)
	}
	return nil
}

// Reopen takes an agent out of Closed or the archive, exactly as it was.
func (b *appBackend) Reopen(id string) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	var transcript string
	archived := false
	b.locked(func() { transcript, archived = a.transcript, a.view.State == appwire.StateArchived })
	_ = b.state.read(func(f *appStateFile) {
		_, marked := f.Archived[id]
		archived = archived || marked
	})
	if archived && transcript != "" {
		if err := session.SetArchived(session.DirOf(transcript), false); err != nil {
			return appwire.Agent{}, appEngineError(err)
		}
	}
	b.unmarkTidy(id)
	b.locked(func() {
		if a.view.State == appwire.StateClosed || a.view.State == appwire.StateArchived {
			a.view.State = appwire.StateIdle
		}
	})
	return b.sayAgent(a), nil
}

// unmarkTidy forgets that an agent was closed or archived: it is open again.
func (b *appBackend) unmarkTidy(id string) {
	known := false
	_ = b.state.read(func(f *appStateFile) {
		_, closed := f.Closed[id]
		_, archived := f.Archived[id]
		known = closed || archived
	})
	if !known {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		delete(f.Closed, id)
		delete(f.Archived, id)
		return nil
	})
}

// Rename gives an agent the name the person typed, in its conversation.
func (b *appBackend) Rename(id, title string) (appwire.Agent, error) {
	a, live, err := b.heldLive(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.nameGate.Lock()
	defer a.nameGate.Unlock()
	door, ok := live.conn.Conv.(appRenamer)
	if !ok {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeEngine, "this conversation cannot be renamed")
	}
	if err := door.RenameByPerson(title); err != nil {
		return appwire.Agent{}, appEngineError(err)
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Title = title
		view = a.view
		if view.LastActiveAt.IsZero() {
			view.LastActiveAt = view.UpdatedAt
		}
		// Manual names use the same ordered queue as generated names.
		// Enqueue while holding the snapshot lock so neither can overtake.
		if a.live == live && !live.closed {
			live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	})
	return view, nil
}

// IdleAgents is every open agent nobody has touched for days (the setting's
// days when none is given), and none that is working or holding a question.
func (b *appBackend) IdleAgents(days int) ([]string, error) {
	if days <= 0 {
		days = appIdleCloseDays
		_ = b.state.read(func(f *appStateFile) { days = f.idleDays() })
	}
	if days <= 0 {
		return nil, nil
	}
	cut := b.now().Add(-time.Duration(days) * 24 * time.Hour)
	all, err := b.ListAgents("")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, a := range all {
		if (a.State == appwire.StateIdle || a.State == appwire.StateClosed) && a.ClosedAt == nil && !a.LastActiveAt.IsZero() && a.LastActiveAt.Before(cut) {
			ids = append(ids, a.ID)
		}
	}
	return ids, nil
}

// sayAgent says an agent again as it now stands, and answers that view.
func (b *appBackend) sayAgent(a *appAgent) appwire.Agent {
	var view appwire.Agent
	b.locked(func() { view = a.view })
	b.tidyMarks(&view, view.State == appwire.StateArchived)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view
}

// SearchAll finds the query among agents, crews, tasks and what was said.
func (b *appBackend) SearchAll(query string, perGroup int) ([]appwire.HitGroup, error) {
	needle := strings.ToLower(query)
	agents, err := b.ListAgents("")
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	agentHits := appwire.HitGroup{Kind: appwire.HitAgent}
	for _, a := range agents {
		titles[a.ID] = a.Title
		if len(agentHits.Hits) < perGroup && strings.Contains(strings.ToLower(a.Title), needle) {
			agentHits.Hits = append(agentHits.Hits, appwire.Hit{Kind: appwire.HitAgent, ID: a.ID, Agent: a.ID, Title: a.Title, At: appKnownTime(a.UpdatedAt)})
		}
	}
	crewHits := appwire.HitGroup{Kind: appwire.HitCrew}
	if crews, err := b.CrewList(true); err == nil {
		for _, c := range crews {
			if len(crewHits.Hits) < perGroup && strings.Contains(strings.ToLower(c.Name), needle) {
				crewHits.Hits = append(crewHits.Hits, appwire.Hit{Kind: appwire.HitCrew, ID: c.ID, Agent: c.Manager, Title: c.Name})
			}
		}
	}
	taskHits := appwire.HitGroup{Kind: appwire.HitTask, Hits: b.searchTasks(query, perGroup)}
	messageHits := appwire.HitGroup{Kind: appwire.HitMessage}
	if b.searchWords != nil {
		if hits, err := b.searchWords(query, perGroup); err == nil {
			for _, h := range hits {
				title := h.Title
				if title == "" {
					title = titles[h.SessionID]
				}
				messageHits.Hits = append(messageHits.Hits, appwire.Hit{Kind: appwire.HitMessage, ID: h.SessionID + "#" + strconv.FormatInt(h.Seq, 10), Agent: h.SessionID, Title: title, Excerpt: h.Body, At: appKnownTime(h.Time)})
			}
		}
	}
	return []appwire.HitGroup{agentHits, crewHits, taskHits, messageHits}, nil
}

// appKnownTime is a time to put on the wire, and nil for the zero time, which
// is a time nobody knows: the emptiness law leaves it out rather than sending
// a date in the first century.
func appKnownTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

// searchTasks reads each project's task index once.
func (b *appBackend) searchTasks(query string, limit int) []appwire.Hit {
	seen := map[string]bool{}
	var rows []session.TaskIndexEntry
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.Transcript == "" {
			continue
		}
		path := session.TaskIndexPath(row.Transcript)
		if seen[path] {
			continue
		}
		seen[path] = true
		for _, entry := range session.ReadTaskIndex(path) {
			if entry.Kind != session.TaskKindQuick {
				rows = append(rows, entry)
			}
		}
	}
	found := session.SearchTaskIndex(rows, query, limit)
	hits := make([]appwire.Hit, 0, len(found))
	for _, entry := range found {
		hits = append(hits, appwire.Hit{Kind: appwire.HitTask, ID: entry.SessionID + ":" + entry.ID, Agent: entry.SessionID, Title: entry.Label})
	}
	return hits
}

// appSearchWords is the search store's reading of every conversation's words,
// or nil where the process keeps no store.
type appSearchWords func(terms string, limit int) ([]store.ConversationHit, error)

// dress lays what this process knows now over an agent about to be said: the
// crews it is a member of, its live line, and working while a turn it holds
// is running. A queued agent.changed reaches the window after the moment it
// was made, so it is dressed with the state as it is when it goes out; it is
// only ever moved up from idle, never down from what a road said.
func (b *appBackend) dress(view *appwire.Agent) {
	view.Crews = b.crewsOf(view.ID)
	// Its own budget as the app's file keeps it (appbudget.go); the open
	// conversation's own reading is agents.budget.get's, never a list's.
	if budget, err := b.budgetRecord(view.ID); err == nil {
		view.Budget = &budget
	}
	b.locked(func() {
		a := b.agents[view.ID]
		if a == nil {
			return
		}
		live := a.live
		if live != nil && live.running && view.State == appwire.StateIdle && !live.needsYou() {
			view.State = appwire.StateWorking
		}
		view.Line = agentLineLocked(a, view.State)
	})
}

// agentLineLocked is an agent's live one-liner for its state: the step it is
// on, or the question it is waiting on, and nothing when there is nothing to
// say. Callers hold b.mu.
func agentLineLocked(a *appAgent, state string) string {
	switch state {
	case appwire.StateWorking:
		return a.step
	case appwire.StateNeedsYou:
		if live := a.live; live != nil {
			for _, ask := range live.asks {
				if !ask.Later {
					return ask.Title
				}
			}
		}
	}
	return ""
}
