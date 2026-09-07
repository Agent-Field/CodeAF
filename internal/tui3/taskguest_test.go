package tui3

// ── A PAGE READ THROUGH SOMEBODY ELSE'S CONVERSATION ────────────────────────
//
// The tasks place can now open a task that belongs to a conversation the engine
// is running rather than this window ([app.openOwnerRoom]). Two things about
// that page are dangerous rather than merely wrong, and this file holds both.
//
//   - THE IDS COLLIDE. Task ids restart with every conversation, so the page is
//     nearly always standing beside a local task of the same number — a real,
//     healthy piece of work with a different name, a different model and a
//     different state. Every reader on the page goes through [app.roomNode], and
//     every ACTION on it (stop, model, effort, approval, steer) has to be absent
//     rather than aimed at that local row.
//   - THE ATTACH IS A ROUND TRIP. It is made off the program loop, so a person
//     who presses a row and then leaves has an answer arriving for a page they
//     are not on. It must close its own connection and open nothing.

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// guestLab is a window holding its own task 7 — running, on a model, with a name
// — beside one row of another conversation's task 7 wearing the same name.
//
// THE TWO ROWS ARE AS ALIKE AS THE RECORD CAN MAKE THEM, deliberately: the only
// thing that tells them apart is the conversation, which is the fact every door
// below has to be asked about.
func guestLab(t *testing.T) (*app, *guestDoor) {
	t.Helper()
	a, _, _ := roomApp(t)
	a.title = "the conversation I am sitting in"
	// This window's own task 7: running, named, on a model of its own.
	a.taskUpdate(update(7, "Port the parser", session.TaskRunning, session.TaskNotice{}))
	if node := a.tasks[7]; node != nil {
		node.model = "mine/model"
	}
	door := &guestDoor{}
	a.openTaskOwner = door.open
	a.away = elsewhereCache{read: true, at: a.now(), held: session.NewElsewhere(a.now(),
		map[string]string{"the-other-window": "docs pass"},
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Port the parser", State: string(session.TaskRunning)}))}
	return a, door
}

// guestDoor stands in for the engine seam. It records what was asked for, hands
// back whatever the test set, and counts the closes — because "the view was given
// back" is the assertion, not an implementation detail.
type guestDoor struct {
	asked   []string
	session string
	err     error
	tail    string
	closed  int
	read    int
	// notices is the OWNER'S OWN LANE, and it is nil until a test asks for one —
	// which is the honest default here, because a door that cannot offer it is a
	// real door ([TaskOwnerView.Watch] says so) and the page has to hold up
	// without it.
	notices chan session.Event
	// left counts the way out of that lane being taken, so "the subscription was
	// given back with the connection" is an assertion rather than a hope.
	left int
}

// watching arms this door with the owner's task lane and hands the test the end
// it writes into.
func (d *guestDoor) watching() chan session.Event {
	d.notices = make(chan session.Event, 8)
	return d.notices
}

func (d *guestDoor) open(ask TaskOwnerAsk) (TaskOwnerView, error) {
	d.asked = append(d.asked, ask.Session)
	if d.err != nil {
		return TaskOwnerView{}, d.err
	}
	file := d.session
	if file == "" {
		file = ask.Session
	}
	view := TaskOwnerView{
		Session: file,
		Room: func(uint64, int) (session.TaskRecord, error) {
			d.read++
			return session.TaskRecord{}, nil
		},
		Close: func() error { d.closed++; return nil },
	}
	if d.notices != nil {
		lane := d.notices
		view.Watch = func() (<-chan session.Event, func()) {
			// THE WAY OUT CLOSES THE LANE, exactly as the real one does
			// ([remote.Agent.WatchTaskUpdates] finishes the stream): a command
			// parked on a channel nobody will ever close is a goroutine held for
			// the life of the process, and this suite counts those.
			return lane, func() {
				d.left++
				if d.notices != nil {
					close(d.notices)
					d.notices = nil
				}
			}
		}
	}
	return view, nil
}

// ownerSays delivers one of the owner's notices to the page the way the program
// loop would, and hands back whatever the page asked to happen next.
func ownerSays(t *testing.T, a *app, ev session.Event) tea.Cmd {
	t.Helper()
	if a.room == nil {
		t.Fatal("no page for the owner to say anything to")
	}
	return a.tookGuestNotice(taskGuestNoticeMsg{gen: a.room.gen, ev: ev})
}

// THE STATE ON A READING PAGE COMES FROM THE CONVERSATION THAT OWNS THE WORK.
//
// It used to come from [app.taskSheetAwayRows] — the presence directory of the
// workspace THIS window is in — and that answered two different questions wrong
// at once. A conversation in another project writes its presence somewhere this
// window never reads, so the lookup found nothing and the page called live work
// finished the moment it opened; and even in the same project, a row missing from
// a file written on a beat is not a row that has landed.
func TestAGuestPageTakesItsStateFromTheOwnerAndNotFromThisWorkspacesPresence(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)

	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	// THE ROW SAID RUNNING AND NOTHING HAS CONTRADICTED IT. The presence reading
	// this window holds is deliberately emptied first: if the page were still
	// asking it, this is the moment it would decide the work was over.
	a.away = elsewhereCache{read: true, at: a.now()}
	if a.room.done {
		t.Fatal("the page read an empty local presence directory as the work being finished")
	}
	if node := a.roomNode(); node == nil || node.state != session.TaskRunning {
		t.Fatalf("the page lost the state the row it opened from carried: %+v", a.roomNode())
	}
	// AND A PAGE WITH THE OWNER'S LANE MAKES NO CAVEAT: it is about to be told.
	if a.roomGuestStale() {
		t.Fatal("a page holding the owner's own lane claims it cannot ask")
	}

	// THE OWNER SAYS THE WORK LANDED, and that — and only that — moves the page.
	next := ownerSays(t, a, session.Event{
		Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: 7, Title: "Port the parser", State: session.TaskDone},
	})
	if !a.room.done {
		t.Fatal("the owner said its work landed and the page went on saying it was running")
	}
	if node := a.roomNode(); node == nil || node.state != session.TaskDone {
		t.Fatalf("the page's own node did not take the owner's state: %+v", a.roomNode())
	}
	// AND THIS WINDOW'S OWN TASK 7 IS UNTOUCHED BY ALL OF IT.
	if node := a.tasks[7]; node == nil || node.state != session.TaskRunning {
		t.Fatalf("the owner's notice moved the local task wearing the same number: %+v", a.tasks[7])
	}
	if next == nil {
		t.Fatal("the page stopped listening to the conversation it is reading")
	}
	// AND LEAVING GIVES BACK THE SUBSCRIPTION AND THE CONNECTION, each once.
	a.closeRoom()
	if door.left != 1 || door.closed != 1 {
		t.Fatalf("leaving the page left %d subscriptions and %d connections behind", 1-door.left, 1-door.closed)
	}
}

