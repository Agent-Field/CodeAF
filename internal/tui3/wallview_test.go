package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// wallTestNow is the fixture's clock; the painter reads no other.
var wallTestNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// wallFixture is n tiles cycling through every state the painter draws:
// working, waiting on a person, idle, frozen, marked, and one with no spark;
// in three teams, some tiles in several.
// wallTestIDs are the fixture's teams' ids, port, infra and research in that
// order.
var wallTestIDs = []string{"a1b2c3d4e5f6", "0f9e8d7c6b5a", "123456abcdef"}

// wallViewHues is the colour of every team in v, in order.
func wallViewHues(v wallView) []teamHueSpec {
	out := make([]teamHueSpec, 0, len(v.teams))
	for _, t := range v.teams {
		out = append(out, t.hue)
	}
	return out
}

func wallFixture(n int) wallView {
	names := []string{"the tree walk", "cut the goldens", "ship the port", "relay audit", "footprint table", "crew reprice", "suite lock"}
	v := wallView{team: wallTestIDs[0], now: wallTestNow, spin: 3}
	reserved := teamReservedFrom(darkRamp)
	var hues []teamHueSpec
	for i, name := range []string{"port", "infra", "research"} {
		hue := nextTeamHue(hues, reserved)
		hues = append(hues, hue)
		v.teams = append(v.teams, wallTeamRow{id: wallTestIDs[i], name: name, hue: hue, count: 3 - i, members: 4 - i})
	}
	v.total = n
	for i := 0; i < n; i++ {
		t := wallTile{
			tab:  chatTab{key: fmt.Sprintf("k%d", i)},
			name: names[i%len(names)],
			live: true,
			age:  fmt.Sprintf("%dm", i+2),
			lines: []wallLine{
				{wallUser, "look at the watcher and tell me whether it caches both facts"},
				{wallTool, "▸ read internal/tui3/keeper.go"},
				{wallProse, "the watcher already caches the two facts at the transitions it computes, so the strip can read both as atomics without taking the agent's mutex"},
				{wallNote, "compacted 12 turns"},
				{wallTool, "bash go test ./internal/tui3 -run TestWall"},
				{wallProse, "writing the change note"},
			},
			spark: []uint8{0, 1, 2, 4, 6, 4, 2, 1, 4, 6, 7, 3},
			moved: wallTestNow.Add(-time.Duration(i+2) * time.Minute),
		}
		switch i % 6 {
		case 0:
			t.signal = tabWorking
			t.doing = "running bash"
			t.fresh, t.freshAt = 2, wallTestNow.Add(-200*time.Millisecond)
			t.teams = []string{wallTestIDs[0], wallTestIDs[1]}
		case 1:
			t.signal = tabNeedsPerson
			t.doing = "waiting on you"
			t.question = "needs your ok to run bash: go test ./internal/tui3"
			t.teams = []string{wallTestIDs[0]}
		case 2:
			t.signal = tabWorking
			t.doing = "writing"
			t.marked = true
			t.teams = []string{wallTestIDs[0], wallTestIDs[1], wallTestIDs[2]}
		case 3:
			t.signal = tabIdle
			t.spark = nil
		case 4:
			t.signal = tabWorking
			t.live = false
		case 5:
			t.signal = tabIdle
			t.marked = true
			t.spark = []uint8{0, 0, 0}
			t.teams = []string{wallTestIDs[2]}
		}
		v.tiles = append(v.tiles, t)
	}
	return v
}

// wallUnmarked is v with nothing picked, so no tray floats over the grid.
func wallUnmarked(v wallView) wallView {
	tiles := append([]wallTile(nil), v.tiles...)
	for i := range tiles {
		tiles[i].marked = false
	}
	v.tiles = tiles
	return v
}

func wallTestPalettes() map[string]palette {
	return map[string]palette{
		"unicode": newPalette(tokens.TrueColor, false),
		"ascii":   newPalette(tokens.TrueColor, true),
	}
}

var wallTestSizes = [][2]int{{80, 24}, {120, 40}, {180, 50}}

// wallPlainFrame is the frame as text, one row per line.
func wallPlainFrame(rows []string) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(ansi.Strip(r))
		b.WriteString("\n")
	}
	return b.String()
}

func TestWallViewFrames(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, n := range []int{1, 6, 13} {
			for _, sz := range wallTestSizes {
				w, h := sz[0], sz[1]
				for _, focus := range []int{0, n / 2, n - 1} {
					v := wallUnmarked(wallFixture(n))
					v.focus = focus
					name := fmt.Sprintf("%s/n%d/%dx%d/f%d", pname, n, w, h, focus)
					rows, hits := renderWall(pal, v, w, h)
					wallCheckRows(t, name, rows, w, h)
					wallCheckHits(t, name, hits, w, h)
					wallCheckFocus(t, name, pal, rows, hits, focus)
				}
			}
		}
	}
}

// wallCheckRows: exactly h rows, none wider than w.
func wallCheckRows(t *testing.T, name string, rows []string, w, h int) {
	t.Helper()
	if len(rows) != h {
		t.Fatalf("%s: %d rows, want %d", name, len(rows), h)
	}
	for y, r := range rows {
		if got := ansi.StringWidth(r); got > w {
			t.Fatalf("%s: row %d is %d cells wide, frame is %d: %q", name, y, got, w, ansi.Strip(r))
		}
	}
}

// wallCheckHits: every hit inside the frame, none overlapping another.
func wallCheckHits(t *testing.T, name string, hits []wallHit, w, h int) {
	t.Helper()
	for i, a := range hits {
		if a.x0 < 0 || a.y0 < 0 || a.x1 > w || a.y1 > h || a.x0 >= a.x1 || a.y0 >= a.y1 {
			t.Fatalf("%s: hit %d out of bounds: %+v", name, i, a)
		}
		for j, b := range hits[:i] {
			if a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1 {
				t.Fatalf("%s: hits %d and %d overlap: %+v %+v", name, j, i, b, a)
			}
		}
	}
}

