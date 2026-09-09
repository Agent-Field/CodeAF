package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// tasktier_test.go proves the two halves of the one table: that every reading
// this surface can be handed draws the cell and the row the design says it does
// (docs/design/task-states/DESIGN.md), and that no source in this package has
// gone back to spelling a state for itself.

// tierStatus is one node's facts as a surface would hold them, read.
func tierStatus(facts session.TaskFacts) session.TaskStatus { return session.ProjectTask(facts) }

// THE GLYPH IS THE TIER AND NOTHING ELSE, over every combination of tier,
// presence and fault a reading can produce — and over every one of the six
// questions a your-call row can be asking, all of which wear the same cell.
func TestEveryTierDrawsItsOwnCell(t *testing.T) {
	plainPal, asciiPal := palOf(false), palOf(true)
	for _, tc := range []struct {
		what  string
		facts session.TaskFacts
		slot  tokens.GlyphID
	}{
		{"queued", session.TaskFacts{State: session.TaskQueued}, tokens.GQueued},
		{"waiting on work", session.TaskFacts{State: session.TaskQueued, Waits: []string{"Collect sources"}}, tokens.GWaitsOn},
		{"working", session.TaskFacts{State: session.TaskRunning, Liveness: session.TaskLivenessHeld}, tokens.GWorking},
		{"finishing", session.TaskFacts{State: session.TaskRunning, Life: session.TaskPhaseChecking, Liveness: session.TaskLivenessHeld}, tokens.GWorking},
		{"done", session.TaskFacts{State: session.TaskDone}, tokens.GSettled},
		{"stopped", session.TaskFacts{State: session.TaskFailed, Stopped: true}, tokens.GStopped},
		{"incomplete", session.TaskFacts{State: session.TaskFailed, Ending: session.TaskEndingSteps}, tokens.GFailed},
		{"a fault", session.TaskFacts{State: session.TaskFailed, Ending: session.TaskEndingError, Report: "the build would not run"}, tokens.GFailed},
		{"your call", session.TaskFacts{State: session.TaskUnverified}, tokens.GNeedsHuman},
		{"nothing known", session.TaskFacts{}, tokens.GQueued},
	} {
		status := tierStatus(tc.facts)
		if got := tierSlot(status); got != tc.slot {
			t.Errorf("%s picks slot %d, want %d", tc.what, got, tc.slot)
		}
		// AND EVERY TIER OF THE REPERTOIRE DRAWS THAT SLOT AND NOTHING ELSE —
		// the plain floor, the screen reader's character, and the icon a patched
		// font has. A surface that reached for a literal would draw the same
		// thing in all three, which is the bug the vocabulary exists to stop.
		if got := tierGlyph(plainPal, status); got != tokens.Plain.Glyph(tc.slot) {
			t.Errorf("%s draws %q in the plain tier, want %q", tc.what, got, tokens.Plain.Glyph(tc.slot))
		}
		if got := tierGlyph(asciiPal, status); got != tokens.ASCII.Glyph(tc.slot) {
			t.Errorf("%s draws %q in the ascii tier, want %q", tc.what, got, tokens.ASCII.Glyph(tc.slot))
		}
		rich := plainPal
		rich.icons = tokens.NerdFont
		if got := tierGlyph(rich, status); got != tokens.NerdFont.Glyph(tc.slot) {
			t.Errorf("%s draws %q in the nerd-font tier, want %q", tc.what, got, tokens.NerdFont.Glyph(tc.slot))
		}
	}

	// AND ALL SIX QUESTIONS WEAR THE SAME CELL. A person who has learned that `?`
	// means "you" has learned it for every reason there can be.
	for _, one := range tierAskCases() {
		status := tierStatus(one.facts)
		if status.Ask.Kind != one.kind {
			t.Fatalf("%s read as %q, want %q", one.what, status.Ask.Kind, one.kind)
		}
		if got := tierSlot(status); got != tokens.GNeedsHuman {
			t.Errorf("%s picks slot %d, want the question", one.what, got)
		}
	}
}

