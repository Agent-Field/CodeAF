package homes

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The page's own gate. It asserts the SHAPE of the surface — the strip, the
// order, the grid, the doors, the tiers — rather than an exact frame, because a
// golden of a page this size would fail on every wording change and teach
// nothing about the laws it is supposed to hold.

// frame renders the page at a size and hands back both the lines and the page,
// so a test can assert about the picture and about the click map that came from
// the same act.
func page(t *testing.T, st *tokens.Styler, state State, sel Selection, width, height int) (*Page, []string) {
	t.Helper()
	p := NewPage(st)
	return p, p.Render(state, sel, width, height)
}

// tab renders one section of the page. The first render is what SEEDS the page
// from the selection ([Page.syncSection]); only after that does an explicit
// switch mean anything, which is the same order a live host runs in — a frame,
// then a keystroke.
func tab(t *testing.T, st *tokens.Styler, state State, sel Selection, s Section, width, height int) (*Page, []string) {
	t.Helper()
	p := NewPage(st)
	p.Render(state, sel, width, height)
	p.SetSection(s)
	return p, p.Render(state, sel, width, height)
}

// body is the frame under the strip and the blank line after it.
func body(lines []string) []string {
	if len(lines) < 2 {
		return nil
	}
	return lines[2:]
}

// -- anatomy -----------------------------------------------------------------

// THE SECTIONS ARE TABS (user-reported 2026-08-11): the strip carries all three
// words in a fixed order, and the BODY carries exactly one of them at a time.
// The user's report of the first live build was "there is just a long list of
// everything … in same page instead of separate tabs even though we have tab at
// top", and this is that sentence as an assertion.
func TestTheSectionsAreTabsAndOnlyOneIsOnScreen(t *testing.T) {
	state := rich()
	// One phrase that can only come from each tab, so "which tab is on screen"
	// is answered by the content and not by a header.
	// Short enough to survive the narrowest pane's ellipsis, since a row is one
	// line and cuts at its measure (§19).
	marks := map[Section]string{
		SectionBeliefs:  "santosh",
		SectionKnowhow:  "release-notes",
		SectionPractice: "how flaky",
	}
	for _, width := range []int{40, 80, 120} {
		for _, on := range Sections() {
			p, lines := tab(t, nil, state, Selection{Home: HomeNotebook}, on, width, 80)
			if len(lines) == 0 {
				t.Fatalf("w=%d %v: nothing rendered", width, on)
			}
			strip := lines[0]
			for _, s := range Sections() {
				if !strings.Contains(strip, s.Word()) {
					t.Fatalf("w=%d: strip %q is missing %q", width, strip, s.Word())
				}
			}
			if !inOrder(strip, "beliefs", "know-how", "practice") {
				t.Fatalf("w=%d: strip out of order: %q", width, strip)
			}
			if got := p.Section(); got != on {
				t.Fatalf("w=%d: switched to %v, page says %v", width, on, got)
			}
			out := strings.Join(body(lines), "\n")
			for s, mark := range marks {
				if s == on {
					if !strings.Contains(out, mark) {
						t.Fatalf("w=%d %v: its own tab is missing %q:\n%s", width, on, mark, out)
					}
					continue
				}
				if strings.Contains(out, mark) {
					t.Fatalf("w=%d %v: another tab's row (%q) is on screen:\n%s", width, on, mark, out)
				}
			}
			// The band word is gone from the body: the strip a line up already
			// says which section this is, and §19 forbids saying it twice.
			for i, line := range body(lines) {
				if strings.TrimSpace(line) == on.Word() {
					t.Fatalf("w=%d %v: body line %d repeats the strip's word", width, on, i)
				}
			}
		}
	}
}

// EVERY strip word switches its tab, at every width and from every tab — the
// direct regression for "I can't click know-how".
//
// The old page answered a strip click by SCROLLING to the band's first line.
// know-how's first line sits inside the last screenful (beliefs is hundreds of
// rows, know-how and practice together are a dozen), so the clamp pinned the
// jump to MaxScroll and the derived Section() read practice off the bottom of
// the document. It was the one word that could fail, and it failed always.
func TestEveryStripWordSwitchesItsTabAtEveryWidth(t *testing.T) {
	state := rich()
	// The shape the defect needs: a long first band and two short ones.
	for i := 0; i < 60; i++ {
		id := "x" + strconv.Itoa(i)
		state.Notebook.Beliefs = append(state.Notebook.Beliefs,
			Belief{ID: id, Body: "belief " + id, Scope: "user"})
	}
	sel := Selection{Home: HomeNotebook}
	for width := 40; width <= 140; width += 4 {
		for _, height := range []int{12, 24, 40} {
			for _, from := range Sections() {
				for _, to := range Sections() {
					p, _ := tab(t, nil, state, sel, from, width, height)
					target := stripTarget(t, p, to)
					// EVERY cell of the word answers, not just its first.
					for _, x := range []int{target.From, (target.From + target.To) / 2, target.To - 1} {
						p.SetSection(from)
						p.Render(state, sel, width, height)
						if _, ok := p.Click(x, target.Line); !ok {
							t.Fatalf("w=%d h=%d %v→%v: cell %d answered nothing", width, height, from, to, x)
						}
						p.Render(state, sel, width, height)
						if got := p.Section(); got != to {
							t.Fatalf("w=%d h=%d: clicking %v from %v landed on %v", width, height, to, from, got)
						}
					}
				}
			}
		}
	}
}

// A tab is a place, so it remembers where it was left. A reader who scrolled
// forty beliefs down, looked at know-how and came back has not asked to start
// again at the top.
func TestEachTabRemembersItsOwnScroll(t *testing.T) {
	state := wide(60)
	sel := Selection{Home: HomeNotebook}
	const width, height = 80, 12
	p, _ := tab(t, nil, state, sel, SectionBeliefs, width, height)
	p.ScrollBy(20)
	p.Render(state, sel, width, height)
	deep := p.Scroll()
	if deep == 0 {
		t.Fatal("the fixture did not scroll")
	}
	p.SetSection(SectionKnowhow)
	p.Render(state, sel, width, height)
	if p.Scroll() != 0 {
		t.Fatalf("a freshly opened tab started at %d, not at its top", p.Scroll())
	}
	p.ScrollBy(5)
	p.Render(state, sel, width, height)
	knowhow := p.Scroll()
	p.SetSection(SectionBeliefs)
	p.Render(state, sel, width, height)
	if p.Scroll() != deep {
		t.Fatalf("beliefs came back at %d, not at %d", p.Scroll(), deep)
	}
	p.SetSection(SectionKnowhow)
	p.Render(state, sel, width, height)
	if p.Scroll() != knowhow {
		t.Fatalf("know-how came back at %d, not at %d", p.Scroll(), knowhow)
	}
}

