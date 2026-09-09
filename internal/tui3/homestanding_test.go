package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE AMBIENT BAND ON HOME ────────────────────────────────────────────────

// standBand wires a lab's app to a fixed set of items, and records what the
// store was asked to write. It is a FUNCTION seam and not a store on disk
// ([StandingSeam] says why): "home with four watches on it" must not be a test
// that writes JSON documents to assert a row's spacing.
type standBand struct {
	items []standing.Item
	// running is the marker each item's own folder would be holding, by id: an
	// absent id is a pass that is not on it. It is a MARK and not a bool because
	// the card says which half of a pass it caught and since when.
	running map[string]standing.RunningMark
	// runs is the ledger's answer for the week, by item id — the seam's
	// [StandingSeam.Runs] without a file on disk.
	runs  map[string]standing.Spend
	saved []standing.Item
	err   error
}

func (b *standBand) wire(a *app) {
	a.stands = StandingSeam{
		Items: func(workspace string) []standing.Item {
			var out []standing.Item
			for _, item := range b.items {
				if item.Workspace == workspace {
					out = append(out, item)
				}
			}
			return out
		},
		Save: func(item standing.Item) error {
			if b.err != nil {
				return b.err
			}
			b.saved = append(b.saved, item)
			for i := range b.items {
				if b.items[i].ID == item.ID {
					b.items[i] = item
				}
			}
			return nil
		},
		Running: func(id string) (standing.RunningMark, bool) {
			mark, found := b.running[id]
			return mark, found
		},
	}
	if b.runs != nil {
		a.stands.Runs = func(time.Time) map[string]standing.Spend { return b.runs }
	}
}

// bandItem is one item in a workspace, with only the fields a row reads.
func bandItem(id, words, workspace string, kind standing.WhenKind, when string) standing.Item {
	return standing.Item{
		ID: id, Words: words, Workspace: workspace,
		When:   standing.When{Kind: kind, Words: when},
		Does:   standing.Action{Kind: standing.ActionSay, Say: words},
		Rails:  standing.Rails{PerRunUSD: 0.05, MaxPerDay: 4},
		Status: standing.StatusActive,
	}
}

// homeLines is home's left column as a reader sees it.
func homeLines(a *app) []string {
	frame, _, _, _ := a.homeFrame(a.width, a.height)
	out := make([]string, 0, len(frame))
	for _, line := range frame {
		out = append(out, plain(line))
	}
	return out
}

// homeRowAt is the index of the first drawn row containing a string.
func homeRowAt(lines []string, want string) int {
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// bandOfSix is the machine every assertion about the band needs: four items
// waiting for their time, one a pass is on right now, one stopped on a question,
// and a conversation to sort them against.
func bandOfSix(t *testing.T) (*standBand, *homeLab, string) {
	t.Helper()
	lab := newHomeLab(t)
	spoke := time.Now().Add(-time.Hour)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", spoke)

	band := &standBand{running: map[string]standing.RunningMark{
		"run": {PID: 1, Since: time.Now(), What: standing.RunningChecking},
	}}
	band.items = []standing.Item{
		bandItem("wait1", "tell me when the cert expires", "/w/alpha", standing.WhenProbe, "when the cert is under 14 days"),
		bandItem("wait2", "remind me on Fridays", "/w/alpha", standing.WhenEvery, "Fridays"),
		bandItem("wait3", "remind me on Sundays", "/w/alpha", standing.WhenEvery, "Sundays"),
		bandItem("wait4", "remind me on Tuesdays", "/w/alpha", standing.WhenEvery, "Tuesdays"),
		bandItem("run", "check the deploy", "/w/alpha", standing.WhenEvery, "in 20 minutes"),
		bandItem("ask", "keep main green", "/w/alpha", standing.WhenProbe, "when CI goes red"),
	}
	for i := range band.items {
		if band.items[i].ID == "ask" {
			band.items[i].NeedsPerson = "the fix touches migrations"
		}
	}
	return band, lab, transcript
}

// AN ITEM TAKES ITS PLACE IN THE ONE TRIAGE ORDER, AND ONLY THE ONES THAT WANT
// SOMETHING TAKE A PLACE AT ALL.
//
// ── WHAT DIED AND WHERE IT WENT ─────────────────────────────────────────────
//
// THE PER-PROJECT BAND IS GONE FROM THE RESTING LIST. It used to be every item
// this machine holds, drawn under its own project's heading, three of them and
// then a door saying how many more there were — and the resting screen has no
// project headings to hang a band under any more (switcher.go, place_home.go).
// An item earns a row on the flat ranked list exactly when it NEEDS SOMEBODY or
// is FIRING ([readSwitcher] takes those two and nothing else); the ones still
// waiting for their time, and the door over them, are the standing place's
// business now. [TestTheStandingBandKeepsItsThreeRowsAndItsDoorOnAPhone] pins
// them where they still stand.
//
// WHAT SURVIVED IS THE ORDER, and it is the half that mattered: one ladder
// across BOTH kinds of row, so an item stopped on a question can never sort
// under conversations that want nothing. The glyph and the position are one
// claim made twice, and this pins them together.
func TestAStandingItemTakesItsPlaceInTheOneTriageOrder(t *testing.T) {
	band, lab, transcript := bandOfSix(t)
	a := lab.app(transcript)
	band.wire(a)
	openHomeOn(a, transcript)

	lines := homeLines(a)
	joined := strings.Join(lines, "\n")

	// EACH ROW IS FOUND BY ITS MARK AND ITS WORDS TOGETHER, which is how the
	// order and the glyphs are pinned in one reading: a row sorted to the top
	// under a mark that says "at rest" would simply not be found here.
	ask := homeRowAt(lines, tokens.GlyphNeedsHuman+" keep main green")
	run := homeRowAt(lines, "check the deploy")
	chat := homeRowAt(lines, tokens.GlyphQueued+" Pricing Research")
	for name, at := range map[string]int{"needs-you": ask, "running": run, "quiet chat": chat} {
		if at < 0 {
			t.Fatalf("home never drew the %s row:\n%s", name, joined)
		}
	}
	if !(ask < run && run < chat) {
		t.Fatalf("the list is not in triage order (ask %d, run %d, chat %d):\n%s", ask, run, chat, joined)
	}
	// AND EACH SAYS WHY IT IS WHERE IT IS. The note is the reading's own
	// ([switcherStandingNote]) and it is the whole of what the band's rollup was
	// for on a row.
	if !strings.Contains(lines[ask], "the fix touches migrations") {
		t.Fatalf("the row that needs somebody does not say what for:\n%s", joined)
	}
	if !strings.Contains(lines[run], standing.RunningChecking+" now") {
		t.Fatalf("the row a pass is on does not say so:\n%s", joined)
	}
	// AND ITS MARK AGREES WITH ITS POSITION. It is the only moving thing on this
	// machine, so it is also the one row the frame gives the turning cell to
	// (homespinner.go) — which is why the mark is asked for as "the still one or
	// the turning one" rather than as one glyph.
	if !strings.ContainsAny(lines[run], tokens.GlyphWorking+"⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the row a pass is on does not wear a moving mark:\n%s", joined)
	}
	// THE FOUR THAT WANT NOTHING ARE NOT ON THIS SCREEN, and neither is a door
	// over them. Four items with nothing to say for themselves used to cost five
	// rows of a twenty-row screen — that is the whole reason this changed.
	for _, quiet := range []string{"tell me when the cert expires", "remind me on Fridays",
		"remind me on Sundays", "remind me on Tuesdays"} {
		if strings.Contains(joined, quiet) {
			t.Fatalf("an item that wants nothing took a row on the resting list (%q):\n%s", quiet, joined)
		}
	}
	if strings.Contains(joined, homeItemsFoldWord) {
		t.Fatalf("the resting list still draws the band's door:\n%s", joined)
	}
}

