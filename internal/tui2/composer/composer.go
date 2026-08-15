package composer

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Hint is what the empty composer says: where the reader is, and what to do
// next (8, as amended). It is the product's "what now" voice, so it is STATE
// rather than a label — a placeholder that read "Type a message" told a person
// staring at a text field that it was a text field, and told them nothing on
// the four occasions when something had actually happened.
//
// The host sets it, because every one of these facts belongs to the room and
// none of them belongs to a draft buffer. The one exception is [HintWorking],
// which this package asserts for itself whenever [Model.Streaming] is on — the
// spinner is one cell to the left of these words and the two may not disagree.
type Hint uint8

const (
	// HintIdle is the resting state: nothing has happened, so the line teaches
	// the three grammars this surface has.
	HintIdle Hint = iota
	// HintWorking is a reply being written right now. It answers the question a
	// person actually has while watching one arrive — may I type?
	//
	// It does NOT name the key that stops the turn any more, and the reason is
	// §8.2.21 rather than tidiness: this package asserts the working state off
	// [Model.Streaming] and cannot know whether esc would in fact interrupt —
	// a visitor window watching a turn it does not run cannot stop it — so the
	// accelerator was being promised on frames where it did nothing. The bar
	// row's `interrupt esc` chip is gated on exactly that fact and is one row
	// away, which also makes this §15's same-fact-twice removed.
	HintWorking
	// HintDelivered is a delivery card that just landed in this room.
	HintDelivered
	// HintQuestion is an open question waiting on this person.
	HintQuestion
	// HintSettled is a room whose work is over. It is the sentence the disabled
	// composer used to draw for itself, moved here so there is ONE mechanism for
	// what the empty line says (the amendment's "unify it").
	HintSettled
	// HintSteer is the steer line (5.11): words that go one way, into work that
	// is already running.
	HintSteer
	hintCount
)

// hintCells is the copy, one row per state, as cells that shed from the right.
//
// They are cells rather than sentences because a narrow terminal has to drop
// the accelerator and keep the sentence, and a sentence cut in half teaches a
// key that does not exist. The first cell of every row is the one that survives
// to the last column — it is the half that says where you are.
//
// Chrome is lowercase (16), and every accelerator cell leads with the exact
// character it names, because the hint IS the keystroke: a reader who copies
// what they see has already done the thing.
var hintCells = [hintCount][]string{
	HintIdle:      {"ask for anything", "@ jobs", "/ commands"},
	HintWorking:   {"keep typing — messages queue"},
	HintDelivered: {"ask about the results", "r rerun"},
	HintQuestion:  {"answer 1–3, or say it in words"},
	HintSettled:   {"this work is settled — ask about it"},
	HintSteer:     {"steer — one-way"},
}

// cells is the copy for a state, with detail substituted for the leading cell
// when the host supplied one. An out-of-range state reads as [HintIdle] rather
// than as nothing: a render must not go quiet because a caller handed it a
// number.
func (h Hint) cells(detail string) []string {
	if h >= hintCount {
		h = HintIdle
	}
	row := hintCells[h]
	if detail == "" || len(row) == 0 {
		return row
	}
	out := make([]string, len(row))
	copy(out, row)
	out[0] = detail
	return out
}

// caretBlock is the accent block the caret is drawn as (5.8). It is READ from
// the gauge ladder's full cell rather than written out again here: the block is
// already in the 5.17 vocabulary, measured by the glyph gate against both
// shipping rulers, and a second spelling of one character is a second thing to
// keep true.
//
// It is drawn as a CHARACTER rather than as a background, which is what makes
// the caret survive a profile with no background to spend
// ([tokens.SelectionMarker]) and a terminal whose own cursor the shell has
// switched off (tui2/shell.go: "the cursor belongs to the composer").
var caretBlock = tokens.GaugeCells[len(tokens.GaugeCells)-1]

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

// Prompt is which of 5.15's two composers this one is: the chat prompt, or the
// steer line's. It exists because the affordance must never lie about where a
// draft will land, and the wiring is the half that knows — the region binds a
// settled row's composer differently from the main thread's.
//
// It is a composer-local enum rather than a [tokens.GlyphID] for one reason
// that matters: the zero value has to be the chat prompt, and glyph slot zero
// is the queued state. A zero Options is a chat composer.
type Prompt uint8

