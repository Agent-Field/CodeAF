package composer

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// placeholderText is the idle hint shown while the draft is empty. It is not
// configurable through [Options] — the contract this package ships against
// is exactly OnSubmit/Styler/SendKey/NewlineKeys — so it lives here as one
// small, honest constant rather than as a field nobody asked for.
const placeholderText = "Type a message"

// defaultSendKey is used when Options.SendKey arrives empty (a caller wiring
// up before capability negotiation finishes, or a test that only cares about
// other behavior). It matches tui2/caps.go's SendKey, so a composer built
// with a zero Options is still a composer that can send.
const defaultSendKey = "enter"

// defaultNewlineKey is the newline binding used when Options.NewlineKeys
// arrives empty. It matches tui2/caps.go's LegacyNewlineKey — the one chord
// that survives every terminal (10.1.2) — so an under-specified Options never
// produces a composer that cannot insert a newline at all.
const defaultNewlineKey = "alt+enter"

// Options configures a [Model]. Every field's name and type is the contract
// the sibling wiring (internal/tui2/chat) builds against — see doc.go.
type Options struct {
	// OnSubmit is called with the trimmed draft when SendKey is pressed and
	// the trimmed draft is non-empty. It is never called with an empty or
	// whitespace-only string, and it is never called from a paste.
	OnSubmit func(string)

	// Styler paints every cell this package draws. A nil Styler degrades to
	// unstyled plain text rather than panicking — the same "capability
	// unknown, draw nothing loud" posture [tokens.NewStyler] takes for an
	// out-of-range profile.
	Styler *tokens.Styler

	// SendKey is the keystroke (in tea.KeyPressMsg.String() form, e.g.
	// "enter") that submits the draft. It comes from the shell's capability
	// negotiation (tui2/caps.go's SendKey) rather than being hardcoded here,
	// so this package never has an opinion about what "send" is bound to.
	SendKey string

	// NewlineKeys are every keystroke that inserts a newline instead of
	// submitting. It comes from tui2/caps.go's Capabilities.NewlineKeys(),
	// which always includes alt+enter regardless of what the terminal
	// negotiated (10.1.2) — this package binds the whole slice unconditionally
	// and never special-cases one entry over another.
	NewlineKeys []string
}

// Model is the composer. The zero value is not meaningful; construct one
// with [New].
type Model struct {
	onSubmit    func(string)
	styler      *tokens.Styler
	sendKey     string
	newlineKeys []string

	value   []rune
	cursor  int
	focused bool

	history      []string
	historyStash string
	historyStep  int // 0 = not recalling; N = N entries back from the newest
}

// New builds a composer from opts. It never fails: a missing OnSubmit means
// send is a no-op rather than a nil-panic, a missing Styler degrades to
// plain text, and missing SendKey/NewlineKeys fall back to the bindings
// 10.1.2 requires every terminal to have (see [defaultSendKey],
// [defaultNewlineKey]) so a composer built ahead of capability negotiation
// still works.
func New(opts Options) *Model {
	sendKey := opts.SendKey
	if sendKey == "" {
		sendKey = defaultSendKey
	}
	newlineKeys := opts.NewlineKeys
	if len(newlineKeys) == 0 {
		newlineKeys = []string{defaultNewlineKey}
	} else {
		newlineKeys = append([]string(nil), newlineKeys...)
	}
	return &Model{
		onSubmit:    opts.OnSubmit,
		styler:      opts.Styler,
		sendKey:     sendKey,
		newlineKeys: newlineKeys,
	}
}

// Value returns the current draft, unstyled and untrimmed.
func (m *Model) Value() string { return string(m.value) }

// Focus implements tui2.PaneFocus. Dimming is a property of the pane
// (8.3), not of the draft, so this only flips which [tokens.Focus] Render
// paints with — it never touches the text or the cursor position.
func (m *Model) Focus(focused bool) { m.focused = focused }

// activeStyler resolves the styler to paint with at the current focus. A nil
// base Styler stays nil (plain text); WithFocus is a no-op allocation when
// the base already matches, so a composer that never loses focus never pays
// for the derivation.
func (m *Model) activeStyler() *tokens.Styler {
	if m.styler == nil {
		return nil
	}
	f := tokens.FocusDimmed
	if m.focused {
		f = tokens.FocusNormal
	}
	return m.styler.WithFocus(f)
}

// paint is the one guarded door to the styler: every render call goes
// through this rather than touching m.styler directly, so a nil Styler is
// handled exactly once.
func paint(sty *tokens.Styler, text string, tok tokens.Token) string {
	if sty == nil || text == "" {
		return text
	}
	return sty.PaintToken(text, tok)
}

// paintOn is [paint]'s counterpart for the inverted caret cell.
func paintOn(sty *tokens.Styler, text string, fg, bg tokens.Token) string {
	if sty == nil || text == "" {
		return text
	}
	return sty.PaintOn(text, fg, bg)
}