// A NOTICE ABOUT ANOTHER OF THAT CONVERSATION'S TASKS CHANGES NOTHING HERE. The
// lane carries the owner's WHOLE roster — it replays every row when it opens —
// so a page that took the last notice it saw would draw a sibling's state under
// this task's name.
func TestAGuestPageIgnoresTheOwnersNoticesAboutItsOtherWork(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)

	ownerSays(t, a, session.Event{
		Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: 9, Title: "Something else", State: session.TaskDone},
	})
	if a.room.done {
		t.Fatal("a notice about the owner's OTHER task settled this page")
	}
	if node := a.roomNode(); node == nil || node.title != "Port the parser" {
		t.Fatalf("the page took another task's name: %+v", a.roomNode())
	}
}

// A DOOR THAT CANNOT ASK THE OWNER SAYS SO ON THE PAGE. The rows above it were
// really read; what cannot be known is whether the state beside them is still
// true, and the page says which of the two it is rather than quietly presenting
// a photograph as a present.
func TestAReadingPageWithNoOwnerLaneSaysTheStateIsTheLastItWasTold(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)

	if !a.roomGuestStale() {
		t.Fatal("a page with no way to ask its owner does not know it")
	}
	if body := roomText(a); !strings.Contains(body, roomGuestStaleWord) {
		t.Fatalf("the page makes no caveat about a state it cannot refresh:\n%s", body)
	}
}

