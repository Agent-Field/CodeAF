package tui3

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// says appends one spoken line to a transcript, in the journal's own message
// shape, so that [session.Peek] has a last-said to report. The lab's sessions
// otherwise hold only a header line, which is a conversation nobody spoke in.
func (l *homeLab) says(transcript, role, text string) {
	l.t.Helper()
	file, err := os.OpenFile(transcript, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.t.Fatal(err)
	}
	defer file.Close()
	line := `{"type":"message","role":"` + role + `","content":` + strconv.Quote(text) + `}` + "\n"
	if _, err := file.WriteString(line); err != nil {
		l.t.Fatal(err)
	}
}

// homeCardFor is the detail column for the row holding a transcript, stripped to
// words, one string per line.
//
// IT WIDENS THE FRAME TO A CARD TIER FIRST. There is no card at all below
// [homeCardMin] any more — at every ordinary width the row's own note carries
// the fact the card was for (SCREEN 1a) — so a test whose subject is a BAND has
// to be asked at a width where a card exists, and a hundred and sixty columns is
// where one does.
func homeCardFor(t *testing.T, a *app, transcript string) []string {
	t.Helper()
	if width, _ := a.size(); width < homeCardMin {
		a.width, a.height = homeCardMin, max(a.height, 30)
		a.home.build()
	}
	a.home.point(transcript)
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("no detail column at width %d", width)
	}
	var out []string
	for _, line := range a.homeDetail(right, 20, a.pal) {
		out = append(out, ansi.Strip(line))
	}
	return out
}

// cardLine finds the first card line containing a phrase, or -1.
func cardLine(card []string, phrase string) int {
	for at, line := range card {
		if strings.Contains(line, phrase) {
			return at
		}
	}
	return -1
}

// ── THERE IS NO CARD BELOW [homeCardMin] ────────────────────────────────────

// ONE COLUMN, AND A SECOND ONE ONLY WHEN THE WIDTH IS REALLY THERE.
//
// The everyday card tier went with the strips (homebridge.go). At every ordinary
// width the right-hand note on each row carries the one fact the card was for,
// and a card drawn beside it would be showing a person what pressing enter shows
// a beat later (SCREEN 1a). The tier begins at exactly [homeSwitchFull] +
// [homeGutter] + [homeCardCol] — where the list has everything it asks for AND a
// card still fits — so it is the sum of the parts rather than a number chosen
// next to them, and one cell under it there is no second column at all.
func TestBelowTheCardTierTheRowCarriesTheFactInstead(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "bounty reward companies", here, now.Add(-3*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "9", Name: "filings", Label: "Read the filings", Title: "Read the filings",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "9", Title: "Read the filings", State: "running", StartedAt: now.Add(-time.Minute)})

	a := lab.app(mine)
	a.width, a.height = homeCardMin-1, 30
	a.openHome()

	// ONE CELL SHORT IS ONE COLUMN, and the column is the whole frame.
	if left, right := homeColumns(a.width); right != 0 || left != a.width {
		t.Fatalf("a %d-cell frame split into %d and %d, want the whole width and no card", a.width, left, right)
	}
	if tier := a.homeTierNow(); tier != homeTierList {
		t.Fatalf("a %d-cell frame is tier %v, want the list", a.width, tier)
	}
	// AND THE FACT THE CARD CARRIED IS ON THE ROW'S OWN NOTE, which is why there
	// is nothing to miss: the note is the reading, not a consolation for one.
	// (What a running node is DOING rides beside it — `· reading filings` — and
	// is never on disk, so no lab can put it there: [session.TaskIndexEntry]'s
	// Activity is built live and marked `json:"-"`.)
	text := homeText(a)
	if !strings.Contains(text, "1 task running") {
		t.Fatalf("the row does not carry the fact the card was for:\n%s", text)
	}
	if strings.Contains(text, "Read the filings") {
		t.Fatalf("a card was drawn under the tier:\n%s", text)
	}

	// AND ONE CELL WIDER THERE IS ONE, about the same row.
	a.width = homeCardMin
	a.openHome()
	if _, right := homeColumns(a.width); right != homeCardCol {
		t.Fatalf("the tier's own width lent the card %d cells, want %d", right, homeCardCol)
	}
	card := homeCardFor(t, a, other)
	if cardLine(card, "Bounty Reward Companies") < 0 || cardLine(card, "Read the filings") < 0 {
		t.Fatalf("the card at the tier's floor is not about the row:\n%s", strings.Join(card, "\n"))
	}
}

// ── THE CARD IS SHAPED BY THE ROW'S STATE ───────────────────────────────────

