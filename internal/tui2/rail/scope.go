package rail

// HomeScopeID is the id of the home scope — the empty string, so a zero-valued
// caller asks for home and gets it.
const HomeScopeID = ""

// Scope is what the rail shows: exactly one conversational surface and its
// members (5.15). It is a value; a [ScopeSource] hands one over and the model
// owns the copy, so a source that keeps rebuilding its own slices cannot mutate
// what is on screen.
type Scope struct {
	// ID identifies the scope to its source. [HomeScopeID] is home; a task
	// scope's ID is the task's row ID, which is how [Model.Enter] descends
	// without a second lookup vocabulary.
	ID string
	// Title is the scope's name: `aforge` at home, the task word inside a task.
	// It is the breadcrumb tail and the rail's scope header (5.15).
	Title string
	// Seed is the identity seed for the scope's pastel (5.16) — the top-level
	// task ID. Empty at home, which has no identity and therefore an untinted
	// selection band.
	Seed string
	// Rows are the scope's rows in DISPLAY order. Row 0 must be the
	// conversational surface; normalisation supplies one if the source did not.
	// Order is the source's to choose and the model's to keep — a visible row
	// never moves (7.2).
	Rows []Row
}

// ScopeSource is the whole data contract of this package.
//
// It is deliberately one method. The integration lane binds it to the reads the
// store already answers — the board (top-level nodes with their usage and open
// questions) for home, and plan sight (a task's steps, its workers and their
// waits-on edges) for a task scope — and nothing about this interface requires
// a new store method to exist first.
//
// The mapping the lane will write, stated once so it does not have to be
// guessed:
//
//	home  → Scope{ID: HomeScopeID, Title: "aforge", Rows: [surface, task…]}
//	         one Row{Kind: RowTask} per top-level node, Life from store.Status,
//	         Meta.Cost from store.JobUsage.Cost, Questions from the open
//	         questions addressed to that subtree.
//	task  → Scope{ID: <task node id>, Title: <task word>, Seed: <task node id>,
//	         Rows: [orchestrator surface, step…, worker…]} with Depth carrying
//	         the tree indent and WaitsOn carrying the edge names.
//
// A source that cannot answer returns false, and the model treats that as "this
// row has no room to descend into" rather than as an error — a rail that
// refuses to move because a read failed is worse than a rail that stays put.
type ScopeSource interface {
	Scope(id string) (Scope, bool)
}

// SourceFunc adapts a function to [ScopeSource]. Tests and thin adapters use
// it; the integration lane will more likely implement the interface on the
// object that already holds the store handle.
type SourceFunc func(id string) (Scope, bool)

// Scope implements [ScopeSource].
func (f SourceFunc) Scope(id string) (Scope, bool) {
	if f == nil {
		return Scope{}, false
	}
	return f(id)
}

// normalize returns the scope the model will hold: row 0 is guaranteed to be a
// surface, depths are non-negative, and the surface sits at depth 0.
//
// The guarantee is worth its cost. Every rendering, the fold policy and the
// composer binding all lean on "row 0 is the surface", and a source that
// forgets would otherwise produce a scope the user cannot speak into — silently,
// and only for that one task.
func (s Scope) normalize() Scope {
	rows := make([]Row, 0, len(s.Rows)+1)
	if len(s.Rows) == 0 || s.Rows[0].Kind != RowSurface {
		rows = append(rows, Row{
			ID:       s.ID,
			Kind:     RowSurface,
			Name:     s.Title,
			Composer: ComposerChat,
			Seed:     s.Seed,
		})
	}
	rows = append(rows, s.Rows...)
	rows[0].Kind = RowSurface
	rows[0].Depth = 0
	if rows[0].Name == "" {
		rows[0].Name = s.Title
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Depth < 0 {
			rows[i].Depth = 0
		}
		if rows[i].Depth > maxIndentDepth {
			rows[i].Depth = maxIndentDepth
		}
	}
	s.Rows = rows
	if s.Title == "" {
		s.Title = rows[0].Name
	}
	return s
}

// maxIndentDepth caps the indent a source can ask for. A DAG can nest further
// than a 28-column rail can show, and an indent that eats the whole row shows
// the structure by hiding the content.
const maxIndentDepth = 4

// Members returns the scope's rows without the surface row.
func (s Scope) Members() []Row {
	if len(s.Rows) < 2 {
		return nil
	}
	return s.Rows[1:]
}

// Live reports whether anything in the scope is moving. It picks the overflow
// policy (8.1.7): a live list protects its running rows, a finalized one gives
// the slots to its failures.
func (s Scope) Live() bool {
	for i := range s.Rows {
		if s.Rows[i].Attention().Live() {
			return true
		}
	}
	return false
}
