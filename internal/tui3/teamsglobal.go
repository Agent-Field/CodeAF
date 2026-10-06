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
	var manager teamsCrewRow
	if hasManager {
		alias, title, state := m.Word, m.Word, "idle"
		for _, r := range a.teamsCrew(root) {
			if r.manager {
				alias, title, state = r.name(), r.title, r.word
			}
		}
		var reports []string
		tree := a.teamTree()
		for _, t := range a.teamsOverviewRoots() {
			home, reportsHere := tree.Home(t.Manager)
			if !t.Closed() && t.Manager != "" && reportsHere && home.Team == root.ID && home.Manager == root.Manager {
				reports = append(reports, t.Name)
			}
		}
		report := "Team managers report here as they are assigned"
		if len(reports) > 0 {
			report = "Reports: " + strings.Join(reports, " "+a.teamsDot()+" ")
		}
		manager = teamsCrewRow{key: m.Key, file: m.File, handle: m.Handle, title: title, word: state, manager: true, asking: state == "asking"}
		preview := a.tp.previews[m.Key]
		// Preserve the existing populated card's height while reclaiming its
		// descriptive rows for the conversation. The absent role stays compact.
		bodyRows := 1 + len(wrap("Optional "+a.teamsDot()+" coordinates the managers of top-level teams", inner)) + len(wrap(state, inner)) + len(wrap(report, inner))
		if title != alias {
			bodyRows += len(wrap(title, inner))
		}
		if spend := a.teamsSpendWords(root); spend != "" {
			bodyRows += len(wrap(spend, inner))
		}
		oldPreview := preview.text
		if preview.unavailable || a.hosted() {
			oldPreview = "Preview unavailable"
		} else if oldPreview == "" {
			oldPreview = "Updates appear here"
		}
		bodyRows += min(len(wrap(oldPreview, inner)), 4)
		words := a.teamsCardMetadata(manager, inner-3)
		words = append(words, a.teamsConversationPreview(m.Key, inner, bodyRows-len(words)-1)...)
		for len(words) < bodyRows-1 {
			words = append(words, "")
		}
		if spend := a.teamsSpendWords(root); spend != "" {
			report = spend + " " + a.teamsDot() + " " + report
		}
		words = append(words, a.pal.dim(ansi.Truncate(report, inner, "...")))
		for row, word := range words {
			cols := inner
			if row == 0 {
				cols -= 3
			}
			line := d.row(word, cols, teamsTarget{act: teamsActMember, id: root.ID, arg: m.Key, x0: 2, y: y + 1 + row, hint: "Open the global manager to read or respond", pane: true}, false)
			if row == 0 {
				line += d.row(a.pal.muted(" x "), 3, teamsTarget{act: teamsActDeleteGlobalManager, id: root.ID, arg: m.Key, x0: width - 5, y: y + 1, hint: "Permanently delete the global manager conversation; every team and its manager remains", pane: true}, false)
			}
			lines = append(lines, wallCardLine{s: line})
		}

	}
	if !hasManager {
		add(a.pal.dim("Optional " + a.teamsDot() + " coordinates the managers of top-level teams"))
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
	return a.teamsConversationCard(d, root, manager, fit("Global manager", width-5), lines, 0, y, width)
}
