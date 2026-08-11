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
// One thing sits in front of that law, and only one: an OPEN `@` filter (see
// below) takes esc to close itself, leaving the draft exactly as typed. That is
// not an exception to the law but the same rule 8.2.21 settles it with — esc
// acts on what the user is watching — and the moment the filter is closed, esc
// means what it always meant here.
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
// # Clearing a draft on purpose (ctrl+u)
//
// esc is not the clear key, and it never was: it is the ladder key (interrupt,
// pop scope, go to the live edge) and 8.2.21's non-negotiable is that it never
// destroys a draft. So the draft has its own kill chord, ctrl+u —
// [Model.KillToStart], readline's unix-line-discard, the same act
// internal/tui and internal/tui2/consentui bind it to. What it removes goes
// into the stash below rather than into nothing, so the ring brings it back.
// It is listed in the command registry as `key.thread.clear-draft` and is
// therefore in the `?` sheet and the palette, per 5.22: a key is an
// accelerator for a verb on a visible object, never the only door.
//
// The recall ring doubles as the "esc-restore" 7.2 asks for: the design note
// allows either a dedicated restore chord or making the stash reachable
// through the existing ring, and this package takes the ring, because it is
// one mechanism instead of two. ↑ from an empty draft walks sent history
// oldest-to-newest and then, last, the stashed draft — so the very next ↑
// after an esc brings the stashed words back, cursor at the end, ready to
// keep typing.
//
// # The `@` grammar (5.18, 5.22), and how a room adopts it
//
// Typing '@' at a word boundary opens an inline as-you-type filter over the
// wiring's dispatch targets: live tasks first with their attention glyph and
// identity hue, then a dim `history` group of settled ones, fuzzy over task
// word + title with the typed characters lit (filter.go, hint.go). Enter or tab
// completes the mention into the draft as a hue-marked token; esc closes the
// filter and touches nothing. Once a token exists, the dispatch chip appears
// under the draft — `↵ stay · ⌃↵ follow` — because 5.22 does not allow the
// power chord to be invisible while it is relevant.
//
// A completed mention is one object: backspace at its edge removes the whole
// token, and it comes back whole out of the esc stash and the recall ring. It
// is not tracked across edits to achieve that — a mention is derived from the
// draft text and the current target list on every edit (mention.go), so the
// text and the token can never disagree and a target that leaves the rail stops
// being addressable at once.
//
// Adoption is two fields and nothing else:
//
//	composer.New(composer.Options{
//		OnSubmit:   send,                 // unchanged; still fires for plain prose
//		Targets:    func() []composer.Target { ... },
//		OnDispatch: func(d composer.Dispatch) { ... },
//	})
//
// Targets is a cheap snapshot of what may be addressed, called when a filter
// opens and thereafter only while the draft holds an '@'. OnDispatch receives
// the addressed sends: the target's ID, whether it was Settled (5.18: settled
// targets are addressed ABOUT, not TO — route those to the main head as
// referenced context), whether the send asked to Follow (ctrl+enter), and the
// trimmed Text with its token still in it. Everything else stays where it was:
// unaddressed prose goes to OnSubmit, and so does an addressed draft when
// OnDispatch is nil, so wiring Targets first and OnDispatch later is safe.
//
// A nil Targets is the whole opt-out, tested byte-for-byte: no filter, no
// token, no chip, no chord, and Render's output identical to this package's
// before the grammar existed. The composer never journals, never routes, and
// never decides what a dispatch means — it reports what the user addressed.
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
