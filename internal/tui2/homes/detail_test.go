package homes

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The drill's own gate: that a row opens a document, that the document is the
// right one, that esc comes back to the row it left, and that a page which lost
// its row underneath it says so by showing the list rather than a blank frame.

// Enter draws the item's own page — trail, title with its receipt, body, verbs —
// and the list is gone while it is up.
func TestEnterOpensTheItemsPage(t *testing.T) {
	state := rich()
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)

	for _, c := range []struct {
		row  string
		want []string
		gone string
	}{
		{
			row: CraftRowPrefix + "release-notes",
			want: []string{
				"notebook " + tokens.GlyphScopeUp + " know-how " + tokens.GlyphScopeUp + " release-notes",
				"collect merged PRs", "ceilings $0.50 · 10m",
				"gather merged PRs since last tag", "draft the notes", "after 1",
				"check every link resolves", "verify", "after 2" + tokens.GlyphSeparator + "3",
				"v3", "tightened the link check", "v1", "forged from",
				"run", "revert", "retire",
			},
			gone: "how flaky is the e2e suite",
		},
		{
			row: BeliefRowPrefix + "41",
			want: []string{
				"notebook " + tokens.GlyphScopeUp + " beliefs " + tokens.GlyphScopeUp,
				"santosh prefers replies", "user · strong",
				"12 recalls", "taught by", "ship the pricing page", "a past note",
				"forget", "edit",
			},
			gone: "release-notes",
		},
		{
			row:  SkillRowPrefix + "sk1",
			want: []string{"imgshrink", "shrink a PNG", "tool · 11 uses", "imgshrink/run.sh", "retire"},
			gone: "pdfsplit",
		},
		{
			row: QuestionRowPrefix + "q1",
			want: []string{
				"how flaky is the e2e suite", "practicing · 2 runs · $0.12",
				"about repo:", "$0.04", "surprise -0.12",
			},
			gone: "uv beats pip",
		},
	} {
		if !p.Enter(c.row) {
			t.Fatalf("%q did not open", c.row)
		}
		if got, ok := p.Detail(); !ok || got != c.row {
			t.Fatalf("%q opened %q/%v", c.row, got, ok)
		}
		out := strings.Join(p.Render(state, Selection{Home: HomeNotebook}, 100, 60), "\n")
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Fatalf("%q is missing %q:\n%s", c.row, want, out)
			}
		}
		if strings.Contains(out, c.gone) {
			t.Fatalf("%q still shows the list (%q):\n%s", c.row, c.gone, out)
		}
		// The strip belongs to the list; the detail says where it is with its
		// trail instead, and one page does not index itself twice.
		if strings.Contains(out, "beliefs 500+") {
			t.Fatalf("%q drew the list's strip:\n%s", c.row, out)
		}
		p.Back()
	}
}

// A question is steered conversationally, so its page draws no verbs at all
// (notebook-split.md §3) — and a trait's page draws none for the same reason a
// trait row does not: a measurement is not something to argue with in a form.
func TestTheReadOnlyPagesDrawNoVerbs(t *testing.T) {
	state := rich()
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)
	for _, row := range []string{QuestionRowPrefix + "q1", BeliefRowPrefix + "6"} {
		if !p.Enter(row) {
			t.Fatalf("%q did not open", row)
		}
		p.Render(state, Selection{Home: HomeNotebook}, 100, 60)
		for _, tg := range p.Targets() {
			if tg.Kind == TargetVerb {
				t.Fatalf("%q drew the verb %q", row, tg.ID)
			}
		}
		p.Back()
	}
}

