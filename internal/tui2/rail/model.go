package rail

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
func (m *Model) Move(delta int) Event {
	lv := m.top()
	return m.Select(lv.cursor + delta)
}

// Select moves the cursor to an index, clamped.
func (m *Model) Select(i int) Event {
	lv := m.top()
	i = clamp(i, 0, len(lv.scope.Rows)-1)
	if i == lv.cursor {
		return Event{}
	}
	lv.cursor = i
	return m.event(EventSelected)
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
//     land at the end. A card that re-sorts under the cursor makes the user
//     lose their place, and the design's answer to "this one needs attention"
//     is a badge, not a jump to the top.
//  2. THE CURSOR KEEPS ITS ROW, by ID rather than by index. If the row it was
//     on is gone the cursor stays at the same index, clamped, which is what a
//     list does when the thing under your finger is deleted.
//  3. A SCOPE THAT VANISHED POPS. If the task you were inside no longer exists,
//     the stack unwinds to the deepest scope that does, rather than rendering a
//     room that is not there.
func (m *Model) Refresh() Event {
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
		scope.Rows = m.stableOrder(lv.scope.Rows, scope.Rows)
		lv.scope = scope
		lv.cursor = indexOfKey(scope.Rows, before, lv.cursor)
	}
	if popped {
		return m.event(EventScopePopped)
	}
	return m.event(EventSelected)
}

// stableOrder merges the incoming rows into the order already on screen: rows
// the user can see keep their places, new rows append. The surface row is
// always index 0 on both sides, so it merges for free.
//
// One map, two passes, and the map doubles as the consumed set — a row is
// emitted exactly once whether the source repeats a key or not.
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
	out := make([]Row, 0, len(next))
	// Known rows, in the order they were already drawn in.
	for i := range old {
		k := old[i].key()
		if k == "" {
			continue
		}
		if j, ok := m.order[k]; ok {
			out = append(out, next[j])
			delete(m.order, k)
		}
	}
	// Then whatever is genuinely new, in the source's order.
	for j := range next {
		k := next[j].key()
		if k == "" {
			out = append(out, next[j])
			continue
		}
		if _, unclaimed := m.order[k]; unclaimed {
			out = append(out, next[j])
			delete(m.order, k)
		}
	}
	return out
}

func indexOfKey(rows []Row, key string, fallback int) int {
	if key != "" {
		for i := range rows {
			if rows[i].key() == key {
				return i
			}
		}
	}
	return clamp(fallback, 0, len(rows)-1)
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
	lv.cursor = clamp(lv.cursor, 0, len(lv.scope.Rows)-1)
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