// wallCheckFocus: the focused tile is on screen, and it and only it is heavy.
// Each tile's first hit is the one on its top-left corner (wallTileHits).
func wallCheckFocus(t *testing.T, name string, pal palette, rows []string, hits []wallHit, focus int) {
	t.Helper()
	heavyTL, lightTL := "┏", "╭"
	if pal.ascii {
		heavyTL, lightTL = "#", "+"
	}
	found := false
	seen := map[int]bool{}
	for _, hit := range hits {
		if hit.kind != wallHitTile || seen[hit.arg] {
			continue
		}
		seen[hit.arg] = true
		corner := ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x0+1))
		want := lightTL
		if hit.arg == focus {
			found = true
			want = heavyTL
		}
		if corner != want {
			t.Fatalf("%s: tile %d corner %q, want %q", name, hit.arg, corner, want)
		}
	}
	if !found {
		t.Fatalf("%s: focused tile %d not on screen", name, focus)
	}
}

func TestWallScrollKeepsFocus(t *testing.T) {
	for _, n := range []int{1, 6, 13} {
		for _, sz := range wallTestSizes {
			w, h := sz[0], sz[1]
			c, _, tileH := wallGrid(n, w, h, 0)
			vis := wallVisibleRows(h, tileH)
			scroll := 0
			// Walk the focus down and back up; every step stays on screen.
			path := make([]int, 0, 2*n)
			for f := 0; f < n; f++ {
				path = append(path, f)
			}
			for f := n - 1; f >= 0; f-- {
				path = append(path, f)
			}
			for _, f := range path {
				scroll = wallScrollFor(f, scroll, n, w, h, 0)
				row := f / c
				if row < scroll || row >= scroll+vis {
					t.Fatalf("n%d %dx%d: focus %d (row %d) off screen at scroll %d, %d rows visible", n, w, h, f, row, scroll, vis)
				}
			}
			// A focus already visible does not move the scroll.
			if got := wallScrollFor(0, 0, n, w, h, 0); got != 0 {
				t.Fatalf("n%d %dx%d: scroll moved to %d for a visible focus", n, w, h, got)
			}
		}
	}
}

func TestWallGridShape(t *testing.T) {
	for _, tc := range []struct {
		w, cols, want int
	}{
		{80, 0, 1}, {120, 0, 2}, {180, 0, 3}, {220, 0, 4},
		// An override past the minimum tile width loses columns instead of
		// squeezing: the rows scroll.
		{120, 4, 2}, {80, 4, 1}, {40, 3, 1}, {200, 4, 4},
	} {
		c, tileW, _ := wallGrid(13, tc.w, 40, tc.cols)
		if c != tc.want {
			t.Errorf("width %d cols %d: %d columns, want %d", tc.w, tc.cols, c, tc.want)
		}
		if c > 1 && tileW < wallTileMinW {
			t.Errorf("width %d cols %d: tile %d wide", tc.w, tc.cols, tileW)
		}
		// The grid keeps a margin either side.
		if used := c*tileW + (c-1)*wallGutter; used > tc.w-2*wallMargin {
			t.Errorf("width %d cols %d: the grid takes %d cells and leaves no margin", tc.w, tc.cols, used)
		}
	}
	for _, h := range []int{24, 40, 50} {
		_, _, tileH := wallGrid(13, 120, h, 0)
		gridH := h - wallChromeRows
		if tileH > wallTileMaxH || (tileH < wallTileMinH && gridH >= wallTileMinH) {
			t.Errorf("height %d: tile %d high", h, tileH)
		}
		// A frame that can hold two rows at the floor always shows two.
		if vis := wallVisibleRows(h, tileH); vis < 2 && gridH >= 2*wallTileMinH+wallRowGap {
			t.Errorf("height %d: %d rows visible, want at least 2", h, vis)
		}
	}
}

func TestWallRenderStates(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallFixture(6)
	rows, _ := renderWall(pal, v, 180, 50)
	frame := wallPlainFrame(rows)
	for _, want := range []string{
		"▦ Conversations", "open in this window · in port", "⠿ 2 running", "? 1 needs you", "6 open",
		"Teams", "All 6", "port 3", "+ New team",
		"Answer ↵", "seen 6m ago", "running bash " + wallGlyphsFor(false).sep + " 2m", "? waiting on you",
		"updated 5m ago", "☑", "▌", tokens.Spinner(v.spin),
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q\n%s", want, frame)
		}
	}
	// A frozen tile draws no spinner, and the ages left the borders.
	if got := strings.Count(frame, tokens.Spinner(v.spin)); got != 2 {
		t.Errorf("want two spinners (the live working tiles), got %d", got)
	}
	for _, r := range rows {
		if top := ansi.Strip(r); strings.Contains(top, "╭─") && strings.Contains(top, " 2m ") {
			t.Errorf("an age is still on a border: %q", top)
		}
	}
	// "wall" and "tab" are never drawn.
	for _, word := range []string{" wall ", " tab "} {
		if strings.Contains(strings.ToLower(frame), word) {
			t.Errorf("the frame says %q", word)
		}
	}

	empty, hits := renderWall(pal, wallView{}, 80, 24)
	if len(empty) != 24 || len(hits) != 1 || hits[0].kind != wallHitAction || hits[0].arg != int(wallActBack) {
		t.Fatalf("empty: %d rows, hits %+v", len(empty), hits)
	}
	if got := ansi.Strip(ansi.Cut(empty[hits[0].y0], hits[0].x0, hits[0].x1)); !strings.Contains(got, "Back") {
		t.Fatalf("the empty frame's way back is drawn as %q", got)
	}
	if !strings.Contains(wallPlainFrame(empty), "No open conversations") {
		t.Fatalf("the empty frame says nothing")
	}

	v.filtering, v.filter = true, "port"
	rows, _ = renderWall(pal, v, 120, 40)
	if got := ansi.Strip(rows[1]); !strings.Contains(got, "/ port▌") || !strings.Contains(got, "Clear esc") {
		t.Errorf("filter row: %q", got)
	}
	v.filtering, v.filter, v.naming, v.name = false, "", true, "night"
	v.choices = teamHueChoices(wallViewHues(v), teamReservedFrom(darkRamp), 6)
	rows, _ = renderWall(pal, v, 120, 40)
	card := wallPlainFrame(rows)
	for _, want := range []string{"─ New team ─", "Name    night▌", "Colour  ◉ ● ● ● ● ●", "2 · ship the port, crew reprice", "Cancel esc", "Create ↵", "Shuffle"} {
		if !strings.Contains(card, want) {
			t.Errorf("the new-team card lacks %q\n%s", want, card)
		}
	}
	// Too short for the card, the prompt falls back to the Teams row.
	rows, _ = renderWall(pal, v, 120, 9)
	if got := ansi.Strip(rows[1]); !strings.Contains(got, "New team › night▌") || !strings.Contains(got, "2 picked") {
		t.Errorf("naming row: %q", got)
	}
}

