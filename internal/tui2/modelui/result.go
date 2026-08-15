package modelui

import "github.com/Agent-Field/aforge-v2/internal/store"

// The result vocabulary — the whole contract between a chosen row and the
// wiring that interprets it.
//
// It is a closed sum: the unexported marker method means no package outside
// this one can add a third shape, so a wiring type-switch over the two below is
// exhaustive today and stays exhaustive. Both members carry a role, a scope and
// nothing that could be mistaken for an instruction: this surface knows what
// the user pointed at, and it does not know what pointing at it means.
//
// # WHAT THE WIRING MUST DO WITH ONE (the part this package cannot enforce)
//
//  1. WRITE IT AT THE STORAGE, WHICH IS THE ROLES TABLE.
//     [SetRole] is (*store.Store).SetRoleBinding(Role, Scope, ModelSlug,
//     origin); [ClearRole] is (*store.Store).ClearRoleBinding(Role, Scope,
//     origin). Origin is the wiring's — a chip is a person, so it is not a
//     "seed:" origin and must never go through SeedRoleBinding, which exists
//     for initializers that may not clobber a human choice.
//
//  2. MOVE THE LIVE WORK THROUGH THE COMMAND DOOR, NOT BY REPOINTING A CLIENT.
//     A binding written while work is running is a statement about that work.
//     The door is store.CommandSetModel — one command per live job, targeted at
//     the node, exactly as (*command.Commander).SetModel already journals it.
//     Repointing the client alone was a lie with a receipt (12.6.5): the next
//     head turn ran on the new model while every live leaf went on running on
//     the old one, and the person was shown a confirmation either way.
//
//  3. SESSION SCOPE IS THE RULE (12.6.5). One SetModel per live top-level job
//     OF THIS SESSION; the funnel refuses a root target, and a headless errand
//     running beside a chat window does not move. Under rooms this is
//     room-scoped by the same rule.
//
//  4. NEXT PROVIDER CALL, NEVER MID-TURN (5.10, 12.3.4). Rewriting nodes.work_
//     model IS the semantics: dispatch re-reads the row and nothing running is
//     touched. A user who wants it immediate cancels the turn and switches —
//     two explicit acts, because a model switch that killed a running turn
//     would make every chip press a gamble.
//
//  5. POST THE RECEIPT (5.10, 5.20 rule 5). "switching remaining work to
//     ⟨model word⟩" lands in the task thread, in model words and never a
//     provider id. THE RECEIPT IS THE WIRING'S DUTY AND NOT THIS PACKAGE'S:
//     a surface that wrote its own receipt would be claiming a mutation it did
//     not perform, and 5.20's predictability law wants the receipt to be
//     evidence of the journal rather than evidence of a click. [ModelWord] is
//     exported so the receipt and the chip say the same word.
//
// # WHAT IS DELIBERATELY NOT IN THE VOCABULARY
//
// No effort member. Effort has no journal axis (12.3.5) — it is encoded inside
// the slug — so a result that carried effort separately would be asking the
// wiring to write a field that does not exist. When Provenance grows the axis
// and the doc is amended, a third member joins this sum and this comment goes.
//
// No boost member. Boost is a transient escalation of the WORK binding and not
// a concept of its own (8.2.16); it arrives as a [SetRole] on
// [store.RoleWork] like anything else, and the chip renders ⇡ because the
// wiring told it the escalation is on.

// Result is what one chosen row yields. Implemented by exactly [SetRole] and
// [ClearRole].
type Result interface {
	// Role is the role the result is about — the chip's subject, and the one
	// identifier both shapes carry.
	Target() string
	isResult()
}

// SetRole asks the wiring to bind one role to one model inside one scope.
type SetRole struct {
	// Role is one of the five (5.23). It is always valid: the picker builds
	// its rows from [store.ModelRoles] and cannot emit anything else.
	Role store.ModelRole
	// ModelSlug is the provider id, verbatim from [ModelOption.Slug] —
	// never the model word. The word is for reading; the slug is what a
	// provider call needs, and shortening it here would make the binding
	// unresolvable.
	ModelSlug string
	// Scope is where the binding lands: global, task:<root>, node:<id>. It is
	// [Catalog.Scope] — what the chip that opened this palette governs.
	Scope store.BindingScope
}

// ClearRole asks the wiring to unbind one role at one scope, so the wider
// scope answers again.
//
// It is a separate shape from a [SetRole] with an empty slug for the reason the
// store keeps Cleared separate from an empty value: an unbound scope INHERITS,
// and a scope bound to "" does not. Collapsing the two would be a binding
// nobody can find and nobody can clear.
type ClearRole struct {
	Role  store.ModelRole
	Scope store.BindingScope
}

// Target implements [Result].
func (r SetRole) Target() string { return string(r.Role) }

// Target implements [Result].
func (r ClearRole) Target() string { return string(r.Role) }

func (SetRole) isResult()   {}
func (ClearRole) isResult() {}

// String names the result for a log line or a test failure. It spells the slug
// rather than the word: a log is provenance, and provenance is where the
// provider id belongs.
func (r SetRole) String() string {
	return "set-role " + string(r.Role) + " → " + r.ModelSlug + " @ " + string(r.Scope)
}

// String names the result for a log line or a test failure.
func (r ClearRole) String() string {
	return "clear-role " + string(r.Role) + " @ " + string(r.Scope)
}