// The strip counts what is worth counting and nothing else: a window with a
// ceiling says so, know-how counts both of its lists as one, an empty collection
// shows nothing rather than a zero (§16's EMPTINESS), and practice never carries
// a number at all — "how many things did I drill" is a number about nothing.
func TestTheStripCountsOnlyWhereACountIsAFact(t *testing.T) {
	state := rich()
	_, lines := page(t, nil, state, Selection{Home: HomeNotebook}, 80, 80)
	strip := lines[0]
	knowhow := len(state.Knowhow.Crafts) + len(state.Knowhow.Skills)
	for _, want := range []string{"beliefs 500+", "know-how " + strconv.Itoa(knowhow)} {
		if !strings.Contains(strip, want) {
			t.Fatalf("strip %q is missing %q", strip, want)
		}
	}
	if i := strings.Index(strip, "practice"); i >= 0 {
		rest := strip[i+len("practice"):]
		if len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9' {
			t.Fatalf("practice wears a count: %q", strip)
		}
	}

	_, empty := page(t, nil, State{}, Selection{Home: HomeNotebook}, 80, 60)
	strip = empty[0]
	for _, s := range Sections() {
		if !strings.Contains(strip, s.Word()) {
			t.Fatalf("empty strip lost %q: %q", s.Word(), strip)
		}
	}
	if strings.ContainsAny(strip, "0123456789") {
		t.Fatalf("an empty page counted zeroes: %q", strip)
	}
}

// The tab keys are the ordinals, they resolve in draw order, and pressing one
// is the same act as clicking its word.
func TestTheTabKeysAreTheOrdinals(t *testing.T) {
	state, sel := rich(), Selection{Home: HomeNotebook}
	const width, height = 80, 10
	p := NewPage(nil)
	p.Render(state, sel, width, height)
	for i, s := range Sections() {
		got, ok := SectionForKey(s.Key())
		if !ok || got != s {
			t.Fatalf("key %q resolved to %v/%v", s.Key(), got, ok)
		}
		if s.Key() != string(rune('1'+i)) {
			t.Fatalf("%v carries key %q, not the ordinal", s, s.Key())
		}
		p.JumpTo(s)
		p.Render(state, sel, width, height)
		if p.Section() != s {
			t.Fatalf("key %q landed on %v", s.Key(), p.Section())
		}
	}
	if _, ok := SectionForKey("b"); ok {
		t.Fatal("an initial resolved to a section; the keys are ordinals")
	}
	if _, ok := SectionForKey("4"); ok {
		t.Fatal("a fourth key still resolves; there are three tabs")
	}
	// A row id names its own tab, so a cursor arriving from the rail opens the
	// section the row is in rather than whichever one was last on screen.
	for _, c := range []struct {
		row  string
		want Section
	}{
		{BeliefRowPrefix + "41", SectionBeliefs},
		{CraftRowPrefix + "release-notes", SectionKnowhow},
		{SkillRowPrefix + "sk1", SectionKnowhow},
		{QuestionRowPrefix + "q1", SectionPractice},
	} {
		got, ok := SectionForRow(c.row)
		if !ok || got != c.want {
			t.Fatalf("%q maps to %v/%v, not %v", c.row, got, ok, c.want)
		}
	}
	if _, ok := SectionForRow("self/dials"); ok {
		t.Fatal("a row this page never draws claimed a tab")
	}
}

// -- beliefs -----------------------------------------------------------------

// The belief band draws four classes of row on one grid, and each says which
// class it is with a WORD in its receipt rather than with a header: a plain
// belief leads with its scope, taste says taste, a trait says how many samples
// are behind it, a playbook says playbook.
func TestTheBeliefBandNamesEachClassInItsReceipt(t *testing.T) {
	_, lines := tab(t, nil, rich(), Selection{Home: HomeNotebook}, SectionBeliefs, 110, 80)
	out := strings.Join(lines, "\n")
	// §19's receipt grammar, one order for every class: KIND · SCOPE · STATUS ·
	// AGE. A plain belief has no kind word and leads with its scope.
	for _, want := range []string{
		"(user · strong",
		"you keep correcting: shorter commit lines",
		"(taste · forming",
		"measured: you usually accept first drafts",
		"(trait · 14 samples",
		"playbook · repo:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	// A trait is MEASURED, and there is no verb for disagreeing with a
	// measurement (notebook-split.md §3): the row draws none even under the
	// cursor.
	trait := Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "6"}
	p, traitLines := tab(t, nil, rich(), trait, SectionBeliefs, 100, 80)
	for _, tg := range p.Targets() {
		if tg.Kind == TargetVerb && tg.Row == trait.Row {
			t.Fatalf("a trait row grew a verb: %+v", tg)
		}
	}
	if strings.Contains(strings.Join(traitLines, "\n"), "forget") {
		t.Fatalf("a trait row drew forget:\n%s", strings.Join(traitLines, "\n"))
	}
}

// The row the cursor is on carries its verbs — present in the frame AND
// answering a click, through the same target table the picture came from. The
// ids are notebook-split.md §4's, and the WORD is never the id (5.14).
func TestBeliefVerbsAreDrawnAndClickable(t *testing.T) {
	state := rich()
	sel := Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "41"}
	p, lines := tab(t, nil, state, sel, SectionBeliefs, 100, 80)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"forget", "edit", "user · strong"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, "belief.") {
		t.Fatalf("a registry id reached the frame:\n%s", out)
	}
	found := map[string]Target{}
	for _, tg := range p.Targets() {
		if tg.Kind == TargetVerb && tg.Row == sel.Row {
			found[tg.ID] = tg
		}
	}
	for _, id := range BeliefVerbs {
		tg, ok := found[id]
		if !ok {
			t.Fatalf("no target for belief verb %q: %+v", id, p.Targets())
		}
		// Every cell of the chip answers, and it answers with the verb rather
		// than with the row it sits on.
		for x := tg.From; x < tg.To; x++ {
			got, ok := p.TargetAt(x, tg.Line)
			if !ok || got.ID != id || got.Kind != TargetVerb {
				t.Fatalf("cell %d of %q answered %+v/%v", x, id, got, ok)
			}
		}
	}
	// A registry-fed verb wins over the fallback, and it brings its key.
	state.Notebook.Beliefs[0].Verbs = []Verb{{ID: "belief.edit", Label: "edit", Key: "e"}}
	_, lines = tab(t, nil, state, sel, SectionBeliefs, 100, 80)
	if out = strings.Join(lines, "\n"); !strings.Contains(out, "edit e") {
		t.Fatalf("a registry verb lost its key:\n%s", out)
	}
	if strings.Contains(out, "forget "+strings.TrimSpace(sepRun)+" edit") {
		t.Fatalf("the fallback survived beside a registry strip:\n%s", out)
	}
}