// Esc comes back to the LIST — the same TAB, at the same scroll, with the row it
// left revealed. It is the whole reason the drill is the page's state: a reader
// who drilled into the last question would otherwise return to the top of the
// page every time.
func TestBackRestoresTheTabAndTheRowItLeft(t *testing.T) {
	state := wide(60)
	const width, height = 80, 12
	sel := Selection{Home: HomeNotebook}
	p := NewPage(nil)
	p.Render(state, sel, width, height)
	p.SetSection(SectionPractice)
	p.Render(state, sel, width, height)
	p.ScrollBottom()
	lines := p.Render(state, sel, width, height)
	if !strings.Contains(strings.Join(lines, "\n"), "gap 59") {
		t.Fatalf("the fixture did not scroll to its last rows:\n%s", strings.Join(lines, "\n"))
	}
	away := p.Scroll()

	row := QuestionRowPrefix + "59"
	if !p.Enter(row) {
		t.Fatalf("%q did not open", row)
	}
	if p.Scroll() != 0 {
		t.Fatalf("a detail opened scrolled to %d", p.Scroll())
	}
	out := strings.Join(p.Render(state, sel, width, height), "\n")
	if !strings.Contains(out, "gap 59") {
		t.Fatalf("the detail is not the row that was opened:\n%s", out)
	}

	if !p.Back() {
		t.Fatal("back reported nothing to leave")
	}
	if p.Back() {
		t.Fatal("back left a page that was already the list")
	}
	if p.Scroll() != away {
		t.Fatalf("back landed at %d, not at %d", p.Scroll(), away)
	}
	if p.Section() != SectionPractice {
		t.Fatalf("back landed on the %v tab", p.Section())
	}
	out = strings.Join(p.Render(state, sel, width, height), "\n")
	if !strings.Contains(out, "gap 59") {
		t.Fatalf("back did not reveal the row it left:\n%s", out)
	}
	// A width change between the drill and the return re-lays the document; the
	// row is still revealed, because the return reveals a ROW and not an offset.
	p.Enter(row)
	p.Render(state, sel, 40, height)
	p.Back()
	out = strings.Join(p.Render(state, sel, 140, height), "\n")
	if !strings.Contains(out, "gap 59") {
		t.Fatalf("a re-laid document lost the row it returned to:\n%s", out)
	}
}

// A row from a tab that is NOT on screen still opens, because [Page.Enter] is
// answered from the collection rather than from the document: a rail cursor or a
// palette hit may name a belief while know-how is showing, and refusing it would
// be the surface's bookkeeping showing through.
func TestARowFromAnotherTabStillOpens(t *testing.T) {
	state := rich()
	sel := Selection{Home: HomeNotebook}
	p := NewPage(nil)
	p.Render(state, sel, 100, 40)
	p.SetSection(SectionKnowhow)
	p.Render(state, sel, 100, 40)
	if !p.Enter(QuestionRowPrefix + "q1") {
		t.Fatal("a row from the practice tab refused to open")
	}
	out := strings.Join(p.Render(state, sel, 100, 40), "\n")
	if !strings.Contains(out, "how flaky is the e2e suite") {
		t.Fatalf("the drill drew the wrong document:\n%s", out)
	}
}

// The section word on the trail is the way back with a pointer: it leaves the
// drill AND lands the list on that band, which is the same act as esc.
func TestTheTrailWordGoesBack(t *testing.T) {
	state := rich()
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)
	if !p.Enter(CraftRowPrefix + "release-notes") {
		t.Fatal("the workflow did not open")
	}
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)
	word := Target{}
	for _, tg := range p.Targets() {
		if tg.Kind == TargetSection && tg.ID == SectionKnowhow.TargetID() {
			word = tg
		}
	}
	if word.ID == "" {
		t.Fatalf("the trail carries no way back: %+v", p.Targets())
	}
	// The home word is NOT a target: clicking where the reader already is could
	// only mean what the strip already does (§16's HOVER rule — nothing inert is
	// clickable and nothing clickable is inert).
	if tg, ok := p.TargetAt(0, 0); ok {
		t.Fatalf("the home word answered a click: %+v", tg)
	}
	if _, ok := p.Click(word.From, word.Line); !ok {
		t.Fatal("the trail word did not answer a click")
	}
	if _, drilled := p.Detail(); drilled {
		t.Fatal("clicking the trail left the drill open")
	}
	p.Render(state, Selection{Home: HomeNotebook}, 100, 12)
	if got := p.Section(); got != SectionKnowhow {
		t.Fatalf("the trail word landed on %v", got)
	}
}

