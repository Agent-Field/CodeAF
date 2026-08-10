package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer seam, and the smallest thing that can stand in it.
//
// internal/tui2/composer is a lane of its own — draft ring, attachments, the
// meta strip, bracketed paste, the recall stash. This file is not that. It is
// the minimum surface that lets the rest of the assembly be built and proven
// while that lane lands: a draft, a caret, the send and newline chords the
// shell negotiated, and the esc law.
//
// The seam is deliberately an interface plus a constructor variable, so
// adopting the real package is one assignment in one place rather than a
// rewrite of the app: everything below the seam only ever sees composerPane.

// composerPane is what the app needs from a composer: a pane the shell can
// draw, a keyboard while it holds the conversation, and a focus signal.
type composerPane interface {
	tui2.Pane
	tui2.PaneKeys
	tui2.PaneFocus
}

// composerOptions mirrors internal/tui2/composer's Options exactly, so the
// swap is a change of constructor and nothing else.
type composerOptions struct {
	// OnSubmit is called with the draft when the send chord is pressed. The
	// draft is already cleared by then: a submission the composer still holds
	// is a submission that can be sent twice.
	OnSubmit func(string)
	// Styler paints the prompt, the draft and the placeholder.
	Styler *tokens.Styler
	// SendKey and NewlineKeys come from the shell's capability negotiation
	// (10.1.2), never from a literal here: alt+enter is bound unconditionally
	// and shift+enter only where the terminal can tell the two enters apart.
	SendKey     string
	NewlineKeys []string
}

// escMsg is the fallback composer's "not consumed" signal: esc arrived against
// an empty draft, so the decision belongs to whoever knows whether this room is
// streaming. It is the same shape internal/tui2/composer's EscMsg carries, and
// the app handles both.
type escMsg struct{}

// fallbackComposer is the stand-in. It holds a rune buffer, a caret, and the
// ring esc stashes into — nothing else.
type fallbackComposer struct {
	opts composerOptions

	draft  []rune
	caret  int
	ring   []string
	recall int

	focused bool
}

var _ composerPane = (*fallbackComposer)(nil)

// newFallbackComposer builds the stand-in.
func newFallbackComposer(opts composerOptions) *fallbackComposer {
	if strings.TrimSpace(opts.SendKey) == "" {
		opts.SendKey = tui2.SendKey
	}
	if len(opts.NewlineKeys) == 0 {
		opts.NewlineKeys = []string{tui2.LegacyNewlineKey}
	}
	return &fallbackComposer{opts: opts, recall: -1}
}

// Focus records whether the composer is the pane being talked to.
func (c *fallbackComposer) Focus(focused bool) { c.focused = focused }

// Key folds one keystroke into the draft.
//
// The esc law (8.2.21) is the only rule here that is not a convenience: a
// non-empty draft is stashed into the ring, never destroyed, and an esc against
// an empty draft is handed back rather than swallowed, because whether it
// should interrupt a turn is a question this pane cannot answer.
func (c *fallbackComposer) Key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch {
	case key == "esc":
		if len(c.draft) == 0 {
			return func() tea.Msg { return escMsg{} }
		}
		c.stash()
		return nil

	case key == c.opts.SendKey:
		text := strings.TrimSpace(string(c.draft))
		if text == "" {
			return nil
		}
		c.remember(text)
		c.draft, c.caret, c.recall = c.draft[:0], 0, -1
		if c.opts.OnSubmit != nil {
			c.opts.OnSubmit(text)
		}
		return nil

	case c.isNewline(key):
		c.insert('\n')
		return nil

	case key == "backspace":
		if c.caret > 0 {
			c.draft = append(c.draft[:c.caret-1], c.draft[c.caret:]...)
			c.caret--
		}
		return nil

	case key == "delete":
		if c.caret < len(c.draft) {
			c.draft = append(c.draft[:c.caret], c.draft[c.caret+1:]...)
		}
		return nil

	case key == "left":
		if c.caret > 0 {
			c.caret--
		}
		return nil

	case key == "right":
		if c.caret < len(c.draft) {
			c.caret++
		}
		return nil

	case key == "home" || key == "ctrl+a":
		c.caret = 0
		return nil

	case key == "end" || key == "ctrl+e":
		c.caret = len(c.draft)
		return nil

	case key == "up":
		c.walkRing(-1)
		return nil

	case key == "down":
		c.walkRing(1)
		return nil
	}

	// Printable text, including anything a terminal without bracketed paste
	// delivers one key at a time.
	if msg.Text != "" {
		for _, r := range msg.Text {
			c.insert(r)
		}
	}
	return nil
}

