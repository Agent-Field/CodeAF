package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE ROSTER'S FOREST ─────────────────────────────────────────────────────
//
// The column used to file every node under one of five state headings, which
// scattered one adaptive run across four sections of itself. It draws families
// now (task.go's [app.railEntries]), and these are the whole of that claim: the
// shape, the order, the fold and its default, the two hands that reach it.

// railKinship hands a node to a parent, in the alphabet the seam speaks
// (taskstrip.go's [taskNode.ParentID]).
func railKinship(a *app, parent uint64, kids ...uint64) {
	for _, id := range kids {
		a.tasks[id].parent = itoa(int(parent))
	}
}

// railRun plants one adaptive run: a root the person started, and the tree its
// planner spawned under it.
//
// THE NODES ARRIVE IN ID ORDER AND THE COLUMN DOES NOT DRAW THEM IN IT. A
// family's members are ranked by what they need — running, then waiting, then
// over — with arrival order deciding between two in the same state (task.go's
// [app.railKin]), so the drawn shape is:
//
//	1 Ship the port        running
//	├─ 3 Write the tree    running
//	│  └─ 4 Cut goldens    queued
//	├─ 5 Wire the seam     queued
//	└─ 2 Read the law      done
func railRun(a *app) {
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{}))
	a.taskUpdate(update(3, "Write the tree", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(4, "Cut the goldens", session.TaskQueued, session.TaskNotice{}))
	a.taskUpdate(update(5, "Wire the seam", session.TaskQueued, session.TaskNotice{}))
	railKinship(a, 1, 2, 3, 5)
	railKinship(a, 3, 4)
	// The spinner is on the frame clock (tokens.Spinner), so a golden has to say
	// which frame it was taken on.
	a.paints = 0
}

// railText is the roster as a reader sees it, row by row.
func railText(a *app, height int) []string {
	rows := a.railRows(height)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = plain(row)
	}
	return out
}

