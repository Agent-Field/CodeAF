package modelui

import "github.com/Agent-Field/aforge-v2/internal/store"

// The input contract. The wiring fills a [Catalog] whenever the facts move — a
// binding is written, the scope changes, a task settles, the model catalog
// finishes loading — and hands it over with [Picker.SetCatalog]. It is not
// consulted per keystroke and never per frame: everything a render needs is
// precomputed at that moment.
//
// WHAT THIS PACKAGE NEEDS FROM ELSEWHERE, and how the wiring gets it. Every one
// of these is a read that already exists; nothing below asks for new API:
//
//	Roles      one [store.ResolvedRole] per role, from
//	           (*store.Store).ResolveRole(role, nodeID) — nodeID empty at home.
//	           Source and Scope come back with it, which is the whole
//	           provenance column.
//	BoundHere  (*store.Store).RoleBindingAt(role, Catalog.Scope) — the second
//	           bool. It is a different question from "what governs this node"
//	           and the store already separates them.
//	Window     (*catalog.Catalog).ContextLength(slug), or
//	           (*command.Commander).ContextWindow(role) for the live slots.
//	Used       the room's journaled prompt high-water (store.RoomSpend), which
//	           is the numerator the engine owns.
//	Models     (*catalog.Catalog).ModelsWithOutput("text") or whatever narrower
//	           slice the role deserves, already filtered by the wiring.
//	Disabled   the wiring's own sentence — see the field docs. This package
//	           will not compute one, because only the caller knows whether a
//	           binding can take effect.
//
// If a read API ever goes missing, the fix is a field here that the wiring
// fills, not a call from this package into a store.

// Catalog is everything the picker draws from.
//
// The zero value is meaningful and renders honestly: the five roles in ladder
// order, every one unbound, no models beneath them, scoped globally. That is
// exactly what a machine that has bound nothing looks like, so an unconfigured
// product and an unwired surface show the same true thing.
type Catalog struct {
	// Scope is what this palette governs — the scope a chosen row binds at.
	// 5.23: "click any chip → one palette, scoped to what the chip governs".
	// The zero value is [store.ScopeGlobal], which is what a chip at home
	// governs and the only scope that needs no target.
	Scope store.BindingScope

	// ScopeWord names that scope in the product's language, for the header:
	// "everywhere", "this task", "this worker". Empty derives a word from
	// Scope.Kind(). It is a word and never an id — 5.14 forbids showing ids,
	// and a scope's target is an id.
	ScopeWord string

	// Roles are the current bindings, one per role. Order does not matter: the
	// picker always shows the five in ladder order ([store.ModelRoles]) and
	// fills each row from here, so a wiring that knows about three roles yields
	// five rows with two of them honestly unbound rather than a short list.
	Roles []RoleRow

	// Models is the catalog one level deeper — what a role can be bound to.
	// The order is the wiring's and is preserved; an empty slice renders an
	// empty-but-honest second level rather than a role that cannot be opened.
	Models []ModelOption

	// Disabled is the reason NOTHING here can take effect, in the wiring's own
	// words: "visitor window — model switching is unavailable", "no live work
	// to move". It disables every row and is shown on each of them (5.20 rule
	// 3: the user never has to guess). Empty means the surface is live.
	Disabled string
}