// A quiet conversation's work reads as a ledger: the task NAMED FIRST, and what
// it came to on the line under it (homeband_work.go). Nothing spells a state
// word on a landed row — done wears no mark at all.
//
// THE LAST THING SAID IS NOT ON THIS CARD ANY MORE, AND NOTHING CARRIES IT. The
// `leftoff` band is one of the eleven the switcher's card dropped (place_home.go's
// [homeCardBands]): the card exists only where the width is genuinely spare, and
// a card drawn there has to be worth more than the row beside it — showing a
// person the sentence that `enter` shows a beat later is the one thing it may not
// spend that width on. So this test no longer asks for it; the band is still
// registered and still drawn for the surfaces that want it.
func TestAQuietCardReadsItsWorkAsALedger(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "landing page copy", "/tmp/alpha", now.Add(-2*time.Hour))
	lab.says(other, "user", "tighten the hero copy")
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "hero-rewrite", Label: "Hero rewrite", Title: "Hero rewrite",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		EndedAt: now.Add(-time.Hour), Outcome: "Led with the outcome and cut the copy by half.",
	})

	a := lab.app(mine)
	a.openHome()
	card := homeCardFor(t, a, other)
	work := cardLine(card, "Hero rewrite")
	outcome := cardLine(card, "Led with the outcome")
	if work < 0 || outcome < 0 {
		t.Fatalf("the quiet card is missing a band (work %d, outcome %d):\n%s",
			work, outcome, strings.Join(card, "\n"))
	}
	if said := cardLine(card, "tighten the hero copy"); said >= 0 {
		t.Fatalf("the card spent its width repeating what enter would show:\n%s",
			strings.Join(card, "\n"))
	}
	if outcome != work+1 {
		t.Fatalf("the outcome does not hang under its task:\n%s", strings.Join(card, "\n"))
	}
	if !strings.HasPrefix(card[outcome], strings.Repeat(" ", homeWorkIndent)) {
		t.Fatalf("the outcome is not indented under its task: %q", card[outcome])
	}
	// And nothing on a landed row claims to be happening, or claims to be done:
	// the absence of a mark is what `done` looks like here.
	if text := strings.Join(card, "\n"); strings.Contains(text, "done Hero") || strings.Contains(text, "running") {
		t.Fatalf("the ledger still spells state words:\n%s", text)
	}
}

// A conversation with work running says so ON the work: the name first, and
// `● running` with what it is doing under it.
//
// IT USED TO SAY IT TWICE — the `state` band over the top of the card and the
// work band under it — and the second reading moved one column left. What a
// conversation is DOING is the row's own right-hand note now
// ([switcherConversationNote]: "2 tasks running · reading filings"), where a
// person who is not pointing at anything can read it (SCREEN 1a), and the card
// keeps only the reading that is about the WORK.
func TestARunningCardLeadsTheRowWithItsState(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "port the picker", "/tmp/alpha", now.Add(-time.Hour))
	lab.says(other, "user", "port the resume picker to v3")
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port", Label: "Port the picker", Title: "Port the picker",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "7", Title: "Port the picker", State: "running", StartedAt: now.Add(-90 * time.Second)})

	a := lab.app(mine)
	a.openHome()
	if !a.homeAnimating() {
		t.Fatal("a row with work running did not earn the paint clock")
	}
	// THE ROW SAYS WHAT IT IS DOING, before any card is asked for — this is the
	// reading the old `state` band was for, on the surface that carries it now.
	if text := homeText(a); !strings.Contains(text, "1 task running") {
		t.Fatalf("the row does not say what the conversation is doing:\n%s", text)
	}
	card := homeCardFor(t, a, other)
	work := cardLine(card, "Port the picker")
	state := cardLine(card, homeLiveGlyph+" running")
	if work < 0 || state < 0 {
		t.Fatalf("the running card is missing a band (work %d, state %d):\n%s",
			work, state, strings.Join(card, "\n"))
	}
	if state != work+1 {
		t.Fatalf("the state does not lead the line under its task:\n%s", strings.Join(card, "\n"))
	}
	// AND THE CARD DOES NOT SAY IT A SECOND TIME. `state` and `leftoff` both went
	// with the strips; a card standing beside a row that already carries the fact
	// would be the layout arguing with itself.
	if said := cardLine(card, "port the resume picker"); said >= 0 {
		t.Fatalf("the card drew the last-said back onto itself:\n%s", strings.Join(card, "\n"))
	}
}