// mustRailRow is [railRowFor] over the whole visible column, for the tests that
// would be lying if the row were missing.
func mustRailRow(t *testing.T, a *app, title string) string {
	t.Helper()
	row, ok := railRowFor(a, a.viewHeight(), title)
	if !ok {
		t.Fatalf("the column has no row for %q:\n%s", title,
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	return row
}

// railRowFor is the drawn row a node's title is on, and whether there is one.
func railRowFor(a *app, height int, title string) (string, bool) {
	for _, row := range railText(a, height) {
		if strings.Contains(row, title) {
			return row, true
		}
	}
	return "", false
}

// railFocusOn walks the roster's cursor onto a node, however far down it is.
func railFocusOn(t *testing.T, a *app, id uint64) {
	t.Helper()
	for i := 0; i < 40 && a.railWhere.id != id; i++ {
		a.railMove(1)
	}
	if a.railWhere.id != id {
		t.Fatalf("the cursor never reached node %d", id)
	}
}

// A FAMILY IS DRAWN WHOLE AND THE CONNECTORS ARE IN THE ROW. One row per node,
// children indented under the thing that spawned them, whatever state each of
// them is in — the settled rows are what make the unsettled ones legible.
//
// It is a byte comparison on purpose: a tree is alignment, and a test that only
// asked "does it contain ├─" would pass on a tree whose second level had
// drifted a cell.
func TestAFamilyIsDrawnWholeUnderItsRoot(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	spin := tokens.Spinner(0)
	want := []string{
		// The column opens with the margin's own section label (margin.go), and the
		// family is drawn whole under it.
		//
		// AND THE MEMBERS ARE RANKED, which is what changed here. They used to be
		// drawn in the order the session met them, so a run that finishes its
		// pieces one at a time put every settled row in front of the ones still
		// going — `done, done, running, running`, with the only rows anybody was
		// watching at the bottom of the block. The column already ranked whole
		// FAMILIES this way and stopped at the family boundary; it now goes all the
		// way down (task.go's [app.railKin]). Arrival order still separates two
		// pieces in the same state, so #5 (queued) leads #2 (done) by state and
		// nothing settled ever trades places with anything else settled.
		"│ " + marginTasksWord,
		"│ " + spin + " Ship the port           #1",
		"│ ├─ " + spin + " Write the tree       #3",
		"│ │  └─ " + glyphQueued + " Cut the goldens   #4",
		"│ ├─ " + glyphQueued + " Wire the seam        #5",
		"│ └─ " + glyphDone + " Read the law         #2",
	}
	got := railText(a, 12)
	if len(got) < len(want) {
		t.Fatalf("the family drew %d rows:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is\n\t%q\nwant\n\t%q\nwhole column:\n%s", i, got[i], want[i], strings.Join(got, "\n"))
		}
	}
	// EVERY ROW IS INSIDE THE COLUMN, connectors and all.
	for i, row := range got {
		if w := ansi.StringWidth(row); w > railCols {
			t.Fatalf("row %d is %d cells wide, want at most %d:\n%q", i, w, railCols, row)
		}
	}
	// EVERY ROW ON THIS COLUMN WEARS ITS STATE AND NOTHING ELSE: the column of
	// glyphs is read downward, and the identity ◆ is not spent on any row of it —
	// tree or flat (task.go's [app.railLead]).
	for i, row := range got {
		if strings.Contains(row, plain(a.taskMark(identFor(2)))) {
			t.Fatalf("row %d carries the identity cell as well as the state:\n%q", i, row)
		}
	}
}

// A FAMILY STANDS WHERE ITS MOST URGENT MEMBER PUTS IT, and its members stand
// the same way inside it — the same ladder at both scales, with the session's own
// admission order breaking ties at each (task.go's [app.railKin] and
// [app.railForest]). Every family here holds one child, so what this pins is the
// outer half; [TestAFamilyIsDrawnWholeUnderItsRoot] pins the inner one.
func TestAFamilyStandsWhereItsMostUrgentMemberPutsIt(t *testing.T) {
	a, _, _ := taskApp(t)
	// A settled family, then a running one, then a family with a conflicted branch in
	// it — planted in that order, which is the opposite of the order they belong
	// in.
	a.taskUpdate(update(1, "Cut the trailer", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.taskUpdate(update(2, "Trim silence", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.taskUpdate(update(3, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(4, "Write the tree", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(5, "Port the parser", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.taskUpdate(update(6, "Render titles", session.TaskFailed, session.TaskNotice{
		Merge: mergeWordConflicted, Branch: "task/render",
	}))
	railKinship(a, 1, 2)
	railKinship(a, 3, 4)
	railKinship(a, 5, 6)
	// Every family open, so the order can be read off the rows themselves.
	for _, id := range []uint64{1, 3, 5} {
		a.railSetOpen(a.tasks[id], true)
	}

	rail := strings.Join(railText(a, 20), "\n")
	at := -1
	for _, want := range []string{"Port the parser", "Render titles", "Ship the port", "Write the tree",
		"Cut the trailer", "Trim silence"} {
		found := strings.Index(rail, want)
		if found < 0 {
			t.Fatalf("the roster has no %q row:\n%s", want, rail)
		}
		if found < at {
			t.Fatalf("%q is out of order:\n%s", want, rail)
		}
		at = found
	}
}

// THE FOLD HAS A DEFAULT WORTH HAVING AND THE PERSON MAY OVERRULE IT. A family
// with anything live in it opens; a family that has entirely settled is one row
// with a count on it; and whatever a person says about either one sticks.
func TestAFamilyOpensWhileItIsLiveAndFoldsOnceItSettles(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	if _, ok := railRowFor(a, 12, "Cut the goldens"); !ok {
		t.Fatalf("a live family came up folded:\n%s", strings.Join(railText(a, 12), "\n"))
	}

	// The whole run lands. Nothing under it is going anywhere, so it stands as one
	// row — a hundred and forty-eight settled nodes are a fact, not a hundred and
	// forty-eight rows.
	for _, id := range []uint64{1, 2, 3, 4, 5} {
		a.taskUpdate(update(id, a.tasks[id].title, session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	}
	rows := railText(a, 12)
	if _, ok := railRowFor(a, 12, "Cut the goldens"); ok {
		t.Fatalf("a settled family stayed open:\n%s", strings.Join(rows, "\n"))
	}
	if _, ok := railRowFor(a, 12, "Ship the port"); !ok {
		t.Fatalf("the folded family lost its own row:\n%s", strings.Join(rows, "\n"))
	}

	// AND THE PERSON'S ANSWER STICKS. Opened by hand, it stays open through the
	// next update — the default is the design and the map is the correction of it.
	a.railSetOpen(a.tasks[1], true)
	a.taskUpdate(update(2, "Read the law", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	if _, ok := railRowFor(a, 12, "Read the law"); !ok {
		t.Fatalf("the fold a person opened closed itself again:\n%s", strings.Join(railText(a, 12), "\n"))
	}
	a.railSetOpen(a.tasks[1], false)
	if _, ok := railRowFor(a, 12, "Read the law"); ok {
		t.Fatalf("the fold a person closed stayed open:\n%s", strings.Join(railText(a, 12), "\n"))
	}
}

// A FOLDED FAMILY WEARS THE WORST THING UNDER IT AND SAYS HOW MUCH IT IS
// STANDING FOR. The row is the whole subtree now, so its one cell is the
// subtree's news and its trailing slot is the count rather than the handle.
func TestAFoldedFamilyWearsItsWorstGlyphAndCountsWhatItHides(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	// Everything settles except one node, which did not come off.
	for _, id := range []uint64{1, 2, 3, 5} {
		a.taskUpdate(update(id, a.tasks[id].title, session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	}
	a.taskUpdate(update(4, "Cut the goldens", session.TaskFailed, session.TaskNotice{
		Report: "the tests did not build",
	}))
	row, ok := railRowFor(a, 12, "Ship the port")
	if !ok {
		t.Fatalf("the folded family has no row:\n%s", strings.Join(railText(a, 12), "\n"))
	}
	// FOUR NODES HIDDEN, and the failure is what the one cell says — the root
	// itself merged clean.
	if !strings.Contains(row, glyphShut+" +4") {
		t.Fatalf("the folded row does not count what it hides:\n%q", row)
	}
	if !strings.HasPrefix(row, "│ "+glyphBad+" ") {
		t.Fatalf("the folded row does not wear the worst glyph under it:\n%q", row)
	}
	if strings.Contains(row, "#1") {
		t.Fatalf("the folded row spent its trailing slot twice:\n%q", row)
	}
	// AND IT IS ONE ROW. A folded family that still said what it was doing would
	// be a fold that hid nothing.
	if rows := railText(a, 12); strings.Contains(strings.Join(rows[:1], ""), mergeWordMerged) {
		t.Fatalf("the folded row kept its under-block:\n%s", strings.Join(rows, "\n"))
	}
}

// →← ARE THE TREE'S OWN GRAMMAR: → opens a folded family and then steps into it,
// ← folds an open one and walks up out of a leaf.
func TestTheTreeGrammarOpensStepsInFoldsAndWalksUp(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	drive(t, a, ctrlT())
	railFocusOn(t, a, 1)

	// ← on an open root folds it and leaves the cursor where the family now is.
	drive(t, a, key("left"))
	if _, ok := railRowFor(a, 12, "Read the law"); ok {
		t.Fatalf("← did not fold the family:\n%s", strings.Join(railText(a, 12), "\n"))
	}
	if a.railWhere.id != 1 {
		t.Fatalf("← left the cursor on %+v, want the root it folded", a.railWhere)
	}
	// → on a folded root opens it, and → again steps onto the first child.
	drive(t, a, key("right"))
	if _, ok := railRowFor(a, 12, "Read the law"); !ok {
		t.Fatalf("→ did not open the family:\n%s", strings.Join(railText(a, 12), "\n"))
	}
	if a.railWhere.id != 1 {
		t.Fatalf("→ opened the family and moved the cursor to %+v", a.railWhere)
	}
	drive(t, a, key("right"))
	if a.railWhere.id != 3 {
		t.Fatalf("→ stepped to %+v, want the first visible child (running before done)", a.railWhere)
	}

	// ← FROM A LEAF JUMPS TO THE PARENT ROW. A cursor left pointing at nothing is
	// a position that means nothing.
	railFocusOn(t, a, 4)
	drive(t, a, key("left"))
	if a.railWhere.id != 3 {
		t.Fatalf("← from the grandchild landed on %+v, want its parent", a.railWhere)
	}
	// And enter is the one activating key: it opens that node's room.
	drive(t, a, key("enter"))
	if !a.roomOpen() || a.room.id != 3 {
		t.Fatalf("enter did not open the focused node's room: open=%v", a.roomOpen())
	}
}

// THE DISCLOSURE IS THE FOLD AND THE REST OF THE ROW IS THE DOOR. One row, two
// targets, and which one a press meant is a question about the column it landed
// in AND about what the frame actually drew there (room.go's [app.railPress]).
func TestPressingTheGlyphCellFoldsAndPressingTheTitleOpensTheRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	rootY := -1
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if node := a.railNodeAt(y); node != nil && node.id == 1 {
			rootY = y
			break
		}
	}
	if rootY < 0 {
		t.Fatalf("the family's root is not on screen:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	glyph := a.bodyWidth() + ansi.StringWidth(railSeam)

	// THE POINTER IS ON THE ROW FIRST, and that is not fixture ceremony: it is the
	// only state in which that cell is a disclosure at all. At rest it draws the
	// root's STATE, and a state is not a control — which is what
	// [TestTheStateCellOpensTheTaskWhenNoDisclosureIsDrawnOnIt] holds the other
	// end of.
	a.setHover(glyph, rootY)
	if !strings.Contains(mustRailRow(t, a, "Ship the port"), glyphOpen) {
		t.Fatalf("the cell about to be pressed is not drawn as a disclosure:\n%q",
			mustRailRow(t, a, "Ship the port"))
	}

	// The disclosure folds the family and opens no room.
	drive(t, a, tea.MouseClickMsg{X: glyph, Y: rootY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: glyph, Y: rootY, Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("a press on the disclosure cell walked into the room behind it")
	}
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); ok {
		t.Fatalf("a press on the disclosure cell did not fold the family:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	// THE ▸ +N EXPANDS IN PLACE, which is the other half of the same affordance.
	line, ok := a.railLineAt(rootY)
	if !ok || !line.badge.pressable() {
		t.Fatalf("the folded row recorded no badge to press: %+v", line)
	}
	drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam) + line.badge.from,
		Y: rootY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam) + line.badge.from,
		Y: rootY, Button: tea.MouseLeft})
	if _, ok := railRowFor(a, a.viewHeight(), "Read the law"); !ok {
		t.Fatalf("a press on the count did not expand the family:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	if a.roomOpen() {
		t.Fatal("a press on the count opened a room as well")
	}

	// And anywhere else on the row is that node's door.
	drive(t, a, tea.MouseClickMsg{X: glyph + 6, Y: rootY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: glyph + 6, Y: rootY, Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("a press on the title did not open the room: open=%v", a.roomOpen())
	}
}

// THE DISCLOSURE IS THE POINTER'S. Nothing on the column says "fold me" at rest
// — a triangle on every root would be a column of widgets — and the moment the
// pointer is over a family root, its state cell becomes the triangle.
func TestTheDisclosureIsRevealedUnderThePointerAndOnlyOnRoots(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	rows := strings.Join(railText(a, 12), "\n")
	if strings.Contains(rows, glyphOpen) || strings.Contains(rows, glyphShut) {
		t.Fatalf("the column drew a disclosure nobody was pointing at:\n%s", rows)
	}

	rootY, leafY := -1, -1
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		switch node := a.railNodeAt(y); {
		case node == nil:
		case node.id == 1:
			rootY = y
		case node.id == 2:
			leafY = y
		}
	}
	if rootY < 0 || leafY < 0 {
		t.Fatalf("the family is not on screen: root=%d leaf=%d", rootY, leafY)
	}
	a.setHover(a.bodyWidth()+4, rootY)
	if a.hot.kind != hoverRail || a.hot.id != 1 {
		t.Fatalf("the pointer over the roster resolved to %+v", a.hot)
	}
	row, _ := railRowFor(a, a.viewHeight(), "Ship the port")
	if !strings.Contains(row, glyphOpen) {
		t.Fatalf("the root under the pointer revealed no disclosure:\n%q", row)
	}
	// A LEAF HAS NOTHING TO DISCLOSE, and it still takes the hover step, because
	// every row of this column is a door.
	a.setHover(a.bodyWidth()+4, leafY)
	row, _ = railRowFor(a, a.viewHeight(), "Read the law")
	if strings.Contains(row, glyphOpen) || strings.Contains(row, glyphShut) {
		t.Fatalf("a leaf offered a fold it does not have:\n%q", row)
	}
	background := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"
	if !strings.Contains(a.railRows(a.viewHeight())[leafY-a.bodyTop()], background) {
		t.Fatalf("the row under the pointer took no hover step:\n%q", row)
	}
	// A FOLDED ROOT SHOWS ITS COUNT AT REST AND ITS TRIANGLE UNDER THE POINTER.
	a.railSetOpen(a.tasks[1], false)
	a.setHover(a.bodyWidth()+4, rootY)
	row, _ = railRowFor(a, a.viewHeight(), "Ship the port")
	if !strings.Contains(row, glyphShut) {
		t.Fatalf("the folded root under the pointer did not point at what it hides:\n%q", row)
	}
}

// THE UNDER-BLOCK HANGS FROM THE STEM IT BELONGS TO. A node's telemetry sits
// between that node's row and its next sibling's, so a block indented with plain
// spaces would put a gap in the line the eye is following down the family.
func TestUnderRowsCarryTheStemTheyHangFrom(t *testing.T) {
	a, _, advance := taskApp(t)
	railRun(a)
	a.tasks[3].tool, a.tasks[3].toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)
	advance(0)

	rows := railText(a, 14)
	at := -1
	for i, row := range rows {
		if strings.Contains(row, "Write the tree") {
			at = i
		}
	}
	if at < 0 || at+1 >= len(rows) {
		t.Fatalf("the running child has no under-row:\n%s", strings.Join(rows, "\n"))
	}
	under := rows[at+1]
	if !strings.Contains(under, "go test") {
		t.Fatalf("the under-row is not the call the node is in:\n%q", under)
	}
	// The row hangs under "├─ ", so the stem continues where the elbow was, and
	// the node's own stem carries on down to its child.
	if want := "│ │  │  "; !strings.HasPrefix(under, want) {
		t.Fatalf("the under-row opens %q, want the stems %q:\n%s", under, want, strings.Join(rows, "\n"))
	}
	// A SETTLED NODE IN A TREE IS ONE LINE: the merge word under every landed row
	// would be a column of history inside a shape somebody is reading for shape.
	for _, row := range rows {
		if strings.Contains(row, mergeWordMerged) {
			t.Fatalf("a settled row in a family kept its under-block:\n%s", strings.Join(rows, "\n"))
		}
	}
}

// THE COLUMN WIDENS ON DEMAND AND OFFERS IT ONLY WHEN IT WOULD HELP. The hint is
// earned by a title the indent cut, it disappears the moment the column is wide,
// and it is pressable because a hint only one hand can use is half a hint.
func TestTheWidenHintIsEarnedByTheIndentAndTogglesTheWideTier(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	// Nothing is cut yet: the names in this run fit their indent.
	if strings.Contains(strings.Join(railText(a, 14), "\n"), railWideHint) {
		t.Fatalf("the column offered the wide tier with nothing to gain:\n%s",
			strings.Join(railText(a, 14), "\n"))
	}

	// A deep node with a long name is a title the indent cuts.
	a.taskUpdate(update(6, "Cut the goldens", session.TaskRunning, session.TaskNotice{}))
	railKinship(a, 4, 6)
	// The roster draws the NAME (taskident.go cuts it), so the long one is written
	// onto the node rather than sent through the notice.
	a.tasks[6].title = "Cut the goldens for the tree"
	rail := strings.Join(railText(a, 14), "\n")
	if !strings.Contains(rail, railWideHint) {
		t.Fatalf("a title cut by its own indent did not offer the wide tier:\n%s", rail)
	}

	// alt+w takes it, and the column is charged against the conversation like the
	// other two tiers. It is a chord and no longer the bare letter `w`, because a
	// bare letter beside a message box is a letter out of somebody's sentence
	// (chordfocus.go).
	drive(t, a, ctrlT(), key(railWidenChord))
	if !a.railWide || a.railWidth() != railWideCols {
		t.Fatalf("%s did not widen the column: wide=%v width=%d", railWidenChord, a.railWide, a.railWidth())
	}
	if a.bodyWidth() != a.width-railWideCols {
		t.Fatalf("the wide column is not charged against the conversation: body=%d", a.bodyWidth())
	}
	wide := strings.Join(railText(a, 14), "\n")
	if !strings.Contains(wide, "Cut the goldens for the tree") {
		t.Fatalf("the wide column still cut the name it was widened for:\n%s", wide)
	}
	if strings.Contains(wide, railWideHint) {
		t.Fatalf("the wide column went on offering to widen:\n%s", wide)
	}
	drive(t, a, key(railWidenChord))
	if a.railWide || a.railWidth() != railCols {
		t.Fatalf("%s did not give the columns back: wide=%v width=%d", railWidenChord, a.railWide, a.railWidth())
	}

	// AND THE HINT IS A BUTTON. A press on that line widens the column.
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if line, ok := a.railLineAt(y); ok && line.hint {
			drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if !a.railWide {
		t.Fatalf("a press on the hint did not widen the column")
	}
	// AND IT GOES WITH THE SESSION. A column widened for a tree that no longer
	// exists is a charge on a conversation that never asked for it.
	a.dropTasks()
	if a.railWide {
		t.Fatal("/new kept the wide column")
	}
}

// THE FULLSCREEN ROSTER IS THE SAME FOREST. Under a hundred columns there is no
// column to lend, so ctrl+t draws the whole thing over the body — the same
// families, the same folds, at the width the frame actually has.
func TestTheFullscreenRosterDrawsTheSameTree(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 80
	a.touch()
	railRun(a)
	drive(t, a, ctrlT())
	if !a.railFull() {
		t.Fatal("ctrl+t did not raise the roster over the body")
	}
	rows := railText(a, a.viewHeight())
	body := strings.Join(rows, "\n")
	for _, want := range []string{"Ship the port", "├─ ", "└─ ", "Cut the goldens"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the fullscreen roster is missing %q:\n%s", want, body)
		}
	}
	// THE SEAM IS A SEAM AND NOT A BORDER, so it is not drawn where there is
	// nothing on the other side of it (task.go's [app.railRows]).
	for i, row := range rows {
		if strings.HasPrefix(row, railSeam) {
			t.Fatalf("fullscreen row %d opens with the rail's seam:\n%q", i, row)
		}
	}
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > a.width {
			t.Fatalf("row %d is %d cells wide on an %d-column frame:\n%q", i, w, a.width, row)
		}
	}
	// The fold is the same fold, from the same key.
	drive(t, a, key("left"))
	if strings.Contains(strings.Join(railText(a, a.viewHeight()), "\n"), "Cut the goldens") {
		t.Fatalf("← did not fold the family over the body:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
	}
}