// A row that leaves the state under an open detail — retracted from another
// window, deleted off disk — puts the reader back on the list rather than on a
// blank frame, and an id the page never drew opens nothing at all.
func TestADetailWhoseRowLeftFallsBackToTheList(t *testing.T) {
	state := rich()
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)
	if p.Enter("belief:does-not-exist") {
		t.Fatal("a row the page never drew opened a detail")
	}
	if !p.Enter(SkillRowPrefix + "sk2") {
		t.Fatal("the skill did not open")
	}
	state.Knowhow.Skills = state.Knowhow.Skills[:1]
	out := strings.Join(p.Render(state, Selection{Home: HomeNotebook}, 100, 40), "\n")
	if _, drilled := p.Detail(); drilled {
		t.Fatalf("the drill survived its row leaving the state")
	}
	if !strings.Contains(out, "beliefs 500+") {
		t.Fatalf("the fallback is not the list:\n%s", out)
	}
}

// A detail page keeps §16's alignment promise at the extremes: one document
// from its first row, never wider than the pane, never taller than the budget.
// (The width sweep in page_test.go walks 1..140 across every state; this is the
// one property that is about the DOCUMENT rather than about the line.)
func TestADetailIsOneScrollingDocument(t *testing.T) {
	state := rich()
	p := NewPage(nil)
	p.Render(state, Selection{Home: HomeNotebook}, 100, 40)
	p.Enter(CraftRowPrefix + "release-notes")
	// COPIED: the frame aliases the page's own buffer and is valid only until
	// the next Render, which is exactly what this test does next.
	full := append([]string(nil), p.Render(state, Selection{Home: HomeNotebook}, 100, 200)...)
	if len(full) < 8 {
		t.Fatalf("the workflow's page is %d lines", len(full))
	}
	if strings.TrimSpace(full[0]) == "" || !strings.Contains(full[0], "notebook") {
		t.Fatalf("the trail is not the first row: %q", full[0])
	}
	short := p.Render(state, Selection{Home: HomeNotebook}, 100, 5)
	if len(short) != 5 {
		t.Fatalf("a 5-row budget drew %d rows", len(short))
	}
	if short[0] != full[0] {
		t.Fatalf("the top of the document moved: %q vs %q", short[0], full[0])
	}
	p.ScrollBy(3)
	scrolled := p.Render(state, Selection{Home: HomeNotebook}, 100, 5)
	if len(scrolled) == 0 || scrolled[0] == full[0] {
		t.Fatalf("the trail did not scroll with the document: %q", scrolled[0])
	}
}

// -- §19, the four defects on one screenshot ---------------------------------

// A TRAIL SEGMENT IS A NAME. The user's screenshot had an entire belief crammed
// into the breadcrumb — `notebook ‹ beliefs ‹ share under ticker SPCX and now
// trades publicly; as of Aug 10, 2026 it trades around $138.74. The company is
// no…`. Every segment caps at [trailNameCap], with the TAIL cut a sentence
// deserves: the first words are what name it.
func TestTheTrailIsANameAndNotABody(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[0].Body = "SpaceX IPO'd on the Nasdaq under ticker SPCX and now trades publicly; " +
		"as of Aug 10, 2026 it trades around $138.74. The company is no longer private."
	sel := Selection{Home: HomeNotebook}
	for _, width := range []int{60, 100, 140, 200} {
		p := NewPage(nil)
		p.Render(state, sel, width, 60)
		if !p.Enter(BeliefRowPrefix + "41") {
			t.Fatal("the belief did not open")
		}
		trail := p.Render(state, sel, width, 60)[0]
		if !strings.HasPrefix(trail, "notebook "+tokens.GlyphScopeUp+" beliefs "+tokens.GlyphScopeUp+" ") {
			t.Fatalf("w=%d: the trail lost its shape: %q", width, trail)
		}
		here := trail[strings.LastIndex(trail, tokens.GlyphScopeUp)+len(tokens.GlyphScopeUp)+1:]
		if blocks.Width(here) > trailNameCap {
			t.Fatalf("w=%d: the trail segment is %d cells: %q", width, blocks.Width(here), here)
		}
		if !strings.HasPrefix(here, "SpaceX IPO") {
			t.Fatalf("w=%d: the trail did not keep the first words: %q", width, here)
		}
		if strings.Contains(trail, "$138.74") {
			t.Fatalf("w=%d: the whole belief is still in the trail: %q", width, trail)
		}
	}
}