const (
	// PromptChat is the main thread's prompt: words that become a message.
	PromptChat Prompt = iota
	// PromptSteer is the steer line's: words that become work (5.11).
	PromptSteer
)

// glyph resolves the prompt through the glyph tier, so a patched font draws the
// icon and every other terminal draws the 5.17 twin. A nil Styler resolves the
// plain tier, which is what [tokens.Styler.Glyph] promises a caller replacing a
// constant with a lookup.
func (p Prompt) glyph(sty *tokens.Styler) string {
	if p == PromptSteer {
		return sty.Glyph(tokens.GPromptSteer)
	}
	return sty.Glyph(tokens.GPromptChat)
}

// State is what is TRUE of this composer right now, as one cell of colour.
//
// It is a third axis beside [Prompt] and [Hint], and the split is worth stating
// because all three end up within two cells of each other. Prompt is what this
// composer IS — a chat line or a steer line — and it does not change while the
// reader sits there. Hint is what the empty line SAYS, in words, and it is gone
// the moment anyone types. State is what the surface is DOING, and it is the
// only one of the three that must remain legible with a full draft in the
// buffer, which is why it is carried by colour and by one glyph rather than by
// a sentence: the edge and the prompt are the two cells that are never covered
// by the reader's own words.
//
// The streaming state is deliberately NOT a member. It arrives through
// [Model.Streaming] because it also drives the spinner's frame, and one fact
// with two doors is one fact that can be told two different things; Streaming
// therefore outranks every value here, exactly as it already outranks the hint.
type State uint8

const (
	// StateIdle is the resting state: the prompt in the accent, awake when this
	// composer holds the keyboard and quiet when it does not.
	StateIdle State = iota
	// StateQuestion is an open question waiting on this person. Amber, because
	// §12 spends amber on exactly one meaning — a human is actually needed —
	// and this is the surface that human is standing at.
	StateQuestion
	// StateFailed is the last send failing. Coral, because §12 spends coral on
	// broken, and the draft KEEPS the words that did not go: a composer that
	// cleared the buffer on a failed send would have destroyed the one copy of
	// the sentence at the exact moment the reader needs to press enter again.
	StateFailed
	stateCount
)

// cell is the glyph slot and the tier a state REPLACES the prompt with, and
// whether it replaces it at all. The glyph and the token come back together
// because the edge and the prompt are painted with one token — §12's affordance
// law read at one cell: a mark and its colour say one thing, and a glyph that
// changed without its colour would be half a signal.
//
// The glyphs are slots rather than constants so the nerd-font tier draws its own
// shapes without this file learning about them, and the plain twins are the
// 5.17 characters every other surface already spells these two states with.
// [StateIdle] answers false, because idle is not a state that overrides the
// prompt — it IS the prompt, and which prompt this composer wears is [Prompt]'s
// question and not this one's.
func (s State) cell() (tokens.GlyphID, tokens.Token, bool) {
	switch s {
	case StateQuestion:
		return tokens.GNeedsHuman, tokens.Amber, true
	case StateFailed:
		return tokens.GFailed, tokens.Coral, true
	}
	return 0, 0, false
}