// THE SHAPE ALONE SAYS THE STATE. Seven readings, seven different cells, in
// every tier — because the roster is read by people who have turned colour off
// and by people who cannot see it, and a vocabulary that needed its hues would
// have nothing to say to either.
func TestEveryStateWearsItsOwnShapeWithNoColour(t *testing.T) {
	states := []session.TaskFacts{
		{State: session.TaskQueued},
		{State: session.TaskQueued, Waits: []string{"Collect sources"}},
		{State: session.TaskRunning, Liveness: session.TaskLivenessHeld},
		{State: session.TaskDone},
		{State: session.TaskFailed, Stopped: true},
		{State: session.TaskFailed, Ending: session.TaskEndingSteps},
		{State: session.TaskUnverified},
	}
	for _, tier := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont, tokens.ASCII} {
		seen := map[string]string{}
		for _, facts := range states {
			status := tierStatus(facts)
			cell := tier.Glyph(tierSlot(status))
			if first, taken := seen[cell]; taken {
				t.Errorf("%s: %q says both %q and %q", tier, cell, first, status.RowWord())
				continue
			}
			seen[cell] = status.RowWord()
		}
	}
}

// tierAskCase is one of the six questions and the facts that produce it.
type tierAskCase struct {
	what  string
	facts session.TaskFacts
	kind  session.TaskAskKind
}

func tierAskCases() []tierAskCase {
	return []tierAskCase{
		{"a proposal with no clock", session.TaskFacts{State: session.TaskQueued, Consent: true}, session.TaskAskStart},
		{"a design waiting", session.TaskFacts{State: session.TaskRunning, Kind: session.TaskKindHarness, Phase: session.HarnessPhaseAsking, Liveness: session.TaskLivenessHeld}, session.TaskAskApprove},
		{"a branch that clashed", session.TaskFacts{State: session.TaskUnverified, Merge: "conflicted", Conflicts: []string{"parser.go", "parser_test.go"}}, session.TaskAskConflict},
		{"nobody could check it", session.TaskFacts{State: session.TaskUnverified}, session.TaskAskCheck},
		{"a landing turned back", session.TaskFacts{State: session.TaskUnverified, Held: true}, session.TaskAskHeld},
		{"a run at its gate", session.TaskFacts{State: session.TaskRunning, Paused: true, Cap: "$5.00", Liveness: session.TaskLivenessHeld}, session.TaskAskCap},
	}
}

// THE HUE IS THE TIER'S TOO, and the one lit element on a row is the question.
// Work that did not finish is DIM unless something actually broke: a dropped
// connection is not a finding.
func TestTheInkIsDimExceptOnAQuestionAndAFault(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	same := func(a, b func(string) string) bool { return a("x") == b("x") }
	for _, tc := range []struct {
		what  string
		facts session.TaskFacts
		want  func(string) string
	}{
		{"your call", session.TaskFacts{State: session.TaskUnverified}, pal.warn},
		{"done", session.TaskFacts{State: session.TaskDone}, pal.muted},
		{"stopped", session.TaskFacts{State: session.TaskFailed, Stopped: true}, pal.dim},
		{"incomplete", session.TaskFacts{State: session.TaskFailed, Ending: session.TaskEndingSteps}, pal.dim},
		{"a fault", session.TaskFacts{State: session.TaskFailed, Ending: session.TaskEndingError, Report: "boom"}, pal.bad},
		{"working", session.TaskFacts{State: session.TaskRunning, Liveness: session.TaskLivenessHeld}, pal.accent},
		{"queued", session.TaskFacts{State: session.TaskQueued}, pal.dim},
	} {
		if got := tierInk(pal, tierStatus(tc.facts)); !same(got, tc.want) {
			t.Errorf("%s is painted %q, want %q", tc.what, got("x"), tc.want("x"))
		}
	}
}

