package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// The grid borrows the window without changing its remembered overlay or draft.
// Opening a tile is an explicit choice to return to the bare conversation.
func TestConversationGridRestoresOverlayOnCancelAndOpensBare(t *testing.T) {
	for _, pointer := range []bool{false, true} {
		for _, chooseCurrent := range []bool{false, true} {
			t.Run(fmt.Sprintf("pointer%v/current%v", pointer, chooseCurrent), func(t *testing.T) {
				a, harbor, orbit := menuApp(t)
				front := a.frontTabKey()
				a.input.insert("answer in progress")
				a.tabView = tabViewport{from: 1, browsing: true}
				spend(t, a, a.openWall())
				_ = a.wallFrame(a.width, a.height)
				tiles := a.wallShown(a.now())
				unrelated := -1
				for i, tile := range tiles {
					if teamHolds(mustTeam(t, a, orbit), tile.tab.key) {
						unrelated = i
					}
				}
				if len(tiles) != 3 || unrelated < 0 || a.wall.activeID != "" {
					t.Fatal("grid was filtered by overlay")
				}
				wallKeyPress(a, "esc")
				if a.wall.on || a.wall.activeID != harbor || a.teamViews.id != harbor || a.frontTabKey() != front || a.input.String() != "answer in progress" || a.tabView.from != 1 {
					t.Fatal("cancel lost overlay, selection, viewport or draft")
				}
				spend(t, a, a.openWall())
				a.wallSettle()
				_ = a.wallFrame(a.width, a.height)
				chosen := unrelated
				if chooseCurrent {
					for i, tile := range tiles {
						if tile.tab.key == front {
							chosen = i
						}
					}
				}
				want := tiles[chosen].tab.key
				if pointer {
					spend(t, a, wallClick(t, a, wallHitFor(t, a, wallHitTile, chosen)))
				} else {
					a.wallMove(chosen, len(tiles))
					spend(t, a, wallKeyPress(a, "enter"))
				}
				if a.wall.on || a.wall.activeID != "" || a.teamViews.id != "" || a.frontTabKey() != want {
					t.Fatal("tile did not enter bare Chats")
				}
				if got := a.teamViews.views[harbor].viewport; got.from != 1 || !got.browsing {
					t.Fatalf("tile overwrote original viewport: %+v", got)
				}
				if chooseCurrent && a.input.String() != "answer in progress" {
					t.Fatal("opening current tile lost the draft")
				}
			})
		}
	}
}

