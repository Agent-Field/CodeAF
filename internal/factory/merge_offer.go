package factory

import (
	"errors"
	"time"
)

// ── THE MANAGER OFFERS THE MERGE AGAIN ──────────────────────────────────────
//
// The Merge card is where the job waits for the person (the owner's decision
// of 2026-11-16), and when the person asks the job's manager to merge, or
// whether it is ready to, the manager shows them that card again, as it stands
// now, rather than telling them to go and find a button. The offer is a stamp
// on the job (MergeOfferedAt) that the job page reads to bring the card up;
// the merge itself stays the person's press on that card, through the same
// gate ([MergeGate]).

// OfferMerge stamps the job's merge offer with now, when the job page offers
// Merge on its open pull request ([MergeOffered]) and no merge is already on
// its way. Otherwise it refuses with why, in the page's own words, and
// changes nothing.
func OfferMerge(it *Item, now time.Time) error {
	if m := it.Merging; m != nil && (m.State == MergeAsked || m.State == MergeQueued || m.State == MergeMerging) {
		return errors.New("a merge of this pull request is already on its way")
	}
	if v := MergeOffered(*it); !v.OK {
		return errors.New("merge is not offered: " + v.Why)
	}
	it.MergeOfferedAt = now.UTC().Truncate(time.Second)
	return nil
}