// Options configures a [Model]. Every field's name and type is the contract
// the sibling wiring (internal/tui2/chat) builds against — see doc.go.
type Options struct {
	// Prompt is which prompt this composer wears (5.15). The zero value is
	// [PromptChat].
	//
	// It is the seam internal/tui2/chat asked for in prose ("composer.Options
	// should carry Prompt and Placeholder"): the region used to swap the glyph
	// by rewriting this package's output with a string replace, which cannot
	// survive a glyph tier that draws a different character. A room whose
	// composer means something different now SAYS so — through this at
	// construction, and through [Model.Bind] and [Model.SetHint] as the room
	// changes under it.
	Prompt Prompt

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
	prompt      Prompt

	// hint is what the empty line says and the host's optional substitute for
	// its leading cell (see [Hint], [Model.SetHint]).
	hint       Hint
	hintDetail string

	// hostCursor says the SHELL is placing the terminal's own cursor at
	// [Model.CaretAt] (tui2's View.Cursor), so this package must not paint a
	// second one. Two caret cells on one screen is the surface disagreeing with
	// itself about where typing lands.
	hostCursor bool

	value   []rune
	cursor  int
	focused bool

	// typed says this draft has been written in. It is what makes the ghost
	// hints vanish on the first keystroke and STAY gone: they teach an opening
	// move, and a person who has just backspaced their sentence away is not at
	// the opening. It clears when the draft itself ends — sent, stashed or
	// killed (see [Model.reset] and [Model.KillToStart]) — because that is when
	// there is an opening again.
	typed bool

	// streaming says a reply is being written right now, and frame is the step
	// of the shell's ONE animation clock the prompt's spinner is drawn at.
	// Together they are 11's whole motion budget as this surface spends it: one
	// braille spinner, at the prompt, and nothing else here moves.
	//
	// The frame arrives from outside because a token layer that read the wall
	// clock would be a token layer with a state (tokens.Spinner's own comment),
	// and the same is true of a pane: Render is a pure function of the model,
	// so the clock is latched above and handed down.
	streaming bool
	frame     int

	// state is what the surface is doing, as one cell of colour (see [State]).
	// It is set by the host through [Model.SetState] for the same reason the
	// hint is: an open question and a failed send are facts about the ROOM, and
	// a draft buffer is not entitled to an opinion about either.
	state State

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
		prompt:      opts.Prompt,
	}
}

// Bind sets which composer this is, for a region whose binding moves with the
// row the reader selected (5.15).
//
// [Options.Prompt] is the resting binding a composer is built with; this is the
// same value said again, because the binding is the WIRING's state and a copy of
// it here would age by one keystroke. It is a setter for exactly the reason
// [Model.Focus] is one: Render must stay a pure function of the model
// (tui2/pane.go), so what the frame is about arrives before the frame, never
// inside it.
//
// The alternative this replaces was the region rewriting this package's output
// with a string replace, which cannot survive a glyph tier that draws a
// different character for the same slot.
func (m *Model) Bind(prompt Prompt) { m.prompt = prompt }

// SetState sets what the surface is doing (see [State]). An out-of-range value
// reads as [StateIdle] rather than panicking a render, on the same rule
// [Hint.cells] states: a paint must not go quiet or die because a caller handed
// it a number.
func (m *Model) SetState(state State) {
	if state >= stateCount {
		state = StateIdle
	}
	m.state = state
}

// SetHint sets what the empty line says (see [Hint]). detail replaces the
// state's leading cell when it is non-empty — the room's own words where the
// state has a shape but not a sentence — and changes nothing else, so the
// accelerators a state teaches cannot be edited away by a caller writing prose.
//
// It is a setter for the same reason [Model.Bind] is: the facts it carries are
// the room's, and Render is a pure function of the model.
func (m *Model) SetHint(state Hint, detail string) {
	m.hint = state
	m.hintDetail = detail
}

// hintNow is the state the line actually draws, which is the host's EXCEPT
// while a reply is arriving: the spinner is one cell to the left of these
// words, and a line that said "ask for anything" beside a turning spinner would
// be the surface contradicting itself in the space of two columns.
func (m *Model) hintNow() Hint {
	if m.streaming {
		return HintWorking
	}
	return m.hint
}

// Streaming tells the composer that a reply is being written right now, and at
// which step of the shell's shared animation clock (see [blocks.Clock.Frame]).
//
// It is the composer's whole side of 8's "the streaming indicator lives at the
// prompt, not as chat rows": the prompt cell becomes the braille spinner while
// this is on, and the transcript below is left to say nothing at all. The
// composer never decides WHETHER a reply is in flight — it is told, by the one
// package that polls the engine — and it never reads a clock, so two composers
// and a rail card drawn in one frame tick in lockstep by construction.
//
// A frame from a calm clock is 0 (10.1.5's reduced motion), which stops the
// spinner on its first glyph without this package knowing that linear mode
// exists.
func (m *Model) Streaming(on bool, frame int) {
	m.streaming = on
	m.frame = frame
}

