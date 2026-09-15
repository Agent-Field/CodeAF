package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	// Row zero is the pointer line now (spendplace.go's [spendReading.railsRow]);
	// the window control is the row under it.
	got := plain(spendTestReading().rows(120, newPalette(tokens.NoColor, false))[1])
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

// THE CHART KEEPS EVERY DAY AND SPENDS THE FRAME IT IS GIVEN. It was one cell
// per day at every width — fourteen cells on a hundred-and-sixty-cell line — so
// the law is now that every bucket gets the SAME number of cells and the chart
// is as wide as the frame allows.
func TestTheSpendSparklineKeepsEveryDayInTheWindow(t *testing.T) {
	r := spendTestReading()
	for _, width := range []int{160, 120, 80, 60} {
		spark := r.sparkline(width)
		cells := ansi.StringWidth(spark)
		if cells%r.window.Buckets() != 0 || cells < r.window.Buckets() {
			t.Fatalf("at %d cells the chart is %d wide, want a whole number of cells for each of the window's %d days: %q",
				width, cells, r.window.Buckets(), spark)
		}
		if cells > width {
			t.Fatalf("at %d cells the chart is %d wide, want it inside the frame: %q", width, cells, spark)
		}
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

// THE MODELS RUN DEAREST FIRST, AND THAT ORDER IS THE WHOLE OF THE CHART NOW.
//
// This table drew a bar beside every model — that model's share of the dearest
// one — and the bar is gone. It was a reading the figure at the end of the same
// row already gives, and it cost a reserved column, an alignment law and a
// second reservation in front of it to stop a role word on one row moving it. A
// sorted list whose rows each say what they cost answers the same question, in
// figures a person can also subtract.
func TestTheSpendModelsRunDearestFirstAndDrawNoBars(t *testing.T) {
	r := spendTestReading()
	if r.models[0].Model != "opus 4.1" || r.models[1].Model != "sonnet 4.5" {
		t.Fatalf("models are not dearest first: %#v", r.models)
	}
	text := strings.Join(plainSpendRows(r.rows(120, newPalette(tokens.NoColor, false))), "\n")
	if strings.Contains(text, "\u2588") {
		t.Fatalf("the spend place still draws a bar:\n%s", text)
	}
}

// spendSectionRows is one table of the drawn page: every row under a heading, up
// to the blank line before the next one ([appendPlaceSection]).
func spendSectionRows(t *testing.T, rows []string, heading string) []string {
	t.Helper()
	for at, row := range rows {
		if !strings.Contains(row, heading) {
			continue
		}
		tail := rows[at+1:]
		for end, next := range tail {
			if next == "" {
				return tail[:end]
			}
		}
		return tail
	}
	t.Fatalf("the page has no %q section:\n%s", heading, strings.Join(rows, "\n"))
	return nil
}

// spendCellAt is where a run of text stands on a drawn row, in terminal cells.
func spendCellAt(t *testing.T, row, text string) int {
	t.Helper()
	at := strings.Index(row, text)
	if at < 0 {
		t.Fatalf("the row %q does not carry %q", row, text)
	}
	return ansi.StringWidth(row[:at])
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
	// THE ROW IS READ WHOLE AND NOT AS A FIXED RUN OF CELLS: what follows the
	// name is the table's own column ([spendMeasured]), so the assertion is about
	// the LINE the model is on and not about the cells beside it.
	opus := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "opus 4.1") {
			opus = line
		}
	}
	if !strings.Contains(opus, "conversation") {
		t.Fatalf("opus does not wear the slot it is bound to:\n%s", text)
	}
	if strings.Contains(opus, "execution") {
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
	// AND A MODEL BOUND TO NOTHING WEARS NO ROLE WORD AT ALL. It is asserted on
	// the separator and not on the cell after the name, because what follows the
	// name now is the run of spaces that carries the row out to its first column
	// ([spendColumnAt]) and not a single space.
	if !strings.Contains(text, "gemini 2.5 pro ") || strings.Contains(text, "gemini 2.5 pro ·") {
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

// THE MODELS TABLE IS A TABLE: THE CALLS AND THE TOKENS EACH STAND IN A COLUMN,
// AND EACH FIGURE'S DIGITS END WHERE THE ONE ABOVE THEM ENDS ([spendMeasured]).
//
// Digits are compared from the RIGHT, and with the bars gone the figures are the
// whole of the comparison a person opened this table to make — so they are
// right-aligned rather than merely started in one place. The unit word rides
// behind each numeral because this surface has no header row, and its position
// is what this test measures: the word can only stand still if the digits in
// front of it end where the digits above them end.
func TestTheSpendModelFiguresStandInColumns(t *testing.T) {
	crew := spendCrew{role: map[string]string{
		"opus 4.1": "conversation", "haiku 4.5": "naming", "sonnet 4.5": "execution",
	}}
	r := spendTestReading().crewed(crew)
	for _, width := range []int{80, 100, 160} {
		rows := plainSpendRows(r.rows(width, newPalette(tokens.NoColor, false)))
		table := spendSectionRows(t, rows, spendModelsWord)
		if len(table) < len(r.models) {
			t.Fatalf("at %d cells the models table is short:\n%s", width, strings.Join(rows, "\n"))
		}
		calls, toks := map[int]bool{}, map[int]bool{}
		for at, model := range r.models {
			row := table[at]
			if !strings.Contains(row, r.modelName(model.Model)) {
				t.Fatalf("at %d cells row %d is not %q:\n%s", width, at, model.Model, row)
			}
			calls[spendCellAt(t, row, " "+plural("call", model.Calls))] = true
			toks[spendCellAt(t, row, " "+plural("token", model.Tokens))] = true
		}
		if len(calls) != 1 || len(toks) != 1 {
			t.Fatalf("at %d cells the call counts end in columns %v and the token volumes in %v, want one of each:\n%s",
				width, calls, toks, strings.Join(rows, "\n"))
		}
	}
}

// AND A ROLE WORD ON ONE ROW MOVES NOTHING ON ANY OTHER.
//
// The role is part of the model's own name field now — the heading says the two
// belong together, `by the model, and the role it was bound to` — so the table's
// columns are measured over it like any other cell of the name. It had a column
// held open on every machine while the bars existed, because exactly ONE model
// can answer for a slot on this surface and that one row's bar went out of line;
// with the bars gone there is no chart to protect and the reservation was a
// column of air.
func TestASpendRoleWordKeepsTheFiguresBehindItInColumn(t *testing.T) {
	line := func(model string, calls int, usd float64) session.UsageLine {
		return session.UsageLine{At: spendTestNow, Model: model, Calls: calls, Input: 100, Output: 10, USD: usd, Session: "talk-1"}
	}
	lines := []session.UsageLine{
		line("z-ai/glm-5.3-flash", 9, 1.41),
		line("deepseek/deepseek-v4-pro-0813", 1204, 0.96),
		line("qwen/qwen3.8-27b", 88, 0.46),
	}
	bare := readSpend(lines, session.LastDays(spendTestNow, 14), spendTestNow)
	bound := bare.crewed(spendCrew{role: map[string]string{"deepseek/deepseek-v4-pro-0813": "conversation"}})
	for _, r := range []spendReading{bare, bound} {
		rows := plainSpendRows(r.rows(120, newPalette(tokens.NoColor, false)))
		calls := map[int]bool{}
		for _, row := range spendSectionRows(t, rows, spendModelsWord) {
			calls[spendCellAt(t, row, " calls")] = true
		}
		if len(calls) != 1 {
			t.Fatalf("the call counts end in columns %v, want one:\n%s", calls, strings.Join(rows, "\n"))
		}
	}
}

// A NAME COLUMN IS THE WIDTH OF WHAT IT HOLDS AND IS NEVER SQUEEZED TO KEEP A
// FIELD BEHIND IT. A narrow frame drops WHOLE FIELDS instead, from the right, in
// order of what they are worth.
//
// Squeezing was the first answer and it was wrong in a way only large figures
// showed: the longest name on a table is very often the row that also wears the
// role word, so a squeezed column put that one row's fields out of line and then
// clipped its token figure — one ragged row among straight ones, which reads as
// a defect in the straight ones.
func TestASpendNameColumnIsNeverSqueezedToKeepAField(t *testing.T) {
	r := spendTestReading().unfolding(true)
	var work []session.SubjectSpend
	for _, subject := range r.subjects {
		if subject.Kind != session.SubjectStanding {
			work = append(work, subject)
		}
	}
	widest := 0
	for _, subject := range work {
		widest = max(widest, ansi.StringWidth(tokens.GlyphProseBullet+" "+r.name(subject)))
	}
	for _, width := range []int{200, 120, 80, 60, 40} {
		_, _, table := r.subjectTable(work, width)
		if table.at[1] != widest+spendGutter {
			t.Fatalf("at %d cells the project column is at %d and the widest name is %d — the name was squeezed",
				width, table.at[1], widest)
		}
	}
	// AND THE FIELDS GO IN ORDER OF WHAT THEY ARE WORTH: the kind word first,
	// because the name already says which thing this is, then the project.
	if _, _, wide := r.subjectTable(work, 200); wide.shown != 3 {
		t.Fatalf("a frame with room to spare draws %d of the 3 fields", wide.shown)
	}
	if _, _, narrow := r.subjectTable(work, 46); narrow.shown != 2 {
		t.Fatalf("a 46-cell frame draws %d fields, want the name and the project", narrow.shown)
	}
	if _, _, tight := r.subjectTable(work, 30); tight.shown != 1 {
		t.Fatalf("a 30-cell frame draws %d fields, want the name alone", tight.shown)
	}
}

// AND A NAME TOO WIDE FOR THE COLUMN KEEPS EVERY CELL OF ITSELF. The name is the
// row's payload, so an overrun pushes the field behind it one space late rather
// than being cut down to line a neighbour's up.
func TestASpendNameWiderThanItsColumnIsNotCutDownToFitIt(t *testing.T) {
	long := tokens.GlyphProseBullet + " deepseek-v4-flash-latest-0731-experimental"
	padded := spendColumnAt(long, 12)
	if !strings.Contains(padded, long) {
		t.Fatalf("a name wider than the column was cut down: %q", padded)
	}
	if got := ansi.StringWidth(padded) - ansi.StringWidth(long); got != 1 {
		t.Fatalf("an overrunning row hangs the next field %d cells out, want the one space every row has: %q", got, padded)
	}
}

// `WHAT IT WAS FOR` IS THE SAME TABLE AS `WHAT RAN IT`, laid out by the same
// machinery: the project and the kind word each stand in a column ([spendMeasured]).
//
// The names under this heading are whatever a person called their work, so the
// two fields behind them landed wherever each name happened to end and the eye
// had to find them again on every row — with the money, right-flushed, the only
// thing on the page that stood in a column at all.
func TestTheSpendSubjectFactsStandInColumns(t *testing.T) {
	r := spendTestReading().unfolding(true)
	var work []session.SubjectSpend
	for _, subject := range r.subjects {
		if subject.Kind != session.SubjectStanding {
			work = append(work, subject)
		}
	}
	// TWO ROWS ARE THE FEWEST THAT CAN PROVE A COLUMN, and the fixture's names
	// are deliberately different lengths — a table whose rows happened to be the
	// same width would pass this test without a column in it.
	if len(work) < 2 {
		t.Fatalf("the fixture has %d rows under this heading — too few to prove a column", len(work))
	}
	for _, width := range []int{100, 160} {
		rows := plainSpendRows(r.rows(width, newPalette(tokens.NoColor, false)))
		table := spendSectionRows(t, rows, spendSubjectsWord)
		if len(table) != len(work) {
			t.Fatalf("at %d cells the table has %d rows and the reading %d:\n%s",
				width, len(table), len(work), strings.Join(rows, "\n"))
		}
		projects, kinds := map[int]bool{}, map[int]bool{}
		for at, subject := range work {
			row := table[at]
			if !strings.Contains(row, r.name(subject)) {
				t.Fatalf("at %d cells row %d does not name %q:\n%s", width, at, r.name(subject), row)
			}
			projects[spendCellAt(t, row, spendProjectField(subject))] = true
			kinds[spendCellAt(t, row, subject.Label)] = true
		}
		if len(projects) != 1 || len(kinds) != 1 {
			t.Fatalf("at %d cells the projects start in columns %v and the kind words in %v, want one of each:\n%s",
				width, projects, kinds, strings.Join(rows, "\n"))
		}
	}
}

// THE KIND WORD IS SHORT BECAUSE IT IS A COLUMN AND NOT A SENTENCE.
//
// It read `a task` and `a conversation` — how prose names those things, and
// twice what a column needs beside a project and a figure. The article is a cell
// that says nothing and `conversation` is twelve of them; `chat` is what this
// surface already counts them in (the tasks place's own head row).
func TestTheSpendKindWordsAreColumnWords(t *testing.T) {
	for kind, want := range map[string]string{
		session.SubjectTask:                       "task",
		session.SubjectConversation:               "chat",
		session.SubjectStanding:                   "standing",
		"something this build has never heard of": "",
	} {
		if got := session.UsageSubjectWord(kind); got != want {
			t.Errorf("%q wears the word %q, want %q", kind, got, want)
		}
	}
}

// AND THE PROMISES ARE A TABLE OF THEIR OWN, with the two facts that are theirs
// and nobody else's: how often the promise went off, and what one firing cost.
//
// Mixed in with the work they wore a `standing · 88 firings` tag crammed into the
// project's column and a kind word that had to be suppressed to stop the row
// saying `standing` twice — two special cases in a table of three rows. Given a
// heading of their own they simply have their own columns.
func TestTheSpendPromisesAreATableOfTheirOwn(t *testing.T) {
	r := spendTestReading()
	rows := plainSpendRows(r.rows(120, newPalette(tokens.NoColor, false)))
	// The fold line stands at the foot of the page, under whichever table was
	// drawn last, so the section runs to the end of the rows and the promise is
	// the first of them.
	table := spendSectionRows(t, rows, spendStandingWord)
	if len(table) == 0 {
		t.Fatalf("the promises table is empty:\n%s", strings.Join(rows, "\n"))
	}
	for _, want := range []string{"repo-watch", "88 firings", "$0.04 a run", "$3.31"} {
		if !strings.Contains(table[0], want) {
			t.Fatalf("the promise row does not carry %q: %q", want, table[0])
		}
	}
	// AND NOTHING SAYS `standing` TWICE, which is what the old shared table did
	// the moment it was not suppressed.
	if strings.Count(strings.Join(rows, "\n"), "standing") != 1 {
		t.Fatalf("the word `standing` is said more than once on the page:\n%s", strings.Join(rows, "\n"))
	}
	// AND A PROMISE THAT COSTS A SLIVER A RUN READS AS THE MONEY COLUMN'S FLOOR
	// rather than as the words `under a cent a run`, which used to stand in the
	// kind word's place and could not be lined up against anything.
	sliver := session.SubjectSpend{Kind: session.SubjectStanding, ID: "repo-watch", Calls: 400, USD: 0.4}
	if got := spendEachFigure(sliver); got != "$0.01" {
		t.Fatalf("a sliver a run reads %q, want the money column's floor", got)
	}
}

// AND A SUBJECT WITH NO PROJECT DRAWS NOTHING IN THAT COLUMN, never a dot.
// [filepath.Base] answers "." for the empty string, so a ledger line naming no
// workspace used to read `· talk-1 · . · a conversation` — a mark standing in
// for a fact nobody recorded, which is the emptiness law inverted.
func TestASpendSubjectWithNoProjectDrawsNoTag(t *testing.T) {
	line := session.UsageLine{At: spendTestNow, Model: "opus 4.1", Calls: 3, Input: 100, Output: 10, USD: 1.25, Session: "talk-1"}
	text := strings.Join(plainSpendRows(readSpend([]session.UsageLine{line},
		session.LastDays(spendTestNow, 1), spendTestNow).rows(120, newPalette(tokens.NoColor, false))), "\n")
	if strings.Contains(text, tokens.GlyphProseBullet+" .") {
		t.Fatalf("a line that named no project drew a dot for one:\n%s", text)
	}
	if !strings.Contains(text, "chat") {
		t.Fatalf("the subject row lost its kind word with its project:\n%s", text)
	}
}

// THE TABLE HOLDS WITH EVERY FIGURE AT FULL WIDTH.
//
// A fixture of small change exercises none of this: five-figure call counts,
// billions of tokens and four-figure money are where a layout that measured one
// field and drew another finally runs off the edge, and where a numeral that was
// not right-aligned is finally visible as a fault. The demo home carries a heavy
// stretch for the same reason (cmd/aforge-demo-home's demoHeavy); this pins what
// the page does with one.
func TestTheSpendTableHoldsWithLargeFigures(t *testing.T) {
	heavy := func(model string, calls, in, out int, usd float64) session.UsageLine {
		return session.UsageLine{At: spendTestNow, Model: model, Calls: calls,
			Input: in, Output: out, USD: usd, Session: "talk-1", Workspace: "/work/the-corpus-sweep"}
	}
	r := readSpend([]session.UsageLine{
		heavy("anthropic/claude-opus-4.1", 128_400, 2_800_000_000, 410_000_000, 4210.55),
		heavy("deepseek/deepseek-v4-pro-0813", 96_120, 1_900_000_000, 260_000_000, 4080.10),
		heavy("z-ai/glm-5.3-flash", 74_300, 1_400_000_000, 190_000_000, 3990.00),
		heavy("openai/gpt-5-mini", 8_400, 90_000_000, 12_000_000, 210.40),
		heavy("mistralai/mistral-nemo", 110, 80_000, 10_000, 0.004),
	}, session.LastDays(spendTestNow, 14), spendTestNow).crewed(spendCrew{
		role: map[string]string{"deepseek/deepseek-v4-pro-0813": "conversation"},
	})

	for _, width := range []int{200, 120, 100, 80, 60} {
		rows := r.rows(width, newPalette(tokens.ANSI256, false))
		// THE ROW WIDTH LAW HOLDS AT FULL FIGURES. Five-figure call counts and
		// four-figure money are where a layout that measured one field and drew
		// another would finally run off the edge.
		for _, row := range rows {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("a spend row is %d cells at width %d: %q", got, width, plain(row))
			}
		}
		// AND THE COLUMNS STILL HOLD, or the frame has given the whole field up
		// — never half of one.
		plainRows := plainSpendRows(rows)
		calls, toks := map[int]bool{}, map[int]bool{}
		for _, row := range spendSectionRows(t, plainRows, spendModelsWord) {
			if at := strings.Index(row, " calls"); at >= 0 {
				calls[ansi.StringWidth(row[:at])] = true
			}
			if at := strings.Index(row, " tokens"); at >= 0 {
				toks[ansi.StringWidth(row[:at])] = true
			}
		}
		if len(calls) > 1 || len(toks) > 1 {
			t.Fatalf("at %d cells the call counts end in columns %v and the token volumes in %v:\n%s",
				width, calls, toks, strings.Join(plainRows, "\n"))
		}
	}
}

// A COUNT AND A FIGURE ON ONE ROW ARE WRITTEN THE SAME WAY, and a unit is never
// left on a number that has outgrown it.
//
// The spend place made both defects plain by putting them side by side. It drew
// `128,400 calls · 3210M` and `$4210.55` on one row: the count grouped, the
// money not — two halves of a row laid out by two people — and a token figure
// three thousand million strong still wearing the million's own `M`, which is
// the reading `1000.0k` was avoided for one rung lower.
func TestSpendFiguresWearOneThousandsMarkAndTheRightUnit(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{groupedInt(128_400), "128,400"},
		{dollars(4210.55), "$4,210.55"},
		{dollars(12491.05), "$12,491.05"},
		{dollars(999.99), "$999.99"},
		{railFigure(50_000), "$50,000"},
		{railFigure(500), "$500"},
		{spendMoneyWord(4210.55), "$4,210.55"},
		{tokenWord(3_210_000_000), "3.2B"},
	} {
		if c.got != c.want {
			t.Errorf("a figure is drawn %q, want %q", c.got, c.want)
		}
	}
	// AND THE PENCE ARE ROUNDED ONCE. The mark goes into digits the formatter
	// has already rounded, so a figure cannot be rounded on the way in and again
	// on the way out.
	if got := dollars(999.995); got != "$1,000.00" {
		t.Errorf("a figure on the rounding boundary is drawn %q, want %q", got, "$1,000.00")
	}
	// AND NO ROW OF THE PAGE CARRIES A NUMBER WITH AN OUTGROWN UNIT ON IT.
	r := readSpend([]session.UsageLine{{At: spendTestNow, Model: "anthropic/claude-opus-4.1",
		Calls: 128_400, Input: 2_800_000_000, Output: 410_000_000, USD: 4210.55, Session: "talk-1"}},
		session.LastDays(spendTestNow, 14), spendTestNow)
	text := strings.Join(plainSpendRows(r.rows(140, newPalette(tokens.NoColor, false))), "\n")
	if outgrown := regexp.MustCompile(`[0-9]{4,}(\.[0-9])?[kMB]`).FindString(text); outgrown != "" {
		t.Fatalf("the page drew %q — a number that has outgrown its unit:\n%s", outgrown, text)
	}
	for _, want := range []string{"128,400 calls", "3.2B", "$4,210.55"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the page does not carry %q:\n%s", want, text)
		}
	}
}

