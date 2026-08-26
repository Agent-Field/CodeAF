package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

var spendTestNow = time.Date(2026, time.August, 25, 12, 0, 0, 0, time.Local)

func spendFixture() []session.UsageLine {
	line := func(day int, model, role string, calls, in, out int, usd float64, task, standing, workspace string) session.UsageLine {
		return session.UsageLine{At: time.Date(2026, time.August, day, 12, 0, 0, 0, time.Local), Model: model, Role: role,
			Calls: calls, Input: in, Output: out, USD: usd, Session: "talk-1", Task: task, Standing: standing, Workspace: workspace}
	}
	return []session.UsageLine{
		line(20, "opus 4.1", "execution", 312, 12_000_000, 6_100_000, 21.40, "the-filings-sweep", "", "/work/bounty-companies"),
		line(22, "sonnet 4.5", "", 1904, 14_000_000, 5_700_000, 9.12, "render-fight-clips", "", "/work/thor-clips"),
		line(24, "gemini 2.5 pro", "verification", 88, 2_000_000, 900_000, 3.31, "", "repo-watch", "/work/aforge"),
		line(25, "haiku 4.5", "naming", 6, 400_000, 100_000, 0.27, "", "", "/work/aforge"),
		line(25, "silent", "", 1, 5, 5, 0, "zero-cost", "", "/work/aforge"),
	}
}

func spendTestReading() spendReading {
	return readSpend(spendFixture(), session.LastDays(spendTestNow, 14), spendTestNow)
}

// THE HEAD ROW IS THE FIGURES AND THE CONTROL, and the span is between the
// arrows — SCREEN 3d's "the label between the arrows is the control and the
// reading at once", drawn by the head row standing and tasks share.
func TestTheSpendPageCarriesTheWindowFiguresInItsHeader(t *testing.T) {
	got := plain(spendTestReading().rows(120, newPalette(tokens.NoColor, false))[0])
	for _, want := range []string{"$34.10", "41.2M tokens", "shift+← aug 12 – aug 25 →", "shift+↑ coarser"} {
		if !strings.Contains(got, want) {
			t.Fatalf("header %q does not carry %q", got, want)
		}
	}
	// AND THE SPAN IS SPELLED ONCE. It used to lead the left field as well, so
	// the label a person moves and the label they read were two runs of one line.
	if n := strings.Count(got, "aug 12"); n != 1 {
		t.Fatalf("the window's span is spelled %d times, want once: %q", n, got)
	}
}

func TestTheSpendSparklineKeepsEveryDayInTheWindow(t *testing.T) {
	r := spendTestReading()
	if got, want := ansi.StringWidth(r.sparkline()), r.window.Buckets(); got != want {
		t.Fatalf("sparkline has %d cells, want the window's %d days: %q", got, want, r.sparkline())
	}
}

func TestReadSpendKeepsARowWhoseWrittenDayIsInsideTheWindow(t *testing.T) {
	win := session.UsageWindow{From: spendTestNow, To: spendTestNow, Grain: session.GrainDay}
	// The writer booked this at 23:30 on august 25, but the reader's shifted
	// clock puts its timestamp just outside the one-day window on august 26.
	line := session.UsageLine{At: time.Date(2026, time.August, 26, 0, 30, 0, 0, time.Local), Day: "2026-08-25",
		Model: "opus 4.1", Calls: 1, Input: 100, Output: 20, USD: 7, Task: "writer-day"}
	r := readSpend([]session.UsageLine{line}, win, spendTestNow)
	if r.totals.USD != 7 || len(r.days) != 1 || r.days[0].USD != 7 {
		t.Fatalf("the inside written day was dropped before bucketing: %+v", r)
	}
	if r.loudFor.ID != "writer-day" {
		t.Fatalf("the loudest written day lost its subject: %+v", r.loudFor)
	}
}

func TestTheSpendPageSaysWhichDayWasLoudest(t *testing.T) {
	text := strings.Join(plainSpendRows(spendTestReading().rows(120, newPalette(tokens.NoColor, false))), "\n")
	if !strings.Contains(text, "aug 20 was the loudest day — $21.40, the-filings-sweep") || !strings.Contains(text, "tasks") {
		t.Fatalf("the loudest day and its door are missing:\n%s", text)
	}
}

