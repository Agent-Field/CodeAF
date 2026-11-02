package main

// ── codeaf app: UNREAD ──────────────────────────────────────────────────────
//
// AN AGENT IS UNREAD when it said something new to the person after the last
// time they saw it. Something new is OUTPUT: an answer its turn sealed on (the
// session's own stamp, [session.Meta.LastReplyAt], a harness run's report
// among them) or a question put to the person. Tool steps and thinking never
// move it. When the person saw it is agents.mark_seen's mark in the app's
// state file, so it survives a restart.
//
// A QUESTION IS OUTPUT ONLY WHILE IT IS OPEN. Once answered, from its chat,
// the Inbox, another window or the terminal, it no longer counts: needs-you
// clears instead of turning into unread, an answer that came after it still
// counts, and a restart, which only sees the questions still waiting, reads
// the agent the same way.
//
// NOBODY GETS A WALL OF DOTS. An agent with no mark is read up to the moment
// this build first asked ([appStateFile.UnreadFrom]), so on upgrade every
// agent starts read, and a new one starts read because it has said nothing.
// That moment is written down only on a request's own road (agents.list,
// agents.mark_seen), never from an event on its way out, so saying an agent
// never writes a file behind a caller's back.
//
// THE WINDOW SAYS WHAT THE PERSON SEES. The engine cannot know which agent is
// on screen, so a window calls agents.mark_seen when it shows an agent and
// again when output lands while it is shown; until it does, an agent it is
// showing may read unread for a moment.

