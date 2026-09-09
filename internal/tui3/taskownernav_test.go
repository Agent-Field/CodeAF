package tui3

// ── ONE ROW, ONE PIECE OF WORK, ONE DOOR ────────────────────────────────────
//
// Every row of work this surface draws is something a person can aim at, and
// what it opens is that row's own task and never another's. Both halves failed,
// in opposite directions, and the two failures look identical from the outside —
// a person clicks a task and does not get that task.
//
//   - A row another WINDOW is running took no cursor at all, so pressing it did
//     nothing and there was no way to tell a refusal from a broken screen.
//   - A row another CONVERSATION ran, numbered the same as one of this session's
//     and named the same, resolved to THIS session's node — so pressing it
//     opened a room, drew a transcript, and the transcript was a different task.
//
// The second is the worse of the two: a wrong page that looks right. Node ids
// restart with every conversation (internal/session's task_index.go states it on
// [session.TaskIndexEntry.ID]), so the pair that identifies a row is the
// conversation and the id — and this file holds that pair to it.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ownerLab is a window that KNOWS WHICH CONVERSATION IT IS, holding this
// session's node 7, with one row of the project's record that another
// conversation wrote under the same number and the same words.
//
// The window's own id is the session FOLDER's name, which is what every row of
// the record is stamped with ([app.taskSheetSelfID]) — a test that left the
// journal path empty would be testing the case where nothing can be compared.
func ownerLab(t *testing.T, theirTitle string) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	a.file = filepath.Join(t.TempDir(), "session-mine", "session.jsonl")
	a.title = "the conversation I am sitting in"
	landed := theirTask("7", "session-theirs", theirTitle, string(session.TaskDone))
	landed.EndedAt = taskFixtureNow.Add(-2 * time.Hour)
	landed.Outcome = "the other conversation's own outcome"
	a.comp.tasks = []session.TaskIndexEntry{landed}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the tasks place opened with no rows on it")
	}
	return a
}

// pointAtOwner parks the page's cursor on the row one CONVERSATION owns, and
// hands back the row it landed on. It walks the layout's own stops rather than
// counting drawn lines, because that is what the keyboard and the pointer both
// resolve against.
func pointAtOwner(t *testing.T, a *app, owner string) tasksItem {
	t.Helper()
	r := a.tasksFiltered()
	width, _ := a.size()
	lines := r.lay(width)
	for at := range lines {
		item, ok := r.at(lines, at)
		if ok && item.entry.SessionID == owner {
			a.taskSheet.cursor = at
			return item
		}
	}
	t.Fatalf("no row on the page belongs to %q:\n%s", owner, taskSheetText(a))
	return tasksItem{}
}

// TWO CONVERSATIONS' TASK 7 ARE TWO PIECES OF WORK, AND THE PAGE OPENS THE ONE
// UNDER THE CURSOR.
//
// The two rows here are as alike as the record can make them: the same id, the
// same title, the same label. Everything that told them apart is the owner, and
// the owner is what the id-and-title rule ignored — so the landed row of a
// conversation that closed months ago opened this window's live node 7.
func TestTwoOwnersWithTheSameNumberEachOpenTheirOwnTask(t *testing.T) {
	a := ownerLab(t, "Fix the nil-map crash")

	// THE OTHER CONVERSATION'S ROW OPENS ITS RECORD, and never a room.
	theirs := pointAtOwner(t, a, "session-theirs")
	if node := a.taskSheetNodeFor(&theirs.entry); node != nil {
		t.Fatalf("another conversation's task 7 resolved to this session's node %d", node.id)
	}
	a.taskSheetEnter()
	if a.roomOpen() {
		t.Fatalf("another conversation's row opened this session's room %d", a.room.id)
	}
	if !a.taskSheet.detailOn || a.taskSheet.detail.SessionID != "session-theirs" {
		t.Fatalf("the row opened no card of its own: %+v", a.taskSheet.detail)
	}
	if !strings.Contains(taskSheetText(a), "the other conversation's own outcome") {
		t.Fatalf("the card is not the other conversation's:\n%s", taskSheetText(a))
	}
	a.taskCardKey("esc")

	// AND THIS SESSION'S OWN ROW STILL OPENS ITS ROOM. The veto is about the
	// owner and nothing else; work this window is holding is unchanged.
	mine := pointAtOwner(t, a, "session-mine")
	if a.taskSheetNodeFor(&mine.entry) == nil {
		t.Fatal("this window no longer recognises its own node 7")
	}
	a.taskSheetEnter()
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatalf("this session's own row did not open its room: open=%t", a.roomOpen())
	}
}

