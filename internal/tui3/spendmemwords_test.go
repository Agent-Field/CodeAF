package tui3

// THE MONEY PLACES AND THE MEMORY PLACE, HELD TO THE WORDS THEY SAY.
//
// Every test here is one row of docs/design/polish/audit-settings.md or
// audit-help.md: a sentence a person read on the screen and could not act on.
// The failure messages print the line that was drawn and the line that should
// have been, because the defect in each case is a reading and not a value.
//
// EVERY CLOCK IN THIS FILE IS PINNED. The spend page's window is the last
// fourteen days, so a test that let the wall clock choose it would go red on a
// date nobody picked — issue #526, and the reason [spendTestNow] exists.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── the spend place ─────────────────────────────────────────────────────────

// TestTheSpendPageSaysWhichTotalIsWhich is audit-settings row 8. The page
// stacked two dollar figures — `today $0.13 of $500` and `$2.05 · 326.5k tokens`
// — with nothing beside the second saying it was the window's. At 160 columns
// the only label it had was a date span 145 cells away between the arrows, which
// is not a label.
func TestTheSpendPageSaysWhichTotalIsWhich(t *testing.T) {
	r := spendTestReading().todayed(0.13).railed(500)
	for _, width := range []int{160, 120, 80} {
		rows := plainSpendRows(r.rows(width, newPalette(tokens.NoColor, false)))
		if len(rows) < 2 {
			t.Fatalf("at %d cells the page drew %d rows, want the pointer line and the head", width, len(rows))
		}
		day, window := rows[0], rows[1]
		if !strings.Contains(day, "today $0.13") {
			t.Fatalf("at %d cells the pointer line is %q, want it to say the day's figure is the day's", width, day)
		}
		if !strings.Contains(window, "14 days") {
			t.Fatalf("at %d cells the window's total is %q, want it to say what period it is the total of — `14 days came to $34.10`",
				width, window)
		}
		if !strings.Contains(window, "$34.10") {
			t.Fatalf("at %d cells the window's total lost its figure: %q", width, window)
		}
	}
}

// AND THE SPAN IS COUNTED IN THE GRAIN'S OWN NOUN, so a window zoomed out to
// months does not go on calling itself a number of days.
func TestTheSpendHeadNamesTheSpanInTheGrainsOwnWord(t *testing.T) {
	for _, test := range []struct {
		win  session.UsageWindow
		want string
	}{
		{session.LastDays(spendTestNow, 14), "14 days"},
		{session.LastDays(spendTestNow, 1), "1 day"},
		{session.UsageWindow{From: spendTestNow.AddDate(0, 0, -21), To: spendTestNow, Grain: session.GrainWeek}, "4 weeks"},
		{session.UsageWindow{From: spendTestNow.AddDate(0, -5, 0), To: spendTestNow, Grain: session.GrainMonth}, "6 months"},
	} {
		if got := spendSpanWord(test.win); got != test.want {
			t.Errorf("the head calls that window %q, want %q", got, test.want)
		}
	}
}

// AND THE LEAD IS NEVER BOUGHT WITH THE WINDOW KEYS. The head's own width is
// what decides whether the arrows are drawn ([placeWindowFits]), so a lead added
// without reserving the control's cells would have paid for the sentence by
// unbinding shift+←/→ on a narrow frame.
func TestTheSpendHeadDoesNotTakeTheWindowKeysToSayTheSpan(t *testing.T) {
	r := spendTestReading()
	for _, width := range []int{160, 120, 80, 60} {
		head := r.headWords(width)
		arrows, _ := placeWindowFits(width, head, r.window)
		if !arrows {
			t.Fatalf("at %d cells the head %q crowded the window control off the row", width, head)
		}
		row := plain(r.windowHeaderRow(width, newPalette(tokens.NoColor, false)))
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("at %d cells the head row measures %d: %q", width, got, row)
		}
	}
}

