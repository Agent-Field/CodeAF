package factory

// ── WHICH BRIEF REVISION EACH PERSON LAST SAW ───────────────────────────────
//
// The job page marks what changed in the Brief since the person last looked.
// That is a fact about one person and one job, kept on the item by normalised
// GitHub handle ([NormalizeHandle]) and answered only to that person: nobody
// reads what someone else has seen.
//
// IT ONLY MOVES FORWARD. Two windows (or a page whose answer arrived late) can
// report revisions out of order, and a person who saw revision 5 has seen 3.

// SeeBrief records that person saw the Brief at rev, keeping the higher of rev
// and what was already recorded. It answers whether the item changed; a blank
// person or a revision below 1 changes nothing.
func SeeBrief(it *Item, person string, rev int) bool {
	person = NormalizeHandle(person)
	if person == "" || rev < 1 || rev <= it.BriefSeen[person] {
		return false
	}
	if it.BriefSeen == nil {
		it.BriefSeen = map[string]int{}
	}
	it.BriefSeen[person] = rev
	return true
}

// BriefSeenBy is the Brief revision person last saw, 0 for none.
func BriefSeenBy(it Item, person string) int {
	return it.BriefSeen[NormalizeHandle(person)]
}
