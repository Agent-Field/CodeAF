package tui3

// THE DISCOVERY, REFUSAL AND EMPTY-STATE SIDE OF THE SURFACE — the rows of the
// polish audit's help lane, each pinned by what a person would SEE.
//
// Every test here was written by reverting the fix it covers and watching it
// fail on the words the defect drew, because two tests on this branch have
// already passed against the very defect they claimed to prevent: one matched a
// string that was a prefix of the wrong answer, and one matched source text and
// hit a comment quoting the old shape.

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── ROW 23: `?` ─────────────────────────────────────────────────────────────

// `?` IS THE FIRST KEY A LOST PERSON PRESSES, and it was bound to nothing at
// all: it typed a question mark into the box, and the only route to the key
// sheet was a slash command somebody had to know already.
//
// It answers on both roads and it may never eat a `?` somebody is typing.
func TestTheQuestionMarkOpensHelpAndNeverEatsATypedOne(t *testing.T) {
	// IN A CONVERSATION IT IS THE KEY SHEET.
	a := newTestApp(&fakeAgent{model: "m"})
	drive(t, a, key(helpAskKey))
	said := lastNote(t, a)
	if !strings.Contains(said, helpAskWord) || !strings.Contains(said, "@path") {
		t.Fatalf("? over an empty box drew %q, want the key sheet (its own row reads %q)", said, helpAskWord)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("? over an empty box left %q in the draft", got)
	}

	// AND WITH ANYTHING TYPED IT IS A QUESTION MARK.
	b := newTestApp(&fakeAgent{model: "m"})
	typeInto(t, b, "does this work")
	before := len(b.entries)
	drive(t, b, key(helpAskKey))
	if got := b.input.String(); got != "does this work?" {
		t.Fatalf("? into a sentence left the draft %q — the key ate a character somebody typed", got)
	}
	if len(b.entries) != before {
		t.Fatalf("? into a sentence also printed something: %q", lastNote(t, b))
	}

	// ON A PLACE IT DRAWS THAT PLACE'S OWN MAP.
	c := placeApp(t)
	c.showPage(pageSpend)
	drive(t, c, key(helpAskKey))
	if !c.mapShowing {
		t.Fatalf("? on a place drew no map · the foot says %q", a.placeHint())
	}
	if got := c.placeHint(); !strings.Contains(got, mapCloseWords) {
		t.Fatalf("? on a place left the foot at %q, want the map's own line", got)
	}
	// AND THE NEXT `?` TAKES IT AWAY AGAIN, exactly as the chord does.
	drive(t, c, key(helpAskKey))
	if c.mapShowing {
		t.Fatal("? twice on a place left the map up — the key is a toggle or it is a mode")
	}

	// AND A PLACE'S BOX KEEPS ITS OWN QUESTION MARK.
	d := placeApp(t)
	d.showPage(pageSpend)
	typeInto(t, d, "how much")
	drive(t, d, key(helpAskKey))
	if got := d.compose.String(); got != "how much?" {
		t.Fatalf("? into a place's box left %q — the key ate a character somebody typed", got)
	}
	if d.mapShowing {
		t.Fatal("? into a half-typed sentence opened the map over the top of it")
	}
}

// AND THE KEY IS ADVERTISED WHERE A PERSON WOULD MEET IT — SCREEN 3a's clause
// is that no key does anything that is not drawn, and a key nobody is told
// about is the same defect wearing the other face.
func TestTheQuestionMarkIsAdvertisedOnTheSheetAndTheOpeningLine(t *testing.T) {
	if !strings.Contains(landingKeysWord, helpAskKey+" for help") {
		t.Fatalf("the line every session opens with is %q and never names the help key", landingKeysWord)
	}
	sheet := helpText("", chordSpelling{meta: chordAltWord})
	if !strings.Contains(sheet, helpKeyRow(helpAskKey, helpAskWord)) {
		t.Fatalf("the key sheet has no row for %q:\n%s", helpAskKey, sheet)
	}
}

// ── ROW 16: THE FIRST-RUN REFUSALS ──────────────────────────────────────────

