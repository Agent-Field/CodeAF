package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TRAFFIC: WHAT PASSES BETWEEN A TEAM'S MANAGER AND ITS MEMBERS ───────────
//
// The Traffic log (internal/teams' traffic.go) is the only channel between this
// interface and the team tools a model calls (internal/session): the manager's
// notes and directives, the members' posts, and the two acts the manager asks
// for, stop and start. This file is the interface's side of it, the clock, the
// read and the acts; teamrail.go draws it.
//
// THE READ IS OFF THE LOOP, AND A QUIET SECOND COSTS A STAT. A clock of its own
// ([trafficEvery]) runs while this window holds a conversation in a team that
// HAS A MANAGER, and at no other time: a team without one writes no Traffic
// worth reading here. Each turn of the clock asks, beside the door line
// ([app.besideLine]), for each held team's log after the cursor this window
// holds and for the teams file only if its stamp moved ([TeamsSeam]); the
// seam stats before it reads (internal/teams' [teamstore.Watch]), so a quiet
// log is answered with nothing from one stat, and over --host with a frame of
// a few bytes. What came back is folded into a cache on the loop, the clock is
// set for its next turn there, and when nothing was new the loop touches
// nothing and tells the frame it may draw what it drew before (view.go's still
// frame). After [trafficIdleTurns] quiet turns the clock slows to
// [trafficEveryIdle], and anything that moves brings it back. THE FRAME DRAWS
// THE CACHE AND NOTHING ELSE (framedisk_law_test.go). The same turn picks up
// the teams file when another process changed it, and gives a member its
// title when it joined before it had one ([teamstore.DeriveHandle] through the
// store's tidy, written as an ordinary edit, [app.teamEdit]).
//
// OVER --host THE CLOCK READS THE ENGINE'S LOG. The session writes its Traffic
// into the profile of the machine it runs on, and the --host door's seam asks
// that machine (internal/remote's Teams.Traffic), so the rail is the team's
// own. An engine without those doors hands no seam, and then there is no clock
// at all ([app.teamsOff]): a rail read from this window's profile would be an
// empty log drawn as if it were the team's.
//
// THE ACTS ARE DONE ONCE, AND ONLY NEW ONES. A stop addressed to a
// conversation this window holds ends that conversation's current turn, as the
// person's own stop does; a start is done by the window that holds the team's
// manager, which opens a conversation in the team's folder BEHIND the one in
// front and puts it in the team under the handle the manager chose. It never
// comes forward and it is never sent the brief as the person's words: the
// brief is the manager's, it reaches the member through the same Traffic
// delivery every other line of the manager's does, and the member's first turn
// is started by the session on seeing itself started (internal/session's
// team_wake.go). Each act is done once per entry id, remembered in memory, and
// a window opening reads the log from its TAIL, so what was asked before it
// opened is history on its rail and is never done again.

const (
	// trafficEvery is how often the log is looked at while this window holds a
	// conversation in a managed team, and trafficEveryIdle how often once
	// trafficIdleTurns looks in a row found nothing.
	trafficEvery     = time.Second
	trafficEveryIdle = 5 * time.Second
	trafficIdleTurns = 10
	// trafficKeep is how many entries a team's cache holds, and how many a
	// first look reads from the tail.
	trafficKeep = 200
	// trafficPage is the most one read pages forward; a longer backlog is read
	// on the turns after.
	trafficPage = 200
	// trafficFromStart is the cursor for a log that was empty at the first
	// look: every entry after it is new.
	trafficFromStart = "000000000000"
)