// TestTheSpendChartUsesTheRoomItHasAndItsAxisIsTwoDates is audit-settings row 9.
// The sparkline was fourteen cells wide on a hundred-and-sixty-cell frame, and
// its axis put a date on the left against a MONEY figure on the right — two ends
// that are not the same kind of thing, over a shape with no scale — where that
// figure was the one the pointer line two rows above had already given.
func TestTheSpendChartUsesTheRoomItHasAndItsAxisIsTwoDates(t *testing.T) {
	r := spendTestReading().todayed(0.13)
	pal := newPalette(tokens.NoColor, false)
	last := 0
	for _, width := range []int{60, 80, 120, 160} {
		spark := r.sparkline(width)
		cells := ansi.StringWidth(spark)
		if cells <= r.window.Buckets() && width > 80 {
			t.Fatalf("at %d cells the chart is still %d wide — one cell a day on a frame with room for more: %q",
				width, cells, spark)
		}
		if cells < last {
			t.Fatalf("at %d cells the chart shrank to %d from %d", width, cells, last)
		}
		last = cells
		axis := plain(r.sparkAxis(cells, pal))
		if !strings.HasPrefix(axis, r.days[0].Label) {
			t.Fatalf("at %d cells the axis starts %q, want the window's first day %q", width, axis, r.days[0].Label)
		}
		if !strings.HasSuffix(strings.TrimRight(axis, " "), spendTodayWord) {
			t.Fatalf("at %d cells the axis ends %q, want it to end on the last bucket's own date", width, axis)
		}
		if strings.Contains(axis, "$") {
			t.Fatalf("at %d cells the axis reads %q, want two dates — the money belongs to the pointer line", width, axis)
		}
	}
}

// TestTheLoudestDaySaysWhereItsDoorGoes is audit-settings row 17. The row
// right-aligned the bare noun `tasks`, which reads as a fourth fact about the
// day, and at 60 columns it abutted the ellipsis of the sentence in front of it:
// `rebuild-the-frame… tasks`.
//
// THE ROW IS GONE AND THE AUDIT'S POINT SURVIVES IT. The loudest day is a clause
// on the head line now and the key is named on the heading directly over the
// rows it works on — which is the same law the audit was applying: a door is
// said where it can be walked through.
func TestTheLoudestDaySaysWhereItsDoorGoes(t *testing.T) {
	r := spendTestReading()
	pal := newPalette(tokens.NoColor, false)
	rows := plainSpendRows(r.rows(140, pal))
	for _, row := range rows {
		if strings.Contains(row, "was the loudest day") {
			t.Fatalf("the loudest day still has a row of its own: %q", row)
		}
	}
	// THE HEAD CARRIES IT, with the figure, the date and what it went on.
	if got := rows[1]; !strings.Contains(got, "loudest day: $21.40 aug 20 (the-filings-sweep)") {
		t.Fatalf("the head line reads %q", got)
	}
	// AND THE DOOR IS NAMED OVER THE ROWS IT OPENS, not four lines above a table
	// it was never about.
	heading, table := -1, -1
	for at, row := range rows {
		if strings.Contains(row, spendSubjectsWord) {
			heading = at
		}
		if heading >= 0 && at > heading && strings.Contains(row, "the-filings-sweep") {
			table = at
			break
		}
	}
	if heading < 0 || table != heading+1 {
		t.Fatalf("the heading and its first row are not neighbours:\n%s", strings.Join(rows, "\n"))
	}
	// AND IT IS A CLAUSE OF THE CAPTION'S OWN SENTENCE, in the grammar the other
	// caption on this page already uses — `what ran it · by the model, and the
	// role it was bound to` — rather than an instruction flushed to the far end
	// of the line, which made one heading two objects.
	if want := spendSubjectsWord + rowSep + spendOpensWord; !strings.Contains(rows[heading], want) {
		t.Fatalf("the heading over the doors reads %q, want %q", rows[heading], want)
	}
	// AND A FRAME WITH NO ROOM FOR BOTH KEEPS THE CAPTION. rowfit's law 1: the
	// identity survives and the fact about it goes.
	tight := len(placeLead) + ansi.StringWidth(spendSubjectsWord+rowSep+spendOpensWord) - 1
	for _, row := range plainSpendRows(r.rows(tight, pal)) {
		if strings.Contains(row, spendSubjectsWord) && strings.Contains(row, "enter") {
			t.Fatalf("a %d-cell frame kept a door word it has no room for: %q", tight, row)
		}
	}
}

