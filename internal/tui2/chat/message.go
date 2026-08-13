package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// One durable journal row, dressed as one finalized block (13.1 items 1 and 2).
//
// The dressing is a PRESENTATION of the journal and never an edit of it. Every
// word on screen came out of the store; what this file decides is the tier it
// is drawn at, the glyph in front of it, whether it opens folded, and how much
// air surrounds it. A renderer that rewrote a chatty reply into a terse one
// would be lying about the record — 13.1 item 3 is explicit that the speaker is
// the head's to fix, never the transcript's.
//
// One block type serves every row kind. That is deliberate: the transcript's
// cache keys on (width, version) per block, and five block types would be five
// places to get finalization wrong. What differs between a receipt and a work
// card is the header, the tiers and the fold — data, not code paths.
//
// The block is finalized from birth: a journaled message does not change. It is
// therefore rendered once per (width, version) and never rebuilt, which is what
// keeps scrolling a long thread free.

// segKind says what one run inside a block is.
type segKind uint8

const (
	// segProse is markdown text, wrapped and rendered at its own tier.
	segProse segKind = iota
	// segRef is a single reference row: a glyph and the thing it points at.
	// Deliverables and cards are references (12.5.1, the artifact law) and are
	// never flattened into the sentence around them.
	segRef
	// segRule is a hairline: 5.13 allows one only at a room boundary, and an
	// arrival is the one boundary a transcript has.
	segRule
	// segGutter is quoted output: bytes a program wrote, drawn dim behind the
	// `│` gutter of §5 and never re-flowed as prose. It is the one run kind
	// that does not go through the markdown pass, because reading a directory
	// listing as markdown would invent structure in somebody else's output.
	// See [gutterRows].
	segGutter
	// segSeam is the card's ONE internal seam: a short quiet hairline between
	// what was asked and what came of it.
	//
	// IT WAS A GROUND SHIFT AND THAT IS THE THING THE READER REJECTED (user
	// review, 2026-08-11): "not a big fan of this thick black line separating —
	// maybe an elegant line but different color or something". A full-measure
	// row painted in the other card rung is a BAND, and a band the width of the
	// card reads as a bar of shadow across it rather than as a boundary — the
	// heavier the two rungs are apart, the heavier the bar.
	//
	// So the seam is now the one hairline §16's RULED LINES law already allows
	// at exactly this boundary ("the delivery/execution seam"), drawn at the
	// dimmest tier and INSET from both of the card's edges so it is a rule
	// between two halves of one card rather than a line across the whole of it.
	// It carries no ground of its own, so at 16 colours and at NoColor — where
	// no rung is drawn at all — it is still exactly the same mark, which the
	// ground shift never was.
	//
	// ONE PER CARD IS THE MAXIMUM. A card with two seams is a lattice, and a
	// lattice is the box §16 refuses wearing a different coat.
	segSeam
	// segPrompt is the head's own reading of the ask, on a job card: one line,
	// clipped, with the whole of it behind the card's fold. It is a kind of its
	// own rather than a plain prose run so [messageBlock.adoptPrompt] can tell
	// whether a card already carries one — a card evolves through many statuses
	// and must grow exactly one prompt line.
	segPrompt
)

// segment is one run inside a message.
type segment struct {
	kind segKind
	// text is the prose, or the reference's label.
	text string
	// tier is the grey-ramp token prose is drawn at (5.13's three tiers).
	tier tokens.Token
	// indent is the depth this run sits at, in cells (5.13: two per depth).
	indent int
	// glyph leads a reference row.
	glyph string
	// hue paints a reference row's glyph.
	hue blocks.Hue
	// state paints a reference row's label.
	state blocks.State
	// folded marks a run that is only drawn once the block is expanded.
	folded bool
	// peek marks the opposite: a run drawn only while the fold is SHUT.
	//
	// It exists for one row and fixes one defect. A card's prompt line is a
	// one-line preview of the reading, and the reading's whole text is the
	// folded run under it — so an opened card drew the preview and then drew the
	// same sentence again immediately below it, which is what the reporter's
	// screenshot shows. Collapsed shows the preview; expanded shows the thing;
	// never both, which is the whole of §10's expand law read honestly.
	peek bool
	// bullet leads a run with a `·` at the child indent — the assumption rows of
	// a parsed reading (§20's grid step, dim). It is a prose run and not a
	// [segRef] because what follows the mark is a sentence that may wrap, and a
	// reference row is one line by contract.
	bullet bool
	// rail draws the ▎ selection mark in the block's GUTTER for this row, without
	// moving one cell of its content (§20: the gutter is chrome, and a content
	// edge that moves under the cursor is not an edge). Only [segRef] honours
	// it, because only a reference row is ever a list entry a cursor rests on.
	rail bool
	// lead marks the run the block's [blockLead] hangs in front of: the one
	// prose run that starts where a speaker header used to sit. Exactly one
	// segment carries it, and [messageBlock.markLead] is the only thing that
	// sets it — a second lead would be a second attribution for one turn.
	lead bool
}

// blockLead is what stands before a turn's first line instead of a row naming
// who is speaking (§3b).
//
// THE ASSISTANT HAS NO LEAD AT ALL, which is the whole shape of the law: it is
// the unmarked voice — most ink, least chrome — and a transcript in which one
// party is unmarked needs no label on the other, because there are only two of
// them and the reader is one. So the lead exists for the two voices that are
// NOT the answer: the reader's own words, which wear the prompt glyph they were
// typed at, and a guest — work speaking under its job's name (13.15) — which
// wears the name, dim, because a third party in a two-party conversation is the
// one thing position alone cannot say.
//
// It hangs: the glyph sits in the gutter at column zero and the words start at
// the shared left edge, so all three voices' prose lines up in one column and
// only the gutter differs. That is §15 doing structure's job with position —
// delete the label and the reader still knows who spoke, because they can see
// where the row starts.
type blockLead struct {
	// glyph is the mark in the gutter: `›` for the reader (5.15's chat prompt,
	// the one they typed at), a state glyph for a guest.
	glyph string
	// hue paints the glyph when there is no identity behind it.
	hue blocks.Hue
	// seed is the guest's identity, so its glyph is the same pastel as its rail
	// card and its breadcrumb dot. Zero means no identity is carried.
	seed uint64
	// name is a guest's job name. Empty for the reader, whose name is the one
	// name a reader never needs told.
	name string
}

// none reports a block that carries no lead — the unmarked voice.
func (l blockLead) none() bool { return l.glyph == "" && l.name == "" }

// cells is how wide the lead is, which is also the indent the run under it
// hangs at: continuation lines sit under the words, never under the gutter.
func (l blockLead) cells() int {
	if l.none() {
		return bodyIndent
	}
	n := 0
	if l.glyph != "" {
		n += blocks.Width(l.glyph) + 1
	}
	if l.name != "" {
		n += blocks.Width(l.name) + leadGap
	}
	return n
}

// render paints the lead. The name is chrome (5.14's litmus: there is nothing a
// reader would DO with it), the glyph carries the identity pastel when the
// speaker has one.
func (l blockLead) render(st blocks.Styler, style *tokens.Styler) string {
	if l.none() {
		return ""
	}
	var out strings.Builder
	if l.glyph != "" {
		if l.seed != 0 && style != nil {
			out.WriteString(style.PaintIdentity(l.glyph, l.seed, blocks.StateSettled))
		} else {
			out.WriteString(st.Paint(l.glyph, blocks.StateChrome, l.hue))
		}
		out.WriteString(" ")
	}
	if l.name != "" {
		out.WriteString(st.Paint(l.name, blocks.StateChrome, blocks.HueNone))
		out.WriteString(strings.Repeat(" ", leadGap))
	}
	return out.String()
}

// leadGap is the air between a guest's name chip and the words after it. Two
// cells, which is the shared left edge said sideways.
const leadGap = 2

// messageBlock renders one store.Message.
type messageBlock struct {
	id    string
	seq   int64
	head  blocks.Header
	segs  []segment
	end   blocks.EndState
	style *tokens.Styler

	// collapsible marks a block whose folded runs can be opened. Only receipts
	// are, today; the flag exists so the toggle never lies about a block that
	// has nothing hidden.
	collapsible bool
	expanded    bool
	// hidden is how many rows the fold is holding, for the expand hint.
	hidden int
	// tight closes the block WITHOUT the blank row every other block ends on.
	//
	// It exists for one grammar and is named for what it does rather than for
	// who asks: the execution rows of a work record are adjacent lines within a
	// round and a blank line between rounds (§5, §15 — "blank lines are
	// boundaries; adjacency is belonging"). A block that always closed on a
	// blank could only draw the second half of that rule, so the rows would
	// read as N unrelated things instead of as one round of work. See
	// [traceBlocks], which is the only caller that sets it.
	tight bool
	// indent is the block's DEPTH in cells — header, body and fold alike —
	// which is 5.13's "indent is descent" for the two rows that genuinely
	// descend: the calls inside an opened batch, and the continuation that
	// holds the rest of a long result. Zero is flush and costs nothing.
	indent int
	// hovered says the pointer is resting on this block's fold row. It paints
	// the header one tier brighter and changes nothing else — see [Rows] and
	// 13.14's hover law, which this is the transcript's share of.
	hovered bool
	// lead is what stands before this block's first line instead of a header
	// row naming its speaker. See [blockLead]; the zero value is the unmarked
	// voice, which is the assistant and most of this transcript.
	lead blockLead
	// foldsBody marks a turn long enough to stand behind a fold: the reader's
	// own, and only the reader's own. An answer NEVER folds — §5's no-truncation
	// law is that the record is shown, and a reply the reader has to open is a
	// reply the surface decided they did not need.
	foldsBody bool
	// foldLine is where the fold's own row landed in the last render, or -1.
	// It is layout, recorded exactly where [messageBlock.rows] is and keyed by
	// the same width: a pointer resolves against the frame that is on screen.
	foldLine int
	// copyHover says the pointer is resting anywhere on this block, which is
	// what surfaces the copy chip on its first row (§3b: copy is a layer).
	copyHover bool
	// copied is the one frame of feedback after the bytes left, in the chip's
	// own cell. It is cleared by a tick, never by the next thing that happens.
	copied bool
	// chipCol is the column the chip was drawn at in the last render, or -1
	// when there was no room for it. Same discipline as [foldLine].
	chipCol int

	// user marks the reader's own turn. It is the boundary the open-question
	// count walks back to: a question stops being open the moment the reader
	// speaks again.
	user bool

	// -- the job block (§3) ---------------------------------------------------
	//
	// job is the node this row reports FOR, and machinery says the row is that
	// job's own progress rather than its delivery, its question or a person's
	// sentence. Together they are what lets consecutive status rows collapse
	// into ONE evolving block instead of a stack of near-identical siblings —
	// see [App.coalesceJob], which is the whole of §3's "evolving IN PLACE".
	job       string
	machinery bool
	// provisional says this card was drawn from its commissioning row before the
	// graph carried the task, so its TITLE is the head's own reading standing in
	// for a name the board could not answer yet. The first snapshot that can
	// name the task clears it ([messageBlock.refreshCard]).
	provisional bool
	// working says the job was still going when this row was dressed, which is
	// what makes the receipt below a LIVE cell rather than a settled one.
	working bool
	// clock, at and elapsed are the live receipt. `elapsed` is what the board
	// measured, `at` is the instant it was measured at, and the clock ages the
	// sum — so the number on this row and the number on the rail's card are one
	// reading and cannot drift apart (12.14).
	//
	// THE NUMBER TICKS AND THE GLYPH DOES NOT. §11 permits exactly three
	// motions and §18.2 spends the spinner on "transient tool rows only … never
	// a rail card, an agent row, or anything durable: a dancing glyph on a
	// long-lived object is a lie about liveness". A job block is durable — it
	// stands in the transcript for the life of the job and beyond it — so what
	// moves here is the clock, which §11 sanctions in as many words ("numbers
	// tick").
	clock      *blocks.Clock
	at         time.Time
	elapsed    time.Duration
	hasElapsed bool
	// meta is the telemetry cells the receipt line is composed from, kept so
	// the line can be recomposed on a frame without re-dressing the block; and
	// metaAt is that segment's index PLUS ONE, so the zero value means "this
	// block has no receipt line" without a constructor having to say so.
	meta   []string
	metaAt int
	// receiptInHead puts the receipt on the block's TITLE ROW instead of on a
	// line of its own — §20's placement 1, and the reason it exists here is
	// that blocks.Header now owns the choice between placement 1 and placement
	// 3 for the whole product ([blocks.receiptGulfMax]): a narrow window keeps
	// the right column, a wide one brings `(20s · $8.65)` home to the title it
	// belongs to. A card that composed its own line could not inherit that.
	//
	// It is a flag rather than a second metaAt convention because the two are
	// genuinely different placements and a sentinel value in an index is how a
	// renderer ends up with a third one nobody named.
	receiptInHead bool
	// prompt is the head's own reading of the ask — the sentence it handed the
	// task over with. It is the card's PROMPT LINE (§3's "task name and the
	// actual prompt it is using"), kept on the block so it survives every status
	// the card evolves through: a status message does not carry the reading, and
	// a card that dropped it on the first update would have shown it once and
	// never again.
	prompt string
	// frame is the [messageBlock.liveStep] the cached rows were rendered at, so
	// a frame landing inside the same second is a cache hit.
	frame int64
	// artifacts says the deliverable card already drew this row's artifact
	// references, so absorbParts must not draw them a second time. The card
	// draws them ABOVE the fold (12.5: a deliverable is referenced by its path,
	// and a path a fold is holding is a path nobody has been given); the parts
	// walk would put the same rows below the body.
	artifacts bool
	// questions is how many askbacks this row carries, for the footer's amber
	// attention column (5.16: amber only ever means a human is actually
	// needed).
	questions int
	// source is the journal row this block was dressed from. It is kept because
	// taking things out (JOURNEY 18) needs the RECORD and not the rendering: a
	// paste wants what was said, not the indents, glyphs and fold hints the
	// rendering added. One pointer per row, against a second store read on a
	// keystroke a person expects to be instant.
	source *store.Message
	// ask is the answerable question this row carries, or nil. It is what the
	// keyboard acts on (question.go), and it is held on the block rather than
	// re-derived per keystroke because the block is where the option rows were
	// drawn: the number on screen and the number a key answers cannot disagree
	// if they are the same slice.
	ask *pendingAsk
	// chosen is the option the cursor rests on, one-based, or zero for none. It
	// is the only thing about a journaled row that moves, and it moves through
	// the version counter like every other committed change (8.1.1).
	chosen int

	// -- the card dress (§4, §18.5) -------------------------------------------
	//
	// card is the ground this block stands on and whether it wears the accent
	// edge. It is a RENDERER and not a carrier: the block underneath is the same
	// messageBlock with the same door onto the task, the same copy chip, the
	// same fold and the same hover, and what the dress adds is the plane those
	// things are drawn on. §4's treatment moved without the block moving, which
	// is the only way it could move without taking that machinery with it.
	card cardDress
	// tailFold puts the fold's door on a row of its own UNDER the body — the
	// card's own tail door — instead of in the header's hint cell.
	//
	// A card's header row is already carrying a glyph, a name and a whole
	// receipt; a fold hint on the end of it is the one cell that would be
	// competing with the money for the right edge. And the reader asked for the
	// door where the text runs out, in as many words: "the result organized …
	// with expand/view-more on click". §10's expand law is unchanged — the whole
	// row is the door, and once open every row of the card closes it again.
	tailFold bool
	// phase is the birth-phase line while a task has no running work yet
	// ([jobPhase]), and breathing marks the one that carries §18.2's breathe.
	// Both are re-derived from the snapshot on every poll, never from a status
	// message: the progress narration that used to say this is the work record's
	// business now (§1).
	phase     string
	breathing bool
	// parts is the task's live subtree as the card draws it — the growing tree
	// that IS the "creating workers" display (§3: "branches show multiplicity").
	// Re-read per snapshot, like the phase.
	parts []jobPart
	// hint is the reading's closing offer, drawn dim at the card's foot in
	// §20's hints grammar.
	hint string
	// padded records that the LAST render actually laid the leading grounded
	// blank down — which is a question about the frame, not about the block: a
	// terminal too narrow for the lane, or a profile with no honest raised
	// ground, gets a card with neither. Every hit test in this file is
	// arithmetic on the block's own shape, so it reads what happened rather
	// than what was intended.
	padded bool
	// seamLine is where the card's one internal seam landed in the last render,
	// or -1. It is layout, recorded exactly where [foldLine] is and keyed by the
	// same width: the shade is painted onto the frame that is on screen.
	seamLine int
	// door is what the last hit test resolved: which of this block's doors the
	// pointer was on ([messageBlock.doorAt]). It is state on the block because
	// the pane's seam passes a BLOCK to the app and not a cell, and the app has
	// to know whether the reader aimed at the fold or at the card.
	door blockDoor

	// activity is the turn's tool calls, folded under this row when the reply
	// landed (activity.go). It is empty on every block that is not a head reply
	// with work behind it, and the state lives HERE — rather than in a map on
	// the app — so a row that scrolls off and back on comes back with the same
	// summary and the same side of its fold.
	activity []activityStep

	version  uint64
	width    int
	measured bool
	rows     []string
}