// AND THE BAND ITSELF IS STILL DRAWN WHERE IT STILL LIVES: the phone's inbox,
// which is built from [homeView.projectBlock] and so still splits a project's
// items around its conversations, three of them and then one door
// (homephone.go's [homeView.buildPhone]).
//
// THE ORDER IS THE ONE THE BAND ALWAYS KEPT: what needs somebody and what is
// running sit above the conversations, what is waiting for its time sits under
// them, and the door sits under that. Six items, three rows, and three behind
// the door — the cap is on the band and never on the cold half, so the two hot
// rows are drawn whatever the count says.
func TestTheStandingBandKeepsItsThreeRowsAndItsDoorOnAPhone(t *testing.T) {
	band, lab, transcript := bandOfSix(t)
	a := lab.app(transcript)
	band.wire(a)
	// Narrower than [layoutTier]'s phone breakpoint, which is the one width at
	// which home is the inbox rather than the switcher.
	a.width, a.height = 50, 40
	openHomeOn(a, transcript)

	lines := homeLines(a)
	joined := strings.Join(lines, "\n")
	// A PHONE ROW IS TWO LINES — the name with its mark, and the rollup under it
	// — so each row is found by the line that carries its mark, and the glyphs
	// are pinned with the positions exactly as they were.
	ask := homeRowAt(lines, homeAskGlyph+" keep main green")
	run := homeRowAt(lines, homeLiveGlyph+" check the deploy")
	chat := homeRowAt(lines, homeIdleGlyph+" Pricing Research")
	wait := homeRowAt(lines, standWaitGlyph+" tell me when the cert expires")
	fold := homeRowAt(lines, homeItemsFoldWord)
	for name, at := range map[string]int{"needs-you": ask, "running": run, "session": chat, "waiting": wait, "fold": fold} {
		if at < 0 {
			t.Fatalf("the phone never drew the %s row:\n%s", name, joined)
		}
	}
	if !(ask < run && run < chat && chat < wait && wait < fold) {
		t.Fatalf("the band is not in triage order (ask %d, run %d, chat %d, wait %d, fold %d):\n%s",
			ask, run, chat, wait, fold, joined)
	}
	if !strings.Contains(joined, "…3"+homeItemsFoldWord) {
		t.Fatalf("the fold does not count what it is hiding:\n%s", joined)
	}
	if !strings.Contains(joined, tierYourCallWord) {
		t.Fatalf("the row that needs somebody lost its rollup:\n%s", joined)
	}
	// AND THE DOOR OPENS. enter on the fold line shows the rest.
	a.home.pointItemFoldForTest()
	drive(t, a, key("enter"))
	if !strings.Contains(strings.Join(homeLines(a), "\n"), "remind me on Tuesdays") {
		t.Fatalf("opening the band did not show what it was hiding:\n%s", strings.Join(homeLines(a), "\n"))
	}
}

