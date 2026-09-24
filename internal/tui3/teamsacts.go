package tui3

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── WHAT THE TEAMS PAGE'S TARGETS DO ────────────────────────────────────────
//
// One switch for the pointer and the keyboard both ([app.teamsDo]), so a press
// and an `enter` on the same target cannot come to mean two things. Every act
// that reaches the store is an edit through [app.teamEdit] (written off the
// loop) or a seam door asked off the loop; nothing here waits on a disk or a
// wire.

// teamsDo is one target, pressed.
func (a *app) teamsDo(t teamsTarget) tea.Cmd {
	a.tp.msg = ""
	a.tp.cur = t.ref()
	switch t.act {
	case teamsActSelect:
		return a.teamsSelect(t.id)
	case teamsActClosedFold:
		a.tp.closedOpen = !a.tp.closedOpen
		if !a.tp.closedOpen {
			if sel, ok := a.teamsSelected(); ok && sel.Closed() {
				a.tp.sel = ""
				a.teamsSettle()
			}
		}
		a.touch()
		return nil
	case teamsActNewTeam:
		return a.teamMenuNewTeam()
	case teamsActOrganize:
		open := a.openWall()
		a.wallSetTeam("")
		return tea.Batch(open, a.wallOrganizeOpen())
	case teamsActWall:
		open := a.openWall()
		a.wallSetTeam(t.id)
		return open
	case teamsActManager:
		return a.teamsManagerStart(t.id)
	case teamsActRootManager:
		return a.teamsRootManagerStart()
	case teamsActSettings:
		return a.teamSheetOpen(t.id, teamSheetSettings)
	case teamsActClose:
		return a.teamsCloseAsk(t.id)
	case teamsActReopen:
		return a.teamsReopen(t.id, false)
	case teamsActReopenParent:
		return a.teamsReopen(t.id, true)
	case teamsActDelete:
		return a.teamSheetOpen(t.id, teamSheetDelete)
	case teamsActMember:
		return a.teamsMemberGo(t.id, t.arg)
	case teamsActOption:
		if t.opt == "" {
			a.tp.expand = t.arg
			a.tp.top = teamsTopCache{}
			a.touch()
			return nil
		}
		return a.teamsDecide(t.arg, t.opt, "")
	case teamsActOwnAnswer:
		a.tp.answering = t.arg
		a.tp.answer.reset()
		a.tp.top = teamsTopCache{}
		a.touch()
		return nil
	case teamsActPrompt:
		return a.teamsPrompt(t.arg, t.opt)
	}
	return nil
}

// teamsSelect puts the pane on team id and, when it has a manager, brings that
// conversation in front, where the pane draws it. The keyboard goes back to the
// composer: choosing a team is choosing whom to talk to.
func (a *app) teamsSelect(id string) tea.Cmd {
	a.tp.sel = id
	a.tp.expand, a.tp.answering = "", ""
	a.tp.top = teamsTopCache{}
	if t, ok := a.teamByID(id); ok && t.Manager != "" && !t.Closed() {
		a.tp.focus = false
	}
	a.touch()
	return tea.Batch(a.teamsBringManager(), a.teamsRead(false))
}

// teamsBringManager brings the selected team's manager in front when it is not
// there yet. It is the one move of the front this page makes, and only ever
// for the team the person chose: the conversation that was in front stays
// open behind, one `tab` away.
func (a *app) teamsBringManager() tea.Cmd {
	t, ok := a.teamsSelected()
	if !ok || t.Closed() || t.Manager == "" || a.teamsOff() || t.Manager == a.frontTabKey() {
		a.tp.opening = ""
		return nil
	}
	a.tp.opening = t.Manager
	return a.trafficGo(t.Manager)
}

// teamsMemberGo is a press on a member: one this window holds is opened, and
// one it does not is resumed BEHIND, as its own tab, without moving the
// person's focus (ruling c-b).
func (a *app) teamsMemberGo(id, key string) tea.Cmd {
	if a.trafficHeld(key) {
		if key == a.frontTabKey() {
			a.leavePlace()
			return nil
		}
		cmd := a.trafficGo(key)
		a.leavePlace()
		return cmd
	}
	t, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	m, ok := t.Member(key)
	if !ok || strings.TrimSpace(m.File) == "" {
		return nil
	}
	return a.teamsResumeBehind(m)
}

