package rail

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// RowKind is what a row IS, which decides its anatomy. It is not what a row is
// doing — that is [Lifecycle] — and the two are kept apart because a card's
// shape must not change when its state does (8.1.6).
type RowKind uint8

const (
	// RowSurface is the scope's conversational surface and is always row 0:
	// `aforge` at home, the task orchestrator inside a task (5.15). It is the
	// one row a fold may never hide, because a scope with no surface is a scope
	// the user cannot talk to.
	RowSurface RowKind = iota
	// RowTask is a top-level task card at home. It carries the full three-line
	// anatomy of 5.9.
	RowTask
	// RowStep is a plan step inside a task scope.
	RowStep
	// RowWorker is a worker inside a task scope, or a per-worker row under a
	// focused card.
	RowWorker
)

// String names the kind.
func (k RowKind) String() string {
	switch k {
	case RowSurface:
		return "surface"
	case RowTask:
		return "task"
	case RowStep:
		return "step"
	case RowWorker:
		return "worker"
	}
	return "invalid"
}

// Lifecycle is the durable state of the work behind a row. It mirrors
// store.Status one for one so the integration lane is a switch and not a
// judgement:
//
//	pending, claimed  → LifeQueued    (claimed is queued with an owner, which
//	                                   is not a fact the rail shows)
//	running           → LifeWorking
//	done              → LifeSettled
//	failed            → LifeFailed
//	cancelled         → LifeCancelled
//
// store.Node.Held, which is scheduling control rather than status, maps to
// [LifePaused] — 5.17 bans ⏸ for width instability and spends `=` on it.
type Lifecycle uint8

const (
	// LifeQueued is admitted and not started.
	LifeQueued Lifecycle = iota
	// LifeWorking is running now.
	LifeWorking
	// LifeSettled finished cleanly.
	LifeSettled
	// LifeFailed finished badly.
	LifeFailed
	// LifeCancelled was stopped by a human or a governor.
	LifeCancelled
	// LifePaused is held.
	LifePaused
)

// String names the lifecycle.
func (l Lifecycle) String() string {
	switch l {
	case LifeQueued:
		return "queued"
	case LifeWorking:
		return "working"
	case LifeSettled:
		return "settled"
	case LifeFailed:
		return "failed"
	case LifeCancelled:
		return "cancelled"
	case LifePaused:
		return "paused"
	}
	return "invalid"
}

// Terminal reports whether the work behind the row has stopped for good. A
// terminal row's composer is disabled (5.15) and its live cells stop ageing.
func (l Lifecycle) Terminal() bool {
	return l == LifeSettled || l == LifeFailed || l == LifeCancelled
}

// Attention is what the row's glyph says, and it answers 5.9's first question:
// does this need me? It is DERIVED from the row (see [Row.Attention]) rather
// than stored, because two facts must never be able to disagree about one
// glyph. Selection, by contrast, is state — it is never derived in render.
type Attention uint8

const (
	// AttnQueued is ○.
	AttnQueued Attention = iota
	// AttnWorking is ◐.
	AttnWorking
	// AttnSettled is ✓.
	AttnSettled
	// AttnFailed is ✕.
	AttnFailed
	// AttnCancelled is ✕ in the broken hue: a cancel and a failure are the same
	// shape because they are the same fact to a reader scanning a rail — the
	// work is not going to happen.
	AttnCancelled
	// AttnPaused is =.
	AttnPaused
	// AttnQuestion is ?, and it OUTRANKS ALL (5.9): a blocked human is the most
	// expensive state this product has.
	AttnQuestion
	// AttnWaitsOn is ⚑, waiting on a sibling.
	AttnWaitsOn
)

// Glyph is the 5.17 vocabulary. Shape encodes the state CATEGORY and may change
// at a true state transition; it never animates on a durable row (8.1.6), which
// is why no spinner frame appears anywhere in this package.
func (a Attention) Glyph() string {
	switch a {
	case AttnWorking:
		return tokens.GlyphWorking
	case AttnSettled:
		return tokens.GlyphSettled
	case AttnFailed, AttnCancelled:
		return tokens.GlyphFailed
	case AttnPaused:
		return tokens.GlyphPaused
	case AttnQuestion:
		return tokens.GlyphNeedsHuman
	case AttnWaitsOn:
		return tokens.GlyphWaitsOn
	}
	return tokens.GlyphQueued
}