// The ids every lane of this wave agreed on (notebook-split.md §4), spelled
// once, here, so a rename shows up as a failing test rather than as a verb that
// silently stops firing.
func TestTheVerbIdsAreTheWaveIds(t *testing.T) {
	for _, c := range []struct {
		ids  []string
		want []string
	}{
		{BeliefVerbs, []string{"belief.forget", "belief.edit"}},
		{CraftVerbs, []string{"craft.run", "craft.revert", "craft.retire"}},
		{SkillVerbs, []string{"skill.retire"}},
	} {
		if len(c.ids) != len(c.want) {
			t.Fatalf("%v is not %v", c.ids, c.want)
		}
		for i := range c.want {
			if c.ids[i] != c.want[i] {
				t.Fatalf("verb %d is %q, not %q", i, c.ids[i], c.want[i])
			}
			if word := VerbWord(c.ids[i]); word == "" || strings.Contains(word, ".") {
				t.Fatalf("%q draws as %q", c.ids[i], word)
			}
		}
	}
	if got := VerbWord("retire"); got != "retire" {
		t.Fatalf("an id with no namespace drew %q", got)
	}
}

// A visitor window may look and may not act: the verbs keep their place and the
// strip spends its tail on the reason, once (5.20 rule 3).
func TestAVisitorSeesTheVerbsAndTheReasonOnce(t *testing.T) {
	state := rich()
	state.Visitor = "visitor window — only the resident may act"
	sel := Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "41"}
	_, lines := tab(t, nil, state, sel, SectionBeliefs, 100, 80)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"forget", "edit", "visitor window"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "visitor window"); n != 1 {
		t.Fatalf("reason repeated %d times:\n%s", n, out)
	}
}

// -- know-how ----------------------------------------------------------------

