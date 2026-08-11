package chat

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
)

// The model surface's wiring: the five duties internal/tui2/modelui's result.go
// spells out, performed here because that package refuses to perform any of
// them.
//
// The split it insists on is the whole design. modelui renders and decides
// nothing: it holds no store, journals nothing, and hands back a closed sum of
// two intents. This file is where an intent becomes a mutation, and it is the
// only place in this surface that writes a role binding. A second place would
// be a second home for model economics, which 8.2.16 forbids in as many words.
//
// The five duties, and where each one is discharged:
//
//	1. write at the storage        applyModelResult → RoleBindings
//	2. move the live work through
//	   the command door            applyModelResult → ModelControl.SetModel,
//	                              which is the RequestCommand/CommandSetModel
//	                              funnel (internal/command's journalWorkModel)
//	3. session scope               the funnel's own rule, unchanged (12.6.5)
//	4. next provider call          nothing here cancels a turn
//	5. post the receipt            applyModelWrite, AFTER the write returned —
//	                              never beside the keystroke
//
// Duty 5 is the one this surface could most easily get wrong, so it is stated
// twice: the receipt is evidence of the journal and not evidence of a click. A
// write that failed posts nothing and says so on the status line instead.

// modelChipOrigin is what the roles table records as the author of a binding
// written from this surface. A chip is a PERSON, so it is not a "seed:" origin
// and never goes through SeedRoleBinding — the never-clobber rule exists to
// stop an initializer overwriting a human choice, and a surface that wrote
// seeds would be an initializer with a keyboard.
const modelChipOrigin = "user:chip"

// RoleBindings is the optional slice of the durable store the model surface
// reads and writes.
//
// It is deliberately not part of [Backend], for the reason [Graph] is not:
// Backend is what a chat surface cannot exist without, and a window whose store
// cannot answer these still draws a conversation. A backend without them gets a
// palette that says so on every row rather than one that silently does nothing.
//
// *store.Store satisfies it, structurally, with no adapter at the entry point.
type RoleBindings interface {
	// ResolveRole answers "what governs this node", pin and ladder applied. The
	// node is empty at home.
	ResolveRole(role store.ModelRole, nodeID string) (store.ResolvedRole, error)
	// RoleBindingAt is the different question: does a binding exist at THIS
	// scope. It is what decides whether the palette offers the inherit row.
	RoleBindingAt(role store.ModelRole, scope store.BindingScope) (store.RoleBinding, bool, error)
	// SetRoleBinding and ClearRoleBinding are the storage half of duty 1. Clear
	// is separate from a set with an empty value because an unbound scope
	// INHERITS and a scope bound to "" does not.
	SetRoleBinding(role store.ModelRole, scope store.BindingScope, value, origin string) (bool, error)
	ClearRoleBinding(role store.ModelRole, scope store.BindingScope, origin string) (bool, error)
}

// ModelControl is the optional slice of the live engine that re-points what a
// role runs on.
//
// SetModel is the command funnel and not a client setter: for the work slot it
// asks CommandSetModel of every live job of this session, through
// RequestCommand, exactly as /model does (12.6.5). This surface never repoints
// a client itself — that was the lie with a receipt result.go names.
//
// Models is the catalog one level deeper, as slugs. It is the widest read this
// seam can make without importing internal/tui, whose ModelChoice carries the
// price and the display name: the v1 window is the surface being replaced, and
// a v2 package that imported it would have to be rewritten the day it goes
// (engine.go states the same rule for the same reason). The cost is the model
// note and the window, which render as the missing mark rather than as a guess.
type ModelControl interface {
	SetModel(slot, slug string) error
	Models() []string
}

// The store is the roles table, and the build says so here rather than at a
// call site: a signature that moves in internal/store fails in the package that
// declared the seam instead of failing as a feature that quietly stopped being
// offered (12.6.2's rule, one interface later).
var _ RoleBindings = (*store.Store)(nil)

// errNoJournal is what a window with no roles table can honestly report. It is
// a sentence rather than a code because it reaches a status line.
var errNoJournal = errors.New("no journal behind this window — nothing to bind")

// -- the legacy slot map ------------------------------------------------------

// modelSlotFor maps one of 5.23's five roles onto the engine slot that still
// holds its client.
//
// Three of the five have one. The other two are real roles with real bindings
// and no consumer yet: nothing in the tree resolves verify or scribe, so a
// binding written for them would sit in the table unread. That is a fact about
// today and not about the design, and the palette says it on the row rather
// than hiding the row — 5.20 rule 3 is that a disabled affordance says why, and
// 5.22 rule 5 is that an affordance which does nothing must not be offered as
// though it does.
func modelSlotFor(role store.ModelRole) (string, bool) {
	switch role {
	case store.RoleOrchestrate:
		return "talk", true
	case store.RolePlan:
		return "plan", true
	case store.RoleWork:
		return "work", true
	}
	return "", false
}

