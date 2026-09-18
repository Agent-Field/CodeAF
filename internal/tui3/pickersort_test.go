package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/lane"
)

// sortCatalog is a list with a value and a GAP in every sortable column, because
// the gaps are what most of the laws below are about.
var sortCatalog = []Model{
	{ID: "b/cheap", PromptPrice: 1e-7, CompletionPrice: 2e-7, ContextLength: 8_000, ArenaElo: 900},
	{ID: "a/dear", PromptPrice: 9e-6, CompletionPrice: 9e-5, ContextLength: 1_000_000, ArenaElo: 1400},
	{ID: "c/middle", PromptPrice: 1e-6, CompletionPrice: 1e-6, ContextLength: 200_000, ArenaElo: 1200},
	// Publishes nothing at all: the row every gap law here has to place last.
	{ID: "d/silent"},
}

func sortApp(t *testing.T) *app {
	t.Helper()
	a := pickerApp(t, &fakeAgent{model: "c/middle"}, sortCatalog)
	a.width, a.height = 120, 40
	typeLine(t, a, "/model")
	return a
}

// sortOn is a table sort naming one column of a set by its head, so a test says
// `elo` where the code says an index.
func sortOn(cols []tableColumn, head string, back bool) tableSort {
	for at, col := range cols {
		if col.head == head {
			return tableSort{at: at + 1, back: back}
		}
	}
	panic("no column " + head)
}

// A TABLE IS ALWAYS SORTED, and the order it opens in is its first column — the
// name — ascending. There is no unsorted state to explain, and the heading always
// carries an arrow saying which order you are in.
func TestATableOpensSortedByItsFirstColumn(t *testing.T) {
	a := sortApp(t)
	if got := a.pick.sort; got != (tableSort{}) {
		t.Fatalf("the list opened on sort %+v, want the zero value", got)
	}
	if got := a.pick.sort.column(); got != tableSortName {
		t.Fatalf("the zero sort orders column %d, want the name", got)
	}
	want := []string{"a/dear", "b/cheap", "c/middle", "d/silent"}
	if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the list opened as %v, want %v", got, want)
	}
	head := a.pick.tableFit(a.width).header()
	if !strings.Contains(head, modelHead+" "+tasksSortDown) {
		t.Fatalf("the opening heading carries no arrow on the name: %q", head)
	}
}

// EVERY COLUMN IS TWO RUNGS: its own direction, then turned round, before the key
// moves on. So one key reaches every order the table has.
func TestTheSortKeyWalksEachColumnBothWaysRound(t *testing.T) {
	a := sortApp(t)
	// This list publishes no `via`, `first` or `t/s`, so those are skipped.
	want := []string{
		"model ↓", "model ↑",
		"in/M ↓", "in/M ↑",
		"out/M ↓", "out/M ↑",
		"window ↓", "window ↑",
		"elo ↓", "elo ↑",
		"model ↓",
	}
	got := []string{sortWord(a.pick.sort)}
	for range len(want) - 1 {
		drive(t, a, key(tasksSortKeyChord))
		got = append(got, sortWord(a.pick.sort))
	}
	if strings.Join(got, " · ") != strings.Join(want, " · ") {
		t.Fatalf("the cycle walked\n  %s\nwant\n  %s", strings.Join(got, " · "), strings.Join(want, " · "))
	}
}

// AND SHIFT RETRACES IT. The back chord walks the same rungs in reverse rather
// than being a second way to say "reverse", which is what it meant while a column
// was one rung.
func TestTheBackChordRetracesTheCycle(t *testing.T) {
	a := sortApp(t)
	forward := []string{}
	for range 6 {
		drive(t, a, key(tasksSortKeyChord))
		forward = append(forward, sortWord(a.pick.sort))
	}
	back := []string{}
	for range 6 {
		back = append(back, sortWord(a.pick.sort))
		drive(t, a, key(tasksSortBackChord))
	}
	for at := range forward {
		if got, want := back[at], forward[len(forward)-1-at]; got != want {
			t.Fatalf("walking back gave %v, which does not retrace %v", back, forward)
		}
	}
	if got := sortWord(a.pick.sort); got != "model ↓" {
		t.Fatalf("six back from six forward is %q, want where it started", got)
	}
}

