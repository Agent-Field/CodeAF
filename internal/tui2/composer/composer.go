package composer

import (
	tea "charm.land/bubbletea/v2"

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

	// Targets supplies what the `@` filter may address (5.18). It is called
	// when a filter session opens and, thereafter, only while the draft
	// actually contains an '@' — a mention-free draft never asks. Return a
	// cheap snapshot (the rail already holds one); this is a keystroke path.
	//
	// A nil Targets is the whole opt-out: no filter ever opens, no mention is
	// ever derived, no chip is ever drawn, and every byte this package renders
	// and every callback it fires is exactly what it was before the `@` grammar
	// existed. The rooms lane adopts by setting this field and nothing else
	// changes underneath it.
	Targets func() []Target

	// OnDispatch is called INSTEAD of OnSubmit when the sent draft carries a
	// mention token. It is additive: a composer with a nil OnDispatch sends an
	// addressed draft through OnSubmit exactly like any other prose, so wiring
	// Targets first and OnDispatch later is a supported order.
	//
	// The composer never journals, never routes and never decides what a
	// dispatch means — it reports which target the user addressed, whether that
	// target was settled (5.18: settled targets are addressed ABOUT, not TO),
	// whether the send asked to follow, and the text. Everything downstream of
	// that is the wiring's.
	OnDispatch func(Dispatch)

	// Attach decides whether one shell-shaped token in the draft names a file
	// that may ride along, and describes it (see attach.go). It is called on
	// every edit, once per token, so it should be cheap and it must not block.
	//
	// A nil Attach is the whole opt-out, exactly as a nil Targets is for the `@`
	// grammar: no token is ever captured, no chip is ever drawn, and the bytes
	// this package renders are what they were before attachments existed. The
	// composer never touches the filesystem itself — this is the only door
	// through which a path becomes an attachment.
	Attach func(token string) (Attachment, bool)

	// Commands supplies the ONE command catalog the `/` line filters (5.22 rule
	// 3: "a slash command is never a separate implementation; it's a text-shaped
	// view of the registry"). It is called when a line opens and never per
	// keystroke, so a snapshot is enough; the wiring already holds one.
	//
	// A nil Commands is the whole opt-out, on the same rule as Targets: no line
	// ever opens, no row is ever drawn, and a '/' is the ordinary character it
	// always was.
	Commands func() []Command

	// OnCommand performs the completed row. The composer never decides what a
	// command MEANS — it hands back the id it was given and clears the draft,
	// and the wiring routes that id to the same executor its palette and its `?`
	// sheet already use. That is what makes the slash surface a view of the
	// catalog rather than a second copy of it.
	OnCommand func(id string) tea.Cmd

	// OnSend is called INSTEAD of OnSubmit when the sent draft carries
	// attachments, on the same additive rule OnDispatch follows: a composer with
	// a nil OnSend can never hold an attachment either (Attach is what captures
	// them), so wiring cannot half-adopt this into a state where a send is lost.
	//
	// [Send.Text] may be empty. A person who attaches a file and presses enter
	// has sent something, and the body for that message is the wiring's to write
	// — it is the half that knows what a picture is.
	OnSend func(Send)
}

// Model is the composer. The zero value is not meaningful; construct one
// with [New].
type Model struct {
	onSubmit    func(string)
	onDispatch  func(Dispatch)
	onSend      func(Send)
	targets     func() []Target
	commands    func() []Command
	onCommand   func(string) tea.Cmd
	attach      func(string) (Attachment, bool)
	styler      *tokens.Styler
	sendKey     string
	newlineKeys []string

	value   []rune
	cursor  int
	focused bool

	history      []string
	historyStash string
	historyStep  int // 0 = not recalling; N = N entries back from the newest

	// mentions are the `@` tokens currently in the draft, derived from the text
	// after every edit (see mention.go) rather than tracked across edits.
	mentions []mention
	// filter is the open inline `@` session, if any (see filter.go).
	filter mentionFilter

	// slash is the open `/` line, if any (slash.go). It is a sibling of filter
	// rather than a mode of it: the two grammars are independent, only one can be
	// open at a time by construction (a draft that begins with `/` has no word
	// boundary for an `@` to open at), and neither knows the other exists.
	slash slashFilter
	// attachments are the files the draft has captured (see attach.go). Unlike
	// mentions they are HELD rather than derived, because capture takes the
	// token out of the text: there is nothing left in the draft to derive them
	// from, which is exactly what makes them objects the send carries rather
	// than words the send says.
	attachments []Attachment
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
		onDispatch:  opts.OnDispatch,
		onSend:      opts.OnSend,
		targets:     opts.Targets,
		commands:    opts.Commands,
		onCommand:   opts.OnCommand,
		attach:      opts.Attach,
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
