package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── ONE GRAMMAR, EVERY PLACE — CHECKED ON EVERY PLACE ───────────────────────
//
// placekeys.go states the law and pages.go states the frame; the seven rooms
// were never all walked through in one test, which is how the built binary came
// to ship a `tab` that could not leave home and four places with no pointer at
// all. This file opens each of them WITH SOMETHING IN IT and asks the same four
// questions of every one.
//
// EACH PLACE BRINGS ITS OWN LAB, because "open with something in it" is the
// whole difficulty: a place with an empty body proves nothing about a cursor
// walking past the bottom of its window.

// everyPlace is one room, opened and filled.
type everyPlace struct {
	// id is which place this is, and open is a surface standing in it.
	id   page
	open func(t *testing.T) *app
	// cursor is the line of its body the keyboard is on, and hits are the body
	// lines the last frame actually drew — the pair that answers "is the row the
	// cursor is on still on the screen".
	cursor func(a *app) int
	hits   func(a *app) []int
}

// everyPlaceTable is the seven rooms, each opened WITH SOMETHING IN IT.
//
// IT IS CHECKED AGAINST THE REGISTRY BY [TestEveryPlaceBringsItsOwnLab], because
// a table of labs that fell one behind the registry would silently stop asking
// the new room any of the questions below — and "the room nobody tested" is
// exactly what `alt+2`, `alt+3` and `alt+4` were on a fresh machine.
func everyPlaceTable() []everyPlace {
	return []everyPlace{
		{
			id:     pageHome,
			open:   func(t *testing.T) *app { return switchPlaceLab(t) },
			cursor: func(a *app) int { return a.home.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.homeFrame(a.width, a.height)
				return hits
			},
		},
		{
			id:     pageTasks,
			open:   func(t *testing.T) *app { return historyApp(t, 200) },
			cursor: func(a *app) int { return a.taskSheet.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.taskSheetFrame(a.width, a.height)
				out := make([]int, 0, len(hits))
				for _, hit := range hits {
					if hit.kind == taskSheetHitRow {
						out = append(out, hit.index)
						continue
					}
					out = append(out, -1)
				}
				return out
			},
		},
		{
			id:     pageStanding,
			open:   func(t *testing.T) *app { return standPlaceLab(t) },
			cursor: func(a *app) int { return a.orders.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.standingPlaceFrame(a.width, a.height)
				return hits
			},
		},
		{
			id:     pageMemory,
			open:   memoryPlaceLab,
			cursor: func(a *app) int { return a.mem.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.memoryFrame(a.width, a.height)
				return hits
			},
		},
		{
			id:     pageSpend,
			open:   spendPlaceLab,
			cursor: func(a *app) int { return a.spend.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.spendFrame(a.width, a.height)
				return hits
			},
		},
		{
			id:     pageSearch,
			open:   searchPlaceWithHits,
			cursor: func(a *app) int { return a.search.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.searchFrame(a.width, a.height)
				return hits
			},
		},
		{
			id:     pageSettings,
			open:   settingsPlaceLab,
			cursor: func(a *app) int { return a.sheet.cursor },
			hits: func(a *app) []int {
				_, hits, _, _ := a.sheetFrame(a.width, a.height)
				out := make([]int, 0, len(hits))
				for _, hit := range hits {
					if hit.kind == sheetHitRow {
						out = append(out, hit.index)
						continue
					}
					out = append(out, -1)
				}
				return out
			},
		},
	}
}

// ── the labs ────────────────────────────────────────────────────────────────

// switchPlaceLab is home over a machine with more conversations than the frame
// can hold, with the fold at its foot standing open — so the list genuinely runs
// on past the bottom of the window.
func switchPlaceLab(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	var mine string
	for i := 0; i < 20; i++ {
		bucket := "-project" + itoa(i%4)
		file := lab.session(bucket, "aaaa0000000000"+itoa(10+i),
			"the "+itoa(i)+"th conversation", lab.workspace("project"+itoa(i%4)),
			now.Add(-time.Duration(i+1)*time.Hour))
		if i == 0 {
			mine = file
		}
	}
	a := lab.app(mine)
	a.width, a.height = 120, 20
	openHomeOn(a, mine)
	// THE FOLD STANDS OPEN, because the resting list caps itself at
	// [switcherShown] and a capped list has nothing under its window to scroll to.
	a.home.foldSwitch(true)
	return a
}

