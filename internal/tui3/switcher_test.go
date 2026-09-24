package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

type switcherLab struct {
	world session.World
	items map[string][]StandingItemView
	fired []StandingItemView
	seen  time.Time
	now   time.Time
	here  string
	gone  switcherGone
}

func newSwitcherLab() switcherLab {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	seen := now.Add(-3 * time.Hour)
	question := session.PresenceQuestion{
		Kind: session.QuestionTask, ID: 7, Text: "add a --report-only mode?\nwith detail",
		Asked: now.Add(-6 * time.Hour), Options: []session.AnswerOption{{Key: "1", Label: "do it"}, {Key: "2", Label: "leave it"}},
	}
	asking := session.SessionRow{ID: "ask", Dir: "/state/alpha/ask", Project: "alpha", ProjectDir: "/work/alpha", Workspace: "/work/alpha", Title: "Asking chat", At: now.Add(-7 * time.Hour), Live: true,
		Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: question.Text, Question: question}}
	running := session.SessionRow{ID: "run", Dir: "/state/beta/run", Project: "beta", ProjectDir: "/work/beta", Title: "Running chat", At: now.Add(-2 * time.Hour), Live: true,
		Presence: session.SessionPresence{State: session.PresenceWorking, RunningTasks: []session.PresenceTask{{ID: "1", StartedAt: now.Add(-2 * time.Hour)}}},
		Tasks:    session.TaskRollup{Running: 2, Rows: []session.TaskIndexEntry{{ID: "1", Status: string(session.TaskRunning), Activity: "reading filings"}}}}
	landed := session.SessionRow{ID: "landed", Dir: "/state/alpha/landed", Project: "alpha", Title: "Landed chat", At: now.Add(-8 * time.Hour), Open: true,
		Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{{ID: "2", Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour), FilesChanged: 3}}}}
	saved := session.SessionRow{ID: "saved", Dir: "/state/beta/saved", Project: "beta", Title: "Saved run", At: now.Add(-24 * time.Hour),
		Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{{ID: "3", Status: string(session.TaskDone), EndedAt: now.Add(-2 * time.Hour), Kind: session.TaskKindSubharness}}}}
	alpha := session.Project{Bucket: "alpha", Dir: "/state/alpha", Path: "/work/alpha", Name: "alpha", Sessions: []session.SessionRow{asking, landed}}
	beta := session.Project{Bucket: "beta", Dir: "/state/beta", Path: "/work/beta", Name: "beta", Sessions: []session.SessionRow{running, saved}}
	for i := 0; i < 10; i++ {
		row := session.SessionRow{ID: fmt.Sprintf("quiet-%02d", i), Dir: fmt.Sprintf("/state/beta/q%d", i), Project: "beta", Title: fmt.Sprintf("Quiet %02d", i), At: now.Add(-time.Duration(48+i*24) * time.Hour)}
		beta.Sessions = append(beta.Sessions, row)
	}
	beta.Sessions = append(beta.Sessions, session.SessionRow{ID: "gone", Title: "Archived chat", Archived: true, At: now})
	standingAsk := standing.Item{ID: "stand-ask", Words: "Standing question", NeedsPerson: "send the digest?", Updated: now.Add(-8 * time.Hour)}
	fired := standing.Item{ID: "stand-fired", Words: "Morning watch", LastFired: now.Add(-time.Hour), LastChecked: now.Add(-time.Hour), LastCheckLine: "nothing had changed", LastOutcome: standing.OutcomeNothing}
	return switcherLab{
		world: session.World{Projects: []session.Project{beta, alpha}, Read: now}, now: now, seen: seen, here: asking.Dir,
		items: map[string][]StandingItemView{alpha.Dir: {{Item: standingAsk}}, beta.Dir: {{Item: fired}}},
	}
}

func (l switcherLab) read(ledger switcherLedgerInput) switcherReading {
	return readSwitcher(l.world, l.items, l.fired, switcherHere{session: l.here}, l.gone, l.seen, l.now, ledger)
}

// switcherStops is every row the reading hands the grid: the `since you left`
// lines, then the ranked conversations and standing things.
func switcherStops(r switcherReading) []switcherRow {
	return append(append([]switcherRow(nil), r.ledger...), r.rows...)
}