// trafficState is the cache, the clock and the rail's own state.
type trafficState struct {
	// ticking says the clock is turning (a trafficTickMsg or the read it
	// started is on its way), and reading that a read is out, so a turn of the
	// clock never starts a second one beside it. idle counts the turns in a row
	// that found nothing.
	ticking bool
	reading bool
	idle    int
	// cursor is each team's last entry id read, by team id; a team with none
	// has not been looked at yet, and its first read is the tail.
	cursor map[string]string
	// rows is each team's cached entries, oldest first, at most trafficKeep.
	rows map[string][]teamstore.Entry
	// done is every stop and start acted on, by team id and entry id.
	done map[string]bool
	// stamp is the teams file's stamp when it was last read or written here
	// (internal/teams' stamp.go), which is what a turn asks the seam to answer
	// "same" to. edits counts this window's own edits and wrote the edits the
	// last write covered, so a read that crossed an edit, or came back while one
	// was still unwritten, does not put the list from before it back.
	stamp string
	edits int
	wrote int
	// seen is, per team, the newest entry the person has had in front of them
	// on the rail, which is what the closed edge counts past.
	seen map[string]string
	// hidden says the person put the rail away on a frame wide enough for it,
	// and over that the traffic is laid over the body as a card on a frame
	// that is not. Both are this window's, in memory: another window keeps its
	// own, as it keeps its own tabs.
	hidden bool
	over   bool
	// open is every message the person laid out in full on the rail or under
	// a thread card, by team and entry id (teamthread.go), and opened counts the
	// presses that changed it, which is what the rail's cache keys on. Memory
	// only, and this window's.
	open   map[string]bool
	opened int
	// version counts the reads that changed any team's cache, which is what a
	// thread card in the conversation keys on (teamthreadcard.go).
	version int
	// The last frame's rail, for the pointer (teamrail.go).
	drawn trafficDrawn
	cache trafficCache
}

// trafficTickMsg is one turn of the Traffic clock coming round.
type trafficTickMsg struct{}

// trafficJob is one team's read: after the cursor, or the tail on a first look.
type trafficJob struct {
	id    string
	after string
	first bool
}

// trafficGot is what one team's read found.
type trafficGot struct {
	trafficJob
	entries []teamstore.Entry
	err     error
}

// trafficTitle is a member that joined with no title and has one now.
type trafficTitle struct{ team, key, word string }

// ── THE CLOCK AND THE READ ──────────────────────────────────────────────────

// trafficHeld reports whether this window holds key: in front or behind.
func (a *app) trafficHeld(key string) bool {
	return key != "" && (key == a.frontTabKey() || a.behind[key] != nil)
}

// trafficTeamWanted reports whether team t is worth reading: it has a manager
// and this window holds one of its conversations. It allocates nothing.
func (a *app) trafficTeamWanted(t team) bool {
	if t.Manager == "" {
		return false
	}
	for _, m := range t.Members {
		if a.trafficHeld(m.Key) {
			return true
		}
	}
	return false
}

// trafficWanted reports whether any team is worth reading, which is when the
// clock runs. It allocates nothing: it is asked after every message.
func (a *app) trafficWanted() bool {
	if !a.wall.loaded || len(a.wall.teams) == 0 || a.teamsOff() {
		return false
	}
	for _, t := range a.wall.teams {
		if a.trafficTeamWanted(t) {
			return true
		}
	}
	return false
}

// trafficArm starts the clock when there is something to read and it is not
// already turning. It is asked after every message ([app.Update]), so a team
// made, a member joined or a manager opened starts it without each of those
// doors having to know. Its first turn reads at once; the read's fold sets the
// turn after it ([app.trafficTake]).
func (a *app) trafficArm() tea.Cmd {
	if a.traffic.ticking || !a.trafficWanted() {
		return nil
	}
	a.traffic.ticking = true
	a.traffic.idle = 0
	return a.trafficReadOf(a.trafficIDs(), true)
}

