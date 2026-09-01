package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE CRUMB'S PARENT CHAIN ────────────────────────────────────────────────
//
// The room header is gone, and the kin rows it pinned went with it. Where a
// room stands in its tree is the TOP BAR'S fact now (topbar.go): the crumb is
// project › chat › parent tasks › this task #N, walked up the engine's own
// parent seam, and the give-way ladder that shortens it is [fitTrail]. What
// these tests hold shut is that the chain is the roster's own — outermost
// first, cycle-safe, the current segment never sacrificed for a parent — and
// that the bar costs the body exactly two rows whenever it stands.

// THE CRUMB WALKS THE ENGINE'S OWN PARENT SEAM, outermost first: the project,
// the chat's own name, every parent task between the conversation and this
// one, and this task's title with its handle.
func TestTheCrumbCarriesTheRoomsWholeChain(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(4, "Cut the goldens")

	segs := a.crumbSegments()
	var texts []string
	for _, seg := range segs {
		texts = append(texts, seg.text)
	}
	// The chain, in the order a reader climbs it: project, chat, the root, the
	// middle, and the room itself. The chat's name is the session's own
	// (names.go's [app.sessionName]); what it is called is the names place's
	// law, what matters here is that it stands between the project and the
	// tasks.
	if len(segs) != 5 {
		t.Fatalf("the crumb has %d steps, want project › chat › root › parent › room: %v",
			len(segs), texts)
	}
	if segs[0].text != a.place {
		t.Fatalf("the crumb's first step is %q, want the project %q", segs[0].text, a.place)
	}
	if segs[1].text != a.sessionName() {
		t.Fatalf("the crumb's second step is %q, want the chat %q", segs[1].text, a.sessionName())
	}
	if segs[2].text != "Ship the port" || segs[3].text != "Write the tree" {
		t.Fatalf("the parent steps are %q › %q, want the root › the middle", segs[2].text, segs[3].text)
	}
	if segs[4].text != "Cut the goldens" {
		t.Fatalf("the crumb's last step is %q, want the room's own title", segs[4].text)
	}
	// THE HANDLE RIDES THE CURRENT STEP and no other: a title the handle has
	// left is a title that names nothing, and a handle on a parent would be a
	// door onto a number that is not where you are.
	if segs[4].handle != " #4" {
		t.Fatalf("the room's step carries %q, want its own handle", segs[4].handle)
	}
	for i, seg := range segs[:4] {
		if seg.handle != "" {
			t.Fatalf("the parent step %q wears the handle %q", seg.text, seg.handle)
		}
		_ = i
	}
}

// A ROOM WITH NO PARENTS IS THE CHAT'S OWN CHILD: project › chat › task, and
// nothing folded in between.
func TestARootRoomsCrumbIsThreeSteps(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(1, "Ship the port")

	segs := a.crumbSegments()
	if len(segs) != 3 {
		var texts []string
		for _, seg := range segs {
			texts = append(texts, seg.text)
		}
		t.Fatalf("a root room's crumb has %d steps: %v", len(segs), texts)
	}
	if segs[2].text != "Ship the port" || segs[2].handle != " #1" {
		t.Fatalf("the root room's step is %q%q", segs[2].text, segs[2].handle)
	}
}

// AND OUT IN THE CONVERSATION THE CRUMB IS THE CHAT'S OWN TWO NAMES — the
// trail is the same fact worn by the chat, which is the whole of the change:
// the conversation has a head now.
func TestTheChatsCrumbIsProjectAndName(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)

	segs := a.crumbSegments()
	if len(segs) != 2 {
		t.Fatalf("the chat's crumb has %d steps, want project › chat", len(segs))
	}
	if segs[0].text != a.place || segs[1].text != a.sessionName() {
		t.Fatalf("the chat's crumb is %q › %q", segs[0].text, segs[1].text)
	}
	// No room, no door on the chat's own name: a press on where you already
	// are is a press on nothing.
	if segs[1].door != topDoorNone {
		t.Fatalf("the chat's own name is a door while no room is open")
	}
}

