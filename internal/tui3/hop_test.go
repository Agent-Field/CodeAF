package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE SWITCHER IS A CARD OVER A DIMMED SURFACE, AND THESE ARE THE FOUR CLAIMS
// THAT MAKE IT ONE (hop.go): what is on it, what the keys do, that nothing
// underneath is left looking live, and that a key which cannot act is neither
// bound nor advertised.

// keepThree puts two more conversations in the keeper and hands back the app
// standing in front of the third. THE NAMES COME OFF THE SIDECAR because a fake
// agent has no title to give, which is also the real fallback: the switcher asks
// the agent, then what the surface was calling it when it was left, and never
// the transcript's file name (hop.go's [hopTitle]).
func keepThree(t *testing.T, a *app) (older, newer *fakeAgent) {
	t.Helper()
	// THE MACHINE IS PINNED EMPTY, and every test in this file that wants rows
	// below the fold hands its own closure. Without a seam the switcher's reading
	// is [session.ReadWorld] over whatever `~/.codeaf` the suite is running as,
	// so a card asserted against the developer's own conversations would pass on
	// one machine and fail on the next (hop.go's [app.hopRest]).
	emptyMachine(a)
	older, newer = &fakeAgent{model: "m"}, &fakeAgent{model: "m"}
	a.stow(Conversation{
		Agent: older, SessionFile: "/tmp/lab/price-scrape.jsonl",
		Workspace: "/tmp/leadgen", Place: "leadgen",
	}, &aside{since: a.now().Add(-3 * time.Hour), title: "openrouter price scrape"})
	a.stow(Conversation{
		Agent: newer, SessionFile: "/tmp/lab/rail-scope.jsonl",
		Workspace: "/tmp/lab", Place: "lab",
	}, &aside{since: a.now().Add(-12 * time.Minute), title: "Refactor the rail scope model"})
	return older, newer
}

// emptyMachine pins the world seam to a machine with nothing else on it.
func emptyMachine(a *app) {
	a.world = func() (session.World, bool) { return session.World{}, true }
}

// TestTheSwitcherDrawsEveryOpenConversationInTheStripsOrder is the reading,
// asserted row by row: the order, the seam, and where the cursor opens.
func TestTheSwitcherDrawsEveryOpenConversationInTheStripsOrder(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	// THE STRIP IS DRAWN FIRST, because it is what the card's order comes from
	// now (hop.go's [app.hopStripOrder]) and a card read against a row nobody
	// laid out is a card asserted on its fallback.
	a.width, a.height = 160, 40
	_ = a.tabsRow(a.width)

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal(hopOpenKey + " did not raise the switcher")
	}
	rows := a.hop.rows
	if len(rows) != 3 {
		t.Fatalf("the card holds %d rows, and three conversations are open", len(rows))
	}
	// THE LEFTMOST TAB IS THE FIRST ROW, and on a window that has entered its
	// three conversations in this order the strip reads rail-scope, price-scrape,
	// the one in front.
	if !strings.Contains(rows[0].title, "rail scope") {
		t.Fatalf("the first row is %q, and the leftmost tab is rail-scope", rows[0].title)
	}
	if words := tabWords(a); len(words) != 3 || !strings.Contains(words[0], "rail scope") {
		t.Fatalf("the strip this card is asserted against reads %+v", words)
	}
	if !strings.Contains(rows[1].title, "price scrape") {
		t.Fatalf("the second row is %q", rows[1].title)
	}
	// AND THE ONE ON SCREEN IS THE LAST TAB HERE, WITH THE SEAM ON IT. It is last
	// because it is the newest tab on this row and not because the card puts the
	// front conversation anywhere in particular — see the strip-order test below.
	if !rows[2].here || rows[2].note != hopHereWord {
		t.Fatalf("the front conversation came out as %+v", rows[2])
	}
	// THE CURSOR OPENS ON THE ROW `tab` WOULD HAVE GONE TO, so the commonest
	// journey through this card is two keys. Here that is also row zero; the test
	// below is the one that holds them apart.
	if a.hop.at != 0 {
		t.Fatalf("the cursor opened on row %d", a.hop.at)
	}
	// AND NEVER ON `you are here`, which is what makes `alt+k enter` land
	// somewhere on a session holding one conversation ([hopFirstStop]).
	if a.hop.rows[a.hop.at].here {
		t.Fatal("the cursor opened on the conversation the person is already in")
	}
	// The project and the age are the two things the tail says about a row it
	// did not have to ask the disk about.
	if rows[1].project != "leadgen" || rows[1].age == "" {
		t.Fatalf("the tail came out as project=%q age=%q", rows[1].project, rows[1].age)
	}
}

// TestTheSwitcherWalksWrapsAndGoes is the keyboard: down, up, round both ends,
// and the one key that actually moves the surface.
func TestTheSwitcherWalksWrapsAndGoes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	drive(t, a, key("tab"))
	if a.hop.at != 1 {
		t.Fatalf("tab left the cursor on %d", a.hop.at)
	}
	drive(t, a, key("shift+tab"))
	drive(t, a, key("shift+tab"))
	// A RING WRAPS AT BOTH ENDS. Walking up off the top lands on the bottom row
	// rather than stopping there, so an overshoot costs one key and not seven.
	if a.hop.at != len(a.hop.rows)-1 {
		t.Fatalf("walking up off the top left the cursor on %d of %d", a.hop.at, len(a.hop.rows))
	}
	// AND ANOTHER PRESS OF THE OPEN KEY IS ANOTHER STEP DOWN, which is what makes
	// this alt+tab rather than a menu: the gesture is one key, tapped.
	drive(t, a, key(hopOpenKey))
	if a.hop.at != 0 {
		t.Fatalf("a second %s left the cursor on %d", hopOpenKey, a.hop.at)
	}

	drive(t, a, key("tab"), key("enter"))
	if a.hopShowing() {
		t.Fatal("enter left the card up")
	}
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("enter landed on %q", a.file)
	}
	// AND THE CONVERSATION THAT WAS IN FRONT IS STILL OPEN — a switch is not a
	// close (keeper.go), so the ring is the same size it was.
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after the switch", a.openCount())
	}
}

// TestTheSwitcherPutsItselfAwayAndChangesNothing is `esc`: the whole point of a
// layer is that leaving it costs nothing.
func TestTheSwitcherPutsItselfAwayAndChangesNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey), key("tab"), key("esc"))
	if a.hopShowing() {
		t.Fatal("esc left the card up")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("esc moved the surface to %q", a.file)
	}
	// AND A KEY THAT MEANS NOTHING HERE PUTS IT AWAY WITHOUT REACHING THE DRAFT.
	drive(t, a, key(hopOpenKey), key("z"))
	if a.hopShowing() {
		t.Fatal("an unrelated key left the card up")
	}
	if !a.input.empty() {
		t.Fatalf("the key that closed the card also landed in the box as %q", a.input.String())
	}
}

