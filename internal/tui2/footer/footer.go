package footer

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// InputState is the composer's input-state axis (10.5.26, the Warp pattern
// adopted at 8.2's rule 26): the footer's hint column changes with it. The
// TEXT of the hint always comes from [FocusContext.Hint] — the wiring knows
// the actual accelerator words for its own state, this package does not
// invent copy — but the STATE still matters here because it picks the
// hint's colour: a failed send is coral (5.16: coral means broken), like
// every other failure this surface draws, and nothing else is.
type InputState uint8

const (
	// InputEmpty is an empty draft. Its default hint, if any, is left to the
	// wiring — 10.5.26 does not mandate a canned line and personality
	// belongs in idle placeholders (8.2's rule 25), not a status row.
	InputEmpty InputState = iota
	// InputTyped is a non-empty, unsent draft.
	InputTyped
	// InputFailed is the last send attempt failing — 8.2's rule 26 names the
	// "attach failed output as context" hint pattern for exactly this state.
	InputFailed
	// InputQueued is one or more type-ahead chips waiting under the composer
	// for the orchestrator's next turn (5.20 rule 4).
	InputQueued
)

// KeyMode says what a bare digit keypress means right now — the checklist
// item 5.22 states once: "digits answer an open question in the current
// room when one is focused; otherwise they jump to rail rows in the current
// scope." [FocusContext.KeyMode] and [FocusContext.KeyModeCount] together
// resolve the ambiguity on screen so nobody has to hold it in their head.
type KeyMode uint8

const (
	// KeyModeNone means digits currently do nothing (no open question, no
	// rail rows in scope) — the column is omitted rather than drawn empty.
	KeyModeNone KeyMode = iota
	// KeyModeAnswer means digits answer the focused open question.
	KeyModeAnswer
	// KeyModeRooms means digits jump to a rail row in the current scope.
	KeyModeRooms
)

// FocusContext is everything one frame of the footer needs, assembled by the
// wiring from whatever pane currently holds focus. See doc.go's "honest by
// construction": a zero-valued field always means "nothing to say here",
// never "say nothing loudly".
type FocusContext struct {
	// Verbs are the entries to offer for the current focus, already ordered
	// most-relevant-first by the wiring — only the pane holding focus knows
	// what is relevant, this package only knows how to lay verbs out and
	// fit them. At most three are shown (5.22 rule 4: "the 2-3 most
	// relevant verbs"); a longer slice is trimmed, never an error.
	Verbs []registry.Entry

	// Input is the composer's input-state axis, for the hint column's
	// colour. See [InputState].
	Input InputState
	// Hint is the wiring-composed text for the current Input state — the
	// actual accelerator words ("↵ send", "alt+↑ edit queued", …). Empty
	// means no state hint to show; EscInterrupts still may show one, see
	// below.
	Hint string
	// EscInterrupts is true only when esc, right now, would interrupt the
	// turn the user is watching (8.2's rule 21, "esc acts on what you are
	// watching") — the same condition that gates the transcript's own
	// awaiting-line hint. When true it takes the hint column over Hint
	// entirely: an interrupt in flight is more urgent than the composer's
	// own state.
	EscInterrupts bool

	// KeyMode and KeyModeCount together render the digit-precedence
	// indicator. KeyModeCount is the upper end of the "1-N" range; a
	// non-positive count with a non-[KeyModeNone] mode is treated as "no
	// indicator to show" — a range of nothing is not information.
	KeyMode      KeyMode
	KeyModeCount int

	// Attention is the count of open questions blocking on a human in the
	// room the footer is describing. Zero omits the column entirely — 5.16:
	// amber only ever means a human is actually needed.
	Attention int

	// Health is the pending-only system states 10.5.23 assigns this row
	// (services, MCP-ish states) — never this-turn cost or context, which
	// belong on the composer's meta strip. Each entry is one already-worded
	// item; nil or empty omits the column.
	Health []string

	// Toast is a transient receipt from another room (5.21) — one line,
	// already worded and already due to decay on its own. Empty omits the
	// column.
	Toast string

	// ScopeTail is the breadcrumb tail already shown above the transcript —
	// "the first thing that can go" (it is the lowest-priority column here
	// on purpose, see [priorityOf]). Empty omits the column.
	ScopeTail string
}

// Options configures a [Model].
type Options struct {
	// Styler paints every cell this package draws. A nil Styler degrades to
	// unstyled plain text, the same posture every sibling package in this
	// tree takes for the same situation.
	Styler *tokens.Styler
}