// Reserved grid cells remain at the window edge for Unicode titles, overflow,
// pinned managers, empty chats, pointer repainting and repeated memoized frames.
func TestTabWallDoorStaysAtRightAcrossWidthsAndOverflow(t *testing.T) {
	for _, overlay := range []bool{false, true} {
		a, harbor, _ := menuApp(t)
		a.start = func(string) (Conversation, error) { return Conversation{}, nil }
		if overlay {
			if err := a.teamMakeManager(harbor, a.teamMenuFront()); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 45; i++ {
				if err := a.teamAdd(harbor, []chatTab{{key: fmt.Sprintf("saved%d", i), file: fmt.Sprintf("/tmp/saved%d/transcript.jsonl", i), word: "広いタイトル café " + fmt.Sprint(i)}}); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			a.teamViewSet("")
		}
		for width := roomHeadFloor; width <= 200; width++ {
			a.width = width
			a.touch()
			for _, hover := range []bool{false, true} {
				row := a.tabsRow(width)
				door := a.wall.door
				if !door.pressable() || door.to != width || ansi.StringWidth(row) != width || plain(ansi.Cut(row, door.from, door.to)) != " ▦ All " {
					t.Fatalf("overlay%v width%d door%+v row%q", overlay, width, door, plain(row))
				}
				for _, hit := range a.chatTabHits {
					if hit.span.to > door.from {
						t.Fatalf("width%d target overlaps grid door: %+v", width, hit)
					}
				}
				if got := a.tabsRow(width); got != row || a.wall.door != door {
					t.Fatal("memo changed the fixed door")
				}
				if hover {
					a.hot = hoverAt{}
				} else {
					var ok bool
					a.hot, ok = a.tabHoverAt(door.from+2, tabStripRow)
					if !ok {
						t.Fatal("grid button has no hover target")
					}
				}
			}
		}
	}
}

// Grid management keys cannot mutate memberships or open competing controls.
func TestConversationGridHasOnlyConversationControls(t *testing.T) {
	a, _, _ := menuApp(t)
	spend(t, a, a.openWall())
	for _, key := range []string{"m", "s", "e", "r", "D", "o", "u", "1", "9"} {
		wallKeyPress(a, key)
		if !a.wall.on || a.wall.activeID != "" || a.wall.naming || a.wall.org.on || a.wall.pop.kind != wallPopNone || a.tsheet.on {
			t.Fatalf("%s opened team controls", key)
		}
	}
	wallKeyPress(a, "?")
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	for _, word := range []string{"New team", "Make team", "Add to", "Team settings", "Switch team", "Organize"} {
		if strings.Contains(frame, word) {
			t.Fatalf("help advertises %q", word)
		}
	}
}

// Filtering narrows the visible tiles without claiming the other open chats vanished.
func TestConversationGridFilterKeepsTotalOpenCount(t *testing.T) {
	a, _, _ := tabApp(t)
	spend(t, a, a.openWall())
	a.wall.filter = "Shipping the parser"
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if len(a.wallShown(a.now())) != 1 || !strings.Contains(frame, "1 of 3 open") {
		t.Fatal(frame)
	}
}

// Team creation remains reachable through the Teams sidebar after retiring grid actions.
func TestConversationGridRetainsTeamsOriginNamingFlow(t *testing.T) {
	a, harbor, _ := menuApp(t)
	runCmd(a.showPage(pageTeams))
	for _, parent := range []string{"", harbor} {
		a.closeWall()
		if parent != "" {
			runCmd(a.teamsSelect(parent))
		}
		act := teamsActNewTeam
		if parent != "" {
			act = teamsActAddSubteam
		}
		target := teamsTargetOf(t, a, act, parent)
		drive(t, a, tea.MouseClickMsg{X: target.x0, Y: target.y, Button: tea.MouseLeft})
		if !a.wall.on || !a.wall.naming || a.wall.nameParent != parent {
			t.Fatal("Teams did not open its naming card")
		}
		wantName := "new top"
		if parent != "" {
			wantName = "new sub"
		}
		a.wall.name = wantName
		_ = a.wallFrame(a.width, a.height)
		spend(t, a, wallClick(t, a, wallHitFor(t, a, wallHitAction, int(wallActSave))))
		made := a.wall.teams[len(a.wall.teams)-1]
		if made.Name != wantName || made.Parent != parent || a.wall.naming {
			t.Fatalf("naming flow: %+v", made)
		}
	}
}

// The picker keeps the selected row visible in a long tree and cannot activate
// an invisible selection when a resize leaves no room for the card.
func TestTeamMenuLongTreeScrollsAndTinyResizeCannotChoose(t *testing.T) {
	a, harbor, _ := menuApp(t)
	a.height = 24
	for i := 0; i < 35; i++ {
		if _, err := a.teamMake(fmt.Sprintf("extra %02d", i), []chatTab{a.teamMenuFront()}); err != nil {
			t.Fatal(err)
		}
	}
	a.openTeamMenu()
	menuFrame(t, a)
	if !a.teamMenu.card.holds(a.teamMenu.card.x0, a.teamMenu.card.y0) || len(a.teamMenu.hits) >= len(a.teamMenuRows()) {
		t.Fatal("long picker was not bounded")
	}
	for range 37 {
		drive(t, a, key("down"))
		menuFrame(t, a)
	}
	chosen := a.teamMenuRows()[a.teamMenu.cursor]
	hit := menuHit(t, a, chosen.code, chosen.id)
	if !a.teamMenu.card.holds(hit.x0, hit.y0) || a.teamMenu.top == 0 {
		t.Fatal("selected last row is offscreen")
	}
	drive(t, a, tea.MouseWheelMsg{X: hit.x0, Y: hit.y0, Button: tea.MouseWheelUp})
	menuFrame(t, a)
	if !a.teamMenu.on || a.teamMenu.cursor >= 37 || a.wall.activeID != harbor {
		t.Fatal("wheel escaped the picker or selected a team")
	}
	for width := 22; width <= 80; width++ {
		a.width = width
		menuFrame(t, a)
		if a.teamMenu.card.x1 > width || len(a.teamMenu.hits) == 0 {
			t.Fatalf("width%d picker escaped frame: %+v", width, a.teamMenu.card)
		}
	}
	a.width = 12
	menuFrame(t, a)
	if len(a.teamMenu.hits) != 0 {
		t.Fatal("tiny picker retained hits")
	}
	drive(t, a, key("enter"))
	if !a.teamMenu.on || a.wall.activeID != harbor {
		t.Fatal("invisible picker selected a team")
	}
	drive(t, a, key("esc"))
	if a.teamMenu.on {
		t.Fatal("tiny picker could not cancel")
	}
}

func TestConversationGridDoesNotMarkDisbandedMemberships(t *testing.T) {
	a, harbor, _ := menuApp(t)
	spend(t, a, a.openWall())
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	here := -1
	for i, tile := range tiles {
		if tile.here {
			here = i
		}
	}
	if here < 0 {
		t.Fatal("no front tile")
	}
	title := func() string {
		rows := a.wallFrame(a.width, a.height)
		hit := wallHitFor(t, a, wallHitTile, here)
		return plain(ansi.Cut(rows[hit.y0], hit.x0, hit.x1))
	}
	if !strings.Contains(title(), "●") {
		t.Fatal("active membership dot absent")
	}
	runCmd(a.teamSheetOpen(harbor, teamSheetClose))
	runCmd(a.teamSheetDo(tsCloseNow))
	if got := title(); strings.Contains(got, "●") {
		t.Fatalf("disbanded membership still marked: %s", got)
	}
}