// cardDress is which of the two card treatments a block wears (§4).
//
// THE DELIVERY'S DRESS IS UNREPEATABLE AND THAT IS THE INFORMATION. §4 says the
// ground plus the `▎` accent edge "mean 'a finished answer' and nothing else may
// wear it", so this enum has exactly three values and the third one is spent.
// The commitment gets a card's SHAPE — a ground, so a reader sees at a glance
// that a thing was made — and is denied the edge, which is what keeps the
// delivery's treatment the only one of its kind. At NoColor, where neither
// ground draws at all, the `▎` is the whole difference and it is still there:
// the two are distinguishable by a printable character and by their state
// glyphs, never by colour alone.
type cardDress uint8

const (
	// dressNone is every block that is not a task's card: speech, receipts, the
	// brief, the record's own rows.
	dressNone cardDress = iota
	// dressCommitment is the task just made and still running: the quieter
	// ground, no edge.
	dressCommitment
	// dressDelivery is the finished answer: the raised ground AND the edge.
	dressDelivery
)

// cardLane is the two cells a card spends on its own gutter before its content
// starts: the accent edge (or the blank where a commitment's would be) and one
// space after it.
//
// It is §20's gutter, used for what §20 says may live there — "selection rails
// and accent edges may also live here" — and it is TWO CELLS FOR BOTH DRESSES on
// purpose. §3's law is that the commitment block "becomes the delivery card in
// place"; a lane that appeared at that moment would move every column of the
// card sideways at the one instant the reader is watching hardest.
const cardLane = 2

// cardPadRight is the cell of the card's own ground kept clear to the right of
// its longest row, so a right-aligned receipt has air behind it rather than
// ending at the plane's edge. The left side spends [cardLane] on the same job.
const cardPadRight = 1

// cardPad is how many rows the dress adds above the block's own first row: one
// grounded blank, which is §16's PADDING RHYTHM ("cards carry one leading and
// trailing blank inside their ground").
//
// It is a method rather than a constant because every hit test in this file is
// arithmetic on the block's own shape, and a dress that shifted the shape
// without telling them would put the copy chip and the fold door one row off.
func (b *messageBlock) cardPad() int {
	if b == nil || !b.padded {
		return 0
	}
	return 1
}

// grounded reports that this card actually has a plane to stand on.
//
// HONEST DEGRADATION, and it is the same gate the sheet and the composer's hug
// pass through ([tokens.Profile.SheetGround]): at 16 colours and none there is
// no raised background a renderer may trust, so the card paints none. What it
// keeps is everything that is not colour — the lane, the `▎` edge, the state
// glyph, the tiers and the spacing — which is why the two dresses are still
// told apart on a terminal with no colour at all.
//
// The two grounded blanks go with the ground, and that is why this is one
// question rather than two. They exist to keep a plane off the text above and
// below it (§16's PADDING RHYTHM); with no plane they are a second blank line
// beside the one the block already leaves, which is §20's "exactly one blank
// line between top-level blocks" broken to make room for nothing.
func (b *messageBlock) grounded() bool {
	return b != nil && b.card != dressNone && b.style != nil &&
		b.style.Profile().SheetGround()
}

// cardGround is the ONE plane this dress stands on. Every row of a card sits on
// it, the seam included — a card is one surface, and a stripe of a second rung
// across it was the bar the reader rejected (see [segSeam]).
func (d cardDress) cardGround() tokens.Token {
	if d == dressDelivery {
		return tokens.CardGroundDelivered
	}
	return tokens.CardGroundWorking
}

// seamHair draws the card's internal seam: a short dim rule, inset from both of
// the card's edges, on the card's own ground.
//
// INSET IS THE WHOLE DESIGN. A rule that ran the card's full measure would be a
// line ACROSS the card; one that starts where the card's words start and stops
// the same distance from its right edge is a rule BETWEEN two halves of it, and
// the two read completely differently at a glance. The tier is the dimmest one
// (§16's DIM RAMP — a separator is chrome and never content), so it is present
// to the eye that looks for the boundary and absent to the eye reading the
// words.
//
// A card too narrow to hold an inset rule draws none: a two-cell dash is not a
// seam, it is a smudge, and §16's EMPTINESS prefers the absence.
func seamHair(inner int, style *tokens.Styler) string {
	measure := inner - 2*bodyIndent
	if measure < seamHairFloor {
		return ""
	}
	// The stroke is [blocks.RuleMark] — §16 admits two ruled lines and one mark
	// draws both — but the INSET arithmetic stays here, because it is a fact
	// about this card's own measure and not about the rule.
	rule := strings.Repeat(blocks.RuleMark, measure)
	if style != nil {
		rule = style.PaintToken(rule, tokens.TextTertiary)
	}
	return strings.Repeat(" ", bodyIndent) + rule
}

// seamHairFloor is the shortest run of dashes that still reads as a rule.
const seamHairFloor = 8

var _ blocks.Block = (*messageBlock)(nil)

// ID is the anchor key and the cache key: the message's own sequence, so the
// reader's place survives every rebuild of the list around it.
func (b *messageBlock) ID() string { return b.id }

// IsFinalized is always true. A journaled message is a record.
func (b *messageBlock) IsFinalized() bool { return true }