// Model is the contextual footer. The zero value is not meaningful;
// construct one with [New].
type Model struct {
	styler *tokens.Styler
}

// New builds a footer from opts. It never fails: a missing Styler degrades
// to plain text.
func New(opts Options) *Model {
	return &Model{styler: opts.Styler}
}

// maxVerbs is 5.22 rule 4's "2-3 most relevant verbs", enforced here
// regardless of how many the wiring hands in — a strip that grew without
// bound would stop being scannable, which is the whole reason the rule
// exists.
const maxVerbs = 3

// helpDoorText is the permanent visible door 5.22's checklist closes with: a
// dim `?` segment naming itself, never a bare rune with no visible hint —
// that shape is the checklist's own kill list ("bare action runes with no
// visible hint").
const helpDoorText = tokens.GlyphNeedsHuman + " help"

// escInterruptHint mirrors the exact wording the transcript's own awaiting
// line uses for the same condition (internal/tui2/chat's awaitingBlock) —
// one phrase for one fact, said in two places the user might be looking.
const escInterruptHint = "esc interrupt"

// enDash renders the digit range in [FocusContext.KeyMode]'s indicator,
// spelled exactly as 5.22's checklist itself writes it ("1-3 answer" — the
// checklist's source markdown uses an en dash, not a hyphen). It is not in
// tokens' glyph table, so it is measured here the way tokens.go measures its
// own vocabulary: one cell under x/ansi and go-runewidth in the default
// mode, Ambiguous (two cells) under a CJK-locale east-asian-width terminal —
// the same risk class tokens already ships for its own `·` and `○`.
const enDash = "–"

// part is one candidate column: already-composed plain text (no escape
// sequences — painting happens after the fitting pass, never before, so a
// truncated candidate can never carry a stray reset code, see [Model.Render])
// and the token it paints with.
type part struct {
	id   string
	text string
	tok  tokens.Token
}

// sep joins parts, and joins verb entries within the verbs column, and joins
// health items within the health column — one separator for the whole
// surface (5.17's `·`).
const sep = " " + tokens.GlyphSeparator + " "

var sepWidth = blocks.Width(sep)

// priorityOf is the drop order 10.5.22 asks for: higher survives longer.
// Five of these numbers are [tokens.FooterColumnOrder]'s own — attention,
// help, keymode, verbs, health, toast and scope all carry the SAME priority
// that canonical table assigns them, because that table is the ledger this
// exact row answers to and a second, drifting copy of the same law would be
// worse than no law. "hint" is new (5.20 rule 6 / 10.5.26 land in this wave,
// after that table was seeded) and sits between "help" and "keymode": an
// esc-interrupt or a failed-send hint is more urgent than knowing which
// meaning a digit carries, but the permanent `?` door still outranks it —
// the capability-honesty surface is not allowed to be the first thing width
// pressure takes.
func priorityOf(id string) int {
	switch id {
	case "attention":
		return 100
	case "help":
		return 90
	case "hint":
		return 85
	case "keymode":
		return 80
	case "verbs":
		return 70
	case "health":
		return 60
	case "toast":
		return 50
	case "scope":
		return 40
	}
	return 0
}

// Render draws the footer at width cells. It is a pure function of ctx and
// width: never more than one row, never wider than width, never a panic,
// down to width=1 and a zero-valued ctx alike.
func (m *Model) Render(ctx FocusContext, width int) string {
	if width <= 0 {
		return ""
	}
	parts := buildParts(ctx)
	if len(parts) == 0 {
		return ""
	}

	cols := make([]tokens.FooterColumn, len(parts))
	for i, p := range parts {
		w := blocks.Width(p.text)
		if i > 0 {
			w += sepWidth
		}
		cols[i] = tokens.FooterColumn{ID: p.id, MinWidth: w, Priority: priorityOf(p.id)}
	}
	kept := tokens.FitFooter(cols, width)
	if len(kept) == 0 {
		return ""
	}
	keepAt := make(map[string]bool, len(kept))
	for _, c := range kept {
		keepAt[c.ID] = true
	}

	// Assemble the PLAIN line first — painting happens only once the exact
	// surviving text is known, so a defensive truncate below (which should
	// never fire; [tokens.FitFooter]'s own accounting already guarantees the
	// fit) can never cut a painted string and orphan its reset sequence
	// (blocks.Header's own documented reason for the same ordering).
	survivors := make([]part, 0, len(parts))
	var plain strings.Builder
	for _, p := range parts {
		if !keepAt[p.id] {
			continue
		}
		if plain.Len() > 0 {
			plain.WriteString(sep)
		}
		plain.WriteString(p.text)
		survivors = append(survivors, p)
	}

	if blocks.Width(plain.String()) > width {
		return blocks.Truncate(plain.String(), width)
	}
	return m.paintParts(survivors)
}

