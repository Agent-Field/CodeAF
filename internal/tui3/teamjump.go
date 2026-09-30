package tui3

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TAKEN TO THE MESSAGE ────────────────────────────────────────────────────
//
// A handle on the Traffic, or on a thread card's answer, opens that member's
// conversation SCROLLED TO THE MESSAGE the row is about, not just the
// conversation: a thread's header lands at the directive as the member was
// told it, and an answer lands at the member's own post. The words on the
// thread's header land at the thread card in the manager's own conversation.
//
// Every place a Traffic entry sits in a transcript carries its number: a
// delivered note ends each line with ` #N` (session's teamLine, read back as
// [session.TeamLine.Thread]), a team_send's answer says `(#N)` and a
// team_post's says ` as #N`. The jump finds the newest entry carrying the
// number, scrolls it into view and lifts it for [trafficLandFor]. The focus
// stays on the conversation just opened: nothing here takes the keyboard.
//
// A conversation that is still opening is waited for: the jump is kept, with
// its member's key, until that conversation is in front, and is dropped after
// [trafficJumpWait] so a later visit does not scroll by surprise. A number
// that is in no entry (an older message, from before this conversation's
// history) opens at the bottom and says so in the hint line.

// trafficLandFor is how long a landed message stays lifted, and
// trafficJumpWait how long a jump waits for its conversation to open.
const (
	trafficLandFor  = 1600 * time.Millisecond
	trafficJumpWait = 10 * time.Second
)

// trafficOlderWords is what the hint line says when the message is not in the
// conversation's history.
const trafficOlderWords = "that message is older than this chat's history"

// trafficJumpTo is a jump waiting for its conversation, and trafficLanding
// the entry lifted after one, or the hint said when it found nothing.
type trafficJumpTo struct {
	key, id, team string
	until         time.Time
	waking        bool
}

type trafficLanding struct {
	entry int
	older bool
	until time.Time
}

// trafficLandedMsg is the lift expiring: one repaint.
type trafficLandedMsg struct{}

// trafficJumpDeadlineMsg gives a still-loading conversation one final chance
// to land before the older-history hint is shown.
type trafficJumpDeadlineMsg struct{}

// trafficJump opens member key's conversation at Traffic entry id, or in
// front at id when key is "" or already in front.
func (a *app) trafficJump(key, id string) tea.Cmd {
	// The column's team owns the clicked message. The rail filter can instead
	// name a team above it, where this manager is also an ordinary member.
	t, _, _ := a.sideTeam()
	return a.trafficJumpIn(key, id, t.ID)
}

// trafficJumpIn retains the source team before opening another conversation,
// whose own team or rail filter must not change the message's identity.
func (a *app) trafficJumpIn(key, id, teamID string) tea.Cmd {
	var cmd tea.Cmd
	if key != "" && key != a.frontTabKey() {
		cmd = a.trafficGo(key)
	}
	if key == "" {
		key = a.frontTabKey()
	}
	a.traffic.jump = trafficJumpTo{key: key, id: id, team: teamID, until: a.now().Add(trafficJumpWait)}
	return tea.Batch(cmd, a.trafficLand())
}

// trafficLand finishes a waiting jump once its conversation is in front. It
// runs on the loop after every message (app.go's Update).
func (a *app) trafficLand() tea.Cmd {
	j := a.traffic.jump
	if j.key == "" {
		return nil
	}
	if a.frontTabKey() != j.key {
		if !a.now().Before(j.until) {
			a.traffic.jump = trafficJumpTo{}
		}
		return nil
	}
	if j.id == "" {
		a.traffic.jump = trafficJumpTo{}
		return nil
	}
	at := -1
	if a.now().Before(j.until) {
		at = a.teamEntryAtIn(j.id, j.team)
	}
	if at < 0 && (a.hostReplayLoading || a.hostReplayWaiting) && a.now().Before(j.until) {
		if !j.waking {
			j.waking = true
			a.traffic.jump = j
			return tea.Tick(j.until.Sub(a.now())+time.Millisecond, func(time.Time) tea.Msg { return trafficJumpDeadlineMsg{} })
		}
		return nil
	}
	a.traffic.jump = trafficJumpTo{}
	land := trafficLanding{entry: at, until: a.now().Add(trafficLandFor)}
	if at < 0 {
		a.offset, a.stick = 0, true
		land.older = true
		land.until = a.now().Add(3 * trafficLandFor)
	} else {
		a.revealTrafficEntry(at)
		a.revealMiddle(at)
	}
	a.traffic.landing = land
	a.touch()
	wait := land.until.Sub(a.now()) + 50*time.Millisecond
	return tea.Tick(wait, func(time.Time) tea.Msg { return trafficLandedMsg{} })
}

// A deliberate message jump opens its enclosing disclosures before scrolling.
// Hidden history remains compact everywhere else in the conversation.
func (a *app) revealTrafficEntry(at int) {
	d := a.conversation()
	folds := a.deckFolds(d)
	if a.workOpen == nil {
		a.workOpen = make(map[int]bool)
	}
	for _, f := range folds {
		if at >= f.start && at < f.answer {
			a.workOpen[f.key] = true
		}
	}
	stampHierarchy(d.entries, folds)
	if a.capOpen == nil {
		a.capOpen = make(map[int]bool)
	}
	for _, c := range deriveCaptions(d.entries, d.runningTurn) {
		if at >= c.start && at < c.end {
			a.capOpen[c.start] = true
		}
	}
	a.touch()
}