// TestTheSurfaceUnderTheSwitcherIsDimmed is the depth claim, and it is the whole
// of what says `layer` on a surface that draws no borders (hop.go).
func TestTheSurfaceUnderTheSwitcherIsDimmed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	if !a.pal.fading() {
		t.Skip("this palette has no depth ladder to fade down")
	}

	body := []string{"one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen"}
	drive(t, a, key(hopOpenKey))
	out := a.hopOver(append([]string(nil), body...), 60, a.pal)
	if len(out) != len(body) {
		t.Fatalf("the layer changed the body's height from %d to %d", len(body), len(out))
	}
	dimmed, carded := 0, 0
	for i, line := range out {
		switch {
		case strings.Contains(line, body[i]):
			// The row survived as itself. It may only do that painted.
			if line == body[i] {
				t.Fatalf("row %d is still at full ink under the card: %q", i, line)
			}
			dimmed++
		default:
			carded++
		}
	}
	if dimmed == 0 {
		t.Fatal("nothing behind the card was dimmed")
	}
	if carded < 3 {
		t.Fatalf("the card wrote %d rows over the body", carded)
	}
	// AND THE CARD ITSELF IS NOT DRAWN AT THE FADED STOP. A card a person has to
	// squint at is a card that has taken the keyboard for nothing.
	head := ""
	for _, line := range out {
		if strings.Contains(plain(line), hopOpenWord) && strings.Contains(plain(line), "enter open") {
			head = line
		}
	}
	if head == "" {
		t.Fatalf("the card's head row is not on the frame:\n%s", strings.Join(out, "\n"))
	}
}

// TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne is the capability
// law: a key that cannot act says so by not being advertised, and the guard and
// the advertisement are one predicate so they cannot come apart.
func TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	emptyMachine(a)
	// The legend slot is dropped whole under [hudTight], where the cells are
	// worth more to the conversation's name than to a reminder (render.go).
	a.width = 100

	drive(t, a, key(hopOpenKey))
	if a.hopShowing() || a.hopAvailable() {
		t.Fatal("the switcher opened over the only conversation this terminal holds")
	}
	if strings.Contains(a.footHint(a.width), hopDoorWord) {
		t.Fatalf("the legend named the switcher with one conversation open: %q", a.footHint(a.width))
	}

	// A second conversation makes the switcher real. Tab still works, but its
	// hint no longer takes a place on the keys row.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/other.jsonl"},
		&aside{since: a.now()})
	got := a.footHint(a.width)
	if !strings.Contains(got, hopDoorWord) || strings.Contains(got, "tab last") {
		t.Fatalf("the legend names %q with two open", got)
	}

	// AND A MACHINE WITH OTHER CONVERSATIONS ON IT NAMES THE SWITCHER WITH NONE
	// OF THEM OPEN — which is the whole discoverability fix: on a fresh session
	// this key is the only thing that gets you anywhere, so it has to be on the
	// frame from the first one.
	fresh := newTestApp(&fakeAgent{model: "m"})
	emptyMachine(fresh)
	fresh.width, fresh.file = 100, "/tmp/lab/this-one.jsonl"
	if fresh.hopAvailable() {
		t.Fatal("the switcher is available before anything has counted the machine")
	}
	fresh.hopKnown = 6
	if !fresh.hopAvailable() {
		t.Fatal("the switcher is not available with six conversations on the machine")
	}
	if got := fresh.footHint(fresh.width); !strings.Contains(got, hopDoorWord) {
		t.Fatalf("the legend does not name the switcher on a fresh session: %q", got)
	}
}

// TestTheSwitcherIsFrozenTheMomentItOpens guards the one race a list like this
// has: a conversation finishing a turn stirs the surface, and rows that re-ranked
// on that stir would move the row under the cursor mid-keystroke.
func TestTheSwitcherIsFrozenTheMomentItOpens(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	first := a.hop.rows[0].title

	// Another conversation arrives in the keeper while the card is up.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/late.jsonl"},
		&aside{since: a.now()})
	if len(a.hop.rows) != 3 || a.hop.rows[0].title != first {
		t.Fatalf("the card re-read itself under the cursor: %d rows, first %q",
			len(a.hop.rows), a.hop.rows[0].title)
	}
}

// TestTheSwitcherSaysWhatChangedWhileYouWereAway is the note column, which is
// what makes this a catch-up rather than a list of names.
func TestTheSwitcherSaysWhatChangedWhileYouWereAway(t *testing.T) {
	if got := hopNote(tabNeedsPerson, 4, 2); got != hopAskingWord {
		t.Fatalf("a conversation that wants a person says %q", got)
	}
	if got := hopNote(tabWorking, 3, 1); got != "3 tasks running" {
		t.Fatalf("three nodes turning says %q", got)
	}
	if got := hopNote(tabWorking, 1, 0); got != "1 task running" {
		t.Fatalf("one node turning says %q", got)
	}
	if got := hopNote(tabIdle, 0, 2); got != hopLandedWord {
		t.Fatalf("a turn that ended while away says %q", got)
	}
	if got := hopNote(tabIdle, 0, 0); got != hopNothingWord {
		t.Fatalf("a quiet conversation says %q", got)
	}
	// AND WORK NOTHING COUNTED STILL SAYS SO. A queued node and a background job
	// are both work the project index does not count, and the row that wears the
	// working mark for them must not fall through to the quiet words beside it —
	// which is the disagreement #708 was: `◐` on the tab, `nothing new` here.
	if got := hopNote(tabWorking, 0, 0); got != tabSignalWord(tabWorking) {
		t.Fatalf("work with no count says %q", got)
	}
	if got := hopNote(tabWorking, 0, 3); got != tabSignalWord(tabWorking) {
		t.Fatalf("work over a turn that landed while away says %q", got)
	}
}

// TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt is the capability law again,
// on the one chord that has no legacy encoding (hop.go).
func TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	a.keysDisambiguated = false
	if a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is bound on a terminal that cannot send it")
	}
	a.keysDisambiguated = true
	if !a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is not bound on a terminal that answered the keyboard query")
	}
	drive(t, a, key(hopAlias))
	if !a.hopShowing() {
		t.Fatal("ctrl+tab did not raise the switcher where it can arrive")
	}
}