// The know-how band chip-tags both of its lists and carries the receipt each
// kind deserves: a workflow's survival record, its cost per run, its version and
// its age; a tool's uses and its age.
func TestKnowhowChipsAndReceipts(t *testing.T) {
	// 130 cells: the receipt column may claim at most half a pane ([receiptShare]),
	// and a workflow with a full survival record has the longest receipt on the
	// page. Narrower panes truncate it, which the width sweep walks.
	_, lines := tab(t, nil, rich(), Selection{Home: HomeSelf}, SectionKnowhow, 130, 80)
	out := strings.Join(lines, "\n")
	for _, want := range []string{
		"release-notes", "workflow · proved 4 runs against 1", "$0.31/run", "v3",
		"fetch-pr-context", "workflow · draft, never run",
		"imgshrink", "tool · 11 uses",
		"pdfsplit", "tool · retired",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	// A workflow that never ran says so in words and NEVER as a zero — "proved
	// 0" would read as a workflow that keeps losing.
	if strings.Contains(out, "proved 0") {
		t.Fatalf("a draft was reported as a loser:\n%s", out)
	}
	if strings.Contains(out, "$0.00") {
		t.Fatalf("an unmeasured run cost rendered as free:\n%s", out)
	}
	// The multiplication sign is East_Asian_Width=Ambiguous and is deliberately
	// not spelled here (see [proved]).
	if strings.Contains(out, "×") {
		t.Fatalf("an ambiguous-width glyph reached a receipt:\n%s", out)
	}
}

// A workflow's verbs are §4's, drawn on the row the cursor is on, and they
// answer a click without the page running any of them (5.22).
func TestCraftAndSkillVerbsComeBackToTheHost(t *testing.T) {
	state := rich()
	state.Knowhow.Crafts[0].Verbs = nil
	state.Knowhow.Skills[0].Verbs = nil
	for _, c := range []struct {
		row  string
		want []string
	}{
		{CraftRowPrefix + "release-notes", CraftVerbs},
		{SkillRowPrefix + "sk1", SkillVerbs},
	} {
		sel := Selection{Home: HomeSelf, Row: c.row}
		p, lines := tab(t, nil, state, sel, SectionKnowhow, 100, 80)
		out := strings.Join(lines, "\n")
		for _, id := range c.want {
			if !strings.Contains(out, VerbWord(id)) {
				t.Fatalf("%q missing from:\n%s", VerbWord(id), out)
			}
			hit := Target{}
			for _, tg := range p.Targets() {
				if tg.Kind == TargetVerb && tg.ID == id && tg.Row == c.row {
					hit = tg
				}
			}
			if hit.ID == "" {
				t.Fatalf("no target for %q: %+v", id, p.Targets())
			}
			got, ok := p.Click(hit.From, hit.Line)
			if !ok || got.ID != id || got.Kind != TargetVerb {
				t.Fatalf("clicking %q answered %+v/%v", id, got, ok)
			}
			// The page applied nothing: a click on a verb is not a drill.
			if _, drilled := p.Detail(); drilled {
				t.Fatalf("clicking the verb %q opened a detail", id)
			}
		}
		if strings.Contains(out, "craft.") || strings.Contains(out, "skill.") {
			t.Fatalf("a registry id reached the frame:\n%s", out)
		}
	}
}

// -- practice ----------------------------------------------------------------

// The practice band carries a question's lifecycle, the competence line and the
// day receipt — and says NOTHING on a day that was asleep, rather than $0.00.
func TestPracticeCarriesItsReceipts(t *testing.T) {
	_, lines := tab(t, nil, rich(), Selection{Home: HomeSelf}, SectionPractice, 100, 80)
	out := strings.Join(lines, "\n")
	for _, want := range []string{
		"how flaky is the e2e suite", "practicing · 2 runs · $0.12",
		"uv beats pip in this repo", "resolved · 1 run",
		"why does the CJK test flap", "open",
		"strongest repo:aforge-v2 · frontier tool:docker",
		"today $8.65 · 42m practiced · 3 learned",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	// tokens.Duration's fixed-width cell belongs to a live column, not to a
	// receipt on a page (reltime's package comment draws that line).
	if strings.Contains(out, "42m00s") {
		t.Fatalf("a receipt wore the live column's spelling:\n%s", out)
	}

	quiet := rich()
	quiet.Practice.Today = Today{}
	quiet.Practice.Competence = Competence{}
	quiet.Practice.Questions[0].HasCost = false
	_, lines = tab(t, nil, quiet, Selection{Home: HomeSelf}, SectionPractice, 100, 80)
	out = strings.Join(lines, "\n")
	for _, bad := range []string{"$0.00", "0 learned", "today ", "strongest"} {
		if strings.Contains(out, bad) {
			t.Fatalf("an empty day was reported (%q):\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "practice") {
		t.Fatalf("the strip lost its word on an empty day:\n%s", out)
	}
}

// -- the door ----------------------------------------------------------------

// §10's whole-line door, drilling: every cell of a row answers, and a click
// anywhere on it opens that row's document (§3).
func TestTheWholeLineIsTheDoor(t *testing.T) {
	state, sel := rich(), Selection{Home: HomeNotebook}
	const width = 90
	p := NewPage(nil)
	p.Render(state, sel, width, 80)
	rowID := BeliefRowPrefix + "41"
	door := Target{}
	for _, tg := range p.Targets() {
		if tg.Kind == TargetDoor && tg.ID == rowID {
			door = tg
		}
	}
	if door.ID == "" {
		t.Fatalf("no door for %q: %+v", rowID, p.Targets())
	}
	if door.From != 0 || door.To != width {
		t.Fatalf("the door is %d..%d, not the whole line", door.From, door.To)
	}
	for _, x := range []int{0, 1, width / 2, width - 1} {
		got, ok := p.TargetAt(x, door.Line)
		if !ok || got.ID != rowID {
			t.Fatalf("cell %d of the door answered %+v/%v", x, got, ok)
		}
	}
	if _, ok := p.Click(width/2, door.Line); !ok {
		t.Fatal("a click on the line did nothing")
	}
	if got, drilled := p.Detail(); !drilled || got != rowID {
		t.Fatalf("the click opened %q/%v", got, drilled)
	}
	// EVERY row of EVERY tab is a door.
	p.Back()
	doors := map[string]bool{}
	for _, section := range Sections() {
		p.SetSection(section)
		p.Render(state, sel, width, 200)
		for _, tg := range p.Targets() {
			if tg.Kind == TargetDoor {
				doors[tg.ID] = true
			}
		}
	}
	for _, row := range pageRows(state) {
		if !doors[row] {
			t.Fatalf("%q is not a door: %+v", row, doors)
		}
	}
}

// -- ink ---------------------------------------------------------------------

// §16's DIM RAMP: three text tiers on the surface and no fourth. The assertion
// is against the ESCAPES in the frame rather than against the source, because a
// fourth tier arrives as a token somebody reached for and the frame is where it
// shows up.
//
// Colour beyond the ramp is allowed in exactly two places and both are checked:
// the state glyph's hue, and the strip's underline colour.
func TestThePageDrawsThreeTiersAndNoFourth(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	state := rich()
	p := NewPage(st)
	frames := []string{
		strings.Join(p.Render(state, Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "41"}, 100, 80), "\n"),
	}
	// The drill's frames are walked too: a detail page is the same three tiers.
	for _, row := range pageRows(state) {
		p.Enter(row)
		frames = append(frames, strings.Join(p.Render(state, Selection{Home: HomeNotebook}, 100, 80), "\n"))
		p.Back()
	}

	allowed := map[string]string{}
	for _, tok := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary} {
		allowed[tok.Fg(tokens.TrueColor, tokens.FocusNormal)] = "tier"
	}
	for _, tok := range []tokens.Token{tokens.Amber, tokens.Cyan, tokens.Green, tokens.Coral} {
		allowed[tok.Fg(tokens.TrueColor, tokens.FocusNormal)] = "hue"
	}
	allowed[stripRule.UnderlineColor(tokens.TrueColor, tokens.FocusNormal)] = "rule"
	for _, seq := range []string{
		tokens.ResetFg(tokens.TrueColor), tokens.Reset(tokens.TrueColor),
		tokens.Underline(tokens.TrueColor), tokens.UnderlineOff(tokens.TrueColor),
		tokens.UnderlineColorOff(tokens.TrueColor),
	} {
		allowed[seq] = "chrome"
	}

	for _, frame := range frames {
		tiers := map[string]bool{}
		for _, seq := range escapes(frame) {
			kind, ok := allowed[seq]
			if !ok {
				t.Fatalf("the page drew an escape outside the ramp: %q", seq)
			}
			if kind == "tier" {
				tiers[seq] = true
			}
		}
		if len(tiers) > 3 {
			t.Fatalf("a fourth text tier reached the frame: %d tiers", len(tiers))
		}
		// No grounds at this level: §16 gives the product exactly one border and
		// the strip is not it, and the coordinator's decision spends no pill here.
		if strings.Contains(frame, "\x1b[48;") || strings.Contains(frame, tokens.Reverse(tokens.TrueColor)) {
			t.Fatalf("the page painted a ground:\n%q", frame)
		}
	}
}

// The current band is marked with a RULE, quieter than the tabs above it: the
// word goes bright and takes an underline in the accent colour, everything else
// stays a dim word with its count.
//
// It degrades in one step and never two. A profile with no SGR 58 keeps the
// plain underline, which is the widely supported half; the no-colour profile
// draws no attribute at all, because its promise is no escapes.
func TestTheCurrentSectionWearsARuleAndNotAGround(t *testing.T) {
	state, sel := rich(), Selection{Home: HomeNotebook}
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256, tokens.ANSI16} {
		st := tokens.NewStyler(profile, tokens.FocusNormal)
		p, lines := page(t, st, state, sel, 80, 80)
		strip := lines[0]
		if !strings.Contains(strip, tokens.Underline(profile)) {
			t.Fatalf("%v: the current word carries no rule: %q", profile, strip)
		}
		if !strings.Contains(strip, tokens.UnderlineOff(profile)) {
			t.Fatalf("%v: the rule is never closed: %q", profile, strip)
		}
		if strings.Contains(strip, "\x1b[48;") {
			t.Fatalf("%v: the strip painted a ground: %q", profile, strip)
		}
		colour := stripRule.UnderlineColor(profile, tokens.FocusNormal)
		switch profile {
		case tokens.ANSI16:
			if colour != "" {
				t.Fatalf("16 colours grew an underline colour: %q", colour)
			}
		default:
			if colour == "" || !strings.Contains(strip, colour) {
				t.Fatalf("%v: the rule is not the accent: %q", profile, strip)
			}
		}
		if got := p.Section(); got != SectionBeliefs {
			t.Fatalf("%v: the bright word is %v, not where the body is", profile, got)
		}
	}
	// NoColor emits no attribute at all — that profile promises no escapes, and
	// its consumers are a dumb pipe and a golden file.
	_, plain := page(t, tokens.NewStyler(tokens.NoColor, tokens.FocusNormal), state, sel, 80, 80)
	for i, line := range plain {
		if strings.ContainsRune(line, 0x1b) {
			t.Fatalf("the no-colour profile emitted an escape on line %d: %q", i, line)
		}
	}
	_, nilStyled := page(t, nil, state, sel, 80, 80)
	for i, line := range nilStyled {
		if strings.ContainsRune(line, 0x1b) {
			t.Fatalf("a nil styler emitted an escape on line %d: %q", i, line)
		}
	}
}

// §16's HOVER: pointer rest promotes the run one tier, never a band, and
// everything that hovers is clickable.
func TestHoverPromotesOneTier(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	state, sel := rich(), Selection{Home: HomeNotebook}
	p := NewPage(st)
	p.Render(state, sel, 80, 80)

	var word Target
	for _, tg := range p.Targets() {
		if tg.Kind == TargetSection && tg.ID == SectionPractice.TargetID() {
			word = tg
		}
	}
	if word.ID == "" {
		t.Fatal("no strip target to hover")
	}
	if !p.SetHover(word.From, word.Line) {
		t.Fatal("hovering a target moved nothing")
	}
	if p.SetHover(word.From, word.Line) {
		t.Fatal("hovering the same target twice asked for a repaint")
	}
	strip := p.Render(state, sel, 80, 80)[0]
	dim := tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal)
	promoted := tokens.Promote(tokens.TextTertiary).Fg(tokens.TrueColor, tokens.FocusNormal)
	if !strings.Contains(strip, promoted) {
		t.Fatalf("hover did not promote the word: %q", strip)
	}
	if !p.ClearHover() {
		t.Fatal("clearing a live hover moved nothing")
	}
	if p.ClearHover() {
		t.Fatal("clearing an empty hover asked for a repaint")
	}
	strip = p.Render(state, sel, 80, 80)[0]
	if !strings.Contains(strip, dim) {
		t.Fatalf("the word did not return to its tier: %q", strip)
	}
	// A pointer on nothing is a cleared hover, not a kept one.
	p.SetHover(word.From, word.Line)
	if !p.SetHover(0, 1) {
		t.Fatal("moving the pointer off every target kept the old one lit")
	}
}