// Hue is the semantic colour of the glyph (5.16). Amber for anything waiting —
// a human question, and 5.21's amber ⚑ for a waits-on edge; coral for broken;
// green for settled; cyan for alive. [HueNone] means the row has no semantic
// word, and a top-level task card spends that case on its identity pastel (see
// [Row.GlyphHue]) so a rail of running work can be told apart at a glance.
func (a Attention) Hue() tokens.Hue {
	switch a {
	case AttnQuestion, AttnWaitsOn:
		return tokens.HueAttention
	case AttnFailed, AttnCancelled:
		return tokens.HueBroken
	case AttnSettled:
		return tokens.HueMoney
	case AttnWorking:
		return tokens.HueAlive
	}
	return tokens.HueNone
}

// Live reports whether the row is moving now. It drives the state axis (8.1.6:
// accent = live, plain = settled) and the choice of overflow policy (8.1.7).
//
// A queued row is deliberately NOT live: it is not moving, and painting it as
// though it were is the lie about liveness that 8.1.6 exists to forbid. A row
// waiting on a human or a sibling IS live, because the task around it is.
func (a Attention) Live() bool {
	switch a {
	case AttnWorking, AttnQuestion, AttnWaitsOn:
		return true
	}
	return false
}

// ComposerMode is the composer a row will bind when it is selected — "you talk
// to what you're looking at" (5.15). The mark it renders at the right edge of
// line 1 (5.11) IS a preview of the prompt glyph the composer will show, so the
// two can never disagree.
type ComposerMode uint8

const (
	// ComposerNone is a row with no surface of its own to speak into.
	ComposerNone ComposerMode = iota
	// ComposerChat is a room with a chat: › .
	ComposerChat
	// ComposerSteer is a steer-only room (5.11, atomic tasks): ↦ . Steering
	// mail is absorbed between turns, not conversed with.
	ComposerSteer
	// ComposerDisabled is a settled row: "this work is settled — ask aforge
	// about it". It draws NO mark, because a mark promising a composer that
	// will refuse the draft is the affordance lying.
	ComposerDisabled
)

// Mark is the right-edge preview glyph, empty when there is nothing to promise.
func (c ComposerMode) Mark() string {
	switch c {
	case ComposerChat:
		return tokens.GlyphPromptChat
	case ComposerSteer:
		return tokens.GlyphPromptSteer
	}
	return ""
}

// String names the mode.
func (c ComposerMode) String() string {
	switch c {
	case ComposerNone:
		return "none"
	case ComposerChat:
		return "chat"
	case ComposerSteer:
		return "steer"
	case ComposerDisabled:
		return "disabled"
	}
	return "invalid"
}

// Telemetry is line 3 of a card (5.9): model word, cost, context, elapsed,
// worker count — the dimmest tier, tabular. Money is always visible; everything
// else may be dropped on a narrow rail, lowest priority first (the discipline of
// 10.5.22, applied to a card).
//
// Every optional number carries its own presence flag rather than leaning on a
// zero value, because 10.2.8 is explicit that a number which has not arrived and
// a number that is zero are different facts, and the missing one renders — as
// nothing at all here, since a card has no room to explain itself.
type Telemetry struct {
	// Model is the model WORD (5.10: "in model-words not provider IDs"). An
	// empty string drops the cell.
	Model string
	// Effort rides the same chip (`K3 · high`) and is the first thing dropped.
	Effort string
	// Boosted adds ⇡ to the model word — a transient escalation of the work-role
	// binding, and not a separate concept (8.2.16).
	Boosted bool

	// Cost is dollars spent by this row's subtree. HasCost distinguishes "no
	// spend yet" from "no answer".
	Cost    float64
	HasCost bool

	// ContextUsed and ContextWindow drive the one-cell gauge (5.17) and the
	// precise percentage at the focused tier (5.14). Context is per
	// conversational surface: a card's reading is the ORCHESTRATOR's window,
	// and each worker row shows its own. A zero window means unknown.
	ContextUsed   int64
	ContextWindow int64

	// Elapsed is wall time since the row started, and Estimate is what it was
	// expected to take. [tokens.ElapsedToken] brightens the reading once it is
	// past the estimate (5.21) — it never turns amber, because a slow worker is
	// not asking for anything.
	//
	// Elapsed is a LIVE cell: it belongs to the rail and the HUD, never to a
	// committed transcript row, which freezes at finalization (8.1.2).
	Elapsed    time.Duration
	Estimate   time.Duration
	HasElapsed bool

	// Workers is the count under this row. Atomic marks a task that owns no
	// plan (5.11) — its count reads `atomic`, which is the honest answer rather
	// than "1".
	Workers    int
	HasWorkers bool
	Atomic     bool
}

