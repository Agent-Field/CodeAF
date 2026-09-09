package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE WORK HOME DRAWS IS A TREE ───────────────────────────────────────────
//
// The conversation is the parent of everything on these bands — it is the row in
// the list and the title on the card — and the work under it hangs off the work
// that asked for it. These pin the four claims hometree.go makes: the shape, the
// order, the fold that cannot orphan a child, and the pointer landing on the row
// it is over rather than on the row that happens to be spelled inside it.

// treeTask is one row of the index, written the way a run writes one.
func treeTask(id, parent, label string, status session.TaskState, ended time.Time) session.TaskIndexEntry {
	return session.TaskIndexEntry{
		ID: id, Parent: parent, SessionID: treeSession,
		Name:  strings.ToLower(strings.ReplaceAll(label, " ", "-")),
		Label: label, Title: label, Status: string(status), EndedAt: ended,
	}
}

const treeSession = "aaaa000000000001"

// treeRow is a conversation holding those rows and nothing else, which is what
// every reading in hometree.go is a function of.
func treeRow(entries ...session.TaskIndexEntry) session.SessionRow {
	return session.SessionRow{Tasks: session.TaskRollup{Rows: entries}}
}

// treeNames is a family said as `id@depth`, which is the whole of what a walk
// has to get right.
func treeNames(family []homeWorkNode) []string {
	out := make([]string, 0, len(family))
	for _, node := range family {
		out = append(out, node.entry.ID+"@"+itoa(node.depth()))
	}
	return out
}

// treeLab is [workLab]'s conversation with a FAMILY of work behind it: the
// entries exactly as given, in the record's own order, with the cursor left on
// the conversation so the card beside it is this one's.
func treeLab(t *testing.T, width int, entries ...session.TaskIndexEntry) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", treeSession, "the working one", "/tmp/alpha", now)
	for _, entry := range entries {
		lab.task("-tmp-alpha", entry)
	}
	a := lab.app(mine)
	a.width, a.height = width, 40
	a.openHome()
	a.home.point(mine)
	return a
}

// THE SHAPE: a root, what it handed out, and what THAT handed out — one family,
// walked depth first, with every row exactly once and at the depth it stands at.
func TestHomeGrowsAConversationsWorkIntoOneTree(t *testing.T) {
	now := time.Now()
	row := treeRow(
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Port the rows", session.TaskDone, now.Add(-2*time.Hour)),
		treeTask("3", "2", "Port the widths", session.TaskDone, now.Add(-3*time.Hour)),
		treeTask("4", "", "Sweep the imports", session.TaskDone, now.Add(-4*time.Hour)),
	)
	families := homeWorkFamilies(row)
	if len(families) != 2 {
		t.Fatalf("four rows in two runs came out as %d families: %v", len(families), families)
	}
	if got := strings.Join(treeNames(families[0]), " "); got != "1@0 2@1 3@2" {
		t.Fatalf("the first family walked as %q, want the root, its child and the child's own child", got)
	}
	if got := strings.Join(treeNames(families[1]), " "); got != "4@0" {
		t.Fatalf("a run that handed nothing out walked as %q", got)
	}
	// AND NESTING IS PRESERVED AT ANY DEPTH: the grandchild knows its parent has
	// a sibling to come or not, which is the difference between a stem running
	// down past it and blank air.
	if len(families[0][2].stems) != 2 {
		t.Fatalf("the grandchild stands at %d levels, want 2", len(families[0][2].stems))
	}
}

// AND EVERY ROW COMES OUT, whatever the parent seam says. A row pointing at a
// task this conversation does not hold is work somebody can still open; two rows
// pointing at each other are a bug in a producer this package does not own, and
// neither may be a piece of work that silently stops being on the screen.
func TestABrokenParentSeamStillDrawsEveryPieceOfWork(t *testing.T) {
	now := time.Now()
	row := treeRow(
		treeTask("1", "99", "Handed out by a stranger", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "3", "One half of a circle", session.TaskDone, now.Add(-2*time.Hour)),
		treeTask("3", "2", "The other half", session.TaskDone, now.Add(-3*time.Hour)),
	)
	seen := map[string]int{}
	for _, family := range homeWorkFamilies(row) {
		for _, node := range family {
			seen[node.entry.ID]++
		}
	}
	if len(seen) != 3 {
		t.Fatalf("three rows came out as %d: %v", len(seen), seen)
	}
	for id, times := range seen {
		if times != 1 {
			t.Fatalf("row %s was drawn %d times", id, times)
		}
	}
}