// THE FIRST SCREEN OF A FRESH INSTALL MAY NOT PRINT A GO ERROR. Seven of the
// setup's refusals were `err.Error()`, so the first sentence a new person could
// be shown was a wrapped chain with a path inside aforge's own storage in it
// and no act anywhere.
func TestTheSetupRefusesInSentencesAndNeverInGoErrors(t *testing.T) {
	// THE SIGN-IN THAT NEVER STARTED.
	a, _, _ := setupApp(t, nil)
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) {
		return nil, &fs.PathError{Op: "dial", Path: "/tmp/router.sock", Err: fs.ErrPermission}
	}
	begin := pressSetup(a, key("enter"))
	if begin == nil {
		t.Fatal("enter on the key step started nothing")
	}
	a.Update(begin())
	if a.setup.refusal != setupConnectFailedWord {
		t.Fatalf("a sign-in that would not start said %q, want %q", a.setup.refusal, setupConnectFailedWord)
	}

	// THE BROWSER THAT WOULD NOT OPEN.
	b, _, _ := setupApp(t, nil)
	b.routerConnect = func(context.Context) (OpenRouterFlow, error) {
		return &setupOpenRouterFlow{url: "https://openrouter.example/auth"}, nil
	}
	was := processOpener
	processOpener = func(string) error {
		return &fs.PathError{Op: "fork/exec", Path: "/usr/bin/xdg-open", Err: fs.ErrNotExist}
	}
	t.Cleanup(func() { processOpener = was })
	begin = pressSetup(b, key("enter"))
	// The answer is landed ALONE and the wait it starts is dropped: this
	// assertion is about the line the failed browser leaves, and running the
	// trip on would overwrite it with whatever the flow said next.
	b.Update(begin())
	if b.setup.refusal != setupBrowserWord {
		t.Fatalf("a browser that would not open said %q, want %q", b.setup.refusal, setupBrowserWord)
	}

	// THE TRIP THAT STARTED AND NEVER CAME BACK.
	processOpener = func(string) error { return nil }
	c, _, _ := setupApp(t, nil)
	c.routerConnect = func(context.Context) (OpenRouterFlow, error) {
		return &setupOpenRouterFlow{url: "https://openrouter.example/auth",
			err: &fs.PathError{Op: "read", Path: "/tmp/return.sock", Err: fs.ErrClosed}}, nil
	}
	begin = pressSetup(c, key("enter"))
	_, wait := c.Update(begin())
	if wait == nil {
		t.Fatal("the browser trip did not start waiting")
	}
	c.Update(wait())
	if c.setup.refusal != setupSignInLostWord {
		t.Fatalf("a sign-in that did not come back said %q, want %q", c.setup.refusal, setupSignInLostWord)
	}

	// AND THE ANSWER THAT COULD NOT BE WRITTEN DOWN — a real read-only profile,
	// which is where the operating system's own error came from.
	d, dir, _ := setupApp(t, nil)
	pressSetup(d, key("enter"))
	if d.setup.step() != setupControls {
		t.Fatalf("the fixture is not on the controls step (%v)", d.setup.step())
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("this filesystem cannot be made read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	pressSetup(d, key("enter"))
	if d.setup.refusal != setupSaveFailedWord {
		t.Fatalf("a profile that cannot be written said %q, want %q", d.setup.refusal, setupSaveFailedWord)
	}
	if !d.setup.open || d.setup.step() != setupControls {
		t.Fatal("a control that could not be written walked on anyway")
	}
	for _, machinery := range []string{"/", ":", "config", "denied"} {
		if strings.Contains(d.setup.refusal, machinery) {
			t.Fatalf("the first screen printed a machine's own words (%q) in %q", machinery, d.setup.refusal)
		}
	}
	if screen := setupScreen(d); !strings.Contains(screen, setupSaveFailedWord) {
		t.Fatalf("the refusal never reached the screen:\n%s", screen)
	}
}

// AND A SETTING'S OWN REFUSAL IS NOT REWRITTEN. The registry writes those for a
// person to read — the settings panel draws them as they stand — so the screen
// keeps them and replaces only the operating system's.
func TestTheSetupKeepsASettingsOwnRefusalAboutWhatWasTyped(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	pressSetup(a, key("enter"))
	if a.setup.step() != setupControls {
		t.Fatalf("the fixture is not on the controls step (%v)", a.setup.step())
	}
	pressSetup(a, key("a"), key("b"), key("c"), key("enter"))
	if a.setup.refusal == setupSaveFailedWord || a.setup.refusal == "" {
		t.Fatalf("a figure the setting refused was answered with %q, want the row's own words", a.setup.refusal)
	}
	if !strings.Contains(a.setup.refusal, "dollar amount") {
		t.Fatalf("the limit said %q, want the setting's own refusal about what was typed", a.setup.refusal)
	}
}

// ── ROWS 13 AND 21: THE KEY SHEET'S OWN CHORDS ──────────────────────────────

// THE SHEET IS THE ONE PLACE A PERSON GOES TO LEARN THE KEYS, so a wrong key
// there is worse than a missing one. Two were wrong: the send-and-wait chord
// was spelled `cmd+enter` on every platform — a modifier a Linux keyboard does
// not have — and `ctrl+r` stood on the sheet twice, thirty-six rows apart, with
// two different meanings and neither row saying the other existed.
func TestTheKeySheetSpellsTheSendChordForThisKeyboardAndScopesBothCtrlR(t *testing.T) {
	linux := helpText("", chordSpelling{meta: chordAltWord})
	if !strings.Contains(linux, chordSuperWord+"enter") {
		t.Fatalf("the sheet on a Linux keyboard never says %q:\n%s", chordSuperWord+"enter", keyRows(linux))
	}
	if strings.Contains(linux, chordCmdWord) {
		t.Fatalf("the sheet on a Linux keyboard still names %q, a modifier that keyboard does not have:\n%s",
			chordCmdWord, keyRows(linux))
	}
	mac := helpText("", chordSpelling{meta: chordMetaWord})
	if !strings.Contains(mac, chordCmdGlyph+"enter") {
		t.Fatalf("the sheet on a Mac never says %q:\n%s", chordCmdGlyph+"enter", keyRows(mac))
	}
	if strings.Contains(mac, chordSuperWord) {
		t.Fatalf("the sheet on a Mac says %q, which is not what that keycap reads:\n%s", chordSuperWord, keyRows(mac))
	}

	// AND EACH `ctrl+r` SAYS WHERE IT ACTS.
	var rows []string
	for _, line := range strings.Split(linux, "\n") {
		if strings.HasPrefix(line, spellOutKey) {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("the sheet draws %d rows starting %q, want the two scoped ones:\n%s",
			len(rows), spellOutKey, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "over a draft:") {
		t.Fatalf("the first %q row says no scope: %q", spellOutKey, rows[0])
	}
	if !strings.Contains(rows[1], "in /files:") {
		t.Fatalf("the second %q row says no scope: %q", spellOutKey, rows[1])
	}
}

// ── ROW 14: THE MAP OVER A PLACE WITH NO VERBS ──────────────────────────────

// A KEY DRAWN THAT DOES NOTHING is SCREEN 3a's clause read backwards. The map
// promised `→ verbs on this row` over all seven places, and the search place
// declares no verbs at all — so the arrow it named opened nothing there.
func TestTheMapNamesTheRowVerbsOnlyWhereTheRowHasThem(t *testing.T) {
	a := placeApp(t)
	a.showPage(pageSearch)
	drive(t, a, key(placeMapKey))
	if got := a.placeHint(); strings.Contains(got, placeMapVerbWords) {
		t.Fatalf("the map over search promises %q with nothing to open: %q", placeMapVerbWords, got)
	} else if !strings.Contains(got, mapCloseWords) || !strings.Contains(got, chordJumpWords) {
		t.Fatalf("the map over search lost the clauses that are true there: %q", got)
	}

	// AND IT KEEPS THE CLAUSE WHERE THE ROW REALLY HAS VERBS.
	b := placeApp(t)
	b.showPage(pageTasks)
	if len((placeTasks{}).verbs(b)) == 0 {
		t.Skip("this fixture's tasks page has no row with verbs to stand on")
	}
	drive(t, b, key(placeMapKey))
	if got := b.placeHint(); !strings.Contains(got, placeMapVerbWords) {
		t.Fatalf("the map over a row with verbs dropped %q: %q", placeMapVerbWords, got)
	}
}

// ── ROWS 11, 12 AND 18: THE SEARCH PLACE'S OWN BODY ─────────────────────────

// THE TEACHING PROSE WRAPS, AND EVERY ROW HANGS FROM THE BODY'S OWN COLUMN.
// Both sentences went through the character ruler, so at eighty columns the
// third one drew `…at the matching t…` and its other half was gone; and the
// rows started at column 1 where tasks, standing and spend all start at 2, so
// walking the tab bar the body stepped sideways.
func TestTheSearchTeachingWrapsAndHangsFromTheBodysColumn(t *testing.T) {
	pal := newPalette(tokens.NoColor, false)
	hits, world := searchFixture()
	for _, width := range []int{60, 80, 120, 160} {
		for _, r := range []searchReading{
			readSearch("", nil, session.World{}, searchTestNow),
			readSearch("amber rail", nil, session.World{}, searchTestNow),
			readSearch("report", hits, world, searchTestNow),
		} {
			for i, row := range r.rows(width, pal) {
				text := ansi.Strip(row)
				if got := ansi.StringWidth(text); got > width {
					t.Fatalf("row %d drew %d cells at %d columns: %q", i, got, width, text)
				}
				if strings.TrimSpace(text) == "" {
					continue
				}
				if !strings.HasPrefix(text, " ") || strings.HasPrefix(text, "  ") {
					t.Fatalf("row %d at %d columns hangs from the wrong column: %q", i, width, text)
				}
				if strings.HasSuffix(strings.TrimRight(text, " "), "…") {
					t.Fatalf("a sentence was cut instead of wrapped at %d columns: %q", width, text)
				}
			}
		}
	}

	// AND THE LONGEST SENTENCE SURVIVES WHOLE ACROSS THE LINES IT TOOK.
	page := searchSaid(readSearch("", nil, session.World{}, searchTestNow), 80)
	if !strings.Contains(page, searchExampleWord) {
		t.Fatalf("the teaching page lost the one line with a verb in it at 80 columns:\n%s", page)
	}
	for _, line := range searchTeachWords {
		if !strings.Contains(page, line) {
			t.Fatalf("the teaching page lost %q at 80 columns:\n%s", line, page)
		}
	}
}

// A SEARCH THAT FINDS NOTHING SAYS WHAT TO DO, and a window with no index
// behind it says which of the two silences this one is. Without the second
// sentence "nobody has said that" and "nothing looked" are the same line.
func TestASearchThatFindsNothingSaysWhatToDoAndAMissingIndexSaysSo(t *testing.T) {
	none := searchSaid(readSearch("amber rail", nil, session.World{}, searchTestNow), 120)
	if !strings.Contains(none, `nothing on this machine says "amber rail"`) {
		t.Fatalf("the empty result stopped naming the words that found nothing: %q", none)
	}
	if !strings.Contains(none, "try fewer words") {
		t.Fatalf("the empty result names no next act: %q", none)
	}

	a := placeApp(t)
	a.searchArm = func(int) tea.Cmd { return nil }
	a.searchStore = nil
	a.showPage(pageSearch)
	typeInto(t, a, "report")
	if cmd := a.searchTick(searchTickMsg{gen: a.search.ask.gen}); cmd != nil {
		t.Fatal("a surface with no index sent a read anyway")
	}
	page := plain(placeFrameText(a))
	if !strings.Contains(page, "no index of this machine's conversations") {
		t.Fatalf("a window with no index behind it does not say so:\n%s", page)
	}
	if strings.Contains(page, `nothing on this machine says "report"`) {
		t.Fatalf("a search that never happened reported a result:\n%s", page)
	}
}

// ── ROW 7: THE MANUAL LISTING ───────────────────────────────────────────────

// A LISTING THAT SHOWS A PAGE THAT DOES NOT EXIST is worse than one that shows
// fewer pages. The listing is one line per page — the name, then the title —
// and an ordinary note RE-FLOWS its text, so a long title wrapped and its last
// word landed at the column the page names are in: `/manual` drew a page called
// `later`, and `/manual later` then answered that there is no such page.
func TestTheManualListingNeverInventsAPage(t *testing.T) {
	pages := map[string]bool{}
	for _, name := range manual.Chat().Pages() {
		pages[name] = true
	}
	for _, width := range []int{60, 80, 120} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.width, a.height = width, 40
		a.railAway = true
		typeLine(t, a, "/manual")
		for _, row := range noticeBlockRows(a, len(a.entries)-1, a.bodyWidth()) {
			// The note's own `· ` marker and the indent law's gutter come off
			// first: what is left is the row as the listing built it, and its
			// first word must be a page.
			said := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(row), "·"))
			name, _, _ := strings.Cut(said, " ")
			if name == "" {
				continue
			}
			if !pages[name] {
				t.Fatalf("the listing at %d columns drew a row whose first word is %q, "+
					"which is not a page — /manual %s answers that there is no such page:\n%s",
					width, name, name, row)
			}
		}
	}
}