// TestSettingsSpendingReadsTheDayThroughTheOneLedgerSeam is audit-settings row 4
// of the fix brief: `readDayCost` opened [app.usageLedger] itself, so on a
// `--host` window Settings → Spending drew THIS laptop's day on a page about
// another machine.
func TestSettingsSpendingReadsTheDayThroughTheOneLedgerSeam(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.Local)
	a := placeApp(t)
	a.clock = func() time.Time { return now }
	// A LOCAL LEDGER THIS WINDOW MUST NOT READ, on a window whose money belongs
	// to the far machine.
	a.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
	writeUsageLines(t, a.usageLedger, []session.UsageLine{
		{At: now.Add(-time.Hour), Model: "opus 4.1", Calls: 1, Input: 10, Output: 5, USD: 9.99},
	})
	a.ledger = func(since time.Time) ([]session.UsageLine, bool, bool) {
		return []session.UsageLine{
			{At: now.Add(-2 * time.Hour), Model: "opus 4.1", Calls: 1, Input: 10, Output: 5, USD: 1.25},
			// A zero-priced line is unpriced and not free: the one arithmetic
			// ([spendDayTotal]) leaves it out, and the walk that used to be here
			// added it in.
			{At: now.Add(-time.Hour), Model: "silent", Calls: 1, Input: 5, Output: 5, USD: 0},
		}, true, true
	}
	a.readDayCost()
	spent, counted := a.spentTodayUSD()
	if !counted || spent != 1.25 {
		t.Fatalf("the Spending tab's `today` is %.2f (counted %v), want the far machine's 1.25 through [app.usageSince]",
			spent, counted)
	}
	// AND A SEAM THAT HAS NOT ANSWERED HAS NOT COUNTED ZERO.
	a.ledger = func(time.Time) ([]session.UsageLine, bool, bool) { return nil, false, false }
	a.readDayCost()
	if _, counted := a.spentTodayUSD(); counted {
		t.Fatal("a ledger nobody could read was counted as a day that cost nothing")
	}
}

// writeUsageLines puts a ledger on disk for a test that needs one to be ignored.
func writeUsageLines(t *testing.T, path string, lines []session.UsageLine) {
	t.Helper()
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
}

// ── the memory place ────────────────────────────────────────────────────────

// TestTheMemoryRowsUseTheSurfacesOneSeparator is audit-settings row 7. The rows
// glued their fields together with two spaces while every clause built for those
// same rows joined with ` · `, so one line carried two separator grammars and
// `· Ships on Fridays  fact  let go` gave a reader no way to tell where the
// memory's own words stopped.
func TestTheMemoryRowsUseTheSurfacesOneSeparator(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r := readMemory(memoryPlaceFixture(now), map[string]bool{store.MemoryScopeUser: true}, "", now)
	rows := r.rows(200, newPalette(tokens.NoColor, false))
	for at, line := range r.lines {
		switch line.kind {
		case memoryReadingShelf, memoryReadingMemory:
		default:
			continue
		}
		row := plain(rows[at])
		// The age is right-aligned and the run of air before it is the column,
		// not a separator: what is under test is the half of the row that
		// carries the facts.
		facts := strings.TrimRight(strings.SplitN(strings.TrimRight(row, " "), "  ", 2)[0], " ")
		if strings.Contains(facts, "  ") {
			t.Fatalf("row %d reads %q, want its facts joined with %q — one separator, one meaning", at, facts, rowSep)
		}
		if len(line.facts) > 0 && !strings.Contains(facts, rowSep) {
			t.Fatalf("row %d reads %q and has facts to say, want them behind %q", at, facts, rowSep)
		}
	}
}