func (c *fallbackComposer) isNewline(key string) bool {
	for _, bound := range c.opts.NewlineKeys {
		if key == bound {
			return true
		}
	}
	return false
}

func (c *fallbackComposer) insert(r rune) {
	c.draft = append(c.draft, 0)
	copy(c.draft[c.caret+1:], c.draft[c.caret:])
	c.draft[c.caret] = r
	c.caret++
	c.recall = -1
}

// stash moves the draft aside. The words go into the ring, where the next ↑
// reaches them — moved, not destroyed.
func (c *fallbackComposer) stash() {
	if text := strings.TrimSpace(string(c.draft)); text != "" {
		c.remember(text)
	}
	c.draft, c.caret, c.recall = c.draft[:0], 0, -1
}

func (c *fallbackComposer) remember(text string) {
	if n := len(c.ring); n > 0 && c.ring[n-1] == text {
		return
	}
	c.ring = append(c.ring, text)
	// The ring is a convenience, not a record. The journal is the record.
	const ringLimit = 64
	if len(c.ring) > ringLimit {
		c.ring = append(c.ring[:0], c.ring[len(c.ring)-ringLimit:]...)
	}
}

func (c *fallbackComposer) walkRing(delta int) {
	if len(c.ring) == 0 {
		return
	}
	switch {
	case c.recall < 0 && delta < 0:
		c.recall = len(c.ring) - 1
	case c.recall < 0:
		return
	default:
		c.recall += delta
	}
	if c.recall < 0 {
		c.recall = 0
	}
	if c.recall >= len(c.ring) {
		c.recall, c.draft, c.caret = -1, c.draft[:0], 0
		return
	}
	c.draft = append(c.draft[:0], []rune(c.ring[c.recall])...)
	c.caret = len(c.draft)
}

// Render draws the prompt and the draft, tail-anchored so the caret's row is
// the last one on screen when the draft outgrows the rectangle.
func (c *fallbackComposer) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	style := c.opts.Styler
	paint := func(text string, state blocks.State, hue blocks.Hue) string {
		if style == nil {
			return text
		}
		return style.Paint(text, state, hue)
	}

	prompt := tokens.GlyphPromptChat + " "
	promptWidth := blocks.Width(prompt)
	body := width - promptWidth
	if body < 1 {
		return paint(blocks.Truncate(prompt, width), blocks.StateChrome, blocks.HueNone)
	}

	text := string(c.draft)
	if text == "" {
		hint := blocks.Truncate("ask aforge anything", body)
		return paint(prompt, blocks.StateChrome, blocks.HueAlive) +
			paint(hint, blocks.StateChrome, blocks.HueNone)
	}

	// The caret is drawn as a character rather than moved with an escape: the
	// shell declares a nil terminal cursor, so a caret the reader can see has
	// to be a cell like any other.
	if c.focused {
		runes := append([]rune(nil), c.draft...)
		text = string(runes[:c.caret]) + "█" + string(runes[c.caret:])
	}

	rows, _ := blocks.Wrap(nil, text, body)
	if len(rows) > height {
		rows = rows[len(rows)-height:]
	}
	out := make([]string, 0, len(rows))
	for i, row := range rows {
		lead := prompt
		if i > 0 {
			lead = strings.Repeat(" ", promptWidth)
		}
		out = append(out,
			paint(lead, blocks.StateChrome, blocks.HueAlive)+
				paint(blocks.Truncate(row, body), blocks.StateSettled, blocks.HueNone))
	}
	return strings.Join(out, "\n")
}