func TestWallRenderPrintsFrame(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		base := wallFixture(6)
		base.focus = 0

		v := wallUnmarked(base)
		rows, _ := renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, at rest, tiles dotted by their teams:\n%s", pname, wallPlainFrame(rows))

		v.hover = wallHitRef{kind: wallHitTile, arg: 1}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, tile 1 hovered:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.hover = wallHitRef{kind: wallHitSelect, arg: 3}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, tile 3's Select hovered, its action row up:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.help = true
		v.hover = wallHitRef{kind: wallHitHelp, arg: 3}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, the help sheet, Next needing you hovered:\n%s", pname, wallPlainFrame(rows))
		rows, _ = renderWall(pal, v, 80, 21)
		t.Logf("%s 80x21 (an 80x24 window's wall), the help sheet scrolling:\n%s", pname, wallPlainFrame(rows))
		v.helpTop = 99
		rows, _ = renderWall(pal, v, 80, 21)
		t.Logf("%s 80x21, the help sheet scrolled to its end:\n%s", pname, wallPlainFrame(rows))

		v = base
		v.hover = wallHitRef{kind: wallHitAction, arg: int(wallActAddTo)}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, tiles 2 and 5 selected, the tray up, every tile's box on its top border:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.pop = wallPop{kind: wallPopMembers, x: 90, y0: 4, y1: 5, targets: []string{"k1"}, cursor: -1}
		v.hover = wallHitRef{kind: wallHitPopRow, id: wallTestIDs[1]}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, the teams popover of tile 1:\n%s", pname, wallPlainFrame(rows))

		v = base
		v.naming, v.name, v.nameFresh = true, "harbor", true
		v.choices = teamHueChoices(wallViewHues(v), teamReservedFrom(darkRamp), 6)
		v.hover = wallHitRef{kind: wallHitAction, arg: int(wallActSave)}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, naming a team:\n%s", pname, wallPlainFrame(rows))

		v.asking, v.hover = true, wallHitRef{}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, naming a team while a name is asked for:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.hover = wallHitRef{kind: wallHitTeams, arg: 0}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, tile 0's teams control hovered, the toolbar saying what it does:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.focus, v.hover = 1, wallHitRef{kind: wallHitOpen, arg: 1}
		v.pointerOn, v.pointerY = true, wallAnswerHit(t, pal, v, 120, 40).y0
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, the waiting tile focused, its Answer hovered:\n%s", pname, wallPlainFrame(rows))

		v = wallUnmarked(base)
		v.pop = wallPop{kind: wallPopSettings, x: 20, y0: 1, y1: 2, team: wallTestIDs[1], name: "infra",
			choices: teamHueChoices(wallViewHues(v), teamReservedFrom(darkRamp), 6)}
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, a team's settings:\n%s", pname, wallPlainFrame(rows))

		for _, sz := range [][2]int{{80, 24}, {180, 50}} {
			rows, _ = renderWall(pal, wallUnmarked(wallFixture(13)), sz[0], sz[1])
			t.Logf("%s %dx%d, 13 conversations:\n%s", pname, sz[0], sz[1], wallPlainFrame(rows))
		}
		v = wallUnmarked(base)
		v.tiles, v.filter = nil, "xyz"
		rows, _ = renderWall(pal, v, 120, 40)
		t.Logf("%s 120x40, a filter matching nothing:\n%s", pname, wallPlainFrame(rows))
	}
}

// wallFrameVariants is the fixture in every state a pointer can put it in:
// each target of the resting frame hovered in turn, with and without picks,
// naming, and with each popover up.
func wallFrameVariants(t *testing.T, pal palette, n, w, h int, each func(name string, v wallView, rows []string, hits []wallHit)) {
	t.Helper()
	type state struct {
		name string
		v    wallView
	}
	base := wallFixture(n)
	naming := base
	naming.naming, naming.name = true, "harbor"
	naming.choices = teamHueChoices(wallViewHues(base), teamReservedFrom(darkRamp), 6)
	members := wallUnmarked(base)
	members.pop = wallPop{kind: wallPopMembers, x: w / 2, y0: 5, y1: 6, targets: []string{"k0", "k1"}, cursor: -1}
	settings := wallUnmarked(base)
	settings.pop = wallPop{kind: wallPopSettings, x: 10, y0: 1, y1: 2, team: wallTestIDs[1], name: "infra", choices: naming.choices}
	confirm := settings
	confirm.pop.confirm = true
	help := wallUnmarked(base)
	help.help = true
	scrolled := help
	scrolled.helpTop = 99
	for _, s := range []state{
		{"rest", wallUnmarked(base)}, {"picked", base}, {"naming", naming},
		{"members", members}, {"settings", settings}, {"confirm", confirm},
		{"help", help}, {"help scrolled", scrolled},
	} {
		_, rest := renderWall(pal, s.v, w, h)
		refs := []wallHitRef{{}}
		for _, hit := range rest {
			refs = append(refs, hit.ref())
		}
		for _, ref := range refs {
			v := s.v
			v.hover = ref
			rows, hits := renderWall(pal, v, w, h)
			each(fmt.Sprintf("n%d/%dx%d/%s/hover=%+v", n, w, h, s.name, ref), v, rows, hits)
		}
	}
}