// TestTheShelvesHeadingIsWholeBeforeItsLegend is audit-settings row 13. At 80
// columns the section's identity was truncated — `shelves · …` — to keep a count
// of correction memories whole, which is rowfit's first law inverted.
func TestTheShelvesHeadingIsWholeBeforeItsLegend(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r := readMemory(memoryPlaceFixture(now), nil, "", now)
	at := -1
	for i, line := range r.lines {
		if line.kind == memoryReadingSection {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the reading drew no shelves heading")
	}
	kinds := 0
	for _, width := range []int{200, 120, 80, 60} {
		// The heading stands on the place's one left edge ([placeLead]).
		row := strings.TrimPrefix(plain(r.rows(width, newPalette(tokens.NoColor, false))[at]), placeLead)
		if !strings.HasPrefix(row, memorySectionWord) {
			t.Fatalf("at %d cells the heading reads %q, want %q whole in front of the legend", width, row, memorySectionWord)
		}
		// AND THE LEGEND IS A PREFIX OF ITSELF: kinds fall off the end one at a
		// time as the frame narrows, biggest first, and none is skipped forward
		// into the gap a wider one could not use.
		if n := strings.Count(row, " · ") - 1; kinds > 0 && n > kinds {
			t.Fatalf("at %d cells the legend grew to %d kinds from %d as the frame narrowed: %q", width, n, kinds, row)
		} else if n >= 0 {
			kinds = n
		}
	}
}

// TestAMemoryLetGoAndOneReplacedAreNotTheSameWord is the fix brief's row 6. The
// phrase `let go` was a count on the head AND the tag on a row, and worse, the
// tag was worn by two different states: a line somebody asked to be forgotten
// and a line the machine retired because something newer contradicted it. The
// head added them together.
func TestAMemoryLetGoAndOneReplacedAreNotTheSameWord(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	shelves := memoryPlaceFixture(now)
	// THE TWO RETIRED LINES ARE PUT WHERE THE PAGE DRAWS THEM. A shelf shows
	// three memories and a fold ([memoryShelfShown]); the fixture's forgotten and
	// superseded lines are the last two of six, which is the fold and not the
	// row this test is about.
	for at, shelf := range shelves.Shelves {
		if shelf.Scope != store.MemoryScopeUser {
			continue
		}
		retired := shelf.Memories[len(shelf.Memories)-2:]
		shelves.Shelves[at].Memories = append(append([]store.Memory{}, retired...), shelf.Memories[:len(shelf.Memories)-2]...)
	}
	r := readMemory(shelves, map[string]bool{store.MemoryScopeUser: true}, "", now)
	text := memoryPlaceText(r, 200)
	if !strings.Contains(text, "1 let go · 1 replaced") {
		t.Fatalf("the head counts the two states as one:\n%s", text)
	}
	if !strings.Contains(text, "Feature branches · fact · "+memoryLetGoWord) {
		t.Fatalf("the memory somebody let go of does not say so:\n%s", text)
	}
	if !strings.Contains(text, "TUI2 is live · project state · "+memoryReplacedWord) {
		t.Fatalf("a memory the machine retired on its own still says %q:\n%s", memoryLetGoWord, text)
	}
	// AND ZERO OF EITHER IS STILL NOTHING (the emptiness law).
	quiet := shelves
	quiet.LetGo, quiet.Superseded = 0, 0
	if got := memoryPlaceText(readMemory(quiet, nil, "", now), 200); strings.Contains(got, "0 "+memoryReplacedWord) {
		t.Fatalf("zero replaced was drawn as a fact:\n%s", got)
	}
}

// A PLACE'S WHISPER WRAPS LIKE HOME'S, AND NOTHING CUTS IT. Audit-help rows 11
// and 12 found memory's explanation of itself cut mid-word at 80 columns; the
// cure was one line that gave up its example after the middle dot — and then
// took an ellipsis wherever there was no clause to give, so at 44 columns the
// tasks whisper read `work you send off with /task lands here, and…`. It now
// takes the dim lines it needs from home's own wrapper (placeprose.go's
// [placeWhisperLines]): each empty place is drawn at three widths, and its
// whisper has to be all of its own words, one to three lines, hung where home
// hangs a panel's, with no `…` anywhere.
func TestEveryPlaceWhisperWrapsLikeHomesAndIsNeverCut(t *testing.T) {
	for _, lab := range everyEmptyPlace() {
		sentence := placeWhisper[lab.id].whisper
		for _, width := range []int{44, 58, 80} {
			a := lab.open(t)
			a.width, a.height = width, 24
			lines, _, _, _ := a.placeDraw(placeFor(lab.id), a.width, a.height)
			rows := make([]string, len(lines))
			for i, line := range lines {
				rows[i] = strings.TrimRight(plain(line), " ")
			}
			said := whisperRowsUnder(rows, placeHeadRows)
			if len(said) == 0 || len(said) > 3 {
				t.Fatalf("the empty %s place at %d cells whispers on %d rows:\n%s",
					lab.id.word(), width, len(said), strings.Join(rows, "\n"))
			}
			// One line where the sentence fits beside its lead, and more only
			// where it does not — a wrap, never a second row nobody needed.
			if fits := len(placeWhisperLead)+ansi.StringWidth(sentence) <= width; fits != (len(said) == 1) {
				t.Fatalf("the empty %s place at %d cells whispers on %d rows, and the sentence fits=%v:\n%s",
					lab.id.word(), width, len(said), fits, strings.Join(said, "\n"))
			}
			words := make([]string, 0, len(said))
			for _, row := range said {
				if !strings.HasPrefix(row, placeWhisperLead) || strings.HasPrefix(row, placeWhisperLead+" ") ||
					strings.Contains(row, glyphMore) || ansi.StringWidth(row) > width {
					t.Fatalf("at %d cells a row of the %s whisper is not hung, whole and inside the frame: %q",
						width, lab.id.word(), row)
				}
				words = append(words, strings.TrimPrefix(row, placeWhisperLead))
			}
			if got := strings.Join(words, " "); got != sentence {
				t.Fatalf("at %d cells the %s whisper says %q, not its own sentence %q", width, lab.id.word(), got, sentence)
			}
		}
	}
}

// whisperRowsUnder is the dim lines under the first heading of a place's body —
// the rows from `top` on that are hung in a whisper's lead, up to the first
// row that is not.
func whisperRowsUnder(rows []string, top int) []string {
	var said []string
	for _, row := range rows[top+1:] {
		if !strings.HasPrefix(row, placeWhisperLead) || strings.TrimSpace(row) == "" {
			break
		}
		said = append(said, row)
	}
	return said
}
