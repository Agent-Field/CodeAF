package homes

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE NOTEBOOK IS A FULL PAGE: three sections and a drill.
//
// # What this page is about
//
// notebook-split.md's decision is DOING versus KNOWING. The work page holds
// everything the resident is doing — tasks, standing watches, services — and
// this page holds everything it has LEARNED: what it believes, what it knows how
// to do, and what it has been practising. Standing and services used to be bands
// here and are not any more; the state types stay ([Standing], [Services]) for
// the rail rooms [View] still draws, and this surface stopped claiming them.
//
// # The API the pages lane wires (read this first)
//
// This file is the whole of the full-width surface. It does NOT take the old
// sidebar-rows shape ([Rows], [Scope], [View.Render] over a [Selection] row) —
// those stay exported and compiling for the rail until the tab-swap lands, and
// they are the OLD product. A host that renders the page needs exactly this:
//
//	p := homes.NewPage(styler)                 // nil styler is legal: no colour
//	p.SetStyler(st)                            // repaint after a focus change
//	lines := p.Render(state, sel, width, height)
//
//	p.Section()                                // which tab the page is showing
//	p.JumpTo(homes.SectionKnowhow)             // SWITCH to that tab
//	homes.SectionForKey("2")                   // the tab keys, 1..3, in draw order
//	p.ScrollBy(+1) / p.ScrollTo(n) / p.ScrollTop() / p.ScrollBottom()
//	p.Scroll() / p.MaxScroll() / p.DocLines()
//
//	p.Enter(rowID) / p.Detail() / p.Back()     // the drill (§3), and the way back
//
//	p.Targets()                                // every clickable run of the LAST frame
//	p.TargetAt(x, y)                           // pane-local cell → target
//	p.Click(x, y)                              // resolves AND applies the page's own
//	                                           // half (jump, drill); a verb comes back
//	                                           // for the host to run through the registry
//	p.SetHover(x, y) / p.ClearHover()          // both report whether the frame moved
//	p.Toggle(rowID) / p.Opened(rowID)          // a host's remembered fold state
//
// [Selection] is still the input for "where is the cursor", because the rail and
// the palette both speak it and a second locator would be a second cursor. What
// the page does with it is CHOOSE A TAB and scroll inside it: a selection landing
// in another home switches to that section, a selection landing on a row switches
// to the section the row lives in, reveals the row and gives that row its verbs.
// It never re-scopes.
//
// # The sections are TABS, not bands on one scroll (user-reported 2026-08-11)
//
// The first live build drew all three bands as one long document and the strip
// only jumped inside it. The user's reading of that: "there is just a long list
// of everything … in same page instead of separate tabs even though we have tab
// at top." So the strip SWITCHES: exactly one section is on screen, full page,
// with its own remembered scroll offset ([Page.offsets]).
//
// That change also closes the defect the user met as "I can't click know-how".
// It was never a hit-target bug — the strip's runs resolved fine. A jump was a
// SCROLL to the band's first line, and know-how's first line sits inside the last
// screenful of the document (beliefs is hundreds of rows, know-how and practice
// together are a dozen), so [Page.clampScroll] pinned the jump to MaxScroll and
// the end-of-page amendment in the old [Page.Section] lit practice. know-how was
// the only word that could fail, because it is the only one that is neither the
// top of the document nor the clamp's own answer. A tab has no offset to clamp.
//
// # The anatomy (§15, §16, §20)
//
//	beliefs 512 · know-how 9 · practice        one faint strip, at the left edge
//	───────
//
//	○ prefers tables over prose in reports (user · strong · 12d)
//	○ you keep correcting: shorter commit lines (taste · forming · 2d)
//	○ measured: you usually accept first drafts (trait · 14 samples · 1d)
//
// The band word is GONE from the body: the strip above already says which
// section this is, and §19 forbids saying a thing twice because two elements
// each wanted it. Sections render even when empty — an empty tab teaches what
// would put something in it (5.22 rule 6) rather than going blank.
//
// THE GRID (§20) is the geometry, and it is the same on every row of every tab:
// the state glyph lives in the GUTTER (cols 0–1), every row name hangs from the
// CONTENT EDGE at col 2, and a child — a verb strip, a workflow's steps — hangs
// one step in at col 4 with its own marker in the two cells before it. Nothing
// on this page starts in column 0 except chrome.
//
// RECEIPTS RIDE THEIR SUBJECT. §20 sanctions three placements and this page uses
// the first: dim, in parentheses, immediately after the name. The old form — a
// shared right column — is legal only in a narrow dense pane, and on a
// hundred-column page it put `1m · $0.09` eighty cells away from the name it
// belonged to, which is the gulf the user has now flagged twice.
//
// ONE LINE PER ROW. A row is ellipsized at its own measure (§19) and the full
// text lives in the detail page. The receipt claims its room first (5.21) but
// never more than [receiptShare] of the width, so a workflow with a long
// survival record cannot push its own name off the row.
//
// COUNTS ONLY WHERE A COUNT IS A FACT. The strip counts beliefs and know-how and
// never practice — "how many things did I drill" is a number nobody has a use
// for — and an empty collection shows nothing at all rather than a zero. The
// same rule governs every receipt on the page: a quiet day draws no day line
// instead of `$0.00 · 0 learned`, which would be three lies about a machine that
// was asleep.
//
// THE STRIP MARKS THE CURRENT TAB WITH A RULE, not with a chip and not with a
// ground. It is the SECOND hierarchy level — the page tabs above it already
// spend chips — so it is deliberately quieter: the current word is bright and
// carries an underline in the accent colour ([stripRule]), every other word
// stays a dim word with its count, and nothing at this level wears a background.
// See [Page.strip] for how it degrades.
//
// Three tiers and no more (§16's DIM RAMP): a row's name rests at SECONDARY and
// is promoted to primary by the cursor or by the pointer, every receipt and
// every piece of chrome is tertiary. The resting tier is deliberately not
// primary: §16 says pointer rest promotes the row one tier, and a row already at
// the top of the ramp has nowhere to be promoted TO — which is exactly why the
// first live build had no visible hover at all. Colour beyond the ramp appears
// only on the state glyph and on the strip's rule — one column and one
// attribute, neither of them text ink (§12).
//
// # What is NOT here
//
// No fold. Every row on this page opens a DOCUMENT instead (detail.go), because
// a belief's evidence and a workflow's steps are documents and a document
// unfolded inside a list is a list nobody can read.
//
// No second sanitiser. Prose reaches the page through [cleanFor], the same door
// the detail panes use, so the hostile fixture survives here for the same reason
// it survives there.
//
// No scroll memory across states. The page holds a scroll offset, a drill, a
// fold set and a hover; all four are presentation, and all four belong to the
// surface a person is looking at rather than to the facts.

// Section is one band of the page. The order is the draw order and it is fixed:
// a band that moved would be a band a hand has to re-find (7.2).
type Section uint8

const (
	// SectionBeliefs is what aforge holds true — beliefs, taste, traits,
	// playbooks.
	SectionBeliefs Section = iota
	// SectionKnowhow is what it learned to repeat: crafts and skills.
	SectionKnowhow
	// SectionPractice is what it drilled while nobody was watching, how
	// competent it measures itself to be, and what the day cost.
	SectionPractice
)

// sectionCount is how many tabs there are. It is the array bound for the scroll
// memory and it is derived from the last member, so adding a section cannot
// leave the table short.
const sectionCount = int(SectionPractice) + 1

// Sections returns the three in draw order.
func Sections() []Section {
	return []Section{SectionBeliefs, SectionKnowhow, SectionPractice}
}

// Valid reports whether s is one of the three.
func (s Section) Valid() bool { return s <= SectionPractice }