// -- the shipping gate -------------------------------------------------------

// At every width from 1 to 140, on every state this package can be handed, in
// the list AND in every detail page it can drill into, a render produces at most
// the rows it was given room for, never a row wider than the pane, never a
// newline inside a row, and never a panic.
func TestPageNeverOverflowsAndNeverPanics(t *testing.T) {
	states := map[string]State{
		"rich":    rich(),
		"hostile": hostile(),
		"empty":   {},
		"wide":    wide(60),
	}
	heights := []int{1, 2, 3, 7, 24, 60}
	for name, state := range states {
		// A REPRESENTATIVE selection set rather than every row (which is what
		// the detail-pane sweep walks, because there a selection chooses the
		// whole picture). The page draws every row of every band whatever the
		// cursor is on; the only thing a selection changes is where it scrolled
		// and which row wears its verbs, and three per band exercises both. The
		// full set is walked in the styler sweep, at nine widths instead of 140.
		for _, sel := range pageSelections(state) {
			for width := 1; width <= 140; width++ {
				for _, height := range heights {
					p := NewPage(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
					for _, row := range drillSample(state) {
						p.Back()
						p.Render(state, sel, width, height)
						if row != "" {
							p.Enter(row)
						}
						lines := p.Render(state, sel, width, height)
						if len(lines) > height {
							t.Fatalf("%s %v %q w=%d h=%d: %d lines", name, sel, row, width, height, len(lines))
						}
						for i, line := range lines {
							if w := blocks.Width(line); w > width {
								t.Fatalf("%s %v %q w=%d h=%d: line %d is %d cells: %q",
									name, sel, row, width, height, i, w, line)
							}
							if strings.ContainsAny(line, "\n\r") {
								t.Fatalf("%s %v w=%d: line %d carries a newline: %q", name, sel, width, i, line)
							}
						}
						for _, tg := range p.Targets() {
							if tg.Line < 0 || tg.Line >= len(lines) {
								t.Fatalf("%s w=%d h=%d: target %+v is off the frame (%d lines)",
									name, width, height, tg, len(lines))
							}
							if tg.From < 0 || tg.To > width || tg.To <= tg.From {
								t.Fatalf("%s w=%d: target %+v is outside the pane", name, width, tg)
							}
						}
					}
				}
			}
		}
	}
}

// Foreign prose reaches the page from four directions — a model wrote the
// belief, a model wrote the question, a workflow name came off disk, a commit
// subject came out of git. None of it may move the cursor or clear the screen,
// at any width the lens can be, in the list or in a detail page.
func TestTheHostilePageCannotDriveTheTerminal(t *testing.T) {
	state := hostile()
	for width := 20; width <= 140; width++ {
		p := NewPage(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
		for _, sel := range selections(state) {
			for _, row := range drillSample(state) {
				p.Back()
				p.Render(state, sel, width, 60)
				if row != "" {
					p.Enter(row)
				}
				for _, line := range p.Render(state, sel, width, 60) {
					for _, bad := range []string{"\x1b[2J", "\x1b[H", "\x1b]0;", "\x1b]8;", "\x07", "\x00"} {
						if strings.Contains(line, bad) {
							t.Fatalf("w=%d %v: %q survived sanitising: %q", width, sel, bad, line)
						}
					}
				}
			}
		}
	}
}

// Every painting configuration, at the widths where something changes. A nil
// Styler is in the set on purpose.
func TestEveryStylerPaintsThePageWithinTheFrame(t *testing.T) {
	state := rich()
	widths := []int{1, 2, 8, 13, 25, 26, 40, 80, 120}
	rows := drillSample(state)
	for name, st := range stylers() {
		for _, sel := range selections(state) {
			for _, width := range widths {
				p := NewPage(st)
				for _, row := range rows {
					p.Back()
					p.Render(state, sel, width, 40)
					if row != "" {
						p.Enter(row)
					}
					for i, line := range p.Render(state, sel, width, 40) {
						if w := blocks.Width(line); w > width {
							t.Fatalf("%s %v %q w=%d: line %d is %d cells: %q",
								name, sel, row, width, i, w, line)
						}
					}
				}
			}
		}
	}
}

// A row id is never rendered (5.14), on the list and in every detail page.
func TestPageRowIdsNeverReachTheFrame(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[0].ID = "ZZBELIEFIDZZ"
	state.Knowhow.Crafts[0].ID = "ZZCRAFTIDZZ"
	state.Knowhow.Skills[0].ID = "ZZSKILLIDZZ"
	state.Practice.Questions[0].ID = "ZZQUESTIONIDZZ"
	ids := []string{"ZZBELIEFIDZZ", "ZZCRAFTIDZZ", "ZZSKILLIDZZ", "ZZQUESTIONIDZZ"}
	p := NewPage(nil)
	for _, sel := range selections(state) {
		for _, row := range append([]string{""}, pageRows(state)...) {
			p.Back()
			p.Render(state, sel, 120, 80)
			if row != "" {
				p.Enter(row)
			}
			out := strings.Join(p.Render(state, sel, 120, 80), "\n")
			for _, id := range ids {
				if strings.Contains(out, id) {
					t.Fatalf("%v %q: id %q rendered:\n%s", sel, row, id, out)
				}
			}
		}
	}
}

// The zero page teaches rather than going blank: three tabs, each naming the
// thing that would put something in it (5.22 rule 6).
func TestTheZeroPageTeaches(t *testing.T) {
	teaching := map[Section]string{
		SectionBeliefs:  "I write one down",
		SectionKnowhow:  "worth repeating",
		SectionPractice: "I practice in the quiet",
	}
	for _, section := range Sections() {
		_, lines := tab(t, nil, State{}, Selection{}, section, 70, 40)
		out := strings.Join(lines, "\n")
		for _, s := range Sections() {
			if !strings.Contains(lines[0], s.Word()) {
				t.Fatalf("the empty strip lost %q:\n%s", s.Word(), out)
			}
		}
		if !strings.Contains(out, teaching[section]) {
			t.Fatalf("%v reported instead of teaching (%q):\n%s", section, teaching[section], out)
		}
	}
}

// A missing clock drops relative times rather than dating the page from the Unix
// epoch (10.2.8).
func TestThePageDropsAgesWithNoClock(t *testing.T) {
	state := rich()
	state.Now = time.Time{}
	_, lines := page(t, nil, state, Selection{Home: HomeNotebook}, 100, 80)
	out := strings.Join(lines, "\n")
	for _, bad := range []string{"yesterday", "jul ", "fri"} {
		if strings.Contains(out, bad) {
			t.Fatalf("an age was rendered with no clock (%q):\n%s", bad, out)
		}
	}
}

// The selection still drives the page: a cursor in another home opens that
// home's tab, and a cursor on a row opens the tab the row lives in and reveals
// it. A cursor in a room that LEFT this page — standing, services — moves
// nothing, because there is no tab here to answer with.
func TestTheSelectionDrivesTheTab(t *testing.T) {
	state := rich()
	const width, height = 80, 10
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, width, height)
	if got := p.Section(); got != SectionBeliefs {
		t.Fatalf("a notebook cursor landed on %v", got)
	}
	p.Render(state, Selection{Home: HomeSelf}, width, height)
	if got := p.Section(); got != SectionKnowhow {
		t.Fatalf("a self cursor landed on %v", got)
	}
	p.Render(state, Selection{Home: HomeStanding}, width, height)
	if got := p.Section(); got != SectionKnowhow {
		t.Fatalf("a standing cursor moved the page to %v", got)
	}
	// A row the page does not draw moves nothing rather than landing somewhere
	// arbitrary.
	before := p.Scroll()
	p.Render(state, Selection{Home: HomeStanding, Row: "self/dials"}, width, height)
	if p.Section() != SectionKnowhow || p.Scroll() != before {
		t.Fatalf("an undrawn row moved the page to %v at %d", p.Section(), p.Scroll())
	}
	// A row that IS drawn opens its own tab and brings the row on screen.
	sel := Selection{Home: HomeStanding, Row: QuestionRowPrefix + "q3"}
	out := strings.Join(p.Render(state, sel, width, height), "\n")
	if p.Section() != SectionPractice {
		t.Fatalf("a question row landed the page on %v", p.Section())
	}
	if !strings.Contains(out, "why does the CJK test flap") {
		t.Fatalf("the row the cursor names is not on screen:\n%s", out)
	}
}

// -- helpers -----------------------------------------------------------------

func inOrder(s string, parts ...string) bool {
	at := 0
	for _, part := range parts {
		i := strings.Index(s[at:], part)
		if i < 0 {
			return false
		}
		at += i + len(part)
	}
	return true
}

// stripTarget is one section's run on the strip of the last frame.
func stripTarget(t *testing.T, p *Page, s Section) Target {
	t.Helper()
	for _, tg := range p.Targets() {
		if tg.Kind == TargetSection && tg.ID == s.TargetID() {
			if tg.Line != 0 {
				t.Fatalf("%v's strip run is on line %d, not the strip", s, tg.Line)
			}
			return tg
		}
	}
	t.Fatalf("no strip run for %v: %+v", s, p.Targets())
	return Target{}
}

// contentColumn is where a line's text starts, or -1 for a blank line.
func contentColumn(line string) int {
	for i, r := range line {
		if r != ' ' {
			return i
		}
	}
	return -1
}

// pageSelections is one selection per interesting case per home: the surface
// row, a row that is not in the state at all, and the first member row.
func pageSelections(s State) []Selection {
	out := []Selection{{Home: HomeNone}}
	for _, h := range All() {
		out = append(out, Selection{Home: h}, Selection{Home: h, Row: "nope"})
		if rows := Scope(s, h).Rows; len(rows) > 1 {
			out = append(out, Selection{Home: h, Row: rows[1].ID})
		}
	}
	return out
}

// foldable is every row id in a state that has something behind a fold.
func foldable(s State) []string {
	out := []string{}
	for i := range s.Notebook.Beliefs {
		if len(s.Notebook.Beliefs[i].Evidence) > 0 {
			out = append(out, BeliefRowPrefix+s.Notebook.Beliefs[i].ID)
		}
	}
	for i := range s.Services.Services {
		sv := s.Services.Services[i]
		if len(sv.Log) > 0 || sv.LogPath != "" {
			out = append(out, ServiceRowPrefix+sv.ID)
		}
	}
	return out
}

// escapes pulls every CSI sequence out of a frame, so a test can ask which inks
// were actually spent rather than which ones the source mentions.
func escapes(frame string) []string {
	out := []string{}
	for i := 0; i < len(frame); {
		j := strings.IndexByte(frame[i:], 0x1b)
		if j < 0 {
			return out
		}
		start := i + j
		end := start
		for end < len(frame) && frame[end] != 'm' {
			end++
		}
		if end >= len(frame) {
			return out
		}
		out = append(out, frame[start:end+1])
		i = end + 1
	}
	return out
}

// -- the grid (§20) ----------------------------------------------------------

// EVERY LINE IS ON THE GRID. A marker lives in the gutter (cols 0–1), content
// hangs from the content edge at col 2, and a child hangs one step further at
// col 4. Nothing lands at 1, at 3, or anywhere else — "never three spaces, never
// one" is the law's own wording and this is it as an assertion.
//
// The strip is chrome and sits at the left edge by design (§19); on a detail
// page the trail is chrome for the same reason. Both are the frame's first line
// of their surface, and both are the only exemptions.
func TestEveryLineSitsOnTheGrid(t *testing.T) {
	states := map[string]State{"rich": rich(), "empty": {}, "wide": wide(12)}
	for name, state := range states {
		for _, width := range []int{40, 60, 80, 120, 140} {
			for _, section := range Sections() {
				sel := Selection{Home: HomeNotebook}
				p, lines := tab(t, nil, state, sel, section, width, 200)
				checkGrid(t, name+"/"+section.String(), body(lines), width)
				for _, row := range drillSample(state) {
					if row == "" {
						continue
					}
					p.Back()
					p.Render(state, sel, width, 200)
					if !p.Enter(row) {
						continue
					}
					drilled := p.Render(state, sel, width, 200)
					if len(drilled) == 0 {
						continue
					}
					// The trail is line 0 and is chrome at the left edge.
					checkGrid(t, name+"/"+row, drilled[1:], width)
				}
				p.Back()
			}
		}
	}
}

// checkGrid asserts §20's two numbers over a block of body lines.
func checkGrid(t *testing.T, where string, lines []string, width int) {
	t.Helper()
	for i, line := range lines {
		col := contentColumn(line)
		switch col {
		case -1, contentEdge, childEdge:
			continue
		case 0:
			// A marker in the gutter: one cell, then the gap, then content at
			// the edge. A wider marker (a two-digit step ordinal) keeps its one
			// space, which is the one place the painter gives the grid up and it
			// gives it up visibly.
			runes := []rune(line)
			if len(runes) > contentEdge && runes[1] == ' ' && runes[contentEdge] != ' ' {
				continue
			}
			if len(runes) > 2 && runes[1] != ' ' {
				continue // a wide marker; still no content in column 1 alone
			}
			t.Fatalf("%s w=%d: line %d has a marker but no content edge: %q", where, width, i, line)
		default:
			t.Fatalf("%s w=%d: line %d starts at column %d, off the grid: %q", where, width, i, col, line)
		}
	}
}

// BLOCKS BREATHE, LINES DON'T (§20): a list of rows carries no blank inside it,
// a row and its verb strip are one tight unit, and the readings that close the
// practice tab are their own block with exactly one blank above them.
func TestBlocksBreatheAndLinesDoNot(t *testing.T) {
	state := rich()
	sel := Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "41"}
	_, lines := tab(t, nil, state, sel, SectionBeliefs, 110, 200)
	rows := body(lines)
	for i, line := range rows {
		if strings.TrimSpace(line) != "" {
			continue
		}
		t.Fatalf("the belief list breathes at line %d; a list is one block:\n%s", i, strings.Join(rows, "\n"))
	}
	// The verb strip is the row's child on the very next line — no blank, one
	// indent step in.
	verbAt := -1
	for i, line := range rows {
		if strings.Contains(line, "forget") {
			verbAt = i
		}
	}
	if verbAt <= 0 {
		t.Fatalf("no verb strip under the cursor row:\n%s", strings.Join(rows, "\n"))
	}
	if got := contentColumn(rows[verbAt]); got != childEdge {
		t.Fatalf("the verb strip hangs at column %d, not at E1: %q", got, rows[verbAt])
	}
	if !strings.Contains(rows[verbAt-1], "santosh prefers replies") {
		t.Fatalf("the verb strip is not adjacent to its row: %q", rows[verbAt-1])
	}

	// The practice tab's closing readings: one blank line, then both at E0.
	_, lines = tab(t, nil, state, Selection{Home: HomeNotebook}, SectionPractice, 110, 200)
	rows = body(lines)
	blanks := 0
	for i, line := range rows {
		if strings.TrimSpace(line) == "" {
			blanks++
			if i+1 >= len(rows) || !strings.Contains(rows[i+1], "strongest") {
				t.Fatalf("the blank at line %d is not the seam before the readings:\n%s", i, strings.Join(rows, "\n"))
			}
		}
	}
	if blanks != 1 {
		t.Fatalf("the practice tab has %d blank lines, not one:\n%s", blanks, strings.Join(rows, "\n"))
	}
	for _, want := range []string{"strongest", "today "} {
		found := false
		for _, line := range rows {
			if strings.Contains(line, want) && contentColumn(line) == contentEdge {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q is not at the content edge:\n%s", want, strings.Join(rows, "\n"))
		}
	}
}

// -- the one-line row law (§19) ----------------------------------------------

// NO ROW EVER WRAPS. Every belief, workflow, tool and gap is one line at every
// width; the full text lives in the detail page. The assertion counts lines
// against rows rather than looking for a wrap, because a wrap is exactly what a
// count would hide.
func TestNoRowEverWraps(t *testing.T) {
	state := rich()
	// Bodies far longer than any pane, including one with newlines in it.
	long := strings.Repeat("a belief with a great deal to say for itself ", 8)
	state.Notebook.Beliefs[0].Body = long
	state.Notebook.Beliefs[1].Body = long + "\n" + long
	state.Knowhow.Crafts[0].Name = long
	state.Practice.Questions[0].Body = long
	counts := map[Section]int{
		SectionBeliefs:  len(state.Notebook.Beliefs),
		SectionKnowhow:  len(state.Knowhow.Crafts) + len(state.Knowhow.Skills),
		SectionPractice: len(state.Practice.Questions),
	}
	for width := 20; width <= 140; width++ {
		for _, section := range Sections() {
			_, lines := tab(t, nil, state, Selection{Home: HomeNotebook}, section, width, 200)
			rows := 0
			for _, line := range body(lines) {
				if strings.TrimSpace(line) == "" {
					break // the practice tab's closing block
				}
				rows++
			}
			if rows != counts[section] {
				t.Fatalf("w=%d %v: %d lines for %d rows — a row wrapped:\n%s",
					width, section, rows, counts[section], strings.Join(body(lines), "\n"))
			}
		}
	}
}

// A ROW'S RECEIPT RIDES ITS NAME (§20's first placement), in parentheses, dim,
// immediately after the words it is about — not right-aligned across a gulf of
// empty cells, which is the defect the user has flagged twice.
func TestAReceiptRidesItsRow(t *testing.T) {
	state := rich()
	_, lines := tab(t, nil, state, Selection{Home: HomeNotebook}, SectionKnowhow, 130, 80)
	row := ""
	for _, line := range body(lines) {
		if strings.Contains(line, "release-notes") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("no workflow row:\n%s", strings.Join(lines, "\n"))
	}
	open := strings.Index(row, " (")
	name := strings.Index(row, "release-notes")
	if open < 0 || open < name {
		t.Fatalf("the receipt does not ride the name: %q", row)
	}
	if gap := open - (name + len("release-notes")); gap != 0 {
		t.Fatalf("%d cells between the name and its receipt: %q", gap, row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), ")") {
		t.Fatalf("the receipt is not closed: %q", row)
	}
	// The figures each kind is entitled to, still present.
	out := strings.Join(lines, "\n")
	for _, want := range []string{"$0.31/run", "tool · 11 uses"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
}

// DIMMING IS A TIER, NOT A MEANING (§19). The user's reading of the first live
// build was "I see highlighted lines and dim lines, not sure what that means";
// every state that matters is now a WORD in the receipt, and the dim tier only
// echoes it.
func TestAStateThatMattersIsAWordAndNotOnlyADimTier(t *testing.T) {
	state := rich()
	_, lines := tab(t, nil, state, Selection{Home: HomeNotebook}, SectionBeliefs, 120, 200)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"let go", "candidate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("a faded row says nothing about why (%q):\n%s", want, out)
		}
	}
	// One spelling per fact: a candidate row does not also carry the
	// credibility word that says the same thing in another vocabulary.
	state.Notebook.Beliefs[3].Trust = "tentative"
	_, lines = tab(t, nil, state, Selection{Home: HomeNotebook}, SectionBeliefs, 120, 200)
	for _, line := range body(lines) {
		if strings.Contains(line, "candidate") && strings.Contains(line, "tentative") {
			t.Fatalf("candidate and tentative are two spellings of one fact: %q", line)
		}
	}
	_, lines = tab(t, nil, state, Selection{Home: HomeNotebook}, SectionKnowhow, 120, 200)
	if !strings.Contains(strings.Join(lines, "\n"), "retired") {
		t.Fatalf("a retired tool says nothing about why:\n%s", strings.Join(lines, "\n"))
	}
}

