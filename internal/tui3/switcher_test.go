package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

type switcherLab struct {
	world  session.World
	items  map[string][]StandingItemView
	seen   time.Time
	now    time.Time
	bucket string
}

func newSwitcherLab() switcherLab {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	seen := now.Add(-3 * time.Hour)
	question := session.PresenceQuestion{
		Kind: session.QuestionTask, ID: 7, Text: "add a --report-only mode?\nwith detail",
		Asked: now.Add(-6 * time.Hour), Options: []session.AnswerOption{{Key: "1", Label: "do it"}, {Key: "2", Label: "leave it"}},
	}
	asking := session.SessionRow{ID: "ask", Dir: "/state/alpha/ask", Project: "alpha", ProjectDir: "/work/alpha", Title: "Asking chat", At: now.Add(-7 * time.Hour), Live: true,
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
		world: session.World{Projects: []session.Project{beta, alpha}, Read: now}, now: now, seen: seen, bucket: alpha.Dir,
		items: map[string][]StandingItemView{alpha.Dir: {{Item: standingAsk}}, beta.Dir: {{Item: fired}}},
	}
}

func (l switcherLab) read(grouped, hideQuiet bool, ledger switcherLedgerInput) switcherReading {
	return readSwitcher(l.world, l.items, l.bucket, l.seen, l.now, grouped, hideQuiet, ledger)
}

func switcherText(r switcherReading, width int) string {
	return ansi.Strip(strings.Join(r.rows(width, newPalette(tokens.NoColor, false)), "\n"))
}