// Word is the section's name as it is drawn: lowercase, one word, the product's
// own vocabulary (§16's CASE rule).
func (s Section) Word() string {
	switch s {
	case SectionBeliefs:
		return "beliefs"
	case SectionKnowhow:
		return "know-how"
	case SectionPractice:
		return "practice"
	}
	return ""
}

// String names the section, "invalid" included.
func (s Section) String() string {
	if w := s.Word(); w != "" {
		return w
	}
	return "invalid"
}

// TargetID is the id a click on this section's strip word answers with.
func (s Section) TargetID() string {
	if !s.Valid() {
		return ""
	}
	return sectionTargetPrefix + s.Word()
}

// Key is the accelerator that switches to this section.
//
// They are DIGITS and not initials, and that is a decision rather than a
// shortage: "standing" and "services" share their first letter, and a strip
// where three words answered to their initial and the fourth did not would be a
// grammar the reader has to learn one exception at a time. Position is already
// the strip's organising fact — the words never move — so the ordinal is the
// honest key.
//
// The page does not DRAW them. §15 forbids a label doing structure's job, and a
// key printed beside every word on a permanent strip is four labels; the host
// publishes them the way it publishes every other accelerator, through the
// registry and the `?` sheet.
func (s Section) Key() string {
	switch s {
	case SectionBeliefs:
		return "1"
	case SectionKnowhow:
		return "2"
	case SectionPractice:
		return "3"
	}
	return ""
}

// SectionForKey resolves a jump key to its section, reporting false for anything
// else — including an initial, which is deliberately not a key here (see
// [Section.Key]).
func SectionForKey(key string) (Section, bool) {
	for _, s := range Sections() {
		if s.Key() == key {
			return s, true
		}
	}
	return 0, false
}

// SectionForHome maps a rail home onto the tab that shows the same facts, so a
// selection arriving from the rail lands on the right place.
//
// Standing and services answer FALSE, and that is the split rather than an
// omission: a charter and a service are things the resident is DOING, they live
// on the work page now (notebook-split.md §1), and a cursor resting on one has
// no tab here to be switched to. Refusing is what keeps the page from moving
// somewhere arbitrary to answer a question about another surface.
func SectionForHome(h Home) (Section, bool) {
	switch h {
	case HomeNotebook:
		return SectionBeliefs, true
	case HomeSelf:
		return SectionKnowhow, true
	}
	return 0, false
}

// SectionForRow maps a row id onto the tab that draws it. It is what lets a
// cursor arriving from the rail, the palette or a restored session land on the
// section the row is actually in rather than on whichever tab was last open —
// with tabs, "reveal this row" is first a question about WHICH page.
//
// A row id this page does not draw answers false and moves nothing.
func SectionForRow(rowID string) (Section, bool) {
	switch {
	case hasPrefix(rowID, BeliefRowPrefix):
		return SectionBeliefs, true
	case hasPrefix(rowID, CraftRowPrefix), hasPrefix(rowID, SkillRowPrefix):
		return SectionKnowhow, true
	case hasPrefix(rowID, QuestionRowPrefix):
		return SectionPractice, true
	}
	return 0, false
}

// The target namespaces. They are prefixed so a page target can never be
// confused with a rail row id or a registry entry id.
const (
	sectionTargetPrefix = "section:"
	// PracticeRowPrefix namespaces a practice row. The other prefixes already
	// existed for the rail ([BeliefRowPrefix] and friends) and the page reuses
	// them verbatim, so a row id means the same thing in both surfaces.
	PracticeRowPrefix = "practice:"
	// CraftRowPrefix, SkillRowPrefix and QuestionRowPrefix namespace the three
	// row kinds the page grew with its own sections. They are prefixes for the
	// reason scope.go's are: one cursor moves over several kinds of thing, and a
	// craft named like a skill must not be one collision away from opening the
	// wrong detail page.
	CraftRowPrefix    = "craft:"
	SkillRowPrefix    = "skill:"
	QuestionRowPrefix = "question:"
)

// TargetKind says what a click on a [Target] means, so a host can route it
// without parsing the id.
type TargetKind uint8

const (
	// TargetSection is a word on the section strip. [Page.Click] handles it.
	TargetSection TargetKind = iota
	// TargetDoor is a row that folds. The WHOLE line is the door (§10), and
	// [Page.Click] toggles it.
	TargetDoor
	// TargetVerb is an affordance on a row. The page never runs one: it hands
	// the registry id back and the host decides (5.22).
	TargetVerb
	// TargetRoom is a door onto work that happened ELSEWHERE — today only the
	// task that taught a belief. Its ID is the work's own handle and the page
	// applies nothing: opening a room is the host's job, exactly as running a
	// verb is, and this package does not know what a room is.
	//
	// It exists because §19 bans a raw id on every surface and a belief's
	// evidence used to be one (`taught by task-5381`). Once the reference has a
	// NAME it is also, unavoidably, a place — a name a reader can see and not
	// reach is worse than the id it replaced.
	TargetRoom
)

// Target is one clickable run of the last frame, in pane-local cells.
//
// From is inclusive and To is exclusive, both measured in printable columns, so
// a caller compares a pointer's x against them directly and never has to know
// that the line it is looking at is full of escape sequences. Line is the
// pane-local screen row, already translated through the scroll offset.
type Target struct {
	Kind TargetKind
	// ID is the section target id, the row id, or the registry verb id.
	ID string
	// Row is the row a verb belongs to, so a host can act on the right object
	// without re-deriving it from the cursor. Empty on sections and doors, whose
	// ID is already the object.
	Row  string
	Line int
	From int
	To   int
}

// Contains reports that a pane-local cell is inside this run.
func (t Target) Contains(x, y int) bool { return y == t.Line && x >= t.From && x < t.To }

// same reports that two targets name the same run, ignoring where it was drawn.
// It is what hover is remembered by: a pointer resting on a verb keeps that verb
// lit across a repaint that scrolled the page under it.
func (t Target) same(o Target) bool { return t.Kind == o.Kind && t.ID == o.ID && t.Row == o.Row }

// BeliefVerbs are the notebook's two affordances, in draw order, spelled with
// the registry ids notebook-split.md §4 fixes for this wave. Like
// [CharterVerbs] and [ServiceVerbs] they are IDS TO RESOLVE and not entries: the
// wiring looks each one up in the registry and fills [Belief.Verbs].
//
// The one difference, and it is the parent lane's call rather than this
// package's instinct: where the wiring has NOT filled them the page still draws
// the two words as hit targets, carrying these ids and no key. The WORD is
// [VerbWord]'s reading of the id — never the id itself, which 5.14 keeps off
// every screen. A verb with an invented KEY is the drift 5.22 exists to forbid;
// a verb with the product's own word and no key is a door the host either opens
// or refuses, exactly as it refuses a [Verb] that arrived Disabled. When the
// registry entries land, the wiring fills [Belief.Verbs] and the fallback is
// never reached.
var BeliefVerbs = []string{"belief.forget", "belief.edit"}

// receiptShare is the most of a row the receipt may claim, as a denominator.
// Half the pane: past that a cadence, a scope and a trust reading stop being the
// receipt on a row and start being the row.
const receiptShare = 2

// The grid (§20), as the two numbers every line on this page is laid against.
//
// contentEdge is where content starts and where it stays — a title, a row name,
// a body, a receipt line and a verb strip all hang from it, and the two cells in
// front of it are the GUTTER, which is chrome's: a state glyph, an ordinal, or
// nothing at all. [indentStep] (view.go, 2) is the step, so a child of a row
// hangs at contentEdge+indentStep with its own marker in the two cells before
// it. There is no third number, and a line that wanted one is a line that is
// wrong.
//
// Both numbers are the GRID's and not this page's: [blocks.ContentEdge] and
// [blocks.Depth] state them once for every surface in the product, and a page
// that spelled its own would be the fifth surface §20 was written about.
const contentEdge = blocks.ContentEdge

