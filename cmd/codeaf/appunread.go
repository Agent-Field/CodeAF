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
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appRowOutputAt is when a conversation on disk last said something to the
// person: its last sealed answer, or the question it is waiting on now.
func appRowOutputAt(row session.SessionRow) time.Time {
	at := row.ReplyAt
	if row.NeedsPerson() && row.Presence.Question.Asked.After(at) {
		at = row.Presence.Question.Asked
	}
	return at
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

// unreadAt says output at outputAt is unread for the agent id.
func (b *appBackend) unreadAt(id string, outputAt time.Time) bool {
	if outputAt.IsZero() {
		return false
	}
	var seen time.Time
	marked := false
	_ = b.state.read(func(f *appStateFile) { seen, marked = f.Seen[id] })
	if !marked {
		seen = b.unreadFrom()
	}
	return outputAt.After(seen)
}

// dressUnread fills the view's unread and remembers it as said, so a later
// change of it alone is said too ([appBackend.noteOutput]).
func (b *appBackend) dressUnread(view *appwire.Agent) {
	var at time.Time
	var a *appAgent
	b.locked(func() {
		if a = b.agents[view.ID]; a != nil {
			at = a.outputAt
		}
	})
	view.Unread = b.unreadAt(view.ID, at)
	if a != nil {
		b.locked(func() { a.saidUnread = view.Unread })
	}
}

// noteOutput moves when the agent last said something to the person, and says
// it again when that turns it unread.
func (b *appBackend) noteOutput(id string, at time.Time) {
	var a *appAgent
	var said bool
	b.locked(func() {
		if a = b.agents[id]; a != nil {
			a.outputAt = laterOf(a.outputAt, at)
			at, said = a.outputAt, a.saidUnread
		}
	})
	if a == nil || said || !b.unreadAt(id, at) {
		return
	}
	b.sayListed(a, id)
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
	b.noteOutput(id, meta.LastReplyAt)
}

// unreadSaw follows the events that are output: a turn's end, whose answer the
// session has stamped, and a question put to the person.
func (b *appBackend) unreadSaw(data any) {
	switch v := data.(type) {
	case appwire.TurnDone:
		b.noteTurnOutput(v.Agent)
	case appwire.AskOpened:
		b.noteOutput(v.Ask.Agent, b.now())
	}
}

// MarkSeen is agents.mark_seen: the person has seen the agent up to now. Only
// the mark is written: the agent is not opened or warmed, and one in Closed or
// the archive stays there.
func (b *appBackend) MarkSeen(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	b.keepUnreadFrom()
	var outputAt time.Time
	b.locked(func() { outputAt = a.outputAt })
	was := b.unreadAt(id, outputAt)
	// SEEN UP TO NOW, and never short of what it has already said: a clock
	// behind the session's own stamp must not leave the dot it was asked to
	// clear.
	at := laterOf(b.now(), outputAt)
	err = b.state.write(func(f *appStateFile) error {
		if f.Seen == nil {
			f.Seen = map[string]time.Time{}
		}
		f.Seen[id] = at
		return nil
	})
	if err != nil {
		return appEngineError(err)
	}
	if was {
		b.sayListed(a, id)
	}
	return nil
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