// AND THE FOOT SAYS WHICH OF THE TWO DOORS IS UNDER THE CURSOR. A page that
// promised `enter open its room` over a row with no room is the page lying about
// its own key, which is the same defect as the row that could not be pressed.
func TestTheFootNamesTheDoorTheRowUnderTheCursorActuallyHas(t *testing.T) {
	a := ownerLab(t, "Fix the nil-map crash")

	pointAtOwner(t, a, "session-theirs")
	if line := a.taskSheetKeysLine(); !strings.Contains(line, tasksEnterInsideWord) {
		t.Fatalf("over another conversation's row the foot says %q", line)
	}
	pointAtOwner(t, a, "session-mine")
	if line := a.taskSheetKeysLine(); !strings.Contains(line, tasksEnterRoomWord) {
		t.Fatalf("over this session's own row the foot says %q", line)
	}
}

// THE MOUSE AND THE KEYBOARD OPEN THE SAME TASK, at every width the page is read
// at. They are two hands on one door, and a surface where the pointer reached a
// different task from `enter` would be a surface with two answers to one
// gesture.
func TestClickAndEnterOpenTheSameOwnersTaskAtEveryWidth(t *testing.T) {
	for _, width := range []int{44, 80, 120} {
		t.Run(itoa(width), func(t *testing.T) {
			a := ownerLab(t, "Fix the nil-map crash")
			a.width = width

			// The keyboard, on this session's own row.
			pointAtOwner(t, a, "session-mine")
			a.taskSheetEnter()
			if !a.roomOpen() || a.room.id != 7 {
				t.Fatalf("enter did not open this session's room at %d columns", width)
			}
			a.closeRoom()

			// And the pointer, aimed at the SCREEN ROW that row is drawn on.
			if !openTaskPlaceWithRows(a) {
				t.Fatal("the tasks place would not reopen")
			}
			row := ownerHitRow(t, a, "session-mine")
			a.taskSheetPress(2, row)
			if !a.roomOpen() || a.room.id != 7 {
				t.Fatalf("a click did not open this session's room at %d columns:\n%s",
					width, taskSheetText(a))
			}
			a.closeRoom()

			// And the same pair over the row this window does not own: both open
			// the record and neither opens a room.
			if !openTaskPlaceWithRows(a) {
				t.Fatal("the tasks place would not reopen")
			}
			row = ownerHitRow(t, a, "session-theirs")
			a.taskSheetPress(2, row)
			if a.roomOpen() {
				t.Fatalf("a click on another conversation's row opened room %d at %d columns",
					a.room.id, width)
			}
			if a.taskSheet.detail.SessionID != "session-theirs" {
				t.Fatalf("a click opened %+v at %d columns", a.taskSheet.detail, width)
			}
		})
	}
}

// ownerHitRow is the screen row one CONVERSATION's task is drawn on, resolved
// through the page's own hit map — which is what a press will name. At
// [tierPhone] a row is a two-line card, so counting lines would aim at the wrong
// one.
func ownerHitRow(t *testing.T, a *app, owner string) int {
	t.Helper()
	item := pointAtOwner(t, a, owner)
	width, height := a.size()
	lines, hits, _, _ := a.taskSheetFrame(width, height)
	for y := range lines {
		if y < len(hits) && hits[y].kind == taskSheetHitRow && hits[y].index == a.taskSheet.cursor {
			return y
		}
	}
	t.Fatalf("%q's row (%s) is on no pressable line:\n%s",
		owner, tasksLabel(item.entry), strings.Join(drawnRows(lines), "\n"))
	return -1
}

// ── THE OWNER IS NAMED, AND IT IS NEVER THIS CONVERSATION BY DEFAULT ────────