// childEdge is depth 1: E1 = 2 + 2·1 (§20). It is a var and not a const only
// because [blocks.Depth] is the grid's own arithmetic and a function cannot be
// folded into a constant; nothing writes to it.
var childEdge = blocks.Depth(1)

// trailNameCap is the widest a trail's last segment may be (§19: "a trail
// segment is a NAME, not a body — ellipsize any segment past ~40 cells").
//
// The defect it closes was a whole belief in a breadcrumb: `notebook ‹ beliefs ‹
// share under ticker SPCX and now trades publicly; as of Aug 10, 2026 it trades
// around $138.74. The company is no…`. A trail POINTS; the page under it says
// the thing in full.
const trailNameCap = 40

// stripRule is the colour of the rule under the current section's word.
//
// It is resolved rather than named: [tokens.ResolveToken] is the one place the
// product decides what "the accent" is, and its own comment says live resolves
// to cyan because "accent = live has to resolve to some accent". A literal here
// would be a second answer to that question.
var stripRule = tokens.ResolveToken(tokens.HueAlive, tokens.StateLive)

// Page is the homes surface as ONE full-width scrolling page.
//
// It owns the buffers a repaint needs and the three pieces of presentation state
// a page has — where it is scrolled, which rows are open, and what the pointer
// is resting on — and nothing else. It is not safe for concurrent use and is not
// meant to be: it belongs to the pane that draws with it.
type Page struct {
	profile tokens.Profile
	focus   tokens.Focus
	glyphs  tokens.GlyphSet

	open   map[string]bool
	scroll int

	// section is the TAB on screen and offsets is where each tab was left.
	// Scroll memory is per-section because a tab is a place: a reader who
	// scrolled forty beliefs down, looked at know-how and came back has not
	// asked to start again at the top.
	section Section
	offsets [sectionCount]int

	// detail is the row the page is drilled into, "" while it is showing the
	// list; listScroll is where the list was when the drill opened and pending
	// is the row a return has to reveal. All three are presentation and all
	// three are the PAGE's, for the reason [Page.Back] states.
	detail     string
	listScroll int
	pending    string
	// known is every row id the STATE holds, in every tab, and it is what
	// [Page.Enter] checks. It is deliberately not [Page.rowAt], which holds only
	// the rows the CURRENT tab drew: a reader may open a row from a section that
	// is not on screen (a rail cursor, a palette hit, a host's key), and a drill
	// that only knew the visible tab would refuse them.
	known map[string]bool

	hover   Target
	hovered bool

	seeded   bool
	lastHome Home
	lastRow  string

	// The document: every line the CURRENT TAB would draw at this width, before
	// the height budget windows it. hits carry DOC line indices while it is
	// being built; [Page.Render] translates them into screen lines.
	doc   []string
	hits  []Target
	rowAt map[string]int

	// budget is how many document lines the last frame had room for, kept so
	// [Page.MaxScroll] can answer without being told a height it was not
	// rendered at.
	budget int

	frame   []string
	targets []Target

	buf  strings.Builder
	line lineBuf
	wrap []string
}

// NewPage returns a Page painting for a Styler's profile, focus and glyph tier.
// A nil Styler means no colour and the plain tier, which is the honest
// degradation for a terminal that would not say what it can do.
func NewPage(st *tokens.Styler) *Page {
	p := &Page{}
	p.SetStyler(st)
	return p
}

// SetStyler re-points the Page. Dimming is a property of the pane (8.3), so a
// pane that just lost focus calls this and repaints.
func (p *Page) SetStyler(st *tokens.Styler) {
	if st == nil {
		p.profile, p.focus, p.glyphs = tokens.NoColor, tokens.FocusNormal, tokens.Plain
		return
	}
	p.profile, p.focus, p.glyphs = st.Profile(), st.Focus(), st.GlyphSet()
}

// Profile reports the terminal profile the Page paints for.
func (p *Page) Profile() tokens.Profile { return p.profile }

// Focus reports the pane focus the Page paints for.
func (p *Page) Focus() tokens.Focus { return p.focus }

// GlyphSet reports the repertoire tier the Page draws with (12.7).
func (p *Page) GlyphSet() tokens.GlyphSet { return p.glyphs }

// Opened reports whether a foldable row is open.
func (p *Page) Opened(rowID string) bool { return p.open[rowID] }

// Toggle opens or closes a foldable row and reports the new state. Rows that do
// not fold are unaffected by it: the page simply never draws a fold for them.
func (p *Page) Toggle(rowID string) bool {
	if rowID == "" {
		return false
	}
	if p.open == nil {
		p.open = make(map[string]bool, 4)
	}
	if p.open[rowID] {
		delete(p.open, rowID)
		return false
	}
	p.open[rowID] = true
	return true
}

// SetOpen forces a row's fold state, for a host restoring a remembered page.
func (p *Page) SetOpen(rowID string, open bool) {
	if rowID == "" {
		return
	}
	if !open {
		delete(p.open, rowID)
		return
	}
	if p.open == nil {
		p.open = make(map[string]bool, 4)
	}
	p.open[rowID] = true
}

// Scroll is the first document line the body shows.
func (p *Page) Scroll() int { return p.scroll }

// DocLines is how many lines the whole page has at the width it was last
// rendered for. It is zero before the first render.
func (p *Page) DocLines() int { return len(p.doc) }

// MaxScroll is the furthest the body can go at the size it was last rendered
// for. It is derived from the last frame rather than from a stored height,
// because the height is the compositor's to change between frames.
func (p *Page) MaxScroll() int { return p.maxScroll(p.budget) }

// ScrollTo moves the body to a document line, clamped.
func (p *Page) ScrollTo(line int) { p.scroll = p.clampScroll(line) }

// ScrollBy moves the body by a signed number of lines, clamped.
func (p *Page) ScrollBy(delta int) { p.ScrollTo(p.scroll + delta) }

// ScrollTop returns to the first line.
func (p *Page) ScrollTop() { p.scroll = 0 }

// ScrollBottom goes as far as the page goes.
func (p *Page) ScrollBottom() { p.scroll = p.MaxScroll() }

// JumpTo switches to a section. The name is the one the wiring already speaks
// (the digit keys go through it) and the act behind it is now a TAB SWAP rather
// than a scroll — see [Page.SetSection].
func (p *Page) JumpTo(s Section) { p.SetSection(s) }

// SetSection puts a tab on screen and reports whether anything moved.
//
// It does three things and each is a decision. It LEAVES ANY DRILL first,
// because a strip word is a place and a detail page is not one of the three; the
// same act reached from the trail is what makes clicking `know-how` on a detail
// page mean exactly what esc means. It REMEMBERS where the tab it is leaving was
// scrolled to and restores where the tab it is opening was left, because a tab
// is a place and a reader who scrolled forty beliefs down, looked at know-how
// and came back has not asked to start again at the top. And it drops any
// pending reveal, since a row waiting to be scrolled into view belongs to the
// section it lives in rather than to the one the reader just chose.
//
// A section the page is already showing still leaves the drill: that is the
// difference between "go to know-how" and "do nothing, you are nearly there".
func (p *Page) SetSection(s Section) bool {
	if !s.Valid() {
		return false
	}
	moved := p.Back()
	if s == p.section {
		return moved
	}
	p.offsets[p.section] = p.scroll
	p.section = s
	p.scroll = p.offsets[s]
	p.pending = ""
	p.ClearHover()
	return true
}

// Section is the tab on screen.
//
// It is a FACT THE PAGE HOLDS and no longer a reading taken off the scroll
// offset. The derived form is what made one of the three words unreachable — see
// this file's header — and it could not be fixed in place: a derivation over a
// single document cannot express "know-how, scrolled to its own last line",
// because that offset is also the answer for practice.
func (p *Page) Section() Section { return p.section }

// Targets is every clickable run of the last frame, in pane-local cells. The
// returned slice aliases the Page's buffer and is valid until the next Render.
func (p *Page) Targets() []Target { return p.targets }