// -- hover (§16, defect 3) ----------------------------------------------------

// A ROW ANSWERS THE POINTER. Its name goes up one tier and its verbs appear —
// "a reader should never wonder whether a row is clickable". The first live
// build could do neither: rows were drawn at the top of the ramp, and
// [tokens.Promote] leaves primary where it is, so hover was a no-op by
// construction.
func TestHoverPromotesTheRowAndShowsItsVerbs(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	state, sel := rich(), Selection{Home: HomeNotebook}
	for _, width := range []int{60, 100, 140} {
		p, _ := tab(t, st, state, sel, SectionBeliefs, width, 200)
		rowID := BeliefRowPrefix + "41"
		door := Target{}
		for _, tg := range p.Targets() {
			if tg.Kind == TargetDoor && tg.ID == rowID {
				door = tg
			}
		}
		if door.ID == "" {
			t.Fatalf("w=%d: no door to hover", width)
		}
		before := strings.Join(p.Render(state, sel, width, 200), "\n")
		if strings.Contains(before, "forget") {
			t.Fatalf("w=%d: an unhovered row already shows its verbs", width)
		}
		if !p.SetHover(width/2, door.Line) {
			t.Fatalf("w=%d: hovering a row moved nothing", width)
		}
		after := strings.Join(p.Render(state, sel, width, 200), "\n")
		if after == before {
			t.Fatalf("w=%d: the frame did not answer the pointer", width)
		}
		if !strings.Contains(after, "forget") {
			t.Fatalf("w=%d: hover did not reveal the row's verbs:\n%s", width, after)
		}
		rest := restTier(false).Fg(tokens.TrueColor, tokens.FocusNormal)
		promoted := tokens.Promote(restTier(false)).Fg(tokens.TrueColor, tokens.FocusNormal)
		if rest == promoted {
			t.Fatal("the resting tier cannot be promoted; hover can never be visible")
		}
		hovered := ""
		for _, line := range strings.Split(after, "\n") {
			if strings.Contains(line, "santosh prefers replies") {
				hovered = line
			}
		}
		if !strings.Contains(hovered, promoted) {
			t.Fatalf("w=%d: the hovered row kept its resting tier: %q", width, hovered)
		}
		// A pointer that walks onto the verb strip keeps the row open, rather
		// than closing it out from under itself.
		verb := Target{}
		for _, tg := range p.Targets() {
			if tg.Kind == TargetVerb && tg.Row == rowID {
				verb = tg
			}
		}
		if verb.ID == "" {
			t.Fatalf("w=%d: the revealed strip has no targets", width)
		}
		p.SetHover(verb.From, verb.Line)
		if !strings.Contains(strings.Join(p.Render(state, sel, width, 200), "\n"), "forget") {
			t.Fatalf("w=%d: the strip closed under the pointer", width)
		}
		p.ClearHover()
		if strings.Contains(strings.Join(p.Render(state, sel, width, 200), "\n"), "forget") {
			t.Fatalf("w=%d: the strip outlived the pointer", width)
		}
	}
}