// THE ROW CARRIES THE WORD AND THE REASON, and the reason is what a person acts
// on — so a bare `waiting` and a bare `your call` are both defects.
func TestTheRowNeverReadsABareWord(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	for _, tc := range []struct {
		what  string
		facts session.TaskFacts
		want  string
	}{
		{"a prerequisite", session.TaskFacts{State: session.TaskQueued, Waits: []string{"Collect sources"}}, "⚑ Port the parser · waiting on Collect sources"},
		// A NODE WHOSE CALLS ARE BEING PACED IS WAITING, not turning: the flag is
		// moving with nothing happening this instant, which is exactly what a
		// hold is — and it is what the row says in words as well.
		{"a hold", session.TaskFacts{State: session.TaskRunning, Hold: "machine busy", Liveness: session.TaskLivenessHeld}, "⚑ Port the parser · waiting · machine busy"},
		{"a clock", session.TaskFacts{State: session.TaskQueued, Consent: true, Countdown: "9s"}, "○ Port the parser · auto-starts in 9s"},
		{"a landing", session.TaskFacts{State: session.TaskDone}, "✓ Port the parser · done"},
		{"a stop", session.TaskFacts{State: session.TaskFailed, Stopped: true}, "■ Port the parser · stopped"},
		{"an ending", session.TaskFacts{State: session.TaskFailed, Ending: session.TaskEndingSteps}, "✕ Port the parser · incomplete · ran out of steps"},
		{"a question", session.TaskFacts{State: session.TaskUnverified}, "? Port the parser · your call · nobody could check it"},
		{"a clash", session.TaskFacts{State: session.TaskUnverified, Merge: "conflicted", Conflicts: []string{"parser.go"}}, "? Port the parser · your call · conflicts with your branch: parser.go"},
	} {
		if got := tierRow(pal, tierStatus(tc.facts), "Port the parser", 80); got != tc.want {
			t.Errorf("%s draws %q, want %q", tc.what, got, tc.want)
		}
	}
}

// THE FILE LIST IS THE FIRST THING TO GO, AND THE VERB IS THE LAST. A row cut
// through the one instruction a person needs has spent its cells saying nothing.
func TestANarrowRowShedsTheFileListBeforeTheWord(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	status := tierStatus(session.TaskFacts{
		State: session.TaskUnverified, Merge: "conflicted",
		Conflicts: []string{"parser.go", "parser_test.go", "keys.go"},
	})
	whole := "? Port the parser · your call · conflicts with your branch: parser.go, parser_test.go, keys.go"
	if got := tierRow(pal, status, "Port the parser", 120); got != whole {
		t.Fatalf("a wide row draws %q, want %q", got, whole)
	}
	// Wide enough for the sentence but not for the files: the files go.
	shed := "? Port the parser · your call · conflicts with your branch"
	if got := tierRow(pal, status, "Port the parser", len([]rune(shed))); got != shed {
		t.Fatalf("a narrowed row draws %q, want %q", got, shed)
	}
	// Narrower still: the TITLE gives way and the sentence stays whole.
	cut := tierRow(pal, status, "Port the parser", 54)
	if !strings.HasSuffix(cut, "your call · conflicts with your branch") {
		t.Fatalf("a narrow row lost the sentence: %q", cut)
	}
	if !strings.HasPrefix(cut, "? Port") {
		t.Fatalf("a narrow row lost the name entirely: %q", cut)
	}
	// And a column with room for one of the two keeps the one that answers the
	// question the row is read for.
	only := tierRow(pal, status, "Port the parser", 24)
	if !strings.Contains(only, "your call") {
		t.Fatalf("the narrowest row dropped the word: %q", only)
	}
}

// THE SCREEN-READER TIER GETS THE WORD, never only the cell — which it does by
// construction, because the word is the row and the cell is one character in
// front of it. This pins that a linear frame still spells every state.
func TestTheLinearTierStillSpellsEveryState(t *testing.T) {
	pal := newPalette(tokens.TrueColor, true)
	for _, facts := range []session.TaskFacts{
		{State: session.TaskQueued},
		{State: session.TaskRunning, Liveness: session.TaskLivenessHeld},
		{State: session.TaskDone},
		{State: session.TaskFailed, Stopped: true},
		{State: session.TaskFailed, Ending: session.TaskEndingSteps},
		{State: session.TaskUnverified},
	} {
		status := tierStatus(facts)
		row := tierRow(pal, status, "Port the parser", 80)
		if word := status.RowWord(); word == "" || !strings.Contains(row, word) {
			t.Errorf("%q draws %q with no word in it", facts.State, row)
		}
	}
}