// teamEntryAt is the newest entry of the conversation that carries Traffic
// entry id, -1 for none.
func (a *app) teamEntryAt(id string) int {
	t, _, _ := a.sideTeam()
	return a.teamEntryAtIn(id, t.ID)
}

// teamEntryAtIn matches both parts of a Traffic message's identity. Numbers
// count separately in every team, including the optional All teams group.
func (a *app) teamEntryAtIn(id, teamID string) int {
	if id == "" {
		return -1
	}
	shown, inTeam := a.teamByID(teamID)
	if teamID != "" && !inTeam {
		return -1
	}
	// A START ROOT IS ANSWERED BY ITS OWN TEAM'S MATCHER FIRST. Traffic numbers
	// count per team, so a `team_send` into another team can carry the same
	// `(#N)` as this team's start, and the newest-first search below would land
	// on a send rather than the start. [app.teamStartEntryAt] also checks the
	// accepted brief and recipient, so a start keeps its own call.
	if inTeam {
		if at := a.teamStartEntryAt(id, shown); at >= 0 {
			return at
		}
	}
	number := teamstore.ThreadNumber(id)
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		switch e.kind {
		case entryTeam:
			for _, l := range e.team {
				if l.Thread == id && (!inTeam || a.trafficLineBelongs(l, shown)) {
					return i
				}
			}
		case entryTool:
			switch e.tool {
			case "team_send":
				if inTeam && sentElsewhere(e, shown) {
					continue
				}
				if strings.Contains(e.detail.Output, "("+number+")") {
					return i
				}
			case "team_post":
				if inTeam && sentElsewhere(e, shown) {
					continue
				}
				if strings.Contains(e.detail.Output, " as "+number+",") || strings.Contains(e.detail.Output, " as "+number+".") {
					return i
				}
			}
		}
	}
	return -1
}

// trafficLineBelongs keeps deliveries in their source team. A delivery stores
// the name it arrived under, so after a rename its retained Traffic row must
// still identify it; a name that belongs to another team never does.
func (a *app) trafficLineBelongs(line session.TeamLine, target team) bool {
	if line.Team == "" || line.Team == target.ID || strings.EqualFold(line.Team, target.Name) {
		return true
	}
	for _, t := range a.wall.teams {
		if line.Team == t.ID || strings.EqualFold(line.Team, t.Name) {
			return false
		}
	}
	for _, e := range a.traffic.rows[target.ID] {
		if e.ID == line.Thread && e.Kind == line.Kind && e.From == line.From && strings.TrimSpace(e.Text) == strings.TrimSpace(line.Text) {
			return true
		}
	}
	return false
}

// sentElsewhere reports a team tool that named a team other than shown, whose
// receipt counts in that team's traffic and says nothing about this one. A
// call that names no team went to the sender's own and is still a candidate.
func sentElsewhere(e *entry, shown team) bool {
	var args struct{ Team string }
	if json.Unmarshal([]byte(e.detail.Args), &args) != nil {
		return false
	}
	target := strings.TrimSpace(args.Team)
	return target != "" && target != shown.ID && target != shown.Name
}

// revealMiddle scrolls so entry's first row sits a third of the way down the
// view, which puts the message and a little of what came after it in sight.
func (a *app) revealMiddle(entry int) {
	height := a.viewHeight()
	if height <= 0 {
		return
	}
	rows := a.visible(a.bodyWidth())
	for i, r := range rows {
		if r.entry != entry {
			continue
		}
		bottom := max(len(rows)-height, 0)
		at := min(max(i-height/3, 0), bottom)
		a.offset, a.stick = at, at == bottom
		return
	}
}

// trafficLandRows lifts the landed entry's rows on the ground a selected row
// wears. It copies only while a lift is on.
func (a *app) trafficLandRows(rows []row, width int) []row {
	l := a.traffic.landing
	if l.older || l.entry < 0 || !a.now().Before(l.until) {
		return rows
	}
	var out []row
	for i, r := range rows {
		if r.entry != l.entry {
			continue
		}
		if out == nil {
			out = append([]row(nil), rows...)
		}
		out[i].text = a.pal.selected(r.text, width)
	}
	if out == nil {
		return rows
	}
	return out
}

// trafficJumpWords is the hint line's word after a jump found nothing, "" at
// every other time.
func (a *app) trafficJumpWords() string {
	l := a.traffic.landing
	if l.older && a.now().Before(l.until) {
		return trafficOlderWords
	}
	return ""
}

// threadRowLand is the Traffic entry a handle on this row of a thread card
// opens its member at: the answer on an answer's row, the card's own message
// on its header, "" on any other row.
func (a *app) threadRowLand(r row) string {
	if r.open != "" {
		_, id, _ := strings.Cut(r.open, "/")
		return id
	}
	if r.entry < 0 || r.entry >= len(a.entries) {
		return ""
	}
	if m := a.entries[r.entry].thread; m != nil && m.found {
		return m.root
	}
	return ""
}