// -- no raw id, anywhere (5.14, §19) ------------------------------------------

// The user found `taught by  task-5381` on a belief's page. Nothing shaped like
// a handle may reach any frame of this surface — not the evidence line, not a
// row, not a trail — in either the list or the drill.
func TestNoHandleShapedTextReachesTheFrame(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[0].Evidence = []Evidence{
		{Name: "ship the pricing page", Room: "task-5381"},
		{Room: "task-9000", When: fixedNow.Add(-3 * time.Hour)},
		{When: fixedNow.Add(-4 * time.Hour)},
	}
	handles := regexp.MustCompile(`task-[0-9]+|node-[0-9]+|#[0-9]+`)
	sel := Selection{Home: HomeNotebook}
	for _, width := range []int{40, 80, 140} {
		for _, section := range Sections() {
			p, lines := tab(t, nil, state, sel, section, width, 200)
			if hit := handles.FindString(strings.Join(lines, "\n")); hit != "" {
				t.Fatalf("w=%d %v: %q reached the list", width, section, hit)
			}
			for _, row := range pageRows(state) {
				p.Back()
				p.Render(state, sel, width, 200)
				if !p.Enter(row) {
					t.Fatalf("%q did not open from %v", row, section)
				}
				out := strings.Join(p.Render(state, sel, width, 200), "\n")
				if hit := handles.FindString(out); hit != "" {
					t.Fatalf("w=%d %q: %q reached the drill:\n%s", width, row, hit, out)
				}
			}
			p.Back()
		}
	}
}