// sortWord is a sort as the heading says it, for a test about the cycle's order.
func sortWord(s tableSort) string {
	head := modelHead
	if at := s.column(); at != tableSortName {
		head = modelColumns[at].head
	}
	return head + " " + s.arrow()
}

// EACH COLUMN OPENS THE WAY THE QUESTION IS ASKED. Nobody opens a price column to
// find the most expensive model, so the useful order is never two presses away.
func TestTheSortOrdersEachColumnTheWayItIsRead(t *testing.T) {
	for _, probe := range []struct {
		head string
		want []string
	}{
		// Cheapest first, and the row with no price last.
		{"in/M", []string{"b/cheap", "c/middle", "a/dear", "d/silent"}},
		{"out/M", []string{"b/cheap", "c/middle", "a/dear", "d/silent"}},
		// Biggest window and highest score first.
		{"window", []string{"a/dear", "c/middle", "b/cheap", "d/silent"}},
		{"elo", []string{"a/dear", "c/middle", "b/cheap", "d/silent"}},
	} {
		t.Run(probe.head, func(t *testing.T) {
			a := sortApp(t)
			a.pick.sort = sortOn(modelColumns, probe.head, false)
			a.pick.rank()
			if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(probe.want, ",") {
				t.Fatalf("%s gave %v, want %v", probe.head, got, probe.want)
			}
		})
	}
}

// WHAT NOBODY PUBLISHED SORTS LAST IN BOTH DIRECTIONS. A model with no published
// price is not the cheapest model, and turning the column round must not make it
// the dearest: it is not in the comparison at all. The `cheap` filter word this
// replaces got the rule right in one direction only, by calling an unknown price
// infinity.
func TestWhatNobodyPublishedSortsLastEitherWayRound(t *testing.T) {
	for _, head := range []string{"in/M", "out/M", "window", "elo"} {
		t.Run(head, func(t *testing.T) {
			a := sortApp(t)
			a.pick.sort = sortOn(modelColumns, head, false)
			a.pick.rank()
			forward := pickerIDs(a)
			if forward[len(forward)-1] != "d/silent" {
				t.Fatalf("%s put the silent row at %v", head, forward)
			}
			a.pick.sort = sortOn(modelColumns, head, true)
			a.pick.rank()
			back := pickerIDs(a)
			if back[len(back)-1] != "d/silent" {
				t.Fatalf("%s reversed put the silent row at %v", head, back)
			}
			// AND THE KNOWN ROWS DID turn round, so this is not a sort that
			// quietly refused: the three that published a figure come back in the
			// opposite order while the silent one stays where it is.
			if strings.Join(back[:3], ",") != strings.Join(reversedOf(forward[:3]), ",") {
				t.Fatalf("%s: forward %v, reversed %v — the known rows did not turn round",
					head, forward, back)
			}
		})
	}
}

// A COLUMN NOBODY PUBLISHED IS NOT A RUNG. With nothing measured there is no
// `via`, no `first` and no `t/s`, and a rung for one would be a press that moves no
// row and turns no arrow — indistinguishable from the key being broken.
func TestTheSortCycleSkipsAColumnNobodyPublished(t *testing.T) {
	forgetLanes()
	lane.Default().Reset()
	a := sortApp(t)
	for range len(modelColumns) * 3 {
		drive(t, a, key(tasksSortKeyChord))
		if at := a.pick.sort.column(); at != tableSortName {
			switch modelColumns[at].head {
			case "via", "first", "t/s":
				t.Fatalf("the cycle stopped on %s, which this list has no column for",
					modelColumns[at].head)
			}
		}
	}

	// AND IT IS A RUNG AGAIN once something has been measured, because the
	// question is about the data and not about the key.
	laneLab(t, threeLanes())
	b := laneApp(t)
	b.width, b.height = 120, 40
	typeLine(t, b, "/model")
	at := 0
	for n, col := range modelColumns {
		if col.head == "first" {
			at = n
		}
	}
	if !b.pick.sortableColumn(at) {
		t.Fatal("first is not sortable on a list whose providers have been measured")
	}
}

