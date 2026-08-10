package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// One durable message, rendered as one finalized block.
//
// It is its own [blocks.Block] rather than a [blocks.TextBlock] because of the
// artifact law (12.5.1). A message is not necessarily prose: it may carry a
// file on disk, a card for a graph node, a progress reading. Prose is for
// meaning; the workspace is for artifacts — so a reference renders AS a
// reference, on its own row, with its own glyph, and is never flattened into
// the sentence around it. A renderer that assumed prose was the only carrier
// is exactly what session bd3c78ed's SVG went missing inside.
//
// The block is finalized from birth: a journaled message does not change. That
// makes it a cache entry the transcript renders once per width and never
// rebuilds, which is what keeps scrolling a long thread free.

// segment is one run inside a message: prose to be wrapped, or a single-row
// reference to something the message points at.
type segment struct {
	// ref is false for prose and true for a reference row.
	ref bool
	// text is the prose, or the reference's label.
	text string
	// glyph leads a reference row.
	glyph string
	// hue paints a reference row.
	hue blocks.Hue
	// state paints a reference row's label.
	state blocks.State
}

// messageBlock renders one store.Message.
type messageBlock struct {
	id    string
	seq   int64
	head  blocks.Header
	segs  []segment
	end   blocks.EndState
	style blocks.Styler

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

// Version never moves. Nothing mutates a journaled message in place; a message
// that changed would arrive as a different message.
func (b *messageBlock) Version() uint64 { return 0 }

// End is how the turn that produced this message ended (12.5.2).
func (b *messageBlock) End() blocks.EndState { return b.end }

// Rows renders the block at width, reusing the last render when the width has
// not moved.
func (b *messageBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	st := b.style
	if st == nil {
		st = blocks.Plain
	}
	rows := b.rows[:0]
	if b.head.Title != "" || b.head.Glyph != "" || len(b.head.Meta) > 0 || b.end.Cut() {
		rows = append(rows, b.head.Render(width, st))
	}
	for i := range b.segs {
		seg := &b.segs[i]
		if seg.ref {
			rows = append(rows, refRow(*seg, width, st))
			continue
		}
		start := len(rows)
		rows, _ = blocks.Wrap(rows, seg.text, width)
		for j := start; j < len(rows); j++ {
			rows[j] = st.Paint(rows[j], blocks.StateSettled, blocks.HueNone)
		}
	}
	if rule := blocks.CutRule(b.end, width, st); rule != "" {
		rows = append(rows, rule)
	}
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
}

// refRow draws one reference: a glyph, a gap, and the thing being pointed at.
// A path is shortened from its middle rather than its end, because the tail of
// a path is the part that names the file.
func refRow(seg segment, width int, st blocks.Styler) string {
	if width <= 0 {
		return ""
	}
	lead := seg.glyph + " "
	leadWidth := blocks.Width(lead)
	if leadWidth >= width {
		return st.Paint(blocks.Truncate(seg.glyph, width), blocks.StateChrome, seg.hue)
	}
	label := blocks.TruncatePath(seg.text, width-leadWidth)
	return st.Paint(lead, blocks.StateChrome, seg.hue) + st.Paint(label, seg.state, blocks.HueNone)
}

// messageID is a message's stable block identity. Unsequenced rows cannot
// happen here — every block this package appends came back from the store with
// a sequence — so the id is total.
func messageID(seq int64) string { return "msg-" + strconv.FormatInt(seq, 10) }

// newMessageBlock turns one journaled message into one block.
//
// Text arrives already sanitized: the poll runs every body and every text part
// through the chokepoint before the message reaches here, so nothing in this
// file has to think about escape sequences and nothing downstream has to
// sanitize twice.
func newMessageBlock(message store.Message, style blocks.Styler) *messageBlock {
	block := &messageBlock{
		id:    messageID(message.Seq),
		seq:   message.Seq,
		head:  messageHeader(message),
		style: style,
	}

	// Prose first, unless the message is parts-native. A message whose parts
	// carry text is one written by a surface that knows about parts, and its
	// blocks are the whole truth; a message without them is a legacy prose
	// message whose Body IS the line, and re-deriving it from parts that do not
	// exist would render an empty turn.
	hasText := false
	for _, part := range message.Parts {
		if part.Kind == store.PartText && strings.TrimSpace(part.Text) != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		if body := strings.TrimRight(message.Body, "\n"); body != "" {
			block.segs = append(block.segs, segment{text: body})
		}
	}

	for _, part := range message.Parts {
		switch part.Kind {
		case store.PartText:
			if text := strings.TrimRight(part.Text, "\n"); text != "" {
				block.segs = append(block.segs, segment{text: text})
			}

		case store.PartArtifact:
			// The artifact law's rendering half: the deliverable is a thing on
			// disk with a path, and that is what the row says. It never carries
			// the bytes and it never pretends to.
			if part.Artifact == nil {
				continue
			}
			label := part.Artifact.Path
			if part.Artifact.Bytes > 0 {
				label += "  " + tokens.Count(part.Artifact.Bytes) + "B"
			}
			block.segs = append(block.segs, segment{
				ref: true, glyph: tokens.GlyphCollapsed, text: label,
				hue: blocks.HueMoney, state: blocks.StateSettled,
			})

		case store.PartCard:
			if part.Card == nil {
				continue
			}
			block.segs = append(block.segs, segment{
				ref: true, glyph: tokens.GlyphCollapsed, text: "card " + part.Card.NodeID,
				hue: blocks.HueIdentity, state: blocks.StateSettled,
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
				label += " · " + part.Progress.Latest
			}
			block.segs = append(block.segs, segment{
				ref: true, glyph: tokens.GlyphStepRunning, text: label,
				hue: blocks.HueAlive, state: blocks.StateChrome,
			})

		case store.PartQuestion:
			if part.Question == nil {
				continue
			}
			block.segs = append(block.segs, segment{
				ref: true, glyph: tokens.GlyphNeedsHuman,
				text: "waiting on you", hue: blocks.HueAttention, state: blocks.StateSettled,
			})

		case store.PartEnded:
			if part.Ended == nil {
				continue
			}
			block.end = endStateFor(part.Ended.How)
			block.head.End = block.end
		}
	}
	return block
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

// messageHeader is the one header grammar (8.1.5) applied to a message: who
// said it, and what is worth knowing about how they said it.
func messageHeader(message store.Message) blocks.Header {
	head := blocks.Header{State: blocks.StateChrome}
	switch message.Role {
	case store.RoleUser:
		head.Glyph, head.Title = tokens.GlyphPromptChat, "you"
	case store.RoleAgent:
		head.Title = "aforge"
		if model := strings.TrimSpace(message.Model); model != "" {
			head.Meta = append(head.Meta, model)
		}
	default:
		head.Title = "system"
	}
	return head
}
