package rail

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The scope model (5.15): one scope on screen, one cursor over its rows, and
// four gestures — move, enter, escape, refresh.
//
// SELECTION IS STATE. It lives in [level.cursor] and is never derived in
// render, so a render is a pure function of the model and the width and two
// renderings of the same model always agree about what is selected. The
// renderer is told; it never decides.

// maxScopeDepth caps the scope stack. 5.6 has three depths and 5.15 recurses
// them, so the cap is generous rather than tight; it exists because a source
// that answers Scope(x) with a scope containing x would otherwise descend
// forever, and a rail must not be able to hang on bad data.
const maxScopeDepth = 8

// EventKind is what a gesture produced.
type EventKind uint8

const (
	// EventNone is a gesture that changed nothing — a cursor already at the
	// end, an escape at home. The shell draws nothing and journals nothing.
	EventNone EventKind = iota
	// EventSelected is a PREVIEW (5.15): the cursor moved, and the main pane
	// should show a lightweight preview of the newly selected row. A keystroke
	// must never cost a full-frame repaint of a whole transcript.
	EventSelected
	// EventOpened is the COMMITMENT: enter on a row that has no scope beneath
	// it. The main pane binds that row's surface for real, and the composer
	// binds the mode in [Event.Composer].
	EventOpened
	// EventScopeEntered is enter on a row that owns a scope: the rail re-scopes
	// to that task's DAG and the cursor lands on its orchestrator.
	EventScopeEntered
	// EventScopePopped is escape: the scope stack popped, and the cursor is
	// back on the row it descended from — esc pops scope, never just selection.
	EventScopePopped
)

// String names the kind.
func (k EventKind) String() string {
	switch k {
	case EventNone:
		return "none"
	case EventSelected:
		return "selected"
	case EventOpened:
		return "opened"
	case EventScopeEntered:
		return "scope-entered"
	case EventScopePopped:
		return "scope-popped"
	}
	return "invalid"
}

// Event is what a gesture produced, in the vocabulary the shell needs to bind
// the main pane and the composer. It is a plain value on purpose: this package
// declares no bubbletea dependency, so the shell wraps an Event in whatever
// message type it uses and the component stays testable without a terminal.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// ScopeID is the scope the model is in AFTER the gesture.
	ScopeID string
	// RowID is the selected row's ID after the gesture. Never rendered (5.14);
	// this is the shell's handle for "show me that surface".
	RowID string
	// RowKind and Composer are what the shell needs to bind the main pane and
	// the composer without asking the model a second question.
	RowKind  RowKind
	Composer ComposerMode
	// Cursor is the selected index in the current scope.
	Cursor int
}

// Empty reports whether nothing happened.
func (e Event) Empty() bool { return e.Kind == EventNone }

// level is one entry on the scope stack: a scope and the cursor inside it. The
// cursor is per level, which is what makes escape restore the row you descended
// from rather than dumping you at the top of the parent.
type level struct {
	scope  Scope
	cursor int
}

// Model is the scope model. It is not safe for concurrent use; it lives on the
// shell's goroutine like every other piece of view state.
type Model struct {
	src   ScopeSource
	stack []level
	// order is scratch for the stable-order merge, kept on the model so a
	// refresh on a timer does not allocate a map per tick.
	order map[string]int
	// clock is the window's shared animation clock, or nil in a headless
	// render; at is the instant the rows on screen were measured at. See
	// [Model.SetClock].
	clock *Clock
	at    time.Time
	// resort arms ONE refresh to take the source's order verbatim. See
	// [Model.Resort].
	resort bool
}

// Resort arms the next [Model.Refresh] to take the source's order as it comes,
// instead of merging it into the order already on screen.
//
// IT IS THE OTHER HALF OF 7.2, and without it the stability law slowly becomes
// a lie about freshness. [Model.stableOrder] freezes the visible order
// CONTINUOUSLY, which is exactly right while somebody is looking: a row that
// moved out from under a pointer already aimed at it is the betrayal that law
// exists to prevent. But a rail that has been collapsed for an hour is not
// being looked at, and re-opening it to the order of an hour ago — a job that
// finished at nine still sitting above one that started at ten — is the
// stability law protecting a place nobody was standing in.
//
// So the resort is a BARRIER rather than a policy: it happens at the moment the
// rail comes back, before the reader's eye has landed anywhere, and never
// while the column is open. The caller that knows when that moment is, is the
// one that owns the collapse state; this is the door it reaches for.
func (m *Model) Resort() {
	if m != nil {
		m.resort = true
	}
}