// ── ROW 20: THE ENTRY NOTE ──────────────────────────────────────────────────

// A RESUMED CONVERSATION OPENS BY SAYING WHICH CONVERSATION IT IS, on BOTH
// doors. The launch line was fixed one wave ago and the picker's own road was
// not, so opening a conversation from the greeting still put four wrapped rows
// of absolute transcript path above the person's first message.
func TestOpeningAConversationFromThePickerNamesItAndNotItsPath(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	a.tilde = "/home/dev"
	a.open = func(_, transcript string) (Conversation, error) {
		return Conversation{Agent: &wiredAgent{fakeAgent: &fakeAgent{model: "m"}, name: "porting the picker"},
			SessionFile: transcript, Workspace: "/tmp/lab", Resumed: true}, nil
	}
	if _, refusal := a.openSession(Session{File: journalUnderHome}); refusal != "" {
		t.Fatalf("the resume refused: %s", refusal)
	}
	said := lastNote(t, a)
	if said != resumedWord+" · porting the picker" {
		t.Fatalf("the picker's road opened with %q, want %q", said, resumedWord+" · porting the picker")
	}
	if strings.Contains(said, "/projects/") {
		t.Fatalf("the opening line is still a transcript path: %q", said)
	}
	if rows := noticeBlockRows(a, len(a.entries)-1, a.bodyWidth()); len(rows) != 1 {
		t.Fatalf("the opening line took %d rows at %d columns:\n%s", len(rows), a.bodyWidth(), strings.Join(rows, "\n"))
	}
}