import (
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appRowAskedAt is when the question a conversation on disk is waiting on the
// person for was put, or zero when none is.
func appRowAskedAt(row session.SessionRow) time.Time {
	if row.NeedsPerson() {
		return row.Presence.Question.Asked
	}
	return time.Time{}
}

// noteRowLocked takes what the conversation's folder says it said: its last
// sealed answer and the question it is waiting on. The question is another
// process's only while this one holds no conversation for the agent; one held
// here says its questions as asks. Callers hold b.mu.
func (a *appAgent) noteRowLocked(replied, asked time.Time) {
	a.repliedAt = laterOf(a.repliedAt, replied)
	if a.live != nil {
		asked = time.Time{}
	}
	a.askedElsewhere = asked
}

// outputsLocked is every instant the agent id said something to the person
// that still counts: its last answer and each question still open, each
// judged on its own, so one stamped ahead never hides another. Its record a
// may be nil: a crew's ask can be said before agents.list has made one.
// Callers hold b.mu.
func (b *appBackend) outputsLocked(id string, a *appAgent) []time.Time {
	var out []time.Time
	if a != nil {
		out = append(out, a.repliedAt, a.askedElsewhere)
	}
	for _, asked := range b.openAsks[id] {
		out = append(out, asked)
	}
	return out
}

// dropAsksLocked forgets the agent's open asks that went with a conversation
// no longer held here, which says no ask.closed for them. Callers hold b.mu.
func (b *appBackend) dropAsksLocked(id string, asks []appwire.Ask) {
	for _, ask := range asks {
		delete(b.openAsks[id], ask.ID)
	}
	if len(b.openAsks[id]) == 0 {
		delete(b.openAsks, id)
	}
}

// forgetAsksOf drops a let-go conversation's questions again once its outbox
// has said its last: an ask.opened still queued when the agent was let go
// came out after the first drop, and no ask.closed follows it.
func (b *appBackend) forgetAsksOf(id string, live *appLive) {
	b.locked(func() { b.dropAsksLocked(id, live.asks) })
	b.restateUnread(id)
}

// laterOf is the later of two instants.
func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// unreadFrom is when this build began keeping unread: the instant an agent with
// no seen mark is read up to. It is the app file's, or, until the file keeps
// one, the moment this process first asked.
func (b *appBackend) unreadFrom() time.Time {
	var from time.Time
	_ = b.state.read(func(f *appStateFile) { from = f.UnreadFrom })
	if !from.IsZero() {
		return from
	}
	b.locked(func() {
		if b.unreadStart.IsZero() {
			b.unreadStart = b.now()
		}
		from = b.unreadStart
	})
	return from
}

// keepUnreadFrom writes [appBackend.unreadFrom] into the app file once, so a
// restart agrees about what was already there. A file that cannot be written
// keeps the moment in this process only.
func (b *appBackend) keepUnreadFrom() {
	var kept bool
	_ = b.state.read(func(f *appStateFile) { kept = !f.UnreadFrom.IsZero() })
	if kept {
		return
	}
	from := b.unreadFrom()
	_ = b.state.write(func(f *appStateFile) error {
		if f.UnreadFrom.IsZero() {
			f.UnreadFrom = from
		}
		return nil
	})
}

// unreadAt says the agent id has output it has not seen: one instant after
// its mark that is not one the person saw stamped ahead of this clock.
func (b *appBackend) unreadAt(id string, outputs []time.Time) bool {
	var said []time.Time
	for _, at := range outputs {
		if !at.IsZero() {
			said = append(said, at)
		}
	}
	if len(said) == 0 {
		return false
	}
	var seen time.Time
	var ahead []time.Time
	marked := false
	_ = b.state.read(func(f *appStateFile) {
		seen, marked = f.Seen[id]
		ahead = f.SeenAhead[id]
	})
	if !marked {
		seen = b.unreadFrom()
	}
	for _, at := range said {
		if at.After(seen) && !slices.ContainsFunc(ahead, at.Equal) {
			return true
		}
	}
	return false
}

// unreadOf is the agent's unread for a method's answer. The answer is not an
// agent.changed, so it neither waits for one being said nor counts as what
// the window was told: an event writer the window has stalled never holds a
// method up.
func (b *appBackend) unreadOf(id string) bool {
	var at []time.Time
	b.locked(func() { at = b.outputsLocked(id, b.agents[id]) })
	return b.unreadAt(id, at)
}

// sayingUnread runs fn holding [appBackend.unreadSay].
func (b *appBackend) sayingUnread(fn func()) {
	b.unreadSay.Lock()
	defer b.unreadSay.Unlock()
	fn()
}

// dressUnread fills the view's unread and remembers it as said, so a later
// change of it alone is said too ([appBackend.restateUnread]).
func (b *appBackend) dressUnread(view *appwire.Agent) {
	b.sayingUnread(func() { b.dressUnreadLocked(view) })
}

// dressUnreadLocked is [appBackend.dressUnread] for a caller holding
// unreadSay.
func (b *appBackend) dressUnreadLocked(view *appwire.Agent) {
	var at []time.Time
	var a *appAgent
	b.locked(func() {
		a = b.agents[view.ID]
		at = b.outputsLocked(view.ID, a)
	})
	view.Unread = b.unreadAt(view.ID, at)
	if a != nil {
		b.locked(func() { a.saidUnread = view.Unread })
	}
}

// restateUnread says the agent again when its unread is no longer what the
// app was last told. What it was last told is read in the same step as an
// agent.changed sets it, so the saying it starts, worked out afresh, is the
// last word whichever road spoke in between.
func (b *appBackend) restateUnread(id string) {
	var a *appAgent
	stale := false
	b.sayingUnread(func() {
		var at []time.Time
		var said bool
		b.locked(func() {
			if a = b.agents[id]; a != nil {
				at, said = b.outputsLocked(id, a), a.saidUnread
			}
		})
		stale = a != nil && said != b.unreadAt(id, at)
	})
	if stale {
		b.sayListed(a, id)
	}
}

// noteTurnOutput reads the answer stamp the session wrote when its turn
// sealed, which is before the turn's end reached this process.
func (b *appBackend) noteTurnOutput(id string) {
	var transcript string
	b.locked(func() {
		if a := b.agents[id]; a != nil {
			transcript = a.transcript
		}
	})
	if transcript == "" {
		return
	}
	meta, err := session.LoadMeta(session.DirOf(transcript))
	if err != nil || meta.LastReplyAt.IsZero() {
		return
	}
	b.locked(func() {
		if a := b.agents[id]; a != nil {
			a.repliedAt = laterOf(a.repliedAt, meta.LastReplyAt)
		}
	})
	b.restateUnread(id)
}

// noteAsk keeps a question put to the person while it is open, from when it
// was first put: its own record's time where it keeps one, so an ask said
// again after a restart, put aside or restated, keeps its time. It is noted
// before the window hears of it and said after ([appBackend.unreadSaw]).
func (b *appBackend) noteAsk(ask appwire.Ask) {
	at := b.askedAt(ask)
	b.locked(func() {
		if _, known := b.openAsks[ask.Agent][ask.ID]; known {
			return
		}
		if b.openAsks == nil {
			b.openAsks = map[string]map[string]time.Time{}
		}
		if b.openAsks[ask.Agent] == nil {
			b.openAsks[ask.Agent] = map[string]time.Time{}
		}
		b.openAsks[ask.Agent][ask.ID] = at
	})
}

// noteAskClosed forgets a question once it is answered, taken back or gone
// with its conversation.
func (b *appBackend) noteAskClosed(id, ask string) {
	b.locked(func() { b.dropAsksLocked(id, []appwire.Ask{{ID: ask}}) })
	b.restateUnread(id)
}

// askedAt is when an ask was first put: its own record's time (a proposal's,
// an order run's, a crew decision's raising), and only a question with none,
// which lives no longer than the conversation this process holds, now.
func (b *appBackend) askedAt(ask appwire.Ask) time.Time {
	if !ask.Asked.IsZero() {
		return ask.Asked
	}
	if ask.Subkind == appwire.SubkindCrewDecision && ask.Subject != nil {
		var run *appCrewRun
		b.locked(func() { run = b.crewRun })
		var raised time.Time
		if run != nil {
			run.locked(func() { raised = run.packets[ask.Subject.Ref].Raised })
		}
		if !raised.IsZero() {
			return raised
		}
	}
	return b.now()
}

// noteElsewhere reads what an agent another process holds has said off its
// folder, so a turn sealed or a question put or answered there is said here
// as agents.list would say it.
func (b *appBackend) noteElsewhere(a *appAgent) {
	var dir string
	b.locked(func() {
		if a.live == nil && a.transcript != "" {
			dir = session.DirOf(a.transcript)
		}
	})
	if dir == "" {
		return
	}
	var replied, asked time.Time
	if meta, err := session.LoadMeta(dir); err == nil {
		replied = meta.LastReplyAt
	}
	if p, live := session.ReadSessionPresence(dir, b.now()); live && p.NeedsPerson() {
		asked = p.Question.Asked
	}
	b.locked(func() { a.noteRowLocked(replied, asked) })
}

// unreadSaw follows the events that move output: a turn's end, whose answer
// the session has stamped, and a question put to the person or closed.
func (b *appBackend) unreadSaw(data any) {
	switch v := data.(type) {
	case appwire.TurnDone:
		b.noteTurnOutput(v.Agent)
	case appwire.AskOpened:
		b.restateUnread(v.Ask.Agent)
	case appwire.AskClosed:
		b.noteAskClosed(v.Agent, v.ID)
	}
}

// MarkSeen is agents.mark_seen: the person has seen the agent up to now. Only
// the mark is written: the agent is not opened or warmed, and one in Closed or
// the archive stays there.
func (b *appBackend) MarkSeen(id string) error {
	if _, err := b.seenTarget(id); err != nil {
		return err
	}
	b.keepUnreadFrom()
	var outputs []time.Time
	b.locked(func() { outputs = b.outputsLocked(id, b.agents[id]) })
	// SEEN UP TO NOW, NEVER AHEAD OF IT: a stamp from a clock running ahead
	// must not hide the output that comes after it. Each output stamped ahead
	// is kept as seen by its own instant, so the dot this was asked to clear
	// does not stay, and other output, whatever its stamp, still counts.
	now := b.now()
	var ahead []time.Time
	for _, at := range outputs {
		if at.After(now) {
			ahead = append(ahead, at)
		}
	}
	err := b.state.write(func(f *appStateFile) error {
		if f.Seen == nil {
			f.Seen = map[string]time.Time{}
		}
		f.Seen[id] = now
		if len(ahead) > 0 {
			if f.SeenAhead == nil {
				f.SeenAhead = map[string][]time.Time{}
			}
			f.SeenAhead[id] = ahead
		} else {
			delete(f.SeenAhead, id)
		}
		return nil
	})
	if err != nil {
		return appEngineError(err)
	}
	// SAID AGAINST WHAT THE APP WAS LAST TOLD, not against what it read
	// before the write: a turn that ended between the read and the write has
	// already said unread, and this mark takes it back.
	b.restateUnread(id)
	return nil
}

// seenTarget is the agent a mark is for, found without refreshing what
// another process says of it: a mark never says an agent's state, so a closed
// one a crashed terminal left a presence for is never said idle.
func (b *appBackend) seenTarget(id string) (*appAgent, error) {
	var a *appAgent
	b.locked(func() {
		if have := b.agents[id]; have != nil && have.transcript != "" {
			a = have
		}
	})
	if a != nil {
		return a, nil
	}
	row, ok := appRowOnDisk(id)
	if !ok {
		return nil, appwire.Errorf(appwire.CodeNotFound, "there is no conversation with id %s", id)
	}
	b.remember(row)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agents[id], nil
}

// sayListed says an agent whose unread changed exactly as agents.list says
// it, so a closed or archived one is said closed or archived: the view this
// process keeps of an agent read off the disk carries neither.
func (b *appBackend) sayListed(a *appAgent, id string) {
	row, ok := appRowOnDisk(id)
	if !ok {
		b.sayAgent(a)
		return
	}
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: b.listedView(row, b.idleCut())})
}
