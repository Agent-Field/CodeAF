// Package homes is the rest of the product as rail-scoped rooms (5.24): the
// notebook, self, standing and services homes, plus the two affordances 5.24
// sites on surfaces that already exist — the spend segment's inline editor and
// the composer's mic.
//
// # Why a package rather than four pages
//
// 5.24's whole argument is that the parts the scope model does not house grow
// their own surfaces again. The old chat proved it: `self` was a page with a
// page-local key grammar, a place enum, its own focus zone and its own esc
// ladder; services were a second page beside it; the notebook was an overlay.
// Four surfaces, four sets of keys, four ways to be lost.
//
// Here every one of them is the SAME object the rail already draws: a scope
// with rows, entered with enter, left with esc, moved through with j/k, and
// previewed in the main pane by whatever the cursor rests on (5.15). This
// package supplies the two halves of that — the [rail.Scope] values, and the
// main-pane renderer for the selected row — and nothing else. It decides
// nothing, journals nothing, reads no store, opens no file and holds no client.
//
// # The shape of the seam
//
//	homes.Rows(state)                 the collapsed dim group, appended to the
//	                                  home scope's rows below the task cards
//	homes.Owns(id) / (*Source).Scope  the four home scopes, as a rail.ScopeSource
//	(*View).Render(state, sel, w, h)   the main pane for the selected row
//	(*Page).Render(state, sel, w, h)   the NOTEBOOK PAGE — see page.go
//	(*Spend).Render / (*Spend).Key    the money segment and its inline editor
//	Mic.Render                        the mic cell on a composer's place line
//
// # The page and the rooms are two surfaces over one State
//
// [Page] is the full-width notebook lens and it draws THREE bands — beliefs,
// know-how, practice — which is this wave's split (audit-notes/notebook-split.md
// §1: the work page holds what the resident is DOING, this page holds what it
// has LEARNED). [View] still draws the four rail rooms above, standing and
// services included, because the rail scopes did not move; what moved is which
// of them the notebook page claims. One [State] feeds both, so the two can never
// disagree about a belief.
//
// Facts arrive through [State], which the wiring fills whenever they move —
// exactly the contract internal/tui2/modelui's Catalog documents, for the same
// reason: a surface that could reach a store would be a surface that reads one
// per keystroke. Every read that fills a [State] field is named in state.go,
// with the ones that DO NOT EXIST YET marked as gaps rather than invented.
//
// # The laws this package is built against
//
//   - 5.15, the one rule: you talk to what you are looking at, and the
//     affordance never lies. A service is not a conversation, so a service row
//     binds [rail.ComposerNone] and the wiring disables the composer over it
//     (5.24). Nothing else here silences a composer, because a person may
//     always ask aforge about a belief or a charter.
//   - 5.16 / 5.17, through internal/tui2/tokens and nothing else. No raw ANSI,
//     no hardcoded colour, no glyph literal tokens already names. Where a
//     rendering needs a glyph the vocabulary has no slot for, it reuses an
//     existing slot and says why (see [Mic] and voice.go) rather than minting a
//     ninth meaning.
//   - 12.5.1, the artifact law: a home never carries a deliverable inline. The
//     craft shelf, the log tail and the belief list all point at paths; the
//     paths render with the middle ellipsis [blocks.TruncatePath] gives them,
//     because tail-truncating a path throws away the filename.
//   - 12.5.2, the truncation law: a log tail that dropped lines says how many
//     and renders visibly cut ([tokens.GlyphCut]), never silently short.
//   - 12.5.3, the repair doctrine, is a head law and has no rendering here; it
//     appears only as the reason a failed service's row keeps its restart verb
//     rather than being drawn dead.
//   - 5.22, no typed-only actions: the verbs on a focused charter or service
//     are drawn from the command registry by the WIRING and handed over as
//     [Verb] values. This package will not invent a verb, a key or an id,
//     because a second list of verbs beside the registry is exactly the drift
//     5.22 exists to forbid.
//
// # Two things this package deliberately does not do
//
// It does not own the collapse state of the home group, and it does not own the
// cursor. Both belong to the rail model that already has them; [State.Expanded]
// and [Selection] are the wiring telling this package what those objects say.
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package homes