// Empty reports whether there is nothing at all to draw on line 3.
func (t Telemetry) Empty() bool {
	return t.Model == "" && !t.HasCost && t.ContextWindow <= 0 && !t.HasElapsed &&
		!t.HasWorkers && !t.Atomic
}

// Step is one plan step as a progress dot (5.21). The dots map 1:1 to steps —
// discrete and honest, which a continuous bar is not — and the numbers beside
// them stay the primary encoding (5.13).
type Step struct {
	// Name is carried for a tooltip or an action strip; the dot row never
	// prints it.
	Name string
	// Life is the step's state.
	Life Lifecycle
	// Blocked marks a step waiting on a sibling: the amber ⚑ dot.
	Blocked bool
}

// Dot is the step's cell in `●●●◐○⚑○`.
func (s Step) Dot() string {
	if s.Blocked {
		return tokens.GlyphStepBlocked
	}
	switch s.Life {
	case LifeWorking:
		return tokens.GlyphStepRunning
	case LifeSettled:
		return tokens.GlyphStepDone
	case LifeFailed, LifeCancelled:
		return tokens.GlyphFailed
	}
	return tokens.GlyphStepPending
}

// token is the dot's colour. A done step is plain rather than green: a row of
// eight green dots would spend the money hue on structure, and 5.16 gives green
// exactly two jobs.
func (s Step) token() tokens.Token {
	if s.Blocked {
		return tokens.Amber
	}
	switch s.Life {
	case LifeWorking:
		return tokens.Cyan
	case LifeSettled:
		return tokens.TextSecondary
	case LifeFailed, LifeCancelled:
		return tokens.Coral
	}
	return tokens.TextTertiary
}

// StepProgress counts settled steps out of the total, for the `3/7` beside the
// dots.
func StepProgress(steps []Step) (done, total int) {
	for i := range steps {
		if steps[i].Life == LifeSettled {
			done++
		}
	}
	return done, len(steps)
}

// Ref is the artifact law made a field (12.5.1): anything the user will USE
// outside the conversation is born on disk and referenced by a path, never
// carried as prose. A card that has produced a deliverable points at it.
//
// The path is the information, so it is drawn with a MIDDLE ellipsis
// (`src/…/navigate.rs`, 5.21) — tail-truncating a path throws away the filename,
// which is the one part a reader was looking for.
type Ref struct {
	// Path is where the artifact lives. Workspace-relative or absolute; the
	// renderer does not care and never rewrites it.
	Path string
	// Label is an optional human name. Empty means the path speaks for itself.
	Label string
}

// Empty reports whether there is no artifact to point at.
func (r Ref) Empty() bool { return r.Path == "" && r.Label == "" }

// Row is one row of a scope: a card at home, a plan step or a worker inside a
// task, or the scope's own conversational surface at index 0.
//
// It is a VIEW shape, not a store shape. It carries no node ID that will be
// drawn, no seq, no JSON — 5.14's "never shown" tier is enforced by there being
// nothing here to show. [Row.ID] is how the cursor keeps its place across a
// refresh and how [ScopeSource] is asked for a sub-scope; it is never rendered,
// and render_test proves it.
type Row struct {
	// ID is the row's stable identity — a node ID, a session ID, a slug. Never
	// rendered (5.14). An empty ID falls back to Name for cursor keeping, so a
	// hand-built scope still behaves.
	ID string
	// Kind decides the anatomy.
	Kind RowKind
	// Depth is the indent level inside the scope: 0 for the surface and for
	// top-level members, 1 for a worker under a step, and so on. Two spaces per
	// depth (5.13's spacing rhythm). Clamped by [Scope] normalisation.
	Depth int

	// Name is line 1's text: the task word, the step name, the worker name.
	Name string
	// Status is line 2 — one line, plain words, FIRST PERSON, written by the
	// orchestrator and refreshed every turn (5.9; a duty, not a courtesy). The
	// view never invents one: an empty status draws no line, because a made-up
	// status is worse than a missing one.
	Status string

	// Life is the durable state.
	Life Lifecycle
	// Questions is how many questions this row has open on the user. One draws
	// the amber ? glyph; more than one adds the `?2` count chip (5.21).
	Questions int
	// WaitsOn names the siblings this row is blocked behind — the waits-on
	// structure 5.15 asks the task scope to make visible. Names, never IDs.
	WaitsOn []string

	// Composer is the composer this row binds when selected (5.15).
	Composer ComposerMode
	// Meta is line 3.
	Meta Telemetry
	// Steps are the plan step dots shown when the card is focused (5.21).
	Steps []Step
	// Workers are the per-worker rows a focused card expands into (5.9). They
	// are Rows so that a worker is one shape everywhere; only Name, Status,
	// Life and Meta are read here.
	Workers []Row
	// Artifact is the deliverable this row produced (12.5.1).
	Artifact Ref
	// Cut records that the row's last turn ended by something other than its own
	// completion (12.5.2). It renders visibly cut.
	Cut tokens.CutKind
	// Seed is the identity seed for the pastel accent (5.16) — normally the
	// top-level task's ID. Empty means the row inherits its scope's seed.
	Seed string
}