func TestTheSpendModelsRunDearestFirstAndTheirBarsStayBounded(t *testing.T) {
	r := spendTestReading()
	if r.models[0].Model != "opus 4.1" || r.models[1].Model != "sonnet 4.5" {
		t.Fatalf("models are not dearest first: %#v", r.models)
	}
	for _, model := range r.models {
		if got := ansi.StringWidth(spendBar(model.USD/r.models[0].USD, spendModelBarCap)); got > spendModelBarCap {
			t.Fatalf("%s grew a %d-cell bar past the %d-cell cap", model.Model, got, spendModelBarCap)
		}
	}
}

// SCREEN 2c: THE ROLE COLUMN IS THE CREW BINDING AND NEVER THE CALL'S OWN WORD.
//
// The fixture's opus lines named themselves `execution`, which is also a slot
// word — so the crew below binds opus to `conversation` instead. A row that drew
// the ledger's word would say `execution` here, and the assertion is that it
// does not: the column is what this machine has that model bound to, which is
// the fact a person can go and change.
func TestTheSpendModelsWearTheRoleTheyAreBoundTo(t *testing.T) {
	crew := spendCrew{
		role: map[string]string{
			"opus 4.1":   "conversation",
			"haiku 4.5":  "naming",
			"sonnet 4.5": "execution",
		},
	}
	if slot, ok := config.ModelSlotFor("plan"); ok {
		crew.unbound = append(crew.unbound, slot)
	}
	text := strings.Join(plainSpendRows(spendTestReading().crewed(crew).rows(120, newPalette(tokens.NoColor, false))), "\n")
	if !strings.Contains(text, "what ran it · by the model, and the role it was bound to") {
		t.Fatalf("the caption does not say what the column is:\n%s", text)
	}
	if !strings.Contains(text, "opus 4.1 · conversation") {
		t.Fatalf("opus does not wear the slot it is bound to:\n%s", text)
	}
	if strings.Contains(text, "opus 4.1 · execution") {
		t.Fatalf("opus wears the word its calls named themselves:\n%s", text)
	}
	// AND A MODEL IS DRAWN BY THE WORD A PERSON SAYS, not by the provider's slug:
	// the vendor prefix, the alias marker and the release stamp come off, exactly
	// as /model and the crew chips spell the same model.
	if got := (spendReading{}).modelName("anthropic/claude-opus-4-1-20260114"); got != "claude-opus-4-1" {
		t.Fatalf("a provider slug is drawn as %q", got)
	}
	if got := (spendReading{}).modelName("~deepseek/deepseek-v4-flash-latest"); got != "deepseek-v4-flash" {
		t.Fatalf("an aliased slug is drawn as %q", got)
	}
	// AND A MODEL BOUND TO NOTHING WEARS NO ROLE WORD AT ALL.
	if !strings.Contains(text, "gemini 2.5 pro █") {
		t.Fatalf("an unbound model grew a role word:\n%s", text)
	}
	// AND THE SLOT NOTHING IS BOUND TO IS A ROW OF ITS OWN, with no figure.
	if !strings.Contains(text, "planning · unbound · follows execution") {
		t.Fatalf("the unbound slot has no row:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "unbound") && strings.Contains(line, "$") {
			t.Fatalf("the unbound row carries a figure nobody measured: %q", line)
		}
	}
}

func TestTheSpendPageShowsThreePurposesThenFoldsTheRest(t *testing.T) {
	text := strings.Join(plainSpendRows(spendTestReading().rows(120, newPalette(tokens.NoColor, false))), "\n")
	for _, want := range []string{"what it was for", "the-filings-sweep", "bounty-companies", "repo-watch", tokens.GlyphCollapsed + " 1 more"} {
		if !strings.Contains(text, want) {
			t.Fatalf("purpose rows do not carry %q:\n%s", want, text)
		}
	}
}