// THE DOORS ARE THE TWO STEPS THAT ARE NOT WHERE YOU ARE: the project is home,
// and the chat's own name — in a room — is the precise way out, one crumb
// level up, the same climb esc makes.
func TestTheCrumbsDoorsAreHomeAndOneLevelUp(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(4, "Cut the goldens")

	segs := a.crumbSegments()
	if segs[0].door != topDoorCrumbHome {
		t.Fatalf("the project step's door is %v, want home", segs[0].door)
	}
	if segs[1].door != topDoorCrumbChat {
		t.Fatalf("the chat step's door is %v, want the way out of the room", segs[1].door)
	}
	// The parents and the current step carry no door: a press on where you
	// already are is a press on nothing, and a parent is not a level the bar
	// climbs to.
	for _, seg := range segs[2:] {
		if seg.door != topDoorNone {
			t.Fatalf("the step %q is a door", seg.text)
		}
	}
}

// THE WALK IS CYCLE-SAFE. A roster that could name its own ancestor would be a
// roster that could not draw it either, and the crumb's guard is the same one:
// the chain stops rather than walking forever.
func TestTheCrumbSurvivesARosterThatNamesItsOwnAncestor(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	// 3's child is 4; pointing 3's parent seam back at 4 closes a loop.
	railKinship(a, 4, 3)
	a.openRoom(3, "Write the tree")

	segs := a.crumbSegments()
	if len(segs) > 6 {
		var texts []string
		for _, seg := range segs {
			texts = append(texts, seg.text)
		}
		t.Fatalf("a cycle in the parent seam walked forever: %v", texts)
	}
	if segs[len(segs)-1].text != "Write the tree" {
		t.Fatalf("the room's own step is %q", segs[len(segs)-1].text)
	}
}

// ── the ladder ──────────────────────────────────────────────────────────────

// THE GIVE-WAY WALKS ONE LADDER, in the one order the bar's spec states: the
// middles fold to one …, then the project goes, then the parent, and the
// current title truncates to its floor last of all — it is the thing the bar
// exists to confirm, so it is never dropped.
func TestTheCrumbFoldsInTheOneOrder(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(4, "Cut the goldens")
	full := a.crumbSegments()

	texts := func(segs []crumbSeg) []string {
		var out []string
		for _, seg := range segs {
			out = append(out, seg.text)
		}
		return out
	}

	// THE MIDDLES FOLD FIRST: the first and the last two are kept, and one …
	// stands for everything between, so a shortened crumb still says that it
	// was shortened.
	mid := fitTrail(full, trailMiddles)
	if got := texts(mid); len(got) != 4 || got[1] != trailEllipsis ||
		got[0] != a.place || got[2] != "Write the tree" || got[3] != "Cut the goldens" {
		t.Fatalf("the middles rung is %v", got)
	}

	// THEN THE PROJECT GOES, and the … it leaves behind is the marker of the
	// drop.
	noProj := fitTrail(full, trailNoProject)
	if got := texts(noProj); len(got) != 3 || got[0] != trailEllipsis ||
		got[1] != "Write the tree" || got[2] != "Cut the goldens" {
		t.Fatalf("the no-project rung is %v", got)
	}

	// THEN THE PARENT, the … standing for it too.
	noParent := fitTrail(full, trailNoParent)
	if got := texts(noParent); len(got) != 2 || got[0] != trailEllipsis || got[1] != "Cut the goldens" {
		t.Fatalf("the no-parent rung is %v", got)
	}

	// AND THE TITLE TRUNCATES LAST, to its floor, with the handle still
	// attached — a title the handle has left is a title that names nothing.
	title := fitTrail(full, trailTitle)
	last := title[len(title)-1]
	if last.handle != " #4" {
		t.Fatalf("the truncated title lost its handle: %q", last.handle)
	}
	if w := len([]rune(last.text)); w > roomHeadFloor {
		t.Fatalf("the truncated title is %d cells, over the %d-cell floor", w, roomHeadFloor)
	}
}

// A SHORT TRAIL SKIPS THE RUNGS IT HAS NOTHING FOR: two steps have no middles
// to fold and no parent to drop, and the ladder leaves them alone until the
// title itself must give.
func TestAShortCrumbSkipsTheRungsItHasNothingFor(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(1, "Ship the port")
	full := a.crumbSegments()

	for _, rung := range []trailRung{trailMiddles, trailNoProject, trailNoParent} {
		got := fitTrail(full, rung)
		if len(got) != len(full) {
			var texts []string
			for _, seg := range got {
				texts = append(texts, seg.text)
			}
			t.Fatalf("rung %v folded a three-step crumb: %v", rung, texts)
		}
	}
}