// Clock is the shared animation clock, named here so this package's callers do
// not have to spell the blocks package to hand one over. It is an alias and not
// a wrapper: there is exactly one clock in a window (8.1.3) and a second type
// standing for it would be a second answer to "what time is this frame".
type Clock = blocks.Clock

// SetClock hands the rail the window's shared animation clock, and stamps the
// instant the rows on screen were measured at.
//
// THIS IS THE RAIL'S ONE MOTION AND IT IS A NUMBER (§11 "numbers tick").
// §18.2 is explicit that the spinner is for "transient tool rows only … never a
// rail card, an agent row, or anything durable: a dancing glyph on a long-lived
// object is a lie about liveness", which is why this package still contains no
// spinner frame. What was genuinely broken is the other half: a card's elapsed
// is measured inside the snapshot that produced the row, snapshots are rebuilt
// only when the journal moves, and a worker inside a tool call journals nothing
// at all — so a running card sat on one figure for minutes and read as a rail
// that had stopped. The clock is what lets the cell keep counting between
// snapshots, and it is the whole of the change.
func (m *Model) SetClock(c *Clock) {
	if m == nil {
		return
	}
	m.clock = c
	if c != nil && m.at.IsZero() {
		m.at = c.Now()
	}
}

// Drift is how long the rows on screen have been standing since they were
// measured: the gap between the snapshot's instant and the clock's latched one.
//
// It is added ONLY to rows that are still moving ([View.telemetry]) — a settled
// row's clock stopped when the work did, and ageing it would be the surface
// inventing time that nobody spent (8.2.20). Without a clock, or before the
// first latch, it is zero and the rail draws exactly what it always drew.
func (m *Model) Drift() time.Duration {
	if m == nil || m.clock == nil || m.at.IsZero() {
		return 0
	}
	now := m.clock.Now()
	if now.IsZero() {
		return 0
	}
	if drift := now.Sub(m.at); drift > 0 {
		return drift
	}
	return 0
}

// New builds a model over a source and loads the home scope. A source that
// cannot answer home yields an empty home scope with just its surface row,
// which is the honest empty state: a rail with `aforge` and nothing else.
func New(src ScopeSource) *Model {
	m := &Model{src: src, stack: make([]level, 0, 4)}
	scope, ok := scopeFrom(src, HomeScopeID)
	if !ok {
		scope = Scope{ID: HomeScopeID, Title: "aforge"}.normalize()
	}
	m.stack = append(m.stack, level{scope: scope})
	return m
}

func scopeFrom(src ScopeSource, id string) (Scope, bool) {
	if src == nil {
		return Scope{}, false
	}
	s, ok := src.Scope(id)
	if !ok {
		return Scope{}, false
	}
	s.ID = id
	return s.normalize(), true
}

// Scope is the scope on screen.
func (m *Model) Scope() Scope { return m.top().scope }

// Rows are the rows on screen, surface first. The slice is the model's; a
// caller may read it and must not keep or mutate it.
func (m *Model) Rows() []Row { return m.top().scope.Rows }

// Len is how many rows the current scope has.
func (m *Model) Len() int { return len(m.top().scope.Rows) }

// Cursor is the selected index. It is always in range: an empty scope is
// impossible because normalisation guarantees a surface row.
func (m *Model) Cursor() int { return m.top().cursor }

// Selected is the row under the cursor.
func (m *Model) Selected() Row {
	lv := m.top()
	return lv.scope.Rows[clamp(lv.cursor, 0, len(lv.scope.Rows)-1)]
}

// Depth is how many scopes deep the rail is: 0 at home.
func (m *Model) Depth() int { return len(m.stack) - 1 }