// trafficIDs is every managed team this window holds a conversation in.
func (a *app) trafficIDs() []string {
	var ids []string
	for _, t := range a.wall.teams {
		if a.trafficTeamWanted(t) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// trafficNext is the clock's next turn: a timer and nothing else. The read is
// started on the loop when it comes round ([app.trafficTick]), so it carries
// the cursors as they are then.
func (a *app) trafficNext() tea.Cmd {
	every := trafficEvery
	if a.traffic.idle >= trafficIdleTurns {
		every = trafficEveryIdle
	}
	return surfaceTick(every, func(time.Time) tea.Msg { return trafficTickMsg{} })
}

// trafficTick is a turn of the clock: read, and come round again from the
// read's fold, while there is anything to read. It stops itself when there is
// not, and [app.trafficArm] starts it again. The turn itself changes nothing a
// frame draws, so it is always quiet; the read's fold says whether anything
// moved.
func (a *app) trafficTick(trafficTickMsg) (cmd tea.Cmd, quiet bool) {
	if !a.trafficWanted() {
		a.traffic.ticking = false
		return nil, true
	}
	return a.trafficReadOf(a.trafficIDs(), true), true
}

// trafficTitles is every member that joined with no title and whose tab has
// one now. It reads memory, and allocates only when there is one.
func (a *app) trafficTitles() []trafficTitle {
	var titles []trafficTitle
	for _, t := range a.wall.teams {
		if !a.trafficTeamWanted(t) {
			continue
		}
		for _, m := range t.Members {
			if strings.TrimSpace(m.Word) != "" {
				continue
			}
			for _, tab := range a.chatTabs {
				if tab.key == m.Key && !tab.start && !tab.work && strings.TrimSpace(tab.word) != "" {
					titles = append(titles, trafficTitle{team: t.ID, key: m.Key, word: tab.word})
					break
				}
			}
		}
	}
	return titles
}

// trafficRead reads every managed team this window holds a conversation in,
// the teams file with them, once, without setting the clock. It is a test's.
func (a *app) trafficRead() tea.Cmd { return a.trafficReadOf(a.trafficIDs(), false) }

// trafficReadOf reads the logs of teams ids after their cursors, and the teams
// file when its stamp moved, through the seam, beside the door line, and folds
// what it found in ([app.trafficTake]). tick says this read is a turn of the
// clock, whose fold sets the next one; a turn that finds a read already out
// leaves the answer to it and only comes round again.
func (a *app) trafficReadOf(ids []string, tick bool) tea.Cmd {
	if titles := a.trafficTitles(); len(titles) > 0 {
		// A member that joined before it had a title takes its tab's name,
		// and with it a handle, as an ordinary edit: [app.teamEdit] gives every
		// member this window has a tab for its tab's name on the way, and the
		// write is made after this message ([app.teamsWrite]).
		_ = a.teamEdit(func(*teamstore.File) error { return nil })
	}
	if a.traffic.reading || !a.wall.loaded {
		if tick {
			return a.trafficNext()
		}
		return nil
	}
	var jobs []trafficJob
	for _, id := range ids {
		after, seen := a.traffic.cursor[id]
		jobs = append(jobs, trafficJob{id: id, after: after, first: !seen})
	}
	a.traffic.reading = true
	seam, reserved, stamp, edits := a.teamsSeam(), teamReservedHues(a.pal), a.traffic.stamp, a.traffic.edits
	return a.besideLine(func() func(bool) tea.Cmd {
		got := make([]trafficGot, len(jobs))
		for i, j := range jobs {
			after, limit := j.after, trafficPage
			if j.first {
				after, limit = "", trafficKeep
			}
			entries, err := seam.Traffic(j.id, after, limit)
			got[i] = trafficGot{trafficJob: j, entries: entries, err: err}
		}
		fresh, at, same, err := seam.ReadSince(stamp, reserved)
		if err != nil || same {
			fresh, at = nil, stamp
		}
		return func(bool) tea.Cmd { return a.trafficTake(got, fresh, at, edits, tick) }
	})
}

// trafficTake folds one read in, on the loop: the teams file when it changed
// and nothing was edited here since the read began or is still unwritten, each
// team's new entries onto its cache, and the stops and starts among them done.
// A read that found nothing new leaves the frame before it standing. A turn of
// the clock sets the next one here, slower after a run of quiet ones.
func (a *app) trafficTake(got []trafficGot, fresh []team, at string, edits int, tick bool) tea.Cmd {
	a.traffic.reading = false
	changed := false
	if edits == a.traffic.edits && a.traffic.wrote == a.traffic.edits {
		if fresh != nil {
			a.teamAdopt(teamsClone(fresh))
			changed = true
		}
		a.traffic.stamp = at
	}
	if a.traffic.cursor == nil {
		a.traffic.cursor, a.traffic.rows, a.traffic.done = map[string]string{}, map[string][]teamstore.Entry{}, map[string]bool{}
	}
	var acts []tea.Cmd
	for _, g := range got {
		if g.err != nil {
			continue
		}
		if cur, seen := a.traffic.cursor[g.id]; g.first == seen || (!g.first && cur != g.after) {
			// Another read got here first; this one's view of the cursor is
			// stale, and taking it would add its entries twice.
			continue
		}
		if len(g.entries) == 0 {
			if g.first {
				a.traffic.cursor[g.id] = trafficFromStart
			}
			continue
		}
		a.traffic.cursor[g.id] = g.entries[len(g.entries)-1].ID
		rows := append(a.traffic.rows[g.id], g.entries...)
		if len(rows) > trafficKeep {
			rows = append([]teamstore.Entry(nil), rows[len(rows)-trafficKeep:]...)
		}
		a.traffic.rows[g.id] = rows
		changed = true
		if g.first {
			// A first look is history: what was asked before this window
			// opened was asked of another window, or of none, and doing it now
			// would do it twice or late. It is history the person has not been
			// shown, but it is not news either, so the edge counts nothing
			// for it.
			if _, ok := a.traffic.seen[g.id]; !ok {
				if a.traffic.seen == nil {
					a.traffic.seen = map[string]string{}
				}
				a.traffic.seen[g.id] = a.traffic.cursor[g.id]
			}
			continue
		}
		for _, e := range g.entries {
			if cmd := a.trafficAct(g.id, e); cmd != nil {
				acts = append(acts, cmd)
			}
		}
	}
	if changed {
		a.traffic.version++
		a.touch()
	} else if len(acts) == 0 {
		a.ptr.still = a.drawn
	}
	if tick {
		if changed {
			a.traffic.idle = 0
		} else {
			a.traffic.idle++
		}
		if a.trafficWanted() {
			acts = append(acts, a.trafficNext())
		} else {
			a.traffic.ticking = false
		}
	}
	return tea.Batch(acts...)
}

// ── THE ACTS ────────────────────────────────────────────────────────────────

// trafficAct does what one new entry asks of this window, once per entry id: a
// stop for a conversation it holds, a start when it holds the team's manager.
func (a *app) trafficAct(teamID string, e teamstore.Entry) tea.Cmd {
	if e.Kind != teamstore.KindStop && e.Kind != teamstore.KindStart {
		return nil
	}
	done := teamID + "/" + e.ID
	if a.traffic.done[done] {
		return nil
	}
	t, ok := a.teamByID(teamID)
	if !ok {
		return nil
	}
	switch e.Kind {
	case teamstore.KindStop:
		key := trafficMemberKey(t, e.To, e.Member)
		if !a.trafficHeld(key) {
			return nil
		}
		a.traffic.done[done] = true
		a.trafficStop(key)
		return nil
	case teamstore.KindStart:
		if t.Manager == "" || !a.trafficHeld(t.Manager) {
			return nil
		}
		a.traffic.done[done] = true
		return a.trafficStart(t, e)
	}
	return nil
}

// trafficMemberKey is the member an entry addresses: by the conversation key
// it carries when it carries one the team holds, and otherwise by its handle.
func trafficMemberKey(t team, handle, key string) string {
	if key != "" && t.Holds(key) {
		return key
	}
	if m, ok := t.ByHandle(handle); ok {
		return m.Key
	}
	return ""
}

// trafficStop is the manager's stop for a conversation this window holds. It
// is the person's own stop and nothing more: the current turn ends, and what
// is queued behind it is left alone.
func (a *app) trafficStop(key string) {
	if key == a.frontTabKey() {
		if a.state != stateWorking {
			return
		}
		a.interruptTurn()
		a.note(a.teamManagerMark() + " the manager stopped this turn")
		return
	}
	if held := a.behind[key]; held != nil && held.conv.Agent != nil {
		held.conv.Agent.Interrupt()
	}
}

// trafficStart is the manager's start: a new conversation in the team's
// folder, opened BEHIND the one in front and never brought forward, in the
// team under the handle the manager chose.
//
// NOTHING A MANAGER DOES MOVES THE PERSON'S FOCUS. Whoever is in front stays
// in front and keeps the keyboard: a start that took the front would send the
// next words somebody was typing to the manager into a conversation they have
// never seen. The new member arrives as a tab at the end of the team's run,
// named by its handle (`@lexer`) until it has a title, and its working mark is
// the only thing that stirs.
//
// THE BRIEF IS NOT SENT FROM HERE. It is the manager's line in the Traffic,
// and the member reads it there as the manager's, at its first step; the
// session starts that first turn itself once it sees it has been made a
// member (internal/session's team_wake.go).
//
// The conversation is opened on the door line, off the loop, because opening
// one is a call to the engine; what comes back is held and joined here.
func (a *app) trafficStart(t team, e teamstore.Entry) tea.Cmd {
	handle := strings.TrimSpace(e.To)
	switch {
	case a.shared:
		a.trafficStartRefused(handle, oneConversationWord)
		return nil
	case a.start == nil || !a.canStart():
		a.trafficStartRefused(handle, newUnavailableWord)
		return nil
	}
	start, where, id := a.start, a.teamWhere(t), t.ID
	return a.besideLine(func() func(bool) tea.Cmd {
		conv, err := start(where)
		return func(bool) tea.Cmd { return a.trafficStarted(id, handle, conv, err) }
	})
}

// trafficStartRefused says, beside the manager, why a start was not done.
func (a *app) trafficStartRefused(handle, why string) {
	a.note(a.teamManagerMark() + " could not start @" + handle + ": " + why)
}

// trafficStarted is the start's conversation back from the engine: held
// behind, joined to the team under its handle, and said beside the manager.
func (a *app) trafficStarted(id, handle string, conv Conversation, err error) tea.Cmd {
	if err != nil || conv.Agent == nil {
		why := "the conversation did not open"
		if err != nil {
			why = err.Error()
		}
		a.trafficStartRefused(handle, why)
		return nil
	}
	t, ok := a.teamByID(id)
	if !ok {
		// The team went while the conversation was opening. It is held all the
		// same, as any other conversation this window opened.
		t = team{}
	}
	key := a.convKey(conv.SessionFile)
	cmd := a.stow(conv, nil)
	a.trafficBehindTop(key)
	a.chatTabBar = tabBar{}
	if t.ID != "" {
		m := teamMember{Key: key, File: conv.SessionFile, Where: conv.Workspace, Handle: handle}
		if err := a.teamEdit(func(f *teamstore.File) error {
			if err := f.AddMember(t.ID, m); err != nil {
				return err
			}
			if got, _ := f.Teams[teamIndex(f.Teams, t.ID)].Member(m.Key); got.Handle != handle && teamstore.ValidHandle(handle) == nil {
				_ = f.SetHandle(t.ID, m.Key, handle)
			}
			return nil
		}); err != nil {
			a.note("@" + handle + " is in " + t.Name + " for this window, but " + err.Error())
		}
	}
	if t.Manager != "" && t.Manager == a.frontTabKey() {
		a.note(a.teamManagerMark() + " started @" + handle + " · its tab is on the strip")
	}
	a.touch()
	return cmd
}

// trafficBehindTop keeps a conversation opened behind from becoming the one
// `tab` goes back to. The keeper put it on top of the previous-stack as it
// does every conversation it takes ([app.rememberOpen]); a start is not
// somewhere the person has been, so it goes under the two they have.
func (a *app) trafficBehindTop(key string) {
	n := len(a.prev)
	if key == "" || n < 2 || a.prev[n-1] != key {
		return
	}
	if n == 2 {
		a.prev[0], a.prev[1] = key, a.prev[0]
		return
	}
	a.prev[n-1], a.prev[n-2] = a.prev[n-2], a.prev[n-3]
	a.prev[n-3] = key
}