func TestTheSpendPageDrawsNoFiguresItDoesNotKnow(t *testing.T) {
	win := session.LastDays(spendTestNow, 14)
	if rows := readSpend(nil, win, spendTestNow).rows(120, newPalette(tokens.NoColor, false)); len(rows) != 0 {
		t.Fatalf("an empty reading drew %#v", rows)
	}
	zero := session.UsageLine{At: spendTestNow, Calls: 1, Input: 10, USD: 0}
	if rows := readSpend([]session.UsageLine{zero}, win, spendTestNow).rows(120, newPalette(tokens.NoColor, false)); len(rows) != 0 {
		t.Fatalf("an unpriced line drew %#v", rows)
	}
}

func TestTheSpendPageLeavesUnknownModelAndRoleBlank(t *testing.T) {
	line := session.UsageLine{At: spendTestNow, Calls: 3, Input: 10, USD: 1.25}
	text := strings.Join(plainSpendRows(readSpend([]session.UsageLine{line}, session.LastDays(spendTestNow, 1), spendTestNow).rows(120, newPalette(tokens.NoColor, false))), "\n")
	for _, invented := range []string{"unnamed model", "conversation"} {
		if strings.Contains(text, invented) {
			t.Fatalf("unknown ledger metadata became %q:\n%s", invented, text)
		}
	}
}

func TestEverySpendRowFitsThePlaceAtEveryPromisedWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, row := range spendTestReading().rows(width, newPalette(tokens.ANSI256, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("a spend row is %d cells at width %d: %q", got, width, plain(row))
			}
		}
	}
}

func TestTheSpendWindowMovesOnlyOnItsFourDrawnKeys(t *testing.T) {
	r := spendTestReading()
	win := r.window
	tests := map[string]session.UsageWindow{
		"shift+left": win.Step(-1), "shift+right": win.Step(1),
		"shift+up": win.Coarser(), "shift+down": win.Finer(), "x": win,
	}
	for key, want := range tests {
		if got := r.step(win, key); got != want {
			t.Errorf("%s moved to %#v, want %#v", key, got, want)
		}
	}
}

func plainSpendRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(row)
	}
	return out
}

// ── the place, as a person meets it ─────────────────────────────────────────

// spendLab is an app standing in the spend place over a ledger this test wrote.
func spendLab(t *testing.T, lines []session.UsageLine) *app {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	var file strings.Builder
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		file.Write(raw)
		file.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	a := placeApp(t)
	a.usageLedger = path
	a.showPage(pageSpend)
	return a
}

// THE LEDGER IS THE PAGE. Walking in reads it once; the three blocks screen 2c
// asks for are all drawn from that one reading.
func TestTheSpendPlaceDrawsTheLedgerItWalkedInOn(t *testing.T) {
	a := spendLab(t, spendFixture())
	text := placeFrameText(a)
	for _, want := range []string{"$34.10", "what ran it", "opus 4.1", "what it was for"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the spend place does not carry %q:\n%s", want, text)
		}
	}
	// AND A MACHINE THAT HAS SPENT NOTHING MEETS THE TEACHING INSTEAD, which is
	// the router's own three sentences and not a second set of words.
	empty := spendLab(t, nil)
	if got := placeFrameText(empty); !strings.Contains(got, "Every model call writes a line") {
		t.Fatalf("an empty ledger did not teach:\n%s", got)
	}
}

// MOVING THE WINDOW IS ARITHMETIC AND NEVER A READ. The lines are already in
// memory, which is what lets somebody hold the arrow down.
func TestTheSpendWindowMovesWithoutReadingTheLedgerAgain(t *testing.T) {
	a := spendLab(t, spendFixture())
	was := a.spend.win
	held := len(a.spend.lines)
	drive(t, a, key("shift+left"))
	if a.spend.win == was {
		t.Fatal("shift+← did not move the window")
	}
	if len(a.spend.lines) != held {
		t.Fatalf("moving the window re-read the ledger: %d lines, was %d", len(a.spend.lines), held)
	}
	drive(t, a, key("shift+right"))
	if a.spend.win != was {
		t.Fatalf("shift+→ did not come back to %v", was)
	}
	drive(t, a, key("shift+up"))
	if a.spend.win.Grain != session.GrainWeek {
		t.Fatalf("shift+↑ left the grain at %q", a.spend.win.Grain)
	}
}