// EVERY TARGET SITS ON WHAT IT DRAWS, and no two targets share a cell, in every
// state the pointer can put the frame in.
func TestWallClickHitsSitOnTheirLabels(t *testing.T) {
	labels := map[wallAct]string{
		wallActBack: "Back", wallActNewTeam: "New team", wallActFilter: "Filter", wallActNext: "needs you",
		wallActColsLess: "−", wallActColsMore: "+", wallActMakeTeam: "Make team", wallActAddTo: "Add to",
		wallActCloseViews: "Close views", wallActClear: "Clear", wallActSave: "Create", wallActCancel: "Cancel",
		wallActFilterClear: "Clear", wallActShuffle: "Shuffle", wallActHelp: "Help",
	}
	for pname, pal := range wallTestPalettes() {
		for _, sz := range wallTestSizes {
			wallFrameVariants(t, pal, 6, sz[0], sz[1], func(name string, v wallView, rows []string, hits []wallHit) {
				name = pname + "/" + name
				wallCheckRows(t, name, rows, sz[0], sz[1])
				wallCheckHits(t, name, hits, sz[0], sz[1])
				for _, hit := range hits {
					drawn := ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x1))
					if strings.TrimSpace(drawn) == "" && hit.kind != wallHitTile {
						t.Fatalf("%s: a %d target over blank cells %+v", name, hit.kind, hit)
					}
					if hit.kind == wallHitAction && hit.y1-hit.y0 == 1 && hit.arg != int(wallActFilter) {
						want := labels[wallAct(hit.arg)]
						if pal.ascii && want == "−" {
							want = "-"
						}
						if !strings.Contains(drawn, want) {
							t.Fatalf("%s: button %d drawn as %q, want %q", name, hit.arg, drawn, want)
						}
					}
				}
			})
		}
	}
}

// THE TRAY IS UP IF AND ONLY IF SOMETHING IS PICKED, and the toolbar under it
// keeps its buttons either way.
func TestWallBarTrayIffMarked(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for _, sz := range wallTestSizes {
		for _, marks := range []bool{false, true} {
			v := wallFixture(6)
			if !marks {
				v = wallUnmarked(v)
			}
			rows, hits := renderWall(pal, v, sz[0], sz[1])
			frame := wallPlainFrame(rows)
			if tray := strings.Contains(frame, "2 selected"); tray != marks {
				t.Fatalf("%dx%d marks=%v: tray drawn=%v\n%s", sz[0], sz[1], marks, tray, frame)
			}
			makes, adds := false, false
			for _, hit := range hits {
				if hit.kind == wallHitAction && hit.arg == int(wallActMakeTeam) {
					makes = true
				}
				if hit.kind == wallHitAction && hit.arg == int(wallActAddTo) {
					adds = true
				}
			}
			if makes != marks || (marks && !adds) {
				t.Fatalf("%dx%d marks=%v: make team=%v add to=%v\n%s", sz[0], sz[1], marks, makes, adds, frame)
			}
			if bar := ansi.Strip(rows[len(rows)-1]); !strings.Contains(bar, "Back esc") {
				t.Fatalf("%dx%d: the toolbar lost its way back: %q", sz[0], sz[1], bar)
			}
		}
	}
}

// THE TOOLBAR NEVER WRAPS AND DROPS WHOLE BUTTONS, keeping the way back longest.
func TestWallBarDropsWholeButtons(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallFixture(6)
	for w := 20; w <= 200; w += 7 {
		row, hits := wallBar(pal, v, w, 0)
		if ansi.StringWidth(row) > w {
			t.Fatalf("width %d: bar is %d cells", w, ansi.StringWidth(row))
		}
		for _, hit := range hits {
			if hit.x1 > w {
				t.Fatalf("width %d: a button past the edge %+v", w, hit)
			}
		}
		if !strings.Contains(ansi.Strip(row), "Back esc") {
			t.Fatalf("width %d: the way back left before the rest: %q", w, ansi.Strip(row))
		}
		if w >= 80 && !strings.Contains(ansi.Strip(row), "Columns") {
			t.Fatalf("width %d: no room was found for the columns: %q", w, ansi.Strip(row))
		}
	}
}

// A TILE'S ACTIONS ARE WORDS ON ITS BOTTOM BORDER, drawn under the pointer or
// the focus and nowhere else, into cells that depend on the width alone:
// hovering a tile moves nothing in it, and its top border carries no control.
func TestWallTileActionRowReservesCells(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, sz := range wallTestSizes {
			v := wallUnmarked(wallFixture(6))
			v.focus = 0
			_, tileW, tileH := wallGrid(len(v.tiles), sz[0], sz[1], 0)
			g := wallGlyphsFor(pal.ascii)
			k := wallKeysFor(pal.ascii)
			name := fmt.Sprintf("%s %dx%d", pname, sz[0], sz[1])
			for _, i := range []int{1, 3, 5} { // waiting, no teams, one team
				rest := wallPaintTile(pal, g, v, v.tiles[i], i, false, tileW, tileH, wallGridTop)
				hv := v
				hv.hover = wallHitRef{kind: wallHitTile, arg: i}
				hover := wallPaintTile(pal, g, hv, v.tiles[i], i, false, tileW, tileH, wallGridTop)
				if len(rest) != tileH || len(hover) != tileH {
					t.Fatalf("%s: %d rows at rest, %d hovered, tile is %d", name, len(rest), len(hover), tileH)
				}
				for y := range rest {
					if ansi.StringWidth(rest[y]) != tileW || ansi.StringWidth(hover[y]) != tileW {
						t.Fatalf("%s row %d: %d then %d cells, tile is %d", name, y,
							ansi.StringWidth(rest[y]), ansi.StringWidth(hover[y]), tileW)
					}
				}
				// The top border is the same row at rest and hovered: dots and a
				// title, and no box outside the selection mode.
				top, topHover := ansi.Strip(rest[0]), ansi.Strip(hover[0])
				if top != topHover {
					t.Fatalf("%s: the top border changed under the pointer:\n%q\n%q", name, top, topHover)
				}
				if strings.Contains(ansi.Cut(top, 0, 5), wallSelGlyph(pal.ascii, false)) || strings.Contains(top, "open") {
					t.Fatalf("%s: the top border carries a control: %q", name, top)
				}
				// Every row but the bottom one is the same; the bottom one is the
				// action row, its buttons where the layout put them.
				for y := 1; y < tileH-1; y++ {
					if ansi.Strip(rest[y]) != ansi.Strip(hover[y]) {
						t.Fatalf("%s row %d moved under the pointer:\n%q\n%q", name, y, ansi.Strip(rest[y]), ansi.Strip(hover[y]))
					}
				}
				bottom, bottomHover := ansi.Strip(rest[tileH-1]), ansi.Strip(hover[tileH-1])
				if strings.Contains(bottom, "Close") {
					t.Fatalf("%s: the action row is drawn at rest: %q", name, bottom)
				}
				acts := wallTileActs(pal.ascii, v.tiles[i], tileW)
				if len(acts) == 0 || acts[0].kind != wallHitOpen {
					t.Fatalf("%s: tile %d keeps no Open: %+v", name, i, acts)
				}
				first := "Open " + k.enter
				if v.tiles[i].signal == tabNeedsPerson {
					first = "Answer " + k.enter
				}
				if !strings.Contains(bottomHover, first) {
					t.Fatalf("%s: tile %d's action row does not start with %q: %q", name, i, first, bottomHover)
				}
				for _, act := range acts {
					got := ansi.Strip(ansi.Cut(hover[tileH-1], act.x+1, act.x+1+ansi.StringWidth(act.btn.label)))
					if got != act.btn.label {
						t.Fatalf("%s: %q drawn as %q at %d: %q", name, act.btn.label, got, act.x, bottomHover)
					}
				}
			}
			// The focused tile shows its row with no pointer at all.
			focused := ansi.Strip(wallPaintTile(pal, g, v, v.tiles[0], 0, true, tileW, tileH, wallGridTop)[tileH-1])
			if !strings.Contains(focused, "Open "+k.enter) {
				t.Fatalf("%s: the focused tile hides its actions: %q", name, focused)
			}
		}
	}
}