// TargetAt answers which run a pane-local cell landed on. Narrow runs win: a
// verb sits inside a row whose whole line is a door (§10), and pointing at the
// verb means the verb.
func (p *Page) TargetAt(x, y int) (Target, bool) {
	best, found := Target{}, false
	for _, t := range p.targets {
		if !t.Contains(x, y) {
			continue
		}
		if !found || t.To-t.From < best.To-best.From {
			best, found = t, true
		}
	}
	return best, found
}

// SetHover lights the run a pointer is resting on and reports whether the frame
// moved. It is the only mutating call on the pointer path that is not a click:
// nothing here selects, folds, or scrolls.
//
// §16: hover promotes the run one tier, never a band. Everything that hovers is
// clickable and everything clickable hovers, which is the property that keeps a
// reader from having to learn which words do something.
func (p *Page) SetHover(x, y int) bool {
	t, ok := p.TargetAt(x, y)
	if !ok {
		return p.ClearHover()
	}
	if p.hovered && p.hover.same(t) {
		return false
	}
	p.hover, p.hovered = t, true
	return true
}

// ClearHover puts the pointer nowhere and reports whether the frame moved.
func (p *Page) ClearHover() bool {
	if !p.hovered {
		return false
	}
	p.hovered = false
	p.hover = Target{}
	return true
}

// Hover reports the run the pointer is resting on.
func (p *Page) Hover() (Target, bool) { return p.hover, p.hovered }

// Click resolves a pane-local cell and applies the half of the answer the page
// owns: a section word jumps, a door folds. Both are navigation, and navigation
// that had to round-trip through a host would be navigation that could lag the
// pointer.
//
// A verb is NOT applied. It comes back for the host to run through the registry,
// because this package does not know what a verb does and 5.22 says it must not
// learn.
func (p *Page) Click(x, y int) (Target, bool) {
	t, ok := p.TargetAt(x, y)
	if !ok {
		return Target{}, false
	}
	switch t.Kind {
	case TargetSection:
		// One act for both surfaces: on the list it swaps the tab, and on a
		// detail page the same word is the way back — leave the drill, land on
		// that tab. [Page.SetSection] does both, which is why nothing here is
		// deferred any more.
		for _, s := range Sections() {
			if s.TargetID() == t.ID {
				p.SetSection(s)
				break
			}
		}
	case TargetDoor:
		// §10's whole-line door, drilling rather than folding: the notebook's
		// rows open documents (§3), and a click anywhere on the line opens one.
		p.Enter(t.ID)
	}
	return t, true
}

// Render draws the page at a size and returns at most height lines, each at most
// width printable cells.
//
// The returned slice aliases the Page's buffer and is valid until the next
// Render. Callers that keep lines across frames must copy them.
func (p *Page) Render(state State, sel Selection, width, height int) []string {
	p.frame = p.frame[:0]
	p.targets = p.targets[:0]
	if width <= 0 || height <= 0 {
		p.doc = p.doc[:0]
		p.hits = p.hits[:0]
		p.budget = 0
		return p.frame
	}

	// THE DRILL DRAWS INSTEAD OF THE LIST, and it draws no strip: its own trail
	// line says where it is, and a strip above a trail would be the page
	// indexing itself twice. A drilled row that has left the state falls back to
	// the list, which is the honest answer — see [Page.buildDetail].
	if p.detail != "" {
		if p.buildDetail(state, width) {
			p.budget = height
			p.scroll = p.clampScroll(p.scroll)
			end := p.scroll + p.budget
			if end > len(p.doc) {
				end = len(p.doc)
			}
			p.frame = append(p.frame, p.doc[p.scroll:end]...)
			for _, t := range p.hits {
				if t.Line < p.scroll || t.Line >= end {
					continue
				}
				t.Line -= p.scroll
				p.targets = append(p.targets, t)
			}
			return p.frame
		}
		p.detail = ""
		p.scroll = p.listScroll
	}

	// WHICH TAB comes first, because the document is one tab's and laying the
	// wrong one would mean laying it twice.
	p.syncSection(sel)
	p.build(state, sel, width)

	// The strip is chrome the body scrolls under (§16's SURFACE SEAMS), so it is
	// drawn once at the top and never scrolls with the rows it indexes.
	head := 1
	if height >= 3 {
		head = 2
	}
	p.budget = height - head

	// A return from a detail lands the list back on the row it left (§3), which
	// the scroll alone cannot promise: the document may have been rebuilt at a
	// different width, or the row may have moved under a poll.
	if p.pending != "" {
		p.reveal(p.pending, p.budget)
		p.pending = ""
	}
	p.scroll = p.clampScroll(p.scroll)

	p.frame = append(p.frame, p.strip(state, width))
	if head == 2 {
		p.frame = append(p.frame, "")
	}

	end := p.scroll + p.budget
	if end > len(p.doc) {
		end = len(p.doc)
	}
	for i := p.scroll; i < end; i++ {
		p.frame = append(p.frame, p.doc[i])
	}
	for _, t := range p.hits {
		if t.Line < p.scroll || t.Line >= end {
			continue
		}
		t.Line += head - p.scroll
		p.targets = append(p.targets, t)
	}
	return p.frame
}

// syncSection answers the rail's cursor with the one thing that has to be
// decided BEFORE the document is laid: which tab this frame is.
//
// A selection in another home opens that home's tab; a selection on a row opens
// the tab the row lives in, which is the question tabs added — under one long
// scroll "reveal this row" was only ever an offset. Both act on CHANGE only, so
// a tab key and a strip click are not undone by the next repaint. The reveal
// itself is [Page.pending] and happens after the build, because a line number is
// a fact about a document that does not exist yet.
func (p *Page) syncSection(sel Selection) {
	if !p.seeded {
		p.seeded = true
		p.lastHome, p.lastRow = sel.Home, sel.Row
		if s, ok := SectionForHome(sel.Home); ok {
			p.section = s
		}
		if s, ok := SectionForRow(sel.Row); ok {
			p.section = s
		}
		p.pending = sel.Row
		return
	}
	if sel.Home != p.lastHome {
		p.lastHome = sel.Home
		if s, ok := SectionForHome(sel.Home); ok {
			p.SetSection(s)
		}
	}
	if sel.Row != p.lastRow {
		p.lastRow = sel.Row
		if s, ok := SectionForRow(sel.Row); ok {
			p.SetSection(s)
		}
		p.pending = sel.Row
	}
}

// reveal scrolls a row into the body, and does nothing when it is already there.
// A row the page does not draw — a self route, a row that left the state — moves
// nothing, because scrolling somewhere arbitrary is worse than not scrolling.
func (p *Page) reveal(rowID string, budget int) {
	if rowID == "" || budget <= 0 {
		return
	}
	line, ok := p.rowAt[rowID]
	if !ok {
		return
	}
	switch {
	case line < p.scroll:
		p.ScrollTo(line)
	case line >= p.scroll+budget:
		p.ScrollTo(line - budget + 1)
	}
}

func (p *Page) clampScroll(line int) int {
	if max := p.maxScroll(p.budget); line > max {
		line = max
	}
	if line < 0 {
		return 0
	}
	return line
}

func (p *Page) maxScroll(budget int) int {
	if budget <= 0 {
		return 0
	}
	if n := len(p.doc) - budget; n > 0 {
		return n
	}
	return 0
}

// colored reports whether the profile admits to colour. It gates the strike and
// nothing else.
func (p *Page) colored() bool { return p.profile != tokens.NoColor }

// clean is the page's door for prose somebody else wrote — the same door the
// detail panes use.
func (p *Page) clean(s string) string { return cleanFor(p.profile, s) }

func (p *Page) glyph(id tokens.GlyphID) string { return p.glyphs.Glyph(id) }

