// Package stream files one chat at a time into a tree that already exists,
// and offers a new place only when Now has gathered enough chats that belong
// together. It implements placeorg.Filer and placeorg.Guesser.
//
// A new chat never starts a place. It is filed, parked, or left in Now. The
// one time a model is asked is a cross-branch contest: the chat fits two
// places in different parts of the library about equally, and the model
// picks among the places it is shown. When it does not answer, the chat
// stays with the broader of those two places.
package stream

import "github.com/Agent-Field/codeaf/internal/placeorg"

// The park figures the reference measured for the primary space. A run writes
// its own into State.Thresholds; these stand in only when that row is absent,
// so a filing before the first calibration still uses the rule the tree was
// tuned with rather than a zero that would file everything.
const (
	defaultFloor  float32 = 0.45
	defaultMargin float32 = 0.04
)

// NeighbourK and NeighbourFloor are the kNN graph a birth is read off. They
// are the same figures the neighbour stage keeps: ten neighbours, and no edge
// below 0.35. A birth is one connected group in that graph, not a new cut.
const (
	NeighbourK             = 10
	NeighbourFloor float32 = 0.35
)

// contestPlaces is how many places a contest shows. Five is the whole menu;
// the model cannot add a place that is not on it.
const contestPlaces = 5

// continuationBoost is the prior for the place the person opened within the
// last hour. It is small on purpose: it tips a close call and cannot outweigh
// a clearly better place, and it never overrides the folder rule.
const continuationBoost float32 = 0.05

// Stream is the filer and the guesser. Zero values use the primary space and
// the default park figures. Embed, Folders, Kinds and Representative are set
// by the engine; nil means that prior is absent, not that it failed.
type Stream struct {
	Space placeorg.SpaceID
	// Thresholds is the park rule for Space, used when a chat is filed.
	// Local is the same rule for the live guess's space. A zero Floor
	// means "use Thresholds", and a zero there means the primary figures.
	Thresholds placeorg.Thresholds
	Local      placeorg.Thresholds
	// Kinds and Representative drop probes and duplicates out of a birth.
	// A chat with no row is a real conversation.
	Kinds          map[string]placeorg.Kind
	Representative map[string]string
	// Embed scores a draft for the live guess. Nil means there is no guess.
	Embed placeorg.Embedder
	// Folders is the folder rule already resolved: a workspace path to the
	// place id. A hit is final, on a filing and on a guess.
	Folders map[string]string
}

// New returns a filer with no priors set. The engine fills the fields it has.
func New() *Stream { return &Stream{} }

var (
	_ placeorg.Filer   = (*Stream)(nil)
	_ placeorg.Guesser = (*Stream)(nil)
	_ placeorg.Judged  = (*Stream)(nil)
)

// Fallbacks states the one deterministic answer this stage takes when the
// model does not answer. There is one judgment point, so there is one row.
func (*Stream) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{{
		When: "a new chat fits two places in different parts of the library about equally",
		Does: "keep the chat at the broader of the two places",
	}}
}

func (s *Stream) space() placeorg.SpaceID {
	if s == nil || s.Space == "" {
		return placeorg.SpacePrimary
	}
	return s.Space
}

func (s *Stream) thresholds() placeorg.Thresholds {
	var th placeorg.Thresholds
	if s != nil {
		th = s.Thresholds
	}
	if th.Floor == 0 {
		th.Floor = defaultFloor
	}
	if th.Margin == 0 {
		th.Margin = defaultMargin
	}
	return th
}

// guessFloor is the live guess's floor. The guess lives in the local space,
// whose calibrated floor is not the primary space's, so it has its own row.
func (s *Stream) guessFloor() float32 {
	if s != nil && s.Local.Floor != 0 {
		return s.Local.Floor
	}
	return s.thresholds().Floor
}

// stage is the journal name on every op this package proposes.
const stage = "stream"