// switcherWords is what a row carries into a panel cell — its title, its note
// and its margin word — so a claim about the words is one string search.
func switcherWords(rows []switcherRow) string {
	var out []string
	for _, row := range rows {
		out = append(out, row.title, row.note, switcherMarginWord(row))
	}
	return strings.Join(out, "\n")
}

// switcherRowByID is one conversation's row, by its session id.
func switcherRowByID(t *testing.T, r switcherReading, id string) switcherRow {
	t.Helper()
	for _, row := range r.rows {
		if row.kind == switcherConversation && row.session.ID == id {
			return row
		}
	}
	t.Fatalf("no row for %q in the reading", id)
	return switcherRow{}
}

// WHAT WANTS THE PERSON FIRST: a question (a watch's before a chat's, the
// longer wait first), then what is moving, then the rest — each row carrying the
// note its panel draws, and nothing archived.
func TestTheSwitcherRanksEveryKindOfThingByWhatWantsThePerson(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(switcherLedgerInput{})
	want := []string{"Standing question", "Asking Chat", "Running Chat"}
	for i, title := range want {
		if r.rows[i].title != title {
			t.Fatalf("row %d is %q, want %q", i, r.rows[i].title, title)
		}
	}
	words := switcherWords(r.rows)
	for _, word := range []string{"asks: add a --report-only mode?", "2 tasks running · reading filings", "3 files made", "ran a saved shape", homeHereWord} {
		if !strings.Contains(words, word) {
			t.Fatalf("the reading lost %q:\n%s", word, words)
		}
	}
	if strings.Contains(words, "Archived chat") {
		t.Fatalf("an archived chat entered the reading:\n%s", words)
	}
}

// A WATCH STOPPED FOR PERMISSION IS SAID, NOT ASKED. The line a refusal leaves
// on an item is already a whole sentence about it, and the ask word used to put
// `asks: stopped` on a row where nobody asked anything. Both spellings of a
// refusal go through standing.IsPermissionLine, the one predicate for which of
// the two things NeedsPerson carries a line is.
func TestAWatchStoppedForPermissionIsSaidNotAsked(t *testing.T) {
	for _, line := range []string{
		standing.NeedsPermissionLead + "bash",
		"needs approval but no resolver is attached: default",
	} {
		view := StandingItemView{Item: standing.Item{
			ID: "w-stop", Words: "watch the lockfile", Status: standing.StatusActive,
			NeedsPerson: line,
		}}
		if got := switcherStandingNote(view); got != line {
			t.Fatalf("the row puts the ask word over a line nobody asked: got %q, want the line bare %q", got, line)
		}
	}
}

// FILES ARE NEWS ONLY AFTER THEIR ROW LANDS. A run started after the last look
// but still carrying no ending is not counted as change since that look; the
// same row becomes news once its closing row dates the landing.
func TestARunningRunIsNotNewsSinceTheLastLook(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-2 * time.Hour)
	noteFor := func(entry session.TaskIndexEntry) string {
		row := session.SessionRow{
			ID: "room-a", Title: "Pricing audit", At: seen.Add(time.Hour),
			Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}},
		}
		world := session.World{Projects: []session.Project{{Name: "pricing", Dir: "/pricing", Sessions: []session.SessionRow{row}}}}
		reading := readSwitcher(world, nil, nil, switcherHere{}, nil, seen, now, switcherLedgerInput{})
		for _, stop := range switcherStops(reading) {
			if stop.kind == switcherConversation {
				return stop.note
			}
		}
		t.Fatal("the conversation is missing from the switcher")
		return ""
	}
	running := session.TaskIndexEntry{
		ID: "1", Title: "Audit the pricing code", Kind: session.TaskKindAdaptive,
		Status: string(session.TaskRunning), FilesChanged: 7,
	}
	if note := noteFor(running); note != "" {
		t.Fatalf("the still-running run is news since the last look: %q", note)
	}
	running.Status = string(session.TaskDone)
	running.EndedAt = now.Add(-time.Hour)
	if note := noteFor(running); note != "7 files made" {
		t.Fatalf("the landed run's news is %q, want its seven files", note)
	}
}