// memoryPlaceLab is the memory place over more lines than the frame can hold.
func memoryPlaceLab(t *testing.T) *app {
	t.Helper()
	rows := make([]store.Memory, 0, 40)
	for i := 0; i < 40; i++ {
		rows = append(rows, store.Memory{
			ID: "m" + itoa(i), Scope: store.MemoryScopeUser, Type: store.MemoryFact,
			Title: "the " + itoa(i) + "th thing", Text: "held true, number " + itoa(i),
			Status: store.MemoryActive, UpdatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	a, _ := memoryPlaceApp(t, rows)
	a.width, a.height = 120, 20
	a.pal = newPalette(tokens.ANSI256, false)
	if cmd := a.showPage(pageMemory); cmd != nil {
		runCmd(cmd)
	}
	if !a.at(pageMemory) {
		t.Fatal("the memory place did not open over forty lines")
	}
	return a
}

// spendPlaceLab is the spend place over the ledger fixture, on the pinned clock
// that fixture is written for.
func spendPlaceLab(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	a.width, a.height = 120, 20
	a.clock = func() time.Time { return spendTestNow }
	if cmd := a.showPage(pageSpend); cmd != nil {
		runCmd(cmd)
	}
	if a.page != pageSpend {
		t.Fatal("the spend place did not open")
	}
	// THE LINES ARE PUT ON THE PLACE RATHER THAN WRITTEN TO A LEDGER FILE,
	// because what is under test is the window over a body and not the reader
	// that fills it (spendplace_test.go owns that). There is a line for every
	// day of the window and three models against each, so the page has more rows
	// a cursor can stop on than the frame has room for — which is the whole
	// condition both the pointer and the window are being asked about.
	var lines []session.UsageLine
	models := []string{"opus 4.1", "sonnet 4.5", "haiku 4.5"}
	for day := 12; day <= 25; day++ {
		for i, model := range models {
			lines = append(lines, session.UsageLine{
				At:    time.Date(2026, time.August, day, 12, 0, 0, 0, time.Local),
				Model: model, Role: "execution", Calls: 10 + i, Input: 100000, Output: 20000,
				USD: float64(day) / 10, Session: "talk-1", Task: "errand-" + itoa(day),
				Workspace: "/work/aforge",
			})
		}
	}
	a.spend.lines, a.spend.read = lines, spendTestNow
	a.spend.win = session.LastDays(spendTestNow, spendWindowDays)
	a.rebuildSpend()
	return a
}

// settingsPlaceLab is the settings place, which opens on any machine.
func settingsPlaceLab(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	a.width, a.height = 120, 20
	a.showPage(pageSettings)
	if !a.at(pageSettings) {
		t.Fatal("the settings place did not open")
	}
	return a
}

// ── the four questions ──────────────────────────────────────────────────────

// tab AND shift+tab LEAVE EVERY PLACE. The circle is the whole point of the
// bar; a room `tab` cannot get out of is a room a person is stuck in, which is
// what home was on a machine with no tasks and no orders.
func TestTabLeavesEveryPlaceAndComesBack(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			if a.page != place.id {
				t.Fatalf("the lab left the router on %q", a.page.word())
			}
			drive(t, a, key("tab"))
			if a.page == place.id {
				t.Fatalf("tab did not leave the %s place", place.id.word())
			}
			a = place.open(t)
			drive(t, a, key("shift+tab"))
			if a.page == place.id {
				t.Fatalf("shift+tab did not leave the %s place", place.id.word())
			}
		})
	}
}

// alt+1…7 JUMPS FROM EVERY PLACE. The numbers are the bar's own order and they
// mean the same thing wherever you are standing — or, for a room with nothing
// in it, they say why and leave you where you were. What they may never do is
// nothing at all.
func TestTheNumbersJumpFromEveryPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			for at, id := range pages() {
				a := place.open(t)
				drive(t, a, key("alt+"+itoa(at+1)))
				switch {
				case a.page == id:
					// it opened, which is the whole of the promise
				case a.page != place.id:
					t.Fatalf("alt+%d from %s landed on %q, which is neither %q nor where it started",
						at+1, place.id.word(), a.page.word(), id.word())
				case a.pageMsg == "":
					t.Fatalf("alt+%d from %s did nothing and said nothing", at+1, place.id.word())
				}
			}
		})
	}
}

// THE SHIFT ARROWS BELONG TO TIME AND NEVER TO THE BAR (SCREEN 3d), on every
// place and not only on the ones a spot check happened to reach.
func TestTheShiftArrowsSwitchNoPlaceAnywhere(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			for _, k := range []string{"shift+left", "shift+right", "shift+up", "shift+down"} {
				drive(t, a, key(k))
				if a.page != place.id {
					t.Fatalf("%s moved from the %s place to %q", k, place.id.word(), a.page.word())
				}
			}
		})
	}
}