// roleForSlot is the reverse, for [settings.ModelMsg]. Boost maps onto the WORK
// role because boost is a transient escalation of the work binding and not a
// concept of its own (8.2.16) — a boost row that opened its own palette would
// be the second toggle that section refuses.
//
// The media slots map onto nothing: vision and the rest are capability flags
// resolved WITHIN a role, never a sixth role (5.23), so the palette opens with
// its whole surface disabled and the reason on every row rather than pretending
// one of the five governs an image model.
func roleForSlot(slot string) (store.ModelRole, bool) {
	switch strings.TrimSpace(slot) {
	case "talk":
		return store.RoleOrchestrate, true
	case "plan":
		return store.RolePlan, true
	case "work", "boost":
		return store.RoleWork, true
	}
	return "", false
}

// -- the catalog --------------------------------------------------------------

// modelScope is what a palette opened right now governs (5.23: "one palette,
// scoped to what the chip governs").
//
// At home that is the machine. Inside a task room it is that task's subtree,
// which is the scope the chip on that room's own header will carry when the
// header grows one — and it is what gives the pinned-node reason a real home:
// a node whose model was promised at splice cannot be rebound by an inheriting
// scope, and the palette has to say so rather than accept the keystroke.
func (a *App) modelScope() (store.BindingScope, string, string) {
	if a.view != nil && a.view.kind == viewNode && a.view.node != "" {
		return store.TaskScope(a.view.node), "this task", a.view.node
	}
	return store.ScopeGlobal, "everywhere", ""
}

// modelCatalog fills modelui's input contract from reads that already exist.
// It is built when a door opens or a fact moves, never per frame and never per
// keystroke — the component's own doc is explicit that this is not a per-render
// read, and every call below would otherwise be a store hit behind an arrow key.
func (a *App) modelCatalog() modelui.Catalog {
	scope, word, node := a.modelScope()
	catalog := modelui.Catalog{
		Scope:     scope,
		ScopeWord: word,
		Disabled:  a.modelDisabled(),
		Roles:     make([]modelui.RoleRow, 0, len(store.ModelRoles())),
		Models:    a.modelOptions(),
	}
	bindings, _ := a.backend.(RoleBindings)
	for _, role := range store.ModelRoles() {
		catalog.Roles = append(catalog.Roles, a.modelRoleRow(bindings, role, scope, node))
	}
	return catalog
}

// modelRoleRow is one role's current answer.
//
// The fallback to the engine's own slot is not a guess and not a default: an
// UNBOUND role means the caller's own resolution stands unchanged, so what the
// next call will actually use is what the client currently holds. Showing it
// with an "unbound" provenance is therefore two true facts side by side — this
// is what runs, and nothing in the table says so — where showing the missing
// mark would have claimed the surface does not know what it is about to use.
func (a *App) modelRoleRow(bindings RoleBindings, role store.ModelRole,
	scope store.BindingScope, node string) modelui.RoleRow {

	row := modelui.RoleRow{Role: role, Source: store.RoleUnbound}
	if bindings != nil {
		if resolved, err := bindings.ResolveRole(role, node); err == nil && resolved.Bound() {
			row.Model, row.Source = resolved.Model, resolved.Source
		}
		if _, found, err := bindings.RoleBindingAt(role, scope); err == nil {
			row.BoundHere = found
		}
	}
	if slot, ok := modelSlotFor(role); ok && a.commander != nil {
		if row.Model == "" {
			row.Model = strings.TrimSpace(a.commander.CurrentModel(slot))
		}
		if window, known := a.commander.ContextWindow(slot); known {
			row.Window = int64(window)
		}
		// The numerator is the room's journaled prompt high-water, which the
		// poll already read for the meta strip — and it is the voice's number,
		// because the voice is the role that spends this room's window. The
		// other four run somewhere the room's gauge does not measure, so they
		// carry a denominator and no numerator, and the chip draws the honest
		// half-known gauge rather than a fraction of the wrong thing.
		if role == store.RoleOrchestrate && a.meta != nil && a.meta.haveUsage {
			row.Used = a.meta.used
		}
	}
	row.Disabled = a.roleDisabled(role, row)
	return row
}

