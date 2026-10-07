package tui3

import (
	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// Disbanding releases coordination recursively without stopping conversations.
// Deletion removes the team's own history, never the conversations it held.
// Closed records remain readable; the surface does not offer reopen or close undo.

// teamsCloseAsk confirms disbanding before coordination ends.
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
	return a.teamSheetOpen(id, teamSheetClose)
}

// teamsCloseNow disbands the reviewed scope and keeps conversations working.
func (a *app) teamsCloseNow(id, report string, expected ...[]string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() || t.Root {
		return nil
	}
	now := a.now()
	if err := a.teamEdit(func(f *teamstore.File) error {
		if len(expected) > 0 {
			if err := f.CheckAffected(id, expected[0]); err != nil {
				return err
			}
		}
		return f.Disband(id, now, report)
	}); err != nil {
		a.tp.msg = "not closed: " + err.Error()
		a.touch()
		return nil
	}
	cmd := a.teamsAfterClose(t, true)
	return cmd
}

// teamsAfterClose releases the overlay and shows the preserved history. A
// local edit announces completion only after its own store write succeeds.
func (a *app) teamsAfterClose(t team, tell bool) tea.Cmd {
	a.tp.msg = ""
	if tell {
		a.tp.disbandName, a.tp.disbandSaid = t.Name, a.teamWriteWatch(nil)
	} else {
		a.teamsDisbandSaid(t.Name, "")
	}
	if active, ok := a.teamByID(a.teamViews.id); !ok || active.Closed() {
		a.teamViewSet("")
	}
	a.tp.closedOpen, a.tp.sel = true, t.ID
	a.touch()
	return nil
}

// teamsDisbandSaid puts the store's result where the person is standing.
func (a *app) teamsDisbandSaid(name, why string) {
	word := name + " is disbanded; conversations and current work continue"
	if why != "" {
		word = teamNotSaved("the disbanding of "+name, why)
	}
	a.tp.msg = word
	if !a.at(pageTeams) {
		a.note(word)
	}
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

// teamsAcceptReport accepts the closing report after writing the decision.
// Coordination ends while conversations and their current work survive. The
// window reads the retained history again after the store changes.
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
	reserved, id := teamReservedHues(a.pal), p.ID
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
				cmd = a.teamsAfterClose(t, false)
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
func (a *app) teamsDelete(id string, expected ...[]string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	seam := a.teamsSeam()
	if seam.Delete == nil {
		a.tp.msg = "delete is not available over this connection"
		a.touch()
		return nil
	}
	reserved, name := teamReservedHues(a.pal), t.Name
	ids := a.teamsAffectedIDs(id)
	if len(expected) > 0 {
		ids = expected[0]
	}
	if seam.DeleteChecked == nil {
		a.tp.msg = "This engine does not offer permanent team deletion"
		a.touch()
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		_, err := seam.DeleteChecked(id, ids)
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
			if active, ok := a.teamByID(a.teamViews.id); !ok || active.Closed() {
				a.teamViewSet("")
			}
			a.traffic.stamp = stamp
			a.tp.msg = name + " is forgotten; its conversations stay in your history"
			if a.tp.sel == id {
				a.tp.sel = ""
				a.teamsSettle()
			}
			a.touch()
			return nil
		}
	})
}