// ── ROW 22: THE UNKNOWN COMMAND ─────────────────────────────────────────────

// EVERY OTHER REFUSAL ON THIS SURFACE IS A SENTENCE ABOUT THE WORLD, and this
// one led with a compiler's noun and a colon. It kept the half that mattered —
// what to do — and now it points at the list itself rather than at a second
// command somebody has to type correctly.
func TestAnUnknownCommandIsRefusedInASentenceAndPointsAtTheList(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	typeLine(t, a, "/nosuchthing")
	said := lastNote(t, a)
	if said != "there is no command called /nosuchthing · "+unknownCommandDoorWord {
		t.Fatalf("an unknown command answered %q", said)
	}
	if strings.Contains(said, "unknown command") || strings.Contains(said, ":") {
		t.Fatalf("the refusal is still written in machinery register: %q", said)
	}
	if strings.Contains(said, "try /help") {
		t.Fatalf("the refusal still points at a second command to type: %q", said)
	}
}

// ── the readings these tests share ──────────────────────────────────────────

// keyRows is the key sheet's chord block alone — the rows below the commands —
// so a failure prints the part of it the assertion was about.
func keyRows(sheet string) string {
	var out []string
	for _, line := range strings.Split(sheet, "\n") {
		if line != "" && !strings.HasPrefix(line, "/") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// searchSaid is the search place's body as one block of words.
func searchSaid(r searchReading, width int) string {
	var out []string
	for _, row := range r.rows(width, newPalette(tokens.NoColor, false)) {
		out = append(out, strings.TrimSpace(ansi.Strip(row)))
	}
	return strings.Join(out, "\n")
}