// A NARROW TILE LOSES ITS BUTTONS FROM THE RIGHT AND KEEPS OPEN, and the row
// never runs past the tile.
func TestWallTileActionRowDropsFromTheRight(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		v := wallUnmarked(wallFixture(6))
		prev := 5
		for w := 60; w >= 12; w-- {
			acts := wallTileActs(pal.ascii, v.tiles[0], w)
			if len(acts) > prev {
				t.Fatalf("%s width %d: %d buttons, more than the %d at a wider tile", pname, w, len(acts), prev)
			}
			prev = len(acts)
			if len(acts) > 0 && acts[0].kind != wallHitOpen {
				t.Fatalf("%s width %d: Open was dropped before %+v", pname, w, acts)
			}
			if n := len(acts); n > 0 && acts[n-1].x+wallButtonW(acts[n-1].btn)+2 > w {
				t.Fatalf("%s width %d: the last button runs into the corner", pname, w)
			}
			hv := v
			hv.hover = wallHitRef{kind: wallHitTile, arg: 0}
			rows := wallPaintTile(pal, wallGlyphsFor(pal.ascii), hv, v.tiles[0], 0, false, w, 12, wallGridTop)
			if got := ansi.StringWidth(rows[len(rows)-1]); got != w {
				t.Fatalf("%s width %d: the action row is %d cells", pname, w, got)
			}
		}
		if n := len(wallTileActs(pal.ascii, v.tiles[0], 200)); n != 4 {
			t.Fatalf("%s: a wide tile carries %d buttons, want 4", pname, n)
		}
	}
}

// A TILE CARRIES ITS TEAMS AS DOTS: up to three, then a count, and no cells at
// all for a tile in none. Colourless, a dot is the team's initial.
func TestWallTileDotsName(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallUnmarked(wallFixture(6))
	g := wallGlyphsFor(false)
	_, tileW, tileH := wallGrid(6, 120, 40, 0)
	top := func(p palette, t wallTile, i int) string {
		return ansi.Strip(wallPaintTile(p, g, v, t, i, false, tileW, tileH, wallGridTop)[0])
	}
	if got := top(pal, v.tiles[2], 2); !strings.Contains(got, "●●● ship the port") {
		t.Fatalf("three teams: %q", got)
	}
	four := v.tiles[2]
	four.teams = []string{wallTestIDs[0], wallTestIDs[1], wallTestIDs[2], wallTestIDs[0]}
	if got := top(pal, four, 2); !strings.Contains(got, "●●●+1 ship the port") {
		t.Fatalf("four teams: %q", got)
	}
	if got := top(pal, v.tiles[3], 3); strings.Contains(got, "●") || !strings.HasPrefix(got, "╭─ relay audit") {
		t.Fatalf("no teams: %q", got)
	}
	plain := newPalette(tokens.ANSI16, false)
	if got := top(plain, v.tiles[2], 2); !strings.Contains(got, "pir ship the port") {
		t.Fatalf("no colour: %q", got)
	}
}

// ONE PICK IS THE SELECTION MODE: every tile shows its box, filled or not.
func TestWallClickSelectionModeShowsEveryBox(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallFixture(4) // tile 2 is picked
	_, tileW, tileH := wallGrid(4, 180, 50, 0)
	g := wallGlyphsFor(false)
	for i, tile := range v.tiles {
		top := ansi.Strip(wallPaintTile(pal, g, v, tile, i, false, tileW, tileH, wallGridTop)[0])
		want := wallSelGlyph(false, tile.marked)
		if !strings.HasPrefix(top, "╭─ "+want) {
			t.Fatalf("tile %d in selection mode: %q, want the box %q", i, top, want)
		}
	}
	rows, hits := renderWall(pal, v, 180, 50)
	boxes := map[int]bool{}
	for _, hit := range hits {
		if hit.kind == wallHitSelect && strings.Contains(ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x1)), wallSelGlyph(false, v.tiles[hit.arg].marked)) {
			boxes[hit.arg] = true
		}
	}
	if len(boxes) != 4 {
		t.Fatalf("%d selection boxes answer the pointer, want all 4", len(boxes))
	}
	// Out of the selection mode no tile draws a box, hovered or not.
	v = wallUnmarked(v)
	v.hover = wallHitRef{kind: wallHitTile, arg: 1}
	for i, tile := range v.tiles {
		top := ansi.Strip(wallPaintTile(pal, g, v, tile, i, false, tileW, tileH, wallGridTop)[0])
		if strings.Contains(top, wallSelGlyph(false, false)) {
			t.Fatalf("tile %d draws a box out of the selection mode: %q", i, top)
		}
	}
}