// theirLiveSession is the conversation the other window is in, as the world scan
// found it: a folder on this machine with a transcript in it.
//
// THE TRANSCRIPT IS WHAT MAKES AN ATTACH POSSIBLE AT ALL. A row another window is
// running carries a state and a title and nothing else — nothing lands an index
// row until the work finishes — so the only place the conversation's own file can
// come from is the walk of the disk ([tasksRowFor]), and a row whose conversation
// the walk never met is a row this window has no way to reach.
var theirLiveSession = session.SessionRow{
	ID:         "the-other-window",
	Title:      "docs pass",
	Transcript: "/w/.aforge/v3/sessions/-w/the-other-window/session.jsonl",
	ProjectDir: "/w",
}

// awayRowOf is the row the OTHER window is running, off the page's own reading.
func awayRowOf(t *testing.T, a *app) tasksItem {
	t.Helper()
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the tasks place opened with no rows on it")
	}
	// The walk of the disk, as this fixture's machine would have answered it.
	a.taskSheet.world = session.World{Projects: []session.Project{
		{Sessions: []session.SessionRow{theirLiveSession}},
	}}
	r := a.tasksFiltered()
	width, _ := a.size()
	lines := r.lay(width)
	for at := range lines {
		item, ok := r.at(lines, at)
		if ok && item.away {
			a.taskSheet.cursor = at
			return item
		}
	}
	t.Fatalf("no row on the page belongs to another window:\n%s", taskSheetText(a))
	return tasksItem{}
}

// enterAway presses the away row and settles whatever the attach handed back.
func enterAway(t *testing.T, a *app) {
	t.Helper()
	awayRowOf(t, a)
	cmd := a.taskSheetEnter()
	if cmd == nil {
		t.Fatal("enter over another window's running work did nothing at all")
	}
	msg, ok := cmd().(taskOwnerMsg)
	if !ok {
		t.Fatalf("enter did not ask the engine for the owner: %T", cmd())
	}
	a.tookTaskOwner(msg)
}