// modelDisabled is why NOTHING in the palette can take effect, in this
// surface's own words (5.20 rule 3).
func (a *App) modelDisabled() string {
	if a.residency.Visitor {
		// A visitor's commander refuses a switch by name ("model switching is
		// unavailable in visitor mode"), and the head it would have to reach is
		// in another process. Letting the keystroke through to find that out is
		// the affordance lying one step later than it has to.
		return "visitor window — the head runs in another process"
	}
	if _, ok := a.backend.(RoleBindings); !ok {
		return errNoJournal.Error()
	}
	return ""
}

// roleDisabled is why THIS role cannot be rebound at this scope.
func (a *App) roleDisabled(role store.ModelRole, row modelui.RoleRow) string {
	if row.Source == store.RoleFromPin {
		return "pinned at this node — a pin outranks a binding"
	}
	if _, ok := modelSlotFor(role); !ok {
		return "no caller reads this role yet — the binding would sit unread"
	}
	if a.commander == nil {
		return "no engine behind this window"
	}
	if _, ok := a.commander.(ModelControl); !ok {
		return "this window cannot re-point live work"
	}
	return ""
}

// modelOptions is the catalog one level deeper. The order is the engine's and
// is preserved; a window with no engine gets an empty-but-honest second level
// rather than a role that cannot be opened.
//
// Window is left at zero on purpose. The per-slug context length lives on the
// model catalog, which this seam cannot reach without importing the v1 window's
// types, and modelui renders a zero window as the missing mark rather than as
// "0" — which is the whole of 8.2.20 applied to a number nobody here knows.
func (a *App) modelOptions() []modelui.ModelOption {
	control, ok := a.commander.(ModelControl)
	if !ok {
		return nil
	}
	slugs := control.Models()
	out := make([]modelui.ModelOption, 0, len(slugs))
	for _, slug := range slugs {
		if slug = strings.TrimSpace(slug); slug == "" {
			continue
		}
		out = append(out, modelui.ModelOption{Slug: slug})
	}
	return out
}

// -- the door ------------------------------------------------------------------

// openModelPicker raises the palette (5.10: "the model chip … is interactive →
// model palette").
//
// It always opens at the role level, which is where [modelui.Picker.Reset]
// puts it. REQUESTED SEAM: the picker has no door to open AT one role, so a
// settings model row — which knows exactly which slot the reader pressed enter
// on — lands them one keystroke above it. Nothing lies; the reader is one enter
// away and the five rows name themselves.
func (a *App) openModelPicker() tea.Cmd {
	if a.models == nil {
		a.models = modelui.New(modelui.Options{
			Styler:     a.style,
			Invalidate: a.shell.Invalidate,
			OnChoose:   a.chooseModel,
			OnClose:    a.closeOverlay,
		})
	}
	a.models.Reset()
	a.models.SetCatalog(a.modelCatalog())
	return a.raise(overlayModel, a.models)
}

// openModelSlot is [settings.ModelMsg]'s landing: the sheet asked for the
// models door for one slot.
//
// The sheet is closed first rather than stacked under, because the plane holds
// exactly one overlay and because closing is what flushes a debounced write the
// reader has already made. A slot outside the five roles opens the same palette
// with its whole surface disabled and the reason on every row (see roleForSlot).
func (a *App) openModelSlot(msg settings.ModelMsg) tea.Cmd {
	closed := a.closeOverlay()
	open := a.openModelPicker()
	if _, ok := roleForSlot(msg.Slot); !ok && a.models != nil {
		catalog := a.modelCatalog()
		if catalog.Disabled == "" {
			catalog.Disabled = "the palette binds the five roles — " +
				strings.TrimSpace(msg.Slot) + " is a capability slot, not one of them"
		}
		a.models.SetCatalog(catalog)
	}
	return tea.Batch(closed, open)
}

// -- what a chosen row means ---------------------------------------------------

// modelResultMsg is one attempted mutation, folded in on the render goroutine.
//
// It carries what was ASKED and what was DONE as separate facts, because the
// receipt is written from the second: a write that changed nothing (the binding
// was already this) is not a mutation and gets no receipt, and a write that
// failed gets a status line instead of one.
type modelResultMsg struct {
	role    store.ModelRole
	slug    string
	cleared bool
	changed bool
	// moved says the command funnel was asked to re-point work already running.
	// Only the work slot journals — the other two are the next call's model and
	// have nothing in flight to move (12.6.5) — so this is what separates 5.10's
	// "switching remaining work" sentence from the weaker true one.
	moved bool
	err   error
}