// TestTheCardHoldsTheWholeMachineAndOpensARowThatIsNotOpenYet is the half of the
// card that made the key worth binding: on a fresh session there is exactly one
// conversation open, and a switcher that listed only those would be a key that
// does nothing until somebody had already learned what it was for.
func TestTheCardHoldsTheWholeMachineAndOpensARowThatIsNotOpenYet(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = filepath.Join(dir, "this-one.jsonl")
	a.workspace = dir

	// A machine with two conversations on it, one of them the one on screen.
	other := filepath.Join(dir, "other.jsonl")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "b", Title: "the other one", Transcript: other, ProjectDir: dir},
			},
		}}}, true
	}
	// The door the card opens a closed row through, standing in for cmd/codeaf's.
	opened := ""
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = transcript
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: transcript, Workspace: workspace}, nil
	}

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("the switcher did not open with one conversation open and another on the machine")
	}
	// THE CARD IS ABOUT WHAT IS OPEN. The one conversation this terminal holds is
	// the whole list, and the other is behind the fold with the count on it.
	if len(a.hop.rows) != 1 || a.hop.rest != 1 {
		t.Fatalf("the shut card holds %d rows and folds %d: %+v", len(a.hop.rows), a.hop.rest, a.hop.rows)
	}
	if plain(a.hopFoot()) != hopFoldKeyWord {
		t.Fatalf("the fold says %q", plain(a.hopFoot()))
	}
	// `→` REACHES THEM.
	drive(t, a, key("right"))
	if len(a.hop.rows) != 2 {
		t.Fatalf("the card holds %d rows once the fold is open: %+v", len(a.hop.rows), a.hop.rows)
	}
	// THE ONE ON SCREEN IS THE OPEN HALF, and the other is below the fold.
	if !a.hop.rows[0].open || !a.hop.rows[0].here {
		t.Fatalf("the first row came out as %+v", a.hop.rows[0])
	}
	if a.hop.rows[1].open || !strings.Contains(strings.ToLower(a.hop.rows[1].title), "other") {
		t.Fatalf("the closed row came out as %+v", a.hop.rows[1])
	}
	// AND A QUIET CLOSED ROW SAYS NOTHING. `not open yet` on nine rows is the
	// layout's own fact said nine times; the rule already draws that line.
	if a.hop.rows[1].note != "" {
		t.Fatalf("a quiet closed row says %q", a.hop.rows[1].note)
	}
	// OPENING THE FOLD MOVED THE CURSOR OFF `you are here`, which is what makes
	// the whole gesture `alt+k → enter` on a session holding one conversation.
	if a.hop.at != 1 {
		t.Fatalf("the cursor is on row %d after the fold opened", a.hop.at)
	}
	drive(t, a, key("enter"))
	if opened != other {
		t.Fatalf("enter opened %q", opened)
	}
	// AND THE CONVERSATION IT LEFT IS STILL RUNNING — the same bargain home's
	// own enter makes (keeper.go).
	if a.openCount() != 2 {
		t.Fatalf("%d conversations are open after taking a closed row", a.openCount())
	}
}

// TestTheFoldKeepsTheCardAboutWhatIsOpen is the shape the owner asked for: the
// ring is the conversations this terminal is holding, and everything else on the
// machine is behind one door with a count on it.
func TestTheFoldKeepsTheCardAboutWhatIsOpen(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	rows := []session.SessionRow{{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir}}
	for i := 0; i < 4; i++ {
		rows = append(rows, session.SessionRow{
			ID: itoa(i), Title: "shut chat " + itoa(i),
			Transcript: filepath.Join(dir, "shut"+itoa(i)+".jsonl"), ProjectDir: dir,
		})
	}
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Name: "lab", Dir: dir, Sessions: rows}}}, true
	}

	drive(t, a, key(hopOpenKey))
	if len(a.hop.rows) != 1 || a.hop.rest != 4 {
		t.Fatalf("the shut card holds %d rows and folds %d", len(a.hop.rows), a.hop.rest)
	}
	// THE FOLD IS ONE INSTRUCTION: the key, and what pressing it does. The count
	// it used to carry is the head's job and the head is one row above it.
	if foot := plain(a.hopFoot()); foot != hopFoldKeyWord {
		t.Fatalf("the fold reads %q", foot)
	}
	// AND THE HEAD COUNTS ONLY WHAT IS OPEN, which is what the card is about.
	if head := plain(a.hopHead(80, a.pal)); !strings.Contains(head, "1 of 5") {
		t.Fatalf("the head reads %q", head)
	}

	drive(t, a, key(hopFoldKey))
	if len(a.hop.rows) != 5 || a.hop.rest != 0 {
		t.Fatalf("the open card holds %d rows and folds %d", len(a.hop.rows), a.hop.rest)
	}
	if foot := plain(a.hopFoot()); !strings.Contains(foot, hopShutKeyWord) {
		t.Fatalf("the open fold reads %q", foot)
	}
	drive(t, a, key(hopShutKey))
	if len(a.hop.rows) != 1 {
		t.Fatalf("`←` left %d rows on the card", len(a.hop.rows))
	}
}

// TestCtrlWDismissesATabAndKeepsItsConversation is the way out, and
// the one guard on it: closing ends the agent, so work turning inside it stops —
// which is what the quit door already warns about, said here on the row.
func TestCtrlWDismissesATabAndKeepsItsConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	older, _ := keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open before anything is closed", a.openCount())
	}
	// The cursor opens on the most recent one behind this. Walk to the older.
	drive(t, a, key("down"))
	if !strings.Contains(a.hop.rows[a.hop.at].title, "price scrape") {
		t.Fatalf("the cursor is on %q", a.hop.rows[a.hop.at].title)
	}
	drive(t, a, key(hopAwayKey))
	if a.openCount() != 3 {
		t.Fatalf("ctrl+w stopped holding a conversation: %d open", a.openCount())
	}
	if older.closes != 0 || older.stops != 0 {
		t.Fatal("ctrl+w ended work instead of dismissing a tab")
	}
	// THE CARD STAYS UP AND SAYS SO, because tidying up is something people do
	// two or three of in a row.
	if !a.hopShowing() {
		t.Fatal("the card came down on a close")
	}
	if !strings.Contains(a.hop.say, hopClosedWord) {
		t.Fatalf("the card says %q", a.hop.say)
	}
}

// Repeated dismissal remains harmless even while work is running — and each
// press closes the NEXT row, because the one just closed has left the list
// (hop.go's [app.hopTabbed]). That is what the key does on the strip, and it is
// why the card stays up for it.
func TestDismissingARunningTabNeverStopsItsWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	emptyMachine(a)
	busy := &busyAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.stow(Conversation{Agent: busy, SessionFile: "/tmp/lab/busy.jsonl", Place: "lab"},
		&aside{since: a.now(), title: "the busy one"})
	quiet := &fakeAgent{model: "m"}
	a.stow(Conversation{Agent: quiet, SessionFile: "/tmp/lab/quiet.jsonl", Place: "lab"},
		&aside{since: a.now().Add(-time.Hour), title: "the quiet one"})

	drive(t, a, key(hopOpenKey), key(hopAwayKey), key(hopAwayKey))
	if a.openCount() != 3 || busy.closes != 0 || busy.stops != 0 {
		t.Fatalf("dismissing stopped work: open=%d closes=%d stops=%d", a.openCount(), busy.closes, busy.stops)
	}
	// BOTH BACKGROUND TABS ARE OFF THE ROW and neither conversation was touched.
	for _, file := range []string{"/tmp/lab/busy.jsonl", "/tmp/lab/quiet.jsonl"} {
		if !a.tabShut[a.convKey(file)] {
			t.Fatalf("dismissal did not take %s off the row", file)
		}
	}
	if !strings.Contains(a.hop.say, hopAwayWord) {
		t.Fatalf("dismissal was not acknowledged: %q", a.hop.say)
	}
	// AND THE ONLY ROW LEFT IS THE ONE THE PERSON IS STANDING IN.
	if len(a.hop.rows) != 1 || !a.hop.rows[0].here {
		t.Fatalf("the card still lists %+v", a.hop.rows)
	}
}

