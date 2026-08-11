// Package modelui is the model surface as a component: the chip that says what
// a thing runs on, and the picker that changes it.
//
// # One home, one surface (5.10 / 8.2.16 / 5.23)
//
// Role BINDINGS are storage — internal/store's role_bindings table, five roles
// and a scope ladder. The CHIP is the only surface. There is no boost page, no
// second toggle, no per-message model syntax in the composer. Everything the
// product says about model economics is said by a chip reading
//
//	⟨role word⟩ ⟨model word⟩ ⟨ctx gauge⟩
//
// and by the one palette that opens when a chip is clicked, scoped to exactly
// what that chip governs.
//
// This package renders both and decides nothing. It journals nothing, reads no
// store, opens no file and holds no client. Facts arrive through [Catalog],
// which the wiring fills whenever they move; a chosen row leaves as a [Result],
// which the wiring routes to the CommandSetModel door and the roles table's
// setter. That split is deliberate and structural: a surface that could write a
// binding would be a second place bindings live, which is the one thing 8.2.16
// forbids in as many words.
//
// # What the consumers do
//
// Three surfaces embed the chip and none of them owns it:
//
//	the home status line   the orchestrate binding
//	a room header          that room's work binding
//	a rail worker row      that worker's resolved model
//
// They call [Chip.Render] for a painted string, or [Chip.Spans] to fold the
// chip's cells into a line they are already assembling. The chip never draws
// its own separators, its own brackets or its own background: it is a run of
// cells that fits in the width it is given, and the line around it belongs to
// whoever drew the line.
//
// # What the picker refuses to do
//
// It refuses to execute. It refuses to invent a reason a row is unavailable —
// reasons are the wiring's words, because only the wiring knows whether a
// binding can take effect (5.20 rule 3). And it refuses to edit reasoning
// effort, because effort has no journal axis yet (12.3.5): it is encoded inside
// the model slug, so the chip DISPLAYS it and the picker says why it cannot be
// changed rather than offering a control that would silently do nothing.
//
// # The semantics a chosen row inherits
//
// A binding takes effect at the NEXT PROVIDER CALL (5.10, 12.3.4). The running
// turn is never killed by a model switch; wanting it immediate is cancel-turn
// plus switch, two explicit acts. Journaling is session-scoped by the same rule
// /model already follows (12.6.5). None of that is enforced here — it is what
// the wiring on the other side of [Result] must honour, and it is written down
// in result.go so the contract is readable from the door.
package modelui