// What taught a belief is drawn BY NAME and is a door onto that work; a
// reference the wiring could not name says so in words, with its age.
func TestEvidenceIsANameAndADoor(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[0].Evidence = []Evidence{
		{Name: "ship the pricing page", Room: "task-5381", When: fixedNow.Add(-2 * time.Hour)},
		{Room: "task-9000", When: fixedNow.Add(-3 * time.Hour)},
	}
	sel := Selection{Home: HomeNotebook}
	p := NewPage(nil)
	p.Render(state, sel, 100, 200)
	if !p.Enter(BeliefRowPrefix + "41") {
		t.Fatal("the belief did not open")
	}
	lines := p.Render(state, sel, 100, 200)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"taught by", "ship the pricing page", "a past task · 3h"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	rooms := map[string]Target{}
	for _, tg := range p.Targets() {
		if tg.Kind == TargetRoom {
			rooms[tg.ID] = tg
		}
	}
	for _, id := range []string{"task-5381", "task-9000"} {
		tg, ok := rooms[id]
		if !ok {
			t.Fatalf("no room door for %q: %+v", id, p.Targets())
		}
		if got, ok := p.Click(tg.From, tg.Line); !ok || got.Kind != TargetRoom || got.ID != id {
			t.Fatalf("clicking the door answered %+v/%v", got, ok)
		}
		// The page applies nothing: opening a room is the host's.
		if _, drilled := p.Detail(); !drilled {
			t.Fatal("clicking an evidence door left the detail page")
		}
	}
	// The evidence rows are children of the group word above them.
	for _, line := range lines {
		if strings.Contains(line, "ship the pricing page") && contentColumn(line) != childEdge {
			t.Fatalf("an evidence row hangs at %d, not at E1: %q", contentColumn(line), line)
		}
	}
}