// THE SORTED COLUMN WEARS THE ARROW, and it is MEASURED rather than appended: a
// head that grew two cells after the columns were budgeted would push the block
// past the measure and move every row under it sideways.
func TestTheSortedColumnWearsTheArrowInsideItsOwnWidth(t *testing.T) {
	a := sortApp(t)
	opened := ansi.StringWidth(a.pick.tableFit(a.width).header())

	a.pick.sort = sortOn(modelColumns, "elo", false)
	a.pick.fitAt = 0
	marked := a.pick.tableFit(a.width)
	head := marked.header()
	if !strings.Contains(head, "elo "+tasksSortDown) {
		t.Fatalf("the heading does not carry the arrow on elo: %q", head)
	}
	if got := ansi.StringWidth(head); got != opened {
		t.Fatalf("the heading is %d cells with the arrow on elo and %d with it on the name — the block moved", got, opened)
	}
	// AND EVERY ROW IS STILL THE TABLE'S WIDTH, which is the alignment itself.
	row := marked.row(a.pick.rowCells(sortCatalog[0], ""))
	if got, want := ansi.StringWidth(row), marked.facts+marked.pad; got != want {
		t.Fatalf("a row is %d cells and the table is %d", got, want)
	}
}

// THE CURSOR LANDS ON THE TOP AFTER THE KEY, because the top is the answer —
// while OPENING the list still lands on the model in use, so enter with nothing
// typed still confirms. Those are two different moments and only one wants row one.
func TestTheSortKeyPutsTheCursorOnTheAnswerAndOpeningDoesNot(t *testing.T) {
	a := sortApp(t)
	if chosen, _ := a.pick.choice(); chosen.ID != "c/middle" {
		t.Fatalf("the list did not open on the model in use: %q", chosen.ID)
	}
	for sortWord(a.pick.sort) != "in/M ↓" {
		drive(t, a, key(tasksSortKeyChord))
	}
	chosen, ok := a.pick.choice()
	if !ok || chosen.ID != "b/cheap" {
		t.Fatalf("after the key the cursor is on %q, want the cheapest", chosen.ID)
	}
}

// THE NAME COLUMN IS RELEVANCE FIRST WHILE SOMETHING IS TYPED. The column's order
// and the search's order are both orders of the same column, and with a query in
// the box the search's is the one that was asked for: plain alphabetical put the
// fuzzy hit above the exact one, which is the search itself going wrong.
func TestTheNameColumnPutsTheBestMatchFirst(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")

	// The list opens sorted by name, so with nothing typed it is plainly
	// alphabetical.
	if got := pickerIDs(a); got[0] != "anthropic/claude-gpt-echo" {
		t.Fatalf("with an empty box the list starts %v, want alphabetical", got)
	}

	// AND WITH `gpt` TYPED the prefix match leads, then the substring, then the
	// loose letters — the three tiers, not the alphabet.
	typeInto(t, a, "gpt")
	want := []string{"gpt-5-classic", "openai/gpt-4.1-mini", "anthropic/claude-gpt-echo"}
	if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("typing gpt gave %v, want %v", got, want)
	}

	// AND THE ARROW STILL MEANS SOMETHING: it decides the order INSIDE a tier, so
	// turning it round cannot promote a worse match over a better one.
	drive(t, a, key(tasksSortKeyChord))
	if got := pickerIDs(a); got[0] != "gpt-5-classic" {
		t.Fatalf("turned round, the best match is no longer first: %v", got)
	}
}