// ── what the bar costs ──────────────────────────────────────────────────────

// THE BAR IS TWO ROWS WHENEVER IT STANDS — itself and the legend's own
// hairline under it — and the body pays exactly that, through the one door
// every region asks.
func TestTheTopBarCostsTheBodyTwoRows(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 120, 24
	a.touch()

	if got := a.headHeight(); got != 2 {
		t.Fatalf("the top bar costs %d rows, want the bar and its rule", got)
	}
	if got := a.topHeight(); got != a.headHeight() {
		t.Fatalf("the body's top inset is %d, the bar's own height is %d", got, a.headHeight())
	}
	rows := a.topBarRows(a.width)
	if len(rows) != 2 {
		t.Fatalf("the bar draws %d rows, want the bar and its rule", len(rows))
	}
	if strings.TrimSpace(plain(rows[1])) == "" {
		t.Fatal("the rule under the bar is blank")
	}
	if strings.Contains(plain(rows[1]), a.sessionName()) {
		t.Fatalf("the rule repeats the bar's words:\n%q", plain(rows[1]))
	}
}

// AND A FRAME TOO SHORT OR TOO NARROW FOR IT PAYS NOTHING: the breathing law
// the blank above the draft stands on is the bar's own floor, and below the
// phone tier the deck's top row takes the crumb instead.
func TestAFrameWithoutBreathingRoomHasNoBar(t *testing.T) {
	a, _, _ := taskApp(t)

	// Too SHORT: a terminal too short for the blank above the draft is too
	// short for a head (view.go's [roomyFloor]).
	a.width, a.height = 120, 5
	a.touch()
	if a.breathingRows() != 0 {
		t.Fatal("five rows still breathes; the floor moved and this test must move with it")
	}
	if got := a.headHeight(); got != 0 {
		t.Fatalf("a frame with no breathing room still pays %d rows for a bar", got)
	}
	if rows := a.topBarRows(a.width); rows != nil {
		t.Fatalf("a frame with no breathing room drew a bar: %q", rows)
	}

	// Too NARROW: below the phone tier the frame is a deck, and the deck's top
	// row takes the crumb.
	a.width, a.height = 44, 24
	a.touch()
	if layoutTier(a.width) != tierPhone {
		t.Skip("44 columns is not the phone tier on this build")
	}
	if got := a.headHeight(); got != 0 {
		t.Fatalf("the phone tier still pays %d rows for a bar", got)
	}
	if rows := a.topBarRows(a.width); rows != nil {
		t.Fatalf("the phone tier drew a bar over the deck: %q", rows)
	}
}

// THE BAR FITS THE FRAME IT IS DRAWN ON, at every width it stands at — the
// give-way is the bar's own, and a row that overflowed would be the ladder
// lying.
func TestTheBarFitsAtEveryWidthItStandsAt(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(4, "Cut the goldens")
	a.height = 24

	for _, width := range []int{200, 120, 90, 70, 60, 52} {
		a.width = width
		a.touch()
		if !a.topBarShowing(width) {
			continue
		}
		bar := plain(a.topBarWord(width))
		if got := len([]rune(bar)); got > width {
			t.Fatalf("at %d columns the bar is %d wide:\n%q", width, got, bar)
		}
		// The room's own title survives every rung — it is the thing the bar
		// exists to confirm.
		if !strings.Contains(bar, "Cut the goldens") && width >= 60 {
			t.Fatalf("at %d columns the room's title is gone:\n%q", width, bar)
		}
	}
}

// THE ROOM'S FACTS RIDE THE BAR AS TRIMMINGS, each dropped when nobody
// published it — an empty segment is not a dim one — and the whole left
// cluster is one lit element while a room is open.
func TestTheBarCarriesTheRoomsFactsAsTrimmings(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(3, "Write the tree")
	a.width, a.height = 160, 24
	a.touch()

	bar := plain(a.topBarWord(a.width))
	node := a.tasks[3]
	for _, want := range []string{
		"Write the tree", "#3",
		a.roomStateWord(node), a.roomClock(node),
	} {
		if want == "" {
			t.Fatal("the fixture published no state or clock")
		}
		if !strings.Contains(bar, want) {
			t.Fatalf("the bar is missing %q:\n%q", want, bar)
		}
	}
	// AND A NODE THAT PUBLISHED NO PRICE SPENDS NO COLUMNS ON ONE.
	if a.roomSpend(node) != "" {
		t.Fatal("the fixture node has a price; the empty-segment law needs one without")
	}
	if strings.Contains(bar, "$") {
		t.Fatalf("the bar drew a spend nobody published:\n%q", bar)
	}
}

