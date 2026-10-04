package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// All teams is an overview of the hierarchy, independent of whether the
// optional global manager exists. Its cards never resume a conversation.
func (a *app) teamsAllSelected() bool {
	if a.tp.sel == teamsAllRow {
		return true
	}
	t, ok := a.teamsSelected()
	return ok && t.Root
}

// Children are read in store order so activity cannot rearrange the overview.
func (a *app) teamsOverviewChildren(parent string) []team {
	var out []team
	for _, t := range a.wall.teams {
		if !t.Root && (!t.Closed() || a.tp.closedOpen) && t.Parent == parent {
			out = append(out, t)
		}
	}
	return out
}

func (a *app) teamsOverviewRoots() []team {
	root, hasRoot := a.teamsRoot()
	var out []team
	for _, t := range a.wall.teams {
		if !t.Root && (!t.Closed() || a.tp.closedOpen) && (t.Parent == "" || hasRoot && t.Parent == root.ID) {
			out = append(out, t)
		}
	}
	return out
}

// A closed record keeps its full ancestry, including closed ancestors. The
// root is a global role rather than an additional level in a team's name.
func (a *app) teamsAncestryName(t team) string {
	var names []string
	for _, p := range a.teamAncestors(t.ID) {
		if !p.Root {
			names = append(names, p.Name)
		}
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	names = append(names, t.Name)
	return strings.Join(names, " "+a.linearMark("›", ">")+" ")
}

// Activity and unread are counted independently, since a running conversation
// can also have unread messages. Counts describe this team's own memberships.
func (a *app) teamsOverviewState(t team) string {
	working, asking, failed, unread := 0, 0, 0, 0
	for _, r := range a.teamsCrew(t) {
		switch r.word {
		case "working":
			working++
		case "asking":
			asking++
		case "failed":
			failed++
		}
		if a.unreadChats[r.key] {
			unread++
		}
	}
	var parts []string
	if working > 0 {
		parts = append(parts, a.pal.dim(itoa(working)+" running"))
	}
	if asking > 0 {
		parts = append(parts, a.pal.ask(itoa(asking)+" waiting on you"))
	}
	if failed > 0 {
		parts = append(parts, a.pal.bad(itoa(failed)+" failed"))
	}
	if unread > 0 {
		parts = append(parts, a.pal.muted(itoa(unread)+" unread"))
	}
	if len(parts) == 0 {
		parts = append(parts, a.pal.dim("idle"))
	}
	return strings.Join(parts, " "+a.teamsDot()+" ")
}

func (a *app) teamsAllHeader(d *teamsDraw, width, y int) []string {
	pal := a.pal
	word := a.teamsSpark() + " Organize"
	buttonW := ansi.StringWidth(word) + 2
	x := max(width-buttonW-1, 1)
	button, _ := d.button(word, teamsTarget{act: teamsActOrganize, x0: x, y: y, hint: "Suggest teams; nothing changes until you apply", pane: true}, pal.muted)
	left := fit(" "+pal.bold(pal.ink(teamstore.RootName)), max(x-1, 1))
	out := []string{teamsPad(left, x) + button}
	// Undo belongs to the same surface as Apply, including its store refusal.
	o := a.wall.org
	if o.said.said() && teamSaidWithin(o.doneAt, a.now(), wallOrganizedFor) {
		word := pal.dim("Organized")
		if o.said.why != "" {
			word = pal.warn(teamNotSaved("organization", o.said.why))
		}
		out = append(out, " "+fit(word, width-1))
		if len(o.undoMade)+len(o.undoJoins) > 0 {
			b, _ := d.button("Undo", teamsTarget{act: teamsActOrganizeUndo, x0: 1, y: y + len(out), hint: "Undo the last organization", pane: true}, pal.muted)
			out = append(out, " "+b)
		}
	}
	return out
}

// Cards compose recursively. Narrow panes use one column; each smaller child
// remains physically inside its parent's frame, including deeper descendants.
func (a *app) teamsAllCards(d *teamsDraw, width, y int) []string {
	if width < 12 {
		return nil
	}
	out := a.teamsGlobalManagerCard(d, width, y)
	out = append(out, "")
	return append(out, a.teamsOverviewGrid(d, a.teamsOverviewRoots(), width, 0, y+len(out), false, map[string]bool{})...)
}

func (a *app) teamsOverviewGrid(d *teamsDraw, teams []team, width, x, y int, compact bool, ancestors map[string]bool) []string {
	columns := 1
	if width >= 100 || compact && width >= 60 {
		columns = 2
	}
	w := (width - (columns-1)*2) / columns
	var out []string
	for first := 0; first < len(teams); first += columns {
		cards := make([][]string, 0, columns)
		height := 0
		for col := 0; col < columns && first+col < len(teams); col++ {
			card := a.teamsOverviewCard(d, teams[first+col], w, x+col*(w+2), y+len(out), compact, ancestors)
			cards = append(cards, card)
			height = max(height, len(card))
		}
		for row := 0; row < height; row++ {
			var parts []string
			for _, card := range cards {
				line := ""
				if row < len(card) {
					line = card[row]
				}
				parts = append(parts, teamsPad(line, w))
			}
			out = append(out, strings.Join(parts, "  "))
		}
		out = append(out, "")
	}
	return out
}

func (a *app) teamsOverviewCard(d *teamsDraw, t team, width, x, y int, compact bool, ancestors map[string]bool) []string {
	if ancestors[t.ID] || width < 12 {
		return nil
	}
	ancestors[t.ID] = true
	defer delete(ancestors, t.ID)
	inner := width - 4
	selectTarget := teamsTarget{act: teamsActSelect, id: t.ID, arg: "overview", x0: x + 2, y: y + 1, hint: "View " + a.teamsAncestryName(t) + "; m moves this team", pane: true}
	if t.Closed() {
		selectTarget.hint = "Read " + a.teamsAncestryName(t) + " history"
	}
	var lines []wallCardLine
	add := func(word string) {
		target := selectTarget
		target.y = y + 1 + len(lines)
		lines = append(lines, wallCardLine{s: d.row(word, inner, target, a.teamDropLit(t.ID))})
	}
	dot := a.tabTeamDot(t)
	if a.tp.picked[t.ID] {
		dot = a.pal.accent(wallGlyphsFor(a.pal.ascii).marked)
	}
	add(dot + " " + a.pal.bold(a.pal.ink(t.Name)))
	count := itoa(len(t.Members)) + " conversations"
	if len(t.Members) == 1 {
		count = "1 conversation"
	}
	add(a.pal.dim(count))
	if t.Closed() {
		add(a.pal.dim("closed " + a.teamsDot() + " read-only history"))
	} else {
		for _, line := range wrap(a.teamsOverviewState(t), inner) {
			add(line)
		}
	}
	if !compact {
		if !t.Closed() {
			add(a.pal.dim(a.teamsSpendWords(t)))
		}
		add("")
	}
	if m, ok := t.Member(t.Manager); ok {
		name := m.Word
		if m.Handle != "" {
			name = "@" + m.Handle
		}
		title := m.Word
		for _, r := range a.teamsCrew(t) {
			if r.manager {
				title = r.title
			}
		}
		word := a.pal.dim("Manager ") + a.teamsConversationLabel(name, m.Key, m.File, inner-8)
		if !t.Closed() && !a.tp.previews[m.Key].missing {
			target := teamsTarget{act: teamsActMember, id: t.ID, arg: m.Key, x0: x + 2, y: y + 1 + len(lines), hint: "Open " + name + " of " + t.Name, pane: true}
			lines = append(lines, wallCardLine{s: d.row(word, inner, target, false)})
		} else {
			add(word)
		}
		if !compact && title != name {
			add(a.pal.dim(title))
		}
		if !compact && !t.Closed() {
			preview := a.tp.previews[m.Key]
			words := preview.text
			if words == "" {
				words = "Updates appear here"
			}
			if preview.unavailable || a.hosted() {
				words = "Preview unavailable"
			}
			if preview.missing {
				words = "Conversation unavailable"
			}
			parts := wrap(words, inner)
			for i := 0; i < 2; i++ {
				line := ""
				if i < len(parts) {
					line = parts[i]
				}
				add(a.pal.ink(line))
			}
		}
	} else if !t.Closed() {
		add(a.pal.dim("Choose a manager in the team overview"))
	}
	children := a.teamsOverviewChildren(t.ID)
	if len(children) > 0 {
		add("")
		add(a.pal.dim("Subteams"))
		// Child targets are registered before their surrounding padding, so a
		// click inside a child always chooses that child rather than its parent.
		childRows := a.teamsOverviewGrid(d, children, inner, x+2, y+1+len(lines), true, ancestors)
		for _, row := range childRows {
			lines = append(lines, wallCardLine{s: row})
		}
	}
	card := wallCardBuild(a.pal, "", lines, x, y, width, 1, 0)
	// Background and frame clicks use the team's door, after every narrower
	// alias and descendant target. The border itself never changes emphasis.
	for row := range card.rows {
		target := selectTarget
		target.x0, target.x1, target.y = x, x+width, y+row
		d.targets = append(d.targets, target)
	}
	return card.rows
}
