package lane

// ── THE FRONTIER IS THE CANDIDATE SET ───────────────────────────────────────
//
// With seventeen lanes on one model, most of them are beaten outright: some
// other lane starts sooner AND writes faster AND costs less AND is more often
// right. A lane like that can never be the answer to any request, whatever λ
// is, so it should never be sampled, never probed and never hedged to. Pruning
// them first is what makes the exploration budget land only where it could
// change a decision, and it usually leaves three to five.
//
// THE COMPARISON IS AT THE p75, NOT THE MEAN. A lane nobody has measured much
// has a wide posterior, and at the p75 that width keeps it in the set: it might
// be good. Pruning on the mean would quietly make this a router that only ever
// uses what it already knows.
//
// This file computes nothing yet. Lane L-B fills it in, and it may not read a
// clock — see the note in choose.go.

// paretoFront is the subset of candidates that no other candidate beats on
// every axis at once. It returns them in the order they were given.
//
// It answers with nothing while lane L-B has not built it, and nothing is the
// honest answer: an unpruned set returned from here would be a claim that every
// lane is worth choosing between, which is exactly the claim this step exists
// to deny.
func paretoFront([]Scored) []Scored { return nil }
