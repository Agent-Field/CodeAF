package tui3

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── NESTING ON THE TEAMS PAGE (rulings c-12, c-13) ──────────────────────────

// nestLab is the teams page with three teams: harbor at the top with orbit
// inside it, and dock at the top holding one conversation this window does not
// have open. The depth limit is two, from Settings, so orbit (two deep) can
// take nothing more. dock is selected.
func nestLab(t *testing.T) (a *app, harbor, orbit, dock string) {
	t.Helper()
	a, harbor, orbit = teamsPlaceLabIDs(t)
	dock = newTeamID()
	if err := a.teamEdit(func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: dock, Name: "dock", Members: []teamstore.Member{
			{Key: a.convKey("/tmp/lab/crane.jsonl"), File: "/tmp/lab/crane.jsonl", Where: "/tmp/lab", Word: "crane work", Handle: "crane"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The limit is the profile's own `teams.depth_limit`, so every read the
	// page makes through the seam answers the same two.
	if err := os.WriteFile(config.BudgetConfigPath(a.profileDir), []byte(`{"teams.depth_limit": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.tp.defaults, a.tp.defaultsOK = teamstore.DefaultsAt(a.profileDir), true
	drive(t, a, runCmd(a.teamsSelect(dock))...)
	a.frame()
	return a, harbor, orbit, dock
}

// nestRow is the frame row that holds want, -1 for none.
//
// The head is skipped: the strip of chats is on every page, and its team chip
// reads `● harbor ▾` over the rail's own `● harbor`.
func nestRow(a *app, want string) int {
	for y, l := range strings.Split(teamsFrameText(a), "\n") {
		if y >= placeHeadRows && strings.Contains(l, want) {
			return y
		}
	}
	return -1
}

// railPoint is a cell on team id's rail row, two cells in.
func railPoint(t *testing.T, a *app, id string) (int, int) {
	t.Helper()
	tg := teamsTargetOf(t, a, teamsActSelect, id)
	return tg.x0 + 2, tg.y
}

// press, drag and release are the pointer's three moves with the left button.
func nestPress(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func dragTo(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func release(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// parentOf is team id's parent as the window holds it.
func parentOf(a *app, id string) string {
	t, _ := a.teamByID(id)
	return t.Parent
}

// THE PICKER LISTS EVERY TEAM AND DIMS THE ONES THAT CANNOT TAKE THE MOVE with
// the reason in its foot and the hint: the team itself, a team under it, and a
// team too deep for the limit (`orbit is 2 levels deep · limit 2 · Settings`).
// Typing filters, and the tree keeps its indent.
func TestMoveIntoPickerDimsInvalidTargetsWithTheReason(t *testing.T) {
	a, harbor, orbit, dock := nestLab(t)
	drive(t, a, key("m"))
	if !a.tmove.on || len(a.tmove.ids) != 1 || a.tmove.ids[0] != dock {
		t.Fatalf("m did not open the picker for dock: %+v", a.tmove)
	}
	rows := a.teamMoveRows()
	want := map[string]string{
		teamMoveTop: "dock is at the top level already",
		harbor:      "",
		orbit:       "orbit is 2 levels deep · limit 2 · Settings",
		dock:        "a team cannot go inside itself",
	}
	if len(rows) != 4 {
		t.Fatalf("the picker has %d rows, want 4: %+v", len(rows), rows)
	}
	for _, r := range rows {
		why, ok := want[r.parent]
		if !ok {
			t.Fatalf("an unexpected row %q", r.name)
		}
		if r.ok != (why == "") || r.why != why {
			t.Fatalf("row %s: ok %v why %q, want why %q", r.name, r.ok, r.why, why)
		}
	}
	// The keyboard starts on the first row that can take the move.
	if a.tmove.cursor != harbor {
		t.Fatalf("the cursor starts on %q", a.tmove.cursor)
	}
	drive(t, a, key("down"))
	text := teamsFrameText(a)
	if !strings.Contains(text, "Move dock into") || !strings.Contains(text, "orbit is 2 levels deep · limit 2 · Settings") {
		t.Fatalf("the dimmed row's reason is not in the picker's foot:\n%s", text)
	}
	if hint := a.teamMoveHint(); !strings.Contains(hint, "limit 2") {
		t.Fatalf("the hint line does not say why: %q", hint)
	}
	// enter on a dimmed row moves nothing and keeps the picker up.
	drive(t, a, key("enter"))
	if !a.tmove.on || parentOf(a, dock) != "" {
		t.Fatal("enter on a blocked row moved the team or closed the picker")
	}
	// The indent survives: orbit stands deeper than harbor.
	hy, oy := nestRow(a, "● harbor"), nestRow(a, "● orbit")
	lines := strings.Split(teamsFrameText(a), "\n")
	if hy < 0 || oy < 0 || strings.Index(lines[oy], "●") <= strings.Index(lines[hy], "●") {
		t.Fatalf("the picker's tree lost its indent:\n%s", text)
	}
	// Typing filters.
	drive(t, a, key("o"), key("r"), key("b"))
	if rows := a.teamMoveRows(); len(rows) != 1 || rows[0].parent != orbit {
		t.Fatalf("the filter `orb` left %+v", rows)
	}
	drive(t, a, key("esc"))
	if a.tmove.on {
		t.Fatal("esc did not close the picker")
	}
}

// A MOVE THAT CHANGES NOTHING A PERSON DECIDES ON IS MADE AT ONCE, and Undo
// puts it back.
func TestMoveIntoAQuietTeamIsInstantWithUndo(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	drive(t, a, key("m"), key("enter"))
	if a.tmove.on || parentOf(a, dock) != harbor {
		t.Fatalf("dock is under %q, the picker on %v", parentOf(a, dock), a.tmove.on)
	}
	if len(a.tmove.pend.ids) > 0 {
		t.Fatal("a quiet move asked first")
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "dock is in harbor now") || !strings.Contains(text, "Undo") {
		t.Fatalf("the move offers no Undo:\n%s", text)
	}
	drive(t, a, key("u"))
	if parentOf(a, dock) != "" {
		t.Fatalf("Undo left dock under %q", parentOf(a, dock))
	}
}

// A MOVE THAT CHANGES WHO DECIDES ASKS ONE LINE FIRST: with harbor managed and
// capped, dock into harbor says whose manager and whose pool, with Move and
// Cancel. Cancel leaves it; Move moves it; Undo is offered after it too.
func TestMoveConsequenceLineOnlyWhenSomethingChanges(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	ten := 10.0
	three := 3.0
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(harbor, teamstore.Member{Key: "boss-key", Handle: "boss", Word: "the boss"}); err != nil {
			return err
		}
		if err := f.SetManager(harbor, "boss-key"); err != nil {
			return err
		}
		if err := f.SetSettings(dock, func(s *teamstore.Settings) { s.CapUSDDay = &three }); err != nil {
			return err
		}
		return f.SetSettings(harbor, func(s *teamstore.Settings) { s.CapUSDDay = &ten })
	}); err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamMoveAsk([]string{dock}, harbor, teamMoveFromPage))...)
	p := a.tmove.pend
	if len(p.ids) == 0 {
		t.Fatal("a move under a manager and a cap did not ask")
	}
	for _, want := range []string{"dock will report to harbor's manager", "its $3/day becomes part of harbor's $10 pool"} {
		if !strings.Contains(p.words, want) {
			t.Fatalf("the line %q lacks %q", p.words, want)
		}
	}
	if parentOf(a, dock) != "" {
		t.Fatal("the move was made before the person said Move")
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "report to harbor's manager") || !strings.Contains(text, "Move") || !strings.Contains(text, "Cancel") {
		t.Fatalf("the consequence line is not on the pane:\n%s", text)
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActMoveNo, "")))...)
	if parentOf(a, dock) != "" || len(a.tmove.pend.ids) > 0 {
		t.Fatal("Cancel moved the team or left the line up")
	}
	drive(t, a, runCmd(a.teamMoveAsk([]string{dock}, harbor, teamMoveFromPage))...)
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActMoveYes, "")))...)
	if parentOf(a, dock) != harbor {
		t.Fatal("Move did not move the team")
	}
	if !a.teamMoveUndoing() || !strings.Contains(teamsFrameText(a), "Undo") {
		t.Fatal("a confirmed move offers no Undo")
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActUndo, "")))...)
	if parentOf(a, dock) != "" {
		t.Fatal("Undo did not put the confirmed move back")
	}
}

// A DRAG STARTS ONLY AFTER TWO CELLS OF HELD MOVEMENT: a one-cell wobble and a
// release is a click that selects; two cells onto harbor is a drag that says
// `Drop to move dock into harbor`, grounds harbor, and moves it on release.
func TestDragStartsAfterTwoCellsAndDropsOnAValidTeam(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	dx, dy := railPoint(t, a, dock)
	drive(t, a, nestPress(dx, dy), dragTo(dx+1, dy))
	if a.tdrag.on {
		t.Fatal("a one-cell wobble started a drag")
	}
	drive(t, a, release(dx+1, dy))
	if a.tdrag.press || parentOf(a, dock) != "" || a.tp.sel != dock {
		t.Fatalf("a click moved the team or did not select it (sel %q)", a.tp.sel)
	}
	hx, hy := railPoint(t, a, harbor)
	drive(t, a, nestPress(dx, dy), dragTo(dx, dy-1), dragTo(hx, hy))
	if !a.tdrag.on {
		t.Fatal("two cells of held movement did not start a drag")
	}
	if hint := a.teamDragHint(); !strings.HasPrefix(hint, "Drop to move dock into harbor") {
		t.Fatalf("the drag's hint: %q", hint)
	}
	if !a.teamDropLit(harbor) {
		t.Fatal("harbor is not grounded as the drop")
	}
	drive(t, a, release(hx, hy))
	if parentOf(a, dock) != harbor || a.tdrag.press {
		t.Fatalf("the drop left dock under %q", parentOf(a, dock))
	}
}

// ONLY A VALID TARGET TAKES A DROP: orbit is past the depth limit, so it is not
// grounded, the hint says why, and a release there moves nothing. The empty rail
// under the tree is the top level, and esc drops a drag with nothing moved.
func TestDragDropsOnlyOnValidTargetsAndEscCancels(t *testing.T) {
	a, harbor, orbit, dock := nestLab(t)
	dx, dy := railPoint(t, a, dock)
	ox, oy := railPoint(t, a, orbit)
	drive(t, a, nestPress(dx, dy), dragTo(dx+4, dy+3), dragTo(ox, oy))
	if a.teamDropLit(orbit) || !strings.Contains(a.teamDragHint(), "orbit is 2 levels deep") {
		t.Fatalf("a blocked target is grounded or unexplained: %q", a.teamDragHint())
	}
	drive(t, a, release(ox, oy))
	if parentOf(a, dock) != "" {
		t.Fatal("a drop on a blocked target moved the team")
	}
	// orbit to the top level, by the empty rail under the tree.
	below := -1
	for _, tg := range a.tp.targets {
		if tg.act == teamsActNewTeam {
			below = tg.y - 1
		}
	}
	drive(t, a, nestPress(ox, oy), dragTo(ox, oy+2), dragTo(3, below))
	if !a.teamDropLit(teamMoveTop) || !strings.Contains(teamsFrameText(a), "Top level") {
		t.Fatalf("the empty rail is not the top level: %q\n%s", a.teamDragHint(), teamsFrameText(a))
	}
	drive(t, a, release(3, below))
	if parentOf(a, orbit) != "" {
		t.Fatalf("orbit is still under %q", parentOf(a, orbit))
	}
	// esc drops a drag and nothing happens.
	hx, hy := railPoint(t, a, harbor)
	drive(t, a, nestPress(dx, dy), dragTo(hx, hy+3), dragTo(hx, hy))
	drive(t, a, key("esc"))
	if a.tdrag.press || a.tdrag.on {
		t.Fatal("esc did not drop the drag")
	}
	drive(t, a, release(hx, hy))
	if parentOf(a, dock) != "" {
		t.Fatal("a drag esc cancelled still moved the team")
	}
}

// DRAGGING A MEMBER ONTO A TEAM ADDS IT, AND NEVER TAKES IT OUT of the team it
// came from: @crane dragged from dock's members card onto harbor is in both.
func TestDraggingAMemberRowOntoATeamAddsIt(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	drive(t, a, key(teamCrewLetter))
	if !a.tcrew.on {
		t.Fatal("p did not open the members card")
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "@crane") || !strings.Contains(text, "Resume") || strings.Contains(text, "not open") {
		t.Fatalf("the members card:\n%s", text)
	}
	var row wallHit
	for _, h := range a.tcrew.hits {
		if h.kind == crewHitRow && h.arg == 0 {
			row = h
		}
	}
	if row.x1 == 0 {
		t.Fatalf("no row on the members card:\n%s", text)
	}
	hx, hy := railPoint(t, a, harbor)
	drive(t, a, nestPress(row.x0+2, row.y0), dragTo(row.x0+5, row.y0+1), dragTo(hx, hy))
	if hint := a.teamDragHint(); !strings.HasPrefix(hint, "Add @crane to harbor") {
		t.Fatalf("the member drag's hint: %q", hint)
	}
	drive(t, a, release(hx, hy))
	key := a.convKey("/tmp/lab/crane.jsonl")
	h, _ := a.teamByID(harbor)
	d, _ := a.teamByID(dock)
	if !h.Holds(key) || !d.Holds(key) {
		t.Fatalf("the drop moved rather than added: harbor %v dock %v", h.Holds(key), d.Holds(key))
	}
}

// SPACE PICKS TEAMS ON THE RAIL, AND ONE MOVE INTO… MOVES THEM ALL.
func TestMultiSelectedTeamsMoveTogether(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	pier := newTeamID()
	if err := a.teamEdit(func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: pier, Name: "pier"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(a.profileDir), []byte(`{"teams.depth_limit": 4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.tp.defaults = teamstore.DefaultsAt(a.profileDir)
	a.frame()
	for _, id := range []string{dock, harbor} {
		a.tp.focus, a.tp.cur = true, teamsRef{act: teamsActSelect, id: id}
		drive(t, a, key(" "))
	}
	if got := a.teamsPickedIDs(); len(got) != 2 {
		t.Fatalf("picked %v", got)
	}
	if !strings.Contains(teamsFrameText(a), wallGlyphsFor(false).marked) {
		t.Fatal("the rail does not mark the picked teams")
	}
	drive(t, a, key("m"))
	if !strings.Contains(teamsFrameText(a), "Move 2 teams into") {
		t.Fatalf("the picker is not for both:\n%s", teamsFrameText(a))
	}
	for _, r := range a.teamMoveRows() {
		if r.parent == pier {
			drive(t, a, runCmd(a.teamMoveChoose(r))...)
		}
	}
	if parentOf(a, dock) != pier || parentOf(a, harbor) != pier {
		t.Fatalf("dock under %q, harbor under %q", parentOf(a, dock), parentOf(a, harbor))
	}
}

// `+ NEW TEAM` WITH A TEAM CHOSEN READS `+ New team in harbor` and makes the
// team inside it; a team at the depth limit dims it and says why.
func TestNewTeamInTheChosenTeam(t *testing.T) {
	a, harbor, orbit, _ := nestLab(t)
	drive(t, a, runCmd(a.teamsSelect(harbor))...)
	if !strings.Contains(teamsFrameText(a), "+ New team in harbor") {
		t.Fatalf("the rail does not offer a team inside harbor:\n%s", teamsFrameText(a))
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActNewTeam, harbor)))...)
	if !a.wall.on || !a.wall.naming || a.wall.nameParent != harbor {
		t.Fatalf("the new-team card is not for a team in harbor: on %v naming %v parent %q", a.wall.on, a.wall.naming, a.wall.nameParent)
	}
	if !strings.Contains(teamsFrameText(a), "New team in harbor") {
		t.Fatalf("the card does not say where the team goes:\n%s", teamsFrameText(a))
	}
	a.wall.name = "slip"
	drive(t, a, runCmd(a.wallMakeTeam(a.wallShown(a.now())))...)
	var made team
	for _, u := range a.wall.teams {
		if u.Name == "slip" {
			made = u
		}
	}
	if made.Parent != harbor {
		t.Fatalf("the new team is under %q, want harbor", made.Parent)
	}
	a.closeWall()
	drive(t, a, key("alt+2"))
	drive(t, a, runCmd(a.teamsSelect(orbit))...)
	tg := teamsTargetOf(t, a, teamsActNewTeam, orbit)
	if !strings.Contains(tg.hint, "limit 2") {
		t.Fatalf("a team at the limit does not say why: %q", tg.hint)
	}
	drive(t, a, runCmd(a.teamsDo(tg))...)
	if a.wall.naming {
		t.Fatal("a team at the limit opened the new-team card")
	}
}

// THE TEAM'S CARD HAS `Inside: …  ▾`, which opens the same picker, and the move
// it makes is offered back on the card.
func TestTheCardsInsideFieldMovesTheTeam(t *testing.T) {
	a, harbor, _, dock := nestLab(t)
	drive(t, a, runCmd(a.teamSheetOpen(dock, teamSheetSettings))...)
	if text := teamsFrameText(a); !strings.Contains(text, "Inside") || !strings.Contains(text, "Top level ▾") {
		t.Fatalf("the card has no Inside field:\n%s", text)
	}
	drive(t, a, runCmd(a.teamSheetDo(tsInside))...)
	if !a.tmove.on || a.tmove.from != teamMoveFromCard {
		t.Fatal("Inside did not open the picker")
	}
	drive(t, a, key("enter"))
	if parentOf(a, dock) != harbor {
		t.Fatalf("dock is under %q", parentOf(a, dock))
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "harbor ▾") || !strings.Contains(text, "Undo") {
		t.Fatalf("the card does not show the move and its Undo:\n%s", text)
	}
	drive(t, a, key("u"))
	if parentOf(a, dock) != "" {
		t.Fatal("u on the card did not undo the move")
	}
}

// THE SWITCHER IS THE TREE, INDENTED; THE WALL'S TEAMS ROW STAYS FLAT and says
// `harbor › orbit` for the team inside harbor.
func TestSwitcherIsTheTreeAndTheWallSaysParentAndChild(t *testing.T) {
	a, harbor, orbit, _ := nestLab(t)
	a.leavePlace()
	a.openTeamMenu()
	frame, _, _ := a.frame()
	lines := strings.Split(plain(frame), "\n")
	hy, oy := -1, -1
	// The switcher's rows, not the strip's chip above it: a radio leads each.
	for y, l := range lines {
		if hy < 0 && strings.Contains(l, "◉ ● harbor") {
			hy = y
		}
		if oy < 0 && strings.Contains(l, "● orbit") && strings.Contains(l, "○") {
			oy = y
		}
	}
	if hy < 0 || oy != hy+1 || strings.Index(lines[oy], "●") <= strings.Index(lines[hy], "●") {
		t.Fatalf("the switcher does not indent orbit under harbor:\n%s", plain(frame))
	}
	a.closeTeamMenu()
	o, _ := a.teamByID(orbit)
	if got := a.wallTeamLabel(o); got != "harbor › orbit" {
		t.Fatalf("the wall calls orbit %q", got)
	}
	h, _ := a.teamByID(harbor)
	if got := a.wallTeamLabel(h); got != "harbor" {
		t.Fatalf("the wall calls harbor %q", got)
	}
	a.width = 160
	drive(t, a, runCmd(a.openWall())...)
	if !strings.Contains(teamsFrameText(a), "harbor › orbit") {
		t.Fatalf("the wall's Teams row does not say harbor › orbit:\n%s", teamsFrameText(a))
	}
}

// THE HEADER IS ONE LINE AT EVERY WIDTH: the name always, the buttons unless
// nothing else fits, a working member as a chip, everyone else one idle word,
// and narrow, the idle word goes before the chip.
func TestTheTeamHeaderIsOneLineAndDropsInOrder(t *testing.T) {
	for _, width := range []int{80, 110, 160} {
		t.Run(itoa(width), func(t *testing.T) {
			a, harbor, _ := teamsPlaceLabIDs(t)
			a.width = width
			a.state = stateWorking
			drive(t, a, runCmd(a.teamsSelect(harbor))...)
			text := teamsFrameText(a)
			y := nestRow(a, "Settings")
			if y < 0 {
				t.Fatalf("no header at %d:\n%s", width, text)
			}
			line := strings.Split(text, "\n")[y]
			if !strings.Contains(line, "harbor") {
				t.Fatalf("the header lost the team's name at %d: %q", width, line)
			}
			chip := strings.Contains(line, "working")
			idle := strings.Contains(line, "idle")
			if idle && !chip {
				t.Fatalf("the idle word stayed while the working chip went at %d: %q", width, line)
			}
			if width >= 110 && (!chip || !idle) {
				t.Fatalf("at %d the header should have room for the chip and the idle word: %q", width, line)
			}
			next := strings.Split(text, "\n")[y+1]
			if strings.Contains(next, "idle") || strings.Contains(next, "not open") {
				t.Fatalf("the members spilled onto a second row at %d: %q", width, next)
			}
		})
	}
}

// A RULING AND A SUB-TEAM START READ AS WHAT THEY ARE on the threaded
// Traffic rail (DESIGN.md 8.10).
func TestTrafficSaysRulingsAndSubTeamStarts(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	h, _ := a.teamByID(harbor)
	lay := func(e teamstore.Entry) (string, string) {
		s := &trafficSheet{a: a, t: h, width: 90, hotRow: -1}
		s.thread(teamstore.Thread{Root: e, Latest: e.ID})
		var rows, hints []string
		for _, r := range s.rows {
			rows = append(rows, plain(r.text))
			for _, d := range r.doors {
				hints = append(hints, d.hint)
			}
		}
		return strings.Join(rows, "\n"), strings.Join(hints, "\n")
	}
	ruling := teamstore.Entry{ID: "000000000007", Kind: teamstore.KindDirective, From: teamstore.FromManager, To: "web", Packet: "p9",
		Text: "ruling on the conflict p9, by ◆ @boss (manager of \"harbor\"): JSON: the form stays"}
	got, hint := lay(ruling)
	if !strings.Contains(got, "ruling") || strings.Contains(got, " do") || !strings.Contains(got, "JSON: the form stays") {
		t.Fatalf("the ruling rows: %q", got)
	}
	if !strings.Contains(hint, "p9") {
		t.Fatalf("the ruling's hint does not name its packet: %q", hint)
	}
	start := teamstore.Entry{ID: "000000000008", Kind: teamstore.KindStart, From: teamstore.FromManager, To: "api", Team: orbit, Text: "run it"}
	if got, _ := lay(start); !strings.Contains(got, "started @api to run orbit") {
		t.Fatalf("the sub-team start rows: %q", got)
	}
}
