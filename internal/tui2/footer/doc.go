// Package footer is the contextual footer of chat-rebuild 5.22 rule 4: one
// dim line under the composer showing the 2-3 most relevant verbs for the
// current focus, rendered FROM [registry] entries — never a hand-rolled verb
// list — plus the small set of other things 10.5.22-10.5.26 and 5.20 rule 6
// put on the same row.
//
// # What lives here versus what does not
//
// 10.5.23 (health vs cost split) draws a hard line this package holds to:
// system health and pending questions live here; this-turn cost and context
// live on the composer's own meta strip. Nothing here duplicates that data.
//
// # The columns
//
// This package's fitting pass runs on [tokens.FitFooter] and [tokens.FooterColumn]
// directly — the priority-drop mechanic 10.5.22 asks for ("a registry of
// columns that drop lowest-priority-first... it shortens, never wraps")
// already lives in tokens, seeded from the exact same law, and
// re-implementing it here would be two copies of one rule drifting apart.
// What this package adds is the CONTENT: up to eight candidate columns, each
// present only when it has something to say, with real content-measured
// widths handed to [tokens.FitFooter] fresh every frame (unlike
// [tokens.FooterColumnOrder]'s static table, a column's width here is
// whatever its actual text costs, not an authored estimate) —
//
//	attention  100  an open question, amber, never dropped before anything
//	           except nothing — a blocked human outranks the row's own space
//	help        90  the permanent "? help" door (5.22 checklist's last item:
//	           "a capability-honesty surface cannot itself be a memory test")
//	hint        85  esc-interrupt (5.20 rule 6) when it is live, else the
//	           input-state hint (10.5.26/8.2.19's Warp pattern) when the
//	           wiring has one; see [FocusContext.EscInterrupts]/[FocusContext.Hint]
//	keymode     80  the digit-precedence indicator (5.22 checklist: "1-3
//	           answer" vs "1-9 rooms" — "the ambiguity is resolved on
//	           screen, never in the user's head")
//	verbs       70  up to three [registry.Entry] rows for the current focus
//	health      60  pending-only system states (10.5.23)
//	toast       50  a transient receipt from another room (5.21)
//	scope       40  the breadcrumb tail — "the first thing that can go"
//
// The five priorities shared with [tokens.FooterColumnOrder] (attention,
// help, keymode, verbs, scope — toast and health too) are the SAME numbers,
// on purpose: that table is the canonical priority ledger for this exact
// row, and a renderer that reordered them locally would make the shared
// table a fiction. "hint" has no entry there yet — 5.20 rule 6's
// esc-interrupt slot and 10.5.26's input-state hint are both landing in this
// wave, after that table was seeded — and it sits just under "help" for the
// reason stated at [priorityOf]: safety-relevant, but the permanent door
// still wins the last column standing.
//
// # FocusContext: the wiring's contract
//
// [FocusContext] is a plain, comparable-by-eye struct the wiring fills once
// per frame from whatever pane holds focus. It is deliberately NOT a
// stateful thing this package remembers between renders — unlike a place
// line's ground, which changes on navigation, focus context changes on
// nearly every keystroke and every stream tick, so a render-time parameter
// is the honest shape, not a Set call the caller would have to remember to
// keep in sync.
//
// Every field that produces a visible hint is honest by construction: a
// field left at its zero value (Attention 0, Hint "", EscInterrupts false,
// Health nil, Toast "", ScopeTail "") omits its column outright rather than
// rendering an empty or misleading cell — the affordance never lies (5.20),
// and that includes lying by presence when there is nothing to say.
//
// # Contract with the shell
//
// [Model] is built with [New]; [Model.Render] is a pure function of a
// [FocusContext] and a width, returning at most one row, never panicking and
// never exceeding the width it was given, down to w=1.
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package footer