// SettledRows is every row: a finalized block has no live tail.
func (b *messageBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version moves when a fold opens or closes, and once a second while a job
// block's clock is running.
//
// The second half is the ONE mutation a journaled row is allowed, and it goes
// through the version counter for the reason the counter exists: the block
// stays FINALIZED, so it is never part of the live tail and a detach can never
// eat the rows after it, and the cache still notices the change because the
// version moved (8.1.1). It is derived from the clock rather than pushed by the
// app because [blocks.Transcript.Frame] latches the clock before it refreshes —
// so within one frame the version is a constant, the rebuild happens once, and
// the strict-mode verifier that re-renders the block finds the same bytes.
func (b *messageBlock) Version() uint64 { return b.version + uint64(b.liveStep()) }

// End is how the turn that produced this message ended (12.5.2).
func (b *messageBlock) End() blocks.EndState { return b.end }

// SetExpanded opens or closes the fold and reports whether anything moved.
func (b *messageBlock) SetExpanded(open bool) bool {
	if !b.collapsible || b.expanded == open {
		return false
	}
	b.expanded = open
	// A long turn's fold draws its own row ([foldLongTurn]) because the header
	// it would have hung on is gone, so the header's hint cell stays empty —
	// otherwise a turn that was BOTH long and cut short would advertise its fold
	// twice, once beside the truncation badge and once under the words. A card's
	// A card's tail door is the same arrangement for the same reason ([foldTail]).
	// An activity fold is the third arrangement of the same kind: it draws its
	// own row ([messageBlock.activityRows]), so a reply that was ALSO cut short
	// does not advertise one fold beside its truncation badge and again below.
	if !b.foldsBody && !b.tailFold && !b.activityOwnsFold() {
		b.head.Hint = blocks.Disclose(open, b.hidden, "line", "lines")
	}
	b.measured = false
	b.version++
	return true
}

// Expanded is whether this block's fold is open. A block with nothing folded is
// never "expanded": there is no fold to be on either side of.
func (b *messageBlock) Expanded() bool { return b.collapsible && b.expanded }

// isFoldRow says whether the given intra-block line is this block's disclosure
// row — the one a click opens or closes.
//
// IT IS THE HEADER, AND ONLY THE HEADER, AND THE WHOLE OF IT. The `▸ 62 lines`
// hint is a trailing CELL of the header row (blocks.Header.Hint), not a row of
// its own, so the affordance a reader sees and the row a pointer must hit are
// the same line by construction. Asking them to hit the eight characters of the
// hint would be a target far narrower than the thing it stands for, which is
// the same reason [optionAtLine] does not consult x either.
//
// Line 0 is the header whenever there is one — [Rows] appends it first — so
// this needs no measurement and cannot drift from what was drawn.
//
// A HEADERLESS TURN PUTS THE DOOR WHERE THE FOLD IS. The reader's own long turns
// have no header to hang a hint on any more (§3b took the speaker rows out), so
// the `▸ N lines` row is its own line and that line is the target — and once the
// turn is open, EVERY row of it closes again, which is §10's expand law in full:
// "click opens; click on the line again — or anywhere inside the expanded
// region — closes". That needs the width, because where the fold row landed is a
// fact about the frame on screen; [Rows] is cached, so asking is free.
func (b *messageBlock) isFoldRow(line, width int) bool {
	if b == nil {
		return false
	}
	// THE PANE'S FOLD SEAM HANDS THE APP A BLOCK AND NOT A CELL, so the hit test
	// that resolved the cell is the one place the cell is still known. It is
	// recorded here, on the block, and read back by [App.toggleFold] one call
	// later — panes.go asks this question immediately before it calls the seam,
	// so the answer it leaves behind is the click's own row and nothing else's.
	b.door = b.doorAt(line, width)
	return b.door != doorNone
}

// blockDoor is what pointing at one row of a block MEANS.
type blockDoor uint8

const (
	// doorNone is a row that is not a target.
	doorNone blockDoor = iota
	// doorFold is the disclosure row: click opens, click again closes (§10).
	doorFold
	// doorRoom is a card's own body: the way in to the task it is about.
	doorRoom
)

// doorAt resolves one intra-block line to the door on it.
//
// THE WHOLE CARD IS A DOOR TO ITS TASK (§3: "the whole block is a click target
// to the record, from the moment it appears"), and the reader filed its absence
// in as many words — "the card has no click to go to task at all". So a card's
// rows answer [doorRoom] wherever they are not already something narrower: the
// disclosure row keeps its fold, the copy chip and an option row are resolved
// before this is ever asked (panes.go's order), and everything else on the card
// is the card.
//
// The trailing blank a block leaves behind it is not part of the card. It is the
// air BETWEEN two blocks, and a click on the gap between two things must not
// perform either of them.
func (b *messageBlock) doorAt(line, width int) blockDoor {
	if b.foldRowAt(line, width) {
		return doorFold
	}
	if b.card == dressNone || b.job == "" || line < 0 {
		return doorNone
	}
	rows := len(b.Rows(width))
	if !b.tight {
		rows--
	}
	if line >= rows {
		return doorNone
	}
	return doorRoom
}

// foldRowAt is the disclosure half of [doorAt], unchanged.
func (b *messageBlock) foldRowAt(line, width int) bool {
	if !b.collapsible {
		return false
	}
	// A REPLY'S ACTIVITY ROW IS A DOOR (activity.go), and it is asked about
	// FIRST because it is the only fold on the block that carries one: an answer
	// never folds its own words (§5), so a row with activity on it has no other
	// door to be confused with — including the header hint a CUT reply would
	// otherwise have offered.
	//
	// Its expanded region hangs BELOW the door rather than above it, which is the
	// one way it differs from the long-turn fold: the list is the disclosure, and
	// §10's law that every row of an opened region closes it again is spelled out
	// against the rows that are actually there.
	if b.activityOwnsFold() {
		b.Rows(width)
		if b.foldLine < 0 {
			return false
		}
		if b.expanded {
			return line >= b.foldLine && line <= b.foldLine+len(b.activity)
		}
		return line == b.foldLine
	}
	if b.hasHead() && !b.tailFold {
		return line == b.cardPad()
	}
	if !b.foldsBody && !b.tailFold {
		return false
	}
	b.Rows(width)
	if b.foldLine < 0 {
		return false
	}
	if b.expanded {
		return line >= 0 && line <= b.foldLine
	}
	return line == b.foldLine
}

// -- the copy chip (§3b: copy is a layer) -------------------------------------
//
// A transcript is a thing people take things out of, and until this wave the
// only doors were two chords that copy "the newest answer" and "the newest
// file" (copy.go). Neither can take THIS message — the one under the pointer,
// which is the one a reader means when they reach for the mouse at all.
//
// It is a layer and not a row: nothing is added to the frame at rest. The chip
// appears in the right edge of a message's first line while the pointer is on
// that message, in the dimmest tier, and it is gone the moment the pointer
// leaves. That is the whole of 5.22's discoverability doctrine — "hover
// promotion, never tutorials" — and the reason it costs no ink in the calm
// frame this surface is otherwise made of.

const (
	// copyWord is the chip at rest, and copiedWord is the one frame of proof
	// after the bytes left. Both lowercase: §16's CASE rule for chrome words.
	copyWord   = "copy"
	copiedWord = "copied"
	// chipGap is the air the chip needs between itself and the words it sits
	// beside, so a full line simply does not wear one.
	chipGap = 2
)

// chipLabel is the chip's word right now, or "" when the block wears none.
func (b *messageBlock) chipLabel() string {
	switch {
	case b == nil:
		return ""
	case b.copied:
		return copiedWord
	case b.copyHover:
		return copyWord
	}
	return ""
}

// wearChip right-aligns the chip on the block's first row.
//
// The right edge is a column (§16), so the chip lands at the same x on every
// message in the room and a reader who copied one row knows where the next
// one's door is without looking. A row with no room for it wears nothing: the
// words are the record and the chip is an affordance, and that order never
// inverts.
func (b *messageBlock) wearChip(rows []string, width int, st blocks.Styler) []string {
	b.chipCol = -1
	chip := b.chipLabel()
	if chip == "" || len(rows) == 0 {
		return rows
	}
	used := blocks.Width(rows[0])
	col := width - blocks.Width(chip)
	if col-used < chipGap {
		return rows
	}
	b.chipCol = col
	state := blocks.StateChrome
	if b.copied {
		// The proof reads one tier brighter than the invitation did, which is
		// how a reader sees that the word changed at all without the row moving.
		state = blocks.StateSettled
	}
	rows[0] = rows[0] + strings.Repeat(" ", col-used) + st.Paint(chip, state, blocks.HueNone)
	return rows
}

// chipAt reports that a pane-local cell is on this block's copy chip.
//
// It consults x, unlike every other hit test in this transcript, and for the
// reason those do not: an option row IS the choice and a fold row IS the
// disclosure, so the whole row stands for the thing. The chip is a second door
// on a row that already has words on it, and a click on the words is a click on
// the conversation.
func (b *messageBlock) chipAt(x, line, width int) bool {
	if b == nil || line != b.cardPad() || b.chipLabel() == "" {
		return false
	}
	b.Rows(width)
	return b.chipCol >= 0 && x >= b.chipCol
}

// SetCopyHover surfaces or withdraws the copy chip and reports whether anything
// moved. Like [SetHovered] it changes paint and nothing a keystroke can read,
// which is what makes it legal through the hover door (13.14).
func (b *messageBlock) SetCopyHover(on bool) bool {
	if b == nil || b.copyHover == on {
		return false
	}
	// A row with no words behind it — a trace line the record built, a bare
	// progress reference — offers nothing, because 5.20 rule 3 forbids naming a
	// door that opens onto nothing.
	if on && b.copyText() == "" {
		return false
	}
	b.copyHover = on
	b.measured = false
	b.version++
	return true
}

// SetCopied shows or clears the one frame of proof in the chip's own cell.
func (b *messageBlock) SetCopied(on bool) bool {
	if b == nil || b.copied == on {
		return false
	}
	b.copied = on
	b.measured = false
	b.version++
	return true
}

// copyText is what a paste of this row contains: what was SAID, not how it was
// drawn.
//
// It is read off the journal row the block was dressed from, never off the
// rendered rows — copy.go's own rule, and the reason the block keeps its
// message. The rendering carries indents, gutter glyphs, fold hints, a chip and
// escape sequences, and a person who copies a message wants none of them.
func (b *messageBlock) copyText() string {
	if b == nil || b.source == nil {
		return ""
	}
	var out strings.Builder
	for _, part := range b.source.Parts {
		if part.Kind != store.PartText {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			if out.Len() > 0 {
				out.WriteString("\n\n")
			}
			out.WriteString(text)
		}
	}
	if out.Len() > 0 {
		return out.String()
	}
	return strings.TrimSpace(b.source.Body)
}

// SetHovered lights this block's fold row for the pointer and reports whether
// anything moved. It changes one tier of paint and no state a keystroke can
// read, which is what makes it legal through the hover door (13.14: nothing
// reachable through PaneHover returns a tea.Cmd).
func (b *messageBlock) SetHovered(on bool) bool {
	if b == nil || b.hovered == on {
		return false
	}
	b.hovered = on
	b.measured = false
	b.version++
	return true
}

// hoverPaint is the styler the header is drawn with while the pointer rests on
// it: one tier brighter on the grey ramp, hues untouched.
//
// It is 5.22's rule for "a control that is also telemetry — dim at rest,
// secondary on focus" said through [tokens.Promote], the same call the rail's
// own hovered row makes (rail/paint.go). It is a promotion and never a band,
// because the band IS the cursor (5.16) and the transcript's cursor is not this.
type hoverPaint struct{ base *tokens.Styler }

func (h hoverPaint) Paint(text string, state blocks.State, hue blocks.Hue) string {
	return h.base.PaintToken(text, tokens.Promote(h.base.Token(state, hue)))
}

// Rows renders the block at width, reusing the last render when the width has
// not moved.
//
// Every block CLOSES with one blank row, unless it is [messageBlock.tight].
// That is 5.13's spacing rhythm — "one blank line between turns" — and it lives
// here rather than in the transcript because a block that knew its own position
// would be a block the cache could not reuse when the list around it changed.
// The tight exception is the same rule seen from the other side: a run of rows
// that belongs together is drawn adjacent, and the blank goes at the boundary.
//
// Trailing rather than leading, for a reason worth stating: the live region is
// assembled by the poll chain out of blocks this file does not build, so the
// only blank a streaming turn can inherit is the one the turn ABOVE it left
// behind. A leading blank would appear the instant the preview became a
// journaled reply, which is a row moving under the reader at the one moment
// they are watching most closely.
func (b *messageBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	// The cache key carries the FRAME as well as the width, because a working
	// job block's receipt is a function of the clock. Everything else in this
	// transcript answers zero for the frame, so the ordinary row is cached
	// exactly as it always was.
	frame := b.liveStep()
	if b.measured && b.width == width && b.frame == frame {
		return b.rows
	}
	if b.metaAt > 0 && b.metaAt <= len(b.segs) {
		b.segs[b.metaAt-1].text = b.receiptLine()
	}
	if b.receiptInHead {
		b.head.Receipt = b.receiptLine()
	}
	b.frame = frame
	st := b.styler()
	rows := b.rows[:0]
	// A block's own depth comes out of its width and goes in front of its rows,
	// so an indented block is a NARROWER block rather than an overflowing one —
	// the same contract blocks.TextBlock states for its own Indent.
	pad, inner := "", width
	if b.indent > 0 && b.indent < width {
		pad, inner = strings.Repeat(" ", b.indent), width-b.indent
	}
	// A CARD IS A NARROWER BLOCK INSIDE ITS OWN GUTTER, exactly as an indented
	// one is. The lane is taken out of the measure here and given back by
	// [messageBlock.dressCard] at the end, so every row the block composes —
	// header, prose, refs, fold — is measured once against the room it will
	// actually be read in, and nothing downstream has to know it is on a card.
	lane := 0
	if b.card != dressNone && inner > cardLane+cardPadRight+cardFloor {
		// THE CARD'S OWN INNER PADDING, both sides (§19: "inside an input
		// surface, text never touches the field's own edge"; the reader filed
		// the same complaint about cards — "the title and inside item does not
		// seem to have proper padding in this card"). The lane is taken off the
		// left, where the accent edge lives, and one cell is taken off the RIGHT
		// so the ground always extends past the longest row instead of a
		// receipt sitting flush against the plane's corner.
		lane, inner = cardLane, inner-cardLane-cardPadRight
	}
	if b.hasHead() {
		head := st
		// Only the HEADER promotes, and only while the pointer is on it. The
		// body under a hovered fold row is the record and not the affordance;
		// brightening it too would say the reader was pointing at the words.
		if b.hovered && b.style != nil {
			head = hoverPaint{base: b.style}
		}
		// A HEADER WITH NO GLYPH STILL STARTS AT THE CONTENT EDGE (§20).
		//
		// The gutter is two cells and it holds a MARKER; a row that has no
		// marker has an empty gutter, not a licence to begin at column zero.
		// This is the same rule the assistant's unmarked voice already obeyed
		// one level down — "the edge is what aligns, not the marker" — and the
		// header path was the one place it was not stated, so a recorder's own
		// free-form note (`contract in force: …`, `nudge`) began at column 0
		// while the `✳` sentence under it began at column 2, and the record
		// claimed two left edges.
		//
		// A header that is nothing but a truncation badge pays one cell less,
		// because the badge brings its own leading space.
		headLead := ""
		if b.head.Glyph == "" && inner > bodyIndent {
			pad := bodyIndent
			if b.head.Title == "" {
				pad--
			}
			headLead = strings.Repeat(" ", pad)
		}
		rows = append(rows, pad+headLead+b.head.Render(inner-blocks.Width(headLead), head))
	}
	// Where this block's own words start. The long-turn fold counts from here,
	// so a truncation badge or a card header is never what a fold hides.
	body := len(rows)
	b.seamLine = -1
	lead, leadCells := b.leadAt(inner, st)
	for i := range b.segs {
		seg := &b.segs[i]
		if (seg.folded && !b.expanded) || (seg.peek && b.expanded) {
			continue
		}
		at := len(rows)
		switch seg.kind {
		case segRef:
			rows = append(rows, refRow(*seg, inner, st))
		case segRule:
			rows = append(rows, hairline(inner, b.style))
		case segPrompt:
			// ONE LINE, CLIPPED AT THE ROW'S OWN MEASURE (§19). The dressing
			// caps the reading at [promptCap] so the card carries a sentence
			// rather than a paragraph, but the measure a reader actually reads
			// at is the frame's — and a preview that wrapped to three lines
			// would be the fold underneath it holding nothing worth opening.
			room := inner - seg.indent
			if room < 1 {
				// No room for a word beside the indent. §16's EMPTINESS: the
				// row is absent rather than overrunning the measure, which is
				// what an unguarded clip did — [clipCell] treats a non-positive
				// limit as "no limit" and handed back the whole sentence.
				continue
			}
			rows = append(rows, strings.Repeat(" ", seg.indent)+
				b.promptCell(blocks.Truncate(clipCell(seg.text, room), room), seg.tier))
		case segSeam:
			// The position is recorded HERE, where the row is actually
			// produced, so a fold that swallowed its neighbours cannot leave
			// the rule stranded on a row that no longer separates anything.
			b.seamLine = len(rows)
			rows = append(rows, seamHair(inner, b.style))
		case segGutter:
			rows = gutterRows(rows, seg.text, inner, seg.indent, b.style)
		default:
			indent := seg.indent
			if seg.lead {
				// The run under a lead hangs at its width, so the second line
				// of a sentence sits under the first and not under the gutter.
				indent = leadCells
			}
			rows = prose{style: b.style, base: seg.tier}.
				rows(rows, seg.text, inner, indent)
			if seg.lead && lead != "" {
				hangLead(rows[at:], lead, leadCells)
			}
			if seg.bullet {
				hangBullet(rows[at:], seg.indent, st)
			}
		}
		if pad != "" {
			for j := at; j < len(rows); j++ {
				if rows[j] != "" {
					rows[j] = pad + rows[j]
				}
			}
		}
	}
	// The live half of a card, and the only rows on it that are not made of a
	// journal row: the birth phase, the growing tree under it, and the reading's
	// closing offer. They sit after the block's own words and before the fold's
	// door, because that is where they belong in the reading order — what was
	// asked, then what is happening, then the way in.
	rows = b.phaseRows(rows, inner, st)
	rows = b.partRows(rows, inner, st)
	rows = b.hintRow(rows, inner)
	rows = b.foldLongTurn(rows, body, inner, pad)
	rows = b.foldTail(rows, inner, pad)
	// HOW THE ANSWER WAS ARRIVED AT, under the answer (activity.go). It is last
	// because it is about the turn rather than about the words, and it is empty
	// on every row that is not a reply with tool calls behind it.
	rows = b.activityRows(rows, inner, pad)
	if rule := blocks.CutRule(b.end, inner, st); rule != "" {
		rows = append(rows, rule)
	}
	rows = b.wearChip(rows, inner, st)
	rows = b.dressCard(rows, width, inner, lane, st)
	if !b.tight {
		rows = append(rows, "")
	}
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
}

// cardFloor is the measure a card's content must keep beside its own gutter
// before the lane is worth spending. Below it the dress is dropped and the block
// draws its words — the same bargain [messageBlock.leadAt] makes for a guest's
// name, and [blocks.CardBlock] for its edge: something honest beats a column of
// decoration with nothing readable beside it.
const cardFloor = 20

// dressCard lays the block's rows onto the card's plane.
//
// THIS IS THE WHOLE OF §4's TREATMENT AND IT IS A RENDERER. The block above is
// unchanged — same id, same door onto the task, same copy chip, same fold, same
// hover — and what happens here is that its rows are moved two cells right, the
// accent edge is drawn in the gutter they left, and every row is padded to the
// full measure so the ground behind them is a rectangle rather than a torn one.
// That last part is not cosmetic: a ground behind a ragged row draws a shape
// with a bite out of it, which is why [blocks.CardBlock] pads for the same
// reason.
//
// The two grounded blanks are §16's PADDING RHYTHM ("cards carry one leading and
// trailing blank inside their ground"). They are INSIDE the plane and the
// block's own separating blank stays outside it, so a card is a rectangle with
// air in it standing on a room that has air around it.
func (b *messageBlock) dressCard(rows []string, width, inner, lane int, st blocks.Styler) []string {
	b.padded = false
	if b.card == dressNone || lane == 0 {
		return rows
	}
	ground := b.card.cardGround()
	// The edge is painted as its own span for the reason [blocks.CardBlock]
	// gives: it is a glyph cell, and a glyph cell is what the tier upgrades.
	edge, gap := " ", " "
	hue := blocks.HueNone
	if b.card == dressDelivery {
		edge = blocks.AccentEdge
		switch b.head.GlyphHue {
		case blocks.HueBroken:
			// §4's failed variant: the edge goes coral, so the card reads as
			// broken from the periphery without a word being read.
			hue = blocks.HueBroken
		default:
			hue = blocks.HueMoney
		}
	}
	lead := st.Paint(edge, blocks.StateChrome, hue) + gap
	// The card's own leading and trailing air carries the edge like every other
	// row of it: an edge that stopped short of the card's corners would read as
	// an inset rule rather than as the card's left side.
	plane := inner + cardPadRight
	blank := lead + strings.Repeat(" ", plane)

	grounded := b.grounded()
	out := make([]string, 0, len(rows)+2)
	on := func(row string, plane tokens.Token) string {
		if !grounded {
			return strings.TrimRight(row, " ")
		}
		return b.style.PaintRowOn(row, plane)
	}
	if grounded {
		out = append(out, on(blank, ground))
		b.padded = true
	}
	for _, row := range rows {
		fill := plane - blocks.Width(row)
		if fill < 0 {
			row, fill = blocks.Truncate(row, plane), 0
		}
		out = append(out, on(lead+row+strings.Repeat(" ", fill), ground))
	}
	if grounded {
		out = append(out, on(blank, ground))
	}
	// The dress moved every row down by its leading blank and every column
	// right by its lane; the two hit tests that recorded a position have to be
	// told, or the copy chip and the fold door answer one row and two cells off.
	if b.foldLine >= 0 {
		b.foldLine += b.cardPad()
	}
	// The seam is a recorded position like the fold's, and it moved by the same
	// leading blank. It was NOT adjusted while the seam was a ground shade,
	// because the shade was painted inside this same loop and never looked up
	// afterwards; now that it is a row of its own it has to be findable.
	if b.seamLine >= 0 {
		b.seamLine += b.cardPad()
	}
	if b.chipCol >= 0 {
		b.chipCol += lane
	}
	return out
}

// -- the live half of a card --------------------------------------------------

// phaseRows draws the birth-phase line: what is happening to a task that has
// been made and has no running work yet.
//
// THE BREATHE IS §18.2's AND NOTHING ELSE MOVES. The frames are the size ramp
// `· • ● •` on one shape, walked up and back down at the house period (1.44s =
// 12 × 120ms), phase-locked to the shared clock so two cards on one screen show
// the same frame and a repaint inside one step produces byte-identical rows.
// §3 asks for this by name — "committed/planning: `◇ <title>` + `planning…` with
// a slow dim→bright breathe" — and it had no producer until now: the card went
// from commissioned straight to a subtree, with nothing in between saying the
// task was alive.
//
// The words carry the phase and the dot carries the liveness, which is what
// makes the row readable at NoColor and on the calm profile: `planning…` and
// `setting up · 3 parts so far` are different sentences whether or not anything
// is moving.
func (b *messageBlock) phaseRows(rows []string, width int, st blocks.Styler) []string {
	room := width - bodyIndent - partIndent
	if b.phase == "" || room < 1 {
		return rows
	}
	mark := blocks.DefaultPulse.Frames[0]
	if b.breathing && b.clock != nil {
		mark = blocks.DefaultPulse.Frames[blocks.DefaultPulse.Index(b.clock)]
	}
	// §20's grid: the mark sits in the two cells before the phase's own edge,
	// and the parts under it descend one more step. Marker column, name column,
	// one x-position each, which is §16's ALIGNMENT down the card.
	row := strings.Repeat(" ", bodyIndent) +
		st.Paint(mark, blocks.StateLive, blocks.HueAlive) + " "
	if b.style != nil {
		return append(rows, row+b.style.PaintToken(
			blocks.Truncate(b.phase, room), tokens.TextSecondary))
	}
	return append(rows, row+blocks.Truncate(b.phase, room))
}

// partRows draws the task's live subtree: one row per part, state glyph and
// name, at the child indent under the card's own content edge.
//
// THE TREE IS THE PROGRESS DISPLAY (§3, §5b). The reader asked to see workers
// being created; §3 answers that branches show multiplicity and forbids the
// word — "NO 'N workers' vocabulary anywhere in the block" — so what appears is
// the rows themselves, one at a time, as the journal lands them. The spinner is
// legal here and nowhere else on this block: §18.2 spends it on "live JOB rows
// on a list … live subtree twigs", and refuses it to the durable card above them.
func (b *messageBlock) partRows(rows []string, width int, st blocks.Styler) []string {
	room := width - bodyIndent - 2*partIndent
	if len(b.parts) == 0 || room < 1 {
		return rows
	}
	for _, part := range b.parts {
		glyph := lifeGlyph(part.Life)
		hue := blocks.HueNone
		state := blocks.StateSettled
		switch part.Life {
		case rail.LifeWorking:
			// A part in flight is the one thing on this block that turns.
			if b.clock != nil {
				glyph = b.clock.Glyph()
			}
			hue, state = blocks.HueAlive, blocks.StateLive
		case rail.LifeSettled:
			hue = blocks.HueMoney
		case rail.LifeFailed, rail.LifeCancelled:
			hue = blocks.HueBroken
		}
		row := strings.Repeat(" ", bodyIndent+partIndent) +
			st.Paint(glyph, state, hue) + " "
		name := blocks.Truncate(part.Name, room)
		if b.style != nil {
			// A twig is DIM: it is the shape of the work, not the work's own
			// words, and the card's title above it is what the eye lands on.
			name = b.style.PaintToken(name, tokens.TextTertiary)
		}
		rows = append(rows, row+name)
		rows = b.previewRow(rows, part, width, room)
	}
	return rows
}

// previewRow is the one line under a running part: what its worker last said.
//
// THE READER ASKED FOR IT BY ITS ABSENCE — "no 1 line update as its running" —
// and it is the same line the record page's tree draws under the same part
// (recordtree.go), fed by the same recorder read (trace.go's
// [App.readCardTracesCmd]). One reading, two surfaces, so the card and the
// record it opens can never disagree about what a worker is doing (12.14).
//
// ONE LINE AND ONLY ONE, ellipsized at the row's own measure (§19: "sentence
// length rows ellipsize at the row's measure — the full text lives in the detail
// page"). A preview that wrapped would push every part under it down the card
// each time a worker wrote a long sentence, which is a row moving under the eye
// that is reading it (8.1.6).
//
// It hangs one step INSIDE its part, at §20's next rung, so the eye reads it as
// belonging to the row above rather than as another part.
func (b *messageBlock) previewRow(rows []string, part jobPart, width, room int) []string {
	if part.Life != rail.LifeWorking {
		return rows
	}
	text := flattenLine(part.Preview)
	if text == "" {
		return rows
	}
	indent := bodyIndent + 2*partIndent
	if room = width - indent; room < previewFloor {
		return rows
	}
	text = blocks.Truncate(text, room)
	if b.style != nil {
		text = b.style.PaintToken(text, tokens.TextTertiary)
	}
	return append(rows, strings.Repeat(" ", indent)+text)
}

// previewFloor is the measure below which a preview says nothing worth a row.
const previewFloor = 12

// partIndent is the cell a part's own glyph occupies before its name — §20's
// indent step, seen from the side that spends it on a marker.
const partIndent = blocks.IndentStep

// promptCell paints the card's one-line preview of the reading.
func (b *messageBlock) promptCell(text string, tier tokens.Token) string {
	if b.style == nil {
		return text
	}
	return b.style.PaintToken(text, tier)
}

// hintRow draws the reading's closing offer at the card's foot.
//
// §20's HINTS grammar: dim, in parentheses, attached to the end of what it
// explains rather than floating. "Correct me anytime — changing course costs
// nothing" is an offer about the whole card, so the card's last row is exactly
// where it is attached.
func (b *messageBlock) hintRow(rows []string, width int) []string {
	// It is the COMMITMENT's offer and only the commitment's: "correct me
	// anytime — changing course costs nothing" is a thing to say about work that
	// is still running, and a delivery card that said it would be offering to
	// change the course of something that has already arrived.
	if b.hint == "" || b.card != dressCommitment || width <= bodyIndent {
		return rows
	}
	row := strings.Repeat(" ", bodyIndent) +
		blocks.Truncate(b.hint, width-bodyIndent)
	if b.style == nil {
		return append(rows, row)
	}
	return append(rows, b.style.PaintToken(row, tokens.TextTertiary))
}

// foldTail puts the card's own door under its body.
//
// It is [foldLongTurn]'s door with the truncation taken out: a card's fold is
// decided at dressing time by which runs are marked folded, so what is left here
// is the affordance. §10's expand law applies in full — the row is the door, and
// once open every row of the card closes it again ([isFoldRow]).
//
// IT USED TO SAY `▸ view more (12 lines)`, in the reader's own words, and the
// words were the problem. `more` is what the chevron already says, so the row
// said it twice and spent a clause teaching a mark every other row in the
// product had already taught — §15's delete test, failed by a door. It is now
// [blocks.Disclose], which is the same door the header hint, the trace row and
// the tree branch wear: one mark, one count, one unit, no verb.
func (b *messageBlock) foldTail(rows []string, width int, pad string) []string {
	if !b.tailFold || !b.collapsible {
		return rows
	}
	hint := blocks.Disclose(b.expanded, b.hidden, "line", "lines")
	b.foldLine = len(rows)
	return append(rows, pad+b.foldRow(hint, width))
}

// hangBullet puts the `·` mark into the gutter a bulleted run left for it.
//
// It replaces rather than prepends, for [hangLead]'s reason: the run was
// measured with the mark's cells already reserved, so every row is exactly as
// wide as it promised to be and the continuation lines of a long assumption sit
// under the words rather than under the dot.
func hangBullet(rows []string, indent int, st blocks.Styler) {
	if len(rows) == 0 || indent < 2 {
		return
	}
	pad := strings.Repeat(" ", indent)
	for i, row := range rows {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if !strings.HasPrefix(row, pad) {
			return
		}
		rows[i] = strings.Repeat(" ", indent-2) +
			st.Paint(tokens.GlyphSeparator, blocks.StateChrome, blocks.HueNone) +
			" " + row[len(pad):]
		return
	}
}

// leadAt is the painted lead for this width, and the indent the run beneath it
// hangs at.
//
// A lead that would not leave a readable measure beside it is dropped and the
// run falls back to the shared left edge. That is prose.wrap's own rule for its
// gutters said one level up — "no room for the affordance: the text itself is
// what matters" — and it can only fire for a guest's name, since the reader's
// `›` costs exactly the indent it hangs in and therefore costs nothing.
func (b *messageBlock) leadAt(width int, st blocks.Styler) (string, int) {
	if b.lead.none() {
		return "", bodyIndent
	}
	cells := b.lead.cells()
	if cells >= width || (cells > bodyIndent && cells+leadFloor > width) {
		return "", bodyIndent
	}
	return b.lead.render(st, b.style), cells
}

// leadFloor is the measure a guest's name must leave beside itself before it is
// worth drawing: below it the row would be a name with three words after it.
const leadFloor = 16

// hangLead puts the lead into the gutter of a run's first drawn row, replacing
// the indent the wrapper left there.
//
// It replaces rather than prepends, which is what keeps the promise the indent
// made: every row of the run is exactly as wide as it was measured to be, and
// the lead occupies space the wrapper had already reserved. A row that does not
// start with that indent is a row the wrapper narrowed under width pressure, and
// then the lead goes rather than the words.
func hangLead(rows []string, lead string, cells int) {
	pad := strings.Repeat(" ", cells)
	for i, row := range rows {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if !strings.HasPrefix(row, pad) {
			return
		}
		rows[i] = lead + row[len(pad):]
		return
	}
}

// longTurnRows is how much of a long turn stands above its fold (§3b).
const longTurnRows = 3

// foldLongTurn stands a long turn's tail behind `▸ N lines`, and puts the `▾`
// witness under it when it is open.
//
// ONLY THE READER'S OWN TURNS. An answer is never folded — §5's law is that the
// record is what is shown, and a reply behind a chevron is a surface deciding
// for the reader what they needed. Their own words are the opposite case: they
// wrote them, they know what they say, and a pasted stack trace they sent an
// hour ago is the one thing in a transcript that is pure scroll cost.
//
// The count is of ROWS AT THIS WIDTH, because that is what the reader is looking
// at: the same message folds on an 80-column terminal and does not on a 200.
func (b *messageBlock) foldLongTurn(rows []string, body, width int, pad string) []string {
	b.foldLine = -1
	if !b.foldsBody {
		return rows
	}
	shown := len(rows) - body
	if shown <= longTurnRows {
		// Nothing is hidden at this width, so there is nothing to witness. An
		// affordance drawn here would be a fold over three visible lines.
		return rows
	}
	hint := blocks.Disclose(true, 0, "line", "lines")
	if !b.expanded {
		hint = blocks.Disclose(false, shown-longTurnRows, "line", "lines")
		rows = rows[:body+longTurnRows]
	}
	b.foldLine = len(rows)
	return append(rows, pad+b.foldRow(hint, width))
}

// foldRow draws the disclosure line under a folded turn, at the shared left
// edge so it sits under the words rather than beside them.
//
// It is dim at rest and one tier brighter under the pointer, which is 5.22's
// amendment: an interactive control may not live permanently in the dimmest
// tier, and this one rises exactly when it becomes reachable.
func (b *messageBlock) foldRow(hint string, width int) string {
	tier := tokens.TextTertiary
	if b.hovered {
		tier = tokens.Promote(tier)
	}
	row := blocks.Truncate(strings.Repeat(" ", bodyIndent)+hint, width)
	if b.style == nil {
		return row
	}
	return b.style.PaintToken(row, tier)
}

func (b *messageBlock) hasHead() bool {
	return b.head.Title != "" || b.head.Glyph != "" || b.head.Desc != "" ||
		len(b.head.Meta) > 0 || len(b.head.Badges) > 0 || b.end.Cut()
}

// styler resolves the block's painter for blocks' own seam. A nil *tokens.Styler
// is a real state (a block built before a profile was chosen) and must not
// become a typed-nil interface, which would panic on the first Paint.
func (b *messageBlock) styler() blocks.Styler {
	if b.style == nil {
		return blocks.Plain
	}
	return b.style
}

// refRow draws one reference: a glyph, a gap, and the thing being pointed at.
// A path is shortened from its middle rather than its end, because the tail of
// a path is the part that names the file.
func refRow(seg segment, width int, st blocks.Styler) string {
	if width <= 0 {
		return ""
	}
	cells, pad := 0, ""
	if seg.indent > 0 && seg.indent < width {
		cells, pad = seg.indent, strings.Repeat(" ", seg.indent)
	}
	// The selection rail takes the gutter's FIRST CELL OUT OF the indent rather
	// than standing in front of it, so a selected row is the same shape as an
	// unselected one with one chrome cell inked (§20). The measure is taken from
	// the indent and not from the painted pad, because a painted pad is mostly
	// escape bytes.
	if seg.rail && cells > 0 {
		pad = st.Paint(tokens.GlyphAccentRail, blocks.StateChrome, blocks.HueNone) +
			strings.Repeat(" ", cells-1)
	}
	room := width - cells
	lead := seg.glyph + " "
	leadWidth := blocks.Width(lead)
	if leadWidth >= room {
		return pad + st.Paint(blocks.Truncate(seg.glyph, room), blocks.StateChrome, seg.hue)
	}
	label := blocks.TruncatePath(seg.text, room-leadWidth)
	return pad + st.Paint(lead, blocks.StateChrome, seg.hue) +
		st.Paint(label, seg.state, blocks.HueNone)
}

// hairline is the one rule 5.13 permits, at the one boundary that earns it.
//
// It is [blocks.Rule] and not a run of dashes assembled here: §16 admits exactly
// two ruled lines, and both of them are drawn by the one renderer that owns the
// stroke, so a boundary in this file and a boundary on any other surface are the
// same bytes at the same tier by construction.
func hairline(width int, style *tokens.Styler) string {
	if style == nil {
		return blocks.Rule(width, nil)
	}
	return blocks.Rule(width, style)
}

// messageID is a message's stable block identity. Unsequenced rows cannot
// happen here — every block this package appends came back from the store with
// a sequence — so the id is total.
func messageID(seq int64) string { return "msg-" + strconv.FormatInt(seq, 10) }

// newMessageBlock turns one journaled message into one dressed block.
//
// Text arrives already sanitized: the poll runs every body and every text part
// through the chokepoint before the message reaches here, so nothing in this
// file has to think about escape sequences and nothing downstream has to
// sanitize twice.
//
// The dispatch below is the whole of 13.1 item 2's taxonomy, read off fields the
// journal already carries and nothing else. No regex reads a body to guess what
// kind of row it is; a row is what its columns say it is.
//
// board is the graph slice a settled deliverable is dressed from — the name,
// the money and the lifecycle a message cannot carry — and it may be nil. A
// surface with no board draws the same card without them, never an invented
// one; see delivery.go.
func newMessageBlock(message store.Message, style *tokens.Styler, board jobSource) *messageBlock {
	block := &messageBlock{
		id:     messageID(message.Seq),
		seq:    message.Seq,
		style:  style,
		source: &message,
	}
	switch {
	case message.Brief != nil:
		block.dressBrief(message)
	case isDelivery(message) && landed(message, board):
		block.dressDelivery(message, board)
	// A DELIVERY-SHAPED ROW FROM A JOB THAT HAS NOT FINISHED IS PROGRESS. See
	// [landed]: this is the branch the reported defect arrived through.
	case isDelivery(message):
		block.dressWork(message, board)
	// A NODE UNDER A ROW DOES NOT MAKE THE ROW A JOB'S. A steer is a user message
	// anchored to the worker it was aimed at (rooms.go's steerNode), and dressing
	// it as work drew the reader's own sentence as a card titled with the node it
	// was sent to, under a `$—` — "as if the user's sentence had a price"
	// (13.8 finding 6). Who SPOKE is the role's answer and the node only says
	// where; so a user's words are speech in every room they land in.
	case message.NodeID != "" && message.Role != store.RoleUser:
		block.dressWork(message, board)
	case message.Role == store.RoleSystem && message.CommandSeq != 0:
		block.dressCommission(message, board)
	case message.Role == store.RoleSystem:
		block.dressReceipt(message)
	default:
		block.dressSpeech(message)
	}
	// What the message CARRIED sits between what it said and what it produced:
	// after the body, because an attachment is shown with the words it came
	// with, and before the parts, because a part is the turn's output and a
	// reference row for an input above it keeps the reading order causal
	// (attach.go).
	block.appendAttachments(message)
	block.absorbParts(message)
	// The lead is attached last because the run it hangs in front of may have
	// arrived from the parts walk rather than from the body, and a lead marked
	// before the parts landed would have hung in front of nothing.
	block.markLead()
	block.markLongFold()
	// A question that arrived without a typed part is still a question: the
	// producer wrote its lifecycle sequence and its options into the message's
	// own columns before the parts model existed, and those columns are fields
	// like any other. What is NOT read is the body — 13.3.1's rule is that v2
	// never scans prose back into structure, and this branch does not.
	if block.questions == 0 && (message.QuestionSeq != 0 || len(message.Options) > 0) &&
		message.Role != store.RoleUser {
		block.questions++
		block.dressQuestion(message, nil)
	}
	return block
}

// landed says the job this row belongs to has actually finished.
//
// IT IS THE MISSING HALF OF [isDelivery], AND ITS ABSENCE IS THE REPORTED
// DEFECT. `isDelivery` reads four columns — a system row, anchored to a node,
// belonging to no command, with words in it — and every one of them is also
// true of a running job's progress line. Measured on the reporter's own journal:
// `task-9196` posted fifteen such rows ("preparing the repository", "reading
// the issue", "running the repository's own checks", …) and the thread dressed
// all fifteen as DELIVERY CARDS, each with the job's name as its title and a
// money cell under it, which is exactly the picture the screenshot shows.
//
// §4 is unambiguous about what that card means: the ground and the `▎` edge
// "mean 'a finished answer' and nothing else may wear it". A job that is still
// working has not produced one, and the board already knows so — the same
// lifecycle the card reads to pick its own glyph and hue. So the question is
// asked one field earlier, off the same column, and a row from a job still in
// flight is dressed as what it is: that job's own progress (§1 — "worker
// lifecycle, progress narration … is the work record's business").
//
// A board that has never heard of the node answers "landed", which keeps every
// surface with no board — a test, a host with no graph — drawing exactly what it
// drew before: the honest reading when the lifecycle is unknowable is the one
// that changes nothing.
func landed(message store.Message, board jobSource) bool {
	if board == nil {
		return true
	}
	facts, known := board.jobFacts(message.NodeID)
	if !known {
		return true
	}
	switch facts.Life {
	case rail.LifeWorking, rail.LifeQueued:
		return false
	}
	return true
}

// -- speech ------------------------------------------------------------------

// dressSpeech is the conversation itself (THREAD-UX: stream = conversation).
//
// NOBODY IS NAMED HERE ANY MORE. Until this wave every turn on both sides wore a
// header row — `› you`, `› aforge · claude-k3` — and a transcript of six turns
// spent six rows saying which of the two parties in a two-party conversation was
// speaking. §15 is the law it broke ("structure is never labeled") and its own
// test is the one that condemns it: delete the label and ask whether the reader
// still knows what they are looking at. They do. There are two voices, one of
// them is theirs, and they can see which one they typed.
//
// So the ANSWER IS UNMARKED: full-width primary prose at the shared left edge,
// no label, no glyph, no header. Most ink, least chrome — which is what the law
// means by unmarked, and what makes the reply the thing the eye lands on when
// the frame appears. The READER'S OWN WORDS are dim and hang the prompt glyph
// they typed at in the gutter: they are the short quiet ones, they are already
// known to their author, and the one thing worth marking about them is where
// each one starts, so the eye can find the questions while scrolling back. Two
// tiers, one gutter column, and the tertiary tier left over for chrome — three
// tiers on the surface, which is §16's DIM RAMP exactly.
//
// The model word goes with the header that carried it. It was meta on a row that
// no longer exists, and it is not worth a row of its own: which model answered
// is a receipt, it is on the composer's meta strip while the turn runs, and it
// is in the record for the turn that has settled. §15 again — a fact a reader
// can get when they want it does not earn a permanent line.
func (b *messageBlock) dressSpeech(message store.Message) {
	if message.Role == store.RoleUser {
		b.user = true
		b.lead = blockLead{glyph: b.style.Glyph(tokens.GPromptChat)}
		b.appendBody(message, tokens.TextSecondary, bodyIndent)
		return
	}
	b.appendBody(message, tokens.TextPrimary, bodyIndent)
}

// markLead attaches the block's lead to the first prose run it will draw, and
// to exactly one: a lead on two runs would be one turn attributed twice.
func (b *messageBlock) markLead() {
	if b.lead.none() {
		return
	}
	for i := range b.segs {
		if b.segs[i].folded {
			continue
		}
		// A card's prompt line is its first line, so the lead hangs in front of
		// THAT and not in front of the status under it.
		if b.segs[i].kind == segProse || b.segs[i].kind == segPrompt {
			b.segs[i].lead = true
			return
		}
	}
}

// markLongFold decides whether this turn is long enough to stand behind a fold.
//
// The estimate is taken at the readable measure (tokens.ProseMeasure) rather
// than at a width, because the decision is made once when the row is dressed and
// the rows are drawn at whatever width the terminal is. What the estimate
// governs is only whether the fold is OFFERED; whether it actually hides
// anything is decided at render, against the rows the reader can see
// ([foldLongTurn]) — so a turn that fits on a wide terminal shows no fold there,
// and the affordance never claims to be holding something it is not.
func (b *messageBlock) markLongFold() {
	if !b.user {
		return
	}
	lines := 0
	for i := range b.segs {
		if b.segs[i].kind != segProse || b.segs[i].folded {
			continue
		}
		lines += proseLines(b.segs[i].text, tokens.ProseMeasure-bodyIndent)
	}
	if lines <= longTurnRows {
		return
	}
	b.foldsBody, b.collapsible = true, true
}

// proseLines is how many rows text would take at a measure. It is an estimate —
// the wrapper breaks on words and this counts cells — and it is used only where
// an estimate is honest: deciding whether a fold is worth offering at all.
func proseLines(text string, measure int) int {
	if measure < 1 {
		measure = 1
	}
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		width := blocks.Width(line)
		if width == 0 {
			rows++
			continue
		}
		rows += (width + measure - 1) / measure
	}
	return rows
}