// A CHILD THAT NEEDS SOMEBODY BRINGS ITS WHOLE FAMILY FORWARD, root first.
//
// The row that says what the child was FOR is its parent, so a surface that
// promoted the child alone would put the question on the screen with its subject
// folded away — and the fold is three deep, so "alone" is how it would have
// arrived.
func TestAChildAskingForSomebodyBringsItsWholeFamilyForward(t *testing.T) {
	now := time.Now()
	row := treeRow(
		treeTask("1", "", "Newer and quiet", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Quiet child", session.TaskDone, now.Add(-2*time.Hour)),
		treeTask("3", "", "Older run", session.TaskDone, now.Add(-3*time.Hour)),
		treeTask("4", "3", "Nobody could judge it", session.TaskUnverified, now.Add(-4*time.Hour)),
	)
	families := homeWorkFamilies(row)
	if len(families) != 2 {
		t.Fatalf("two runs came out as %d families", len(families))
	}
	if got := strings.Join(treeNames(families[0]), " "); got != "3@0 4@1" {
		t.Fatalf("the family holding the row that wants a person came second: %q", got)
	}
	// AND THE CHILD DID NOT LEAVE ITS PARENT BEHIND to get there: it is still
	// under the root, not standing beside it.
	if families[0][1].depth() != 1 {
		t.Fatalf("the promoted child was lifted out of its family, to depth %d", families[0][1].depth())
	}
}

// ── THE CARD ────────────────────────────────────────────────────────────────

// A CHILD IS DRAWN UNDER THE TASK THAT ASKED FOR IT, behind the roster's own
// elbow — never beside it as one more peer.
func TestTheCardHangsAChildUnderTheTaskThatAskedForIt(t *testing.T) {
	now := time.Now()
	a := treeLab(t, homeCardWidest,
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Move the rows", session.TaskDone, now.Add(-2*time.Hour)),
		treeTask("3", "2", "Measure the widths", session.TaskDone, now.Add(-3*time.Hour)),
	)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	root := workRowAt(t, rows, "Port the Picker")
	if strings.HasPrefix(strings.TrimLeft(rows[root], " "), treeLast) {
		t.Fatalf("the run's own row wears a connector, so nothing says it is the top of the family:\n%s", card)
	}
	child := workRowAt(t, rows, "Move the rows")
	if child != root+1 {
		t.Fatalf("the child is %d rows under its parent, want the next row:\n%s", child-root, card)
	}
	if !strings.HasPrefix(rows[child], treeLast) {
		t.Fatalf("the child is not drawn under anything: %q", rows[child])
	}
	// AND THE GRANDCHILD KEEPS ITS OWN DEPTH: three cells a level, so a reader
	// can tell which of the two rows above it handed this one out.
	grand := workRowAt(t, rows, "Measure the widths")
	if !strings.HasPrefix(rows[grand], treeVoid+treeLast) {
		t.Fatalf("the grandchild is drawn at the child's depth: %q", rows[grand])
	}
}

// The compact preview may hide descendants, but it never draws a child without
// its parent. Its remainder counts tasks and opens the complete tree.
func TestTheCardsWorkFoldNeverOrphansAChild(t *testing.T) {
	now := time.Now()
	var entries []session.TaskIndexEntry
	for i := 0; i < 4; i++ {
		root := itoa(i*2 + 1)
		entries = append(entries,
			treeTask(root, "", "Run "+itoa(i), session.TaskDone, now.Add(-time.Duration(i*2+1)*time.Hour)),
			treeTask(itoa(i*2+2), root, "Part of "+itoa(i), session.TaskDone, now.Add(-time.Duration(i*2+2)*time.Hour)))
	}
	a := treeLab(t, homeCardWidest, entries...)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	if workRowAt(t, rows, "Part of 0") != workRowAt(t, rows, "Run 0")+1 {
		t.Fatalf("the visible child lost its parent:\n%s", card)
	}
	if !strings.Contains(card, "Run 1") || strings.Contains(card, "Part of 1") || strings.Contains(card, "Run 2") {
		t.Fatalf("the preview did not keep its three-task allowance:\n%s", card)
	}
	fold := workRowAt(t, rows, "more tasks")
	if !strings.Contains(rows[fold], "5 more tasks") {
		t.Fatalf("the fold counts families rather than the work behind it: %q", rows[fold])
	}
	if !strings.HasSuffix(strings.TrimRight(rows[fold], " "), pageTasks.word()) {
		t.Fatalf("the fold line stopped naming the tasks place: %q", rows[fold])
	}
}

// THE CONVERSATION IS THE PARENT, AND ITS ROW OPENS THE CHAT.
//
// Home has two doors on one subject and they must never be the same door: the
// ROW is the conversation — enter on it opens the chat, which is what a person
// picking a conversation off this screen is asking for — and the work drawn
// beside it opens that piece of work's own record. A surface that made the row
// open the task it happened to be named after, or the task open the chat, would
// have one of the two things a person came here for missing.
func TestTheConversationRowOpensTheChatAndItsWorkStaysOnTheCard(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	workspace := t.TempDir()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", workspace, now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "the big port", workspace, now.Add(-time.Hour))
	for _, entry := range []session.TaskIndexEntry{
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-2*time.Hour)),
		treeTask("2", "1", "Move the rows", session.TaskDone, now.Add(-3*time.Hour)),
	} {
		entry.SessionID = "aaaa000000000002"
		lab.task("-tmp-alpha", entry)
	}
	a := lab.app(mine)
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.width, a.height = homeCardWidest, 40
	a.openHome()

	// The card beside that row names both pieces of work, the child under its run.
	card := homeCardFor(t, a, other)
	root, kid := cardLine(card, "Port the Picker"), cardLine(card, "Move the rows")
	if root < 0 || kid < 0 || kid <= root {
		t.Fatalf("the family is not drawn under the conversation:\n%s", strings.Join(card, "\n"))
	}

	// And enter on the row opens the CONVERSATION — no record, no task page.
	a.home.point(other)
	a.homeEnter()
	if a.at(pageHome) {
		t.Fatalf("enter on the conversation left home up saying %q", a.home.msg)
	}
	if a.file != other {
		t.Fatalf("home opened %q, want the conversation the cursor was on", a.file)
	}
	if a.taskSheet.detailOn {
		t.Fatal("picking the conversation opened one of the tasks under it")
	}
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// A CLICK ON A CHILD OPENS THE CHILD, AND NOT THE RUN IT HANGS UNDER.
//
// The two rows say nearly the same words, because a run and the piece it handed
// out are about the same thing — and the card finds a door by looking for its
// painted text inside the frame line, so the parent's row is spelled INSIDE its
// own child's. A first-match scan handed every nested row to its parent, which
// is the one failure a pointer must not have: the row that lights and the row
// that opens have to be the row under the hand.
func TestAClickOnAChildTaskOpensTheChildAndNotItsParent(t *testing.T) {
	now := time.Now()
	nested := []session.TaskIndexEntry{
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Port the Picker rows", session.TaskDone, now.Add(-2*time.Hour)),
	}
	press := func(t *testing.T, a *app, at int) string {
		t.Helper()
		width, _ := a.size()
		left, _ := homeColumns(width)
		drive(t, a,
			tea.MouseClickMsg{X: left + homeGutter + 2, Y: at, Button: tea.MouseLeft},
			tea.MouseReleaseMsg{X: left + homeGutter + 2, Y: at, Button: tea.MouseLeft})
		if !a.at(pageTasks) || !a.taskSheet.detailOn {
			t.Fatalf("a press on row %d opened no record:\n%s", at, homeText(a))
		}
		return homeTaskText(a.taskSheet.detail)
	}
	a := treeLab(t, homeCardWidest, nested...)
	if got := press(t, a, treeRowY(t, a, "Port the Picker rows", "")); got != "Port the Picker rows" {
		t.Fatalf("a click on the child opened %q", got)
	}

	// And the run's own row still opens the run: what changed is which door a row
	// answers with, not one row answering for two.
	a = treeLab(t, homeCardWidest, nested...)
	if got := press(t, a, treeRowY(t, a, "Port the Picker", "rows")); got != "Port the Picker" {
		t.Fatalf("a click on the run opened %q", got)
	}
}