// teamsResumeBehind opens one member's conversation behind the one in front,
// off the loop, and holds it as a tab of its own.
func (a *app) teamsResumeBehind(m teamMember) tea.Cmd {
	name := m.Word
	if m.Handle != "" {
		name = "@" + m.Handle
	}
	switch {
	case a.shared:
		a.tp.msg = name + " could not open beside this one: " + oneConversationWord
		a.touch()
		return nil
	case !a.canOpen():
		a.tp.msg = name + ": " + resumeUnavailableWord
		a.touch()
		return nil
	}
	open, resume, file, where := a.open, a.resume, m.File, m.Where
	a.tp.msg = "opening " + name + " behind" + a.linearMark("…", "...")
	a.touch()
	return a.besideLine(func() func(bool) tea.Cmd {
		var conv Conversation
		var err error
		if open != nil {
			conv, err = open(where, file)
		} else {
			var agent Agent
			agent, err = resume(file)
			conv = Conversation{Agent: agent, Workspace: where, SessionFile: file}
		}
		return func(bool) tea.Cmd {
			if err != nil || conv.Agent == nil {
				why := "the conversation did not open"
				if err != nil {
					why = err.Error()
				}
				a.tp.msg = name + ": " + why
				a.touch()
				return nil
			}
			key := a.convKey(conv.SessionFile)
			cmd := a.stow(conv, nil)
			a.trafficBehindTop(key)
			a.chatTabBar = tabBar{}
			a.tp.msg = name + " is open behind, in its own tab"
			a.tp.top = teamsTopCache{}
			a.touch()
			return cmd
		}
	})
}

// teamsManagerStart is `+ Manager` on team id: a new conversation in the
// team's folder, made its manager, and in front for the person's first words
// to it, which is where the pane draws it.
func (a *app) teamsManagerStart(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Manager != "" || t.Closed() {
		return nil
	}
	if a.teamsOff() {
		a.tp.msg = teamHostedWord
		a.touch()
		return nil
	}
	return a.teamsStartManager(a.teamWhere(t), func(tab chatTab) {
		if err := a.teamMakeManager(id, tab); err != nil {
			a.note("the manager is set for this window, but " + err.Error())
		}
	})
}

// teamsRootManagerStart is `+ Manager` on the `All teams` row: the optional
// global manager. It makes the root team (every top-level team moves under it,
// internal/teams' root.go) and its manager in one edit.
func (a *app) teamsRootManagerStart() tea.Cmd {
	if a.teamsOff() {
		a.tp.msg = teamHostedWord
		a.touch()
		return nil
	}
	if root, ok := a.teamsRoot(); ok {
		if root.Manager != "" {
			return a.teamsSelect(root.ID)
		}
		return a.teamsManagerStart(root.ID)
	}
	// The id is minted here, once: the edit is made twice (teams.go's
	// [app.teamEdit]) and must name the same root both times.
	rootID, now := newTeamID(), a.now()
	return a.teamsStartManager(a.workspace, func(tab chatTab) {
		m := teamFromTabs("", []chatTab{tab}, now).Members
		err := a.teamEdit(func(f *teamstore.File) error {
			id := rootID
			if r, ok := f.Root(); ok {
				id = r.ID
			} else {
				f.Teams = append([]teamstore.Team{{ID: rootID, Name: teamstore.RootName, Made: now, Root: true}}, f.Teams...)
				for i := range f.Teams {
					if f.Teams[i].ID != rootID && f.Teams[i].Parent == "" {
						f.Teams[i].Parent = rootID
					}
				}
			}
			if len(m) > 0 {
				if err := f.AddMember(id, m[0]); err != nil {
					return err
				}
			}
			return f.SetManager(id, tab.key)
		})
		if err != nil {
			a.note("the manager is set for this window, but " + err.Error())
		}
		a.tp.sel = rootID
		if r, ok := a.teamsRoot(); ok {
			a.tp.sel = r.ID
		}
	})
}

