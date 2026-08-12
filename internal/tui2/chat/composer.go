package chat

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer region: three parts, one rectangle.
//
// internal/tui2/composer owns the draft: the rune buffer, the caret, the send
// and newline chords the shell negotiated, the recall ring, bracketed paste,
// and the esc law's first half — a non-empty draft is stashed into the ring,
// never destroyed. This package owns the second half: an esc the composer did
// not consume arrives here as [composer.EscMsg], and only this side knows
// whether the room is streaming and therefore what that esc means (8.2.21).
//
// THE REGION IS ONE ROW NOW. It used to be four: a place line welded to its top
// edge, two rows of draft, and a meta strip of numbers welded to its bottom. §7
// dissolved both strips into the bar row under this one — the directory into the
// bar's right zone, the model word into the same zone as the picker's door, the
// turn's cost and age into the middle zone where they exist only while the turn
// does — and what is left here is the draft and whatever it grows for itself: the
// bounded HUD, the `@` and `/` lists, the wrapped rows of a long message.
//
// The reason is §15's, applied to two rows of permanent chrome: a window that
// carried an elapsed cell and a `$—` on every frame of every minute nobody was
// waiting for anything was spending two rows of a conversation on structure
// announcing itself. Everything those rows said is still said; none of it is
// said standing up.
//
// The region is assembled here rather than by the shell because the shell gives
// a pane its whole rectangle and reserves nothing inside it (pane.go's
// contract).

// composerPane is what the app binds into the composer region.
type composerPane interface {
	tui2.Pane
	tui2.PaneKeys
	tui2.PaneFocus
	tui2.PaneMouse
	tui2.PaneHover
	// Draft is the text currently in the buffer. The footer's input-state axis
	// (10.5.26) is read off it, and nothing else consults it — the app never
	// reaches into the draft to change it.
	Draft() string
	// HintRows is how many rows an open inline completion wants under the
	// draft. The region is budgeted by the layout for a draft and two strips,
	// so a list of candidates needs the region to grow — and only the composer
	// knows whether there is a list. See [App.refresh], which is where the
	// answer becomes rows.
	HintRows() int
}

// composerOptions is the composer's own option struct, named here so the app
// reads as one package rather than two.
type composerOptions = composer.Options

// composerStack is the assembled region.
type composerStack struct {
	draft *composer.Model
	// style paints whatever this region draws itself — the disabled sentence,
	// and the hug's own ground under the whole rectangle. The draft carries its
	// own styler; this is the region's.
	style *tokens.Styler

	// mode reports what the composer is bound to right now (5.15). It is a
	// function rather than a field because the binding is the app's state and
	// this region renders it; a copy here would be a second truth that ages by
	// one keystroke.
	mode func() composerBind
	// hud draws the bounded live summary above the draft (8.2.8). It returns
	// the rows it wants and never more than it is offered; nil means this frame
	// has a rail and does not need one.
	hud func(width, height int) []string
	// focus is 5.14's "you talk to what you are looking at", read for a hand
	// instead of an eye: POINTING IS LOOKING, so a click anywhere in this
	// rectangle asks for the keyboard. It is a function for the same reason the
	// two above are — this region can tell that it was pointed at and cannot
	// know whether the keyboard is currently on the map, which is the app's
	// flag. It reports whether custody actually moved, because a disabled
	// composer refuses it (App.focusConversation) and a caret must not be placed
	// in a draft nobody may type into.
	focus func() bool
	// The rectangle this stack was last drawn at, so a pointer can be resolved
	// against the row that is actually on screen.
	lastWidth, lastHeight int
}

var (
	_ tui2.Pane      = (*composerStack)(nil)
	_ tui2.PaneKeys  = (*composerStack)(nil)
	_ tui2.PaneFocus = (*composerStack)(nil)
)

