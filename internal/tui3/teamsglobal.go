package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The global role has its own card so it cannot be mistaken for a team's
// manager or for the All teams navigation door. All reads use the current frame.
func (a *app) teamsGlobalManagerCard(d *teamsDraw, width, y int) []string {
	inner := width - 4
	var lines []wallCardLine
	add := func(word string) {
		for _, part := range wrap(word, inner) {
			lines = append(lines, wallCardLine{s: part})
		}
	}
	control := func(word string, act teamsAct, id, key, hint string) {
		lines = append(lines, wallCardLine{s: d.row(a.pal.muted(word), inner,
			teamsTarget{act: act, id: id, arg: key, x0: 2, y: y + 1 + len(lines), hint: hint, pane: true}, false)})
	}
	add(a.pal.dim("Optional " + a.teamsDot() + " coordinates the managers of top-level teams"))
	var actions []struct {
		word, id, key, hint string
		act                 teamsAct
	}
	action := func(word string, act teamsAct, id, key, hint string) {
		actions = append(actions, struct {
			word, id, key, hint string
			act                 teamsAct
		}{word, id, key, hint, act})
	}
	root, _ := a.teamsRoot()
	m, hasManager := root.Member(root.Manager)
	hasManager = hasManager && !a.teamsManagerMissing(root)
	if hasManager {
		alias, title, state := m.Word, m.Word, "idle"
		for _, r := range a.teamsCrew(root) {
			if r.manager {
				alias, title, state = r.name(), r.title, r.word
			}
		}
		preview := a.tp.previews[m.Key]
		control(alias, teamsActMember, root.ID, m.Key, "Open the global manager conversation")
		if title != alias {
			add(a.pal.dim(title))
		}
		add(a.teamsCrewInk(state)(state))
		if words := a.teamsSpendWords(root); words != "" {
			add(a.pal.dim(words))
		}
		words := preview.text
		switch {
		case preview.unavailable || a.hosted():
			words = "Preview unavailable"
		case words == "":
			words = "Updates appear here"
		}
		parts := wrap(words, inner)
		for _, part := range parts[:min(len(parts), 4)] {
			control(part, teamsActMember, root.ID, m.Key, "Open the global manager to read or respond")
		}
		var reports []string
		tree := a.teamTree()
		for _, t := range a.teamsOverviewRoots() {
			home, reportsHere := tree.Home(t.Manager)
			if !t.Closed() && t.Manager != "" && reportsHere && home.Team == root.ID && home.Manager == root.Manager {
				reports = append(reports, t.Name)
			}
		}
		if len(reports) > 0 {
			add(a.pal.dim("Reports: " + strings.Join(reports, " "+a.teamsDot()+" ")))
		} else {
			add(a.pal.dim("Team managers report here as they are assigned"))
		}
		action("x", teamsActDeleteGlobalManager, root.ID, m.Key, "Permanently delete the global manager conversation; every team and its manager remains")
	}
	if !hasManager {
		action(teamGlobalManagerSlotWord, teamsActRootManager, "", "", "Create the optional global manager conversation")
	}
	if hasManager {
		action("+ Add member", teamsActAddMember, root.ID, "", "Add a conversation to the global manager team")
		action("Settings", teamsActSettings, root.ID, "", "Global manager settings and spending controls")
	}
	line, x := "", 0
	for _, button := range actions {
		w := min(ansi.StringWidth(button.word)+2, inner)
		if x+w > inner {
			lines = append(lines, wallCardLine{s: line})
			line, x = "", 0
		}
		line += d.row(a.pal.muted(" "+button.word+" "), w, teamsTarget{act: button.act, id: button.id, arg: button.key, x0: 2 + x, y: y + 1 + len(lines), hint: button.hint, pane: true}, false)
		x += w
	}
	if line != "" {
		lines = append(lines, wallCardLine{s: line})
	}
	return wallCardBuild(a.pal, fit("Global manager", width-5), lines, 0, y, width, 1, 0).rows
}