// pointItemFoldForTest puts the cursor on the band's door. It is a test seam
// because the cursor is otherwise walked there with arrows, and a test that
// counted keystrokes would break the day a row was added above it.
func (h *homeView) pointItemFoldForTest() {
	for at, line := range h.lines {
		if line.kind == homeItemFold {
			h.cursor, h.picked = at, true
			return
		}
	}
}

// THE ROLLUP SAYS ONLY WHAT IS TRUE. A watch that has looked says what it found
// — and "nothing" is a finding, the difference between a watch that works and
// one that never ran. An item that has never fired says nothing about firing.
func TestAStandingRowSaysOnlyWhatItKnows(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	probe := bandItem("p", "tell me when CI goes red", "/w", standing.WhenProbe, "when CI goes red")
	probe.LastChecked = now.Add(-6 * time.Minute)
	if got := standRollup(StandingItemView{Item: probe}, now); got != "checked 6m ago · nothing" {
		t.Fatalf("a quiet check reads %q", got)
	}
	probe.LastCheckLine = "the last run on main is green"
	if got := standRollup(StandingItemView{Item: probe}, now); got != "checked 6m ago · the last run on main is green" {
		t.Fatalf("a check that found something reads %q", got)
	}

	routine := bandItem("r", "post the standup note", "/w", standing.WhenEvery, "Mondays 9am")
	if got := standRollup(StandingItemView{Item: routine}, now); got != "Mondays 9am" {
		t.Fatalf("an item that never fired reads %q", got)
	}
	routine.LastFired = now.Add(-3 * 24 * time.Hour)
	if got := standRollup(StandingItemView{Item: routine}, now); got != "Mondays 9am · last Mon" {
		t.Fatalf("a fired routine reads %q", got)
	}

	stuck := bandItem("n", "keep main green", "/w", standing.WhenProbe, "when CI goes red")
	stuck.NeedsPerson = "the fix touches migrations"
	if got := standRollup(StandingItemView{Item: stuck}, now); got != tierYourCallWord+" · the fix touches migrations" {
		t.Fatalf("a stopped item reads %q", got)
	}
}