// buildParts composes every candidate column in DISPLAY order, including
// only the ones that have something to say. Order here is left-to-right
// screen order; [priorityOf] is drop order, and the two are intentionally
// different — see [tokens.FooterColumnOrder]'s own comment for why
// (attention is drawn first because the eye lands there, and it is dropped
// last because a blocked human is the most expensive state this product
// has).
func buildParts(ctx FocusContext) []part {
	out := make([]part, 0, 8)
	if ctx.Attention > 0 {
		out = append(out, part{"attention", tokens.GlyphNeedsHuman + strconv.Itoa(ctx.Attention), tokens.Amber})
	}
	if text, tok, ok := hintOf(ctx); ok {
		out = append(out, part{"hint", text, tok})
	}
	if ctx.KeyMode != KeyModeNone && ctx.KeyModeCount > 0 {
		out = append(out, part{"keymode", keyModeText(ctx.KeyMode, ctx.KeyModeCount), tokens.TextTertiary})
	}
	if v := verbsText(ctx.Verbs); v != "" {
		out = append(out, part{"verbs", v, tokens.TextTertiary})
	}
	if len(ctx.Health) > 0 {
		out = append(out, part{"health", strings.Join(ctx.Health, sep), tokens.TextTertiary})
	}
	if ctx.Toast != "" {
		out = append(out, part{"toast", ctx.Toast, tokens.TextTertiary})
	}
	if ctx.ScopeTail != "" {
		out = append(out, part{"scope", ctx.ScopeTail, tokens.TextTertiary})
	}
	// help is the one column with nothing to test for: it is always there.
	out = append(out, part{"help", helpDoorText, tokens.TextTertiary})
	return out
}

// hintOf resolves the hint column: esc-interrupt when it is live (5.20 rule
// 6), else the wiring's own input-state hint when it has one, else no
// column at all — the affordance never lies by showing up with nothing to
// say.
func hintOf(ctx FocusContext) (text string, tok tokens.Token, ok bool) {
	if ctx.EscInterrupts {
		return escInterruptHint, tokens.TextTertiary, true
	}
	if ctx.Hint == "" {
		return "", 0, false
	}
	if ctx.Input == InputFailed {
		return ctx.Hint, tokens.Coral, true
	}
	return ctx.Hint, tokens.TextTertiary, true
}

// keyModeText renders the digit-precedence indicator.
func keyModeText(mode KeyMode, n int) string {
	word := "rooms"
	if mode == KeyModeAnswer {
		word = "answer"
	}
	return "1" + enDash + strconv.Itoa(n) + " " + word
}

// verbsText renders up to [maxVerbs] entries as "key verb" (or "/slash verb"
// when the entry has no key, or bare "verb" when it has neither — a
// ScopeTalk-only entry reached solely through the head belt, still worth
// naming for capability honesty even with no accelerator of its own).
func verbsText(entries []registry.Entry) string {
	if len(entries) == 0 {
		return ""
	}
	n := len(entries)
	if n > maxVerbs {
		n = maxVerbs
	}
	items := make([]string, n)
	for i, e := range entries[:n] {
		items[i] = verbLabel(e)
	}
	return strings.Join(items, sep)
}

func verbLabel(e registry.Entry) string {
	switch {
	case e.Key != "":
		return e.Key + " " + e.Verb
	case e.Slash != "":
		return "/" + e.Slash + " " + e.Verb
	default:
		return e.Verb
	}
}

// paintParts paints the already-fitted survivors, one Styler call per part
// and one per separator, so a nil Styler still returns the plain text (the
// [Model.paint] guard) rather than panicking.
func (m *Model) paintParts(parts []part) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(m.paint(sep, tokens.TextTertiary))
		}
		b.WriteString(m.paint(p.text, p.tok))
	}
	return b.String()
}

func (m *Model) paint(text string, tok tokens.Token) string {
	if m.styler == nil || text == "" {
		return text
	}
	return m.styler.PaintToken(text, tok)
}