// busyAgent is a fake with work turning in it — the one door the switcher's own
// close guard reads ([runningTasks]).
type busyAgent struct{ *fakeAgent }

func (b *busyAgent) TaskIndex() []session.TaskIndexEntry {
	return []session.TaskIndexEntry{
		{ID: "1", Status: string(session.TaskRunning)},
		{ID: "2", Status: string(session.TaskRunning)},
		{ID: "3", Status: string(session.TaskDone)},
	}
}

// TestTheReverseChordArrivesEverywhereAndItsAliasStillDoesNot is the asymmetry
// the move to `alt+` bought (hop.go's [hopBackKey]): `alt+shift+k` is
// escape-then-`K`, a different byte from the forward chord's escape-then-`k`, so
// it is bound flat — while `ctrl+shift+tab` is still `ctrl+tab`'s own reverse and
// still only real where the terminal answered the keyboard query. Under the old
// `ctrl+shift+k` spelling BOTH were gated, because an ordinary terminal sent it
// as a bare `ctrl+k`.
func TestTheReverseChordArrivesEverywhereAndItsAliasStillDoesNot(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	a.keysDisambiguated = false
	if !a.hopBacks(hopBackKey) {
		t.Fatal("the reverse chord is not bound on a terminal that can plainly send it")
	}
	if a.hopBacks(hopBackAlias) {
		t.Fatal("the alias's reverse is bound on a terminal that cannot send it")
	}
	a.keysDisambiguated = true
	if !a.hopBacks(hopBackKey) || !a.hopBacks(hopBackAlias) {
		t.Fatal("the reverse chords are not bound where the terminal answered")
	}
	// AND IT WALKS THE RING ON THE PLAINEST TERMINAL THERE IS, which is the whole
	// of what changed: this is the same gesture the old spelling could only make
	// where the keyboard query had been answered.
	a.keysDisambiguated = false
	drive(t, a, key(hopOpenKey), key(hopBackKey))
	if a.hop.at != len(a.hop.rows)-1 {
		t.Fatalf("%s left the cursor on %d of %d", hopBackKey, a.hop.at, len(a.hop.rows))
	}
	// AND `shift+tab` STILL DOES IT TOO, on every terminal, because it is the
	// spelling a hand reaches for once the card is up.
	drive(t, a, key("shift+tab"))
	if a.hop.at != len(a.hop.rows)-2 {
		t.Fatalf("shift+tab left the cursor on %d", a.hop.at)
	}
}

// ── QUICK SWITCHING — the press is the switch (hop.go's second mode) ────────

// TestQuickSwitchingSwitchesOnThePressAndTheCardFades is the default gesture:
// each press of the chord lands you in the next conversation at once, the card
// is a receipt, and the pause after the last press is what puts it away.
func TestQuickSwitchingSwitchesOnThePressAndTheCardFades(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("the first press left the surface on %q", a.file)
	}
	if !a.hopShowing() || !a.hop.live {
		t.Fatal("the receipt card is not up over the conversation just landed in")
	}
	// THE `you are here` MARK MOVED WITH THE SURFACE: the row slid to wears it,
	// and the row just left does not.
	if !a.hop.rows[a.hop.at].here || a.hop.rows[a.hop.at].note != hopHereWord {
		t.Fatalf("the mark did not move with the switch: %+v", a.hop.rows[a.hop.at])
	}
	for at, row := range a.hop.rows {
		if at != a.hop.at && row.here {
			t.Fatalf("row %d still claims to be where you are", at)
		}
	}

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the second press left the surface on %q", a.file)
	}

	// A STALE FADE IS OUTRUN BY CONSTRUCTION: the timer an earlier press
	// scheduled carries an earlier pulse, and the card ignores it.
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse - 1})
	if !a.hopShowing() {
		t.Fatal("a stale fade timer took the card down")
	}
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if a.hopShowing() {
		t.Fatal("the fade left the card up")
	}
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the fade moved the surface to %q — it is a receipt, not a commit", a.file)
	}
	// AND THE BURST IS SEALED: `tab` goes back to where the burst STARTED, not
	// to the stepping stone it passed through (hop.go's [app.hopSeal]).
	if last, ok := a.lastBehind(); !ok || last != a.convKey("/tmp/lab/this-one.jsonl") {
		t.Fatalf("tab would go to %q after the burst", last)
	}
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after the burst — switching is not closing", a.openCount())
	}
}

// TestQuickSwitchingEscTakesTheWholeBurstBack is the undo: the surface has
// already moved, and `esc` is the person saying they were only looking.
func TestQuickSwitchingEscTakesTheWholeBurstBack(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias), key(hopAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("two presses landed on %q", a.file)
	}
	drive(t, a, key("esc"))
	if a.hopShowing() {
		t.Fatal("esc left the card up")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("esc left the surface on %q rather than where the burst began", a.file)
	}
}

// TestTouchingAnythingButTheChordConvertsTheReceiptToTheBrowsingCard: an arrow
// is the person looking rather than switching, so the card stops fading, the
// cursor moves without the surface moving, and `enter` is the commit again.
func TestTouchingAnythingButTheChordConvertsTheReceiptToTheBrowsingCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	landed := a.file
	drive(t, a, key("down"))
	if a.hop.live {
		t.Fatal("an arrow left the card fading")
	}
	if a.file != landed {
		t.Fatalf("an arrow moved the surface to %q", a.file)
	}
	// THE FADE TIMER THE SLIDE SCHEDULED IS DEAD NOW: a card being read must
	// never go down on its own.
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if !a.hopShowing() {
		t.Fatal("the fade took down a card somebody was reading")
	}
	drive(t, a, key("enter"))
	if a.hopShowing() {
		t.Fatal("enter left the card up")
	}
}

// TestTypingRidesStraightThroughALiveCard: under quick switching the switch
// already happened, so a letter typed while the receipt lingers lands in the
// draft rather than being eaten by it. The browsing card swallows the same
// letter on purpose, and the pair of claims is the difference between the modes.
func TestTypingRidesStraightThroughALiveCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias), key("z"))
	if a.hopShowing() {
		t.Fatal("typing left the receipt up")
	}
	if a.input.String() != "z" {
		t.Fatalf("the letter typed over the receipt became %q rather than draft", a.input.String())
	}
}