// newComposer builds the region the app binds. hostCursor says the shell will
// place the terminal's own blinking cursor on the caret (§8/§11), so the
// painted block yields to the real one; linear mode keeps the painted block,
// because the shell's cursor stays nil there.
func newComposer(opts composerOptions, hostCursor bool) *composerStack {
	draft := composer.New(opts)
	draft.HostCursor(hostCursor)
	return &composerStack{draft: draft, style: opts.Styler}
}

// Key hands every keystroke to the draft. Nothing else in this rectangle takes
// the keyboard — the region is the draft and the chrome it opens for itself.
//
// A DISABLED composer takes nothing. 5.15's one rule is that a settled row's
// composer is disabled, and a disabled composer that quietly accepted a draft
// nobody could send would be the affordance lying in the most frustrating way
// available: the reader types a paragraph and only then finds out.
func (s *composerStack) Key(msg tea.KeyPressMsg) tea.Cmd {
	if s.disabled() {
		return nil
	}
	return s.draft.Key(msg)
}

// Paste is offered only when the draft accepts one, which is how the app's
// paste door finds it (app.go asks the pane for the method rather than assuming
// it).
func (s *composerStack) Paste(msg tea.PasteMsg) tea.Cmd { return s.draft.Paste(msg) }

// Focus flows to the draft, which is the one part of this region that renders
// differently for it: the caret appears and the ghost line starts speaking.
func (s *composerStack) Focus(focused bool) { s.draft.Focus(focused) }

// Draft is the current buffer.
func (s *composerStack) Draft() string { return s.draft.Value() }

// draftPad is the blank row the region keeps between the words being typed and
// the bar row under them.
//
// It is a SPACE and not a mark, which is §16's whole answer to separation, and
// it is here because a reader on the live build said the writing area "feels
// cramped" — the draft sat one row above a row of telemetry with nothing
// between them, so the sentence being composed read as another line of chrome.
// One blank row is the cheapest thing that makes the composer a place rather
// than a field, and it is the same padding rhythm a card gets inside its ground.
const draftPad = 1

// draftFloor is the least room the writing area gets even when it is empty.
//
// TWO ROWS, and the second one is deliberately blank most of the time. A
// one-row composer is a text INPUT — it says "type a short thing here" — and
// this surface's whole premise is that a person describes work in sentences. The
// floor costs one row of transcript and buys the reader somewhere to look while
// they think; past it the region grows upward as the draft wraps, exactly as
// before ([composer.Model.GrowRows]).
const draftFloor = 2

// draftBand is where the composer's own rows sit inside this region and how
// many of them there are: the bounded HUD borrows from the top, the padding row
// takes the bottom, and the draft takes the rest. It restates Render's own
// reservations rather than recording them, so the pointer and the paint answer
// the same question from the same numbers.
func (s *composerStack) draftBand() (top, body int) {
	height := s.lastHeight
	if height <= 0 {
		return 0, 0
	}
	body = height
	// The padding row is given up before the draft is: a region squeezed to two
	// rows should spend both on words.
	if body > draftFloor {
		body -= draftPad
	}
	if s.hud != nil && body > 1 {
		if rows := len(s.hud(s.lastWidth, body-1)); rows > 0 {
			body -= rows
			top += rows
		}
	}
	return top, body
}