// ── the rail ────────────────────────────────────────────────────────────────

// THE ORDER IS THE TIER'S: the person's call first, then work in flight, then
// work that is over — with the one rank the design keeps for the news a folded
// family wears, because "something in here did not come off" is louder than
// "something in here has not started".
func TestTheRailRanksYourCallFirstThenMovingThenOver(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged})},
		streamEventMsg{gen: a.gen, ev: update(2, "Mix audio", session.TaskQueued, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(3, "Fix the nil-map", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(4, "Cut the goldens", session.TaskFailed, session.TaskNotice{Ending: session.TaskEndingSteps})},
		streamEventMsg{gen: a.gen, ev: update(5, "Port the parser", session.TaskUnverified, session.TaskNotice{Merge: mergeWordAborted, Branch: "task/parser"})},
	)
	ranks := map[uint64]int{}
	for id := uint64(1); id <= 5; id++ {
		ranks[id] = a.railGlyphRank(a.tasks[id])
	}
	if !(ranks[5] < ranks[3] && ranks[3] < ranks[4] && ranks[4] < ranks[2] && ranks[2] < ranks[1]) {
		t.Fatalf("the rail ranks are out of order: %v", ranks)
	}
}

// A FOLDED FAMILY WEARS ITS LOUDEST CHILD, and a child whose decision belongs to
// the parent's own agent is not what makes the fold a demand — the parent is
// already holding that question. It is read off the ask's owner and off nothing
// else, and the row still says what it is asking about.
func TestAFoldedFamilyWearsItsLoudestChild(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Port the parser", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged})},
		streamEventMsg{gen: a.gen, ev: update(2, "Cut the goldens", session.TaskUnverified, session.TaskNotice{
			Merge: mergeWordAborted, Branch: "task/goldens",
		})},
	)
	// A ROOT QUESTION NOBODY ELSE IS HOLDING IS THE LOUDEST THING ON THE COLUMN.
	kid := a.tasks[2]
	if got := a.railGlyphRank(kid); got != 0 {
		t.Fatalf("an unanswered question ranks %d, want 0", got)
	}
	if glyph := plain(a.railTreeGlyph(kid)); glyph != glyphAsk {
		t.Fatalf("the child wears %q, want %q", glyph, glyphAsk)
	}
	// Hand the decision to the model and the fold stops being a demand — and the
	// row goes on reading its reason either way, because a person watching it is
	// owed what it is asking about whoever is answering.
	kid.decider = session.TaskAskOwnerModel
	if got := a.railGlyphRank(kid); got == 0 {
		t.Fatalf("a question the model holds still ranks as a demand")
	}
	if group := a.railGroupOf(kid); group == railDone {
		t.Fatalf("a question nobody has answered is filed under %q", railGroupWords[group])
	}
	under := plain(strings.Join(a.railUnder(kid, 60), " "))
	if !strings.Contains(under, tierYourCallWord) || !strings.Contains(under, "nobody could check it") {
		t.Fatalf("the row stopped reading its reason: %q", under)
	}
}

// ── the roster, the record and home ─────────────────────────────────────────

