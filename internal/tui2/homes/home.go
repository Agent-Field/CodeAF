package homes

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// Home is one of the four rooms 5.24 gives the rail: the notebook, self,
// standing and services. They are rooms and not pages — each is a scope the
// rail re-scopes into on enter, with the main pane showing whichever member the
// cursor rests on (5.15).
//
// The zero value is [HomeNone], which is not a room; it is what
// [ParseScopeID] returns when an id belongs to somebody else, so a wiring that
// forwards every scope id here gets a clean "not mine" rather than a wrong room.
type Home uint8

const (
	// HomeNone is not a home. See [Home].
	HomeNone Home = iota
	// HomeNotebook is what aforge believes — the belief notebook, which was an
	// overlay in the old chat and is a room here.
	HomeNotebook
	// HomeSelf is the eight self routes as rail rows (5.24: "no separate self
	// page, no page-local key grammar").
	HomeSelf
	// HomeStanding is the charter list, whose focused card's affordances ARE
	// the charter command verbs, rendered from the registry (5.22).
	HomeStanding
	// HomeServices is the service list. A service room is a log tail and three
	// verbs with the composer disabled — a service is not a conversation.
	HomeServices
)

// All returns the four homes in the order the rail draws them. The order is
// 5.24's own sentence order and is deliberately stable: a row that moves is a
// row a hand has to re-find (7.2).
func All() []Home { return []Home{HomeNotebook, HomeSelf, HomeStanding, HomeServices} }

// Word is the room's name as it is drawn — lowercase, one word, the product's
// own vocabulary. It is also the last element of the scope id.
func (h Home) Word() string {
	switch h {
	case HomeNotebook:
		return "notebook"
	case HomeSelf:
		return "self"
	case HomeStanding:
		return "standing"
	case HomeServices:
		return "services"
	}
	return ""
}

// String names the home, "none" included, for tests and for a %v that never
// prints a bare integer.
func (h Home) String() string {
	if w := h.Word(); w != "" {
		return w
	}
	return "none"
}

// Valid reports whether h is one of the four.
func (h Home) Valid() bool { return h >= HomeNotebook && h <= HomeServices }

// scopePrefix namespaces every id this package answers to. It is a prefix and
// not a set of bare words so that a home can never collide with a task id, a
// room id or the empty [rail.HomeScopeID] — chat's own row ids are namespaced
// the same way, and an unprefixed "self" would be one badly-named task away
// from opening the wrong room.
const scopePrefix = "home:"

// ScopeID is the id the rail asks [Source.Scope] for, and the id a row in the
// home group carries. [HomeNone] has none and returns "".
func (h Home) ScopeID() string {
	if !h.Valid() {
		return ""
	}
	return scopePrefix + h.Word()
}

// ParseScopeID resolves a scope id to its home, reporting false for anything
// that is not one of the four. An unknown id inside this package's own
// namespace is still false: a namespace is not a promise that every spelling
// in it exists, and guessing would open a room the id did not name.
func ParseScopeID(id string) (Home, bool) {
	if !strings.HasPrefix(id, scopePrefix) {
		return HomeNone, false
	}
	switch id[len(scopePrefix):] {
	case "notebook":
		return HomeNotebook, true
	case "self":
		return HomeSelf, true
	case "standing":
		return HomeStanding, true
	case "services":
		return HomeServices, true
	}
	return HomeNone, false
}

// Owns reports whether a scope id belongs to this package. The wiring's
// [rail.ScopeSource] calls it before its own lookup, so the two dispatch
// vocabularies never have to agree about anything except the prefix.
func Owns(id string) bool {
	_, ok := ParseScopeID(id)
	return ok
}

// Blurb is the dim sentence under a home's row when the cursor rests on it —
// what this room is, in aforge's own voice. It is here rather than at the
// render site for the reason the old self page put its explainers on the
// section: this line is the only place a person learns what the room is for,
// and a sentence that lives at a render site is a sentence that gets two
// spellings.
func (h Home) Blurb() string {
	switch h {
	case HomeNotebook:
		return "what I hold true about you and this machine — corrections welcome"
	case HomeSelf:
		return "what I know how to do, and what I did while nobody was watching"
	case HomeStanding:
		return "standing promises checking on their own schedule"
	case HomeServices:
		return "processes I keep alive for you"
	}
	return ""
}

// Composer is the composer a home's SURFACE row binds when it is selected
// (5.15). Three of the four are ordinary chat: a person may always ask aforge
// about a belief, a charter or a route, and the reply lands in the home thread
// they were already speaking into.
//
// Services is [rail.ComposerNone] — 5.24's one explicit silencing, and the
// reason it is explicit is that a log tail LOOKS like a transcript. A composer
// under it would be an affordance claiming a conversation that does not exist.
// Note that this is the mode the row REQUESTS; the chat surface's own bind step
// is what enforces it, because only the wiring knows the sentence to show in
// place of the prompt (see the adoption note, 12.10).
func (h Home) Composer() rail.ComposerMode {
	if h == HomeServices {
		return rail.ComposerNone
	}
	return rail.ComposerChat
}