// Mouse takes the keyboard, places the caret, and performs a chip.
//
// The first of those three is the correction this lane exists for. The comment
// that used to stand here said "the draft is where the keyboard already is by
// the time this runs", and it was reading the SHELL's rule (a click focuses the
// layer it hit) as though it were the whole story. It is not: this surface
// keeps its own custody flag, because the map's keyboard is the app's and not
// the shell's (rooms.go), and the shell moving its own LayerID changed nothing
// a key would do. So the map kept the keyboard through a click on the draft,
// and the only way back was ctrl+o — reported verbatim as "clicking on the
// typing part or anywhere does not seem to go there — I have to press ctrl+o".
func (s *composerStack) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return nil
	}
	// Pointing at the composer is talking to the composer. It happens before
	// anything below decides what the click also DID, because it is true of
	// every cell of this rectangle including the two chips and the empty rows
	// between them — one rule for the whole region, which is what makes it a
	// rule a reader can hold rather than a map they have to learn.
	took := s.focus == nil || s.focus()

	// The candidate list next: it is drawn inside this rectangle, between the
	// two chips, and it is the only part of the region where a row means
	// something on its own. draftTop is where the composer's own rows begin —
	// the place line and the HUD sit above them.
	if s.draft != nil {
		top, body := s.draftBand()
		if body > 0 {
			if cmd, taken := s.draft.ClickHint(s.lastWidth, body, local.Y-top); taken {
				return cmd
			}
			// The caret goes where the finger went. It is offered only to a
			// composer that actually holds the keyboard: placing a caret in a
			// draft that refuses every key would be the affordance lying in the
			// quietest way it can, and a disabled composer refuses (5.15).
			if took && !s.disabled() {
				s.draft.ClickCaret(s.lastWidth, body, local.X, local.Y-top)
			}
		}
	}
	return nil
}

// Hover lights the candidate row the pointer is on, and nothing else: the two
// chips this region used to carry moved to the bar row with the strips they
// stood on, and what is left in this rectangle is the draft and its own lists.
func (s *composerStack) Hover(local image.Point, inside bool) bool {
	if !inside || s.draft == nil {
		return false
	}
	top, body := s.draftBand()
	return body > 0 && s.draft.HoverHint(s.lastWidth, body, local.Y-top)
}

// HintRows is every row the draft wants beyond its anchor row: its own wrapped
// rows (capped) plus an open completion's. The stack adds nothing to the number
// — it has nothing of its own left to add.
func (s *composerStack) HintRows() int { return s.draft.GrowRows(s.lastWidth) }

func (s *composerStack) Streaming(on bool, frame int) { s.draft.Streaming(on, frame) }

func (s *composerStack) SetHint(state composer.Hint, detail string) { s.draft.SetHint(state, detail) }

// SetState is §7's state channel: what the prompt cell and the edge beside it
// say about this room right now.
func (s *composerStack) SetState(state composer.State) { s.draft.SetState(state) }

// Restore puts a sent draft back after the post failed. See
// [composer.Model.Restore] for why the composer cannot do this for itself and
// why it refuses when the reader has started typing again.
func (s *composerStack) Restore(text string) bool { return s.draft.Restore(text) }

// Seed drops a steering sentence into an empty draft — the finished form of a
// chip that says what to say (itemverb.go). It shares [composer.Model.Restore]'s
// refusal: words the person already typed are never overwritten.
func (s *composerStack) Seed(text string) bool { return s.draft.Restore(text) }

// CaretAt is the draft's caret moved down by the rows the HUD took, so the
// shell can place the terminal's own cursor on it.
func (s *composerStack) CaretAt(width, height int) (int, int, bool) {
	s.lastWidth, s.lastHeight = width, height
	top, body := s.draftBand()
	if body <= 0 || s.disabled() {
		return 0, 0, false
	}
	x, y, ok := s.draft.CaretAt(width, body)
	return x, y + top, ok
}

// KillToStart is the clear-draft verb reached from the `?` sheet or the palette
// rather than from ctrl+u. The chord itself never comes through here — it is an
// ordinary keystroke and Key hands it to the draft like any other — so this
// exists only so the registry row has a handler, and it goes through the same
// [composer.Model.KillToStart] the chord does rather than clearing the buffer a
// second way. A disabled composer has no draft to clear and refuses, on the same
// rule Key states above it.
func (s *composerStack) KillToStart() {
	if s.disabled() {
		return
	}
	s.draft.KillToStart()
}