// Breadcrumb is the spatial truth (5.15), root first. The rail's scope header
// is its tail.
func (m *Model) Breadcrumb() []string {
	out := make([]string, len(m.stack))
	for i := range m.stack {
		out[i] = m.stack[i].scope.Title
	}
	return out
}

// Seed is the current scope's identity seed, which tints the selection band
// inside a task's scope and leaves it plain at home (5.16).
func (m *Model) Seed() string { return m.top().scope.Seed }

// Preview is the current selection as an Event, for a shell binding the main
// pane at startup or after a resize. It reports [EventSelected] even though
// nothing moved, because the caller is asking what to show, not what changed.
func (m *Model) Preview() Event { return m.event(EventSelected) }

// Move slides the cursor by delta and previews what it lands on (5.15:
// selection previews, enter commits). It CLAMPS rather than wrapping — a rail
// that wraps teleports the eye from the bottom of a list to the top, and the
// gesture the user meant was "further down".
//
// A HEADING IS NOT A STOP. j crossing the seam between the threads section and
// the work section lands on the first job, not on the word `work`: the section
// row is chrome (see [RowKind.Selectable]), and a cursor that had to be pressed
// past it twice would be charging the reader a keystroke for a line that is not
// a place. The step is taken in the direction of travel, which is what makes
// the skip invisible in both directions.
func (m *Model) Move(delta int) Event {
	lv := m.top()
	step := 1
	if delta < 0 {
		step = -1
	}
	return m.Select(seekSelectable(lv.scope.Rows, lv.cursor+delta, step))
}

// Select moves the cursor to an index, clamped, and snapped onto the nearest
// row a cursor may rest on.
//
// The snap searches FORWARD first because the two chrome kinds both stand
// ABOVE what they describe — a heading names the rows under it and a note
// stands where those rows would have been — so the row a caller pointing at one
// meant is the one after it. Falling back to a backwards search covers the one
// case forward cannot: a note at the very foot of the rail.
func (m *Model) Select(i int) Event {
	lv := m.top()
	i = seekSelectable(lv.scope.Rows, i, 1)
	if i == lv.cursor {
		return Event{}
	}
	lv.cursor = i
	return m.event(EventSelected)
}

// seekSelectable clamps an index into the scope and walks it in step's
// direction until it finds a row the cursor may hold, then back the other way.
// A scope of nothing but chrome — which normalisation makes impossible, since
// row 0 is always the surface — returns the clamped index unchanged rather than
// looping.
func seekSelectable(rows []Row, i, step int) int {
	if len(rows) == 0 {
		return 0
	}
	i = clamp(i, 0, len(rows)-1)
	for j := i; j >= 0 && j < len(rows); j += step {
		if rows[j].Kind.Selectable() {
			return j
		}
	}
	for j := i; j >= 0 && j < len(rows); j -= step {
		if rows[j].Kind.Selectable() {
			return j
		}
	}
	return i
}

// SelectID moves the cursor to the row with this ID (or name, for a row with no
// ID) and reports whether it was found. It is how the shell restores a
// selection — after a refresh, or when a dispatch echo asks the rail to point
// at the task it woke.
func (m *Model) SelectID(id string) (Event, bool) {
	lv := m.top()
	for i := range lv.scope.Rows {
		if lv.scope.Rows[i].key() == id {
			return m.Select(i), true
		}
	}
	return Event{}, false
}

// Enter commits. On a row that owns a scope it DESCENDS — the rail re-scopes to
// that task's DAG and the cursor lands on the orchestrator, which is the row
// you came for (5.15). On anything else it opens the row's surface in the main
// pane.
//
// Whether a row owns a scope is asked of the source, not stored on the row, so
// there is exactly one answer to the question and it is always current.
func (m *Model) Enter() Event {
	row := m.Selected()
	if row.Kind == RowSurface {
		return m.event(EventOpened)
	}
	if len(m.stack) >= maxScopeDepth {
		return m.event(EventOpened)
	}
	key := row.key()
	if key == "" || m.inStack(key) {
		return m.event(EventOpened)
	}
	scope, ok := scopeFrom(m.src, key)
	if !ok {
		return m.event(EventOpened)
	}
	if scope.Seed == "" {
		scope.Seed = row.Seed
		if scope.Seed == "" {
			scope.Seed = key
		}
	}
	m.stack = append(m.stack, level{scope: scope})
	return m.event(EventScopeEntered)
}