// THE SORT ORDERS WHAT THE FILTER KEPT, so `e` then the key is the matching rows in
// that column's order rather than the cheapest rows that happen to match.
func TestTheSortOrdersWhatTheFilterKept(t *testing.T) {
	a := sortApp(t)
	typeInto(t, a, "e")
	kept := pickerIDs(a)
	if len(kept) < 2 {
		t.Fatalf("the filter kept %v, too few to order", kept)
	}
	a.pick.sort = sortOn(modelColumns, "in/M", false)
	a.pick.rank()
	sorted := pickerIDs(a)
	if len(sorted) != len(kept) {
		t.Fatalf("the sort changed which rows were kept: %v then %v", kept, sorted)
	}
	if sorted[0] != "b/cheap" {
		t.Fatalf("the sorted hits start %v, want the cheapest of them", sorted)
	}
}

// ── THE PROVIDERS' OWN TABLE ────────────────────────────────────────────────

// THE SAME KEY ORDERS THE PROVIDERS WHEN THE CURSOR IS IN THEM, which is the rule
// the fold's other keys already follow: typing there filters the machines and `←`
// there closes the machines. A key that ordered the models while somebody was
// reading a provider table would be the one gesture here that ignores the cursor.
func TestTheSortKeyOrdersTheProvidersWhenTheCursorIsInThem(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down"), key("right")) // into the machines

	// The fold opens in ITS first column, the provider name, ascending — which is
	// the alphabetical order these rows have always been drawn in.
	if got := a.pick.laneSort; got != (tableSort{}) {
		t.Fatalf("the fold opened on sort %+v, want the zero value", got)
	}
	if got := laneNames(a.pick.lanes); strings.Join(got, ",") != "cloudflare,coreweave,deepinfra" {
		t.Fatalf("the fold opened as %v, want alphabetical", got)
	}
	modelBefore := a.pick.sort

	// ONE press turns the NAME column round, because the name is two rungs like
	// every other column.
	drive(t, a, key(tasksSortKeyChord))
	if a.pick.sort != modelBefore {
		t.Fatalf("the key moved the MODEL list's sort to %+v while the cursor was in the providers", a.pick.sort)
	}
	if got := laneNames(a.pick.lanes); strings.Join(got, ",") != "deepinfra,coreweave,cloudflare" {
		t.Fatalf("the name turned round holds %v, want reverse alphabetical", got)
	}
	// AND THE SECOND press moves on to `first`, ascending: coreweave 0.4s,
	// cloudflare 0.8s, deepinfra 1.2s.
	drive(t, a, key(tasksSortKeyChord))
	if got := laneNames(a.pick.lanes); strings.Join(got, ",") != "coreweave,cloudflare,deepinfra" {
		t.Fatalf("sorted by first the fold holds %v", got)
	}
	// AND THE PROVIDERS' HEADING WEARS ITS OWN ARROW.
	if head := a.pick.laneFit(a.width).header(); !strings.Contains(head, "first "+tasksSortDown) {
		t.Fatalf("the providers' heading reads %q", head)
	}

	// Turned round: deepinfra slowest first.
	drive(t, a, key(tasksSortKeyChord))
	if got := laneNames(a.pick.lanes); strings.Join(got, ",") != "deepinfra,cloudflare,coreweave" {
		t.Fatalf("reversed by first the fold holds %v", got)
	}
	if head := a.pick.laneFit(a.width).header(); !strings.Contains(head, "first "+tasksSortUp) {
		t.Fatalf("the reversed providers' heading reads %q", head)
	}
}

// AND THE MODEL LIST'S SORT IS UNTOUCHED BY THE FOLD'S, which is what two sorts
// means: walking out of a fold leaves the list in the order it was in.
func TestTheTwoSortsAreHeldApart(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key(tasksSortKeyChord)) // the model list, off the name column
	models := a.pick.sort
	if models == (tableSort{}) {
		t.Fatal("the key did not move the model list's sort")
	}

	drive(t, a, key("right"), key("down"), key("right"), key(tasksSortKeyChord))
	if a.pick.sort != models {
		t.Fatalf("the model list's sort moved to %+v while the fold was being sorted", a.pick.sort)
	}
	drive(t, a, key("left"), key("left"))
	if a.pick.sort != models {
		t.Fatalf("walking out of the fold left the model list on %+v, want %+v", a.pick.sort, models)
	}
}