// THE LEDGER'S LINES ARE DOORS: each one names the place that owns it, and
// only what was recorded — a watch's own last-look sentence, what memory said,
// the work that landed — is a line at all.
func TestTheSwitcherSinceYouLeftLedgerDrawsOnlyRecordedDoors(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(switcherLedgerInput{learned: 2, letGo: 1})
	words := switcherWords(r.ledger)
	for _, word := range []string{"nothing had changed", "learned 2 things, let go of 1", "Landed Chat", "Saved Run"} {
		if !strings.Contains(words, word) {
			t.Fatalf("ledger lost %q:\n%s", word, words)
		}
	}
	doors := map[string]bool{}
	for _, row := range r.ledger {
		doors[row.place] = true
	}
	for _, door := range []string{"standing", "memory", "sessions"} {
		if !doors[door] {
			t.Fatalf("%s was not a ledger door", door)
		}
	}
}

// A QUIET MORNING CLAIMS NOTHING: no row needs anybody or is moving, and a look
// stamp of just now leaves nothing to say since it.
func TestTheSwitcherOnAQuietMorningClaimsNothing(t *testing.T) {
	lab := newSwitcherLab()
	for pi := range lab.world.Projects {
		for ri := range lab.world.Projects[pi].Sessions {
			row := &lab.world.Projects[pi].Sessions[ri]
			row.Live = false
			row.Presence = session.SessionPresence{}
			row.Tasks.Running = 0
		}
	}
	lab.items = nil
	lab.seen = lab.now
	r := lab.read(switcherLedgerInput{})
	for _, row := range r.rows {
		if row.needs || row.moving {
			t.Fatalf("a quiet morning ranked %q as wanting somebody", row.title)
		}
	}
	if len(r.ledger) != 0 {
		t.Fatalf("a quiet morning had news: %+v", r.ledger)
	}
}

// A ROW'S VERBS ARE THE DOORS IT DESCRIBES: a question's own answers on their
// own keys, a conversation's folder doors, a watch's pause.
func TestSwitcherStopsAndVerbsCarryTheDoorTheyDescribe(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(switcherLedgerInput{})
	var standingRow switcherRow
	for _, row := range r.rows {
		if row.kind == switcherStanding {
			standingRow = row
		}
	}
	// The question's answers wear THEIR OWN KEYS. This read `y do it · n leave
	// it` until the questions wave, which was the strip putting yes and no on
	// whichever two answers came first — and on the consent lane, whose answers
	// are `1 allow once · 2 always · 3 deny`, that put a widening approval
	// under the key a person presses for yes (switcher.go).
	if got := wordsOfSwitcherVerbs(switcherVerbsFor(switcherRowByID(t, r, "ask"))); !strings.Contains(got, "1 do it") || !strings.Contains(got, "2 leave it") || !strings.Contains(got, "x close") || !strings.Contains(got, "c copy name") {
		t.Fatalf("asking verbs are %q", got)
	}
	if got := wordsOfSwitcherVerbs(switcherVerbsFor(standingRow)); !strings.Contains(got, "p "+homeItemPauseWord) {
		t.Fatalf("standing verbs are %q", got)
	}
}

func TestSwitcherVerbsRequireTheStateAndAddressTheyActOn(t *testing.T) {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	bare := session.SessionRow{ID: "bare", Title: "Bare", Presence: session.SessionPresence{Question: session.PresenceQuestion{Options: []session.AnswerOption{{Label: "yes"}, {Label: "no"}}}}}
	paused := standing.Item{ID: "paused", Words: "Paused", Status: standing.StatusPaused, NeedsPerson: "old words without a pending question"}
	world := session.World{Projects: []session.Project{{Name: "p", Sessions: []session.SessionRow{bare}}}}
	r := readSwitcher(world, map[string][]StandingItemView{"": {{Item: paused}}}, nil, switcherHere{}, nil, time.Time{}, now, switcherLedgerInput{})
	for _, row := range r.rows {
		got := wordsOfSwitcherVerbs(switcherVerbsFor(row))
		switch row.kind {
		case switcherConversation:
			for _, absent := range []string{"yes", "no", "new in project", "open folder", "copy project"} {
				if strings.Contains(got, absent) {
					t.Fatalf("addressless conversation offered %q in %q", absent, got)
				}
			}
		case switcherStanding:
			if !strings.Contains(got, switcherResumeWord) || strings.Contains(got, "p "+homeItemPauseWord) || strings.Contains(got, "y ") || strings.Contains(got, "n ") {
				t.Fatalf("paused standing verbs are %q", got)
			}
		}
	}
}