// Escape pops the scope, never just the selection (5.15). At home it reports
// nothing happened and leaves the meaning of esc to the shell's ladder
// (8.2.21: esc acts on what you are watching).
func (m *Model) Escape() Event {
	if len(m.stack) <= 1 {
		return Event{}
	}
	m.stack = m.stack[:len(m.stack)-1]
	return m.event(EventScopePopped)
}

// Home pops every scope at once, back to the home rail.
func (m *Model) Home() Event {
	if len(m.stack) <= 1 {
		return Event{}
	}
	m.stack = m.stack[:1]
	return m.event(EventScopePopped)
}

// Refresh re-reads every scope on the stack from the source. It is the only
// door new data comes through, and it keeps three promises:
//
//  1. THE ORDER DOES NOT MOVE (7.2). Rows already on screen keep their relative
//     order whatever order the source now returns them in; genuinely new rows
//     land where the SOURCE puts them, which for a newest-first list is above
//     the rows they arrived after. A card that re-sorts under the cursor makes
//     the user lose their place, and the design's answer to "this one needs
//     attention" is a badge, not a jump to the top.
//  2. THE CURSOR KEEPS ITS ROW, by ID rather than by index. If the row it was
//     on is gone the cursor stays at the same index, clamped, which is what a
//     list does when the thing under your finger is deleted.
//  3. A SCOPE THAT VANISHED POPS. If the task you were inside no longer exists,
//     the stack unwinds to the deepest scope that does, rather than rendering a
//     room that is not there.
func (m *Model) Refresh() Event {
	// The rows about to be loaded were measured in the snapshot this refresh is
	// reading, so the drift restarts from here: ageing a fresh figure by the
	// time since the LAST snapshot would double-count every interval.
	if m.clock != nil {
		m.at = m.clock.Now()
	}
	// The barrier is consumed whether or not a scope answered, so an armed
	// resort cannot survive to reorder a LATER refresh that the reader is
	// watching.
	resort := m.resort
	m.resort = false
	popped := false
	for i := 0; i < len(m.stack); i++ {
		scope, ok := scopeFrom(m.src, m.stack[i].scope.ID)
		if !ok {
			if i == 0 {
				// Home always exists, even when the source is empty.
				continue
			}
			m.stack = m.stack[:i]
			popped = true
			break
		}
		lv := &m.stack[i]
		before := lv.scope.Rows[clamp(lv.cursor, 0, len(lv.scope.Rows)-1)].key()
		if !resort {
			scope.Rows = m.stableOrder(lv.scope.Rows, scope.Rows)
		}
		lv.scope = scope
		lv.cursor = indexOfKey(scope.Rows, before, lv.cursor)
	}
	if popped {
		return m.event(EventScopePopped)
	}
	return m.event(EventSelected)
}