// teamsStartManager opens a fresh conversation in where, in front, and hands
// it to made once it is. The conversation is opened on the door line, off the
// loop, because opening one is a call to the engine.
func (a *app) teamsStartManager(where string, made func(chatTab)) tea.Cmd {
	take := func() {
		made(chatTab{key: a.convKey(a.file), file: a.file, where: a.workspace})
		a.tp.top = teamsTopCache{}
		a.touch()
	}
	if a.start == nil || a.shared {
		cmd, refusal := a.teamsStartIn(where)
		if refusal != "" {
			a.tp.msg = refusal
			a.touch()
			return nil
		}
		take()
		return cmd
	}
	if !a.canStart() {
		a.tp.msg = newUnavailableWord
		a.touch()
		return nil
	}
	start := a.start
	return a.besideLine(func() func(bool) tea.Cmd {
		conv, err := start(where)
		return func(bool) tea.Cmd {
			if err != nil || conv.Agent == nil {
				why := newUnavailableWord
				if err != nil {
					why = err.Error()
				}
				a.tp.msg = why
				a.touch()
				return nil
			}
			cmd := a.takeBeside(conv)
			take()
			return cmd
		}
	})
}

// teamsStartIn is [app.teamStartIn] for a folder rather than a team.
func (a *app) teamsStartIn(where string) (tea.Cmd, string) {
	if a.start != nil && strings.TrimSpace(where) != "" && where != a.workspace {
		return a.startBeside(where)
	}
	if a.start != nil {
		return a.startBeside(a.workspace)
	}
	cmd, ok := a.renew()
	if !ok {
		return nil, newUnavailableWord
	}
	return cmd, ""
}

// ── DECIDING ────────────────────────────────────────────────────────────────

// teamsCapAmount reads the figure a cap packet's `raise` option names, `Raise
// to $10`, false when it names none.
var teamsCapAmount = regexp.MustCompile(`\$\s*([0-9]+(?:\.[0-9]+)?)`)

// teamsDecide decides packet id with option opt, or with the person's own
// words, through the seam, off the loop. A closing report's `Close` closes the
// team, `Close now` closes it although the wrap-up did not finish, and a cap's
// `Raise to $10` sets the cap on the team it was raised for: money is the
// person's, so the interface writes it, never a manager.
func (a *app) teamsDecide(id, opt, words string) tea.Cmd {
	seam := a.teamsSeam()
	if !seam.delegation() {
		a.tp.msg = teamsHostedWord
		a.touch()
		return nil
	}
	var packet teamstore.Packet
	found := false
	for _, p := range a.tp.packets {
		if p.ID == id {
			packet, found = p, true
		}
	}
	if !found {
		return nil
	}
	decision := opt
	if decision == "" {
		decision = words
	}
	label := decision
	if o, ok := packet.Option(opt); ok {
		label = o.Label
	}
	a.tp.msg = "decided " + a.teamsDot() + " " + label
	a.tp.top = teamsTopCache{}
	a.touch()
	var after tea.Cmd
	switch {
	case packet.Kind == teamstore.PacketClosing && (opt == teamstore.OptionClose || opt == teamstore.OptionCloseNow):
		after = a.teamsCloseNow(packet.Origin, packet.ID)
	case packet.Kind == teamstore.PacketCap && opt == teamstore.OptionRaiseCap:
		if o, ok := packet.Option(opt); ok {
			if m := teamsCapAmount.FindStringSubmatch(o.Label); m != nil {
				if usd, err := strconv.ParseFloat(m[1], 64); err == nil {
					team := packet.Origin
					_ = a.teamEdit(func(f *teamstore.File) error {
						return f.SetSettings(team, func(s *teamstore.Settings) { s.CapUSDDay = &usd })
					})
				}
			}
		}
	}
	decide := a.offLoop(func() func(bool) tea.Cmd {
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
	return tea.Batch(after, decide)
}

// teamsPrompt answers a member's permission prompt from the page, through
// home's own door ([app.sendAnswer]): the answer is left on that
// conversation's doorstep, or given in this window's own hands when it is the
// one in front.
func (a *app) teamsPrompt(file, key string) tea.Cmd {
	row, ok := a.tp.world[filepath.Clean(file)]
	if !ok {
		return nil
	}
	question, ok := answerable(row, time.Now())
	if !ok {
		return nil
	}
	cmd, took := a.sendAnswer(row, question, key)
	if took {
		a.tp.msg = answerSentWord + question.Label(key)
		a.tp.top = teamsTopCache{}
		a.touch()
	}
	return cmd
}