// ONE VOCABULARY ACROSS THE THREE LISTS. The roster row, the record's word and
// home's cell are three drawings of one reading, and this walks every shape past
// all three.
func TestTheRosterTheRecordAndHomeAgreeOnEveryShape(t *testing.T) {
	a, _, _ := taskApp(t)
	for _, tc := range []struct {
		what  string
		entry session.TaskIndexEntry
		word  string
		glyph string
	}{
		{"done", session.TaskIndexEntry{Status: string(session.TaskDone)}, "done", glyphDone},
		{"stopped", session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: session.TaskEndingStopped}, "stopped", glyphStopped},
		{"incomplete", session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: session.TaskEndingSteps}, "incomplete", glyphBad},
		{"your call", session.TaskIndexEntry{Status: string(session.TaskUnverified)}, tierYourCallWord, glyphAsk},
	} {
		if got := taskStateWord(tc.entry, false); got != tc.word {
			t.Errorf("%s: the record says %q, want %q", tc.what, got, tc.word)
		}
		glyph, _ := tasksGlyph(tasksItem{entry: tc.entry}, a.pal)
		if glyph != tc.glyph {
			t.Errorf("%s: the roster draws %q, want %q", tc.what, glyph, tc.glyph)
		}
		if got := plain(a.homeTaskGlyph(tc.entry, session.SessionRow{})); got != tc.glyph {
			t.Errorf("%s: home draws %q, want %q", tc.what, got, tc.glyph)
		}
	}

	// AND A LIVE ROW SAYS THE MOVING WORDS, which the record can only say about a
	// row a window still vouches for: the file cannot correct itself, so an
	// unvouched claim of running is `incomplete` and not a state it never reached.
	live := session.TaskIndexEntry{Status: string(session.TaskQueued)}
	if got := taskStateWord(live, true); got != "queued" {
		t.Errorf("a vouched queued row says %q, want %q", got, "queued")
	}
	if glyph, _ := tasksGlyph(tasksItem{entry: live, runs: true}, a.pal); glyph != glyphQueued {
		t.Errorf("a vouched queued row draws %q, want %q", glyph, glyphQueued)
	}
	if glyph, _ := tasksGlyph(tasksItem{entry: live}, a.pal); glyph != glyphIdle {
		t.Errorf("an unvouched queued row draws %q, want %q", glyph, glyphIdle)
	}
}

// HOME'S STANDING ROWS SAY THE SAME WORD, and the reason travels with it. The
// clause obeys the emptiness law, which is what a quiet item draws.
func TestHomeStandingRowsSayYourCallAndTheirReason(t *testing.T) {
	item := standing.Item{
		ID: "i1", Words: "keep main green", Workspace: "/w/alpha",
		When:   standing.When{Kind: standing.WhenEvery, Words: "when CI goes red"},
		Status: standing.StatusActive,
	}
	item.NeedsPerson = "the fix touches migrations"
	row := plain(standRollup(StandingItemView{Item: item}, time.Time{}))
	if want := tierYourCallWord + tierReasonSep + "the fix touches migrations"; row != want {
		t.Fatalf("the standing rollup says %q, want %q", row, want)
	}
	item.NeedsPerson = ""
	if got := plain(standRollup(StandingItemView{Item: item}, time.Time{})); strings.Contains(got, tierYourCallWord) {
		t.Fatalf("a row with nothing to report says %q", got)
	}

	// AND THE NEWS LINE UNDER IT SAYS THE SAME WORD.
	if got := standUpdateWord("needs-you", ""); got != tierYourCallWord {
		t.Fatalf("a news line says %q, want %q", got, tierYourCallWord)
	}
	if got := standUpdateWord("needs-you", "the fix touches migrations"); got != tierYourCallWord+": the fix touches migrations" {
		t.Fatalf("a news line with a reason says %q", got)
	}
}

// ── the structural law ──────────────────────────────────────────────────────

// tierDeletedWords are the person-facing spellings the task-states wave removed.
// Each was one surface's private name for a reading internal/session now spells
// once (docs/design/task-states/DESIGN.md).
var tierDeletedWords = []string{
	"needs your look",
	"finished, but needs your look",
	"finished — look it over",
	"awaiting review",
	"stopped — branch kept",
	"delivery needs attention",
}

// tierCardLaneFiles are the landing card's own sources. The card lane owns their
// removal and is landing separately; tasks1words.go is the shim that keeps them
// building and goes when they do.
var tierCardLaneFiles = map[string]bool{
	"taskdone.go":          true,
	"tasksettle.go":        true,
	"taskdeliverylabel.go": true,
	"taskreviewstate.go":   true,
	"tasks1words.go":       true,
}

// NO SOURCE ON THIS SURFACE SPELLS A DELETED STATE. It reads STRING LITERALS and
// not the file's text, on purpose: a comment saying what a word used to be is
// the record of the change and is worth keeping, and a comment cannot reach a
// screen. What may not stand is a literal a row could draw.
func TestNoSourceSpellsADeletedStateWord(t *testing.T) {
	forEachSurfaceLiteral(t, func(file, value string) {
		low := strings.ToLower(value)
		for _, banned := range tierDeletedWords {
			if strings.Contains(low, banned) {
				t.Errorf("%s still spells %q", file, banned)
			}
		}
	})
}