// THE TEAMS ROW IS A ROW OF DOORS: All, each team and + New team; a
// segment's dot and, under the pointer, its ⋯ open its settings. The minimap
// is there only when the conversations do not all fit.
func TestWallClickTeamsAndMinimap(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for _, n := range []int{6, 13} {
		v := wallUnmarked(wallFixture(n))
		rows, hits := renderWall(pal, v, 120, 40)
		chips, menus, minis, add := map[string]bool{}, 0, 0, false
		for _, hit := range hits {
			switch hit.kind {
			case wallHitChip:
				chips[hit.id] = true
			case wallHitChipMenu:
				menus++
			case wallHitMini:
				minis++
			case wallHitAddTeam:
				add = true
			}
		}
		c, _, tileH := wallGrid(n, 120, 40, 0)
		overflow := (n+c-1)/c > wallVisibleRows(40, tileH)
		if len(chips) != 4 || !add || menus != 3 || (minis > 0) != overflow {
			t.Fatalf("n%d: chips %v, + New team %v, %d dots, %d minimap cells\n%s", n, chips, add, menus, minis, ansi.Strip(rows[1]))
		}
	}
	v := wallUnmarked(wallFixture(6))
	v.hover = wallHitRef{kind: wallHitChip, id: wallTestIDs[1]}
	rows, hits := renderWall(pal, v, 120, 40)
	if !strings.Contains(ansi.Strip(rows[1]), "infra ⋯ │") {
		t.Fatalf("the hovered segment shows no ⋯: %q", ansi.Strip(rows[1]))
	}
	tails := 0
	for _, hit := range hits {
		if hit.kind == wallHitChipMenu && hit.id == wallTestIDs[1] {
			tails++
		}
	}
	if tails != 2 || strings.Count(ansi.Strip(rows[1]), "⋯") != 1 {
		t.Fatalf("the hovered segment: %d settings targets, row %q", tails, ansi.Strip(rows[1]))
	}
}

// THE TEAMS POPOVER SAYS, PER TEAM, WHETHER ITS TARGETS ARE IN IT: all, none,
// or some.
func TestWallPopoverBoxesAreTriState(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallUnmarked(wallFixture(6))
	// k0 is in port and infra, k1 in port alone.
	v.pop = wallPop{kind: wallPopMembers, x: 60, y0: 4, y1: 5, targets: []string{"k0", "k1"}, cursor: -1}
	rows, hits := renderWall(pal, v, 120, 40)
	frame := wallPlainFrame(rows)
	for _, want := range []string{"─ Teams ─", "☑ ● port", "▣ ● infra", "☐ ● research", "+ New team…"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the popover lacks %q\n%s", want, frame)
		}
	}
	rowsHit := 0
	for _, hit := range hits {
		if hit.kind == wallHitPopRow {
			rowsHit++
		}
	}
	if rowsHit != 4 {
		t.Fatalf("%d popover rows answer the pointer, want 4", rowsHit)
	}
}

func TestTeamFreshNameAvoidsTakenNames(t *testing.T) {
	first := func(int) int { return 0 }
	shared := []chatTab{{key: "a", where: "/w/harbor-app"}, {key: "b", where: "/w/harbor-app"}}
	if got := teamFreshName(shared, nil, "", first); got != "harbor-app" {
		t.Fatalf("shared folder: %q", got)
	}
	if got := teamFreshName(shared, []string{"Harbor-App"}, "", first); got != "harbor" {
		t.Fatalf("a taken folder name should fall to a word: %q", got)
	}
	apart := []chatTab{{key: "a", where: "/w/one"}, {key: "b", where: "/w/two"}}
	if got := teamFreshName(apart, []string{"harbor", "orbit"}, "", first); got != "lumen" {
		t.Fatalf("a word already a team's name was offered: %q", got)
	}
	if got := teamFreshName(shared, nil, "harbor-app", first); got != "harbor" {
		t.Fatalf("a shuffle should leave the folder and the current name: %q", got)
	}
	if got := teamFreshName(apart, teamWords, "", first); got != "harbor2" {
		t.Fatalf("every word taken: %q", got)
	}
}

func TestTeamAddAndRemoveKeepOrderAndSave(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithProfile(dir, nil)
	i, err := a.teamMake("port", []chatTab{{key: "k1", word: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.teamAdd(i, []chatTab{{key: "k1"}, {key: "k2", word: "two"}, {key: "k3", word: "three"}}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	got, _ := loadTeams(dir, nil)
	if len(got) != 1 || len(got[0].Members) != 3 || got[0].Members[1].Key != "k2" {
		t.Fatalf("after add: %+v", got)
	}
	if err := a.teamRemove(i, []string{"k1", "k3"}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	got, _ = loadTeams(dir, nil)
	if len(got[0].Members) != 1 || got[0].Members[0].Key != "k2" {
		t.Fatalf("after remove: %+v", got)
	}
}

// wallAnswerHit is where tile 1's Answer button landed: the open target that
// is not on the tile's top border.
func wallAnswerHit(t *testing.T, pal palette, v wallView, w, h int) wallHit {
	t.Helper()
	rows, hits := renderWall(pal, v, w, h)
	for _, hit := range hits {
		if hit.kind == wallHitOpen && hit.arg == 1 && strings.Contains(ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x1)), "Answer") {
			return hit
		}
	}
	t.Fatalf("no Answer button on the waiting tile:\n%s", wallPlainFrame(rows))
	return wallHit{}
}

// EVERY TILE KEEPS ONE RHYTHM: its state line, exactly one blank row, then the
// body from the top, whatever the body's length.
func TestWallTileBodyHangsFromTheTop(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, sz := range wallTestSizes {
			v := wallUnmarked(wallFixture(6))
			// Tile 3 has a tail far shorter than its room.
			v.tiles[3].lines = v.tiles[3].lines[:1]
			_, tileW, tileH := wallGrid(len(v.tiles), sz[0], sz[1], 0)
			g := wallGlyphsFor(pal.ascii)
			padY, meta, _ := wallTileRoom(tileH)
			if !meta {
				continue
			}
			for i, tile := range v.tiles {
				rows := wallPaintTile(pal, g, v, tile, i, false, tileW, tileH, wallGridTop)
				inner := func(y int) string { return strings.TrimSpace(ansi.Strip(ansi.Cut(rows[y], 1, tileW-1))) }
				state, gap, first := 1+padY, 2+padY, 3+padY
				if inner(gap) != "" || inner(first) == "" {
					t.Fatalf("%s %dx%d tile %d: state %q, then %q, then %q", pname, sz[0], sz[1], i, inner(state), inner(gap), inner(first))
				}
			}
		}
	}
}

// A WAITING TILE ENDS IN A BUTTON: the question, a blank row, and Answer,
// which is the tile's open and sits on its own label.
func TestWallNeedsYouTileAnswers(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		v := wallUnmarked(wallFixture(6))
		hit := wallAnswerHit(t, pal, v, 120, 40)
		rows, _ := renderWall(pal, v, 120, 40)
		above := strings.TrimSpace(ansi.Strip(ansi.Cut(rows[hit.y0-1], hit.x0, hit.x0+50)))
		question := ansi.Strip(rows[hit.y0-2])
		if above != "" || !strings.Contains(question, "needs your ok") {
			t.Fatalf("%s: over the button %q, then %q", pname, question, above)
		}
		// The label starts on the text's column, its ground in the padding.
		if got := ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0+1, hit.x0+7)); got != "Answer" {
			t.Fatalf("%s: the label is %q one cell into the button", pname, got)
		}
		if col := ansi.StringWidth(question[:strings.Index(question, "needs")]); col != hit.x0+1 {
			t.Fatalf("%s: the question and the button's label do not share a column: %q at %d", pname, question, hit.x0+1)
		}
		// With the pointer's row known, the Answer lights and the corner's
		// open does not, though both answer to the same target.
		v.hover = hit.ref()
		v.pointerOn, v.pointerY = true, hit.y0
		lit, _ := renderWall(pal, v, 120, 40)
		if lit[hit.y0] == rows[hit.y0] {
			t.Fatalf("%s: the hovered Answer is drawn as at rest", pname)
		}
	}
}