// RoleRow is one role's current answer, as the wiring resolved it.
//
// It is this package's own shape and not a [store.ResolvedRole] because the row
// needs two facts the resolution does not carry — the context reading behind
// the gauge, and whether a binding exists at THIS scope — and because a surface
// that accepted the store's type would be one refactor away from calling the
// store.
type RoleRow struct {
	// Role is which of the five. A role outside the ladder is dropped.
	Role store.ModelRole

	// Model is the resolved slug: pin, then node, then task, then global, then
	// the compiled-in default (see [store.Store.ResolveRole]). Empty is the
	// honest unbound answer and renders as [tokens.GlyphMissing].
	Model string

	// Source is which rung answered, and becomes the provenance column.
	// [store.RoleUnbound] and the zero value both read "unbound".
	Source store.RoleSource

	// BoundHere says a binding exists at [Catalog.Scope] for this role — the
	// second return of (*store.Store).RoleBindingAt. It is what makes the
	// "inherit" row appear one level deeper: a scope that binds nothing has
	// nothing to clear, and offering the clear anyway would be an affordance
	// that does nothing (5.22 rule 5).
	BoundHere bool

	// Boosted marks the work role's transient escalation, which draws ⇡ on the
	// chip and nowhere else. There is no boost row, no boost page and no second
	// toggle (8.2.16).
	Boosted bool

	// Used and Window are the context reading. See [Chip].
	Used, Window int64

	// Disabled is why THIS role cannot be rebound at this scope, in the
	// wiring's own words: "pinned at this node — a pin outranks a binding",
	// "no live work to move". The row still renders, spends its description
	// column on the reason, and refuses enter (5.20 rule 3).
	Disabled string
}

// ModelOption is one model the second level offers.
type ModelOption struct {
	// Slug is the provider id — what a chosen row carries back in
	// [SetRole.ModelSlug]. It is never rendered raw: the row shows
	// [ModelWord] of it.
	Slug string

	// Window is the model's context length ((*catalog.Catalog).ContextLength).
	// Zero renders [tokens.GlyphMissing] rather than "0": a window nobody knows
	// is not a window of nothing.
	Window int64

	// Note is the wiring's one line about this model — a tier word, a price, a
	// capability. It is fuzzy-matched alongside the model word and is the first
	// column width pressure takes.
	Note string

	// Disabled is why this model cannot be chosen here ("no API key for this
	// provider", "no image input — this role needs it"). The row renders, shows
	// the reason instead of the note, and refuses enter.
	Disabled string
}

// scopeWord is the header's word for a scope. It never renders the target id
// (5.14), and an unrecognized scope reads "this scope" rather than being shown
// raw — a malformed scope on screen would be an id by another name.
func (c Catalog) scopeWord() string {
	if c.ScopeWord != "" {
		return c.ScopeWord
	}
	switch c.scope().Kind() {
	case "global":
		return "everywhere"
	case "task":
		return "this task"
	case "node":
		return "this worker"
	}
	return "this scope"
}

// scope is [Catalog.Scope] with the zero value resolved. A scope that names no
// target is not a scope the store would accept, so an invalid one degrades to
// global rather than to a binding nobody can find and nobody can clear.
func (c Catalog) scope() store.BindingScope {
	if c.Scope.Valid() {
		return c.Scope
	}
	return store.ScopeGlobal
}

// roleRow finds the wiring's row for one role, or the honest unbound one.
func (c Catalog) roleRow(role store.ModelRole) RoleRow {
	for i := range c.Roles {
		if c.Roles[i].Role == role {
			return c.Roles[i]
		}
	}
	return RoleRow{Role: role, Source: store.RoleUnbound}
}

// provenanceWord is the right column of a role row: which rung answered, in one
// word. The words are the ladder's own (5.23) and the scope's target never
// appears — "task" says everything the reader can act on, and the id says
// nothing they could.
func provenanceWord(source store.RoleSource) string {
	switch source {
	case store.RoleFromPin:
		return "pinned"
	case store.RoleFromNode:
		return "worker"
	case store.RoleFromTask:
		return "task"
	case store.RoleFromGlobal:
		return "global"
	case store.RoleFromDefault:
		return "default"
	}
	return "unbound"
}

// roleHint is the one-line description beside a role's word — what the role
// DOES, straight from 5.23's table. It is here rather than on [store.ModelRole]
// because it is a sentence for a reader and the store's Word() is a name for a
// chip; the two live at different altitudes and a store that carried UI prose
// would be a store with a voice.
func roleHint(role store.ModelRole) string {
	switch role {
	case store.RoleOrchestrate:
		return "head and task-orchestrator turns"
	case store.RolePlan:
		return "compile, replan, revise the graph"
	case store.RoleWork:
		return "worker and executor turns"
	case store.RoleVerify:
		return "gates, judges, escalation probes"
	case store.RoleScribe:
		return "labels, titles, folds, briefs"
	}
	return ""
}