// -- receipts ----------------------------------------------------------------

// dressReceipt is 13.1 item 2's first sentence: a system receipt renders as a
// dim collapsed row, never a naked line.
//
// A receipt is a mutation's proof (5.20 rule 5) and the reader needs to know it
// happened far more often than they need to read it. So the first line becomes
// the header — the receipt's own words, never a stamp that flattened a refusal
// and a compile note into the same grey sentence — and everything under it is
// folded behind the shared expand hint.
func (b *messageBlock) dressReceipt(message store.Message) {
	summary, rest := splitHeadline(message.Body)
	b.head = blocks.Header{
		Glyph: tokens.GlyphCollapsed,
		Title: summary,
		State: blocks.StateChrome,
	}
	if rest == "" {
		return
	}
	b.collapsible = true
	b.hidden = strings.Count(rest, "\n") + 1
	b.head.Hint = blocks.Disclose(false, b.hidden, "line", "lines")
	b.segs = append(b.segs, segment{
		kind: segProse, text: rest, tier: tokens.TextSecondary,
		indent: bodyIndent, folded: true,
	})
}

// -- commissioning -----------------------------------------------------------

// dressCommission is 5.20 rule 1: when the head turns prose into work, the
// transcript shows the decision as a distinct commissioning row — never a
// silent side effect behind a chatty reply. The chat-or-work fork is THE
// ambiguity this surface exists to ink.
//
// It is drawn with the steer prompt rather than the chat prompt because that is
// exactly what it records: words that left the conversation and became work,
// one way (5.15's two prompts differ so the affordance never lies about which
// surface a draft lands in — the same distinction, one row later). The glyph is
// cyan because commissioning is the moment something became alive (5.16).
//
// The command's sequence number is not shown and never will be: 5.14's "never
// shown" tier names journal seqs explicitly.
func (b *messageBlock) dressCommission(message store.Message, board jobSource) {
	// THE COMMITMENT IS A CARD, NOT A LOG LINE. It used to render as
	// `↦ commissioned: Here's my reading: …` at the dim chrome tier — machinery
	// vocabulary (§14: "commissioned" names a mechanism) wearing the shape of a
	// receipt, for the one row that is the whole reason the reader is watching.
	// §3 says what it is: the job block, born at the position of the ask, with
	// the task's name on it and the head's reading under it, evolving in place
	// for the life of the task.
	//
	// A commissioning whose task the board cannot name keeps the old dim row.
	// That is not a fallback so much as the honest reading: with no task there
	// is no card to be the first frame of.
	b.job = jobOf(message, board)
	facts, known := jobFacts{}, false
	if board != nil && b.job != "" {
		facts, known = board.jobFacts(b.job)
	}
	if !known || strings.TrimSpace(facts.Name) == "" {
		b.dressCommitting(message)
		return
	}
	b.prompt = strings.TrimSpace(message.Body)
	b.machinery = true
	b.working = facts.Life == rail.LifeWorking || facts.Life == rail.LifeQueued
	b.elapsed, b.hasElapsed = facts.Elapsed, facts.HasElapsed
	// THE COMMITMENT WEARS A CARD'S SHAPE AND NOT ITS DRESS. It gets a ground, so
	// a reader sees at a glance that a thing was made; it is denied the `▎` edge,
	// because §4 spends that on "a finished answer" and nothing else may wear it.
	// The lane is the same two cells either way, so the card does not move
	// sideways at the moment it settles (§3: the block BECOMES the delivery card
	// in place).
	b.card, b.tailFold = dressCommitment, true
	b.wearCardHead(lifeGlyph(facts.Life), facts.Name)
	b.adoptPrompt(b.prompt)
	b.meta, b.receiptInHead = commissionCells(facts), true
	b.readBoard(board)
}

