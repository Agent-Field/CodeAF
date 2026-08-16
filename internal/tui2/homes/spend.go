package homes

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The money segment, and the editor that opens on it (5.24: "the money segment
// in the status line is clickable → inline numeric editor… rails are edited
// where they are shown").
//
// The last clause is the design. A daily budget that can only be changed
// through a slash command is a rail that is displayed in one place and edited
// in another, and the person who notices they are near it is by definition
// looking at the segment, not at a command line. So the segment is the control:
// it shows the reading, it takes focus, and it becomes a field in place. There
// is no budget page, no budget dialog and no second spelling of the number.
//
// What it will NOT do is write. A chosen value leaves as a [SpendResult] and
// the wiring routes it to the door that already exists —
// (*command.Commander).Budget, whose `default <amt>` arm writes the config
// default and whose bare `<amt>` arm raises today's ceiling. A surface that
// could write a budget would be a second place budgets live, and 12.6.5's rule
// about who may journal a rail is the same rule.

// SpendState is the reading the segment shows. See state.go's read map for
// which store call fills each field.
type SpendState struct {
	// SpentUSD is what has been spent in the window. HasSpent separates "spent
	// nothing" from "nobody has counted" (10.2.8).
	SpentUSD float64
	HasSpent bool

	// LimitUSD is the ceiling in force — store.DailyRail.Ceiling, which already
	// folds in any raise. HasLimit false means no rail is set, and the segment
	// shows the spend alone rather than inventing a denominator.
	LimitUSD float64
	HasLimit bool

	// Unlimited is store.DailyRail.Unlimited: the rail was lifted for today. It
	// renders as a word, because a ceiling of infinity is not a number and
	// printing one would be a lie with a decimal point in it.
	Unlimited bool

	// Reached is store.DailyRail.Reached. It turns the segment amber, which is
	// the five-word vocabulary being used exactly: a reached rail needs a human
	// (5.16), and it is the only condition under which this segment is not
	// chrome.
	Reached bool

	// Editable is false when the window may not change the rail — a visitor
	// window (5.24). The segment still shows the reading; it just refuses to
	// open, and [Spend.Open] reports that it did.
	Editable bool
}

// SpendResult is a committed edit. It carries the number and nothing else: what
// that number MEANS — today's ceiling, or the default every day starts from —
// is the wiring's decision, because only the wiring knows which door it holds.
type SpendResult struct {
	// LimitUSD is the value the person typed, already parsed and non-negative.
	LimitUSD float64
}

// Spend is the money segment as a component: a reading, a focus state, and an
// inline field.
//
// The zero value renders nothing and edits nothing, which is what an unwired
// status line should show.
type Spend struct {
	profile tokens.Profile
	focus   tokens.Focus
	glyphs  tokens.GlyphSet

	state   SpendState
	focused bool
	editing bool
	invalid bool

	runes  []rune
	cursor int

	buf  strings.Builder
	line lineBuf
}

// NewSpend returns a segment painting for a Styler. A nil Styler means no
// colour and the plain glyph tier.
func NewSpend(st *tokens.Styler) *Spend {
	s := &Spend{}
	s.SetStyler(st)
	return s
}

// SetStyler re-points the segment.
func (s *Spend) SetStyler(st *tokens.Styler) {
	if st == nil {
		s.profile, s.focus, s.glyphs = tokens.NoColor, tokens.FocusNormal, tokens.Plain
		return
	}
	s.profile, s.focus, s.glyphs = st.Profile(), st.Focus(), st.GlyphSet()
}

// SetState replaces the reading. An edit in flight SURVIVES a state change: the
// poll behind the reading fires on a timer, and a field that cleared itself
// under the person's hands every two seconds would be unusable.
func (s *Spend) SetState(state SpendState) { s.state = state }

// State returns the current reading.
func (s *Spend) State() SpendState { return s.state }

// Focus marks the segment as the thing the cursor is on. An interactive chip
// brightens one tier on focus and never lives permanently in the dimmest tier
// (5.22's amendment to 5.13).
func (s *Spend) Focus(on bool) {
	s.focused = on
	if !on {
		s.Cancel()
	}
}

// Focused reports whether the segment holds focus.
func (s *Spend) Focused() bool { return s.focused }

// Editing reports whether the inline field is open. The wiring routes keys here
// only while it is, and shows the field's own hint line rather than the
// composer's.
func (s *Spend) Editing() bool { return s.editing }

// Open puts the segment into edit, seeded with the current ceiling. It reports
// false when the rail may not be changed from this window, so the caller can
// say why rather than having the key do nothing (5.20 rule 3).
func (s *Spend) Open() bool {
	if !s.state.Editable {
		return false
	}
	s.editing = true
	s.invalid = false
	s.runes = s.runes[:0]
	if s.state.HasLimit && !s.state.Unlimited {
		s.runes = append(s.runes, []rune(trimZeros(s.state.LimitUSD))...)
	}
	s.cursor = len(s.runes)
	return true
}

// Cancel closes the field without committing. The draft is dropped: a number
// half typed is not a budget, and keeping it would mean the next open showed a
// figure nobody chose.
func (s *Spend) Cancel() {
	s.editing = false
	s.invalid = false
	s.runes = s.runes[:0]
	s.cursor = 0
}

// Commit closes the field and returns the value, reporting false when the draft
// is not a number this package will hand on. It does NOT clear on a bad draft:
// the field stays open, marked invalid, with what was typed still there — a
// mistyped budget that vanished would make the person start over to fix one
// character.
func (s *Spend) Commit() (SpendResult, bool) {
	if !s.editing {
		return SpendResult{}, false
	}
	v, ok := parseUSD(string(s.runes))
	if !ok {
		s.invalid = true
		return SpendResult{}, false
	}
	s.Cancel()
	return SpendResult{LimitUSD: v}, true
}

// Key drives the inline field. It returns the committed result when enter
// closed the field, and reports whether the key was consumed — a key it does
// not consume belongs to whoever owns the surface, unchanged.
//
// The accepted grammar is deliberately tiny: digits, one decimal point, the
// motions a one-line field has, esc and enter. Everything else is refused
// rather than inserted, because a budget field that accepts letters is a field
// that fails at commit instead of at the keystroke.
func (s *Spend) Key(msg tea.KeyPressMsg) (SpendResult, bool, bool) {
	if !s.editing {
		return SpendResult{}, false, false
	}
	switch msg.String() {
	case "esc":
		s.Cancel()
		return SpendResult{}, false, true
	case "enter":
		r, ok := s.Commit()
		return r, ok, true
	case "backspace":
		s.backspace()
		return SpendResult{}, false, true
	case "delete":
		s.deleteForward()
		return SpendResult{}, false, true
	case "left":
		if s.cursor > 0 {
			s.cursor--
		}
		return SpendResult{}, false, true
	case "right":
		if s.cursor < len(s.runes) {
			s.cursor++
		}
		return SpendResult{}, false, true
	case "home", "ctrl+a":
		s.cursor = 0
		return SpendResult{}, false, true
	case "end", "ctrl+e":
		s.cursor = len(s.runes)
		return SpendResult{}, false, true
	}
	for _, r := range msg.Text {
		s.insert(r)
	}
	return SpendResult{}, false, true
}

func (s *Spend) insert(r rune) {
	switch {
	case r >= '0' && r <= '9':
	case r == '.' && !strings.ContainsRune(string(s.runes), '.'):
	default:
		return
	}
	if len(s.runes) >= maxSpendDigits {
		return
	}
	s.runes = append(s.runes, 0)
	copy(s.runes[s.cursor+1:], s.runes[s.cursor:])
	s.runes[s.cursor] = r
	s.cursor++
	s.invalid = false
}

func (s *Spend) backspace() {
	if s.cursor == 0 {
		return
	}
	s.runes = append(s.runes[:s.cursor-1], s.runes[s.cursor:]...)
	s.cursor--
	s.invalid = false
}

func (s *Spend) deleteForward() {
	if s.cursor >= len(s.runes) {
		return
	}
	s.runes = append(s.runes[:s.cursor], s.runes[s.cursor+1:]...)
	s.invalid = false
}

// maxSpendDigits bounds the field. Nine characters is $99,999.99 without the
// separators — past any budget this product has a use for, and short enough
// that the field can never push the rest of the status line off its own row.
const maxSpendDigits = 9

// Draft is what has been typed, for a caller that wants to echo it elsewhere.
func (s *Spend) Draft() string { return string(s.runes) }

// Invalid reports that the last commit was refused.
func (s *Spend) Invalid() bool { return s.invalid }

// Text is the segment's reading as plain text, with no escapes — what a
// clipboard copy of the chip should contain (5.22 rule 5: a chip is a button,
// and a button that shows a number should hand over the number).
func (s *Spend) Text() string {
	if s.editing {
		return s.glyph() + " " + string(s.runes)
	}
	var b strings.Builder
	b.WriteString(s.glyph())
	b.WriteByte(' ')
	if s.state.HasSpent {
		b.WriteString(tokens.Money(s.state.SpentUSD))
	} else {
		b.WriteString(tokens.GlyphMissing)
	}
	switch {
	case s.state.Unlimited:
		b.WriteString(" / no rail")
	case s.state.HasLimit:
		b.WriteString(" / ")
		b.WriteString(tokens.Money(s.state.LimitUSD))
	}
	return b.String()
}

// Width is how many cells the segment wants. A status line fits its columns by
// asking (10.5.22), and a segment that reported the wrong number would be the
// one that got dropped.
func (s *Spend) Width() int { return blocks.Width(s.Text()) }

// Render draws the segment into width cells.
//
// Editing, the field is drawn with the cursor as a reversed cell rather than as
// a separate glyph: the status line has no room for a caret column, and the
// reverse is the same mechanism the selection band already falls back to on a
// profile with no trustworthy background.
func (s *Spend) Render(width int) string {
	if width <= 0 {
		return ""
	}
	l := &s.line
	l.reset(width)
	if s.editing {
		s.renderField(l)
	} else {
		s.renderReading(l)
	}
	return l.emit(&s.buf, s.profile, s.focus, width, false, tokens.Ground)
}

// renderReading is the resting segment: `$ 8.65 / 20.00`.
func (s *Spend) renderReading(l *lineBuf) {
	tok := s.tier(tokens.TextTertiary)
	if s.state.Reached {
		tok = tokens.Amber
	}
	l.add(s.glyph(), tok)
	l.add(" ", tokens.TextTertiary)
	if s.state.HasSpent {
		l.add(tokens.Money(s.state.SpentUSD), s.tier(tokens.Green))
	} else {
		l.add(tokens.GlyphMissing, tokens.TextTertiary)
	}
	switch {
	case s.state.Unlimited:
		l.add(" / ", tokens.TextTertiary)
		l.add("no rail", tok)
	case s.state.HasLimit:
		l.add(" / ", tokens.TextTertiary)
		l.add(tokens.Money(s.state.LimitUSD), tok)
	}
}

// renderField is the open editor.
func (s *Spend) renderField(l *lineBuf) {
	tok := tokens.TextPrimary
	if s.invalid {
		tok = tokens.Coral
	}
	l.add(s.glyph(), tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	for i, r := range s.runes {
		t := tok
		if i == s.cursor {
			t = tokens.Promote(tok)
		}
		l.add(string(r), t)
	}
	if s.cursor >= len(s.runes) {
		// The field's own end mark. It is the block cursor a one-line numeric
		// field wants, and it is [tokens.GlyphAccentRail] rather than a new
		// character because the vocabulary already owns a one-cell vertical bar.
		l.add(tokens.GlyphAccentRail, tokens.Promote(tok))
	}
}

// tier brightens the segment one step while it holds focus (5.22 rule 5's
// amendment to 5.13: an interactive chip may never live permanently in the
// dimmest tier).
func (s *Spend) tier(t tokens.Token) tokens.Token {
	if s.focused {
		return tokens.Promote(t)
	}
	return t
}

func (s *Spend) glyph() string { return s.glyphs.Glyph(tokens.GSpend) }

// parseUSD reads the field. It refuses anything a strconv would accept but a
// budget should not — a negative, a NaN, an exponent — because the door on the
// other side takes dollars and a surface that passed one of those on would be
// asking the store to store nonsense.
func parseUSD(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	if raw == "" {
		return 0, false
	}
	for _, r := range raw {
		if (r < '0' || r > '9') && r != '.' {
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// trimZeros renders a dollar figure for EDITING — the number without padding,
// without a currency mark and without a thousands separator, because every one
// of those is a character the person would have to delete before typing.
func trimZeros(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
