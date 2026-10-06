package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// Ten levels exercise the configured maximum, rather than just the sample's
// three levels. Older or externally written trees must remain readable too.
func teamsDeepOverviewLab(t *testing.T) *app {
	t.Helper()
	a, _, _ := teamsPlaceLabIDs(t)
	a.wall.teams = nil
	for i := 0; i < 12; i++ {
		parent := ""
		if i > 0 {
			parent = "depth-" + itoa(i-1)
		}
		a.wall.teams = append(a.wall.teams, team{ID: "depth-" + itoa(i), Name: "Level " + itoa(i+1), Parent: parent})
	}
	a.tp.sel = teamsAllRow
	return a
}

func TestTeamsAllDeepTreeKeepsEveryTeamAndBoundsIndentation(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, width := range []int{12, 24, 40, 80, 130, 240} {
			a := teamsDeepOverviewLab(t)
			a.pal.ascii = ascii
			d := &teamsDraw{a: a}
			rows := a.teamsOverviewGrid(d, a.teamsOverviewRoots(), width, 0, 0, false, map[string]bool{})
			for _, line := range rows {
				if ansi.StringWidth(line) > width {
					t.Fatalf("row exceeds width %d (ascii %v): %s", width, ascii, line)
				}
			}
			for i := 0; i < 12; i++ {
				found := false
				for _, target := range d.targets {
					if target.arg == "overview" && target.id == "depth-"+itoa(i) {
						found = true
						if target.x1 <= target.x0 || target.x1 > width || target.y >= len(rows) {
							t.Fatalf("unreachable target at width %d: %+v", width, target)
						}
					}
				}
				if !found {
					t.Fatalf("level %d vanished at width %d", i+1, width)
				}
			}
		}
	}
}

func TestTeamsAllSubteamCardsUseOneColumnAndNamedBranchGuides(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	a.wall.teams = append(a.wall.teams, team{ID: "sibling", Name: "Sibling", Parent: harbor})
	d := &teamsDraw{a: a}
	parent, _ := a.teamByID(harbor)
	rows := a.teamsOverviewCard(d, parent, 150, 0, 0, false, map[string]bool{})
	text := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"Subteams of harbor", "2", a.icon(tokens.GTreeBranch), a.icon(tokens.GTreeLast)} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	var first, second teamsTarget
	for _, target := range d.targets {
		if target.arg != "overview" {
			continue
		}
		if target.id == orbit && first.id == "" {
			first = target
		}
		if target.id == "sibling" && second.id == "" {
			second = target
		}
	}
	if first.id == "" || second.id == "" || first.x0 != second.x0 || first.y >= second.y {
		t.Fatalf("children do not stack: %+v %+v", first, second)
	}
	// Long parent names yield their space to the direct-child count.
	parent.Name = strings.Repeat("長い名前", 12)
	rows = a.teamsOverviewCard(&teamsDraw{a: a}, parent, 45, 0, 0, false, map[string]bool{})
	found := false
	for _, row := range rows {
		plain := ansi.Strip(row)
		if strings.Contains(plain, "Subteams of") {
			found = true
			if !strings.Contains(plain, a.teamsDot()+" 2") || ansi.StringWidth(row) > 45 {
				t.Fatalf("long heading lost count or overflowed: %s", row)
			}
		}
	}
	if !found {
		t.Fatal("missing long parent heading")
	}
}

func TestTeamsAllCompactTreeFoldsWithMouseAndKeyboardAndKeepsNavigation(t *testing.T) {
	a := teamsDeepOverviewLab(t)
	a.width, a.height = 150, 60
	front := a.frontTabKey()
	a.input.insert("keep this draft")
	_ = teamsFrameText(a)
	fold := teamsTargetOf(t, a, teamsActSubteamsFold, "depth-3")
	drive(t, a, tea.MouseClickMsg{X: fold.x0, Y: fold.y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: fold.x0, Y: fold.y, Button: tea.MouseLeft})
	_ = teamsFrameText(a)
	if !a.tp.foldedSubteams["depth-3"] || a.tp.sel != teamsAllRow {
		t.Fatal("fold selected the team or did not collapse")
	}
	for _, target := range a.tp.targets {
		if target.pane && target.id == "depth-4" {
			t.Fatal("collapsed child retained a target")
		}
	}
	// The same focused control expands with Enter, without changing Chats.
	drive(t, a, key("enter"))
	_ = teamsFrameText(a)
	if a.tp.foldedSubteams["depth-3"] || a.frontTabKey() != front || a.input.String() != "keep this draft" {
		t.Fatal("keyboard expand changed Chats or failed")
	}
	deep := teamsTargetOf(t, a, teamsActSelect, "depth-11")
	// Prefer the pane's compact row over the independent sidebar target.
	for _, target := range a.tp.targets {
		if target.id == deep.id && target.pane && target.arg == "overview" {
			deep = target
			break
		}
	}
	drive(t, a, tea.MouseClickMsg{X: deep.x0, Y: deep.y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: deep.x0, Y: deep.y, Button: tea.MouseLeft})
	if a.tp.sel != deep.id || !a.at(pageTeams) || a.frontTabKey() != front {
		t.Fatal("compact row did not open its own team overview")
	}
}

func TestTeamsAllCompactTreeScrollReachesDeepestRowAndClosedHistory(t *testing.T) {
	a := teamsDeepOverviewLab(t)
	a.width, a.height = 42, 20
	a.tp.closedOpen = true
	a.wall.teams[11].State = "closed"
	a.wall.teams[11].Name = "最深 team"
	_ = teamsFrameText(a)
	for i := 0; i < 40; i++ {
		a.teamsScrollPane(3)
		_ = teamsFrameText(a)
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "最深 team") || a.tp.paneOffset != a.tp.paneRows-a.tp.paneRoom {
		t.Fatalf("lowest descendant not visible at bottom: %s", text)
	}
	var leaf teamsTarget
	for _, target := range a.tp.targets {
		if target.pane && target.id == "depth-11" && target.arg == "overview" {
			leaf = target
			break
		}
	}
	if leaf.id == "" || !strings.Contains(leaf.hint, "Level 1") || !strings.Contains(leaf.hint, "history") {
		t.Fatalf("closed row lost ancestry/history: %+v", leaf)
	}
	hit, ok := a.teamsTargetAt(leaf.x0, leaf.y)
	if !ok || hit.id != leaf.id {
		t.Fatal("parent frame intercepted lowest row")
	}
}
