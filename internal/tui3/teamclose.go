package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── CLOSING, REOPENING AND DELETING A TEAM (DESIGN.md section 8.5, c-9) ─────
//
// A team is open or closed. `Close…` asks what to do only when there is
// something to decide: with nothing running it closes at once and offers Undo,
// because a question with one useful answer is friction. With work running the
// card offers three ([app.teamSheetOpen]'s close mode):
//
//   - `Wrap up first` (the default while a manager has work running): the
//     manager is asked, in the team's Traffic, to have everyone finish and
//     commit, answer what it can, and bring the person a closing report. The
//     team closes only when the person picks `Close` on that report, which is
//     an ordinary card in the inbox.
//   - `Close now`: every member's current turn is stopped (the person's own
//     Stop, nothing deleted), the team's tabs close, and the team moves to
//     Closed, in one step, with Undo.
//   - `Cancel`.
//
// A CONVERSATION THAT IS ALSO IN ANOTHER OPEN TEAM IS NEVER CLOSED by this, and
// never stopped, with ONE EXCEPTION: the manager of a team being closed. A
// sub-team's manager is also a member of the team above, so it is still that
// team's and keeps its tab; but the turn it is running is this team's work, so
// it counts as working (the card, and `Wrap up first`, rather than a close at
// once while it runs on) and `Close now` stops that turn. Closing a team
// closes the teams under it
// (internal/teams' [teamstore.File.Close]); reopening reopens exactly what the
// close closed. Delete is only ever offered on a closed team, and forgets the
// grouping, the Traffic and the packets; the conversations stay in history.

// teamsUndoFor is how long a close offers Undo: the wall's own span for an
// Apply, so a person learns one length of time for taking a thing back.
const teamsUndoFor = wallOrganizedFor

// teamsUndo is the last close this window made, for Undo.
type teamsUndo struct {
	team string
	name string
	shut []string
	at   time.Time
}

// teamsCloseKeys is every conversation a close of team id stops and whose tab
// it closes: the members of the team and of every open team under it, less any
// that is also in an open team outside them.
func (a *app) teamsCloseKeys(id string) []string {
	closing := map[string]bool{id: true}
	for _, t := range a.teamTree().Descendants(id) {
		if !t.Closed() {
			closing[t.ID] = true
		}
	}
	elsewhere := map[string]bool{}
	for _, t := range a.wall.teams {
		if closing[t.ID] || t.Closed() || t.Root {
			continue
		}
		for _, m := range t.Members {
			elsewhere[m.Key] = true
		}
	}
	var keys []string
	seen := map[string]bool{}
	for _, t := range a.wall.teams {
		if !closing[t.ID] {
			continue
		}
		for _, m := range t.Members {
			if elsewhere[m.Key] || seen[m.Key] {
				continue
			}
			seen[m.Key] = true
			keys = append(keys, m.Key)
		}
	}
	return keys
}

// teamsClosingManagers includes the manager of each team being closed even
// when that conversation is also a member of an open team above it.
func (a *app) teamsClosingManagers(id string) map[string]bool {
	closing := map[string]bool{id: true}
	for _, t := range a.teamTree().Descendants(id) {
		if !t.Closed() {
			closing[t.ID] = true
		}
	}
	managers := map[string]bool{}
	for _, t := range a.wall.teams {
		if closing[t.ID] && t.Manager != "" {
			managers[t.Manager] = true
		}
	}
	return managers
}

