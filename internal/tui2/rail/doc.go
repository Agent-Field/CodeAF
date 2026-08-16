// Package rail is the scoped master-detail map of chat-rebuild 5.15: one scope
// model, three renderings, one cursor.
//
// # The rail is a map, not a menu (5.15)
//
// The rail always shows exactly ONE scope. A scope is a conversational surface
// plus its members: at home, row 0 is `aforge` and the members are the
// top-level task cards; inside a task, row 0 is that task's orchestrator and
// the members are its plan steps and workers, indented, with the waits-on
// structure visible. One cursor moves over those rows. Selection previews,
// enter commits or descends, esc pops the scope.
//
// # Width independence (Part 9.12)
//
// Scope is NOT a rail feature — the rail is one rendering of it. The same rows,
// the same keys and the same selection semantics render three ways:
//
//   - [ModeRail]   the right rail at or above [tokens.RailAtWidth] (90).
//   - [ModeList]   the same scope as a full-pane list below that breakpoint,
//     which at an ordinary 80-column terminal is the PRIMARY
//     experience and not a fallback.
//   - [ModeHUD]    the bounded sticky summary of 8.2.8: at most
//     [tokens.HUDRowCap] live rows plus a fold line, above the
//     composer. It carries the live summary, never the scope map.
//
// # What this package is and is not
//
// It is a component. It owns a scope model and its rendering; it owns no
// keymap, no store handle and no terminal. Data arrives through [ScopeSource],
// an interface declared here and shaped to mirror what the store already
// answers (see [Row] for the field-for-field mapping), so binding it to the
// real board and plan-sight reads is mechanical. Nothing here imports
// internal/tui, internal/store, or the tui2 shell.
//
// Style comes from internal/tui2/tokens and from nowhere else: every glyph,
// colour, breakpoint and formatted live cell is named there. Layout primitives
// (ANSI-aware width, truncation, the fold-line grammar, the 8.1.7 collapse
// policies) come from internal/tui2/blocks. Both are consumed read-only.
//
// # The laws this package implements
//
//   - 5.9  card anatomy: attention glyph + name + composer mark, first-person
//     status, dim telemetry with money always visible.
//   - 5.11 the composer-mode mark (› chat, ↦ steer) previews the composer the
//     row will bind, and a settled row shows no mark because its composer
//     is disabled — the affordance never lies.
//   - 5.14 never render node IDs, seqs, journal internals or raw JSON. [Row.ID]
//     exists to be selected by, never to be drawn.
//   - 5.15 one scope, one cursor, selection previews and enter commits.
//   - 5.16 identity hue lives ONLY in the glyph and the selection band tint;
//     selection is a background band, not a foreground colour.
//   - 5.17 the glyph vocabulary, and the one-cell context gauge.
//   - 5.21 the left accent rail, elapsed that ages, count chips,
//     middle-ellipsised paths, width-stable live cells.
//   - §3   inside a job the members are a PLAN, drawn as v1's connector tree —
//     ├─ and ╰─ with │ guides — one row each, state glyph, name, the
//     receipt flush right, and `waits: <deps>` when the row is blocked.
//   - §13  every tree row carries its own money and clock, and a row that
//     stands for a subtree carries the rollup. Absent renders as NOTHING;
//     $0.00 is a measurement and a different sentence.
//   - §14  no machinery vocabulary. No fractions (a plan that can be replanned
//     cannot promise a denominator), no worker counts, no `atomic`: a
//     single-part job says nothing at all about its shape.
//   - 7.2  rows never re-sort while visible; a badge pulls the eye instead. Nor
//     do they MOVE because the cursor rested somewhere: a preview is capped
//     and its lines are reserved, so selecting a row changes no other row's
//     place on screen (see the detail reserve in render.go).
//   - 8.1.6 shape changes only at a true state transition and never animates on
//     a durable row; accent = live, plain = settled, dim = chrome.
//   - 8.1.7 overflow: live lists keep the running rows, finalized lists give the
//     slots to the failures, and the fold line carries the breakdown.
//   - 12.5.1 the artifact law: a deliverable row carries a path ([Ref]), never
//     prose. 12.5.2 the truncation law: a cut row renders visibly cut.
package rail