// THE TOOLBAR SAYS WHAT THE CONTROL UNDER THE POINTER DOES, and says nothing
// with the pointer on no control.
func TestWallToolbarExplainsTheHover(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallUnmarked(wallFixture(6))
	bar := func(v wallView) string {
		rows, _ := renderWall(pal, v, 120, 40)
		return ansi.Strip(rows[len(rows)-1])
	}
	rest := bar(v)
	for ref, want := range map[wallHitRef]string{
		{kind: wallHitTeams, arg: 0}:                     "Add this conversation to teams · m",
		{kind: wallHitOpen, arg: 0}:                      "Open conversation · enter",
		{kind: wallHitClose, arg: 0}:                     "Close this view; the work keeps running · x",
		{kind: wallHitSelect, arg: 0}:                    "Select for a team · space",
		{kind: wallHitChip, id: wallTestIDs[1]}:          "infra · 2 open here · 3 members",
		{kind: wallHitAction, arg: int(wallActColsMore)}: "More columns · +",
	} {
		hv := v
		hv.hover = ref
		got := bar(hv)
		if !strings.Contains(got, want) {
			t.Fatalf("hover %+v: toolbar %q, want %q", ref, got, want)
		}
		// The status line moves no button.
		col := func(s string) int { return ansi.StringWidth(s[:strings.Index(s, "Filter /")]) }
		if col(got) != col(rest) {
			t.Fatalf("hover %+v moved the toolbar's buttons:\n%q\n%q", ref, rest, got)
		}
	}
	if strings.Contains(rest, " · ") {
		t.Fatalf("the toolbar explains a hover with none: %q", rest)
	}
}

// THE TEAMS ROW IS ONE SEGMENTED CONTROL: every separator has one blank cell
// either side, and + New team is its last segment.
func TestWallTeamsRowIsEvenlyPadded(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, hover := range []wallHitRef{{}, {kind: wallHitChip, id: wallTestIDs[1]}} {
			v := wallUnmarked(wallFixture(6))
			v.hover = hover
			rows, _ := renderWall(pal, v, 120, 40)
			row := ansi.Strip(rows[1])
			sep := "│"
			if pal.ascii {
				sep = "|"
			}
			parts := strings.Split(row, sep)
			if len(parts) != 5 {
				t.Fatalf("%s: %d segments in %q", pname, len(parts), row)
			}
			for i, p := range parts {
				if i > 0 && (!strings.HasPrefix(p, " ") || strings.HasPrefix(p, "  ")) {
					t.Fatalf("%s: segment %d is %q", pname, i, p)
				}
				if i < len(parts)-1 && (!strings.HasSuffix(p, " ") || strings.HasSuffix(p, "  ")) {
					t.Fatalf("%s: segment %d is %q", pname, i, p)
				}
			}
			if !strings.HasPrefix(parts[4], " + New team ") {
				t.Fatalf("%s: + New team is not the last segment: %q", pname, row)
			}
		}
	}
}

// THE HEAD AND THE FOOT END WHERE THE GRID ENDS, and a count growing a digit
// moves nothing to its right.
func TestWallChromeAlignsWithTheGrid(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for _, sz := range wallTestSizes {
		var ends []int
		for _, n := range []int{6, 13} {
			v := wallUnmarked(wallFixture(n))
			rows, _ := renderWall(pal, v, sz[0], sz[1])
			c, tileW, _ := wallGrid(n, sz[0], sz[1], 0)
			edge := wallMargin + c*tileW + (c-1)*wallGutter
			title := strings.TrimRight(ansi.Strip(rows[0]), " ")
			bar := strings.TrimRight(ansi.Strip(rows[len(rows)-1]), " ")
			if ansi.StringWidth(title) != edge || ansi.StringWidth(bar) != edge {
				t.Fatalf("%dx%d n%d: title ends at %d, toolbar at %d, the grid at %d", sz[0], sz[1], n, ansi.StringWidth(title), ansi.StringWidth(bar), edge)
			}
			ends = append(ends, ansi.StringWidth(title))
			// The foot mirrors the head: a blank row over a rule over the toolbar.
			if strings.TrimSpace(ansi.Strip(rows[len(rows)-3])) != "" || !strings.HasPrefix(ansi.Strip(rows[len(rows)-2]), "───") {
				t.Fatalf("%dx%d n%d: the foot is not a blank row and a rule:\n%s", sz[0], sz[1], n, wallPlainFrame(rows))
			}
		}
		if ends[0] != ends[1] {
			t.Fatalf("%dx%d: the counts end at %d, then %d", sz[0], sz[1], ends[0], ends[1])
		}
	}
}