// TestTheReverseChordEntersTheRingAtTheFarEnd: with the card down, the reverse
// chord opens the ring at the open conversation longest unlooked-at — and under
// quick switching it lands there at once.
func TestTheReverseChordEntersTheRingAtTheFarEnd(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopBackAlias))
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("the reverse chord landed on %q rather than the far end of the ring", a.file)
	}
	if !a.hopShowing() || !a.hop.live {
		t.Fatal("the receipt card is not up after a reverse entry")
	}
}

// TestQuickSwitchingOffIsTheBrowsingCardAlone: the setting turns the chord back
// into a menu — nothing moves until `enter`.
func TestQuickSwitchingOffIsTheBrowsingCardAlone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = false
	a.keysDisambiguated = true
	keepThree(t, a)

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("with quick switching off the press moved the surface to %q", a.file)
	}
	if !a.hopShowing() || a.hop.live {
		t.Fatal("the card should be up, and browsing rather than fading")
	}
}

// Reading the list must never navigate, even with a profile saved while quick
// switch was the default. A timeout is not evidence that Ctrl was released.
func TestCtrlKPreviewsUntilAnExplicitChoiceEvenWithQuickSwitch(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick = true
	keepThree(t, a)
	drive(t, a, key(hopOpenKey), key(hopOpenKey))
	if a.file != "/tmp/lab/this-one.jsonl" || a.hop.live || a.hop.at != 1 {
		t.Fatal("cycling navigated instead of previewing")
	}
	drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
	if !a.hopShowing() {
		t.Fatal("the list disappeared while being read")
	}
	drive(t, a, key("esc"))
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatal("cancel changed chats")
	}
	drive(t, a, key(hopOpenKey), key(hopOpenKey), key("enter"))
	if a.file != "/tmp/lab/price-scrape.jsonl" || a.hopShowing() {
		t.Fatal("enter did not open the highlighted chat")
	}
}

func TestSwitcherMouseOpensOnlyVisibleRowsAndKeepsSelectionVisible(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.originY = 4
	body := make([]string, 16)
	a.hopOver(body, 60, a.pal)
	a.hopPress(0, 0)
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatal("backdrop click switched chats")
	}
	if a.hopShowing() {
		t.Fatal("backdrop click left the switcher open")
	}
	drive(t, a, key(hopOpenKey))
	a.hop.originY = 4
	a.hopOver(body, 60, a.pal)
	target := a.hop.spots[1]
	a.hopPress(a.hop.left+3, a.hop.top+target.row)
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatal("click did not open the rendered row")
	}
	drive(t, a, key(hopOpenKey))
	a.hop.at = len(a.hop.rows) - 1
	a.hopCardLines(40, 6, a.pal)
	found := false
	for _, spot := range a.hop.spots {
		found = found || spot.at == a.hop.at
	}
	if !found {
		t.Fatal("short switcher hid the highlighted choice")
	}
}

// A LONG NAME IS GIVEN ROOM ON ITS OWN ROW AND IS NEVER READ OUT TWICE. The card
// used to re-wrap the selected row's title in a block under the list — a second
// reading of the row the cursor was already on, present or absent depending on
// how long that one name happened to be. The room went to the subject column
// instead (hop.go's [hopLine]).
func TestALongNameTakesTheRoomAndIsNotRepeatedUnderTheList(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	long := "Investigate the parsing regression in weekly reports"
	a.hop.rows[0].title = long
	a.hop.at = 0

	// ON A WIDE FRAME THE WHOLE NAME IS ON ITS OWN ROW, once.
	body := plain(strings.Join(a.hopCardLines(140, 18, a.pal), "\n"))
	if !strings.Contains(body, long) {
		t.Fatalf("the name did not fit its row on a wide card:\n%s", body)
	}
	if strings.Count(body, "weekly reports") != 1 {
		t.Fatalf("the name is drawn %d times:\n%s", strings.Count(body, "weekly reports"), body)
	}
	// AND THE CLAUSE IS STILL A COLUMN BESIDE IT, not a suffix stuck to the name.
	if strings.Contains(body, "reports"+hopNothingWord) || strings.Contains(body, "reports "+hopNothingWord) {
		t.Fatalf("the name and the clause ran together:\n%s", body)
	}
	// ON A NARROW ONE THE NAME IS CUT AND NOTHING IS DRAWN UNDER THE LIST TO MAKE
	// UP FOR IT: the rows, the foot, and no second reading.
	narrow := a.hopCardLines(60, 18, a.pal)
	rows := 0
	for _, line := range narrow {
		if strings.Contains(plain(line), "Investigate") {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("the cut name appears on %d rows:\n%s", rows, plain(strings.Join(narrow, "\n")))
	}
}

// ── THE CARD LANDS YOU IN WHAT YOU TOOK, FROM WHEREVER YOU TOOK IT ──────────
//
// The switcher opens over a place as readily as over a conversation, and for a
// while taking a row from one only moved the conversation UNDERNEATH the place:
// from home, `enter` read as a key that did nothing while it had quietly swapped
// what was behind the screen. These four are that door, asserted from a place
// (hop.go's [app.hopLand]).

// TestTakingARowFromHomeLandsInTheConversation is the owner's own report: home
// up, `ctrl+k`, `enter`, and you are IN the conversation you chose.
func TestTakingARowFromHomeLandsInTheConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drain(t, a, a.showPage(pageHome))
	if !a.at(pageHome) {
		t.Fatal("the fixture is not standing on home")
	}

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("ctrl+k did not raise the switcher on home")
	}
	drive(t, a, key("enter"))
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("enter left the surface on %q", a.file)
	}
	// THE PLACE CAME DOWN WITH THE TAKE. This is the whole defect: the line above
	// passed while home stood in front of the conversation it had just switched.
	if a.pageShowing() {
		t.Fatalf("enter on the card left %v standing over the conversation", a.page)
	}
	// AND THE ONE HOME WAS OVER IS STILL RUNNING, the bargain every door between
	// conversations makes (keeper.go).
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after taking a row from home", a.openCount())
	}
}

// TestTakingARowFromAnyOtherPlaceLandsToo holds the same law one door wider: the
// card asks whether A place is standing and never whether HOME is, because it
// opens on all seven.
func TestTakingARowFromAnyOtherPlaceLandsToo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drain(t, a, a.showPage(pageSpend))
	if !a.at(pageSpend) {
		t.Fatal("the fixture is not standing on the spend place")
	}

	drive(t, a, key(hopOpenKey), key("enter"))
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("enter left the surface on %q", a.file)
	}
	if a.pageShowing() {
		t.Fatalf("enter on the card left %v standing over the conversation", a.page)
	}
}

