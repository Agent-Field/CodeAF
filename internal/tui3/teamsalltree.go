package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// The overview keeps at most two boxes inside a top-level card. A narrower
// child uses rows before its metadata becomes unreadable; neither threshold
// changes the team's configured depth limit or its authority.
const (
	teamsOverviewBoxLevels = 2
	teamsOverviewBoxMin    = 36
)

// Branch guides connect child headings to their parent while leaving each
// team's own colour intact. Children stack in one column at every width.
func (a *app) teamsOverviewBranches(d *teamsDraw, children []team, width, x, y int, ancestors map[string]bool) []string {
	if len(ancestors) > teamsOverviewBoxLevels || width-2 < teamsOverviewBoxMin {
		return a.teamsOverviewTree(d, children, width, x, y, nil, ancestors)
	}
	var out []string
	for i, child := range children {
		last := i == len(children)-1
		if len(out) > 0 {
			out = append(out, a.pal.dim(a.icon(tokens.GTreeVert)))
		}
		rows := a.teamsOverviewCard(d, child, width-2, x+2, y+len(out), true, ancestors)
		for row, text := range rows {
			guide := " "
			if !last || row == 0 {
				guide = a.icon(tokens.GTreeVert)
			}
			if row == 1 {
				guide = a.icon(tokens.GTreeBranch)
				if last {
					guide = a.icon(tokens.GTreeLast)
				}
			}
			out = append(out, a.pal.dim(guide)+" "+text)
		}
	}
	return out
}

// A compact tree keeps every descendant reachable. Indentation stops taking
// columns once it would crowd the name; the target hint retains full ancestry.
// Folds affect only this overview, never membership or the sidebar's tree.
func (a *app) teamsOverviewTree(d *teamsDraw, children []team, width, x, y int, continued []bool, ancestors map[string]bool) []string {
	var out []string
	for i, child := range children {
		if ancestors[child.ID] {
			continue
		}
		last := i == len(children)-1
		guides := continued
		budget := max((width-24)/2, 0)
		if len(guides) > budget {
			guides = guides[len(guides)-budget:]
		}
		var prefix strings.Builder
		for _, on := range guides {
			if on {
				prefix.WriteString(a.icon(tokens.GTreeVert))
			} else {
				prefix.WriteByte(' ')
			}
			prefix.WriteByte(' ')
		}
		branch := tokens.GTreeBranch
		if last {
			branch = tokens.GTreeLast
		}
		prefix.WriteString(a.icon(branch))
		prefix.WriteByte(' ')
		lead := prefix.String()
		leadW := ansi.StringWidth(lead)
		grandchildren := a.teamsOverviewChildren(child.ID)
		folded := a.tp.foldedSubteams[child.ID]
		fold := "   "
		if len(grandchildren) > 0 {
			mark, verb := tokens.GExpanded, "Collapse"
			if folded {
				mark, verb = tokens.GCollapsed, "Expand"
			}
			fold, _ = d.button(a.icon(mark), teamsTarget{act: teamsActSubteamsFold, id: child.ID, x0: x + leadW, y: y + len(out), pane: true,
				hint: verb + " subteams of " + a.teamsAncestryName(child)}, a.pal.dim)
		}
		foldW := ansi.StringWidth(fold)
		dot := a.tabTeamDot(child)
		if a.tp.picked[child.ID] {
			dot = a.pal.accent(wallGlyphsFor(a.pal.ascii).marked)
		}
		word := dot + " " + a.pal.ink(child.Name)
		if child.Closed() {
			word += a.pal.dim(" " + a.teamsDot() + " closed")
		} else if len(grandchildren) > 0 {
			word += a.pal.dim(" " + a.teamsDot() + " " + itoa(len(grandchildren)) + " subteams")
		}
		target := teamsTarget{act: teamsActSelect, id: child.ID, arg: "overview", x0: x + leadW + foldW, y: y + len(out), pane: true,
			hint: "View " + a.teamsAncestryName(child) + "; m moves this team"}
		if child.Closed() {
			target.hint = "Read " + a.teamsAncestryName(child) + " history"
		}
		out = append(out, a.pal.dim(lead)+fold+d.row(word, max(width-leadW-foldW, 1), target, a.teamDropLit(child.ID)))
		if !folded && len(grandchildren) > 0 {
			ancestors[child.ID] = true
			next := append(append([]bool(nil), continued...), !last)
			out = append(out, a.teamsOverviewTree(d, grandchildren, width, x, y+len(out), next, ancestors)...)
			delete(ancestors, child.ID)
		}
	}
	return out
}