// SAY A THING ONCE (§19). The user saw "candidate" twice on one belief page —
// on the title line AND in the receipt. The receipt is its home; the title line
// carries the glyph and a short title and nothing else.
func TestTheDetailTitleDoesNotRepeatTheReceipt(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[3].Scope = "domain:spacex"
	sel := Selection{Home: HomeNotebook}
	p := NewPage(nil)
	p.Render(state, sel, 110, 60)
	if !p.Enter(BeliefRowPrefix + "9") {
		t.Fatal("the candidate belief did not open")
	}
	lines := p.Render(state, sel, 110, 60)
	out := strings.Join(lines, "\n")
	if n := strings.Count(out, "candidate"); n != 1 {
		t.Fatalf("candidate is said %d times:\n%s", n, out)
	}
	// The claim is the subject and the receipt sits UNDER it, at the same edge
	// (§20's second placement): the claim line carries no status word at all.
	claim, receipt := lines[2], lines[3]
	if !strings.Contains(claim, "maybe the compositor") {
		t.Fatalf("the first line of the block is not the claim: %q", claim)
	}
	if strings.Contains(claim, "candidate") {
		t.Fatalf("the claim line carries the status word: %q", claim)
	}
	if !strings.Contains(receipt, "domain:spacex · candidate") {
		t.Fatalf("the receipt is not the status word's home: %q", receipt)
	}
	if contentColumn(receipt) != contentEdge {
		t.Fatalf("the receipt left the claim's edge: %q", receipt)
	}
	// The claim is drawn ONCE and in full — no title line repeating its first
	// words above the body that says them again.
	if strings.Count(out, "maybe the compositor owns the interruption budget") != 1 {
		t.Fatalf("the claim is on the page twice:\n%s", out)
	}
}

// THE PADDING RHYTHM (§19, §20): a blank line between the title block and the
// body, one before each labelled group, one before the verb strip, and never two
// in a row.
func TestTheDetailPageKeepsTheRhythm(t *testing.T) {
	state := rich()
	sel := Selection{Home: HomeNotebook}
	for _, width := range []int{50, 80, 120} {
		p := NewPage(nil)
		p.Render(state, sel, width, 60)
		for _, row := range pageRows(state) {
			p.Back()
			p.Render(state, sel, width, 60)
			if !p.Enter(row) {
				t.Fatalf("%q did not open", row)
			}
			lines := p.Render(state, sel, width, 400)
			if len(lines) < 3 {
				t.Fatalf("%q drew %d lines", row, len(lines))
			}
			if strings.TrimSpace(lines[0]) == "" {
				t.Fatalf("%q: the trail is not the first row", row)
			}
			if strings.TrimSpace(lines[1]) != "" {
				t.Fatalf("%q: no blank between the trail and the title: %q", row, lines[1])
			}
			if strings.TrimSpace(lines[2]) == "" {
				t.Fatalf("%q: the title block is missing", row)
			}
			for i := 1; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "" && strings.TrimSpace(lines[i-1]) == "" {
					t.Fatalf("%q w=%d: two blank lines at %d:\n%s", row, width, i, strings.Join(lines, "\n"))
				}
			}
			if strings.TrimSpace(lines[len(lines)-1]) == "" {
				t.Fatalf("%q: the document ends on a blank line", row)
			}
			// A verb strip, where there is one, has a blank line above it.
			for i, line := range lines {
				if !strings.Contains(line, "forget") && !strings.Contains(line, "revert") {
					continue
				}
				if i == 0 || strings.TrimSpace(lines[i-1]) != "" {
					t.Fatalf("%q: the verb strip has no blank above it:\n%s", row, strings.Join(lines, "\n"))
				}
				if got := contentColumn(line); got != contentEdge {
					t.Fatalf("%q: the verb strip hangs at %d, not at the content edge", row, got)
				}
			}
		}
		p.Back()
	}
}