// A CONSENT QUESTION UP DROPS THE WHOLE LEFT CLUSTER TO DIM — the same fall a
// finished fact gets, because the bar is one sentence about one thing and the
// question is now the lit thing. The question is the room's own approval
// surface (roomapproval.go): a harness node at the phase where the only
// remaining step is the person's, with its design card on the feed.
func TestAConsentQuestionDropsTheBarsClusterToDim(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 160, 24
	a.touch()

	// A harness node, running, with its finished design on the feed — the
	// roomapproval surface's own two facts.
	a.taskUpdate(update(7, "Research a topic", session.TaskRunning,
		session.TaskNotice{Kind: session.TaskKindHarness}))
	a.designEvent(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "research a topic"})
	page := designedPage()
	a.designEvent(session.Event{Kind: session.EventHarnessDesignDone, ID: 7, Harness: &page})
	a.openRoom(7, "Research a topic")

	lit := a.topBarWord(a.width)
	if !strings.Contains(lit, sgr256(hueAccent)) {
		t.Fatalf("the open room's bar is not lit:\n%q", lit)
	}
	if a.roomApprovalHeight() != 0 {
		t.Fatal("the node is asking before the phase says so; the fixture is wrong")
	}

	// The phase where the only remaining step is the person's.
	a.taskUpdate(update(7, "Research a topic", session.TaskRunning,
		session.TaskNotice{Kind: session.TaskKindHarness, Doing: session.HarnessPhaseAsking}))
	if a.roomApprovalHeight() == 0 {
		t.Fatal("the room is not asking; the fixture is wrong")
	}
	dimmed := a.topBarWord(a.width)
	if strings.Contains(dimmed, sgr256(hueAccent)) {
		t.Fatalf("the bar is still lit over a consent question:\n%q", dimmed)
	}
}

// THE CHAT AT REST WEARS INK AND DIM AND NEVER ACCENT: nothing about a
// conversation at rest is lit.
func TestTheChatsBarIsNeverLit(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 160, 24
	a.touch()

	bar := a.topBarWord(a.width)
	if strings.Contains(bar, sgr256(hueAccent)) {
		t.Fatalf("the chat at rest has a lit bar:\n%q", bar)
	}
	if !strings.Contains(plain(bar), a.place) {
		t.Fatalf("the chat's bar does not say where you are:\n%q", plain(bar))
	}
}

// THE LEAD GLYPH IS THE ONE MARK THAT MOVES: the conversation's own `·` at
// rest, and the node's state glyph in a room — first, because the eye lands on
// the thing that changes.
func TestTheBarsLeadGlyphIsTheRoomsMark(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 160, 24
	a.touch()

	if bar := plain(a.topBarWord(a.width)); !strings.HasPrefix(bar, "·") {
		t.Fatalf("the chat's bar does not open with its own dot:\n%q", bar)
	}

	railRun(a)
	a.openRoom(2, "Read the law") // done: a still glyph, not a spinner
	bar := plain(a.topBarWord(a.width))
	if !strings.HasPrefix(bar, glyphDone) {
		t.Fatalf("the room's bar does not open with the node's mark:\n%q", bar)
	}
}

// A NODE THAT SETTLED KEEPS ITS MARK ON THE BAR — the same third state the
// rail draws, on the room's own head.
func TestTheBarWearsTheSettledMark(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.openRoom(2, "Read the law")
	a.width, a.height = 160, 24
	a.touch()

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, "Read the law",
		session.TaskFailed, session.TaskNotice{})})
	if bar := plain(a.topBarWord(a.width)); !strings.HasPrefix(bar, glyphBad) {
		t.Fatalf("a failed node's bar opens with %q, want the bad mark:\n%q",
			string([]rune(bar)[0]), bar)
	}
}