// teamsRunning is the members of team id (and the teams under it) that are
// working now, by handle or name, and whether its manager is one of them or
// has any running at all.
func (a *app) teamsRunning(id string) (names []string, managed bool) {
	t, ok := a.teamByID(id)
	if !ok {
		return nil, false
	}
	front := a.frontTabKey()
	managers := a.teamsClosingManagers(id)
	keys := a.teamsCloseKeys(id)
	seen := map[string]bool{}
	for _, key := range keys {
		seen[key] = true
	}
	var extra []string
	for key := range managers {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	for _, key := range keys {
		if !a.trafficHeld(key) || a.tabSignalFor(key, key == front) != tabWorking {
			continue
		}
		name := key
		if managers[key] {
			name = a.teamManagerMark() + " manager"
			names = append(names, name)
			continue
		}
		for _, u := range a.wall.teams {
			if m, ok := u.Member(key); ok {
				name = m.Word
				if m.Handle != "" {
					name = "@" + m.Handle
				}
				if key == u.Manager {
					name = a.teamManagerMark() + " manager"
				}
				break
			}
		}
		names = append(names, name)
	}
	return names, t.Manager != "" && len(names) > 0
}

// teamsCloseAsk is `Close…`: at once with Undo when nothing runs, and the card
// when something does.
func (a *app) teamsCloseAsk(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() {
		return nil
	}
	if t.Root {
		a.tp.msg = "All teams does not close; remove its manager instead"
		a.touch()
		return nil
	}
	if a.teamsOff() {
		a.tp.msg = teamHostedWord
		a.touch()
		return nil
	}
	if names, _ := a.teamsRunning(id); len(names) > 0 {
		return a.teamSheetOpen(id, teamSheetClose)
	}
	return a.teamsCloseNow(id, "")
}

// teamsCloseNow closes team id at once: every member's turn stopped, the
// team's tabs closed, the team moved to Closed, and Undo offered. report is the
// closing report's packet id when the close was the report's `Close`.
func (a *app) teamsCloseNow(id, report string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() || t.Root {
		return nil
	}
	now := a.now()
	shut := a.teamsStopMembers(id)
	if err := a.teamEdit(func(f *teamstore.File) error { return f.Close(id, now, report) }); err != nil {
		a.tp.msg = "not closed: " + err.Error()
		a.touch()
		return nil
	}
	return a.teamsAfterClose(t, shut, now, true)
}

// teamsStopMembers is the interface's half of every close (DESIGN.md 8.5):
// each member of team id (and of the open teams under it, less any shared
// with an open team outside them) that is working has its turn stopped, and
// every one but the conversation in front has its tab closed. It answers the
// tabs it closed, for Undo.
func (a *app) teamsStopMembers(id string) []string {
	front := a.frontTabKey()
	var shut []string
	closing := a.teamsCloseKeys(id)
	covered := map[string]bool{}
	for _, key := range closing {
		covered[key] = true
		if !a.trafficHeld(key) {
			continue
		}
		// THE PERSON'S OWN STOP, and nothing more: the current turn ends and
		// nothing is deleted (teamtraffic.go's [app.trafficStop]).
		if a.tabSignalFor(key, key == front) == tabWorking {
			a.trafficStop(key)
		}
		// The conversation in front is what this page hosts; its tab stays,
		// because closing it here would move the person's focus.
		if key != front {
			a.tabShutKey(key)
			shut = append(shut, key)
		}
	}
	var extra []string
	for key := range a.teamsClosingManagers(id) {
		extra = append(extra, key)
	}
	sort.Strings(extra)
	for _, key := range extra {
		if covered[key] || !a.trafficHeld(key) || a.tabSignalFor(key, key == front) != tabWorking {
			continue
		}
		// A manager shared with an open team keeps its tab but cannot keep
		// working on the team being closed.
		a.trafficStop(key)
	}
	return shut
}

// teamsAfterClose is what follows a close on this window: Undo offered, the
// selection and the wall moved off the team, and its Traffic told when tell
// says the store did not tell it already.
func (a *app) teamsAfterClose(t team, shut []string, now time.Time, tell bool) tea.Cmd {
	id := t.ID
	// On the page the Undo row says it; off it the close is said where the
	// person is standing.
	a.tp.undo = teamsUndo{team: id, name: t.Name, shut: shut, at: now}
	if !a.at(pageTeams) {
		a.note(t.Name + " is closed · its conversations are kept · Closed on the teams page reopens it")
	}
	if a.tp.sel == id {
		a.tp.sel = ""
		a.teamsSettle()
	}
	if a.wall.activeID == id {
		a.wall.activeID = ""
	}
	a.tp.top = teamsTopCache{}
	a.touch()
	if !tell {
		return a.teamsBringManager()
	}
	return tea.Batch(a.teamsTell(id, teamstore.Entry{Kind: teamstore.KindClose, From: teamstore.FromYou, To: teamstore.ToEveryone,
		Text: "the person closed the team"}), a.teamsBringManager())
}

// teamsUndoing reports whether Undo is still offered for the last close.
func (a *app) teamsUndoing() bool {
	u := a.tp.undo
	return u.team != "" && a.now().Sub(u.at) < teamsUndoFor
}

// teamsUndoClose takes the last close back: the team reopened, and the tabs it
// closed back on the strip (the conversations were held behind the whole time).
func (a *app) teamsUndoClose() tea.Cmd {
	if !a.teamsUndoing() {
		return nil
	}
	u := a.tp.undo
	a.tp.undo = teamsUndo{}
	for _, key := range u.shut {
		delete(a.tabShut, key)
	}
	a.chatTabBar = tabBar{}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.Reopen(u.team) }); err != nil {
		a.tp.msg = "not reopened: " + err.Error()
		a.touch()
		return nil
	}
	a.tp.msg = u.name + " is open again"
	a.tp.sel = u.team
	a.tp.top = teamsTopCache{}
	a.touch()
	return tea.Batch(a.teamsTell(u.team, teamstore.Entry{Kind: teamstore.KindReopen, From: teamstore.FromYou, To: teamstore.ToEveryone,
		Text: "the person reopened the team"}), a.teamsBringManager())
}

