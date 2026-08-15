package profile

import "sort"

// Straggler is the point past which one running leaf has stopped resembling
// anything this worker has ever done.
//
// It exists because of a measured incident and not a theory. One leaf of a real
// run consumed 150,000 prompt tokens — thirty-six times what its structurally
// identical siblings each spent, 31% of the whole run's bill — and it set the
// wall time of the barrier every sibling was waiting on. Nothing in the system
// reacted, because nothing in the system was comparing a leaf against what
// leaves of its kind actually cost. The budget ceiling could not: it is a
// per-task grant sized for the worst honest leaf, so a leaf thirty-six times the
// median is still comfortably inside it and stays inside it for as long as it
// takes to spend the whole grant.
//
// The threshold is derived from the profile's own spread rather than picked,
// because a constant would be wrong for every worker but one. What the spread
// says is how far this worker's work legitimately varies: identical per-city
// briefs came back at 9, 16 and 25 turns, so a factor of roughly 1.5 between the
// middle and the extreme is normal here and means nothing. The trigger is one
// more of those factors past the extreme — as far beyond the most expensive leaf
// ever recorded as that leaf is beyond the typical one. A leaf there is not at
// the top of the observed range; it is off the end of it, and the profile has no
// evidence that any such leaf has ever finished.
type Straggler struct {
	// Samples is how many measured leaves the threshold was derived from.
	Samples int
	// Anchor is the median tokens of this worker's measured leaves: the
	// typical price of one leaf of this kind.
	Anchor int
	// Extreme is the most expensive leaf ever recorded for this worker. It is
	// the top of what has actually been observed to happen, honest leaves
	// included.
	Extreme int
	// Multiple is how many anchors the threshold sits at, and it is the spread's
	// own dispersion applied twice: once to reach the observed extreme, once
	// more to clear it. It is reported so a reader can see the number was
	// derived and from what.
	Multiple float64
	// Threshold is Anchor × Multiple, in the same weighted tokens a leaf's own
	// spend is counted in.
	Threshold int
}

// Straggler derives the overrun trigger from what this worker has actually
// spent, or reports that there is not enough evidence to derive one.
//
// The evidence floor is MinSamples, the same one the ruler may not be rewritten
// below and for the same reason: a median of three leaves is not a measurement,
// and a threshold derived from one would fire on ordinary work. Below it there
// is no threshold at all rather than a guessed one — nothing fires, and the leaf
// runs exactly as it does today.
func (p *Profile) Straggler() (Straggler, bool) {
	if p == nil {
		return Straggler{}, false
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	tokens := make([]int, 0, len(p.Records))
	for _, record := range p.Records {
		// Reflex micro-leaves are a different population with a different
		// envelope, and mixing them in would drag the anchor toward work that
		// was never allowed to be large in the first place.
		if record.Size == BucketReflex || record.Tokens <= 0 {
			continue
		}
		tokens = append(tokens, record.Tokens)
	}
	if len(tokens) < MinSamples {
		return Straggler{}, false
	}
	sort.Ints(tokens)
	straggler := Straggler{
		Samples: len(tokens),
		Anchor:  tokens[len(tokens)/2],
		Extreme: tokens[len(tokens)-1],
	}
	if straggler.Anchor <= 0 {
		return Straggler{}, false
	}
	dispersion := float64(straggler.Extreme) / float64(straggler.Anchor)
	// A worker whose leaves have all cost the same has a dispersion of exactly
	// one, and one squared is one — a threshold sitting on the median, which
	// would fire on half of everything. That is the one case the spread cannot
	// speak to, so the profile declines to speak: no threshold, nothing fires.
	if dispersion <= 1 {
		return Straggler{}, false
	}
	straggler.Multiple = dispersion * dispersion
	straggler.Threshold = int(float64(straggler.Anchor) * straggler.Multiple)
	return straggler, true
}