// THE CARD OBEYS THE EMPTINESS LAW. An item made ten seconds ago is a title, a
// place, a cadence and the keys — no `0 runs`, no `$0.00`, no line about a check
// that never happened.
func TestTheItemCardDrawsNothingItDoesNotKnow(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	fresh := bandItem("f", "remind me at 6 to leave", "/w/alpha", standing.WhenAt, "at 6 today")
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	card := strings.Join(plainAll(StandingItemCard(a, StandingItemView{Item: fresh},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	for _, want := range []string{"remind me at 6 to leave", "alpha · /w/alpha", "at 6 today", homeItemActions} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, card)
		}
	}
	for _, absent := range []string{"$0.00", "0 run", "checked", "last went off"} {
		if strings.Contains(card, absent) {
			t.Fatalf("the card of a brand new item drew %q:\n%s", absent, card)
		}
	}

	// AND IT DRAWS THE ARITHMETIC THE MOMENT THERE IS ANY.
	worked := fresh
	worked.Runs, worked.SpentUSD = 4, 0.08
	worked.LastFired = now.Add(-2 * time.Hour)
	worked.LastOutcome = "said it"
	card = strings.Join(plainAll(StandingItemCard(a, StandingItemView{Item: worked},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	if !strings.Contains(card, "4 runs · spent $0.08") {
		t.Fatalf("the card does not draw what it knows:\n%s", card)
	}
}

// AN ITEM'S CARD SAYS WHAT IT HAS ACTUALLY DONE LATELY, off the ledger, under
// the lifetime figures its own document remembers.
func TestTheItemCardSaysWhatTheThingDidThisWeek(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	item := bandItem("i1", "check the deploy", "/w/alpha", standing.WhenEvery, "every morning")
	item.Runs, item.SpentUSD = 9, 0.31

	// WITH NO LEDGER READER THE LINE IS SIMPLY ABSENT — a surface that cannot
	// ask must not draw a figure, and the rest of the card is unchanged.
	card := strings.Join(plainAll(StandingItemCard(a, StandingItemView{Item: item},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	if strings.Contains(card, homeWeekWord) {
		t.Fatalf("a surface with no ledger reader drew a weekly line:\n%s", card)
	}

	band := &standBand{runs: map[string]standing.Spend{"i1": {Fired: 3, USD: 0.04}}}
	band.wire(a)
	card = strings.Join(plainAll(StandingItemCard(a, StandingItemView{Item: item},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	if !strings.Contains(card, "9 runs · spent $0.31") {
		t.Fatalf("the card lost the lifetime figures:\n%s", card)
	}
	if !strings.Contains(card, "ran 3 times this week · $0.04") {
		t.Fatalf("the card does not say what it did this week:\n%s", card)
	}

	// AND A WEEK IN WHICH IT DID NOTHING SAYS NOTHING — not `0 runs this week`.
	quiet := newTestApp(&fakeAgent{model: "m"})
	(&standBand{runs: map[string]standing.Spend{"other": {Fired: 2, USD: 1}}}).wire(quiet)
	card = strings.Join(plainAll(StandingItemCard(quiet, StandingItemView{Item: item},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	if strings.Contains(card, homeWeekWord) {
		t.Fatalf("an item that fired nothing this week drew a weekly line:\n%s", card)
	}
}

// stuckItem is a watch STOPPED ON A QUESTION, which is what it takes for a
// standing item to have a row on the resting list at all ([readSwitcher] draws
// the ones that need somebody or are firing and leaves the rest to the standing
// place). Everything below is about the keys that act on such a row, so the row
// has to exist.
func stuckItem() standing.Item {
	item := bandItem("one", "remind me on Fridays", "/w/alpha", standing.WhenEvery, "Fridays")
	item.NeedsPerson = "should I send it to the whole team?"
	item.Updated = time.Now().Add(-time.Minute)
	return item
}

// p AND s GO THROUGH THE STORE, and the row afterwards is what the store says
// rather than what the keystroke hoped for.
func TestPauseAndStopReachTheStore(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", time.Now().Add(-time.Hour))
	band := &standBand{items: []standing.Item{stuckItem()}}
	a := lab.app(transcript)
	band.wire(a)
	a.openHome()
	a.home.pointItemForTest("one")

	drive(t, a, key("ctrl+e"))
	if len(band.saved) != 1 || band.saved[0].Status != standing.StatusPaused {
		t.Fatalf("ctrl+e did not pause through the store: %+v", band.saved)
	}
	if !strings.Contains(strings.Join(homeLines(a), "\n"), homeItemPaused) {
		t.Fatalf("the paused row does not say so:\n%s", strings.Join(homeLines(a), "\n"))
	}
	// AND THE ROW IS READ BACK OUT OF THE STORE RATHER THAN OUT OF THE KEYSTROKE.
	// The reading is rebuilt from the document the write landed in, so the verb
	// the row now offers is `resume it` — the one thing on the screen that could
	// not be right unless the write really happened and really came back.
	var offered []string
	for _, v := range a.homeRowVerbs() {
		offered = append(offered, v.word)
	}
	if strings.Join(offered, ", ") != switcherResumeWord {
		t.Fatalf("the paused row offers %v, want only %q", offered, switcherResumeWord)
	}

	a.home.pointItemForTest("one")
	drive(t, a, key("ctrl+x"))
	if len(band.saved) != 2 || band.saved[1].Status != standing.StatusRetired {
		t.Fatalf("ctrl+x did not stop through the store: %+v", band.saved)
	}
	if band.saved[1].RetiredWhy != homeStoppedWhy {
		t.Fatalf("a stopped item recorded %q, want %q", band.saved[1].RetiredWhy, homeStoppedWhy)
	}

	// A SURFACE WITH NO WAY TO WRITE SAYS SO rather than pretending.
	b := lab.app(transcript)
	b.stands = StandingSeam{Items: func(string) []standing.Item {
		return []standing.Item{stuckItem()}
	}}
	b.openHome()
	b.home.pointItemForTest("one")
	drive(t, b, key("ctrl+e"))
	if b.home.msg != homeItemNoStore {
		t.Fatalf("a read-only home said %q, want %q", b.home.msg, homeItemNoStore)
	}
}

// pointItemForTest puts the cursor on one item's row, by id.
func (h *homeView) pointItemForTest(id string) {
	h.pointItem(id)
	h.picked = true
}

// THE SEGMENT EXISTS ONLY WHEN THERE IS SOMETHING TO SAY, and it moves only
// while one of them is actually firing.
func TestTheKeepingAnEyeSegmentAppearsOnlyWhenThereAreItems(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width = 200
	// THE READING IS CACHED ON HOME'S OWN BEAT ([app.keepingCount]), so the
	// clock is pinned and walked past that beat between the states below —
	// which is the honest way to test a cache and the only way to test one
	// without sleeping.
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	stale := func() { now = now.Add(keepEvery + time.Second) }

	if strings.Contains(plain(a.status(200)), "keeping an eye") {
		t.Fatalf("a surface with the ambient side off grew a segment:\n%s", plain(a.status(200)))
	}

	band := &standBand{items: []standing.Item{
		bandItem("one", "remind me on Fridays", "/tmp/lab", standing.WhenEvery, "Fridays"),
		bandItem("two", "tell me when CI goes red", "/tmp/lab", standing.WhenProbe, "when CI goes red"),
	}}
	band.wire(a)
	stale()
	want := standWaitGlyph + homeKeepingWord + "2"
	if !strings.Contains(plain(a.status(200)), want) {
		t.Fatalf("the status row is missing %q:\n%s", want, plain(a.status(200)))
	}

	// AT REST THE GLYPH IS STILL. It breathes only while a firing is in flight.
	if strings.Contains(plain(a.status(200)), "keeping an eye on 2") && a.keepingWord() != want {
		t.Fatalf("a quiet band is animating: %q", a.keepingWord())
	}
	band.running = map[string]standing.RunningMark{
		"two": {PID: 1, Since: time.Now(), What: standing.RunningFiring},
	}
	stale()
	if a.keepingWord() == want {
		t.Fatalf("a firing band is not breathing: %q", a.keepingWord())
	}
	if !strings.HasSuffix(a.keepingWord(), homeKeepingWord+"2") {
		t.Fatalf("the breathing segment lost its count: %q", a.keepingWord())
	}

	// A PAUSED ITEM IS NOT KEEPING AN EYE ON ANYTHING.
	band.items[0].Status = standing.StatusPaused
	band.items[1].Status = standing.StatusPaused
	stale()
	if strings.Contains(plain(a.status(200)), "keeping an eye") {
		t.Fatalf("a band of paused items still claims to be watching:\n%s", plain(a.status(200)))
	}
}

// /status SAYS WHETHER ANYTHING IS LOOKED AT WITH NO WINDOW OPEN, and says
// nothing at all when it cannot know.
func TestStatusPrintsKeepingWatchOnlyWhenTheSeamAnswers(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if strings.Contains(a.statusText(), homeWatchLabel) {
		t.Fatalf("a surface with no watch seam printed a line:\n%s", a.statusText())
	}

	a.stands.Watch = func() (standing.WatchStatus, bool) { return standing.WatchStatus{}, false }
	if strings.Contains(a.statusText(), homeWatchLabel) {
		t.Fatalf("a seam with no answer printed a line:\n%s", a.statusText())
	}

	a.stands.Watch = func() (standing.WatchStatus, bool) {
		return standing.WatchStatus{Installed: true, LastWake: time.Now().Add(-4 * time.Minute)}, true
	}
	text := a.statusText()
	if !strings.Contains(text, homeWatchLabel) || !strings.Contains(text, homeWatchInstalled) ||
		!strings.Contains(text, homeWatchLastWord+"4m") {
		t.Fatalf("/status does not carry the derived line:\n%s", text)
	}

	a.stands.Watch = func() (standing.WatchStatus, bool) { return standing.WatchStatus{}, true }
	a.stands.Ticking = func() bool { return true }
	text = a.statusText()
	if !strings.Contains(text, homeWatchWindow) {
		t.Fatalf("an uninstalled timer does not say what still checks:\n%s", text)
	}
	if strings.Contains(text, homeWatchLastWord) {
		t.Fatalf("a machine that has never woken claimed a last check:\n%s", text)
	}
}

// AND IT SAYS SO WHEN NOTHING IS CHECKING AT ALL — the state a person asking
// /status most needs and the line could not say. It is not the word "off": the
// items are still there and the next window to open will check them.
func TestStatusSaysNothingIsCheckingAndWhy(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	// The seam is here — this machine has an ambient side — and neither the OS
	// timer nor this process is keeping time.
	a.stands.Items = func(string) []standing.Item { return nil }
	a.stands.Watch = func() (standing.WatchStatus, bool) { return standing.WatchStatus{}, true }

	text := a.statusText()
	if !strings.Contains(text, homeWatchLabel+"  ") || !strings.Contains(text, homeWatchNobody) {
		t.Fatalf("/status does not say that nothing is checking:\n%s", text)
	}
	if strings.Contains(text, "off") {
		t.Fatalf("/status called a working capability off:\n%s", text)
	}
	// Nothing has ever stood here, so the tail is the move that starts the
	// whole thing.
	a.stands.BackgroundTold = func() bool { return false }
	if text := a.statusText(); !strings.Contains(text, homeWatchNobody+homeWatchStart) {
		t.Fatalf("a machine where nothing has stood does not offer the way in:\n%s", text)
	}
	// And once something has stood — so the timer was installed once and said
	// so — nothing installed means the row is off, and the tail names the row
	// rather than inviting them to do the thing they already did.
	a.stands.BackgroundTold = func() bool { return true }
	text = a.statusText()
	if !strings.Contains(text, homeWatchNobody+homeWatchOffRow) {
		t.Fatalf("a machine whose background checks are off does not say so:\n%s", text)
	}
	if strings.Contains(text, homeWatchStart) {
		t.Fatalf("/status offered a move the person has already made:\n%s", text)
	}
}

// A SURFACE WITH NO AMBIENT SIDE AT ALL STAYS SILENT. The --host door wires no
// standing seam, and a capability that cannot work is absent, not off.
func TestStatusSaysNothingAboutWatchingWithNoStandingSeam(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if text := a.statusText(); strings.Contains(text, homeWatchLabel) || strings.Contains(text, homeWatchNobody) {
		t.Fatalf("a surface with no ambient side talked about checking:\n%s", text)
	}
}

// A FRAME THAT IS NOT WIDE ENOUGH DROPS THE CARD AND KEEPS THE INDEX, which is
// home's own law applied to the other kind of row: an index somebody can read
// beats a preview nobody can.
//
// THE FLOOR MOVED AND THE LAW DID NOT. The everyday card tier went with the
// strips, and there is exactly one width at which a card appears now —
// [homeCardMin], which is what the list and the card and the gutter add up to
// (homebridge.go). So the two frames this test uses are that floor and the cell
// below it, which is the honest way to pin a boundary: a round number beside it
// would stop meaning anything the day one of the columns changed.
func TestANarrowHomeDropsTheItemCard(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", time.Now().Add(-time.Hour))
	band := &standBand{items: []standing.Item{stuckItem()}}
	a := lab.app(transcript)
	band.wire(a)
	a.openHome()
	a.home.pointItemForTest("one")

	// The card's second band is `project · path`, and it is the one string on
	// the frame that only the card draws — the hint line at the foot names the
	// same keys the card's last band does, so a test that looked for those would
	// be finding the hint.
	const place = "alpha · /w/alpha"

	a.width, a.height = homeCardMin-1, 24
	narrow := strings.Join(homeLines(a), "\n")
	if !strings.Contains(narrow, "remind me on Fridays") {
		t.Fatalf("the narrow frame lost the row itself:\n%s", narrow)
	}
	if strings.Contains(narrow, place) {
		t.Fatalf("the narrow frame kept the card:\n%s", narrow)
	}

	a.width = homeCardMin
	wide := strings.Join(homeLines(a), "\n")
	if !strings.Contains(wide, place) {
		t.Fatalf("the frame at the card's own floor lost the card:\n%s", wide)
	}
}

// ENTER ON AN ITEM IS ITS PROVENANCE, and an item that never became a
// conversation says so rather than offering a door onto nothing.
func TestEnterOnAnItemOpensWhereItWasAsked(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", time.Now().Add(-time.Hour))
	item := stuckItem()
	item.Origin.Exchange = "exchange"
	band := &standBand{items: []standing.Item{item}}
	a := lab.app(transcript)
	band.wire(a)
	a.openHome()
	a.home.pointItemForTest("one")
	drive(t, a, key("enter"))
	if a.home.msg != homeItemNoDoor {
		t.Fatalf("an item made at home said %q, want %q", a.home.msg, homeItemNoDoor)
	}
	if !a.at(pageHome) {
		t.Fatal("home closed on a door that goes nowhere")
	}
}

// THE ◆ IS DERIVED AND NEVER ASSERTED: it means the thing went off after the
// last time this person spoke in the conversation that asked for it, and an item
// with no conversation to compare against does not wear it at all.
//
// IT IS DRAWN WHEREVER [StandingItemRow] IS, which since the switcher landed is
// the band under the phone tier and the drop-up under a query — the resting list
// paints its own rows from the reading ([switcherPaintRow]) and has three marks
// rather than five, and an item that only has NEWS earns no row on it at all
// ([readSwitcher]). So this is pinned at the tier that still draws the band; the
// derivation itself is one function and [TestNewsNeverOverwritesTheLouderMarks]
// holds it straight.
func TestTheNewsGlyphIsDerivedFromWhenYouLastSpoke(t *testing.T) {
	lab := newHomeLab(t)
	spoke := time.Now().Add(-3 * time.Hour)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", spoke)

	fired := bandItem("one", "post the standup note", "/w/alpha", standing.WhenEvery, "Mondays 9am")
	fired.Origin.Transcript = transcript
	fired.LastFired = spoke.Add(time.Hour)
	band := &standBand{items: []standing.Item{fired}}

	a := lab.app(transcript)
	band.wire(a)
	a.width, a.height = 50, 40
	a.openHome()
	if !strings.Contains(strings.Join(homeLines(a), "\n"), standNewsGlyph) {
		t.Fatalf("an item that fired since you last spoke is not marked:\n%s", strings.Join(homeLines(a), "\n"))
	}

	// FIRED BEFORE you were last in the room is not news.
	band.items[0].LastFired = spoke.Add(-time.Hour)
	a.refreshHome()
	if strings.Contains(strings.Join(homeLines(a), "\n"), standNewsGlyph) {
		t.Fatalf("an old firing was drawn as news:\n%s", strings.Join(homeLines(a), "\n"))
	}

	// AND AN ITEM WITH NO PROVENANCE ON THIS MACHINE NEVER WEARS IT. There is
	// nothing to compare against, and a mark that meant "new" for everything
	// unknown would be a mark that means nothing.
	band.items[0].Origin.Transcript = ""
	band.items[0].LastFired = time.Now()
	a.refreshHome()
	if strings.Contains(strings.Join(homeLines(a), "\n"), standNewsGlyph) {
		t.Fatalf("an item with no conversation behind it was drawn as news:\n%s", strings.Join(homeLines(a), "\n"))
	}
}

// NEEDS-YOU AND RUNNING OUTRANK NEWS. The store's own order is kept whole:
// turning a `▲` into a `◆` because something also fired would lose the one fact
// on the screen that costs a keystroke to act on.
func TestNewsNeverOverwritesTheLouderMarks(t *testing.T) {
	item := bandItem("x", "keep main green", "/w", standing.WhenProbe, "when CI goes red")
	item.NeedsPerson = "the fix touches migrations"
	if got := standGlyph(item, false, true, false); got != homeAskGlyph {
		t.Fatalf("news overwrote needs-you: %q", got)
	}
	quiet := bandItem("y", "check the deploy", "/w", standing.WhenEvery, "in 20 minutes")
	if got := standGlyph(quiet, true, true, false); got != homeLiveGlyph {
		t.Fatalf("news overwrote running: %q", got)
	}
	if got := standGlyph(quiet, false, true, false); got != standNewsGlyph {
		t.Fatalf("a waiting item with news reads %q", got)
	}
	// AND THE ASCII TIER HAS A STAND-IN FOR EVERY ONE OF THEM.
	for _, probe := range []struct {
		glyph string
		want  string
	}{
		{standGlyph(item, false, false, true), homeAskASCII},
		{standGlyph(quiet, true, false, true), homeLiveASCII},
		{standGlyph(quiet, false, true, true), standNewsASCII},
		{standGlyph(quiet, false, false, true), standWaitASCII},
	} {
		if probe.glyph != probe.want {
			t.Fatalf("the ascii tier drew %q, want %q", probe.glyph, probe.want)
		}
	}
	paused := quiet
	paused.Status = standing.StatusPaused
	if got := standGlyph(paused, false, false, false); got != standOffGlyph {
		t.Fatalf("a paused item reads %q", got)
	}
	if got := standGlyph(paused, false, false, true); got != standOffASCII {
		t.Fatalf("a paused item in ascii reads %q", got)
	}
}

// A WORKSPACE WHOSE ONLY CONTENT IS A WATCH IS STILL SOMETHING HOME HAS TO SHOW.
//
// The list is read off the projects root, so a directory somebody set a
// reminder in and never held a conversation in had no heading, no band and no
// row — the person set the thing up and the one screen that exists to say what
// is true said nothing about it. The place is synthesised from the item's own
// workspace ([app.readBareBands]), and its rows are cursor stops with the item's
// own keys on them.
//
// ── WHERE THIS LAW NOW HOLDS, AND WHERE IT DOES NOT ─────────────────────────
//
// THE HEADING IS GONE, because the resting list has no headings: it is one flat
// ranked reading with the project as a tag on the row (switcher.go). So the
// clauses about a heading's position in the recency order and about the word
// `elsewhere` on it died with the tree they were about.
//
// AND THE ROW ITSELF ONLY SURVIVES AT THE PHONE TIER, which is where this is
// drawn. [readSwitcher] is handed [homeView.world] and walks `world.Projects`,
// and a bare workspace is not one of them — it lives in [homeView.bare], which
// only [homeView.everyProject] reads and only homephone.go calls. So at every
// width above the phone breakpoint a watch on a conversation-less workspace has
// no row on home AT ALL, even one stopped on a question. That is a hole in the
// production reading rather than a law this wave repealed, and it is reported
// as one; this test pins the behaviour where it is still whole so that the day
// the seam is closed there is something to widen rather than something to write.
func TestAWorkspaceWhoseOnlyContentIsAWatchIsStillOnTheScreen(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", now.Add(-3*time.Hour))
	lab.session("beta", "s2", "Older Notes", "/w/beta", now.Add(-5*time.Hour))

	band := &standBand{}
	watch := bandItem("watch", "tell me when CI on main goes red", "/w/quiet", standing.WhenProbe, "when CI goes red")
	watch.NeedsPerson = "the build has been red since Tuesday"
	watch.LastChecked = now.Add(-time.Minute)
	watch.Created = now.Add(-time.Hour)
	band.items = []standing.Item{
		bandItem("here", "remind me on Fridays", "/w/alpha", standing.WhenEvery, "Fridays"),
		watch,
	}

	a := lab.app(transcript)
	a.workspace = "/w/quiet"
	band.wire(a)
	a.width, a.height = 50, 40
	openHomeOn(a, transcript)

	lines := homeLines(a)
	joined := strings.Join(lines, "\n")
	// THE WATCH IS ON THE SCREEN, in the section for everything that has stopped
	// and is asking for a hand — which is where a person looks first.
	row := homeRowAt(lines, "tell me when CI")
	if row < 0 {
		t.Fatalf("the items-only workspace is invisible:\n%s", joined)
	}
	// AND THE WORKSPACE ITSELF HAS A PLACE OF ITS OWN, named from the item's
	// address because nothing else on this machine names it.
	place := -1
	for i, line := range a.home.lines {
		if line.kind == homeProject && strings.Contains(line.project, "quiet") {
			place = i
		}
	}
	if place < 0 {
		t.Fatalf("the items-only workspace got no place of its own:\n%s", joined)
	}
	// THE ROW IS A REAL CURSOR STOP with the item's own keys on it: the fold
	// laws and the band's own writes reach it exactly as they reach any other.
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeItem && line.item.ID == "watch" {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the watch is not a line of the column:\n%s", joined)
	}
	a.home.cursor = at
	drive(t, a, key("ctrl+e"))
	if len(band.saved) != 1 || band.saved[0].ID != "watch" || band.saved[0].Status != standing.StatusPaused {
		t.Fatalf("ctrl+e on the row did not pause it, the store saw %+v", band.saved)
	}
}

// ── `●` IS THE SEAM'S ANSWER AND NOBODY ELSE'S ──────────────────────────────
//
// A firing happens in whichever process holds the tick lock — another window,
// or the operating system's timer with nothing open at all — so the glyph is
// read from a marker that process left ([StandingSeam.Running]) and never
// guessed from the item's own document. These pin the whole of what a surface
// does with that answer: the mark on the row, the sentence on the card, and the
// stillness of both when nobody is asking.
func TestAnItemBeingCheckedElsewhereWearsTheDotAndSaysSince(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	item := bandItem("c", "tell me when CI goes red", "/w/alpha", standing.WhenProbe, "when CI goes red")

	view := StandingItemView{
		Item:    item,
		Running: true,
		Mark:    standing.RunningMark{PID: 4321, Since: now.Add(-4 * time.Second), What: standing.RunningChecking},
	}
	if got := standGlyph(view.Item, view.Running, view.News, false); got != homeLiveGlyph {
		t.Fatalf("an item a pass is on reads %q, wanted %q", got, homeLiveGlyph)
	}
	if got := standRollup(view, now); got != "checking now · since 4s" {
		t.Fatalf("the row's tail reads %q", got)
	}
	card := strings.Join(plainAll(StandingItemCard(a, view, "alpha", "/w/alpha", 60, 20, now)), "\n")
	if !strings.Contains(card, homeLiveGlyph+" checking now · since 4s") {
		t.Fatalf("the card does not say what the pass is doing:\n%s", card)
	}

	// THE OTHER HALF OF A PASS IS THE OTHER WORD. Checking is the look — a
	// probe, a fingerprint, the sentinel — and firing is the work that followed
	// a yes; they cost different money and a row that said "running" for both
	// would drop the only fact this glyph carries.
	view.Mark = standing.RunningMark{PID: 4321, Since: now, What: standing.RunningFiring}
	if got := standRollup(view, now); got != "firing now" {
		t.Fatalf("a firing that just started reads %q, wanted no age at all", got)
	}
	card = strings.Join(plainAll(StandingItemCard(a, view, "alpha", "/w/alpha", 60, 20, now)), "\n")
	if !strings.Contains(card, homeLiveGlyph+" firing now") {
		t.Fatalf("the card does not say it is firing:\n%s", card)
	}

	// AND A MINUTE IN IT IS THE ORDINARY AGE AGAIN, because seconds have stopped
	// being the interesting unit.
	view.Mark.Since = now.Add(-3 * time.Minute)
	if got := standRollup(view, now); got != "firing now · since 3m" {
		t.Fatalf("a firing three minutes old reads %q", got)
	}

	// NOTHING RUNNING, NOTHING SAID. The card of the same item with no marker
	// behind it draws no line about now at all.
	quiet := strings.Join(plainAll(StandingItemCard(a, StandingItemView{Item: item},
		"alpha", "/w/alpha", 60, 20, now)), "\n")
	if strings.Contains(quiet, "now ·") || strings.Contains(quiet, "checking now") {
		t.Fatalf("an item nobody is on claims to be running:\n%s", quiet)
	}
}

// A SURFACE WITH NO WAY TO ASK NEVER MAKES THE CLAIM. A nil Running is a home
// where no row ever wears `●` and the status segment never breathes — which is
// honest, because the glyph is a statement about this instant.
func TestASurfaceThatCannotAskNeverDrawsTheDot(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", time.Now().Add(-time.Hour))
	band := &standBand{items: []standing.Item{
		bandItem("one", "tell me when CI goes red", "/w/alpha", standing.WhenProbe, "when CI goes red"),
	}}
	a := lab.app(transcript)
	band.wire(a)
	a.stands.Running = nil
	a.openHome()

	views := a.standItems("/w/alpha")
	if len(views) != 1 || views[0].Running {
		t.Fatalf("a surface with no seam decided something was running: %+v", views)
	}
	if strings.Contains(strings.Join(homeLines(a), "\n"), homeLiveGlyph+" tell me when CI goes red") {
		t.Fatalf("a row wore `●` with nothing behind it:\n%s", strings.Join(homeLines(a), "\n"))
	}
}

// ── a rule that never wakes ─────────────────────────────────────────────────

// A RULE SAYS `holds` WHEREVER A WATCH WOULD SAY WHAT IT LAST FOUND.
//
// Every clause the other kinds carry is a fact a rule will never have: it is
// never checked, so there is no last look; it never fires, so there is no last
// firing; it is never due, so there is no cadence and no countdown. The one word
// is what IS true of it, and it is the person's own verb — nothing on a screen
// says "hold kind" or "no trigger".
func TestARuleSaysItHolds(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	rule := bandItem("h", "never touch the public API", "/w", standing.WhenHold, "")
	rule.Does, rule.Rails = standing.Action{}, standing.Rails{}

	if got := standRollup(StandingItemView{Item: rule}, now); got != standHoldsWord {
		t.Fatalf("home's row for a rule reads %q, wanted %q", got, standHoldsWord)
	}
	if got := standWhenClause(rule, now); got != standHoldsWord {
		t.Fatalf("the band's clause for a rule reads %q, wanted %q", got, standHoldsWord)
	}
	// AND A CADENCE SOMEBODY WROTE ONTO ONE ANYWAY IS NOT QUOTED AS ITS TIME.
	// "always" is not a moment, and a row that said it there would read as when
	// this thing next wakes up.
	loud := rule
	loud.When.Words = "always"
	if got := standWhenClause(loud, now); got != standHoldsWord {
		t.Fatalf("a rule's clause read %q back as a cadence", got)
	}
	// A paused rule still says it is paused: the status of the thing outranks
	// what the thing is, exactly as it does for every other kind.
	paused := rule
	paused.Status = standing.StatusPaused
	if got := standRollup(StandingItemView{Item: paused}, now); got != homeItemPaused {
		t.Fatalf("a paused rule reads %q", got)
	}
}

// THE RULES COME AFTER THE APPOINTMENTS, NEWEST FIRST.
//
// The band answers "what happens next", and a rule has no next — it is simply
// true, and it will still be true after everything above it has gone off. Sorted
// among the appointments it would take the front of the band on the strength of
// having none, which is soonest-first saying something false.
func TestTheBandsListRulesAfterEverythingWithATime(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	soon := bandItem("a", "remind me at 10", "/w", standing.WhenAt, "at 10")
	soon.NextDue = now.Add(time.Hour)
	later := bandItem("b", "draft the weekly update", "/w", standing.WhenEvery, "Mondays 9am")
	later.NextDue = now.Add(48 * time.Hour)
	old := bandItem("c", "we use tabs here", "/w", standing.WhenHold, "")
	old.Created = now.Add(-72 * time.Hour)
	fresh := bandItem("d", "never touch the public API", "/w", standing.WhenHold, "")
	fresh.Created = now.Add(-time.Hour)

	views := []StandingItemView{{Item: old}, {Item: fresh}, {Item: later}, {Item: soon}}
	standByNextDue(views)
	var order []string
	for _, view := range views {
		order = append(order, view.Item.ID)
	}
	if strings.Join(order, "") != "abdc" {
		t.Fatalf("the band reads %v, wanted the appointments soonest first and the rules newest first behind them", order)
	}
}