// treeRowY is [cardRowY] for a card that draws one row's words inside another's:
// the screen row holding `want` and NOT holding `notWant`.
func treeRowY(t *testing.T, a *app, want, notWant string) int {
	t.Helper()
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	for y, line := range lines {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, want) {
			continue
		}
		if notWant != "" && strings.Contains(plain, notWant) {
			continue
		}
		return y
	}
	t.Fatalf("the frame holds no line saying %q without %q:\n%s", want, notWant, homeText(a))
	return -1
}

// AND THE ROW THAT LIGHTS IS THE ROW THAT OPENS, on a nested row too. The hover
// and the press read one registry, so a pointer over a child may not light the
// run above it (carddoors.go).
func TestHoveringAChildTaskLightsTheChildsOwnRow(t *testing.T) {
	now := time.Now()
	a := treeLab(t, homeCardWidest,
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Port the Picker rows", session.TaskDone, now.Add(-2*time.Hour)),
	)
	child := treeRowY(t, a, "Port the Picker rows", "")
	root := treeRowY(t, a, "Port the Picker", "rows")
	if root == child {
		t.Fatal("the run and its child were found on one row")
	}
	before := framePaint(t, a, root)
	pointAtCard(t, a, child)
	if lit := framePaint(t, a, child); !strings.Contains(lit, cardHoverInk(a)) {
		t.Fatalf("the child row under the pointer wears no hover ink: %q", lit)
	}
	if after := framePaint(t, a, root); after != before {
		t.Fatalf("pointing at the child lit its parent:\n%q\n%q", before, after)
	}
}

