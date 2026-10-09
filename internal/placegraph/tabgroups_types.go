package placegraph

// Types for a tab-group offer.
//
// THESE ARE NOT PLACE PROPOSALS. A tab group is a suggestion drawn on the
// strip. Accepting it groups tabs in that window. It does not create a place,
// file a chat, or write the place graph. The real-places recommender keeps
// recommend.go and places_advice.go; this file is the offer's own contract.

// TabChat is one open chat as the engine's own record states it. Title and
// Recap are copied from that chat's meta.json. Workspace is the recorded
// tools root, which is the input to a repo identity and is never shown to
// the model as a path to guess from. A caller does not supply these fields.
type TabChat struct {
	ID        string
	Title     string
	Recap     string
	Workspace string
}

// TabOffer is one suggestion. Basis is "repo" or "topic". IDs are canonical
// chat ids the engine resolved, in the order they were considered. Title is
// a proper name or empty; an empty title is drawn as a question with no name.
type TabOffer struct {
	Basis string
	Key   string
	IDs   []string
	Title string
}
