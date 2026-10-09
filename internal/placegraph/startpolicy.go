package placegraph

// WHAT A CHAT STARTED IN A PLACE WOULD BE DECIDED (Home's model caption).
//
// Home shows which model the first message will run on BEFORE a conversation
// exists, so there is no chat id to resolve. This answers the same question
// [Snapshot.Resolve] answers, for a chat that has been filed in one place and
// nothing else, by running the SAME policy rule over a context list built as if
// that filing had happened.
//
// IT IS PURE. The filing is imagined on a private copy of the snapshot's
// membership list; nothing is written, no source is stat'ed, no model is called,
// and the snapshot it was handed is left exactly as it was. A new chat has no
// remembered picks, so a disagreement no place above can settle stays
// PolicyNeedsPick, which is the truth about that chat's first turn.

// startChatID names the imagined chat. It is not a valid chat id on purpose, so
// it can never collide with a real membership row.
const startChatID = "\x00start"

// StartPolicy returns the decision for field f of a brand-new chat filed in
// placeID, and false when no place in its context has an opinion on f (or the
// place is unknown or archived, so a chat could not be filed there).
func (s *Snapshot) StartPolicy(placeID string, f PolicyField) (PolicyDecision, bool) {
	if !s.active(placeID) {
		return PolicyDecision{}, false
	}
	imagined := *s
	// A full-slice expression forces append to copy, so the real list is never written.
	imagined.Memberships = append(s.Memberships[:len(s.Memberships):len(s.Memberships)],
		Membership{ChatID: startChatID, PlaceID: placeID, AddedBy: AddedByYou})
	for _, d := range imagined.resolvePolicy(startChatID, imagined.ContextPlaces(startChatID), nil) {
		if d.Field == f {
			return d, true
		}
	}
	return PolicyDecision{}, false
}