// teamsReopen is `Reopen` on a closed team, and `Reopen <parent> too` on one
// whose parent is closed: the closed teams above it are reopened first, top
// down, because the store refuses a child under a closed parent
// ([teamstore.ErrParentClosed]). Its members' tabs come back where this window
// still holds them, and its manager resumes by being brought in front.
func (a *app) teamsReopen(id string, withParents bool) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || !t.Closed() {
		return nil
	}
	var chain []string
	if withParents {
		ups := a.teamAncestors(id)
		for i := len(ups) - 1; i >= 0; i-- {
			if ups[i].Closed() {
				chain = append(chain, ups[i].ID)
			}
		}
	} else if p, closed := a.teamsParentClosed(t); closed {
		a.tp.msg = t.Name + " sits under " + p.Name + ", which is closed: reopen " + p.Name + " too"
		a.touch()
		return nil
	}
	chain = append(chain, id)
	err := a.teamEdit(func(f *teamstore.File) error {
		for _, c := range chain {
			if u, ok := f.Team(c); ok && !u.Closed() {
				continue
			}
			if err := f.Reopen(c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		a.tp.msg = "not reopened: " + err.Error()
		a.touch()
		return nil
	}
	for _, c := range chain {
		if u, ok := a.teamByID(c); ok {
			for _, m := range u.Members {
				delete(a.tabShut, m.Key)
			}
		}
	}
	a.chatTabBar = tabBar{}
	a.tp.sel = id
	a.tp.msg = t.Name + " is open again"
	a.tp.top = teamsTopCache{}
	a.touch()
	var tells []tea.Cmd
	for _, c := range chain {
		tells = append(tells, a.teamsTell(c, teamstore.Entry{Kind: teamstore.KindReopen, From: teamstore.FromYou, To: teamstore.ToEveryone,
			Text: "the person reopened the team"}))
	}
	return tea.Batch(append(tells, a.teamsBringManager())...)
}

// teamsWrapUp is `Wrap up first`: the seam's wrap-up door appends the one
// Traffic entry the manager's session reads as the request
// (teamstore.WrapUpRequest, DESIGN.md 8.8), and the session does the rest: the
// manager is woken, told to finish and commit, and brings a closing report,
// which arrives here as a card; past its time or money codeaf raises the
// report itself, marked incomplete. Over a connection whose engine has no
// such door the page says so and offers the other two.
func (a *app) teamsWrapUp(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Manager == "" {
		return nil
	}
	door := a.teamsSeam().WrapUp
	if door == nil {
		a.tp.msg = teamsNoWrapUpWord
		a.touch()
		return nil
	}
	a.tp.msg = "asked " + a.teamManagerMark() + " " + t.Name + "'s manager to wrap up · its closing report will be a card here"
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(id, "")
		return func(bool) tea.Cmd {
			if err != nil {
				a.tp.msg = "the wrap-up was not asked for: " + err.Error()
				a.touch()
			}
			return nil
		}
	})
}

// teamsNoWrapUpWord is what the close card and the page say where the seam has
// no wrap-up door: an engine older than the doors, over --host.
const teamsNoWrapUpWord = "Wrap up first is not offered over this connection: Close now, or Cancel"