// readBoard takes the live half of a card off the snapshot: the birth phase and
// the parts that exist so far.
//
// IT IS DERIVED AND RE-DERIVED, never remembered. The card is dressed once from
// a journal row, but "how many parts exist" and "has anything started" are facts
// about the GRAPH that move without any row being written — so this runs at
// dressing and again on every snapshot that moves ([App.refreshJobCards]). A
// card that read them once would show `planning…` for the life of a task whose
// plan landed a second later, which is the picture the reader reported.
//
// The reads are optional interfaces for the reason every other board read here
// is: a surface with no graph draws the card it can draw rather than no card.
func (b *messageBlock) readBoard(board jobSource) {
	if b == nil || b.job == "" || board == nil {
		return
	}
	facts, known := board.jobFacts(b.job)
	if !known {
		return
	}
	if twigs, ok := board.(jobTwigs); ok {
		b.parts = twigs.jobParts(b.job)
	}
	phase := phaseOf(facts, b.parts)
	b.phase, b.breathing = phase.word, phase.breathing
}

// refreshCard re-takes everything on a card that is a fact about the graph:
// its state glyph, its money, its burn, and its live shape.
//
// It is the other half of [readBoard] and it runs on the one cycle where any of
// this can have moved — the snapshot the poll just loaded. Nothing here reads
// the store: [taskReceipt] and [jobParts] both answer off the scope cache the
// rail was built from, so a card and the rail card for the same task are one
// reading taken at one moment (12.14) rather than two opinions.
func (b *messageBlock) refreshCard(facts jobFacts, board jobSource) {
	if b == nil || b.card == dressNone || b.job == "" {
		return
	}
	// THE SKELETON BECOMES THE CARD, in place. A card drawn before the graph
	// caught up ([messageBlock.dressCommitting]) wears the head's own reading as
	// its title; the moment the board can name the task, the name replaces it —
	// same block, same id, same lane, so the only thing that moves is the word.
	if b.provisional && strings.TrimSpace(facts.Name) != "" {
		b.wearCardHead(lifeGlyph(facts.Life), facts.Name)
		b.provisional = false
		// The reading takes the row under the name it was standing in for. It is
		// idempotent, so a card that already carries one is untouched.
		b.adoptPrompt(b.prompt)
		b.version++
		b.measured = false
	}
	before := strings.Join(b.meta, "\x00") + "\x00" + b.phase + "\x00" + partsKey(b.parts)
	if b.card == dressDelivery {
		b.meta = nil
		if receipt := taskReceipt(facts, board, b.job); receipt != "" {
			b.head.Meta = []string{receipt}
		}
	} else {
		b.meta = commissionCells(facts)
		b.head.Glyph = lifeGlyph(facts.Life)
	}
	b.readBoard(board)
	if after := strings.Join(b.meta, "\x00") + "\x00" + b.phase + "\x00" + partsKey(b.parts); after != before {
		// A committed change moves through the version counter, which is the
		// only coherent way to change bytes the transcript has already cached
		// (8.1.1). A snapshot that changed nothing costs one string compare and
		// no repaint — the calm frame this surface is made of.
		b.version++
		b.measured = false
	}
}

