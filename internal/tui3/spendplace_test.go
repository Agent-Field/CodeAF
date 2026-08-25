package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
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

func TestTheSpendPageCarriesTheWindowFiguresInItsHeader(t *testing.T) {
	got := plain(spendTestReading().rows(120, newPalette(tokens.NoColor, false))[0])
	for _, want := range []string{"aug 12 – aug 25", "$34.10", "41.2M tokens", "shift+←→ window"} {
		if !strings.Contains(got, want) {
			t.Fatalf("header %q does not carry %q", got, want)
		}
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