// THE LEDGER HOLDS IDS AND NO TITLES, so the page joins them against the
// records it is already reading and a subject nobody can name keeps its id.
func TestTheSpendPlaceNamesWhatTheLedgerOnlyHasAnIdFor(t *testing.T) {
	win := session.LastDays(spendTestNow, 14)
	r := readSpend(spendFixture(), win, spendTestNow).naming(map[string]string{
		session.SubjectTask + "\x00" + "the-filings-sweep": "read 40 filings for reward mentions",
	})
	text := strings.Join(plainSpendRows(r.rows(120, newPalette(tokens.NoColor, false))), "\n")
	if !strings.Contains(text, "read 40 filings for reward mentions") {
		t.Fatalf("the joined title is not on the page:\n%s", text)
	}
	if !strings.Contains(text, "render-fight-clips") {
		t.Fatalf("a subject nobody could name lost its id:\n%s", text)
	}
}

// `what it was for` IS PRESENT WHENEVER THE LEDGER NAMES ANY SUBJECT AT ALL,
// which on this machine is every line: the engine's one door onto the ledger
// stamps the conversation on every record it writes
// ([Agent.recordUsageLine]), so a line with no task and no promise is still a
// line that went on SOMETHING.
//
// AND THE TITLE COMES OFF THE WORLD THIS SURFACE IS ALREADY HOLDING. The ledger
// has the sixteen hex and nothing else; the row is headed with what the person
// calls that conversation.
func TestWhatItWasForIsDrawnForAConversationTheLedgerOnlyHasAnIdFor(t *testing.T) {
	a := placeApp(t)
	id := ""
	for _, project := range a.home.world.Projects {
		for _, row := range project.Sessions {
			if strings.EqualFold(strings.TrimSpace(row.Title), "porting the picker") {
				id = row.ID
			}
		}
	}
	if id == "" {
		t.Fatal("the lab has no conversation to spend money in")
	}
	line := session.UsageLine{At: spendTestNow, Model: "opus 4.1", Calls: 2, Input: 100, Output: 20,
		USD: 4.25, Session: id}
	b := spendLab(t, []session.UsageLine{line})
	b.home.world = a.home.world
	b.rebuildSpend()
	text := placeFrameText(b)
	if !strings.Contains(text, "what it was for") {
		t.Fatalf("a ledger that names a conversation drew no `what it was for`:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "porting the picker") {
		t.Fatalf("the conversation kept its id where the world knows its title:\n%s", text)
	}
}

// AN EMPTY WINDOW IS NOT AN EMPTY MACHINE, and only one of the two is taught at.
//
// Paging back a fortnight on a machine that HAS spent money drew the three
// sentences saying what the spend place is for — and took the header with them,
// which is the only thing on that frame naming the window the four arrow keys
// move. `shift+←` looked like the page had been wiped with no way back on
// screen. It is the same defect the tasks place had, told apart the same way
// ([spendPage.held]).
func TestAnEmptySpendWindowKeepsTheControlThatPagesItBack(t *testing.T) {
	a := spendLab(t, spendFixture())
	if !strings.Contains(placeFrameText(a), "what ran it") {
		t.Fatalf("the lab did not open on the ledger:\n%s", placeFrameText(a))
	}
	// A fortnight back, where this fixture spent nothing.
	drive(t, a, key("shift+left"))
	text := placeFrameText(a)
	if strings.Contains(text, "There is nothing to set here") {
		t.Fatalf("an empty window drew the empty machine's lesson:\n%s", text)
	}
	if !strings.Contains(text, spendNothingWord) {
		t.Fatalf("an empty window does not say so in words:\n%s", text)
	}
	if !strings.Contains(text, "shift+←") {
		t.Fatalf("an empty window lost the control that pages it back:\n%s", text)
	}
	if strings.Contains(text, "$0.00") {
		t.Fatalf("an empty window drew the figure the emptiness law forbids:\n%s", text)
	}
	// AND A MACHINE THAT HAS SPENT NOTHING IS STILL TAUGHT AT.
	b := spendLab(t, nil)
	if !strings.Contains(placeFrameText(b), "There is nothing to set here") {
		t.Fatalf("an empty machine was not taught:\n%s", placeFrameText(b))
	}
}

// `enter` ON A ROW OF "WHAT IT WAS FOR" OPENS WHAT IT WAS FOR — the money is
// the reading and the thing it went on is the door.
func TestEnterOnASpendRowOpensTheThingTheMoneyWentOn(t *testing.T) {
	talk := session.UsageLine{At: spendTestNow, Model: "opus 4.1", Calls: 2, Input: 100, Output: 20,
		USD: 4.25, Session: "aaaa000000000001", Workspace: "/work/alpha"}
	a := spendLab(t, []session.UsageLine{talk})
	stop := a.spendStopAt(a.spend.cursor)
	if !stop.ok {
		t.Fatalf("the cursor did not open on a door: %d of %d", a.spend.cursor, len(a.spend.stops))
	}
	if stop.subject.Kind != session.SubjectConversation {
		t.Fatalf("the only door is a %q row", stop.subject.Kind)
	}
	drive(t, a, key("enter"))
	if a.page != pageHome {
		t.Fatalf("enter on a conversation's row landed on %q", a.page.word())
	}
}

// AND A ROW THAT IS NOT A DOOR IS NOT ONE. The window header, the sparkline and
// the section headings are the reading; nothing stops on them, so `enter` there
// is the composer's own road.
func TestTheSpendCursorStopsOnlyOnRowsThatNameSomething(t *testing.T) {
	a := spendLab(t, spendFixture())
	seen := 0
	for i, stop := range a.spend.stops {
		if !stop.ok {
			continue
		}
		seen++
		if i == 0 {
			t.Fatal("the window header was offered as a door")
		}
	}
	if seen != spendSubjectCap {
		t.Fatalf("%d doors were drawn, want the %d shown subjects", seen, spendSubjectCap)
	}
}

// THE WINDOW IS ARITHMETIC OVER WHAT IS ALREADY HELD, AND THE STORE IS READ ON
// THE BEAT AND NOWHERE ELSE.
//
// place_spend.go promises this in as many words — "the lines are already in
// memory, so moving the window is arithmetic and never a read … which is what
// lets a person hold the arrow down" — and for one wave it was false: the
// rebuild every window keystroke ends with joined ids against titles, and the
// standing half of that join asked the seam once per project, each ask being a
// walk of the standing root and a parse of every document under it. Held down,
// that is a directory walk per repeat.
//
// The count here is what makes the promise checkable: one read on the way in,
// none for any number of arrows, and one more when the three-second beat says
// the world may have moved.
func TestTheSpendWindowMovesWithoutTouchingTheStandingStore(t *testing.T) {
	a := spendLab(t, spendFixture())
	reads := 0
	a.stands.All = func() []standing.Item {
		reads++
		return []standing.Item{standOrder("watch-1", "watch the filings", standing.AltitudeMachine)}
	}
	a.stands.Items = func(string) []standing.Item {
		t.Fatal("the spend place asked for one project's orders, which is the seam it walked N+1 times")
		return nil
	}
	// The open is where the join is made, and it is made once.
	a.showPage(pageSpend)
	if reads != 1 {
		t.Fatalf("walking in read the standing store %d times, want one", reads)
	}
	for i := 0; i < 20; i++ {
		drive(t, a, key("shift+left"))
		drive(t, a, key("shift+right"))
	}
	if reads != 1 {
		t.Fatalf("forty window keystrokes read the standing store %d times, want the one from the open", reads)
	}
	// AND THE BEAT IS WHERE IT IS ALLOWED TO COST SOMETHING. A page that never
	// re-read would name a promise made in another window by its id forever.
	a.placeBeat(a.placeGen)
	if reads != 2 {
		t.Fatalf("the beat left the standing store read %d times, want a second read", reads)
	}
}