// partsKey is the live subtree flattened for comparison. It is a cheap identity
// and not a hash: what it has to notice is a part arriving or a part's state
// moving, which is exactly what a glyph and a name spell out.
func partsKey(parts []jobPart) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Name)
		b.WriteByte('\x01')
		b.WriteString(string(part.Life))
		b.WriteByte('\x00')
	}
	return b.String()
}

// wearCardHead puts a job card's name on its own TITLE ROW: the state glyph in
// the gutter at column 0, the job's name at the content edge at column 2 (§20).
//
// IT IS THE SAME GRAMMAR THE DELIVERY CARD ALREADY WORE, and that is the whole
// reason it exists. §3's law is that the commitment block "becomes the delivery
// card in place", and a card whose title was an inline chip while it ran and a
// header row once it settled changed GEOMETRY at the one moment the reader is
// watching hardest — the name jumped columns and the body under it jumped with
// it. §20 is one grid for every surface, and a card cannot hold two.
//
// What the chip form also could not hold was its own first row. The lead hangs
// in front of the first prose run ([messageBlock.markLead]), so the instant a
// status arrived the card's name moved DOWN the block to sit in front of it,
// leaving the prompt stranded above the title that was supposed to introduce
// it — measured on a running task, the card read
//
//	  Here's my reading: Bring the wisp browser to parity …
//	◐ wisp-parity  preparing the repository
//
// with the marker in the middle of the block. A title row cannot do that: it is
// row zero because it is the head, not because of what is under it.
//
// §3b's name CHIP is untouched and still the law for a guest voice — work
// speaking a sentence under its job's name. This is not that: §3 gives the job
// BLOCK a title (`◇ <title>` + `planning…`), and a card is not a sentence.
func (b *messageBlock) wearCardHead(glyph, name string) {
	b.head = blocks.Header{
		Glyph:     glyph,
		GlyphHue:  blocks.HueIdentity,
		GlyphSeed: blocks.Seed(b.job),
		Title:     name,
		State:     blocks.StateSettled,
	}
}

// dressCommitting is the card's SKELETON: the frame of the commitment card,
// drawn from the commissioning row alone, for the seconds between the head
// handing work over and the graph catching up (see [creatingWord]).
//
// SAME BLOCK ID, SAME DRESS, SAME LANE, so nothing jumps when the real card
// arrives — the real one is not a different block, it is this one told the
// task's name ([messageBlock.refreshCard]). §3's law is that the job block is
// "born at the position of the ask and evolves IN PLACE for the whole life of
// the task"; this is the first frame of that life rather than a placeholder
// standing in front of it.
//
// WHAT IS ON IT IS ONLY WHAT THE ROW ITSELF CARRIES. The title is the head's own
// reading of the ask, which is the sentence the task will be made from; the
// phase says what is happening; there is no glyph state, no receipt and no
// subtree, because none of those is knowable yet and §16's EMPTINESS says an
// absent figure renders as absence. A commissioning with no command sequence
// cannot name a task at all and keeps the pre-card row below.
func (b *messageBlock) dressCommitting(message store.Message) {
	if message.CommandSeq == 0 {
		b.dressCommissionNote(message)
		return
	}
	b.job = commandTaskID(message.CommandSeq)
	b.prompt = strings.TrimSpace(message.Body)
	b.machinery = true
	b.working, b.provisional = true, true
	b.card, b.tailFold = dressCommitment, true
	b.wearCardHead(lifeGlyph(rail.LifeQueued), committingTitle(b.prompt))
	b.phase, b.breathing = creatingWord, true
	// THE READING IS NOT DRAWN TWICE. On a named card the title is the task's
	// name and the row under it is the head's reading; here the title IS the
	// reading, so a prompt row would be §19's "never say a thing twice on one
	// screen because two elements each wanted it". The zones arrive with the
	// name, in [messageBlock.refreshCard], where the title stops being the
	// sentence they belong to.
}

// dressUnstarted is the skeleton's own ending: the command it was drawn from
// settled without ever minting a task. The refusal speaks for itself in the
// thread ([receiptVoice]'s law, over in the resident) — the card's only job
// here is to stop breathing over work that is not coming.
func (b *messageBlock) dressUnstarted() {
	b.working, b.provisional, b.breathing = false, false, false
	b.phase = unstartedWord
	b.head.Glyph = lifeGlyph(rail.LifeFailed)
	b.version++
	b.measured = false
}

// committingTitle is the provisional name a skeleton wears: the head's own
// reading of the ask, clipped to a title's length.
//
// It is NEVER a node id (5.14) and never the word "task": §14 bans machinery
// vocabulary, and a card titled "task-9380" would be the never-shown tier on
// screen at the one moment the reader is watching. The reading is what the task
// will be named from, so the provisional title and the real one are the same
// sentence at two lengths rather than two different things.
func committingTitle(body string) string {
	read := parseReading(body)
	title := strings.TrimSpace(read.goal)
	if title == "" {
		title = strings.TrimSpace(splitFirst(read.plain))
	}
	if title == "" {
		title = strings.TrimSpace(splitFirst(body))
	}
	return clipCell(title, committingTitleCap)
}

// committingTitleCap is how much of the reading stands as a title. A card's name
// is a phrase; the whole reading is one row below it and behind the fold.
const committingTitleCap = 60

// dressCommissionNote is the pre-card row, kept for a commissioning whose task
// this window cannot name.
func (b *messageBlock) dressCommissionNote(message store.Message) {
	summary, rest := splitHeadline(message.Body)
	b.head = blocks.Header{
		Glyph:    tokens.GlyphPromptSteer,
		GlyphHue: blocks.HueAlive,
		Title:    "commissioned",
		Desc:     summary,
		State:    blocks.StateChrome,
	}
	if rest == "" {
		return
	}
	b.collapsible = true
	b.hidden = strings.Count(rest, "\n") + 1
	b.head.Hint = blocks.Disclose(false, b.hidden, "line", "lines")
	b.segs = append(b.segs, segment{
		kind: segProse, text: rest, tier: tokens.TextSecondary,
		indent: bodyIndent, folded: true,
	})
}

// commissionCells is the card's telemetry while the task runs: whatever the
// board can say, and nothing it cannot.
//
// Money, then the worker — the same order [taskReceipt] spells for the finished
// card, for the same reason: the figures are a column a reader scans down and a
// name in the middle of it breaks the scan. A running card has no model cell
// yet (nothing has billed under a name), so the worker is simply the last word
// on the line, exactly where the model will join it when the job delivers.
func commissionCells(facts jobFacts) []string {
	cells := make([]string, 0, 2)
	if facts.HasCost {
		cells = append(cells, tokens.Money(facts.Cost))
	} else {
		cells = append(cells, tokens.GlyphSpend+tokens.GlyphMissing)
	}
	return append(cells, harnessWords(facts)...)
}

// lifeGlyph is the state vocabulary of 5.17 read off a lifecycle.
func lifeGlyph(life rail.Lifecycle) string {
	switch life {
	case rail.LifeSettled:
		return tokens.GlyphSettled
	case rail.LifeFailed, rail.LifeCancelled:
		return tokens.GlyphFailed
	case rail.LifeQueued:
		return tokens.GlyphQueued
	}
	return tokens.GlyphWorking
}