// THE WHEEL IS THE PLACE'S AND NEVER THE CONVERSATION'S. Every place takes the
// whole frame, so a wheel that fell through would scroll a transcript nobody
// can see — and leave a person back on a page that has silently moved when esc
// gives the frame back.
func TestTheWheelNeverReachesTheConversationFromAPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			offset, stick := a.offset, a.stick
			for i := 0; i < 6; i++ {
				drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
			}
			for i := 0; i < 6; i++ {
				drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelUp})
			}
			if a.offset != offset || a.stick != stick {
				t.Fatalf("the wheel on the %s place moved the conversation under it: offset %d→%d, stick %v→%v",
					place.id.word(), offset, a.offset, stick, a.stick)
			}
			if a.page != place.id {
				t.Fatalf("the wheel on the %s place left it for %q", place.id.word(), a.page.word())
			}
		})
	}
}

// THE POINTER PREVIEWS ON EVERY PROMOTED PLACE, and moves nothing. It is home's
// own law ([homeView.previewLine], homehover_test.go) owed to the four lists the
// router promoted out of the conversation's chrome — the standing place's hover
// map answered -1 by declaration and the other three had none at all, so a
// pointer crossing any of them lit nothing.
func TestThePointerPreviewsOnEveryPromotedPlace(t *testing.T) {
	promoted := map[page]bool{pageStanding: true, pageMemory: true, pageSpend: true, pageSearch: true}
	for _, place := range everyPlaceTable() {
		if !promoted[place.id] {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// A TALLER FRAME THAN THE LABS OPEN ON, because this law needs two
			// rows a pointer can land on to be drawn AT ONCE — the spend place's
			// stops are the few rows that name something money went to, and a
			// short window can draw exactly one of them. The window itself is
			// what the short frames in this file are for.
			a.height = 40
			cursor := place.cursor(a)
			lit := false
			for y, at := range place.hits(a) {
				if at < 0 || at == cursor {
					continue
				}
				before := placeFrameLines(a, place)
				drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
				if got := place.cursor(a); got != cursor {
					t.Fatalf("the pointer over row %d moved the cursor from %d to %d", y, cursor, got)
				}
				after := placeFrameLines(a, place)
				if y < len(before) && y < len(after) && before[y] != after[y] {
					lit = true
					break
				}
			}
			if !lit {
				t.Fatalf("no row of the %s place lights under the pointer", place.id.word())
			}
		})
	}
}

// placeFrameLines is whatever place is up, drawn — the same rows the pointer is
// resolved against.
func placeFrameLines(a *app, place everyPlace) []string {
	place.hits(a)
	lines, _, _ := a.frame()
	return strings.Split(lines, "\n")
}

// ↓ PAST THE BOTTOM OF THE WINDOW SCROLLS THE LIST. A place whose body is cut
// at the room and never moved walks its own cursor off the screen, which is the
// one thing a list may never do — and three of these places did exactly that.
func TestWalkingPastTheWindowKeepsTheCursorOnTheFrame(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			drawn := 0
			for _, at := range place.hits(a) {
				if at >= 0 {
					drawn++
				}
			}
			// Well past the last drawn row, and past the end of most of these
			// bodies — a cursor that stops at the end is still a cursor on screen.
			for i := 0; i < drawn+12; i++ {
				drive(t, a, key("down"))
			}
			cursor := place.cursor(a)
			for _, at := range place.hits(a) {
				if at == cursor {
					return
				}
			}
			t.Fatalf("the cursor walked to body line %d and the %s place does not draw it",
				cursor, place.id.word())
		})
	}
}

// EVERY REGISTERED PLACE BRINGS ITS OWN LAB. The five questions above and the
// six laws in placelaws_test.go all walk this table, so a place missing from it
// is a place with no test at all — and the table is a literal because "open it
// with something in it" is the whole difficulty and cannot be derived.
func TestEveryPlaceBringsItsOwnLab(t *testing.T) {
	labs := map[page]bool{}
	for _, place := range everyPlaceTable() {
		if labs[place.id] {
			t.Fatalf("two labs open the %s place", place.id.word())
		}
		labs[place.id] = true
	}
	for _, id := range pages() {
		if !labs[id] {
			t.Fatalf("the %s place is registered and has no lab in everyPlaceTable", id.word())
		}
	}
	if len(labs) != len(placeRegistry) {
		t.Fatalf("%d labs for %d registered places", len(labs), len(placeRegistry))
	}
}