// TestTakingTheRowYouAreOnFromHomeStillLandsInIt is the one row that is not a
// switch and is still a door. `you are here` brings nothing forward — but from
// home it is the person saying "that one", and answering with nothing at all
// would leave them on the screen they pressed a conversation on.
func TestTakingTheRowYouAreOnFromHomeStillLandsInIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drain(t, a, a.showPage(pageHome))

	drive(t, a, key(hopOpenKey))
	here := -1
	for at, row := range a.hop.rows {
		if row.here {
			here = at
		}
	}
	if here < 0 {
		t.Fatalf("no row is marked `you are here`: %+v", a.hop.rows)
	}
	a.hop.at = here
	drive(t, a, key("enter"))
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("taking `you are here` moved the surface to %q", a.file)
	}
	if a.pageShowing() {
		t.Fatalf("taking `you are here` from home left %v standing", a.page)
	}
}

// TestARefusedRowLeavesHomeStandingAndSaysSoThere is the other half of the law:
// a take that did not happen must not move the person, and its sentence goes on
// the line home already has for saying things rather than into a transcript
// nobody is looking at (hop.go's [app.hopSay]).
func TestARefusedRowLeavesHomeStandingAndSaysSoThere(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	other := filepath.Join(dir, "other.jsonl")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "b", Title: "the other one", Transcript: other, ProjectDir: dir},
			},
		}}}, true
	}
	a.open = func(workspace, transcript string) (Conversation, error) {
		return Conversation{}, session.ErrSessionLocked
	}
	drain(t, a, a.showPage(pageHome))

	// `→` opens the fold and puts the cursor on the closed row, which is the row
	// the door refuses.
	drive(t, a, key(hopOpenKey), key("right"), key("enter"))
	if a.file != filepath.Join(dir, "this-one.jsonl") {
		t.Fatalf("a refused row moved the surface to %q", a.file)
	}
	if !a.at(pageHome) {
		t.Fatalf("a refused row took home down and left %v", a.page)
	}
	if a.home.msg != sessionBusyWord {
		t.Fatalf("home said %q about a row the door refused", a.home.msg)
	}
}

// TestTakingAClosedRowFromHomeLandsInItToo is the fold's half of the door: a row
// this terminal was NOT holding is opened beside the others, and the person ends
// up looking at it rather than at the home they pressed it from.
func TestTakingAClosedRowFromHomeLandsInItToo(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	other := filepath.Join(dir, "other.jsonl")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "b", Title: "the other one", Transcript: other, ProjectDir: dir},
			},
		}}}, true
	}
	opened := ""
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = transcript
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: transcript, Workspace: workspace}, nil
	}
	drain(t, a, a.showPage(pageHome))

	drive(t, a, key(hopOpenKey), key("right"), key("enter"))
	if opened != other {
		t.Fatalf("enter opened %q", opened)
	}
	if a.pageShowing() {
		t.Fatalf("opening a closed row from home left %v standing over it", a.page)
	}
}

// TestQuickSwitchingFromHomeLandsToo holds the chord that switches without a
// choice to the same law: it is the same act on a different key.
func TestQuickSwitchingFromHomeLandsToo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	a.hopQuick, a.keysDisambiguated = true, true
	keepThree(t, a)
	drain(t, a, a.showPage(pageHome))

	drive(t, a, key(hopAlias))
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("the press left the surface on %q", a.file)
	}
	if a.pageShowing() {
		t.Fatalf("quick switching from home left %v standing over the conversation", a.page)
	}
}

// ── THE CARD IS LAID OUT THE WAY THE TAB ROW IS ─────────────────────────────
//
// The strip and the card are two readings of one set of conversations, drawn one
// line apart and used together. The card used to be ordered by recency, so the
// third tab could be the first row and neither position meant anything; it now
// takes the strip's own order (hop.go's [app.hopStripOrder]).

// stripKeys is the strip's order, as the keys the card is addressed by.
func stripKeys(a *app) []string {
	keys := make([]string, 0, len(a.chatTabs))
	for _, tab := range a.chatTabs {
		if tab.key != "" && !tab.start {
			keys = append(keys, tab.key)
		}
	}
	return keys
}

// openKeys is the card's order, open rows only — the half the strip draws.
func openKeys(a *app) []string {
	keys := make([]string, 0, len(a.hop.rows))
	for _, row := range a.hop.rows {
		if row.open {
			keys = append(keys, a.convKey(row.file))
		}
	}
	return keys
}

// TestTheCardHoldsTheStripsOrderWhenRecencyDoesNot is the claim with the two
// orders pulled apart: going to the middle tab and opening the card must not
// move a single row, because the strip did not move either.
func TestTheCardHoldsTheStripsOrderWhenRecencyDoesNot(t *testing.T) {
	a, _, _ := tabApp(t)
	before := stripKeys(a)
	if len(before) != 3 {
		t.Fatalf("the fixture drew %d tabs: %+v", len(before), tabWords(a))
	}

	// GO TO THE MIDDLE TAB. Recency now says this one, then the one just left;
	// the strip still says what it said.
	a.hopOpen()
	a.hop.at = 1
	drive(t, a, key("enter"))
	_ = a.tabsRow(a.width)
	if now := stripKeys(a); !equalStrings(now, before) {
		t.Fatalf("the strip re-ordered itself on a switch: %+v then %+v", before, now)
	}

	a.hopOpen()
	if got := openKeys(a); !equalStrings(got, before) {
		t.Fatalf("the card reads %+v and the strip reads %+v", got, before)
	}
	// AND THE ONE IN FRONT IS WHEREVER ITS TAB IS — the middle — rather than at
	// either end. This is the row the old reading always drew last.
	if !a.hop.rows[1].here {
		t.Fatalf("the `you are here` mark is not on the middle row: %+v", a.hop.rows)
	}
	// THE CURSOR STILL OPENS ON THE ONE `tab` WOULD GO TO, which is now row two
	// rather than row zero: the LIST follows the strip and the CURSOR follows
	// recency, and `ctrl+k` `enter` still means "the last one" (hopFirstStop).
	if a.hop.at != 2 || a.convKey(a.hop.rows[a.hop.at].file) != before[2] {
		t.Fatalf("the cursor opened on row %d of %+v", a.hop.at, a.hop.rows)
	}
}