// adoptPrompt puts the head's reading at the top of the card, as one line
// collapsed and as its own zones opened, with the card's one seam under it.
//
// It INSERTS rather than appends, because the prompt is what the card is ABOUT
// and the status under it is what is happening to it — §15's "adjacency is
// belonging" read downwards. It is idempotent and safe on a card that already
// has one: a card evolving through twenty statuses adopts the same reading
// twenty times and grows one prompt line.
//
// THE PREVIEW AND THE THING ARE NEVER BOTH ON SCREEN, which is the reported
// defect and the one line of it that matters. The collapsed row is a clip of the
// reading with §16's ellipsis on it; the opened rows are the reading itself. A
// door that showed you what was behind it AND what it was standing in front of
// would not be a door, and the screenshot shows exactly that: the ellipsized
// sentence with its own full text repeated immediately underneath.
//
// WHAT OPENS IS STRUCTURED, NOT DUMPED. The head's reading has a grammar its
// producers write in one place each ([parseReading]) — a goal, the reader's own
// words verbatim, the assumptions it made, and the offer to be corrected — and a
// card that wrapped all four into one grey paragraph was throwing away structure
// somebody had already written down. Each zone gets the dressing the law already
// has for it: the goal is the lead, the verbatim block is the reader's own voice
// (§3b: dim, `›` in the gutter), each assumption is a bullet at the child indent,
// and the offer is a hint at the foot (§20). A reading that does not answer the
// grammar keeps the plain wrapped paragraph it always had.
func (b *messageBlock) adoptPrompt(text string) {
	text = strings.TrimSpace(text)
	if b == nil || text == "" {
		return
	}
	b.prompt = text
	for i := range b.segs {
		if b.segs[i].kind == segPrompt {
			return
		}
	}
	read := parseReading(text)
	if read.plain != "" {
		text = read.plain
	}
	lead := read.goal
	if lead == "" {
		lead = splitFirst(text)
	}
	segs := make([]segment, 0, len(b.segs)+8)
	segs = append(segs, segment{
		kind: segPrompt, text: clipCell(strings.TrimSpace(lead), promptCap),
		tier: tokens.TextSecondary, indent: bodyIndent, peek: true,
	})
	opened := b.readingSegs(read, lead, text)
	segs = append(segs, opened...)
	if len(opened) > 0 {
		b.collapsible, b.tailFold = true, true
		b.hidden += len(opened)
	}
	// THE CARD'S ONE SEAM (§16). It sits between what was asked and what came of
	// it, and it is a ground-shade row rather than a rule — the law allows a
	// hairline in exactly two places and this is neither of them.
	segs = append(segs, segment{kind: segSeam})
	// The receipt line moved down by however many rows went in above it.
	if b.metaAt > 0 {
		b.metaAt += len(segs)
	}
	b.segs = append(segs, b.segs...)
	b.measured = false
}

// readingSegs is the opened form of a reading: its zones when it has them, its
// whole text when it does not.
//
// The hint is NOT a segment. It is the card's last row wherever the card ends
// (§20's hints ride with their subject, and this one's subject is the card), so
// it is held on the block and drawn by [messageBlock.hintRow] — which also means
// a fold cannot swallow the one line that tells a reader they may push back.
func (b *messageBlock) readingSegs(read reading, lead, text string) []segment {
	if !read.matched {
		// The plain paragraph, whole and unedited. It is only worth a door at
		// all when the clip actually hid something; a reading that fitted on its
		// own line opens onto nothing, which 5.20 rule 3 forbids naming.
		if clipCell(strings.TrimSpace(lead), promptCap) == strings.TrimSpace(text) {
			return nil
		}
		return []segment{{
			kind: segProse, text: text, tier: tokens.TextSecondary,
			indent: bodyIndent, folded: true,
		}}
	}
	segs := make([]segment, 0, len(read.assumed)+3)
	if goal := strings.TrimSpace(read.goal); goal != "" {
		segs = append(segs, segment{
			kind: segProse, text: goal, tier: tokens.TextSecondary,
			indent: bodyIndent, folded: true,
		})
	}
	if verbatim := strings.TrimSpace(read.verbatim); verbatim != "" {
		// The reader's own words, in the reader's own voice: dim, indented, and
		// wearing the prompt glyph they were typed at (§3b). It is a quotation
		// inside the card and is dressed as one rather than as more of the
		// head's sentence.
		segs = append(segs, segment{
			kind: segGutter, text: verbatim, indent: bodyIndent + partIndent,
			folded: true,
		})
	}
	for _, assumed := range read.assumed {
		// One step under the goal, sharing the quote's marker column: the
		// verbatim block's `│` and an assumption's `·` are both children of the
		// sentence above them, and §16's ALIGNMENT is that columns within a
		// section share x-positions across rows.
		segs = append(segs, segment{
			kind: segProse, text: assumed, tier: tokens.TextTertiary,
			indent: bodyIndent + 2*partIndent, folded: true, bullet: true,
		})
	}
	if rest := strings.TrimSpace(read.rest); rest != "" {
		segs = append(segs, segment{
			kind: segProse, text: rest, tier: tokens.TextTertiary,
			indent: bodyIndent, folded: true,
		})
	}
	b.hint = strings.TrimSpace(read.hint)
	return segs
}

// splitFirst is a reading's first line, for deciding whether the clip actually
// hid anything.
func splitFirst(text string) string {
	head, _, _ := strings.Cut(text, "\n")
	return head
}

// promptCap is how much of the head's reading stands on the card. One line at
// an ordinary width — enough to recognise the task by what was asked rather
// than only by its name, with §16's one ellipsis grammar saying there is more.
const promptCap = 96

// -- the settled deliverable card --------------------------------------------

// dressDelivery is 4.3's other sentence — "settled deliverable cards stay inline
// at birth position" — and 5.9's progressive disclosure applied to the one row
// that never had it.
//
// Collapsed, it is 5.9's card anatomy, line for line:
//
//	✓ three river haiku                            · $0.0012  ▸ 6 lines
//	  rivers.txt is written with three original haiku, each about rivers:
//	  ▸ /…/workspace/task-16/rivers.txt
//
// Line 1 answers "does it need me" and "what did it cost". The glyph and its hue
// say how it ended — green for delivered, because green is the word for money
// AND success (5.16), coral for a failure, and neither unless the board actually
// said so, because a failure drawn green is the one mistake this row can make.
// The title is the job's human name (5.14 keeps its id off the screen). The
// money and the fold affordance ride in the header's own cells.
//
// Line 2 is the brief — the first line the job wrote, at the secondary tier
// 5.13 gives a status line — and it is the whole reason this row is readable:
// it is what a reader needs to decide whether to open the rest.
//
// THE BRIEF DOES NOT RIDE IN THE HEADER'S DESCRIPTION, and that is a decision
// rather than a layout accident. The header degrades meta-first, so a brief long
// enough to be worth reading would push the money and the fold hint off the row
// at any ordinary width — the two cells 5.9 says a card must never lose. A card
// whose own text eats its money is not a card.
//
// THE ARTIFACT ROWS ARE NEVER FOLDED. 12.5's artifact law is that a deliverable
// is born on disk and referenced by its PATH, and a path a collapsed row is
// holding has not been handed over. The rest of the prose is what folds — whole
// and unedited, because the dressing is a presentation of the record and never a
// rewrite of it (13.1 item 3: a chatty result is the head's to fix, not the
// transcript's).
func (b *messageBlock) dressDelivery(message store.Message, board jobSource) {
	// THE RESULT IS ORGANIZED AND NOT TEASED. One line above a fold is a
	// headline, and the reader asked for "the result organized — with
	// expand/view-more on click and ellipsis": enough of the finding to be worth
	// reading standing up, and the rest one click away. §4's card anatomy says
	// the same thing in its own words — "3–5 sentences the assistant absorbed,
	// never 'see the file'".
	brief, rest := splitCardBody(message.Body)
	facts, known := jobFacts{}, false
	if board != nil {
		facts, known = board.jobFacts(message.NodeID)
	}

	title := "delivered"
	if known && strings.TrimSpace(facts.Name) != "" {
		title = facts.Name
	}
	// The card knows whose job it is so it can take that job's block's place
	// rather than land under it (§3: "delivered: the block becomes the delivery
	// card in place"). It is not machinery — a delivery is one of the three
	// sentence classes the chat carries (§1) — so nothing ever collapses IT.
	b.job = jobOf(message, board)
	// §4's TREATMENT, AND THE ONLY BLOCK IN THE PRODUCT THAT WEARS IT: the raised
	// ground and the `▎` accent edge, which together mean "a finished answer".
	// The commitment card this replaces stood on the rung below with the same
	// lane and no edge, so what changes at the moment of delivery is the plane
	// and the edge — not one column of the geometry.
	b.card = dressDelivery
	b.head = blocks.Header{
		Glyph: tokens.GlyphCollapsed,
		Title: title,
		State: blocks.StateSettled,
	}
	// The state axis, read off the board and never off the prose. An unknown
	// lifecycle keeps the neutral collapsed glyph: 8.2.20's law is that missing
	// data is drawn as missing, and a hue is a claim.
	switch facts.Life {
	case rail.LifeSettled:
		b.head.Glyph, b.head.GlyphHue = b.style.Glyph(tokens.GSettled), blocks.HueMoney
	case rail.LifeFailed, rail.LifeCancelled:
		b.head.Glyph, b.head.GlyphHue = b.style.Glyph(tokens.GFailed), blocks.HueBroken
	}
	// THE WHOLE RECEIPT, not the money alone (§5's record header, said for the
	// card that opens it): wall clock, money, tokens burned, and the models that
	// did the work. The reader asked for exactly these four — "the result has
	// token cost, model etc." — and every one of them is a read that already
	// exists; what was missing was the row that spends them.
	if receipt := taskReceipt(facts, board, b.job); receipt != "" {
		b.head.Meta = append(b.head.Meta, receipt)
	}

	// THE FIRST LINE OF THE ANSWER IS THE ANSWER'S LEAD. The reader asked for
	// "proper lines and headings", and §16 answers that inside a card the
	// structure comes from tier and spacing rather than from a lattice of rules:
	// the finding's opening sentence stands at the primary tier — the brightest
	// thing on the card after its own name — and the body follows it one tier
	// down. Two tiers and a title is three, which is §16's DIM RAMP exactly.
	if brief != "" {
		lead, body := splitHeadline(brief)
		if lead != "" {
			b.segs = append(b.segs, segment{
				kind: segProse, text: lead, tier: tokens.TextPrimary, indent: bodyIndent,
			})
		}
		if body != "" {
			b.segs = append(b.segs, segment{
				kind: segProse, text: body, tier: tokens.TextSecondary, indent: bodyIndent,
			})
		}
	}
	for _, path := range deliveryFiles(message) {
		b.artifacts = true
		b.segs = append(b.segs, segment{
			kind: segRef, glyph: tokens.GlyphCollapsed, text: path,
			hue: blocks.HueMoney, state: blocks.StateSettled, indent: bodyIndent,
		})
	}
	if rest == "" {
		return
	}
	b.collapsible, b.tailFold = true, true
	b.hidden = strings.Count(rest, "\n") + 1
	b.segs = append(b.segs, segment{
		kind: segProse, text: rest, tier: tokens.TextSecondary,
		indent: bodyIndent, folded: true,
	})
}

// -- work cards --------------------------------------------------------------

// dressWork is the other half of the THREAD-UX presentation law: card = work.
// A row anchored to a graph node is a job reporting, not a turn of
// conversation, and it is drawn with the card anatomy of 5.9 as far as the data
// in a message reaches:
//
//	◐ wisp-parity                     line 1: attention glyph + name
//	  reworking NavCtx after ...      line 2: the orchestrator's own status
//	  K3 · $—                         line 3: telemetry, dimmest tier
//
// Line 3 is honest about what this seam does not carry. Money is the one number
// 5.9 says is always visible, and the v2 engine seam has no per-node cost to
// read yet, so it renders as the missing-data glyph — 8.2.20's law is "missing
// data renders —, never an estimate", and an absent money cell would be the
// estimate zero.
// THE TITLE IS THE JOB'S NAME AND NEVER ITS ID (5.14, 13.8 finding 6). It used
// to be nodeLabel — the id's own last segment — so a narrator's progress line in
// a real room was headed `job-wisp` while the card beside it said `wisp-parity`,
// which is 5.14's never-shown tier on screen and two names for one thing on the
// same frame. The board already answers this for the delivery card (13.10); it
// answers for every other node-anchored row now, and falls back to the id's tail
// only where the board has never heard of the node.
// THE NAME IS A CHIP NOW, NOT A HEADER (§3b). The two parties of the
// conversation go unnamed, so a third one has to be named or the reader cannot
// tell whose sentence they are reading — that is the one thing §15 admits a
// label for, and it is why this row keeps its name while `you` and `aforge`
// lost theirs. It is small, dim, and it rides in front of the first line rather
// than over it: a guest gets a chip, not a row.
func (b *messageBlock) dressWork(message store.Message, board jobSource) {
	// THE CARD WEARS THE TASK'S NAME AND NEVER THE PART'S. A row from a worker
	// three levels down is still the task reporting — the reader commissioned
	// one thing and is owed one name for it — so the name, the lifecycle and the
	// money are all read off the ROOT (§14: a part is machinery vocabulary, and
	// "a single-part job says nothing about its shape").
	root := jobOf(message, board)
	title := nodeLabel(root)
	facts, known := jobFacts{}, false
	if board != nil {
		facts, known = board.jobFacts(root)
		if known && strings.TrimSpace(facts.Name) != "" {
			title = facts.Name
		}
	}
	// THIS ROW IS THE JOB'S, AND ONLY ONE OF THEM MAY BE ON SCREEN (§3). The
	// marking is here rather than at the coalescer because this is the only
	// place that knows what the row IS: a node-anchored line the job wrote
	// about itself, which §1 puts squarely in the work record's business and
	// never in the conversation. The reported defect is what happens without
	// it — "preparing the repository" and "reading the issue" arriving as two
	// blocks, each re-stating the job's name and receipt above one status line.
	// THE ROW BELONGS TO ITS TASK AND NOT TO ITS NODE. A child's completion, a
	// ruler line, a splitting line and a flag are all anchored somewhere under
	// the task the reader commissioned, so they all resolve to that one task and
	// all fold into its one card ([jobOf]). Without this, every child node in
	// the reporter's screenshots — "Candidate summaries", "Compile Cooling Stock
	// Data", "Write full report" — minted a top-level block of its own.
	b.job = root
	// A LEARNING MOMENT IS A VOICE. It keeps its own single dim line and is never
	// folded into a job's card; what it loses is the job's title and receipt as a
	// header, which is what made a sentence about a workflow wear "20 best stocks
	// ranked · $0.27" above it ([speaks]).
	b.machinery = !speaks(message.Body)
	b.working = !known || facts.Life == rail.LifeWorking || facts.Life == rail.LifeQueued
	b.elapsed, b.hasElapsed = facts.Elapsed, facts.HasElapsed
	// The state glyph stays in the gutter, where the reader's `›` is: one column
	// on the left says who is speaking, and for a job it also says how it is
	// doing. The identity pastel rides on it — a message with no node has no
	// identity, and a seedless one would give every such row the same accent.
	if !b.machinery {
		// One line, machinery tier, no header and no chip: it is the machine
		// saying something about itself, and the job it happened under is not
		// what a reader takes from it.
		b.appendBody(message, tokens.TextTertiary, bodyIndent)
		return
	}
	// The same card the commissioning opened, one status later: same ground, same
	// lane, no edge. §3's "one block per job, evolving in place" is a promise
	// about the DRESS as much as about the id — a row that changed plane every
	// time the job spoke would be a new card each time in everything but name.
	b.card, b.tailFold = dressCommitment, true
	b.wearCardHead(workGlyph(message), title)
	b.appendBody(message, tokens.TextSecondary, bodyIndent)
	b.readBoard(board)

	cells := make([]string, 0, 3)
	if model := modelWord(message.Model); model != "" {
		cells = append(cells, model)
	}
	// The board's money when the board has it, the missing glyph when it does
	// not. `$0.00` is the loudest estimate there is (§16's MONEY) and the row
	// used to draw the missing glyph unconditionally, which said "unknowable"
	// about a figure the rail one column over was already showing.
	switch {
	case known && facts.HasCost:
		cells = append(cells, tokens.Money(facts.Cost))
	default:
		cells = append(cells, tokens.GlyphSpend+tokens.GlyphMissing)
	}
	b.meta, b.receiptInHead = cells, true
}