// lit reports that the pointer is resting on a particular run, so the painter
// can promote it one tier.
func (p *Page) lit(kind TargetKind, id, row string) bool {
	return p.hovered && p.hover.Kind == kind && p.hover.ID == id && p.hover.Row == row
}

// -- the document ------------------------------------------------------------

// build lays the whole page at a width. Every line is produced here, with its
// targets, so the click map cannot disagree with the picture: it is not computed
// from the same inputs, it is computed from the same ACT (the rail's finding,
// applied here).
func (p *Page) build(state State, sel Selection, width int) {
	p.doc = p.doc[:0]
	p.hits = p.hits[:0]
	if p.rowAt == nil {
		p.rowAt = make(map[string]int, 32)
		p.known = make(map[string]bool, 32)
	}
	clear(p.rowAt)
	p.learn(state)
	// ONE TAB. No band word above it: the strip a line up already says which
	// section this is, and §19 forbids a screen saying a thing twice because two
	// elements each wanted it.
	switch p.section {
	case SectionKnowhow:
		p.knowhow(state, sel, width)
	case SectionPractice:
		p.practice(state, sel, width)
	default:
		p.beliefs(state, sel, width)
	}
}

// learn records every row id the STATE holds, in every tab.
//
// It is separate from the drawing pass because the drawing pass only lays one
// tab and [Page.Enter] has to answer for all three: a rail cursor, a palette hit
// or a restored session may open a belief while know-how is on screen, and a
// drill that only knew the visible tab would refuse it and paint nothing.
func (p *Page) learn(state State) {
	clear(p.known)
	for i := range state.Notebook.Beliefs {
		p.known[BeliefRowPrefix+state.Notebook.Beliefs[i].ID] = true
	}
	for i := range state.Knowhow.Crafts {
		p.known[CraftRowPrefix+state.Knowhow.Crafts[i].ID] = true
	}
	for i := range state.Knowhow.Skills {
		p.known[SkillRowPrefix+state.Knowhow.Skills[i].ID] = true
	}
	for i := range state.Practice.Questions {
		p.known[QuestionRowPrefix+state.Practice.Questions[i].ID] = true
	}
}

// emit appends one document line.
func (p *Page) emit(line string) { p.doc = append(p.doc, line) }

// mapRow records where a row of the CURRENT tab landed, for [Page.reveal].
func (p *Page) mapRow(rowID string) { p.rowAt[rowID] = len(p.doc) }

// mark records a target on the document line that is about to be emitted.
func (p *Page) mark(kind TargetKind, id, row string, from, to int) {
	if id == "" || to <= from {
		return
	}
	p.hits = append(p.hits, Target{Kind: kind, ID: id, Row: row, Line: len(p.doc), From: from, To: to})
}

// strip is the one faint line that indexes the page, and — since the sections
// became tabs — the only thing that names the section on screen (§15: at most
// one faint lowercase word may announce a section, and this is three of them on
// one row instead of three headers scattered down a page).
//
// THE CURRENT TAB IS MARKED WITH A RULE. The word goes bright and takes an
// underline in the accent colour; every other word stays dim and keeps its
// count. It is deliberately quieter than the page tabs above it — this is the
// second hierarchy level, and a second row of chips would make the two compete —
// and it wears no ground and no pill at all.
//
// It degrades in one step, never two: a profile with an underline colour (SGR
// 58) gets the accent rule; [tokens.ANSI16] gets a plain SGR 4, which is the
// widely-supported half; [tokens.NoColor] gets no attribute, because that
// profile's promise is no escapes at all, and a pipe that wanted the tab back
// has the whole tab's contents under the strip to read it from.
//
// Counts ride the words they count and only where a count is a fact worth
// carrying: practice never has one — "how many things did I drill" is not a
// number a reader has a use for — and an empty collection shows nothing rather
// than a zero (§16's EMPTINESS).
func (p *Page) strip(state State, width int) string {
	current := p.Section()
	l := &p.line
	l.reset(width)
	// Recorded straight into the frame's target list: the strip is not part of
	// the scrolling document, so its runs need no translation.
	for _, s := range Sections() {
		if l.w > 0 {
			l.add(sepRun, tokens.TextTertiary)
		}
		from := l.w
		switch {
		case s == current:
			l.addRuled(s.Word(), tokens.TextPrimary, stripRule)
		case p.lit(TargetSection, s.TargetID(), ""):
			l.add(s.Word(), tokens.Promote(tokens.TextTertiary))
		default:
			l.add(s.Word(), tokens.TextTertiary)
		}
		if n := stripCount(state, s); n != "" {
			l.add(" ", tokens.TextTertiary)
			l.add(n, tokens.TextTertiary)
		}
		// The whole word plus its count is one target: a reader points at the
		// name of a place, not at the number beside it. A run the narrow strip
		// clipped is not a target at all — the reader cannot see it, so pointing
		// where it would have been must do nothing.
		if from < l.w {
			p.targets = append(p.targets, Target{
				Kind: TargetSection, ID: s.TargetID(), Line: 0, From: from, To: l.w,
			})
		}
	}
	return l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground)
}

// stripCount is the number beside a section's word, or "" where a count would be
// a number about nothing.
func stripCount(state State, s Section) string {
	switch s {
	case SectionBeliefs:
		n := state.Notebook.Total
		if n <= 0 {
			n = len(state.Notebook.Beliefs)
		}
		if n <= 0 {
			return ""
		}
		return count(n, state.Notebook.AtCeiling)
	case SectionKnowhow:
		// The two lists are ONE number, because the band is one question —
		// "what can you already do" — and a reader who wanted the split can
		// see it two lines down. Counting them apart on a strip would be the
		// count arguing with the section it indexes.
		if n := len(state.Knowhow.Crafts) + len(state.Knowhow.Skills); n > 0 {
			return count(n, false)
		}
	}
	return ""
}

// -- the rows ----------------------------------------------------------------

// beliefs is the first tab: what aforge holds true. Four classes of row live in
// it — a plain belief, a taste rule, a measured trait, a playbook — and they
// share one grid and one receipt grammar, because a reader scanning this tab is
// asking one question of all four.
//
// The class is carried by the RECEIPT's first word (taste / trait / playbook)
// and, where the body is not a statement on its own, by the lead the class
// supplies ("you keep correcting: …"). It is never carried by a header,
// because a header per class would be four labels doing what four words already
// do (§15).
func (p *Page) beliefs(state State, sel Selection, width int) {
	list := state.Notebook.Beliefs
	if len(list) == 0 {
		if q := state.Notebook.Query; q != "" {
			p.teach("nothing learned about \""+q+"\"", width)
			return
		}
		p.teach(RouteBeliefs.Empty(), width)
		return
	}
	for i := range list {
		b := list[i]
		rowID := BeliefRowPrefix + b.ID
		glyph, gtok := p.lifeGlyph(LifeQueued, false)
		p.mapRow(rowID)
		p.row(rowLine{
			glyph: glyph, gtok: gtok,
			name: firstLine(b.Class.Lead() + b.Body), tok: restTier(b.Retired || b.Provisional),
			struck:  b.Retired,
			receipt: beliefReceipt(b, state.Now),
			door:    rowID, focused: p.focused(rowID, sel),
		}, width)
		if p.showVerbs(rowID, sel) {
			p.verbStrip(beliefVerbs(b, state.Visitor), rowID, childEdge, width)
		}
	}
}

// restTier is where a row's name sits when nothing is pointing at it: one step
// below primary so §16's HOVER promotion has somewhere to go, and one step below
// THAT for a row whose subject is no longer load bearing.
//
// The dim step is a TIER and never the meaning (§19): every row that takes it
// also says what happened in a word in its receipt, because the user's reading
// of the first live build was "I see highlighted lines and dim lines, not sure
// what that means" — which is what a surface gets for encoding a fact in an ink.
func restTier(faded bool) tokens.Token {
	if faded {
		return tokens.TextTertiary
	}
	return tokens.TextSecondary
}