// AND NO CONSTANT ON IT IS `failed` OR `unverified`. Those two are single words
// an engine token is also spelled with — a standing update's kind, a state on the
// wire — so the literal sweep above would name a switch that is reading rather
// than drawing. What a wave can still do wrong is DECLARE one as a word, which is
// exactly what `doneFailWord` and the old presence table did.
func TestNoConstantOnTheSurfaceIsAStateWordAgain(t *testing.T) {
	banned := map[string]bool{"failed": true, "unverified": true, "needs your look": true, "awaiting review": true}
	forEachSurfaceConst(t, func(file, name, value string) {
		if banned[strings.ToLower(value)] {
			t.Errorf("%s declares %s = %q, a word this surface no longer says", file, name, value)
		}
	})
}

// forEachSurfaceLiteral walks every string literal in this package's own
// non-test sources, skipping the card lane's files.
func forEachSurfaceLiteral(t *testing.T, look func(file, value string)) {
	t.Helper()
	forEachSurfaceFile(t, func(name string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(lit.Value); err == nil {
				look(name, value)
			}
			return true
		})
	})
}

// forEachSurfaceConst walks every constant declaration with a literal value.
func forEachSurfaceConst(t *testing.T, look func(file, name, value string)) {
	t.Helper()
	forEachSurfaceFile(t, func(name string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for at, one := range value.Values {
					lit, ok := one.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING || at >= len(value.Names) {
						continue
					}
					if text, err := strconv.Unquote(lit.Value); err == nil {
						look(name, value.Names[at].Name, text)
					}
				}
			}
			return true
		})
	})
}

// forEachSurfaceFile parses this package's non-test sources, minus the card
// lane's.
func forEachSurfaceFile(t *testing.T, look func(name string, file *ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	set := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if tierCardLaneFiles[name] {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		look(name, file)
	}
}

// THE COLUMN ANSWERS THE QUESTION TOO, AND IT ANSWERS IT IN ITS OWN SHAPE.
//
// `<tier glyph> <title> · <word or reason>`, cut from the right, is thirty cells
// of reading in a column that is thirty cells wide, so it is laid over the row
// and the block under it: the name on the row, the word and the reason wrapped
// beneath ([app.railUnder]). A reader that took the first line alone and called
// the rest missing is what filed issue #707 against a column that was saying the
// word all along, so this asserts the ROWS TOGETHER — the glyph, the name, the
// word and the reason are on the column or they are not.
func TestTheColumnSaysTheWordAndTheReasonForALandingThatIsYourCall(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Merge: mergeWordAborted, Branch: "task/parser"})})
	// THE WRAP IS NOT A GAP IN WHAT WAS SAID, so the column is read as one
	// sentence: a reason that ran onto the next row has still been said.
	said := railSaid(a)
	for _, want := range []string{glyphAsk, "Port the parser", tierYourCallWord, "nobody could check it"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the column says nothing about %q:\n%s",
				want, strings.Join(railText(a, a.viewHeight()), "\n"))
		}
	}
	// AND THE FILE LIST IS WHAT GIVES GROUND FIRST on a conflict, never the verb:
	// the sentence that says what happened survives the width the names do not.
	b, _, _ := taskApp(t)
	drive(t, b, streamEventMsg{gen: b.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Merge: mergeWordConflicted, Branch: "task/parser",
			Conflicts: []string{"parser.go", "parser_test.go"}})})
	if said := railSaid(b); !strings.Contains(said, askConflictReason) {
		t.Fatalf("the column stopped naming the clash:\n%s",
			strings.Join(railText(b, b.viewHeight()), "\n"))
	}
}

// railSaid is the whole column as one sentence, with the seam it is drawn
// behind and the wrapping taken out.
func railSaid(a *app) string {
	var out []string
	for _, row := range railText(a, a.viewHeight()) {
		if at := strings.Index(row, railSeam); at >= 0 {
			row = row[at+len(railSeam):]
		}
		out = append(out, row)
	}
	return strings.Join(strings.Fields(strings.Join(out, " ")), " ")
}