// stableOrder merges the incoming rows into the order already on screen: rows
// the user can see keep their places, and a row the source has NEVER shown
// before is spliced in ABOVE the first row it precedes. The surface row is
// always index 0 on both sides, so it merges for free.
//
// THE DEFECT, reported in eight words: "rail seems to be adding tasks to bottom
// instead of top down". The source has always handed this list newest-first
// (chat/scope.go sorts the job roots on CreatedSeq, descending) and this
// function appended every unseen row to the END of the whole scope — so a job
// commissioned ten seconds ago appeared under every older job AND under the
// collapsed homes group at the foot of the rail, which is the last place a
// reader looks. §6 asks for the opposite and says why: "live floats, settled
// sinks", with the stability law holding only for rows that are ALREADY VISIBLE.
// A row nobody has seen yet cannot lose its place, because it does not have one.
//
// The merge is therefore an ANCHOR merge rather than an append. Walking the
// source in its own order, every unseen row is parked against the next row that
// IS on screen; then the old rows are emitted in their own order, each preceded
// by whatever parked against it. What that buys, in one sentence per property:
//
//   - a new job lands directly above the newest job already drawn, which is the
//     top of the task section rather than the bottom of the rail;
//   - a visible row never moves relative to another visible row, because the old
//     order is what drives the output loop;
//   - the section a new row belongs to is decided by the SOURCE, which is the
//     only thing that knows — this function never learns what a section is.
//
// A row whose anchor has itself vanished from the source still emits, at the
// tail, because a job that arrived in the same refresh that retired the row
// above it is still a job.
func (m *Model) stableOrder(old, next []Row) []Row {
	if len(old) == 0 || len(next) == 0 {
		return next
	}
	if m.order == nil {
		m.order = make(map[string]int, len(next))
	}
	for k := range m.order {
		delete(m.order, k)
	}
	for j := range next {
		if k := next[j].key(); k != "" {
			if _, dup := m.order[k]; !dup {
				m.order[k] = j
			}
		}
	}
	// where each visible row already sits, so an unseen row can be told from a
	// moved one and an anchor can be compared against its rivals.
	where := make(map[string]int, len(old))
	for i := range old {
		if k := old[i].key(); k != "" {
			if _, dup := where[k]; !dup {
				where[k] = i
			}
		}
	}

	// Pass one, walked BACKWARDS: each unseen row is parked against the visible
	// row it will be drawn above.
	//
	// The anchor is not simply the next visible row in the source — it is the
	// HIGHEST-STANDING of every visible row that follows it there. The source is
	// free to re-sort the rows the reader can see, and that re-sort is ignored
	// (property 2 above); if the anchor were the next source row, a re-sort
	// happening in the same refresh as an arrival would decide where the arrival
	// landed, and the reader would watch a new job appear in the middle of a
	// list nothing else about the frame explains. Reading the source backwards
	// is what makes "the highest of them" a running minimum instead of a scan,
	// and it keeps two arrivals in the source's own order within one anchor.
	arrivals := make(map[string][]Row, 4)
	parked := make(map[string]bool, 4)
	var tail []Row
	anchor, anchorAt := "", len(old)
	for j := len(next) - 1; j >= 0; j-- {
		k := next[j].key()
		if k != "" {
			if at, visible := where[k]; visible {
				if at < anchorAt {
					anchor, anchorAt = k, at
				}
				continue
			}
			// A source that repeats a key still draws the row once.
			if parked[k] {
				continue
			}
			parked[k] = true
		}
		// A keyless row has no identity to remember, so it is new every refresh
		// and belongs exactly where the source drew it.
		if anchor == "" {
			tail = append([]Row{next[j]}, tail...)
			continue
		}
		arrivals[anchor] = append([]Row{next[j]}, arrivals[anchor]...)
	}

	out := make([]Row, 0, len(next))
	for i := range old {
		k := old[i].key()
		if k == "" {
			continue
		}
		// The arrivals flush even when their anchor has itself retired out of
		// the source this refresh: a job that landed in the same read that
		// settled the row above it is still a job.
		out = append(out, arrivals[k]...)
		delete(arrivals, k)
		if j, ok := m.order[k]; ok {
			out = append(out, next[j])
			delete(m.order, k)
		}
	}
	// Whatever followed the LAST visible row — the first job on an empty rail,
	// or rows the source drew below everything already on screen.
	return append(out, tail...)
}

func indexOfKey(rows []Row, key string, fallback int) int {
	if key != "" {
		for i := range rows {
			if rows[i].key() == key {
				return i
			}
		}
	}
	return seekSelectable(rows, fallback, 1)
}

func (m *Model) inStack(id string) bool {
	for i := range m.stack {
		if m.stack[i].scope.ID == id {
			return true
		}
	}
	return false
}

func (m *Model) top() *level { return &m.stack[len(m.stack)-1] }

func (m *Model) event(kind EventKind) Event {
	lv := m.top()
	lv.cursor = seekSelectable(lv.scope.Rows, lv.cursor, 1)
	row := lv.scope.Rows[lv.cursor]
	return Event{
		Kind:     kind,
		ScopeID:  lv.scope.ID,
		RowID:    row.key(),
		RowKind:  row.Kind,
		Composer: row.EffectiveComposer(),
		Cursor:   lv.cursor,
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