// beliefReceipt is the dim reading beside a belief, in the grammar §19 fixes:
// KIND · SCOPE · STATUS · AGE. The order is the same on every row of the tab, so
// the status word is always the second-from-last cell and the eye stops hunting
// for it.
//
// THE STATUS CELL HOLDS EXACTLY ONE WORD. "candidate" and "tentative" are the
// lifecycle and the credibility of the same line, and the user met them side by
// side in one receipt as two spellings of one fact; where a belief has a
// lifecycle to report, that wins and the credibility reading is dropped. A trait
// spends the same cell on its SAMPLE COUNT, which is the honest swap rather than
// a special case: credibility is a reading about a claim somebody made, and
// nobody made a measurement.
func beliefReceipt(b Belief, now time.Time) string {
	var c clause
	c.add(b.Class.Word())
	c.add(b.Scope)
	switch {
	case b.Retired:
		c.add("let go")
	case b.Provisional:
		c.add("candidate")
	case b.HasSamples:
		c.addInt(b.Samples, "samples")
	case b.Status != "":
		// A class with a lifecycle of its own — a taste shelf's forming/kept —
		// has already answered "how much is this trusted"; adding the
		// credibility word beside it would be the same two-spellings defect one
		// class down.
		c.add(b.Status)
	default:
		c.add(b.Trust)
	}
	c.add(since(b.Learned, now))
	return c.String()
}

// beliefVerbs is the notebook's affordance strip. See [BeliefVerbs] for why a
// missing registry entry still draws a word, and [BeliefClass.ReadOnly] for the
// one class that draws none: a trait is measured, and there is no verb for
// disagreeing with a measurement (notebook-split.md §3).
func beliefVerbs(b Belief, visitor string) []Verb {
	if b.Class.ReadOnly() {
		return nil
	}
	return fallbackVerbs(BeliefVerbs, b.Verbs, visitor)
}

// knowhow is the second tab: the shapes aforge learned to repeat and the tools
// it forged. Crafts first, then skills, adjacent with no word and no blank
// between them — adjacency is belonging (§15, §20: zero blank lines inside a
// block) and the chip word on each row says which list a row is in without a
// header claiming it.
func (p *Page) knowhow(state State, sel Selection, width int) {
	k := state.Knowhow
	if len(k.Crafts) == 0 && len(k.Skills) == 0 {
		p.teach(RouteCrafts.Empty(), width)
		return
	}
	for i := range k.Crafts {
		c := k.Crafts[i]
		rowID := CraftRowPrefix + c.ID
		glyph, gtok := p.lifeGlyph(LifeQueued, false)
		p.mapRow(rowID)
		p.row(rowLine{
			glyph: glyph, gtok: gtok,
			name: firstLine(c.Name), tok: restTier(false),
			receipt: craftReceipt(c, state.Now),
			door:    rowID, focused: p.focused(rowID, sel),
		}, width)
		if p.showVerbs(rowID, sel) {
			p.verbStrip(fallbackVerbs(CraftVerbs, c.Verbs, state.Visitor), rowID, childEdge, width)
		}
	}
	for i := range k.Skills {
		s := k.Skills[i]
		rowID := SkillRowPrefix + s.ID
		glyph, gtok := p.lifeGlyph(LifeQueued, false)
		p.mapRow(rowID)
		p.row(rowLine{
			glyph: glyph, gtok: gtok,
			name: firstLine(s.Name), tok: restTier(s.Retired), struck: s.Retired,
			receipt: skillReceipt(s, state.Now),
			door:    rowID, focused: p.focused(rowID, sel),
		}, width)
		if p.showVerbs(rowID, sel) {
			p.verbStrip(fallbackVerbs(SkillVerbs, s.Verbs, state.Visitor), rowID, childEdge, width)
		}
	}
}

// craftWord and skillWord are the two chips this tab spends. They are WORDS in
// the receipt rather than glyphs in the state column, because the state column
// says one thing product-wide (§12) and "this is a workflow" is not a state.
const (
	craftWord = "workflow"
	skillWord = "tool"
)

// craftReceipt is a workflow's reading: what kind of thing it is,
// whether it has ever proved itself, what a run costs, which version runs
// today, and how long since the last commit.
//
// A workflow that has NEVER RUN says so in words. It is the one place on this
// page where a missing measurement is spoken rather than dropped, because the
// absence is the fact: "draft, never run" is what a reader needs to know before
// they run it, and a row that merely omitted the record would read as a proven
// workflow whose numbers were off screen.
func craftReceipt(c Craft, now time.Time) string {
	var cl clause
	cl.add(craftWord)
	switch {
	case !c.HasRecord:
		cl.add("draft, never run")
	case c.Against > 0:
		cl.add(proved(c.Proved) + " against " + count(c.Against, false))
	default:
		cl.add(proved(c.Proved))
	}
	if c.HasCost {
		cl.add(tokens.Money(c.CostPerRun) + "/run")
	}
	cl.add(version(c.Version))
	cl.add(since(c.Updated, now))
	return cl.String()
}

// proved renders the survival record's winning half. The multiplication sign
// the design sketch used is deliberately not spelled: U+00D7 is
// East_Asian_Width=Ambiguous (internal/tui2/placeholder.go says so about the
// same character), so a receipt carrying it measures one cell for us and two in
// a CJK locale — which is the row overflowing on somebody else's terminal.
func proved(n int) string {
	var buf [24]byte
	out := append(buf[:0], "proved "...)
	out = strconv.AppendInt(out, int64(n), 10)
	if n == 1 {
		return string(append(out, " run"...))
	}
	return string(append(out, " runs"...))
}

// version renders `v3`, or nothing at all for a workflow with no commits
// counted — a v0 would be a version that does not exist.
func version(n int) string {
	if n <= 0 {
		return ""
	}
	var buf [24]byte
	return string(strconv.AppendInt(append(buf[:0], 'v'), int64(n), 10))
}

// skillReceipt is a tool's reading: what kind of thing it is, whether it is
// still offered, how often work has reached for it, and how long it has been
// kept. The retirement is a WORD and not only the dim tier it also wears (§19).
func skillReceipt(s Skill, now time.Time) string {
	var cl clause
	cl.add(skillWord)
	if s.Retired {
		cl.add("retired")
	}
	if s.HasUses && s.Uses > 0 {
		cl.addInt(s.Uses, "uses")
	}
	cl.add(since(s.Learned, now))
	return cl.String()
}

// practice is the third tab: the questions aforge is drilling, the one line it
// is entitled to say about its own competence, and the day receipt.
//
// The two closing lines are a BLOCK OF THEIR OWN — one blank line under the
// questions, both hanging from the content edge (§20) — rather than dim
// continuations indented under the last gap they have nothing to do with.
// Neither is a thing a reader can open: they are readings about the tab, and one
// blank line is what says so without a label saying it (§15).
func (p *Page) practice(state State, sel Selection, width int) {
	pr := state.Practice
	if len(pr.Questions) == 0 {
		p.teach(RoutePractice.Empty(), width)
	}
	for i := range pr.Questions {
		q := pr.Questions[i]
		rowID := QuestionRowPrefix + q.ID
		glyph, gtok := p.lifeGlyph(q.Life.life(), false)
		p.mapRow(rowID)
		p.row(rowLine{
			glyph: glyph, gtok: gtok,
			name: firstLine(q.Body), tok: restTier(false),
			receipt: questionReceipt(q, state.Now),
			door:    rowID, focused: p.focused(rowID, sel),
		}, width)
		// No verb strip, at any cursor: a gap is steered conversationally
		// (notebook-split.md §3), and the composer under the page is where a
		// sentence goes.
	}
	competence, day := competenceLine(pr.Competence), dayReceipt(pr.Today)
	if competence == "" && day == "" {
		return
	}
	if len(p.doc) > 0 {
		p.emit("")
	}
	if competence != "" {
		p.at(contentEdge, competence, tokens.TextTertiary, width)
	}
	if day != "" {
		p.at(contentEdge, "today "+day, tokens.TextTertiary, width)
	}
}