// THE MONEY COLUMN IS A COLUMN OF CENTS WITH A FLOOR UNDER IT.
//
// It used to mix three grammars — `$21.40`, `$0.0068` and the words `under a
// cent` — so three rows of one table were three different kinds of thing, none
// comparable at a glance with the row above. Everything under a cent is now
// drawn as a cent: the smallest figure this column can say and still be read,
// and never `$0.00`, which would report real money as nothing at all.
func TestTheSpendMoneyColumnIsCentsWithAFloor(t *testing.T) {
	for _, row := range []struct {
		usd  float64
		want string
	}{
		{21.40, "$21.40"},
		{0.46, "$0.46"},
		{0.01, "$0.01"},
		{0.0068, "$0.01"},
		{0.0007, "$0.01"},
		{0.000001, "$0.01"},
	} {
		if got := spendMoneyWord(row.usd); got != row.want {
			t.Errorf("%v in the money column is %q, want %q", row.usd, got, row.want)
		}
	}
	// AND THE SENTENCE KEEPS ITS OWN WORDS. The detached turn's note reads `it
	// spent under a cent`, which the manual quotes in those words
	// (internal/manual/chat/keys.md) — a phrase has room in prose and none in a
	// column, which is why the two readings parted rather than one being bent.
	if got := spendSliverWord(0.0017); got != "under a cent" {
		t.Errorf("a sub-cent amount in a sentence is %q, want %q", got, "under a cent")
	}
	if got := stopDetachedNote(session.Usage{CostUSD: 0.0017}); !strings.HasSuffix(got, "it spent under a cent") {
		t.Errorf("the detached note is %q", got)
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
	// THE LAB OPENS ON THE FIXTURE'S CLOCK, NOT THE WALL'S. [app.openSpend]
	// windows the ledger with session.LastDays(a.now(), 14), which is arithmetic
	// on the moment of the open, while every line above is written on a fixed
	// August 2026 date — so a lab left on the wall clock passes only until the
	// fixture drifts out of the fortnight. It did: at midnight on 2026-09-03 the
	// $21.40 line on August 20 fell out of the window and the two tests below
	// went red on a clean tree with no merge behind it.
	// placeeveryone_test.go's [spendPlaceLab] pins the same instant for the
	// same reason, and is the shape to copy.
	a.clock = func() time.Time { return spendTestNow }
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
	// AND A MACHINE THAT HAS SPENT NOTHING MEETS ITS WHISPER INSTEAD, which is
	// the one table's words and not a second set (placeprose.go's [placeWhisper]).
	empty := spendLab(t, nil)
	if got := placeFrameText(empty); !strings.Contains(got, whisperOf(pageSpend)) {
		t.Fatalf("an empty ledger does not say what arrives here:\n%s", got)
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
	// AND A MACHINE THAT HAS SPENT NOTHING STILL MEETS ITS WHISPER.
	b := spendLab(t, nil)
	if !strings.Contains(placeFrameText(b), whisperOf(pageSpend)) {
		t.Fatalf("an empty machine does not say what arrives here:\n%s", placeFrameText(b))
	}
}

// `enter` ON A ROW OF "WHAT IT WAS FOR" OPENS WHAT IT WAS FOR — the money is
// the reading and the thing it went on is the door.
func TestEnterOnASpendRowOpensTheThingTheMoneyWentOn(t *testing.T) {
	talk := session.UsageLine{At: spendTestNow, Model: "opus 4.1", Calls: 2, Input: 100, Output: 20,
		USD: 4.25, Session: "aaaa000000000001", Workspace: "/work/alpha"}
	a := spendLab(t, []session.UsageLine{talk})
	// The cursor opens on the pointer line, which is row zero and a door of its
	// own onto the Spending tab. `↓` walks to the first thing money went on.
	a.moveSpend(1)
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
//
// THE POINTER LINE IS THE ONE EXCEPTION AND IT IS NOT A SUBJECT: it is row zero,
// it names no spend, and its `enter` walks to the one editor money has
// (settingspend.go).
func TestTheSpendCursorStopsOnlyOnRowsThatNameSomething(t *testing.T) {
	a := spendLab(t, spendFixture())
	seen, folds := 0, 0
	for i, stop := range a.spend.stops {
		if !stop.ok {
			continue
		}
		if i == 0 {
			if !stop.rails {
				t.Fatal("row zero is the pointer line and nothing else")
			}
			continue
		}
		if stop.rails {
			t.Fatalf("row %d claims to be the pointer line", i)
		}
		// THE FOLD LINE IS A DOOR ONTO THE REST OF THE SUBJECTS, and it names
		// nothing money was spent on, so it is counted apart.
		if stop.fold {
			folds++
			continue
		}
		seen++
	}
	// THE LOUDEST DAY IS A DOOR TOO, because its row names a thing money was
	// spent on and now says `enter opens it in tasks` out at the right — a key
	// drawn is a key bound (spendplace.go's [spendReading.loudestRow]).
	if want := spendSubjectCap + 1; seen != want {
		t.Fatalf("%d doors were drawn, want %d — the %d shown subjects and the loudest day", seen, want, spendSubjectCap)
	}
	if len(a.spend.reading.subjects) > spendSubjectCap && folds != 1 {
		t.Fatalf("%d fold doors were drawn under %d subjects, want 1", folds, len(a.spend.reading.subjects))
	}
	if !a.spend.stops[a.loudestSpendRow(t)].ok {
		t.Fatal("the loudest day names a task and says so, but nothing opens there")
	}
	if got := plain(a.spend.reading.rows(120, newPalette(tokens.NoColor, false))[0]); !strings.Contains(got, "/budget sets the limits") {
		t.Fatalf("the pointer line reads %q", got)
	}
}

// loudestSpendRow is the drawn row that says which day was loudest.
func (a *app) loudestSpendRow(t *testing.T) int {
	t.Helper()
	for at, row := range plainSpendRows(a.spend.reading.rows(120, newPalette(tokens.NoColor, false))) {
		if strings.Contains(row, "was the loudest day") {
			return at
		}
	}
	t.Fatal("no row says which day was loudest")
	return 0
}

// THE POINTER LINE IS A POINTER AND NOT AN EDITOR, which is what keeps this
// place's own law intact — and `enter` on it opens the one editor there is.
func TestThePointerLineOnTheSpendPlaceOpensTheSpendingTab(t *testing.T) {
	a := spendLab(t, spendFixture())
	a.spend.cursor = 0
	if !a.spendStopAt(0).rails {
		t.Fatal("the pointer line must be a door")
	}
	drive(t, a, key("enter"))
	if a.page != pageSettings || settingTabs[a.sheet.tab] != tabSpending {
		t.Fatalf("enter on the pointer line landed on %q", a.page.word())
	}
}

// AND `b` IS REAL WHERE A BARE LETTER MAY BE REAL: on the verb strip, which is
// drawn before it works (verbstrip.go's first law).
func TestBOnTheSpendPlacesStripOpensTheLimits(t *testing.T) {
	a := spendLab(t, spendFixture())
	verbs := placeSpend{}.verbs(a)
	if len(verbs) != 1 || verbs[0].key != 'b' || verbs[0].word != "the limits" {
		t.Fatalf("the strip offers %v", verbs)
	}
	drive(t, a, key("right"), key("b"))
	if a.page != pageSettings || settingTabs[a.sheet.tab] != tabSpending {
		t.Fatalf("b on the strip landed on %q", a.page.word())
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

// FOCUS WAKES AT THE CENTRE OF MASS. The spend place is asked "what did it cost,
// and on what", so the cursor arrives on the first thing the money went on —
// the head of `what it was for` — and not on the pointer line, whose `enter`
// leaves the bill for the limits editor (PLACES-AUDIT.md finding 16).
func TestSpendFocusWakesOnTheFirstThingTheMoneyWentOn(t *testing.T) {
	a := spendLab(t, spendFixture())
	stop := a.spendStopAt(a.spend.cursor)
	if !stop.ok || stop.rails || stop.fold {
		t.Fatalf("focus woke on body line %d, which is not a subject (%+v)", a.spend.cursor, stop)
	}
	if got, want := spendSubjectKey(stop.subject), spendSubjectKey(a.spend.reading.subjects[0]); got != want {
		t.Fatalf("focus woke on %q, want the biggest subject %q", got, want)
	}
	if row := plainSpendRows(a.spend.reading.rows(120, newPalette(tokens.NoColor, false)))[a.spend.cursor]; strings.Contains(row, "loudest day") {
		t.Fatalf("focus woke on the loudest day and not under `what it was for`: %q", row)
	}
}