// chooseModel is [modelui.Options.OnChoose]: the closed sum of two intents,
// turned into the two writes and nothing else. It performs neither itself —
// both go off the render goroutine, because both touch the store.
func (a *App) chooseModel(result modelui.Result) tea.Cmd {
	bindings, _ := a.backend.(RoleBindings)
	control, _ := a.commander.(ModelControl)
	switch chosen := result.(type) {
	case modelui.SetRole:
		return func() tea.Msg {
			return setRoleResult(bindings, control, chosen)
		}
	case modelui.ClearRole:
		return func() tea.Msg {
			return clearRoleResult(bindings, chosen)
		}
	}
	return nil
}

// setRoleResult performs duties 1 and 2, in that order and never the other way.
//
// The storage is written first because it is the durable answer: a funnel that
// journaled a re-point against a binding that then failed to save would leave
// the running work on one model and the table saying another. If the funnel
// refuses afterwards — a visitor engine, a slot with no client — the binding
// still stands and the error is reported, which is the honest half-success:
// what was bound is bound, and what could not move did not.
func setRoleResult(bindings RoleBindings, control ModelControl,
	chosen modelui.SetRole) modelResultMsg {

	msg := modelResultMsg{role: chosen.Role, slug: chosen.ModelSlug}
	if bindings == nil {
		msg.err = errNoJournal
		return msg
	}
	changed, err := bindings.SetRoleBinding(chosen.Role, chosen.Scope,
		chosen.ModelSlug, modelChipOrigin)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.changed = changed
	slot, mapped := modelSlotFor(chosen.Role)
	if !mapped || control == nil {
		return msg
	}
	if err := control.SetModel(slot, chosen.ModelSlug); err != nil {
		msg.err = err
		return msg
	}
	// Only the work slot walks the RequestCommand door: it is the slot the graph
	// carries, so a change to it asks CommandSetModel of every live job this
	// room owns. The other two are read at the next call and have nothing in
	// flight to journal against.
	msg.moved = slot == "work"
	return msg
}

// clearRoleResult unbinds one role at one scope so the wider scope answers
// again.
func clearRoleResult(bindings RoleBindings, chosen modelui.ClearRole) modelResultMsg {
	msg := modelResultMsg{role: chosen.Role, cleared: true}
	if bindings == nil {
		msg.err = errNoJournal
		return msg
	}
	changed, err := bindings.ClearRoleBinding(chosen.Role, chosen.Scope, modelChipOrigin)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.changed = changed
	return msg
}

// applyModelResult folds one attempted mutation in and posts its receipt.
func (a *App) applyModelResult(msg modelResultMsg) tea.Cmd {
	if msg.err != nil {
		a.status.err = msg.err.Error()
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	if a.models != nil {
		// The facts moved, so the palette is told — it is not consulted per
		// frame and would otherwise keep drawing the binding that was replaced.
		a.models.SetCatalog(a.modelCatalog())
	}
	a.refresh()
	if !msg.changed {
		// Nothing moved: the role already ran on this. A receipt here would be a
		// mutation claimed and not performed, which is the one thing 5.20 rule 5
		// exists to prevent from the other direction.
		return nil
	}
	return a.postReceipt(a.modelReceipt(msg))
}

// modelReceipt is 5.10's sentence, and the two weaker true ones for the cases
// it does not cover.
//
// The strong sentence is reserved for the case that earned it: the work slot,
// whose change asked the funnel to move work that is already running. A role
// read at the next provider call has moved nothing yet, and saying "switching
// remaining work" about it would be describing a mutation that has not happened.
func (a *App) modelReceipt(msg modelResultMsg) string {
	word := modelui.ModelWord(msg.slug)
	switch {
	case msg.cleared:
		return msg.role.Word() + " unbound here — the wider scope answers again"
	case msg.moved:
		return "switching remaining work to " + word
	default:
		return msg.role.Word() + " switches to " + word + " at the next call"
	}
}

// postReceipt journals one system row into the room the reader is looking at.
//
// It goes through internal/thread's single door like every other write this
// surface makes, and it is a SYSTEM row: the poll never answers one, the
// question reader only looks at agent rows, and every renderer here already
// draws a system row as the dim collapsed receipt a receipt belongs in.
func (a *App) postReceipt(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" || a.backend == nil {
		return nil
	}
	backend, session := a.backend, a.session
	return func() tea.Msg {
		posted, err := thread.Post(backend, store.Message{
			SessionID: session,
			Role:      store.RoleSystem,
			Body:      text,
		})
		return receiptPostedMsg{message: posted, err: err}
	}
}

// receiptPostedMsg is one journaled receipt. Nothing is drawn from it directly:
// the row is in the store, and the poll that is already running draws it there,
// which is what keeps the screen a rendering of the journal rather than a
// second copy of it.
type receiptPostedMsg struct {
	message store.Message
	err     error
}