// `up` STANDS BEFORE `note` in the providers' table: uptime is a figure and reads
// down its last digit with the three figures before it, while a note is prose and
// prose in the middle of a run of numbers breaks the run. It is the drop order too
// — a narrow fold keeps the comparable figure and gives up the one row's caveat.
func TestTheProvidersTablePutsUptimeBeforeTheNote(t *testing.T) {
	heads := make([]string, 0, len(laneColumns))
	for _, col := range laneColumns {
		heads = append(heads, col.head)
	}
	if got := strings.Join(heads, " "); got != "first t/s $/M up note last 8" {
		t.Fatalf("the providers' columns are %q", got)
	}
	// AND THE CELLS ARE IN THE SAME ORDER, which is the only thing that keeps a
	// figure under its own head.
	view := laneView{Name: "x", TTFT: 0.4, Rate: 24, PriceOut: 2.8e-7, Uptime: 99, Tools: true, Known: true}
	cells := laneCells(view)
	if len(cells) != len(laneColumns) {
		t.Fatalf("%d cells for %d columns", len(cells), len(laneColumns))
	}
	if cells[3] != "99%" {
		t.Fatalf("the fourth cell is %q, want the uptime under `up`", cells[3])
	}
}

// reversedOf is a copy of a slice back to front, for a test about two orders being
// each other's opposite.
func reversedOf(in []string) []string {
	out := make([]string, len(in))
	for at, one := range in {
		out[len(in)-1-at] = one
	}
	return out
}

// ── THE FOOT'S ORDER, AND THE TACK ──────────────────────────────────────────

// THE WHOLE LINE READS IN ONE ORDER AT EVERY LEVEL:
//
//	↑↓ pick · ← back · → providers · alt+s sort · enter <verb> · ctrl+t effort · esc
//
// grouped by what each key MOVES — the cursor, then the list, then the one key
// that decides, then the one key that is not about the list at all.
func TestTheFootReadsInOneOrderAtEveryLevel(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 140, 40
	typeLine(t, a, "/model")

	for _, probe := range []struct {
		where string
		walk  []tea.Msg
		want  []string
	}{
		{"a model row", nil,
			[]string{"→ providers", sortKeyWord, "enter switch", effortKeyWord}},
		{"the auto row", []tea.Msg{key("right")},
			[]string{"← back", sortKeyWord, "enter choose"}},
		{"the openrouter row", []tea.Msg{key("right"), key("down")},
			[]string{"← back", "→ providers", sortKeyWord, "enter choose"}},
		{"a machine", []tea.Msg{key("right"), key("down"), key("right")},
			[]string{"← back", sortKeyWord, "enter choose"}},
	} {
		t.Run(probe.where, func(t *testing.T) {
			b := laneApp(t)
			b.width, b.height = 140, 40
			typeLine(t, b, "/model")
			drive(t, b, probe.walk...)
			got := b.hintWord()
			at := -1
			for _, want := range probe.want {
				next := strings.Index(got, want)
				if next < 0 {
					t.Fatalf("on %s the foot does not name %q: %q", probe.where, want, got)
				}
				if next < at {
					t.Fatalf("on %s the foot has %q out of order: %q", probe.where, want, got)
				}
				at = next
			}
			// AND `esc` IS LAST WHEREVER IT IS, because it is the way out.
			if esc := strings.Index(got, "esc"); esc < at {
				t.Fatalf("on %s `esc` is not last: %q", probe.where, got)
			}
		})
	}
}