// questionReceipt is a gap's reading: where it is in its lifecycle,
// how many rounds it has had, what they cost, and how long ago it was written.
// A gap nobody has drilled carries no run count and no money — §16's EMPTINESS,
// and the reason a quiet day never renders `$0.00`.
func questionReceipt(q Question, now time.Time) string {
	var cl clause
	cl.add(q.Life.Word())
	if q.Runs > 0 {
		cl.addInt(q.Runs, runWord(q.Runs))
	}
	if q.HasCost {
		cl.add(tokens.Money(q.CostUSD))
	}
	cl.add(since(q.Asked, now))
	return cl.String()
}

func runWord(n int) string {
	if n == 1 {
		return "run"
	}
	return "runs"
}

// competenceLine is the one sentence aforge may say about how good it is, and
// both halves are measured (store.CompetenceMap) rather than claimed. A machine
// with nothing measured says nothing.
func competenceLine(c Competence) string {
	var cl clause
	if c.Strongest != "" {
		cl.add("strongest " + c.Strongest)
	}
	if c.Frontier != "" {
		cl.add("frontier " + c.Frontier)
	}
	return cl.String()
}

// dayReceipt is the practice tab's closing line: what the day cost, how much
// of it was practice, and what it taught.
//
// It is [today]'s clauses in the PAGE's vocabulary rather than the rail's:
// [reltime.Elapsed] where [today] spends [tokens.Duration], because a receipt on
// a page a person reads wants "42m" and the fixed-width cell wants "42m00s"
// (reltime's own package comment draws that line). Money goes through the tokens
// rungs either way, because there is only one of those.
//
// A day with nothing in it draws NOTHING — the band still has its word, and a
// receipt reading `$0.00 · 0 learned` would be three lies about a machine that
// was simply asleep.
func dayReceipt(t Today) string {
	var c clause
	if t.HasSpend {
		c.add(tokens.Money(t.SpendUSD))
	}
	if t.Practiced > 0 {
		c.add(reltime.Elapsed(t.Practiced) + " practiced")
	}
	if t.Learned > 0 {
		c.addInt(t.Learned, "learned")
	}
	return c.String()
}

// -- the line painters -------------------------------------------------------

// rowLine is one row's parts, so every tab shares one painter and therefore one
// grid (§20): a marker in the gutter, the name at the content edge, the receipt
// riding it in dim parentheses.
type rowLine struct {
	glyph string
	gtok  tokens.Token
	name  string
	tok   tokens.Token
	// struck draws the name with the strike a let-go thing wears. The words in
	// the receipt still say what happened; the strike is the echo (§19).
	struck bool
	// receipt is the dim reading that rides the name in parentheses. Never a
	// right-aligned column on this page — see [Page.row].
	receipt string
	// depth is how many indent steps in the row hangs: 0 is the content edge, 1
	// is a child (a workflow's step, an attempt).
	depth int
	// door is the row id when the WHOLE line opens something (§10). Empty when
	// the row opens nothing, which is what keeps an inert line from wearing a
	// clickable's clothes.
	door string
	// focused is the cursor resting here or the pointer resting here. It
	// promotes the name one tier, which is the whole of §16's HOVER law and the
	// thing the first live build could not do — see this file's header.
	focused bool
}

// row lays one row of any tab, on the grid and only on the grid.
//
// THE MARKER IS IN THE GUTTER AND THE NAME IS AT THE EDGE (§20). Depth n hangs
// at contentEdge+n·indentStep and its marker occupies the two cells in front of
// that; a marker too wide for its two cells keeps one space rather than eating
// the first letter of the name, which is the only place this painter gives up
// the grid and it gives it up visibly.
//
// THE RECEIPT RIDES THE NAME. §20 gives three placements and this is the first:
// dim, parenthesised, immediately after the words it is about. The old form —
// right-aligned to a column shared by the whole page — is what put `user ·
// strong · 12d` sixty cells away from the belief it described, and the user has
// now flagged that gulf twice. The receipt still claims its room BEFORE the name
// does (5.21's width-stability law) so a name growing by a character cannot push
// a number off the row, and it still never takes more than [receiptShare] of the
// pane, so a workflow with a long survival record cannot become its own row.
//
// ONE LINE, ALWAYS (§19). The name is ellipsized at whatever is left; the full
// text is one keystroke away in the detail page, and a list row that wrapped
// would cost the reader the column alignment of every row under it.
func (p *Page) row(r rowLine, width int) {
	l := &p.line
	l.reset(width)
	edge := contentEdge + r.depth*indentStep
	if r.glyph != "" {
		l.padTo(edge - indentStep)
		l.add(r.glyph, r.gtok)
	}
	if l.w >= edge {
		l.add(" ", tokens.TextTertiary)
	} else {
		l.padTo(edge)
	}

	tok := r.tok
	if r.focused {
		tok = tokens.Promote(tok)
	}
	name := p.clean(r.name)
	rec := p.clean(r.receipt)
	room, nameW := l.room(), blocks.Width(name)
	recW := 0
	if rec != "" {
		recW = blocks.Width(rec) + 3 // " (" and ")"
	}
	if nameW+recW > room {
		// THE RECEIPT YIELDS ONLY AS FAR AS HALF THE PANE. Where the name does
		// not want its half the receipt keeps it, which is what lets a short
		// name — a workflow's, a tool's — carry a long survival record without
		// the cap cutting a figure off a row that had the room for it.
		keep := room / receiptShare
		if left := room - nameW; left > keep {
			keep = left
		}
		if recW > keep {
			recW = keep
			if recW <= 3 {
				rec, recW = "", 0
			} else {
				rec = blocks.Truncate(rec, recW-3)
				recW = blocks.Width(rec) + 3
			}
		}
	}
	if avail := room - recW; blocks.Width(name) > avail {
		if avail < 0 {
			avail = 0
		}
		name = blocks.Truncate(name, avail)
	}
	if r.struck {
		l.addStruck(name, tok, p.colored())
	} else {
		l.add(name, tok)
	}
	if recW > 0 && l.room() >= recW {
		l.add(" (", tokens.TextTertiary)
		l.add(rec, tokens.TextTertiary)
		l.add(")", tokens.TextTertiary)
	}
	// §10: the whole line is the door, not a chevron on it.
	if r.door != "" {
		p.mark(TargetDoor, r.door, "", 0, width)
	}
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
}

// verbStrip draws an object's affordances at the edge its caller names — E1 for
// a list row, where the strip is the row's CHILD on the line directly under it
// with no blank between them (a parent and its children are one tight unit,
// §20), and E0 for a detail page, where the verbs are the document's own. The grammar is §16's — the verb
// at the interactive tier and its key after it, dim, one tier down — and the
// whole chip, the gap between the halves included, is one target, because a
// reader points at the words and not at the key.
//
// A disabled verb keeps its place and the strip spends its tail on the reason,
// once, for the same reason [View.verbs] does: the common case by far is one
// reason disabling every verb at the same moment — a visitor window.
func (p *Page) verbStrip(list []Verb, rowID string, edge, width int) {
	if len(list) == 0 {
		return
	}
	l := &p.line
	l.reset(width)
	l.padTo(edge)
	reason := ""
	drawn := false
	for i := range list {
		if list[i].Label == "" {
			continue
		}
		if drawn {
			l.add(sepRun, tokens.TextTertiary)
		}
		drawn = true
		from := l.w
		tok := tokens.TextSecondary
		if list[i].Disabled != "" {
			tok = tokens.TextTertiary
			if reason == "" {
				reason = list[i].Disabled
			}
		} else if p.lit(TargetVerb, list[i].ID, rowID) {
			tok = tokens.Promote(tok)
		}
		l.add(p.clean(list[i].Label), tok)
		if list[i].Key != "" {
			l.add(" ", tokens.TextTertiary)
			l.add(p.clean(list[i].Key), tokens.TextTertiary)
		}
		p.mark(TargetVerb, list[i].ID, rowID, from, l.w)
	}
	if !drawn {
		return
	}
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
	if reason != "" {
		p.at(edge, reason, tokens.TextTertiary, width)
	}
}