// key is what the cursor and the stable-order merge remember a row by.
func (r Row) key() string {
	if r.ID != "" {
		return r.ID
	}
	return r.Name
}

// Attention derives the glyph state from the row. The precedence IS 5.9's
// question order — does it need me, is it blocked, is it broken, is it moving —
// and a question outranks everything.
func (r Row) Attention() Attention {
	if r.Questions > 0 {
		return AttnQuestion
	}
	switch r.Life {
	case LifeFailed:
		return AttnFailed
	case LifeCancelled:
		return AttnCancelled
	case LifeSettled:
		return AttnSettled
	case LifePaused:
		return AttnPaused
	case LifeWorking:
		return AttnWorking
	}
	// Queued: a waits-on edge is the reason it is queued, and saying so is more
	// information than ○ for the same cell.
	if len(r.WaitsOn) > 0 {
		return AttnWaitsOn
	}
	return AttnQueued
}

// State is the liveness axis (8.1.6): accent while the row is moving, plain
// once it has settled. It carries the row's NAME, which is why a running card's
// title is the accent and a settled card's title is plain text.
func (r Row) State() tokens.State {
	if r.Attention().Live() {
		return tokens.StateLive
	}
	return tokens.StateSettled
}

// GlyphHue is the hue the row's glyph takes, and it is the one place identity
// and the five-word vocabulary are reconciled (5.16 gives the task glyph to
// identity AND working glyphs to cyan, which is a contradiction only until the
// cases are separated):
//
//   - A semantic state wins. A question is amber, a failure coral, a settle
//     green — those are words in the vocabulary and they mean the same thing on
//     every row in the product.
//   - Otherwise a TOP-LEVEL TASK glyph carries its identity pastel, because
//     "which task is this" is the question a rail of ordinary running work
//     actually raises, and liveness is already carried by the state axis on the
//     name beside it.
//   - Otherwise (steps, workers, the surface row) the glyph is cyan while alive
//     and unhued at rest. Those rows have no identity of their own — they
//     belong to the task whose scope they are in, and that identity is already
//     on the scope header.
func (r Row) GlyphHue() tokens.Hue {
	if h := r.Attention().Hue(); h != tokens.HueNone && h != tokens.HueAlive {
		return h
	}
	if r.Kind == RowTask {
		return tokens.HueIdentity
	}
	return r.Attention().Hue()
}

// EffectiveComposer is what the composer will actually be when this row is
// selected. A terminal row disables it — "this work is settled — ask aforge
// about it" (5.15) — regardless of what room it once had.
func (r Row) EffectiveComposer() ComposerMode {
	if r.Composer == ComposerNone {
		return ComposerNone
	}
	if r.Life.Terminal() {
		return ComposerDisabled
	}
	return r.Composer
}

// itemState maps the row onto the collapse vocabulary of internal/tui2/blocks,
// so the fold line's breakdown reads in the words the rest of the surface uses.
func (r Row) itemState() blocks.ItemState {
	switch r.Attention() {
	case AttnQuestion, AttnWaitsOn:
		return blocks.ItemWaiting
	case AttnWorking:
		return blocks.ItemRunning
	case AttnSettled:
		return blocks.ItemDone
	case AttnFailed:
		return blocks.ItemFailed
	case AttnCancelled:
		return blocks.ItemAborted
	}
	return blocks.ItemPending
}

// cutMark is the short word a cut row carries beside [tokens.GlyphCut]. The
// wording comes from blocks' EndState so the rail and the transcript say the
// same thing about the same event.
func cutMark(k tokens.CutKind) string {
	switch k {
	case tokens.CutLengthCap:
		return blocks.EndTruncatedByCap.Mark()
	case tokens.CutStreamDrop:
		return blocks.EndStreamDropped.Mark()
	case tokens.CutInterrupt:
		return blocks.EndInterrupted.Mark()
	}
	return ""
}