func switcherStops(r switcherReading) []switcherRow {
	var rows []switcherRow
	for i := range r.lines {
		if row, ok := r.at(i); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestTheSwitcherRanksEveryKindOfThingByWhatWantsThePerson(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(false, false, switcherLedgerInput{})
	var stops []switcherRow
	for _, row := range switcherStops(r) {
		if row.kind == switcherConversation || row.kind == switcherStanding {
			stops = append(stops, row)
		}
	}
	want := []string{"Standing question", "Asking Chat", "Running Chat"}
	for i, title := range want {
		if stops[i].title != title {
			t.Fatalf("stop %d is %q, want %q", i, stops[i].title, title)
		}
	}
	text := switcherText(r, 120)
	for _, word := range []string{"? Standing question", "? Asking Chat", "◐ Running Chat", "asks: add a --report-only mode?", "2 tasks running · reading filings", "3 files made", "ran a saved shape", "here"} {
		if !strings.Contains(text, word) {
			t.Fatalf("switcher lost %q:\n%s", word, text)
		}
	}
	if strings.Contains(text, "Archived chat") {
		t.Fatalf("an archived chat entered the resting list:\n%s", text)
	}
}

func TestTheSwitcherUsesAmberOnlyForRowsThatNeedThePerson(t *testing.T) {
	lab := newSwitcherLab()
	pal := newPalette(tokens.TrueColor, false)
	lines := lab.read(false, false, switcherLedgerInput{}).rows(120, pal)
	warn := pal.warn(tokens.GlyphNeedsHuman)
	accent := pal.accent(tokens.GlyphWorking)
	warns := 0
	for _, line := range lines {
		warns += strings.Count(line, warn)
	}
	if warns != 2 {
		t.Fatalf("amber appeared %d times, want the two needs-you rows", warns)
	}
	if !strings.Contains(strings.Join(lines, "\n"), accent) {
		t.Fatal("the moving row did not use the live ink")
	}
}

func TestTheSwitcherFoldsOnlyTheQuietTailAndCanHideIt(t *testing.T) {
	lab := newSwitcherLab()
	text := switcherText(lab.read(false, false, switcherLedgerInput{}), 120)
	if !strings.Contains(text, "▸ 7 more, quiet since aug 20") {
		t.Fatalf("the quiet fold is wrong:\n%s", text)
	}
	hidden := switcherText(lab.read(false, true, switcherLedgerInput{}), 120)
	if !strings.Contains(hidden, "▸ 12 quiet") || strings.Contains(hidden, "Quiet 00") {
		t.Fatalf("hide-quiet did not become one honest fold:\n%s", hidden)
	}
}

func TestTheGroupedSwitcherPutsThisProjectFirstAndHeadingsAreNotStops(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(true, false, switcherLedgerInput{})
	text := switcherText(r, 120)
	if strings.Index(text, "alpha") > strings.Index(text, "beta") {
		t.Fatalf("this project was not first:\n%s", text)
	}
	for i, line := range r.lines {
		if line.heading != "" {
			if _, ok := r.at(i); ok {
				t.Fatalf("heading %q became a stop", line.heading)
			}
		}
	}
}

func TestTheSwitcherSinceYouLeftLedgerDrawsOnlyRecordedDoors(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(false, false, switcherLedgerInput{learned: 2, letGo: 1})
	text := switcherText(r, 120)
	for _, word := range []string{"since you left · 3h", "nothing had changed", "standing", "learned 2 things, let go of 1", "memory", "2 tasks landed", "tasks"} {
		if !strings.Contains(text, word) {
			t.Fatalf("ledger lost %q:\n%s", word, text)
		}
	}
	doors := map[string]bool{}
	for _, row := range switcherStops(r) {
		if row.kind == switcherLedger {
			doors[row.place] = true
		}
	}
	for _, door := range []string{"standing", "memory", "tasks"} {
		if !doors[door] {
			t.Fatalf("%s was not a ledger door", door)
		}
	}
}

func TestTheSwitcherOnAQuietMorningHasNoSectionClaimLedgerOrAccent(t *testing.T) {
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
	r := lab.read(false, false, switcherLedgerInput{})
	plain := switcherText(r, 80)
	if strings.Contains(plain, "what wants you first") || strings.Contains(plain, "since you left") || strings.Contains(plain, "?") || strings.Contains(plain, "◐") {
		t.Fatalf("quiet morning made a claim:\n%s", plain)
	}
	painted := strings.Join(r.rows(80, newPalette(tokens.TrueColor, false)), "\n")
	if strings.Contains(painted, newPalette(tokens.TrueColor, false).warn(tokens.GlyphNeedsHuman)) {
		t.Fatal("quiet morning spent amber")
	}
}

func TestEverySwitcherRowFitsItsFrameAndNarrowRowsDropFactsInOrder(t *testing.T) {
	lab := newSwitcherLab()
	for _, width := range []int{60, 80, 120, 200} {
		for _, row := range lab.read(false, false, switcherLedgerInput{learned: 2}).rows(width, newPalette(tokens.TrueColor, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("%d-column row used %d cells: %q", width, got, ansi.Strip(row))
			}
		}
	}
	narrow := switcherText(lab.read(false, false, switcherLedgerInput{}), 60)
	if strings.Contains(narrow, "reading filings") {
		t.Fatalf("a narrow row kept its note:\n%s", narrow)
	}
}

func TestSwitcherStopsAndVerbsCarryTheDoorTheyDescribe(t *testing.T) {
	lab := newSwitcherLab()
	r := lab.read(false, false, switcherLedgerInput{})
	var askingAt, standingAt, foldAt = -1, -1, -1
	for i := range r.lines {
		row, ok := r.at(i)
		if !ok {
			continue
		}
		switch {
		case row.session.ID == "ask":
			askingAt = i
		case row.kind == switcherStanding:
			standingAt = i
		case row.fold:
			foldAt = i
		}
	}
	words := func(verbs []switcherVerb) string {
		var out []string
		for _, verb := range verbs {
			out = append(out, string(verb.key)+" "+verb.word)
		}
		return strings.Join(out, " · ")
	}
	if got := words(r.verbs(askingAt)); !strings.Contains(got, "y do it") || !strings.Contains(got, "n leave it") || !strings.Contains(got, "a put it away") || !strings.Contains(got, "c copy path") {
		t.Fatalf("asking verbs are %q", got)
	}
	if got := words(r.verbs(standingAt)); !strings.Contains(got, "p pause it") {
		t.Fatalf("standing verbs are %q", got)
	}
	if row, ok := r.at(foldAt); !ok || !row.fold {
		t.Fatal("the fold was not a door")
	}
}

func TestTheSwitcherRepeatsAConsentQuestionInItsOwnWords(t *testing.T) {
	now := time.Date(2026, time.August, 25, 13, 0, 0, 0, time.UTC)
	question := session.PresenceQuestion{Kind: session.QuestionConsent, ID: 9, Text: "send the report on your behalf?", Asked: now.Add(-time.Hour), Options: []session.AnswerOption{{Key: "1", Label: "let it send"}, {Key: "3", Label: "not this time"}}}
	row := session.SessionRow{ID: "consent", Title: "Consent", At: now.Add(-time.Hour), Live: true, Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: question.Text, Question: question}}
	world := session.World{Projects: []session.Project{{Dir: "/p", Name: "p", Sessions: []session.SessionRow{row}}}}
	reading := readSwitcher(world, nil, "/p", time.Time{}, now, false, false, switcherLedgerInput{})
	if text := switcherText(reading, 120); !strings.Contains(text, "wants to send the report on your behalf") {
		t.Fatalf("consent was respelled:\n%s", text)
	}
	for i := range reading.lines {
		if got := reading.verbs(i); len(got) > 1 && got[0].word == "let it send" && got[1].word == "not this time" {
			return
		}
	}
	t.Fatal("the consent row lost the question's own option words")
}

func TestTheSwitcherKeepsUnknownAndZeroFactsEmpty(t *testing.T) {
	world := session.World{Projects: []session.Project{{Dir: "/p", Name: "p", Sessions: []session.SessionRow{{ID: "empty", Title: "Empty"}}}}}
	text := switcherText(readSwitcher(world, nil, "/p", time.Time{}, time.Time{}, false, false, switcherLedgerInput{}), 80)
	for _, invented := range []string{"0 tasks", "0 files", "Jan 1", "since you left"} {
		if strings.Contains(text, invented) {
			t.Fatalf("zero became %q:\n%s", invented, text)
		}
	}
}