// claim draws a subject that is a SENTENCE rather than a name: the state glyph
// in the gutter of its first line, the sentence wrapped at the content edge, and
// its receipt on the line directly below at that same edge — §20's SECOND
// receipt placement, which is the one for a subject too long to ride.
//
// It exists because a belief has no name. Giving it a title row would mean
// inventing one out of its own first words, and then either printing the claim
// twice or printing only the cut — which is how the first live build lost the
// end of a belief on the very page that was supposed to say it in full. The
// whole claim is here, once, and the receipt sits under it rather than beside a
// truncation.
func (p *Page) claim(glyph string, gtok tokens.Token, text, receipt string, struck bool, width int) {
	text = p.clean(text)
	measure := width - contentEdge
	if measure < 1 {
		measure = width
	}
	p.wrap, _ = blocks.Wrap(p.wrap[:0], text, measure)
	for i, row := range p.wrap {
		l := &p.line
		l.reset(width)
		if i == 0 && glyph != "" {
			l.add(glyph, gtok)
		}
		if l.w >= contentEdge {
			l.add(" ", tokens.TextTertiary)
		} else {
			l.padTo(contentEdge)
		}
		if struck {
			l.addStruck(row, tokens.TextPrimary, p.colored())
		} else {
			l.add(row, tokens.TextPrimary)
		}
		p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
	}
	p.at(contentEdge, receipt, tokens.TextTertiary, width)
}

// at lays one line of chrome or receipt at a grid edge. Every caller names the
// edge it means — contentEdge or childEdge — so there is no default indent to
// drift and no line in this package that starts anywhere else.
func (p *Page) at(edge int, s string, tok tokens.Token, width int) {
	s = p.clean(s)
	if s == "" {
		return
	}
	l := &p.line
	l.reset(width)
	l.padTo(edge)
	l.add(s, tok)
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
}

// atPath is [Page.at] for an artifact (12.5.1): middle ellipsis, never a tail
// cut, because tail-truncating a path throws away the filename.
func (p *Page) atPath(edge int, path string, width int) {
	l := &p.line
	l.reset(width)
	l.padTo(edge)
	l.addPath(p.clean(path), tokens.TextTertiary)
	p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
}

// teach is an empty tab's sentence: it names the thing that would put something
// there (5.22 rule 6) rather than reporting a zero. It wraps at the content edge,
// because it is the one place on a LIST that prose is drawn rather than a row.
func (p *Page) teach(s string, width int) {
	p.paragraph(s, tokens.TextSecondary, width)
}

// paragraph wraps prose at the content edge (§20), every line of it hanging from
// the same one. It is the shared body of [Page.teach] and [Page.body]; the two
// differ in exactly one thing, the tier, because a teaching line is chrome and a
// belief is the content.
func (p *Page) paragraph(text string, tok tokens.Token, width int) {
	text = p.clean(text)
	if text == "" {
		return
	}
	measure := width - contentEdge
	if measure < 1 {
		measure = width
	}
	p.wrap, _ = blocks.Wrap(p.wrap[:0], text, measure)
	for _, row := range p.wrap {
		l := &p.line
		l.reset(width)
		l.padTo(contentEdge)
		l.add(row, tok)
		p.emit(l.emit(&p.buf, p.profile, p.focus, width, false, tokens.Ground))
	}
}

// focused reports that a row has the eye: the rail's cursor rests on it, or the
// pointer does.
func (p *Page) focused(rowID string, sel Selection) bool {
	return rowID != "" && (sel.Row == rowID || p.hoverRow() == rowID)
}

// hoverRow is the row the pointer is inside, whichever run of it the pointer
// actually landed on.
//
// A verb answers with the ROW it belongs to and not with itself, and that is
// what makes the affordance strip stable: the strip appears because the pointer
// entered the row, it is drawn on the line below, and the pointer's next step
// downward lands on the strip — which, if a verb reported no row, would close
// the strip out from under the pointer and reopen it on the following frame.
func (p *Page) hoverRow() string {
	if !p.hovered {
		return ""
	}
	switch p.hover.Kind {
	case TargetDoor:
		return p.hover.ID
	case TargetVerb:
		return p.hover.Row
	}
	return ""
}

// showVerbs decides whether a row draws its affordances: the row the cursor is
// on, or the row the pointer is resting on.
//
// HOVER IS IN THE CONDITION, and that is defect 3 taken literally — "a reader
// should never wonder whether a row is clickable". The strip appears BELOW the
// hovered row, so the row under the pointer never moves; only the rows further
// down shift, which is the calm half of the trade. Discoverability stays ambient
// (§12): the verbs belong to the row you are looking at, and a strip under every
// row of a page of forty would be forty strips nobody reads.
func (p *Page) showVerbs(rowID string, sel Selection) bool {
	return p.focused(rowID, sel)
}

// THE PAGE NO LONGER FOLDS, AND THE CHEVRON LEFT WITH THE FOLD.
//
// Every row here is a door onto a DOCUMENT (§3, [Page.Enter]), so the two
// helpers that used to reconcile "this row folds" with "this row has a state" —
// doorID and doorGlyph — have nothing left to reconcile: the glyph column
// always carries the row's own state, and the whole line always opens
// something. What tells a reader the line is a door is what §12 says it should
// be, ambient: hover promotes it, exactly as it promotes every other target on
// the page.
//
// The fold machinery itself ([Page.Toggle], [Page.Opened], [Page.SetOpen])
// stays exported and working, because it is a host's remembered state and this
// package does not get to delete a host's memory in a content wave.

// lifeGlyph resolves a lifecycle to its glyph and colour. It is the same mapping
// [View.stateGlyph] draws with, in one place, so a row means the same thing on
// the page and in the rail it came from.
func (p *Page) lifeGlyph(life Lifecycle, needs bool) (string, tokens.Token) {
	return lifeGlyph(p.glyphs, life, needs)
}

func lifeGlyph(gs tokens.GlyphSet, life Lifecycle, needs bool) (string, tokens.Token) {
	if needs {
		return gs.Glyph(tokens.GNeedsHuman), tokens.Amber
	}
	switch life {
	case LifeWorking:
		return gs.Glyph(tokens.GWorking), tokens.ResolveToken(tokens.HueAlive, tokens.StateLive)
	case LifeSettled:
		return gs.Glyph(tokens.GSettled), tokens.ResolveToken(tokens.HueMoney, tokens.StateSettled)
	case LifeFailed, LifeCancelled:
		return gs.Glyph(tokens.GFailed), tokens.ResolveToken(tokens.HueBroken, tokens.StateSettled)
	case LifePaused:
		return gs.Glyph(tokens.GPaused), tokens.TextTertiary
	}
	return gs.Glyph(tokens.GQueued), tokens.TextTertiary
}

// since is the page's reading of an instant: [reltime.Short]'s words rather than
// [tokens.Elapsed]'s fixed-width cell, because this is a page a person reads and
// not a column that re-renders every frame. A missing clock or a missing stamp
// draws NOTHING — 10.2.8: a number that has not arrived and a number that is
// zero are different facts.
func since(at, now time.Time) string {
	if at.IsZero() || now.IsZero() {
		return ""
	}
	return reltime.Short(at, now)
}

// indentRun is the continuation indent as a string, for a line that is already
// indented once and wants a second step inside its own text.
const indentRun = "  "
