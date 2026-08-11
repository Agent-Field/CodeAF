package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
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
}

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

	// user marks the reader's own turn. It is the boundary the open-question
	// count walks back to: a question stops being open the moment the reader
	// speaks again.
	user bool
	// questions is how many askbacks this row carries, for the footer's amber
	// attention column (5.16: amber only ever means a human is actually
	// needed).
	questions int

	version  uint64
	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*messageBlock)(nil)

// ID is the anchor key and the cache key: the message's own sequence, so the
// reader's place survives every rebuild of the list around it.
func (b *messageBlock) ID() string { return b.id }

// IsFinalized is always true. A journaled message is a record.
func (b *messageBlock) IsFinalized() bool { return true }

// SettledRows is every row: a finalized block has no live tail.
func (b *messageBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version moves only when a fold opens or closes. Nothing else mutates a
// journaled message in place — a message that changed would arrive as a
// different message — and a fold is a change to committed bytes, which is
// exactly what the version counter exists to make coherent (8.1.1).
func (b *messageBlock) Version() uint64 { return b.version }

// End is how the turn that produced this message ended (12.5.2).
func (b *messageBlock) End() blocks.EndState { return b.end }

// SetExpanded opens or closes the fold and reports whether anything moved.
func (b *messageBlock) SetExpanded(open bool) bool {
	if !b.collapsible || b.expanded == open {
		return false
	}
	b.expanded = open
	b.head.Hint = blocks.ExpandHint(open, b.hidden)
	b.measured = false
	b.version++
	return true
}

// Rows renders the block at width, reusing the last render when the width has
// not moved.
//
// Every block CLOSES with one blank row. That is 5.13's spacing rhythm — "one
// blank line between turns" — and it lives here rather than in the transcript
// because a block that knew its own position would be a block the cache could
// not reuse when the list around it changed.
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
	if b.measured && b.width == width {
		return b.rows
	}
	st := b.styler()
	rows := b.rows[:0]
	if b.hasHead() {
		rows = append(rows, b.head.Render(width, st))
	}
	for i := range b.segs {
		seg := &b.segs[i]
		if seg.folded && !b.expanded {
			continue
		}
		switch seg.kind {
		case segRef:
			rows = append(rows, refRow(*seg, width, st))
		case segRule:
			rows = append(rows, hairline(width, b.style))
		default:
			rows = prose{style: b.style, base: seg.tier}.
				rows(rows, seg.text, width, seg.indent)
		}
	}
	if rule := blocks.CutRule(b.end, width, st); rule != "" {
		rows = append(rows, rule)
	}
	rows = append(rows, "")
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
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
	pad := ""
	if seg.indent > 0 && seg.indent < width {
		pad = strings.Repeat(" ", seg.indent)
	}
	room := width - blocks.Width(pad)
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
func hairline(width int, style *tokens.Styler) string {
	if width <= 0 {
		return ""
	}
	rule := strings.Repeat("─", width)
	if style == nil {
		return rule
	}
	return style.PaintToken(rule, tokens.TextTertiary)
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
func newMessageBlock(message store.Message, style *tokens.Styler) *messageBlock {
	block := &messageBlock{
		id:    messageID(message.Seq),
		seq:   message.Seq,
		style: style,
	}
	switch {
	case message.Brief != nil:
		block.dressBrief(message)
	case message.NodeID != "":
		block.dressWork(message)
	case message.Role == store.RoleSystem && message.CommandSeq != 0:
		block.dressCommission(message)
	case message.Role == store.RoleSystem:
		block.dressReceipt(message)
	default:
		block.dressSpeech(message)
	}
	block.absorbParts(message)
	// A question that arrived without a typed part is still a question: the
	// producer wrote its lifecycle sequence and its options into the message's
	// own columns before the parts model existed, and those columns are fields
	// like any other. What is NOT read is the body — 13.3.1's rule is that v2
	// never scans prose back into structure, and this branch does not.
	if block.questions == 0 && (message.QuestionSeq != 0 || len(message.Options) > 0) &&
		message.Role != store.RoleUser {
		block.questions++
		block.dressQuestion(message)
	}
	return block
}

// -- speech ------------------------------------------------------------------

// dressSpeech is the conversation itself (THREAD-UX: stream = conversation).
//
// The voice hierarchy is carried by the header and the tier, and by nothing
// else — no second header grammar, no per-role box, no colour (5.16: an accent
// hue never colorizes running text, and a whole coloured sentence means
// something is wrong). "you" is the quiet one: a reader knows what they typed
// and the row exists to prove it landed, so its label sits in the chrome tier
// behind the composer glyph they typed it at. "aforge" is named at full
// contrast with the model that answered as its meta, because the answer is the
// thing the reader came for and the model is the fact they will ask about.
func (b *messageBlock) dressSpeech(message store.Message) {
	switch message.Role {
	case store.RoleUser:
		b.user = true
		b.head = blocks.Header{
			Glyph: tokens.GlyphPromptChat,
			Title: "you",
			State: blocks.StateChrome,
		}
	default:
		b.head = blocks.Header{Title: "aforge", State: blocks.StateSettled}
		if model := modelWord(message.Model); model != "" {
			b.head.Meta = append(b.head.Meta, model)
		}
	}
	b.appendBody(message, tokens.TextPrimary, bodyIndent)
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
	b.head.Hint = blocks.ExpandHint(false, b.hidden)
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
func (b *messageBlock) dressCommission(message store.Message) {
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
	b.head.Hint = blocks.ExpandHint(false, b.hidden)
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
func (b *messageBlock) dressWork(message store.Message) {
	b.head = blocks.Header{
		Glyph: workGlyph(message),
		Title: nodeLabel(message.NodeID),
		State: blocks.StateSettled,
	}
	// The identity pastel goes through the one header grammar now that Header
	// carries a seed beside its hue (the seam this file used to reach around).
	// The guard is the whole of the old workaround's honesty: a message with no
	// node has no identity, and a seedless HueIdentity would resolve to wheel
	// entry zero and give every such row the same accent.
	if seed := blocks.Seed(message.NodeID); seed != 0 {
		b.head.GlyphHue, b.head.GlyphSeed = blocks.HueIdentity, seed
	}
	b.appendBody(message, tokens.TextSecondary, bodyIndent)

	cells := make([]string, 0, 2)
	if model := modelWord(message.Model); model != "" {
		cells = append(cells, model)
	}
	cells = append(cells, "$"+tokens.GlyphMissing)
	b.segs = append(b.segs, segment{
		kind: segProse, text: strings.Join(cells, " "+tokens.GlyphSeparator+" "),
		tier: tokens.TextTertiary, indent: bodyIndent,
	})
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
		{tokens.GlyphSettled, brief.Done, blocks.HueMoney},
		{tokens.GlyphFailed, brief.Failed + brief.Cancelled, blocks.HueBroken},
		{tokens.GlyphNeedsHuman, brief.Questions + brief.Waiting, blocks.HueAttention},
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
			if part.Artifact == nil {
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
			b.dressQuestion(message)

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
// vendor prefix and the date suffix are provenance, not identity, and the
// header's meta cells are the first thing width pressure takes.
func modelWord(model string) string {
	model = strings.TrimSpace(model)
	if index := strings.LastIndexByte(model, '/'); index >= 0 && index+1 < len(model) {
		model = model[index+1:]
	}
	return model
}