// teamsAcceptReport is the person's `Close` on a closing report: the interface
// stops the team's turns and closes its tabs now, and the store closes the
// team with the packet as its report (teamstore.AcceptClosing), after the
// decision is written. The window then reads the teams again, because the
// file moved under it.
func (a *app) teamsAcceptReport(p teamstore.Packet, decision string) tea.Cmd {
	t, ok := a.teamByID(p.Origin)
	if !ok || t.Closed() {
		return nil
	}
	seam := a.teamsSeam()
	if seam.AcceptClosing == nil {
		// No door to close it on its report: close it here, the report named.
		cmd := a.teamsCloseNow(p.Origin, p.ID)
		return tea.Batch(cmd, a.teamsDecideOnly(seam, p.ID, decision))
	}
	shut := a.teamsStopMembers(p.Origin)
	reserved, now, id := teamReservedHues(a.pal), a.now(), p.ID
	return a.offLoop(func() func(bool) tea.Cmd {
		_, err := seam.Decide(id, teamstore.Person, decision, "")
		closed := false
		var teams []team
		var stamp string
		if err == nil {
			closed, err = seam.AcceptClosing(id)
		}
		if err == nil {
			teams, stamp, _, err = seam.ReadSince("", reserved)
		}
		return func(bool) tea.Cmd {
			a.tp.packetsStamp = ""
			if err != nil {
				a.tp.msg = "not closed: " + err.Error()
				a.touch()
				return a.teamsRead(false)
			}
			a.teamAdopt(teamsClone(teams))
			a.traffic.stamp = stamp
			var cmd tea.Cmd
			if closed {
				cmd = a.teamsAfterClose(t, shut, now, false)
			}
			return tea.Batch(cmd, a.teamsRead(false))
		}
	})
}

// teamsDecideOnly writes the person's decision on packet id and reads the
// packets again.
func (a *app) teamsDecideOnly(seam TeamsSeam, id, decision string) tea.Cmd {
	return a.offLoop(func() func(bool) tea.Cmd {
		_, err := seam.Decide(id, teamstore.Person, decision, "")
		return func(bool) tea.Cmd {
			if err != nil {
				a.tp.msg = "not decided: " + err.Error()
				a.touch()
			}
			a.tp.packetsStamp = ""
			return a.teamsRead(false)
		}
	})
}

// teamsTell appends one entry to team id's Traffic through the seam, off the
// ordered door line (it is the person's gesture). A seam with no Traffic door
// tells nothing, and the caller has already said so where it matters.
func (a *app) teamsTell(id string, e teamstore.Entry) tea.Cmd {
	door := a.teamsSeam().Append
	if door == nil {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(id, e)
		return func(bool) tea.Cmd {
			if err != nil {
				a.tp.msg = "the team's Traffic did not take it: " + err.Error()
				a.touch()
			}
			return nil
		}
	})
}

// teamsDelete forgets closed team id through the seam, off the loop, and then
// reads the teams again, because the file moved under the window.
func (a *app) teamsDelete(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	if !t.Closed() {
		a.tp.msg = "only a closed team can be deleted"
		a.touch()
		return nil
	}
	seam := a.teamsSeam()
	if seam.Delete == nil {
		a.tp.msg = "delete is not available over this connection"
		a.touch()
		return nil
	}
	reserved, name := teamReservedHues(a.pal), t.Name
	return a.offLoop(func() func(bool) tea.Cmd {
		_, err := seam.Delete(id)
		var teams []team
		var stamp string
		if err == nil {
			teams, stamp, _, err = seam.ReadSince("", reserved)
		}
		return func(bool) tea.Cmd {
			if err != nil {
				a.tp.msg = "not deleted: " + err.Error()
				a.touch()
				return nil
			}
			a.teamAdopt(teamsClone(teams))
			a.traffic.stamp = stamp
			a.tp.msg = name + " is forgotten; its conversations stay in your history"
			if a.tp.sel == id {
				a.tp.sel = ""
				a.teamsSettle()
			}
			a.tp.top = teamsTopCache{}
			a.touch()
			return nil
		}
	})
}

// teamsCloseWords is the close card's sentence about what is running.
func teamsCloseWords(names []string) string {
	switch len(names) {
	case 0:
		return "nothing is running"
	case 1:
		return names[0] + " is still working"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " are still working"
}