// TestClosingATabTakesItOffTheCardAsWellAsTheRow is the sync, from the `✕`: the
// list and the strip are one reading, so a tab dismissed with the pointer leaves
// both. The conversation is still held and still running — it is BEHIND THE
// FOLD, with everything else this window is not showing, and `enter` on it there
// brings it and its tab back.
func TestClosingATabTakesItOffTheCardAsWellAsTheRow(t *testing.T) {
	a, _, _ := tabApp(t)
	shut := stripKeys(a)[0]
	span := tabCloseSpanFor(t, a, "Refactor the rail scope model")
	clickTab(t, a, span.from)
	_ = a.tabsRow(a.width)
	if keysHold(stripKeys(a), shut) {
		t.Fatalf("the dismissed tab is still on the row: %+v", tabWords(a))
	}
	if a.behind[shut] == nil {
		t.Fatal("dismissing the tab took its conversation out of the keeper")
	}

	a.hopOpen()
	if keys := openKeys(a); !equalStrings(keys, stripKeys(a)) {
		t.Fatalf("the card reads %+v and the strip reads %+v", keys, stripKeys(a))
	}
	if a.hop.rest != 1 {
		t.Fatalf("the fold stands for %d conversations and one tab was closed", a.hop.rest)
	}
	// THE HEAD COUNTS TABS, so it cannot say three while the row shows two.
	if a.hopOpenRows() != len(stripKeys(a)) {
		t.Fatalf("the head says %d open and the row has %d tabs", a.hopOpenRows(), len(stripKeys(a)))
	}

	// AND IT IS BEHIND THE FOLD, ALIVE, AND ONE `enter` FROM COMING BACK.
	a.hopSpread(true)
	at := -1
	for i, row := range a.hop.rows {
		if a.convKey(row.file) == shut {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the closed conversation left the card entirely: %+v", a.hop.rows)
	}
	if !a.hop.rows[at].open || a.hop.rows[at].held || a.hop.rows[at].gone {
		t.Fatalf("the closed conversation is drawn as unreachable: %+v", a.hop.rows[at])
	}
	a.hop.at = at
	drive(t, a, key("enter"))
	if a.convKey(a.file) != shut {
		t.Fatalf("enter on the fold landed on %q", a.file)
	}
	_ = a.tabsRow(a.width)
	if !keysHold(stripKeys(a), shut) {
		t.Fatalf("its tab did not come back: %+v", tabWords(a))
	}
}

// TestCtrlWOnTheCardTakesTheRowOffTheCardAtOnce is the same sync from the card's
// own key: the row the person just closed leaves the list under their hand,
// rather than sitting there looking open until something else redrew the strip.
func TestCtrlWOnTheCardTakesTheRowOffTheCardAtOnce(t *testing.T) {
	a, _, _ := tabApp(t)
	a.hopOpen()
	if len(a.hop.rows) != 3 {
		t.Fatalf("the card opened with %d rows: %+v", len(a.hop.rows), a.hop.rows)
	}
	shut := a.convKey(a.hop.rows[0].file)

	a.hop.at = 0
	drive(t, a, key(hopAwayKey))
	// THE CARD IS STILL UP — closing tabs is done two or three at a time — and
	// the row is gone from it WITHOUT a frame having been drawn in between.
	if !a.hopShowing() {
		t.Fatal("the card came down on a close")
	}
	for _, row := range a.hop.rows {
		if a.convKey(row.file) == shut {
			t.Fatalf("the row ctrl+w closed is still on the card: %+v", a.hop.rows)
		}
	}
	if a.hop.rest != 1 || a.hopOpenRows() != 2 {
		t.Fatalf("the card says %d open and %d behind the fold", a.hopOpenRows(), a.hop.rest)
	}
	// AND THE STRIP AGREES THE MOMENT IT IS DRAWN.
	_ = a.tabsRow(a.width)
	if keysHold(stripKeys(a), shut) {
		t.Fatalf("the strip still draws the closed tab: %+v", tabWords(a))
	}
	if !equalStrings(openKeys(a), stripKeys(a)) {
		t.Fatalf("the card reads %+v and the strip reads %+v", openKeys(a), stripKeys(a))
	}
}

// TestCtrlWBelowTheFoldClosesNothingAndSaysNothing is the other half: a row with
// no tab — one this terminal never opened, or one whose tab was closed a moment
// ago — is a key press that has already got what it asked for, so nothing closes
// and nothing is said about it.
func TestCtrlWBelowTheFoldClosesNothingAndSaysNothing(t *testing.T) {
	a, _, _ := tabApp(t)
	a.hopOpen()
	shut := a.convKey(a.hop.rows[0].file)
	a.hop.at = 0
	drive(t, a, key(hopAwayKey))

	a.hopSpread(true)
	at := -1
	for i, row := range a.hop.rows {
		if a.convKey(row.file) == shut {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the closed conversation is not behind the fold: %+v", a.hop.rows)
	}
	a.hop.at = at
	rows := append([]hopRow(nil), a.hop.rows...)
	drive(t, a, key(hopAwayKey))
	// IT SAYS NOTHING AT ALL. The tab this key exists to close is already closed,
	// so there is nothing for the card to report and no sentence to read.
	if a.hop.say != "" {
		t.Fatalf("ctrl+w on a row with no tab said %q", a.hop.say)
	}
	if len(a.hop.rows) != len(rows) {
		t.Fatalf("ctrl+w on a row with no tab re-read the card: %d rows, was %d", len(a.hop.rows), len(rows))
	}
	if a.behind[shut] == nil {
		t.Fatal("a second ctrl+w took the conversation out of the keeper")
	}
}

// ── THE SEAM THE FOLD OPENS INTO ────────────────────────────────────────────

// TestTheFoldSaysWhereTheTabsStop is the owner's own question — why the
// conversations they closed look like the ones that are open, and what the `✕`
// on some rows means. It is answered by a word at the seam (hop.go's
// [hopClosedLabel]) and by every refusing row saying why it refuses.
func TestTheFoldSaysWhereTheTabsStop(t *testing.T) {
	dir := t.TempDir()
	a, _, _ := tabApp(t)
	gone := filepath.Join(dir, "vanished")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "x", Title: "a chat from last week", Transcript: filepath.Join(dir, "x.jsonl"), ProjectDir: dir},
				{ID: "y", Title: "held elsewhere", Transcript: filepath.Join(dir, "y.jsonl"), ProjectDir: dir, Open: true},
				{ID: "z", Title: "no folder", Transcript: filepath.Join(dir, "z.jsonl"), ProjectDir: gone},
			},
		}}}, true
	}
	// A closed tab, so the fold holds one of each kind: a conversation this
	// window is still holding, and three it is not.
	span := tabCloseSpanFor(t, a, "Refactor the rail scope model")
	clickTab(t, a, span.from)
	_ = a.tabsRow(a.width)

	// WITH THE FOLD SHUT THERE IS NO SEAM TO DRAW, because every row is a tab.
	a.hopOpen()
	if seamAt(a.hopCardLines(120, 20, a.pal)) >= 0 {
		t.Fatal("the shut card drew the closed label over a list that is all tabs")
	}

	// WITH IT OPEN THE WORD STANDS WHERE THE TABS STOP.
	a.hopOpen()
	a.hopSpread(true)
	body := a.hopCardLines(120, 20, a.pal)
	label, first := seamAt(body), -1
	for at, line := range body {
		if strings.Contains(plain(line), "Refactor the rail scope model") {
			first = at
		}
	}
	if label < 0 {
		t.Fatalf("the open fold drew no seam:\n%s", plain(strings.Join(body, "\n")))
	}
	// AND IT IS SPACED THE WAY THE HEAD IS: one blank line above it and one
	// below, so it reads as a heading over the rows under it rather than as a row
	// wedged between two lists. The head is the measure — `open`, a blank, then
	// its first row — and this is the same three lines.
	blank := func(at int) bool {
		return at >= 0 && at < len(body) && strings.TrimSpace(strings.Trim(plain(body[at]), "│")) == ""
	}
	if !blank(label - 1) {
		t.Fatalf("no air above the seam:\n%s", plain(strings.Join(body, "\n")))
	}
	if !blank(label + 1) {
		t.Fatalf("no air below the seam:\n%s", plain(strings.Join(body, "\n")))
	}
	if first != label+2 {
		t.Fatalf("the seam is at %d and the first closed row at %d:\n%s", label, first, plain(strings.Join(body, "\n")))
	}
	// THE HEAD IS SPACED THE SAME WAY, which is what "equally spaced" means here:
	// the word, a blank, the first row of its own list.
	head := -1
	for at, line := range body {
		if strings.Contains(plain(line), hopOpenWord) && strings.Contains(plain(line), "enter open") {
			head = at
		}
	}
	if head < 0 || !blank(head+1) {
		t.Fatalf("the head is at %d and is not followed by its blank:\n%s", head, plain(strings.Join(body, "\n")))
	}
	if first := head + 2; !strings.Contains(plain(body[first]), "openrouter price scrape") {
		t.Fatalf("the head's own first row is not two lines under it:\n%s", plain(strings.Join(body, "\n")))
	}
	// AND THE SEAM IS NOT A ROW: the cursor cannot land on it and a press on it
	// opens nothing.
	for _, spot := range a.hop.spots {
		if spot.row == label {
			t.Fatalf("the seam answered as a row: %+v", spot)
		}
	}
}