// Every node of a family is its own row on the card, root and child alike. The
// SHAPE of a family — child indented under the root that started it — is the
// task page's to draw (task.go's roster); this band is a list of things that
// happened, name first, and it draws them in the index's own order.
func TestEveryNodeOfAFamilyIsItsOwnRowOnTheCard(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "the big port", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port", Label: "Port everything", Title: "Port everything",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "8", Parent: "7", Name: "port-tests", Label: "Port the tests", Title: "Port the tests",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "8", Title: "Port the tests", State: "running", StartedAt: now.Add(-time.Minute)})

	a := lab.app(mine)
	a.openHome()
	card := homeCardFor(t, a, other)
	root := cardLine(card, "Port everything")
	kid := cardLine(card, "Port the tests")
	if root < 0 || kid < 0 {
		t.Fatalf("the family is not on the card (root %d, kid %d):\n%s", root, kid, strings.Join(card, "\n"))
	}
	if strings.HasPrefix(card[root], " ") || strings.HasPrefix(card[kid], " ") {
		t.Fatalf("a task name on this band is indented:\n%s", strings.Join(card, "\n"))
	}
}

// ── SINCE YOU LAST LOOKED ───────────────────────────────────────────────────

// Work that landed after home was last closed is still visible the moment the
// screen opens, and closing home is what writes the next origin — so looking at
// the news is what retires it.
//
// THE ✓ ON THE ROW IS GONE AND NOTHING BROUGHT IT BACK. The switcher's marks say
// what a row IS — `?` needs you, `◐` moving, `○` at rest, `=` a paused watch —
// and a tick on every settled row would spend the loudest cell in the list on
// the rows that want nothing, which is the same argument homeband_work.go's
// "done is the absence of a mark" already makes one column over. The law the
// tick was defending — you can see that something landed while you were away —
// is carried by two things that say it in words instead:
//
//   - THE ROW'S OWN NOTE, `3 files made` ([switcherConversationNote]), which is
//     the one fact the card used to be for (SCREEN 1a);
//   - THE `since you left` LEDGER at the top of the list, which is a door into
//     the page that owns what happened rather than a mark on a row.
//
// The card's caption over the fresh rows ([homeFreshWord]) is unchanged, and it
// is the last per-row NEWS claim on this screen.
func TestWorkLandedSinceYouLastLookedIsMarked(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "pricing research", here, now.Add(-2*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		EndedAt: now.Add(-10 * time.Minute), FilesChanged: 3, Outcome: "Both models drafted.",
	})
	session.NoteLook(lab.root, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if strings.Contains(text, glyphDone+" Pricing Research") {
		t.Fatalf("a resting row wears a tick again:\n%s", text)
	}
	if !strings.Contains(text, "3 files made") {
		t.Fatalf("the row does not say what landed while you were away:\n%s", text)
	}
	if !strings.Contains(text, "since you left") || !strings.Contains(text, "landed") {
		t.Fatalf("the ledger does not say anything landed:\n%s", text)
	}
	card := homeCardFor(t, a, other)
	if cardLine(card, homeFreshWord) < 0 {
		t.Fatalf("the card does not caption the news:\n%s", strings.Join(card, "\n"))
	}

	// Closing is the look: the stamp advances, and the next open marks nothing.
	a.homeKey(key("esc"))
	if look := session.LastLook(lab.root); !look.After(now.Add(-time.Minute)) {
		t.Fatalf("closing home did not write the look stamp (got %v)", look)
	}
	a.openHome()
	text = homeText(a)
	for _, stale := range []string{"3 files made", "since you left", glyphDone + " Pricing Research"} {
		if strings.Contains(text, stale) {
			t.Fatalf("news survived being looked at (%q):\n%s", stale, text)
		}
	}
	if card := homeCardFor(t, a, other); cardLine(card, homeFreshWord) >= 0 {
		t.Fatalf("the card still captions news that has been looked at:\n%s", strings.Join(card, "\n"))
	}
}

