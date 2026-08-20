package tui3

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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

// card is the detail column for the row holding a transcript, stripped to
// words, one string per line.
func homeCard(t *testing.T, a *app, transcript string) []string {
	t.Helper()
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

// ── THE CARD IS SHAPED BY THE ROW'S STATE ───────────────────────────────────

// A quiet conversation leads with where you left off, and reads its work as a
// ledger: the task under the last-said, its outcome sentence under the task.
func TestAQuietCardLeadsWithWhereYouLeftOff(t *testing.T) {
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
	card := homeCard(t, a, other)
	said := cardLine(card, "tighten the hero copy")
	work := cardLine(card, "Hero rewrite")
	outcome := cardLine(card, "Led with the outcome")
	if said < 0 || work < 0 || outcome < 0 {
		t.Fatalf("the quiet card is missing a band (said %d, work %d, outcome %d):\n%s",
			said, work, outcome, strings.Join(card, "\n"))
	}
	if said > work {
		t.Fatalf("a quiet card puts the work above where you left off:\n%s", strings.Join(card, "\n"))
	}
	if outcome != work+1 {
		t.Fatalf("the outcome does not hang under its task:\n%s", strings.Join(card, "\n"))
	}
	// And nothing on a quiet card claims to be happening: no spinner, no state
	// words — the ✓ carries "done" by itself.
	if text := strings.Join(card, "\n"); strings.Contains(text, "done Hero") || strings.Contains(text, "running") {
		t.Fatalf("the ledger still spells state words:\n%s", text)
	}
}

// A conversation with work running leads with the work, and the work is ALIVE:
// the spinner cell, and a count-up from when the presence says the node began.
func TestARunningCardLeadsWithTheWorkAlive(t *testing.T) {
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
	card := homeCard(t, a, other)
	work := cardLine(card, "Port the picker")
	said := cardLine(card, "port the resume picker")
	if work < 0 || said < 0 {
		t.Fatalf("the running card is missing a band (work %d, said %d):\n%s",
			work, said, strings.Join(card, "\n"))
	}
	if work > said {
		t.Fatalf("a running card puts the last-said above the moving work:\n%s", strings.Join(card, "\n"))
	}
	if !strings.Contains(card[work], tokens.Spinner(a.paints/spinnerStep)) {
		t.Fatalf("the running row does not turn the spinner:\n%s", card[work])
	}
	if !strings.Contains(card[work], "1m") {
		t.Fatalf("the running row does not count up from its start:\n%s", card[work])
	}
}

// While any of a family runs, its children are expanded under the root — the
// shape of the work is the thing a glance cannot get anywhere else.
func TestALiveFamilyExpandsOnTheCard(t *testing.T) {
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
	// The session vouches for the CHILD only: the root reads as not-turning, and
	// the family still expands because one of its nodes runs.
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "8", Title: "Port the tests", State: "running", StartedAt: now.Add(-time.Minute)})

	a := lab.app(mine)
	a.openHome()
	card := homeCard(t, a, other)
	root := cardLine(card, "Port everything")
	kid := cardLine(card, "Port the tests")
	if root < 0 || kid < 0 {
		t.Fatalf("the family is not on the card (root %d, kid %d):\n%s", root, kid, strings.Join(card, "\n"))
	}
	if kid != root+1 {
		t.Fatalf("the child does not sit under its root:\n%s", strings.Join(card, "\n"))
	}
	if !strings.HasPrefix(card[kid], strings.Repeat(" ", homeOutcomeIndent)) {
		t.Fatalf("the child row is not indented under its root: %q", card[kid])
	}
	if strings.HasPrefix(card[root], " ") {
		t.Fatalf("the root row is indented: %q", card[root])
	}
}

// ── SINCE YOU LAST LOOKED ───────────────────────────────────────────────────

// Work that landed after home was last closed is marked — the ✓ on the row, the
// `landed` count in its note, the caption on its card — and closing home is
// what writes the next origin, so looking at the news is what retires it.
func TestWorkLandedSinceYouLastLookedIsMarked(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "pricing research", "/tmp/alpha", now.Add(-2*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		EndedAt: now.Add(-10 * time.Minute), Outcome: "Both models drafted.",
	})
	session.NoteLook(lab.root, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if !strings.Contains(text, glyphDone+" Pricing Research") {
		t.Fatalf("the row that landed work does not wear the tick:\n%s", text)
	}
	if !strings.Contains(text, "1 "+homeLandedWord) {
		t.Fatalf("the row does not count what landed:\n%s", text)
	}
	card := homeCard(t, a, other)
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
	if strings.Contains(text, homeLandedWord) || strings.Contains(text, glyphDone+" Pricing Research") {
		t.Fatalf("news survived being looked at:\n%s", text)
	}
}

// The first look has no origin, so it marks NOTHING — the alternative is a
// first open where everything ever done shouts "new".
func TestTheFirstLookMarksNothing(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "pricing research", "/tmp/alpha", now.Add(-2*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		EndedAt: now.Add(-10 * time.Minute),
	})

	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if strings.Contains(text, homeLandedWord) || strings.Contains(text, homeFreshWord) {
		t.Fatalf("a first look invented news:\n%s", text)
	}
	if strings.Contains(text, glyphDone+" Pricing Research") {
		t.Fatalf("a first look lit the tick on a row:\n%s", text)
	}
}

// And the conversation THIS window is in never carries the mark: its landings
// were watched happening, not missed.
func TestYourOwnLandingsAreNotNews(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "tiers", Label: "Model the tiers", Title: "Model the tiers",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
		EndedAt: now.Add(-10 * time.Minute),
	})
	session.NoteLook(lab.root, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	if text := homeText(a); strings.Contains(text, homeLandedWord) {
		t.Fatalf("this window's own work was marked as news:\n%s", text)
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

// The footer counts what the card could not show, and carries the one physical
// number the index holds: how many files the work wrote.
func TestTheFactsLineCountsWhatTheCardCouldNotShow(t *testing.T) {
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
	card := strings.Join(homeCard(t, a, other), "\n")
	if !strings.Contains(card, "6 tasks") {
		t.Fatalf("the footer does not count the tasks the card could not show:\n%s", card)
	}
	// 0+1+2+3+4+5 files across the rows.
	if !strings.Contains(card, "touched 15 files") {
		t.Fatalf("the footer does not carry the files figure:\n%s", card)
	}
	// And a card that showed everything does not restate the count: the quiet
	// lab in [TestTheFactsFooterOmitsWhatIsNotAFact] pins the other half.
}