// AND THE PHONE'S SHEET STILL ANSWERS A NESTED ROW. It maps a painted row onto
// the task it names by that row's own text, so the connectors have to come off
// the front before the comparison — otherwise the rows a person most wants to
// open are the only ones on the sheet that do nothing.
func TestTheSheetsTapFindsTheTaskUnderTheConnectors(t *testing.T) {
	a := newTestApp(nil)
	now := time.Now()
	row := treeRow(
		treeTask("1", "", "Port the Picker", session.TaskDone, now.Add(-time.Hour)),
		treeTask("2", "1", "Move the rows", session.TaskDone, now.Add(-2*time.Hour)),
	)
	drawn := drawWorkBand(a, ambientBandContext(a, row, now, 44))
	hits := homeSheetTaskHits(drawn, ambientBandContext(a, row, now, 44))
	at := -1
	for i, painted := range drawn {
		if strings.Contains(ansi.Strip(painted), "Move the rows") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the band drew no child row:\n%s", plain(strings.Join(drawn, "\n")))
	}
	if hits[at].kind != homeSheetHitTask || hits[at].index != 1 {
		t.Fatalf("the child row answers %+v, want a tap onto the second row of the index", hits[at])
	}
}

// ── WIDTH ───────────────────────────────────────────────────────────────────

// THE TREE IS PAID FOR OUT OF THE ROW AND NEVER OUT OF THE BAND. Nothing a
// nested row draws reaches past the band's own edge, at any width — measured in
// CELLS, because a connector is a box-drawing glyph and a column counted in
// bytes is a column that is wrong on the first terminal that disagrees.
func TestANestedWorkRowStaysInsideTheBandsWidth(t *testing.T) {
	a := newTestApp(nil)
	now := time.Now()
	row := treeRow(
		session.TaskIndexEntry{ID: "1", SessionID: treeSession, Label: "Port the Picker",
			Status: string(session.TaskDone), Outcome: "the roster resumes cleanly",
			FilesChanged: 14, Cost: .12, EndedAt: now.Add(-time.Hour)},
		session.TaskIndexEntry{ID: "2", Parent: "1", SessionID: treeSession, Label: "Move the rows",
			Status: string(session.TaskUnverified), Outcome: "nobody could judge it",
			FilesChanged: 3, Cost: .04, EndedAt: now.Add(-2 * time.Hour)},
		session.TaskIndexEntry{ID: "3", Parent: "2", SessionID: treeSession, Label: "Measure the widths",
			Status: string(session.TaskDone), Outcome: "every column fits",
			Cost: .01, EndedAt: now.Add(-3 * time.Hour)},
	)
	for _, width := range []int{20, 26, 34, 44, 80} {
		rows := drawWorkBand(a, ambientBandContext(a, row, now, width))
		if len(rows) == 0 {
			t.Fatalf("the band drew nothing at %d cells", width)
		}
		for _, drawn := range rows {
			if ansi.StringWidth(drawn) > width {
				t.Fatalf("a row is %d cells at width %d: %q", ansi.StringWidth(drawn), width, plain(drawn))
			}
		}
	}
}

// AND WHERE THE TREE WILL NOT FIT, THE TREE GIVES WAY AND THE NAME DOES NOT.
// A row drawn so deep that its name is an ellipsis has spent the only cells that
// say which piece of work it is.
func TestANarrowBandGivesUpTheTreeBeforeTheName(t *testing.T) {
	a := newTestApp(nil)
	now := time.Now()
	var entries []session.TaskIndexEntry
	for i := 0; i < 8; i++ {
		parent := ""
		if i > 0 {
			parent = itoa(i)
		}
		entries = append(entries, treeTask(itoa(i+1), parent, "Level "+itoa(i), session.TaskDone, now.Add(-time.Duration(i+1)*time.Hour)))
	}
	rows := drawWorkBand(a, ambientBandContext(a, treeRow(entries...), now, 30))
	deepest := ""
	for _, drawn := range rows {
		if strings.Contains(ansi.Strip(drawn), "Level 7") {
			deepest = plain(drawn)
		}
	}
	if deepest == "" {
		t.Fatalf("the deepest row was dropped rather than drawn shallow:\n%s", plain(strings.Join(rows, "\n")))
	}
	// The name keeps its floor: the connectors collapsed to the one elbow that
	// says this row hangs off something, so the name starts inside the first
	// level rather than eight levels in.
	lead := ansi.StringWidth(deepest[:strings.Index(deepest, "Level 7")])
	if lead > treeIndentCols {
		t.Fatalf("the deepest row is drawn %d cells in at 30 wide: %q", lead, deepest)
	}
	if lead == 0 {
		t.Fatalf("the deepest row gave up the one elbow that says it hangs off something: %q", deepest)
	}
}