func wordsOfSwitcherVerbs(verbs []switcherVerb) string {
	var out []string
	for _, verb := range verbs {
		out = append(out, string(verb.key)+" "+verb.word)
	}
	return strings.Join(out, " · ")
}

// TWO EMPTY ADDRESSES ARE NOT `here`: a window standing nowhere and a row that
// recorded nowhere do not match each other.
func TestSwitcherNeverCallsAnEmptyAddressHere(t *testing.T) {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	world := session.World{Projects: []session.Project{{Name: "p", Sessions: []session.SessionRow{{Title: "Missing address"}}}}}
	r := readSwitcher(world, nil, nil, switcherHere{}, nil, time.Time{}, now, switcherLedgerInput{})
	for _, row := range r.rows {
		if row.here {
			t.Fatal("two empty addresses became here")
		}
	}
}

// A CONSENT ROW SAYS THE GATE'S OWN SENTENCE AND NOT A WORD MORE.
//
// THE SENTENCE IS THE ENGINE'S AND IT IS FED IN HERE VERBATIM. internal/session's
// consent.go writes `needs your ok to run ` plus the tool's name, once, as "the
// one line another window may answer this from" — a whole predicate about the
// conversation whose name this row draws beside it. This test used to feed a
// sentence nobody writes (`send the report on your behalf?`), which is a bare
// action, and the row prefixed `wants to ` onto it to make a clause of it. Both
// halves were green and what the tmux suite read on a real screen was
//
//	consentws wants to needs your ok to run bash
//
// So the fixture is the engine's line now, and the assertion is that the row is
// exactly it. A prefix put back on this side fails here.
func TestTheSwitcherRepeatsAConsentQuestionInItsOwnWords(t *testing.T) {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	// The sentence internal/session/consent.go hands the presence file, spelled
	// as that lane spells it.
	said := "needs your ok to run bash"
	question := session.PresenceQuestion{Kind: session.QuestionConsent, ID: 9, Text: said, Asked: now.Add(-time.Hour), Options: []session.AnswerOption{{Key: "1", Label: "allow once"}, {Key: "3", Label: "not this time"}}}
	row := session.SessionRow{ID: "consent", Title: "Consent", At: now.Add(-time.Hour), Live: true, Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: question.Text, Question: question}}
	world := session.World{Projects: []session.Project{{Dir: "/p", Name: "p", Sessions: []session.SessionRow{row}}}}
	reading := readSwitcher(world, nil, nil, switcherHere{}, nil, time.Time{}, now, switcherLedgerInput{})
	// AND NOTHING WAS PUT IN FRONT OF IT. Any word this row adds shows up as
	// something standing between the conversation's name and the gate's sentence.
	got := switcherRowByID(t, reading, "consent")
	if got.note != said {
		t.Fatalf("the row wrote its own grammar around the gate's sentence: %q, want %q", got.note, said)
	}
	if verbs := switcherVerbsFor(got); len(verbs) < 2 || verbs[0].word != "allow once" || verbs[1].word != "not this time" {
		t.Fatalf("the consent row lost the question's own option words: %+v", verbs)
	}
}

// ZERO IS NOTHING: a conversation with no work, no time and no look stamp
// carries no note, no age and no ledger line.
func TestTheSwitcherKeepsUnknownAndZeroFactsEmpty(t *testing.T) {
	world := session.World{Projects: []session.Project{{Dir: "/p", Name: "p", Sessions: []session.SessionRow{{ID: "empty", Title: "Empty"}}}}}
	r := readSwitcher(world, nil, nil, switcherHere{}, nil, time.Time{}, time.Time{}, switcherLedgerInput{})
	row := switcherRowByID(t, r, "empty")
	if row.note != "" || row.age != "" || len(r.ledger) != 0 {
		t.Fatalf("zero became a fact: note %q, age %q, ledger %+v", row.note, row.age, r.ledger)
	}
}
