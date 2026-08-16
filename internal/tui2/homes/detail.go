package homes

import (
	"strconv"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE DRILL: one row, opened as a FULL PAGE.
//
// notebook-split.md §3 is explicit that this is not a fold. A belief's evidence,
// a workflow's steps and a question's attempts are documents, and a document
// unfolded inside a list is a list nobody can read — the row above it and the
// row below it are still there, competing for the same eye, and the thing the
// reader asked to see is the only one of the three that is not fully on screen.
//
// So the page swaps: the list goes away, the item takes the whole surface, and
// esc brings the list back SCROLLED TO THE ROW IT LEFT (see [Page.Back]). That
// last clause is the whole reason the drill is the page's own state rather than
// the host's: only the page knows where the row was, and a reader who drilled
// into the ninth workflow and came back to the top of the band would have to
// re-find their place every time they looked at something.
//
// # The grammar, one for every kind (§3)
//
//	notebook ‹ know-how ‹ release-notes           trail
//	○ release-notes      workflow · v3 · proved…  title + receipt
//	collect merged PRs, draft the notes, verify…  body
//	ceilings $0.50 · 10m
//
//	  1 gather merged PRs since last tag
//	  2 draft the notes                  after 1
//
//	run · revert · retire                          verbs
//
// The trail SCROLLS WITH THE DOCUMENT and is not chrome: §5's record header law
// says the header is the first row of one list, and a detail page is a record of
// a different kind of thing. The section word inside it is a click target —
// clicking `know-how` is the same act as esc, and lands the list on that band —
// so the way back is on the screen rather than in a key nobody was told about.

// Enter drills into a row and reports whether it opened one.
//
// It takes the row id the LIST already carries ([BeliefRowPrefix]…), because
// that is the id the rail's cursor, the click map and the reveal table all
// speak; a second identifier for the same row would be a second place they
// could disagree. A row id the page has never drawn opens nothing, which is
// what keeps a stale selection from painting an empty document.
func (p *Page) Enter(rowID string) bool {
	if rowID == "" || p.detail == rowID {
		return false
	}
	if !p.known[rowID] {
		return false
	}
	if p.detail == "" {
		p.listScroll = p.scroll
	}
	p.detail = rowID
	p.scroll = 0
	p.ClearHover()
	return true
}

// Detail is the row the page is drilled into, and false when it is showing the
// list.
func (p *Page) Detail() (string, bool) { return p.detail, p.detail != "" }

// Back returns to the list at the scroll it was left at, and reports whether
// there was a detail to leave. A host wires it to esc; false is the signal that
// esc means something else here (leave the page), which is exactly what the
// shell's own esc ladder needs to know.
func (p *Page) Back() bool {
	if p.detail == "" {
		return false
	}
	row := p.detail
	p.detail = ""
	p.scroll = p.listScroll
	p.pending = row
	p.ClearHover()
	return true
}

// buildDetail lays the drilled row as its own document, and reports false when
// the row is no longer in the state — a belief retracted from another window, a
// workflow deleted off disk between the poll and the frame. The caller draws the
// list instead, which is the honest answer: the thing is gone, and the list is
// where a reader finds out what is still there.
func (p *Page) buildDetail(state State, width int) bool {
	p.doc = p.doc[:0]
	p.hits = p.hits[:0]
	if p.rowAt == nil {
		p.rowAt = make(map[string]int, 32)
	}
	clear(p.rowAt)
	id := p.detail
	switch {
	case hasPrefix(id, BeliefRowPrefix):
		for i := range state.Notebook.Beliefs {
			if BeliefRowPrefix+state.Notebook.Beliefs[i].ID == id {
				p.beliefDetail(state, state.Notebook.Beliefs[i], width)
				return true
			}
		}
	case hasPrefix(id, CraftRowPrefix):
		for i := range state.Knowhow.Crafts {
			if CraftRowPrefix+state.Knowhow.Crafts[i].ID == id {
				p.craftDetail(state, state.Knowhow.Crafts[i], width)
				return true
			}
		}
	case hasPrefix(id, SkillRowPrefix):
		for i := range state.Knowhow.Skills {
			if SkillRowPrefix+state.Knowhow.Skills[i].ID == id {
				p.skillDetail(state, state.Knowhow.Skills[i], width)
				return true
			}
		}
	case hasPrefix(id, QuestionRowPrefix):
		for i := range state.Practice.Questions {
			if QuestionRowPrefix+state.Practice.Questions[i].ID == id {
				p.questionDetail(state, state.Practice.Questions[i], width)
				return true
			}
		}
	}
	return false
}

// hasPrefix is strings.HasPrefix without the import, kept local because it is
// asked once per row kind on a path that runs per frame.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// -- the four bodies ---------------------------------------------------------

// beliefDetail is one belief opened: the claim in full, where it came from,
// what taught it, and how often it has been recalled since.
//
// THE TITLE LINE IS A TITLE. It carries the glyph and the belief's first words
// and NOTHING ELSE — no lifecycle word, because the receipt beside it already
// says "candidate" and §19 forbids a screen saying a thing twice because two
// elements each wanted it. That is the defect the user met as "candidate
// appeared twice"; the receipt is its home.
func (p *Page) beliefDetail(state State, b Belief, width int) {
	p.trail(SectionBeliefs, beliefTitle(b), width)
	p.emit("")
	glyph, gtok := p.lifeGlyph(LifeQueued, false)
	p.claim(glyph, gtok, b.Class.Lead()+b.Body, beliefReceipt(b, state.Now), b.Retired, width)
	// WHERE IT CAME FROM, in one sentence rather than a field. "you said it" is
	// the whole of what a channel means to a person, and a `channel  stated` row
	// would spend two cells teaching the store's vocabulary.
	provenance := b.Channel.Word()
	recalls := beliefRecalls(b, state.Now)
	if provenance != "" || recalls != "" {
		p.emit("")
		p.at(contentEdge, provenance, tokens.TextTertiary, width)
		p.at(contentEdge, recalls, tokens.TextTertiary, width)
	}
	if len(b.Evidence) > 0 {
		p.emit("")
		p.at(contentEdge, "taught by", tokens.TextTertiary, width)
		for _, ref := range b.Evidence {
			p.evidence(ref, state.Now, width)
		}
	}
	p.detailVerbs(beliefVerbs(b, state.Visitor), width)
}

// beliefTitle is the belief's first words — the NAME a trail and a title line
// carry. The whole claim lives in the body under it and in the list row's own
// ellipsis, never in a breadcrumb (§19).
func beliefTitle(b Belief) string { return firstLine(b.Class.Lead() + b.Body) }

// beliefRecalls is how often the belief has been pulled into a model's context
// and when it last was, or nothing at all when the count was never kept.
func beliefRecalls(b Belief, now time.Time) string {
	if !b.HasUses {
		return ""
	}
	var c clause
	c.addInt(b.Uses, recallWord(b.Uses))
	if seen := since(b.LastUsed, now); seen != "" {
		c.add("last " + seen)
	}
	return c.String()
}

// evidence draws one piece of work that taught a belief, BY NAME and as a door.
//
// This is the line the user found reading `taught by  task-5381`. A raw id is
// banned on every surface (5.14, §19) and the fix cannot be to drop the
// reference — what taught a belief is the most load-bearing thing on the page —
// so the wiring resolves the work's own title and the page draws that. A
// reference it could not name says `a past task` with its age, which is true;
// the handle stays behind the row as a [TargetRoom] the host can open, because a
// name a reader can see and not reach is worse than the id it replaced.
func (p *Page) evidence(e Evidence, now time.Time, width int) {
	name := firstLine(p.clean(e.Name))
	tok := tokens.TextSecondary
	if name == "" {
		tok = tokens.TextTertiary
		var c clause
		if e.Room != "" {
			c.add("a past task")
		} else {
			c.add("a past note")
		}
		c.add(since(e.When, now))
		name = c.String()
	}
	l := &p.line
	l.reset(width)
	l.padTo(childEdge)
	if e.Room != "" && p.lit(TargetRoom, e.Room, "") {
		tok = tokens.Promote(tok)
	}
	from := l.w
	l.add(name, tok)
	if e.Room != "" && from < l.w {
		p.mark(TargetRoom, e.Room, "", from, l.w)
	}
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
}

func recallWord(n int) string {
	if n == 1 {
		return "recall"
	}
	return "recalls"
}

// craftDetail is one workflow opened: what it does, what it may spend, the steps
// in file order, and how it got here.
func (p *Page) craftDetail(state State, c Craft, width int) {
	p.trail(SectionKnowhow, c.Name, width)
	p.emit("")
	glyph, gtok := p.lifeGlyph(LifeQueued, false)
	p.row(rowLine{
		glyph: glyph, gtok: gtok,
		name: firstLine(c.Name), tok: tokens.TextPrimary,
		receipt: craftReceipt(c, state.Now),
	}, width)
	if c.Description != "" {
		p.emit("")
		p.body(c.Description, width)
	}
	ceilings := ceilingLine(c.Ceilings)
	if ceilings != "" || c.Dir != "" {
		p.emit("")
		p.at(contentEdge, ceilings, tokens.TextTertiary, width)
		if c.Dir != "" {
			p.atPath(contentEdge, c.Dir, width)
		}
	}
	if len(c.Steps) > 0 {
		p.emit("")
		for i := range c.Steps {
			p.step(i+1, c.Steps[i], width)
		}
	}
	if len(c.History) > 0 {
		p.emit("")
		for i := range c.History {
			v := c.History[i]
			// The version rides the NAME rather than the gutter: `v3` is two
			// cells and the gutter is one marker plus its space, so a version
			// in the marker slot would push every subject one cell off the
			// grid (§20's "never three spaces, never one").
			p.row(rowLine{
				depth: 1,
				name:  version(v.Version) + " " + firstLine(v.Subject), tok: tokens.TextSecondary,
				receipt: since(v.When, state.Now),
			}, width)
		}
	}
	p.detailVerbs(fallbackVerbs(CraftVerbs, c.Verbs, state.Visitor), width)
}

// step is one leaf of a workflow: its ordinal in the gutter of its own indent
// step (§20), what it does at E1, and — riding the name in parentheses — what it
// waits on, what runs it, and whether it checks its own work.
//
// The ordinal is CONTENT and not a label (§15): a workflow's steps are ordered
// and the number is how the step after it names it ("after 1"), so deleting the
// number would delete the reference too.
func (p *Page) step(n int, s CraftStep, width int) {
	var buf [24]byte
	ordinal := string(strconv.AppendInt(buf[:0], int64(n), 10))
	var rec clause
	if len(s.Needs) > 0 {
		rec.add("after " + joinNeeds(s.Needs))
	}
	rec.add(s.Model)
	rec.add(s.Skill)
	if s.Verify {
		rec.add("verify")
	}
	p.row(rowLine{
		glyph: ordinal, gtok: tokens.TextTertiary, depth: 1,
		name: firstLine(s.Brief), tok: tokens.TextSecondary,
		receipt: rec.String(),
	}, width)
}

// joinNeeds spells a step's upstreams the way the design sketch does — `1·3`,
// the telemetry separator with no spaces, because the pair is one cell of
// meaning and spacing it out would read as two receipts.
func joinNeeds(needs []string) string {
	out := ""
	for _, need := range needs {
		if need == "" {
			continue
		}
		if out != "" {
			out += tokens.GlyphSeparator
		}
		out += need
	}
	return out
}

// ceilingLine is what a run may spend before it is stopped. Absent bounds render
// as absence: a workflow with no ceiling is not a workflow with a ceiling of
// zero, and the difference is the whole of 12.9.2.
func ceilingLine(c CraftCeilings) string {
	var cl clause
	if c.HasCost {
		cl.add(tokens.Money(c.CostUSD))
	}
	if c.WallClock > 0 {
		cl.add(reltime.Elapsed(c.WallClock))
	}
	if cl.String() == "" {
		return ""
	}
	return "ceilings " + cl.String()
}

// skillDetail is one forged tool opened: what it does, where it lives, and — for
// a retired one — the store's own sentence about why it stopped being offered.
func (p *Page) skillDetail(state State, s Skill, width int) {
	p.trail(SectionKnowhow, s.Name, width)
	p.emit("")
	glyph, gtok := p.lifeGlyph(LifeQueued, false)
	p.row(rowLine{
		glyph: glyph, gtok: gtok,
		name: firstLine(s.Name), tok: tokens.TextPrimary, struck: s.Retired,
		receipt: skillReceipt(s, state.Now),
	}, width)
	if s.Body != "" {
		p.emit("")
		p.body(s.Body, width)
	}
	if s.Path != "" || s.Note != "" {
		p.emit("")
		if s.Path != "" {
			p.atPath(contentEdge, s.Path, width)
		}
		p.at(contentEdge, firstLine(s.Note), tokens.TextTertiary, width)
	}
	p.detailVerbs(fallbackVerbs(SkillVerbs, s.Verbs, state.Visitor), width)
}

// questionDetail is one knowledge gap opened: where it is in its lifecycle, what
// drilling it has cost, and what each round changed.
//
// It draws NO VERBS, and that is notebook-split.md §3's decision rather than an
// omission: a question is steered conversationally. There is no button for
// "answer this" because the answer is a sentence, and the composer under the
// page is where sentences go.
func (p *Page) questionDetail(state State, q Question, width int) {
	p.trail(SectionPractice, firstLine(q.Body), width)
	p.emit("")
	glyph, gtok := p.lifeGlyph(q.Life.life(), false)
	p.claim(glyph, gtok, q.Body, questionReceipt(q, state.Now), false, width)
	if q.Scope != "" || q.Note != "" {
		p.emit("")
		if q.Scope != "" {
			p.at(contentEdge, "about "+q.Scope, tokens.TextTertiary, width)
		}
		p.at(contentEdge, firstLine(q.Note), tokens.TextTertiary, width)
	}
	if len(q.Attempts) == 0 {
		return
	}
	p.emit("")
	for i := range q.Attempts {
		a := q.Attempts[i]
		var c clause
		if a.HasCost {
			c.add(tokens.Money(a.CostUSD))
		}
		if a.HasDelta {
			c.add("surprise " + delta(a.Delta))
		}
		p.row(rowLine{
			depth: 1,
			name:  c.String(), tok: tokens.TextSecondary,
			receipt: since(a.When, state.Now),
		}, width)
	}
}

// delta renders a surprise change with its sign kept. A round that removed
// surprise reads `-0.12`; one that added it reads `+0.06`, because a drill that
// made the world LESS predictable is the most interesting thing on the page and
// a bare magnitude would hide it.
func delta(v float64) string {
	var buf [32]byte
	out := buf[:0]
	if v > 0 {
		out = append(out, '+')
	}
	return string(strconv.AppendFloat(out, v, 'f', 2, 64))
}

// -- the shared parts --------------------------------------------------------

// trail is the detail page's first row: where this document sits, in the page's
// own words. `notebook ‹ know-how ‹ release-notes`.
//
// A TRAIL SEGMENT IS A NAME (§19). The user met this line carrying an entire
// belief — `notebook ‹ beliefs ‹ share under ticker SPCX and now trades
// publicly; as of Aug 10, 2026 it trades around $138.74. The company is no…` —
// which is a breadcrumb that has stopped pointing and started reciting. Every
// segment is capped at [trailNameCap] with a TAIL cut, because the first words
// of a sentence are what name it; the middle cut belongs to paths, where the
// filename is at the end.
//
// The SECTION word is a click target and the home word is not, which is not an
// inconsistency but the one rule §16 states about hover: everything clickable
// hovers and nothing inert wears an accent. `notebook` is where the reader
// already is — clicking it could only mean "go to the top of this page", which
// the strip above the list already does — while `know-how` is the tab this row
// came from and the way back to it.
func (p *Page) trail(s Section, here string, width int) {
	l := &p.line
	l.reset(width)
	l.add(HomeNotebook.Word(), tokens.TextTertiary)
	l.add(trailRun, tokens.TextTertiary)
	from := l.w
	tok := tokens.TextTertiary
	if p.lit(TargetSection, s.TargetID(), "") {
		tok = tokens.Promote(tok)
	}
	l.add(s.Word(), tok)
	if from < l.w {
		p.mark(TargetSection, s.TargetID(), "", from, l.w)
	}
	l.add(trailRun, tokens.TextTertiary)
	l.add(trailName(p.clean(firstLine(here))), tokens.TextSecondary)
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
}

// trailName cuts a segment down to a name.
func trailName(s string) string {
	if blocks.Width(s) <= trailNameCap {
		return s
	}
	return blocks.Truncate(s, trailNameCap)
}

// trailRun is the trail separator with its spaces: the scope glyph, which means
// "up" everywhere else in the product too (12.7).
var trailRun = " " + tokens.GlyphScopeUp + " "

// body is a paragraph of somebody else's prose at the reader's tier, wrapped to
// the content edge. It is [Page.teach]'s twin and differs in exactly one thing —
// the tier — because a teaching line is chrome and a belief is the content.
func (p *Page) body(text string, width int) {
	p.paragraph(text, tokens.TextPrimary, width)
}

// detailVerbs is the verb row at the foot of a detail page, with one blank line
// above it (§19) and at the document's own edge rather than a row's child indent.
// It is [Page.verbStrip] with the row id the page is drilled into, so a click
// comes back naming the object as well as the verb — the host runs a registry
// entry against a target and never has to re-derive which one.
func (p *Page) detailVerbs(list []Verb, width int) {
	if len(list) == 0 {
		return
	}
	p.emit("")
	p.verbStrip(list, p.detail, contentEdge, width)
}
