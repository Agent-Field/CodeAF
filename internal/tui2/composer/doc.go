// Package composer is the v2 chat composer: a multi-line draft editor bound
// to one region of the shell (internal/tui2), styled only through
// [tokens.Styler] and never touching a raw ANSI escape of its own.
//
// # Contract with the shell
//
// [Model] satisfies tui2.Pane, tui2.PaneKeys and tui2.PaneFocus (see
// internal/tui2/pane.go): Render(w, h) is a pure function of the draft and
// the given rectangle, Key handles the keyboard while the composer holds
// focus, and Focus tells it whether it is the pane the user is talking to
// (5.15). [SendKey] and [NewlineKeys] in [Options] come from the shell's
// capability negotiation (internal/tui2/caps.go) — the composer binds
// whatever it is given and never hardcodes a chord, which is what keeps
// alt+enter working unconditionally (10.1.2: no important chord may require
// Shift+Enter or a key-release event, because tmux can deliver neither).
//
// # The esc law (8.2.21), non-negotiable
//
// Esc against a NON-EMPTY draft stashes it: the text is cleared from the
// composer but kept in the recall ring (see below) where the next ↑ reaches
// it. This is [Model.Key] fully consuming the keystroke — typed text is
// never destroyed, only moved aside.
//
// Esc against an EMPTY draft is NOT the composer's decision. Whether that
// esc should interrupt a turn the user is watching or pop scope/navigate
// depends on state the composer does not have (is the current room
// streaming?), so the composer does not swallow it: [Model.Key] returns a
// command that delivers [EscMsg] to whatever drives the shell's Update loop.
// Producing a message the caller must handle is this architecture's only way
// to say "not consumed, over to you" — a Bubble Tea pane cannot decline a
// keystroke and let it fall through on its own, so a message is the fallthrough.
// The wiring's duty (12.5, the composer's only obligation under it): esc
// handling must never destroy user input and must never swallow the esc a
// turn-in-progress needs to mark itself interrupted. Both halves are upheld
// here — the draft is protected by this package, and the interrupt decision
// is handed, not eaten, to the package that can actually make it.
//
// The recall ring doubles as the "esc-restore" 7.2 asks for: the design note
// allows either a dedicated restore chord or making the stash reachable
// through the existing ring, and this package takes the ring, because it is
// one mechanism instead of two. ↑ from an empty draft walks sent history
// oldest-to-newest and then, last, the stashed draft — so the very next ↑
// after an esc brings the stashed words back, cursor at the end, ready to
// keep typing.
//
// # Paste
//
// Bracketed paste arrives from the terminal as a single tea.PasteMsg, not as
// a tea.KeyPressMsg, so it does not reach [Model.Key] at all — [PaneKeys] has
// no seam for it. This package exposes [Model.Paste] for that: it inserts
// the pasted content as draft text and never calls OnSubmit, however many
// newlines the paste carries (7.2: multi-line paste never auto-sends). The
// shell wiring must route tea.PasteMsg to it — the natural shape, mirroring
// [tui2.PaneMouse] and [tui2.PaneKeys], is a PanePaste interface the wiring
// adds when it starts routing tea.PasteMsg; until that exists, [Model.Paste]
// is here and ready, and the shape it fills is asserted against a local
// pastePane interface in the tests so a signature drift fails this package's
// build rather than surfacing as a silent no-op in the shell. A terminal
// that never advertises bracketed paste falls back to individual key events,
// each of which [Model.Key] handles exactly like typing — a pasted newline
// becomes SendKey on such a terminal exactly as it would if the user had
// actually pressed it, which is a terminal limitation this package cannot
// see through, not a bug here.
//
// # Rendering
//
// Render draws the [tokens.GlyphPromptChat] "›" prompt, the draft (hard
// wrapped to the given width, one style per row, tail-anchored so the
// caret's row is always the last one on screen when the draft outgrows the
// rect), a placeholder when the draft is empty, and an inverted single-cell
// caret when focused. Every row is passed through ansi.Truncate as a last
// step regardless of how it was assembled, so a pathological width (down to
// w=1) or a wide grapheme in a narrow column degrades by clipping rather
// than by exceeding the rectangle the compositor gave it (tui2/pane.go's
// contract) or panicking. Nothing here reads the wall clock or holds a
// blinking-cursor timer: the caret is always drawn when focused, which is
// simpler than a blink loop and correct for the "cursor visible" requirement
// without a timer command to leak.
//
// # Provenance
//
// This package is written fresh against Bubble Tea v2 / Lip Gloss v2. It
// does not import or copy code from internal/tui or its charmbubbles
// textinput fork (v1, single-line, Bubble Tea v1) — the shapes below (a
// []rune buffer, a rune-index cursor, a draft ring with an escape stash) are
// the obvious data model for any line editor and were written from scratch,
// not lifted.
package composer