// AND `← back` STANDS BESIDE THE WALK rather than after enter, because walking out
// of a fold is a move of the CURSOR exactly as `↑↓` is. Home is where both are on
// the line, so home is where the adjacency can be read.
func TestBackStandsBesideThePickAtHome(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "a session", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())
	typeHome(a, "/model")
	runCmd(a.key(key("enter")))
	drive(t, a, key("right")) // into the fold, where there is a way back

	foot := a.targetPickFoot()
	want := targetPickWalkWord + " · ← back"
	if !strings.Contains(foot, want) {
		t.Fatalf("home's foot reads %q, want %q in it", foot, want)
	}
	// AND THE VERB IS `choose` AT THIS DOOR TOO. It said `use it`, which was a
	// second word for the gesture every other row of this same fold called
	// `choose`.
	if !strings.Contains(foot, "enter choose") || strings.Contains(foot, "use it") {
		t.Fatalf("home's foot reads %q, want it to say enter choose", foot)
	}
}

// PINNING A MODEL AT HOME SAYS NOTHING UNDER THE BOX. The line used to read
// `model · <name> · for the next conversation you start here`, which is what the
// SEAM already carries — and carries for as long as the pin lasts, rather than
// until the next note replaces it.
func TestPinningAModelAtHomeLeavesTheLineAlone(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "a session", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	a.pinTargetModel("zhipu/glm-5.3")
	if a.home.msg != "" {
		t.Fatalf("home's line reads %q, want nothing", a.home.msg)
	}
	// AND THE PIN IS STILL VISIBLE, on the rule, which is the point of dropping
	// the sentence rather than the answer.
	if text := homeText(a); !strings.Contains(text, modelBase("zhipu/glm-5.3")) {
		t.Fatalf("the rule does not carry the pinned model:\n%s", text)
	}
}

// THE COMMAND THAT OPENED THE LIST STAYS IN THE BOX, with the placeholder after
// it. The box used to read `› filter by name` alone — a prompt and a grey phrase
// that could have belonged to any list on this surface.
func TestTheModelCommandStaysTackedInTheFilterBox(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")

	box := ""
	for _, line := range strings.Split(plain(frame(a)), "\n") {
		if strings.Contains(line, pickerHint) || strings.Contains(line, "filter") {
			box = strings.TrimRight(line, " ")
		}
	}
	if box == "" {
		t.Fatalf("the filter box is not on the frame:\n%s", plain(frame(a)))
	}
	if !strings.Contains(box, slashPickerTack) {
		t.Fatalf("the box does not keep the command: %q", box)
	}
	// THE TACK COMES FIRST AND THE PLACEHOLDER AFTER IT, which is the order they
	// were typed in.
	if tack, hint := strings.Index(box, slashPickerTack), strings.Index(box, "filter"); tack > hint {
		t.Fatalf("the placeholder is before the tack: %q", box)
	}
	// AND IT IS DRAWN AS A CHIP and not as text, so it reads as the command it is.
	painted := ""
	for _, line := range strings.Split(mustFrame(a), "\n") {
		if strings.Contains(ansi.Strip(line), slashPickerTack) {
			painted = line
		}
	}
	if !strings.Contains(painted, a.pal.chip(slashPickerTack)) {
		t.Fatalf("the tack is not chipped: %q", painted)
	}

	// AND TYPING GOES AFTER IT, leaving the tack whole.
	typeInto(t, a, "deep")
	if got := string(a.pick.filter.value); got != "deep" {
		t.Fatalf("the filter holds %q — the tack is not part of the text", got)
	}
	typed := ""
	for _, line := range strings.Split(plain(frame(a)), "\n") {
		if strings.Contains(line, slashPickerTack) {
			typed = strings.TrimRight(line, " ")
		}
	}
	if !strings.Contains(typed, slashPickerTack+" deep") {
		t.Fatalf("what was typed does not follow the tack: %q", typed)
	}
}

// AND THE TACK NAMES A COMMAND THIS SURFACE ACTUALLY HAS. A chip for a command
// nobody can type would be the box teaching a gesture that does not exist.
func TestTheTackNamesARealCommand(t *testing.T) {
	name := strings.TrimPrefix(slashPickerTack, "/")
	for _, cmd := range commands {
		if cmd.name == name {
			return
		}
	}
	t.Fatalf("%q is not a command this surface has", slashPickerTack)
}
