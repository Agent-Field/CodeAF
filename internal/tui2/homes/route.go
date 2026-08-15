package homes

import "github.com/Agent-Field/aforge-v2/internal/tui2/rail"

// Lifecycle is [rail.Lifecycle], aliased rather than redeclared. A home's rows
// become rail rows and a rail row's state vocabulary is already the product's;
// a parallel enum here would be a second place the mapping from store.Status
// lives, and the two would drift on the day a status is added.
type Lifecycle = rail.Lifecycle

// The lifecycle vocabulary, re-exported so a wiring filling a [State] does not
// have to import the rail to name a state.
const (
	LifeQueued    = rail.LifeQueued
	LifeWorking   = rail.LifeWorking
	LifeSettled   = rail.LifeSettled
	LifeFailed    = rail.LifeFailed
	LifeCancelled = rail.LifeCancelled
	LifePaused    = rail.LifePaused
)

// RouteID is one of the eight routes the self room re-scopes into (5.24: "self
// re-scopes to its 9 sub-routes as rail rows; the main pane is the selected
// route").
//
// The count reconciles like this, and it is worth stating because 5.24 says
// nine: the old page had NINE routes, of which one — `root` — was the list of
// the other eight. Under the scope model that root IS the scope, row 0, the
// surface you speak into; it is not a member of itself. So nine routes become
// one scope and eight rows, and no route was lost. The order below is the old
// page's, unchanged, because a row that moves is a row a hand has to re-find.
type RouteID uint8

const (
	// RouteCrafts is what aforge has learned to repeat.
	RouteCrafts RouteID = iota
	// RouteCompetence is where it is strong and where its frontier is.
	RouteCompetence
	// RouteBeliefs is the notebook, seen from self. It is the SAME collection
	// the notebook home shows — one fact table, two doors, one read.
	RouteBeliefs
	// RouteSkills is the tools it built and checked.
	RouteSkills
	// RouteWatches is the charters, seen from self — again one collection, and
	// the standing home is its other door.
	RouteWatches
	// RouteServices is the services, seen from self.
	RouteServices
	// RoutePractice is what it did with idle time.
	RoutePractice
	// RouteDials is how it balances demand against curiosity. A settings
	// projection, not a collection.
	RouteDials
)

// Routes returns the eight in display order.
func Routes() []RouteID {
	return []RouteID{
		RouteCrafts, RouteCompetence, RouteBeliefs, RouteSkills,
		RouteWatches, RouteServices, RoutePractice, RouteDials,
	}
}

// Valid reports whether r is one of the eight.
func (r RouteID) Valid() bool { return r <= RouteDials }

// Word is the route's name as it is drawn. Lowercase, one word where one word
// is honest — the old page's Title Case belonged to a settings app, and 5.13
// gives chrome a quieter register.
func (r RouteID) Word() string {
	switch r {
	case RouteCrafts:
		return "know-how"
	case RouteCompetence:
		return "competence"
	case RouteBeliefs:
		return "beliefs"
	case RouteSkills:
		return "skills"
	case RouteWatches:
		return "watches"
	case RouteServices:
		return "services"
	case RoutePractice:
		return "practice"
	case RouteDials:
		return "dials"
	}
	return ""
}

// String names the route, "invalid" included.
func (r RouteID) String() string {
	if w := r.Word(); w != "" {
		return w
	}
	return "invalid"
}

// RowID is the id the route's rail row carries. Namespaced under the self
// scope so a route id can never collide with an item id inside another room.
func (r RouteID) RowID() string {
	if !r.Valid() {
		return ""
	}
	return "self/" + r.Word()
}

// ParseRowID resolves a self-scope row id back to its route.
func ParseRowID(id string) (RouteID, bool) {
	for _, r := range Routes() {
		if r.RowID() == id {
			return r, true
		}
	}
	return 0, false
}

// Explain is the sentence beside the route's name — the only place a person
// learns what aforge was doing while they were away. It lives on the route, not
// at the render site, for the reason the old page gave: an explainer that lives
// at a render site is an explainer that gets two spellings.
func (r RouteID) Explain() string {
	switch r {
	case RouteCrafts:
		return "what I've learned to repeat — versioned, measured, reused"
	case RouteCompetence:
		return "where I'm strong and where I'm at my frontier — measured, not guessed"
	case RouteBeliefs:
		return "what I hold true about you and this machine — corrections welcome"
	case RouteSkills:
		return "tools I built and checked; everything I run for you can reach them"
	case RouteWatches:
		return "standing goals checking on their own schedule"
	case RouteServices:
		return "processes I keep alive for you"
	case RoutePractice:
		return "what I did with idle time, and what it taught me"
	case RouteDials:
		return "how I balance demand against curiosity, and what I may propose"
	}
	return ""
}

// Empty is what the route says when it has nothing — and it TEACHES rather than
// reporting, which is 5.22 rule 6 taken literally: an empty state names the
// thing that would put something there.
func (r RouteID) Empty() string {
	switch r {
	case RouteCrafts:
		return "none yet; I keep one when a job's shape looks worth repeating"
	case RouteCompetence:
		return "nothing measured yet; a scope needs runs behind it before I'll claim anything"
	case RouteBeliefs:
		return "nothing yet; I write one down when work teaches me something durable"
	case RouteSkills:
		return "none yet; a procedure has to run and pass twice before I keep it"
	case RouteWatches:
		return "none yet; say \"whenever…\" or \"remind me…\" and I'll stand one up"
	case RouteServices:
		return "none running; I start one when work needs something to stay up"
	case RoutePractice:
		return "nothing yet; I practice in the quiet, inside the carve-out you set"
	case RouteDials:
		return "no dials wired yet"
	}
	return ""
}

// Counted reports whether the route has a count worth showing. Practice and
// Dials are states rather than collections — a number in front of them would be
// a number about nothing.
func (r RouteID) Counted() bool {
	switch r {
	case RoutePractice, RouteDials:
		return false
	}
	return true
}

// Route returns the wiring's row for one route, or the honest empty one. A
// route the wiring never filled still has a name, an explainer and an empty
// line, so a partially wired surface shows eight rooms with nothing in them
// rather than a short list that looks complete.
func (s Self) Route(id RouteID) Route {
	for i := range s.Routes {
		if s.Routes[i].ID == id {
			return s.Routes[i]
		}
	}
	return Route{ID: id, HasCount: false}
}