// -- the terminal's own cursor (8, 11's third motion) -------------------------

// HostCursor tells the composer that the SHELL is placing the terminal's real
// cursor on the cell [Model.CaretAt] reports, so this package stops painting one
// of its own. Two caret cells on one screen is the surface disagreeing with
// itself about where typing lands.
//
// It is off by default, which keeps a composer that nobody has wired a cursor
// for — a test, a preview, a linear-mode frame — showing a caret the reader can
// find. The painted block is therefore the FLOOR and the terminal cursor is the
// enhancement, which is the same direction every other degradation in this tree
// runs.
func (m *Model) HostCursor(on bool) { m.hostCursor = on }

// CaretAt is the cell the caret occupies inside a rectangle of this size, so a
// host can put the terminal's real cursor exactly where the paint put the
// caret. It reports false when the caret is not on screen at all — an unfocused
// composer, a degenerate rectangle, or a caret scrolled out of a draft taller
// than its room (which [Model.plan] does not do, and which this answers
// honestly anyway rather than pointing at a row that is not there).
//
// The geometry is the paint's own, run again (render.go), so the block and the
// terminal cursor cannot land on different cells.
func (m *Model) CaretAt(width, height int) (x, y int, ok bool) {
	if !m.focused || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	p := m.plan(m.activeStyler(), width, height)
	row := p.cursorRow - p.first
	if row < 0 || row >= p.visible {
		return 0, 0, false
	}
	col := 0
	if sp := p.rows[p.cursorRow]; m.cursor > sp.Start {
		for i := sp.Start; i < m.cursor && i < sp.End; i++ {
			col += runeCells(m.value[i])
		}
	}
	return TextColumn(width) + col, p.draftTop() + row, true
}

// The DECSCUSR request 11's third motion is spent on: a BLINKING BLOCK while
// the composer holds the keyboard, and the terminal's own default the moment it
// does not.
//
// Bubble Tea already writes exactly these bytes when a View declares a Cursor
// (its renderer encodes shape+blink into the same CSI Ps SP q), and that is the
// path the shell should take — it also restores the default on quit, on suspend
// and on the panic teardown, which raw bytes from a pane could not promise. The
// sequences are named and built here anyway, for two reasons: a host that
// manages its own terminal can ask for them by name instead of by memory, and a
// request with a test is a request that cannot rot into the wrong Ps.
//
// A terminal that does not implement DECSCUSR ignores the sequence whole. That
// is what makes this a progressive enhancement rather than a requirement: the
// painted block is still the floor.
const (
	// CursorBlinkingBlock is DECSCUSR Ps=1.
	CursorBlinkingBlock = "\x1b[1 q"
	// CursorDefault is DECSCUSR Ps=0: whatever the user's terminal was set to
	// before we asked. It is the honest restore — there is no way to read the
	// original shape back on a terminal that does not answer DECRQSS, so
	// "restore" means "stop asking" rather than a value we saved.
	CursorDefault = "\x1b[0 q"
)

// CursorSequence is the terminal-mode bytes for a composer that does or does
// not hold the keyboard. It is deliberately a pure function of one bool rather
// than a queue: the caller writes it when custody moves and on the way out, and
// a function with no memory cannot leak a state across a resize.
func CursorSequence(focused bool) string {
	if focused {
		return CursorBlinkingBlock
	}
	return CursorDefault
}

// Value returns the current draft, unstyled and untrimmed.
func (m *Model) Value() string { return string(m.value) }

// Focus implements tui2.PaneFocus. Dimming is a property of the pane
// (8.3), not of the draft, so this only flips which [tokens.Focus] Render
// paints with — it never touches the text or the cursor position.
func (m *Model) Focus(focused bool) { m.focused = focused }

// Focused reports whether the draft has the keyboard. It exists for the one
// caller that has to know without owning the answer: the region draws a place
// line whose "show the whole path" tier is its own focus, and a pointer resting
// on that line reveals the same thing — so the region has to be able to put the
// focus back where the keyboard actually is when the pointer leaves.
func (m *Model) Focused() bool { return m.focused }

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