// THE PAGE IS THE OTHER CONVERSATION'S TASK AND THE LOCAL ONE IS UNTOUCHED.
func TestAGuestPageNeverDescribesOrTouchesTheLocalTaskWearingTheSameNumber(t *testing.T) {
	a, door := guestLab(t)
	enterAway(t, a)

	if !a.roomOpen() || !a.roomIsGuest() {
		t.Fatalf("pressing another conversation's running work opened no guest page: room=%v", a.room)
	}
	// AND IT WAS ASKED FOR THE OWNER'S OWN TRANSCRIPT, once.
	if len(door.asked) != 1 || door.asked[0] != theirLiveSession.Transcript {
		t.Fatalf("the engine was asked for %v, want one ask for %q",
			door.asked, theirLiveSession.Transcript)
	}
	// THE NODE IS THE PAGE'S OWN AND NOT THE GRAPH'S. Same number, different
	// object — and the local one is still exactly what it was.
	node := a.roomNode()
	if node == nil {
		t.Fatal("the guest page has no node at all, so its header has nothing to say")
	}
	if node == a.tasks[7] {
		t.Fatal("the guest page resolved its id against this window's own graph")
	}
	if node.model != "" {
		t.Fatalf("the guest page claims the local task's model %q", node.model)
	}
	// EVERY ACTION IS ABSENT RATHER THAN AIMED SOMEWHERE SAFE.
	if a.roomModelMovable() {
		t.Fatal("the guest page offers to move a model, which would retarget the local task")
	}
	if !a.stopHere().empty() {
		t.Fatal("the guest page offers a stop, which would end the local task")
	}
	if _, ok := a.effortTaskHere(); ok {
		t.Fatal("the guest page offers an effort rung, which would set the local task's")
	}
	if a.roomApprovalCard() != nil || a.roomApprovalAsking() {
		t.Fatal("the guest page drew this window's own approval card")
	}
	if _, ok := a.roomSteerDoors(); ok {
		t.Fatal("the guest page has a steering door, which would steer the local task")
	}
	// AND THE BOX SAYS SO RATHER THAN OFFERING TO STEER.
	box, _, _ := a.inputBlock(80)
	if lane := plain(strings.Join(a.roomSteerLaneRows(box, 80), "")); !strings.Contains(lane, roomGuestLane) {
		t.Fatalf("the guest composer offers %q", lane)
	}
	// A LINE TYPED ANYWAY STAYS IN THE BOX AND REACHES NOTHING.
	a.input.setText("stop what you are doing")
	a.steer()
	if got := string(a.input.value); got != "stop what you are doing" {
		t.Fatalf("the guest page spent the words: %q", got)
	}
	// THE TRAIL NAMES WHOSE CONVERSATION THIS IS.
	if trail := a.roomTrail(); !strings.HasPrefix(trail, roomGuestOwnerWord) {
		t.Fatalf("the guest page's trail hangs off this conversation: %q", trail)
	}

	// AND LEAVING GIVES BACK THIS VIEW'S CONNECTION AND NOTHING ELSE.
	a.closeRoom()
	if door.closed != 1 {
		t.Fatalf("closing the guest page released the view %d times", door.closed)
	}
	if node := a.tasks[7]; node == nil || node.state != session.TaskRunning || node.model != "mine/model" {
		t.Fatalf("the local task 7 was changed by a page that was never about it: %+v", node)
	}
}

// THE ATTACH IS A ROUND TRIP, AND AN ANSWER NOBODY WANTS IS CLOSED ON ARRIVAL.
//
// A person presses a row, changes their mind and leaves. The engine answers a
// moment later. Opening a page then would replace whatever they went to, and
// dropping the answer would leave a reader attached to somebody else's engine for
// the life of this window.
func TestAnAttachThatLandsAfterThePersonMovedOnClosesItselfAndOpensNothing(t *testing.T) {
	a, door := guestLab(t)
	awayRowOf(t, a)
	cmd := a.taskSheetEnter()
	if cmd == nil {
		t.Fatal("enter asked the engine for nothing")
	}
	// THE KEYSTROKE DID NOT WAIT. The page is still up and answering, which is the
	// whole point of doing this off the loop.
	if !a.at(pageTasks) {
		t.Fatal("the attach opened a page before the engine had answered")
	}
	msg, ok := cmd().(taskOwnerMsg)
	if !ok {
		t.Fatalf("enter did not ask the engine for the owner: %T", cmd())
	}
	// ...and in the meantime they pressed something else.
	a.taskOwnerGen++
	a.tookTaskOwner(msg)
	if a.roomOpen() {
		t.Fatal("a stale attach opened a page over whatever the person went to")
	}
	if door.closed != 1 {
		t.Fatalf("a stale attach released its view %d times, want once", door.closed)
	}
}

// A CONVERSATION THAT CAME BACK UNDER THE WRONG NAME DRAWS NOTHING.
//
// This is the failure the whole owner lane exists to end, one layer down: an
// engine that answered about a different conversation would put one task's
// transcript under another task's name, with every id on the page read against
// the wrong graph.
func TestAnEngineThatOpensADifferentConversationIsRefusedAndReleased(t *testing.T) {
	a, door := guestLab(t)
	door.session = "/somewhere/else/session.jsonl"
	enterAway(t, a)

	if a.roomOpen() {
		t.Fatalf("a view onto the wrong conversation drew a page:\n%s", taskText(a))
	}
	if door.closed != 1 {
		t.Fatalf("the refused view was released %d times, want once", door.closed)
	}
	if !strings.Contains(a.pageMsg, taskOwnerWrongWord) {
		t.Fatalf("the page says %q about a view it refused", a.pageMsg)
	}
}