// The first look has no origin, so it marks NOTHING — the alternative is a
// first open where everything ever done shouts "new".
//
// THIS IS A LAW THE PRODUCTION CODE STATES IN AS MANY WORDS ([homeView.seen]:
// "Zero means there is no origin to measure from — a first look — and nothing at
// all is marked"), and every surface that measures from the stamp owes it. The
// tick and the `landed` count went with the strips, so the surfaces it is owed
// on are the three that replaced them: the row's own note, the `since you left`
// ledger, and the card's caption over the fresh rows.
//
// TWO OF THE THREE DO NOT PAY IT YET, AND THAT IS A BUG IN THE READING RATHER
// THAN A LAW THAT DIED. [app.homeFresh] guards the caption with `seen.IsZero()`;
// switcher.go's [switcherReading.addLedger] and [switcherConversationNote] both
// compare against `seen` with no such guard, so a zero stamp makes every task
// this machine ever ran land "since you left" and every quiet row claim files it
// made "while you were away". The fix is one condition in each, and this test
// stays red until it is there — it is the same claim
// [TestTheSwitcherKeepsUnknownAndZeroFactsEmpty] already makes about `since you
// left`, on a machine that has actually done some work.
func TestTheFirstLookMarksNothing(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "pricing research", here, now.Add(-2*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		EndedAt: now.Add(-10 * time.Minute), FilesChanged: 3,
	})

	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if strings.Contains(text, glyphDone+" Pricing Research") {
		t.Fatalf("a first look lit the tick on a row:\n%s", text)
	}
	// A WINDOW WITH NO ORIGIN CANNOT SAY `since you left`, because there is no
	// since. It is the same claim [TestTheSwitcherKeepsUnknownAndZeroFactsEmpty]
	// makes about a reading with nothing in it, on a machine that has done work.
	if strings.Contains(text, "since you left") || strings.Contains(text, "landed") {
		t.Fatalf("a first look invented a ledger:\n%s", text)
	}
	// AND NEITHER CAN A ROW'S NOTE, which is scoped to the same stamp: "3 files
	// made" means "since you last looked", and with no last look it means
	// "ever", which is a different sentence.
	if strings.Contains(text, "files made") {
		t.Fatalf("a first look counted everything a row ever did as news:\n%s", text)
	}
	if card := homeCardFor(t, a, other); cardLine(card, homeFreshWord) >= 0 {
		t.Fatalf("a first look captioned a card:\n%s", strings.Join(card, "\n"))
	}
}

// And the conversation THIS window is in never carries the NEWS MARK: its
// landings were watched happening, not missed.
//
// THE MARK THIS IS STILL ABOUT IS THE CARD'S CAPTION, and it is the only per-row
// news claim left on the screen ([app.homeFresh] holds both of its exemptions —
// no stamp, and this window's own conversation). The `since you left` ledger is
// deliberately NOT held to it: it is a count about the MACHINE with a door into
// the tasks page, not a mark on a row, and every task that landed while home was
// closed belongs in it whichever conversation ran it.
func TestYourOwnLandingsAreNotNews(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
		EndedAt: now.Add(-10 * time.Minute), FilesChanged: 3,
	})
	session.NoteLook(lab.root, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	if card := homeCardFor(t, a, mine); cardLine(card, homeFreshWord) >= 0 {
		t.Fatalf("this window's own work was captioned as news:\n%s", strings.Join(card, "\n"))
	}
}

// ── THE PAINT CLOCK ─────────────────────────────────────────────────────────

// Home earns the fast clock exactly while a visible row runs, and never in the
// linear tier, whose law is that nothing animates.
func TestHomeAnimatesOnlyWhileWorkRuns(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "quiet", "/tmp/alpha", now)
	a := lab.app(mine)
	a.openHome()
	if a.homeAnimating() {
		t.Fatal("a home with nothing running claims to be animating")
	}
	a.homeKey(key("esc"))

	lab.session("-tmp-alpha", "aaaa000000000002", "the long one", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port", Label: "Port the picker", Title: "Port the picker",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "7", Title: "Port the picker", State: "running", StartedAt: now})
	a.openHome()
	if !a.homeAnimating() {
		t.Fatal("a home with a running row does not animate")
	}
	a.linear = true
	if a.homeAnimating() {
		t.Fatal("the linear tier animated")
	}
}

// ── THE FACTS FOOTER ────────────────────────────────────────────────────────

// The footer carries the one physical number the index holds — how many files
// the work wrote — and the work band itself says how much of it did not fit.
func TestTheFactsLineCarriesTheFilesFigure(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "the busy one", "/tmp/alpha", now.Add(-time.Hour))
	for i := 0; i < 6; i++ {
		lab.task("-tmp-alpha", session.TaskIndexEntry{
			ID: strconv.Itoa(i + 1), Name: "job", Label: "Job " + strconv.Itoa(i+1), Title: "Job " + strconv.Itoa(i+1),
			Status: string(session.TaskDone), SessionID: "aaaa000000000002",
			EndedAt: now.Add(-time.Duration(i+1) * time.Hour), FilesChanged: i,
		})
	}

	a := lab.app(mine)
	a.openHome()
	card := strings.Join(homeCardFor(t, a, other), "\n")
	// 0+1+2+3+4+5 files across the rows.
	if !strings.Contains(card, "touched 15 files") {
		t.Fatalf("the footer does not carry the files figure:\n%s", card)
	}
	// THE COUNT BELONGS TO THE BAND THAT COULD NOT SHOW THEM, not to the footer:
	// the work band folds past three and says how many are behind the line
	// (homeband_work.go).
	if !strings.Contains(card, "tasks") {
		t.Fatalf("the work band does not say what it could not show:\n%s", card)
	}
}
