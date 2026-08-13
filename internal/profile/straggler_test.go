package profile

import (
	"fmt"
	"testing"
)

func measured(tokens ...int) *Profile {
	profile := &Profile{Model: "m", Subharness: "linear"}
	for index, count := range tokens {
		profile.Records = append(profile.Records, Record{
			Title: fmt.Sprintf("leaf %d", index), Size: "atomic", Turns: 8, Tokens: count,
		})
	}
	return profile
}

// Nothing is derived from nothing. Below the evidence gate there is no
// threshold rather than a guessed one, because a threshold guessed from three
// leaves would fire on ordinary work and the mechanism would be discredited by
// its first week.
func TestNoThresholdIsDerivedBelowTheEvidenceGate(t *testing.T) {
	for _, count := range []int{0, 1, MinSamples - 1} {
		tokens := make([]int, count)
		for index := range tokens {
			tokens[index] = 40_000 + index
		}
		if _, ok := measured(tokens...).Straggler(); ok {
			t.Fatalf("%d samples produced a threshold; want none until %d", count, MinSamples)
		}
	}
	if _, ok := (*Profile)(nil).Straggler(); ok {
		t.Fatal("a nil profile produced a threshold")
	}
}

// The multiple is the spread's, not a constant. Two workers whose medians are
// identical and whose spreads differ must get different thresholds, or the
// number is a magic constant wearing a derivation.
func TestTheMultipleComesFromTheSpreadAndNotFromAConstant(t *testing.T) {
	tight := measured(38_000, 39_000, 40_000, 40_000, 40_000, 42_000, 43_000, 44_000)
	wide := measured(10_000, 20_000, 30_000, 40_000, 40_000, 60_000, 90_000, 160_000)

	tightStraggler, ok := tight.Straggler()
	if !ok {
		t.Fatal("a tight but non-degenerate spread produced no threshold")
	}
	wideStraggler, ok := wide.Straggler()
	if !ok {
		t.Fatal("a wide spread produced no threshold")
	}
	if tightStraggler.Anchor != wideStraggler.Anchor {
		t.Fatalf("the two fixtures no longer share a median: %d vs %d",
			tightStraggler.Anchor, wideStraggler.Anchor)
	}
	if !(wideStraggler.Multiple > tightStraggler.Multiple) {
		t.Fatalf("the wider spread did not buy a wider tolerance: %.2f vs %.2f",
			wideStraggler.Multiple, tightStraggler.Multiple)
	}
	// And the derivation is stated rather than merely applied: the threshold is
	// the anchor times the multiple, and the multiple reaches one dispersion
	// past the observed extreme.
	for _, straggler := range []Straggler{tightStraggler, wideStraggler} {
		if want := int(float64(straggler.Anchor) * straggler.Multiple); straggler.Threshold != want {
			t.Fatalf("threshold %d is not anchor × multiple (%d)", straggler.Threshold, want)
		}
		if straggler.Threshold <= straggler.Extreme {
			t.Fatalf("threshold %d does not clear the observed extreme %d",
				straggler.Threshold, straggler.Extreme)
		}
	}
}

// A worker whose every leaf cost the same has a spread that says nothing about
// tolerance, and a threshold on the median would fire on half of everything.
// The profile declines rather than inventing a margin.
func TestADegenerateSpreadDeclinesToProduceAThreshold(t *testing.T) {
	flat := measured(40_000, 40_000, 40_000, 40_000, 40_000, 40_000, 40_000, 40_000)
	if straggler, ok := flat.Straggler(); ok {
		t.Fatalf("a flat spread produced a threshold: %+v", straggler)
	}
}

// The incident this exists for, as a test. Eight structurally identical
// siblings at about 4,000 tokens each and one leaf at 150,000: the straggler
// must be past the threshold and the siblings must not be anywhere near it.
func TestTheMeasuredIncidentFiresAndItsSiblingsDoNot(t *testing.T) {
	siblings := measured(3_900, 4_000, 4_100, 4_200, 4_200, 4_400, 4_800, 6_100)
	straggler, ok := siblings.Straggler()
	if !ok {
		t.Fatal("eight measured siblings produced no threshold")
	}
	if 150_000 < straggler.Threshold {
		t.Fatalf("the 150,000-token straggler sits under the %d threshold", straggler.Threshold)
	}
	for _, sibling := range []int{3_900, 4_200, 6_100} {
		if sibling >= straggler.Threshold {
			t.Fatalf("an ordinary sibling at %d crosses the %d threshold",
				sibling, straggler.Threshold)
		}
	}
	if straggler.Samples != 8 {
		t.Fatalf("samples = %d, want the eight it was derived from", straggler.Samples)
	}
}

// Reflex micro-leaves are a different population with an envelope that was
// never allowed to be large. Letting them into the anchor would drag it toward
// work that could not have been big, and every ordinary leaf would look like a
// straggler.
func TestReflexRecordsDoNotSetTheAnchor(t *testing.T) {
	profile := measured(38_000, 40_000, 41_000, 42_000, 44_000, 48_000, 61_000, 90_000)
	ordinary, ok := profile.Straggler()
	if !ok {
		t.Fatal("the ordinary fixture produced no threshold")
	}
	for index := 0; index < 20; index++ {
		profile.Records = append(profile.Records, Record{
			Title: "reflex", Size: BucketReflex, Turns: 1, Tokens: 900,
		})
	}
	withReflex, ok := profile.Straggler()
	if !ok {
		t.Fatal("adding reflex records removed the threshold")
	}
	if withReflex.Anchor != ordinary.Anchor || withReflex.Threshold != ordinary.Threshold {
		t.Fatalf("reflex records moved the anchor: %+v became %+v", ordinary, withReflex)
	}
}