// AND A DOOR THAT REFUSES LEAVES THE PERSON A PAGE AND A REASON.
//
// The row is still there to press again, the card says where the work is, and the
// engine's own sentence is beside it rather than swallowed.
func TestAnAttachTheEngineRefusesFallsBackToTheCardWithTheReason(t *testing.T) {
	a, door := guestLab(t)
	door.err = errors.New("that conversation is not open on this machine")
	enterAway(t, a)

	if a.roomOpen() {
		t.Fatal("a refused attach opened a page anyway")
	}
	if !a.taskSheet.awayOwner.on {
		t.Fatalf("a refused attach left the person nothing:\n%s", taskSheetText(a))
	}
	for _, want := range []string{taskOwnerBusyWord, "not open on this machine"} {
		if !strings.Contains(a.pageMsg, want) {
			t.Fatalf("the page says %q and never %q", a.pageMsg, want)
		}
	}
	if door.closed != 0 {
		t.Fatalf("a door that handed back nothing was closed %d times", door.closed)
	}
}

// THE CONVERSATION UNDER A GUEST PAGE CAN BE REPLACED WHILE IT IS BEING READ.
//
// The engine refuses the next read by identity (internal/remote's
// [Session.serving]), and that is not a failure to read: there is nothing more to
// have. So the page keeps everything it did read, says the one true last thing,
// and STOPS ASKING — a retry here would be a beat forever against a conversation
// that has already given its final answer.
func TestAGuestPageWhoseConversationWasReplacedKeepsWhatItReadAndStops(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	if !a.roomIsGuest() {
		t.Fatal("no guest page to lose")
	}
	gen := a.room.gen

	again := a.farRoomRead(roomRecordMsg{
		gen: gen,
		err: errors.New("engine: that conversation is not open here any more"),
	})
	if !a.room.guest.lost {
		t.Fatal("the page did not take the engine's final answer")
	}
	if again != nil {
		// The beat is armed by a batch; what must not happen is another read.
		if _, armed := again().(farRoomTickMsg); armed {
			t.Fatal("the page is still beating against a conversation that is gone")
		}
	}
	if a.room.readFailed {
		t.Fatal("a conversation that was replaced is drawn as a failed reading, which invites a retry")
	}
	// THE ROOM'S OWN BODY IS WHERE THE SENTENCE LIVES, and it is asked for by
	// name. [taskText] is the CONVERSATION's rows — the transcript this window is
	// sitting in — which a guest page is not and never was: it drew nothing here
	// because there is nothing in it, not because the room said nothing. The
	// room's rows are [app.roomRows] and every other page-state line in this
	// package is pinned through the same probe (roomunlanded_test.go).
	body := roomText(a)
	if !strings.Contains(body, taskGuestGoneWord) {
		t.Fatalf("the page never says what happened:\n%s", body)
	}
	// AND THE OWNER IS STILL NAMED. Losing the conversation must not turn the page
	// into something that reads as this window's own work. The trail is the header
	// above the body rather than a row of it, so it is asked where it is drawn.
	if trail := a.roomTrail(); !strings.HasPrefix(trail, roomGuestOwnerWord) {
		t.Fatalf("the lost page stopped saying whose it was: %q", trail)
	}
}

// A WINDOW WITH NO ENGINE ROAD STILL ANSWERS THE ROW. The capability is absent
// rather than broken, so the card is the door and nothing is said about machinery
// the person cannot see.
func TestAWindowWithNoOwnerDoorOpensTheCardWithoutSayingWhy(t *testing.T) {
	a, _ := guestLab(t)
	a.openTaskOwner = nil
	awayRowOf(t, a)
	a.taskSheetEnter()

	if !a.taskSheet.awayOwner.on {
		t.Fatalf("a window with no engine road answered nothing:\n%s", taskSheetText(a))
	}
	if a.pageMsg != "" {
		t.Fatalf("a window with no engine road blamed something: %q", a.pageMsg)
	}
}