// A ROW'S CONVERSATION IS THE ONE THAT RAN IT. Where the scan has met that
// conversation the row wears its title; where it has not, the row says nothing —
// and never falls through to the conversation the person is sitting in, which is
// the one answer that is certainly wrong.
func TestARowsConversationIsItsOwnAndNeverThisOneByDefault(t *testing.T) {
	mine := tasksMine{row: session.SessionRow{ID: "session-mine", Title: "what I am doing"}}
	next := session.SessionRow{ID: "session-theirs", Title: "next door"}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{next}}}}

	if got := tasksRowFor(world, mine, session.TaskIndexEntry{ID: "7", SessionID: "session-theirs"}); got.Title != "next door" {
		t.Fatalf("a row the scan has met is labelled %q", got.Title)
	}
	// The scan has never met this one, and the honest answer is silence.
	unmet := tasksRowFor(session.World{}, mine, session.TaskIndexEntry{ID: "7", SessionID: "session-theirs"})
	if unmet.Title != "" || unmet.ID != "" {
		t.Fatalf("a row of another conversation was credited to %+v", unmet)
	}
	// This window's own work still wears this window's own label, and so does a
	// row that names no conversation at all — a row from before ids were written
	// down, whose only provenance is the project it was found in.
	for _, owner := range []string{"session-mine", ""} {
		if got := tasksRowFor(session.World{}, mine, session.TaskIndexEntry{ID: "7", SessionID: owner}); got.Title != "what I am doing" {
			t.Fatalf("a row owned by %q is labelled %q, want this conversation's own", owner, got.Title)
		}
	}
}

// AND THE CARD SAYS THE SAME THING THE ROW DOES. `out of <this conversation>` on
// a card over somebody else's work is the one sentence on that page a person
// would act on, and it named the wrong conversation.
func TestTheCardOverAnotherConversationsWorkDoesNotClaimThisOne(t *testing.T) {
	a := ownerLab(t, "Fix the nil-map crash")
	pointAtOwner(t, a, "session-theirs")
	a.taskSheetEnter()
	if card := taskSheetText(a); strings.Contains(card, a.title) {
		t.Fatalf("the card credits another conversation's work to this one:\n%s", card)
	}
}

// ── THE SAME LAWS THROUGH THE LOCAL ENGINE WIRE ─────────────────────────────

// The default local window talks to its engine through the same wire as SSH
// (localtaskroom_test.go). Everything above is the surface's own arithmetic;
// this is the same journey with a real client behind it, because that is the
// window a person actually has open.
func TestTheLocalWireWindowOpensItsOwnTaskAndTheRecoveryCardForAnothersInOneSession(t *testing.T) {
	a, engine := localTaskRoomLab(t)
	a.away = elsewhereCache{read: true, at: a.now(), held: session.NewElsewhere(a.now(),
		map[string]string{"the-other-window": "docs pass"},
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))}

	// THIS WINDOW'S OWN TASK 7 OPENS AND IS STEERED, over the wire.
	clickRail(t, a, 0)
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatalf("the local wire window did not open its own task:\n%s", taskText(a))
	}
	a.input.setText("Keep the existing API compatible.")
	drive(t, a, key("enter"))

	// AND THE OTHER WINDOW'S TASK 7 — the same number, a different owner — opens
	// the recovery card and never this room.
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the tasks place refused to open over another window's work")
	}
	away := pointAtOwner(t, a, "the-other-window")
	if !away.away {
		t.Fatalf("the other window's row is not filed as another window's: %+v", away)
	}
	a.taskSheetEnter()
	if !a.taskSheet.awayOwner.on {
		t.Fatalf("the other window's row opened no recovery card:\n%s", taskSheetText(a))
	}
	for _, want := range []string{taskAwayCardWhere("docs pass"), taskAwayCardNoRoom} {
		if !strings.Contains(taskSheetText(a), want) {
			t.Fatalf("the card never says %q:\n%s", want, taskSheetText(a))
		}
	}

	// NAVIGATING DID NOT TOUCH THE WORK. The room this window is holding is still
	// its room, the correction that was already sent was sent once, and nothing
	// about opening a page onto somebody else's task steered anything.
	if a.room == nil || a.room.id != 7 {
		t.Fatal("opening another window's card closed the room this window was in")
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.steered) != 1 || engine.ids[0] != 7 {
		t.Fatalf("navigation changed what the engine was told: %v %v", engine.ids, engine.steered)
	}
}