// Render lays the region out from the bottom up: the draft owns the last rows
// and the bounded HUD borrows whatever is above them.
//
// The two strips that used to be reserved here are gone (§7), and with them the
// rule that the draft was "the last to go" — it is now the only thing that can
// go, which makes the arithmetic honest rather than defensive.
func (s *composerStack) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	s.lastWidth, s.lastHeight = width, height
	rows := make([]string, 0, height)
	body := height
	pad := 0
	if body > draftFloor {
		body -= draftPad
		pad = draftPad
	}
	// The bounded HUD (8.2.8) borrows from the DRAFT and from nothing else, and
	// never takes its last row. What it cannot fit, its fold line accounts for —
	// that is what "bounded" means here.
	var summary []string
	if s.hud != nil && body > 1 {
		if summary = s.hud(width, body-1); len(summary) > 0 {
			body -= len(summary)
		}
	}
	rows = append(rows, summary...)
	drafted := strings.Split(s.renderDraft(width, body), "\n")
	if len(drafted) == 1 && drafted[0] == "" {
		drafted = nil
	}
	for i := 0; i < body; i++ {
		if i < len(drafted) {
			rows = append(rows, drafted[i])
			continue
		}
		// The draft is shorter than its room. The blank rows are emitted rather
		// than skipped so the region keeps its full rectangle and the ground
		// under it stays a rectangle too.
		rows = append(rows, "")
	}
	for range pad {
		rows = append(rows, "")
	}
	// The region stands on the LIGHTER of the hug's two rungs (§16 SURFACE SEAMS
	// ARE GROUNDS): scrolling content stops visibly at the composer's top edge,
	// and the row you type into is the nearer plane of the two.
	return seamStrip(s.styler(), strings.Join(rows, "\n"), width, height, tokens.HugGroundInput)
}

// -- what the composer is bound to (5.15) ------------------------------------

// steerPlaceholder is the steer line's idle hint (5.11: "steer — one-way").
const steerPlaceholder = "steer — one-way"

// bind reports what this region is bound to, defaulting to an ordinary chat so
// a stack built without an app behind it is still a composer.
func (s *composerStack) bind() composerBind {
	if s.mode == nil {
		return composerBind{mode: rail.ComposerChat}
	}
	return s.mode()
}

func (s *composerStack) disabled() bool { return s.bind().mode == rail.ComposerDisabled }

// renderDraft draws the draft in whatever mode the selected row bound.
//
// A DISABLED composer is not a greyed-out text field: it is a sentence saying
// why, drawn where the draft would have been (5.20 rule 3). Nothing takes the
// keyboard, no caret is shown, and no prompt glyph promises a send.
//
// A STEER composer is the ordinary draft with its two visible words changed —
// the prompt glyph and the idle hint — because everything else about typing one
// line of text is the same and forking the editor would fork its bugs. The
// glyph swap is width-preserving by construction: both prompts are single-cell
// glyphs from the same table (5.17), so the row's fit is unchanged.
func (s *composerStack) renderDraft(width, body int) string {
	if body <= 0 || width <= 0 {
		return ""
	}
	bind := s.bind()
	if bind.mode == rail.ComposerDisabled {
		note := bind.note
		if note == "" {
			note = "this work is settled — ask aforge about it"
		}
		line := blocks.Truncate(tokens.GlyphMissing+" "+note, width)
		if s.draft != nil {
			line = paintDim(s.styler(), line)
		}
		rows := make([]string, body)
		rows[0] = line
		return strings.Join(rows, "\n")
	}
	// The prompt and the idle words are the composer's own now (Prompt enum +
	// state-driven hints), so the steer dressing is a binding, not a rewrite.
	if bind.mode == rail.ComposerSteer {
		s.draft.Bind(composer.PromptSteer)
		s.draft.SetHint(composer.HintSteer, "")
	} else {
		s.draft.Bind(composer.PromptChat)
	}
	return s.draft.Render(width, body)
}

// styler is the region's painter, at the focus the draft is drawn with.
func (s *composerStack) styler() *tokens.Styler { return s.style }

// paintDim draws chrome-tier text, or plain text when there is no profile.
func paintDim(style *tokens.Styler, text string) string {
	if style == nil {
		return text
	}
	return style.PaintToken(text, tokens.TextTertiary)
}