// A LONG TITLE IS CUT WITH AN ELLIPSIS AND KEEPS TWO RULE CELLS BEFORE THE
// CORNER, at rest, hovered and in the selection mode alike.
func TestWallLongTitleKeepsItsDistance(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	g := wallGlyphsFor(false)
	v := wallUnmarked(wallFixture(6))
	v.tiles[3].name = strings.Repeat("a very long title ", 8)
	for _, sz := range wallTestSizes {
		_, tileW, tileH := wallGrid(6, sz[0], sz[1], 0)
		for _, state := range []string{"rest", "hover", "picking"} {
			hv := v
			switch state {
			case "hover":
				hv.hover = wallHitRef{kind: wallHitTile, arg: 3}
			case "picking":
				hv.tiles = append([]wallTile(nil), v.tiles...)
				hv.tiles[0].marked = true
			}
			top := ansi.Strip(wallPaintTile(pal, g, hv, hv.tiles[3], 3, false, tileW, tileH, wallGridTop)[0])
			cut := strings.Index(top, "…")
			if cut < 0 {
				t.Fatalf("%dx%d %s: the long title is not cut: %q", sz[0], sz[1], state, top)
			}
			runes := []rune(top)
			at := len([]rune(top[:cut]))
			gap := runes[at+2 : len(runes)-1]
			if len(gap) < wallTitleGap || strings.Trim(string(gap), "─") != "" {
				t.Fatalf("%dx%d %s: %d rule cells between the title and the corner: %q", sz[0], sz[1], state, len(gap), top)
			}
		}
	}
}

// A POPOVER STAYS INSIDE THE FRAME AND OFF THE FOOT, and flips over its
// control when there is no room under it.
func TestWallPopoverStaysInTheFrame(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for _, sz := range wallTestSizes {
		w, h := sz[0], sz[1]
		for _, at := range []wallPop{{x: w - 3, y0: 4, y1: 5}, {x: 0, y0: h - 5, y1: h - 4}} {
			v := wallUnmarked(wallFixture(6))
			at.kind, at.targets, at.cursor = wallPopMembers, []string{"k0"}, -1
			v.pop = at
			card := wallPopCard(pal, wallGlyphsFor(false), v, w, h)
			if len(card.rows) == 0 {
				t.Fatalf("%dx%d: no popover", w, h)
			}
			bottom := card.y + len(card.rows)
			if card.x < wallMargin || card.x+card.w > w-wallMargin || bottom > h-wallFootRows+1 {
				t.Fatalf("%dx%d anchor %+v: popover at %d,%d %dx%d", w, h, at, card.x, card.y, card.w, len(card.rows))
			}
			if at.y1 > h/2 && bottom > at.y0 {
				t.Fatalf("%dx%d: a popover with no room below did not flip over its control: rows %d..%d, control on %d", w, h, card.y, bottom, at.y0)
			}
			// One blank row and two blank cells inside the border.
			if got := ansi.Strip(card.rows[1]); strings.Trim(got, "│ ") != "" {
				t.Fatalf("%dx%d: the popover's first inner row is %q", w, h, got)
			}
			if got := ansi.Strip(card.rows[2]); !strings.HasPrefix(got, "│  ") {
				t.Fatalf("%dx%d: the popover's first line is %q", w, h, got)
			}
		}
	}
}

// A NARROWED GRID WITH NOTHING IN IT KEEPS ITS HEAD AND SAYS SO, beside the
// one button that undoes the narrowing.
func TestWallNarrowedToNothing(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, sz := range wallTestSizes {
			v := wallUnmarked(wallFixture(6))
			v.tiles, v.filter = nil, "xyz"
			rows, hits := renderWall(pal, v, sz[0], sz[1])
			name := fmt.Sprintf("%s %dx%d", pname, sz[0], sz[1])
			wallCheckRows(t, name, rows, sz[0], sz[1])
			wallCheckHits(t, name, hits, sz[0], sz[1])
			frame := wallPlainFrame(rows)
			if !strings.Contains(frame, `No conversations match "xyz"`) || !strings.Contains(ansi.Strip(rows[1]), "/ xyz") {
				t.Fatalf("%s: the empty filter:\n%s", name, frame)
			}
			clears := 0
			for _, hit := range hits {
				if hit.kind == wallHitAction && hit.arg == int(wallActFilterClear) {
					clears++
				}
			}
			if clears != 2 {
				t.Fatalf("%s: %d Clear buttons, want the filter row's and the message's", name, clears)
			}
			v.filter = ""
			rows, _ = renderWall(pal, v, sz[0], sz[1])
			if !strings.Contains(wallPlainFrame(rows), "No open conversations in port") {
				t.Fatalf("%s: the empty team:\n%s", name, wallPlainFrame(rows))
			}
		}
	}
}

// THE WALL IS WHAT IS OPEN IN THIS WINDOW, and a team's members that are not
// open here are one quiet word button on the title, never tiles: `2 more in
// port · Open them`, with its key on the hint line, and no mark at all when
// every member is open (the emptiness law).
func TestWallTitleOffersTheTeamsMembersNotOpenHere(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	v := wallUnmarked(wallFixture(6))
	v.away = 2
	rows, hits := renderWall(pal, v, 180, 50)
	title := ansi.Strip(rows[0])
	for _, want := range []string{"open in this window · in port", "2 more in port · Open them", "6 open"} {
		if !strings.Contains(title, want) {
			t.Fatalf("title %q lacks %q", title, want)
		}
	}
	var hit *wallHit
	for i := range hits {
		if hits[i].kind == wallHitAction && hits[i].arg == int(wallActResume) {
			hit = &hits[i]
		}
	}
	if hit == nil || hit.y0 != 0 {
		t.Fatalf("Open them is not a target on the title: %+v", hits)
	}
	if got := ansi.Strip(ansi.Cut(rows[0], hit.x0, hit.x1)); strings.TrimSpace(got) != "2 more in port · Open them" {
		t.Fatalf("the target covers %q", got)
	}
	hv := v
	hv.hover = hit.ref()
	hrows, _ := renderWall(pal, hv, 180, 50)
	if hrows[0] == rows[0] {
		t.Fatal("the button takes no ground under the pointer")
	}
	if bar := ansi.Strip(hrows[len(hrows)-1]); !strings.Contains(bar, "Resume the 2 conversations of port not open here; nothing in front moves · r") {
		t.Fatalf("hint line %q", bar)
	}
	v.away = 0
	rows, _ = renderWall(pal, v, 180, 50)
	if title := ansi.Strip(rows[0]); strings.Contains(title, "more") || strings.Contains(title, wallResumeWord) {
		t.Fatalf("every member open, yet the title offers more: %q", title)
	}
	// Narrow, the name goes from the button before the button goes.
	v.away = 2
	rows, _ = renderWall(pal, v, 80, 30)
	if title := ansi.Strip(rows[0]); strings.Contains(title, "in port · Open") {
		t.Fatalf("80 wide keeps the long button: %q", title)
	}
}