// receiptLine composes the job block's telemetry row at the current instant.
//
// The elapsed leads, because "how long has this been going" is the cell a
// reader watching a running job is watching (rail/render.go ranks it the same
// way and for the same reason), and it is the one cell that MOVES.
func (b *messageBlock) receiptLine() string {
	cells := b.meta
	if shown, ok := b.liveElapsed(); ok {
		cells = append([]string{tokens.Elapsed(shown)}, cells...)
	}
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// liveElapsed is how long the job has been at it AT THIS FRAME: what the board
// measured, plus what the clock has counted since that measurement.
//
// A settled job's figure stands still, which is correct — it stopped. A job
// with no measured clock draws no cell at all rather than a zero (§16's
// EMPTINESS), and a clock that has never been latched adds nothing rather than
// subtracting the epoch from it.
func (b *messageBlock) liveElapsed() (time.Duration, bool) {
	if !b.hasElapsed {
		return 0, false
	}
	shown := b.elapsed
	if b.working && b.clock != nil && !b.at.IsZero() {
		if since := b.clock.Now().Sub(b.at); since > 0 {
			shown += since
		}
	}
	return shown, true
}

// liveStep is what this row's motion is COUNTED IN: whole seconds since the
// receipt was measured.
//
// It is seconds and not animation steps on purpose. The only thing moving on
// this row is a figure printed in seconds, so a version that moved eight times
// a second would rebuild the block eight times to produce byte-identical rows —
// which is exactly the wasted paint the shared clock's floor(now/interval)
// design exists to make impossible (8.1.3). A settled row, a row with no
// measured clock and a headless render all answer zero and therefore never
// move.
//
// CALM DOES NOT REACH IT. §18's calm profile freezes the GLYPH and keeps the
// clock ticking, which is the established rule and the honest one: a still
// glyph beside a number that changes is still a row visibly alive, and freezing
// the number would make a running job look stopped.
func (b *messageBlock) liveStep() int64 {
	if !b.working || b.clock == nil {
		return 0
	}
	// A CARD WITH A MOTION ON IT IS COUNTED IN THE MOTION'S OWN STEPS. The
	// breathe on `planning…` and the spinner on a running part advance on the
	// house grid (§18.1's 120ms), so a row counted in seconds would hold eight
	// frames of a ten-frame cycle and the motion would read as a stutter. It is
	// still the shared clock's floor(now/interval), so two cards show the same
	// frame and a repaint inside one step produces byte-identical rows.
	if b.breathing || b.partsAlive() {
		return b.clock.Step()
	}
	if b.at.IsZero() {
		return 0
	}
	if b.metaAt == 0 && !b.receiptInHead {
		return 0
	}
	since := b.clock.Now().Sub(b.at)
	if since <= 0 {
		return 0
	}
	return int64(since / time.Second)
}

// partsAlive says at least one of the card's parts is in flight, which is what
// makes the spinner on that row legal (§18.2) and what makes the block worth
// repainting on the animation grid.
func (b *messageBlock) partsAlive() bool {
	for _, part := range b.parts {
		if part.Life == rail.LifeWorking {
			return true
		}
	}
	return false
}

// animate hands the block the clock its receipt ages against, and latches the
// instant the board's measurement was taken.
//
// The latch is the honest half: the elapsed on a rail row was measured against
// some `now` inside the snapshot that produced it, and adding the time since
// THAT instant is a measurement continued rather than a number invented
// (8.2.20). A block with no clock simply never moves, which is what a headless
// test gets.
func (b *messageBlock) animate(clock *blocks.Clock, at time.Time) {
	if b == nil || b.job == "" {
		return
	}
	b.clock, b.at = clock, at
	b.measured = false
}

// workGlyph reads the state vocabulary of 5.17 off the row itself. A node row
// that carries an ending part says how it ended; anything else is a job that
// was working when it spoke, and shape encodes the state CATEGORY (8.1.6).
func workGlyph(message store.Message) string {
	for _, part := range message.Parts {
		if part.Kind == store.PartEnded && part.Ended != nil {
			switch part.Ended.How {
			case store.EndLength, store.EndStreamDrop:
				return tokens.GlyphFailed
			case store.EndInterrupted:
				return tokens.GlyphPaused
			default:
				return tokens.GlyphSettled
			}
		}
	}
	return tokens.GlyphWorking
}

// nodeLabel is the name a card wears. Node ids are never shown (5.14's "never
// shown" tier), so the label is the id's own last segment, which is what a
// spawn actually named the task.
func nodeLabel(nodeID string) string {
	if index := strings.LastIndexAny(nodeID, ":/"); index >= 0 && index+1 < len(nodeID) {
		return nodeID[index+1:]
	}
	return nodeID
}

// -- the arrival brief -------------------------------------------------------

// dressBrief is 5.24: the arrival brief is the FIRST transcript block on
// attach, not a banner and not a dock. It is styled as one — a titled block
// whose counts ride as badges in the header's own grammar, its items as
// reference rows, and a hairline under it because an arrival is the one room
// boundary a transcript has (5.13 allows a rule exactly there).
//
// Every badge carries the hue its meaning owns and no other: settled work is
// green because green is money and success, failures are coral, and anything
// waiting on a person is amber. A brief with nothing to report carries no
// badges at all rather than a row of zeroes.
func (b *messageBlock) dressBrief(message store.Message) {
	brief := message.Brief
	b.head = blocks.Header{
		Title: "while you were away",
		State: blocks.StateSettled,
	}
	for _, badge := range []struct {
		glyph string
		count int
		hue   blocks.Hue
	}{
		{b.style.Glyph(tokens.GSettled), brief.Done, blocks.HueMoney},
		{b.style.Glyph(tokens.GFailed), brief.Failed + brief.Cancelled, blocks.HueBroken},
		{b.style.Glyph(tokens.GNeedsHuman), brief.Questions + brief.Waiting, blocks.HueAttention},
	} {
		if badge.count > 0 {
			b.head.Badges = append(b.head.Badges, blocks.CountBadge(badge.glyph, badge.count, badge.hue))
		}
	}
	if brief.CostUSD > 0 {
		b.head.Meta = append(b.head.Meta, tokens.Money(brief.CostUSD))
	}

	b.appendBody(message, tokens.TextPrimary, bodyIndent)
	for _, item := range brief.Items {
		if strings.TrimSpace(item.Body) == "" {
			continue
		}
		b.segs = append(b.segs, segment{
			kind: segRef, glyph: tokens.GlyphSeparator, text: firstLine(item.Body),
			hue: blocks.HueNone, state: blocks.StateSettled, indent: bodyIndent,
		})
	}
	b.segs = append(b.segs, segment{kind: segRule})
}

// -- parts -------------------------------------------------------------------

// appendBody adds the message's prose, from parts when it is parts-native and
// from Body when it is not.
//
// A message whose parts carry text was written by a surface that knows about
// parts, and its parts are the whole truth; a message without them is a legacy
// prose message whose Body IS the line, and re-deriving it from parts that do
// not exist would render an empty turn.
func (b *messageBlock) appendBody(message store.Message, tier tokens.Token, indent int) {
	for _, part := range message.Parts {
		if part.Kind == store.PartText && strings.TrimSpace(part.Text) != "" {
			return
		}
	}
	if body := strings.TrimRight(message.Body, "\n"); body != "" {
		b.segs = append(b.segs, segment{kind: segProse, text: body, tier: tier, indent: indent})
	}
}

// bodyTier is the tier a block's own text parts are drawn at, which is the tier
// its Body was drawn at: a receipt's parts are still a receipt.
func (b *messageBlock) bodyTier() tokens.Token {
	for i := range b.segs {
		if b.segs[i].kind == segProse {
			return b.segs[i].tier
		}
	}
	if b.head.State == blocks.StateChrome {
		return tokens.TextSecondary
	}
	return tokens.TextPrimary
}

// absorbParts folds the typed blocks a message carries into the dressing. They
// are appended after the body in journal order, because that is the order they
// were produced in and re-sorting them would be a claim about causality the
// renderer is not entitled to make.
func (b *messageBlock) absorbParts(message store.Message) {
	tier := b.bodyTier()
	for _, part := range message.Parts {
		switch part.Kind {
		case store.PartText:
			if text := strings.TrimRight(part.Text, "\n"); text != "" {
				b.segs = append(b.segs, segment{
					kind: segProse, text: text, tier: tier, indent: bodyIndent,
				})
			}

		case store.PartArtifact:
			// The artifact law's rendering half (12.5.1): the deliverable is a
			// thing on disk with a path, and that is what the row says. It never
			// carries the bytes and it never pretends to.
			if part.Artifact == nil || b.artifacts {
				continue
			}
			label := part.Artifact.Path
			if part.Artifact.Bytes > 0 {
				label += "  " + tokens.Count(part.Artifact.Bytes) + "B"
			}
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: tokens.GlyphCollapsed, text: label,
				hue: blocks.HueMoney, state: blocks.StateSettled, indent: bodyIndent,
			})

		case store.PartCard:
			if part.Card == nil {
				continue
			}
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: tokens.GlyphCollapsed,
				text: "card " + nodeLabel(part.Card.NodeID),
				hue:  blocks.HueIdentity, state: blocks.StateSettled, indent: bodyIndent,
			})

		case store.PartProgress:
			if part.Progress == nil {
				continue
			}
			label := part.Progress.Phase
			if part.Progress.Total > 0 {
				label += " " + strconv.Itoa(part.Progress.Done) + "/" + strconv.Itoa(part.Progress.Total)
			}
			if part.Progress.Latest != "" {
				label += " " + tokens.GlyphSeparator + " " + part.Progress.Latest
			}
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: tokens.GlyphStepRunning, text: label,
				hue: blocks.HueAlive, state: blocks.StateChrome, indent: bodyIndent,
			})

		case store.PartQuestion:
			if part.Question == nil {
				continue
			}
			b.questions++
			// 13.3.1: the options are drawn from the message's own field, never
			// scanned back out of its prose. question.go owns the two shapes.
			b.dressQuestion(message, part.Question)

		case store.PartEnded:
			if part.Ended == nil {
				continue
			}
			b.end = endStateFor(part.Ended.How)
			b.head.End = b.end
		}
	}
}

// endStateFor reads the store's ending vocabulary into the renderer's. The two
// are separate on purpose: one is a record and grows with what providers say,
// the other is a rendering decision and grows with what a reader must be shown.
func endStateFor(how store.EndKind) blocks.EndState {
	switch how {
	case store.EndLength:
		return blocks.EndTruncatedByCap
	case store.EndStreamDrop:
		return blocks.EndStreamDropped
	case store.EndInterrupted:
		return blocks.EndInterrupted
	default:
		return blocks.EndCompleted
	}
}

// -- small readings ----------------------------------------------------------

// splitHeadline separates a body's first line from the rest. The first line is
// the receipt's own summary; everything after it is what the fold holds.
func splitHeadline(body string) (headline, rest string) {
	body = strings.TrimRight(body, "\n")
	head, tail, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(head), strings.TrimLeft(tail, "\n")
}

// firstLine is the one line a collapsed row shows.
func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// modelWord shortens a provider's model id to the word a person says. The
// vendor prefix, the variant suffix and the date suffix are provenance, not
// identity, and the header's meta cells are the first thing width pressure
// takes.
//
// It is [modelui.ModelWord] and nothing else. This package used to carry its
// own half of the rule — the vendor prefix only — which meant a reply header
// said "claude-sonnet-4-20250514" while the chip on the composer under it said
// "claude-sonnet-4", and the receipt a model switch posts would have been a
// third spelling. One word, one function: the chip, the header meta and the
// receipt cannot disagree about what a model is called.
func modelWord(model string) string { return modelui.ModelWord(model) }