// TestEveryRowThatRefusesSaysWhy is the other half of that report: the `✕` means
// this row will not open, and a mark a person cannot account for is worse than
// no mark. A conversation another window holds said so already; one whose folder
// has gone wore the mark with an empty clause.
func TestEveryRowThatRefusesSaysWhy(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	gone := filepath.Join(dir, "vanished")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "y", Title: "held elsewhere", Transcript: filepath.Join(dir, "y.jsonl"), ProjectDir: dir, Open: true},
				{ID: "z", Title: "no folder", Transcript: filepath.Join(dir, "z.jsonl"), ProjectDir: gone},
			},
		}}}, true
	}
	a.hopOpen()
	a.hopSpread(true)
	for _, row := range a.hop.rows {
		if !row.held && !row.gone {
			continue
		}
		if strings.TrimSpace(row.note) == "" {
			t.Fatalf("a row drawn with the refusing mark says nothing about why: %+v", row)
		}
	}
	// AND THE TWO REASONS ARE THE TWO SENTENCES, not one sentence for both.
	var words []string
	for _, row := range a.hop.rows {
		if row.held || row.gone {
			words = append(words, row.note)
		}
	}
	if len(words) != 2 {
		t.Fatalf("the card drew %d refusing rows: %+v", len(words), a.hop.rows)
	}
	if !keysHold(words, hopHeldWord) || !keysHold(words, homeGoneWord) {
		t.Fatalf("the refusing rows say %+v", words)
	}
}

// seamAt is which drawn line is the fold's seam, and it is matched WHOLE: the
// foot says `show closed` and `hide closed`, so a substring match finds the foot
// on every card that has one.
func seamAt(lines []string) int {
	for at, line := range lines {
		inside := strings.TrimSpace(plain(strings.Trim(plain(line), "│")))
		if inside == hopClosedLabel {
			return at
		}
	}
	return -1
}

// TestCtrlWOnAConversationThisWindowNeverOpenedClosesNothing is the half of the
// fold `tabShut` cannot answer on its own: nothing ever dismissed a conversation
// this terminal has not held, so the map is silent about it — and silence read as
// "it has a tab". The key swore `tab closed` at a machine row, marked a
// conversation it had never held as dismissed, and changed nothing on the row.
func TestCtrlWOnAConversationThisWindowNeverOpenedClosesNothing(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	never := filepath.Join(dir, "never.jsonl")
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{
			Name: "lab", Dir: dir,
			Sessions: []session.SessionRow{
				{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir},
				{ID: "n", Title: "never opened here", Transcript: never, ProjectDir: dir},
			},
		}}}, true
	}
	a.hopOpen()
	a.hopSpread(true)
	at := -1
	for i, row := range a.hop.rows {
		if a.convKey(row.file) == a.convKey(never) {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the never-opened row is not on the card: %+v", a.hop.rows)
	}

	a.hop.at = at
	drive(t, a, key(hopAwayKey))
	if a.hop.say != "" {
		t.Fatalf("ctrl+w on a conversation this window never opened said %q", a.hop.say)
	}
	// AND IT LEFT NO MARK BEHIND. A dismissal recorded against a conversation
	// this window has never held is a lie the strip would have to be told to
	// forget (keeper.go's [app.rememberOpen] is the only door that clears it).
	if a.tabShut[a.convKey(never)] {
		t.Fatal("ctrl+w marked a conversation this window never held as dismissed")
	}
	if len(a.closedTabs) != 0 {
		t.Fatalf("ctrl+w put %d tabs on the reopen stack", len(a.closedTabs))
	}
}

// TestAShortCardStillShowsWhatTheFoldOpened is the seam's own cost, charged only
// where it is drawn. The three lines used to come off every short card whether or
// not the window reached a closed row, so `→ show closed` made the list SHORTER
// and showed nothing at all while the foot offered `← hide closed`.
func TestAShortCardStillShowsWhatTheFoldOpened(t *testing.T) {
	dir := t.TempDir()
	a, _, _ := tabApp(t)
	rows := []session.SessionRow{{ID: "a", Title: "this one", Transcript: a.file, ProjectDir: dir}}
	for i := 0; i < 6; i++ {
		rows = append(rows, session.SessionRow{
			ID: itoa(i), Title: "a closed chat " + itoa(i),
			Transcript: filepath.Join(dir, itoa(i)+".jsonl"), ProjectDir: dir,
		})
	}
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Name: "lab", Dir: dir, Sessions: rows}}}, true
	}

	// Card heights a small terminal actually hands this card. Eleven is where the
	// window first reaches a closed row at all; below it the list is three rows
	// long and every one of them is a tab, which is honest.
	for _, height := range []int{11, 12, 13, 14, 16} {
		a.hopOpen()
		a.hopSpread(true)
		body := plain(strings.Join(a.hopCardLines(100, height, a.pal), "\n"))
		if !strings.Contains(body, "Closed Chat") {
			t.Fatalf("the fold opened on a %d-line card and drew no closed row:\n%s", height, body)
		}
		// AND THE OPEN ROWS DID NOT PAY FOR A SEAM NOBODY DREW: every row the card
		// has room for is a row, and the word is only there when it is earned.
		if seamAt(a.hopCardLines(100, height, a.pal)) < 0 && height >= 12 {
			t.Fatalf("a %d-line card reached the closed rows without the word:\n%s", height, body)
		}
	}
}
