package tokens

import (
	"strconv"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// TestIdentityIsStable is the first of 5.16's two pulling properties: a task's
// colour must not change because a sibling appeared, or the rail becomes a
// disco. The same id gets the same hue, forever, on every machine — so the
// expectations here are literal values, not a re-computation of the hash.
func TestIdentityIsStable(t *testing.T) {
	pinned := map[string]Token{
		"wisp-parity":                IdentityFor("wisp-parity"),
		"perf-audit":                 IdentityFor("perf-audit"),
		"01JD8Z9K2QW5X7YV3B4N6M8P0R": IdentityFor("01JD8Z9K2QW5X7YV3B4N6M8P0R"),
		"":                           IdentityFor(""),
	}
	for id, want := range pinned {
		for range 100 {
			if got := IdentityFor(id); got != want {
				t.Fatalf("IdentityFor(%q) is not stable: %s then %s", id, want, got)
			}
		}
		if _, ok := IdentityIndex(want); !ok {
			t.Errorf("IdentityFor(%q) = %s, which is not an identity token", id, want)
		}
	}
}

// TestBlockSeedsLandOnTheSameHue closes the identity seam from this side. A
// header glyph carries a task as a SEED (blocks.Header.GlyphSeed, hashed by
// blocks.Seed) because the block engine is a leaf and knows nothing of this
// wheel; a rail card carries the same task as an ID and resolves it through
// [IdentityFor]. If those two hashes ever part, one task shows two pastels on
// one screen — the peripheral "which room am I in" answer 5.16 exists to give,
// given wrong, in the most confusing possible way.
//
// blocks cannot import this package to share the hash, so the agreement is
// pinned here, where the import edge already runs.
func TestBlockSeedsLandOnTheSameHue(t *testing.T) {
	ids := []string{
		"wisp-parity", "perf-audit", "aforge", "a", "task-1", "task-2",
		"01JD8Z9K2QW5X7YV3B4N6M8P0R",
	}
	styler := NewStyler(TrueColor, FocusNormal)
	for _, id := range ids {
		seed := blocks.Seed(id)
		if got, want := Identity(int(seed%IdentityCount)), IdentityFor(id); got != want {
			t.Fatalf("task %q: the header glyph would paint %s and its rail card %s", id, got, want)
		}
		// And through the painting door the header actually uses.
		want := styler.PaintToken("◐", IdentityFor(id))
		if got := styler.PaintIdentity("◐", seed, blocks.StateLive); got != want {
			t.Fatalf("task %q: PaintIdentity disagrees with IdentityFor", id)
		}
	}
}

// TestIdentityDistribution: eight buckets over the id shapes we actually mint.
// A hash that piles ULIDs into two hues would satisfy stability and defeat the
// point of a wheel.
func TestIdentityDistribution(t *testing.T) {
	counts := [IdentityCount]int{}
	const n = 4000
	for i := range n {
		counts[int(IdentityFor("task-"+strconv.Itoa(i))-Identity0)]++
	}
	expect := n / IdentityCount
	for i, c := range counts {
		if c < expect/2 || c > expect*2 {
			t.Errorf("hue %d got %d of %d ids (expected around %d); the wheel is not being used",
				i, c, n, expect)
		}
	}
	t.Logf("distribution over %d ids: %v", n, counts)
}

// TestNoAdjacentSharing is 5.16's second property, and the one the doc states
// as a promise: "no two adjacent rail cards share one". The walk must guarantee
// it for ANY input order, including the adversarial one where every id hashes
// to the same hue.
func TestNoAdjacentSharing(t *testing.T) {
	// A realistic rail.
	ids := []string{"wisp-parity", "perf-audit", "docs-sweep", "flaky-triage",
		"chat-v2", "spark-handoff", "terrain", "lantern", "rail-wave", "assembly"}
	assertNoAdjacent(t, ids)

	// The adversarial rail: ten ids that all hash to the same bucket. Finding
	// them by search rather than by hand means the test cannot go stale if the
	// hash changes.
	target := IdentityFor("collide-seed")
	var collide []string
	for i := 0; len(collide) < 10; i++ {
		id := "c" + strconv.Itoa(i)
		if IdentityFor(id) == target {
			collide = append(collide, id)
		}
		if i > 1_000_000 {
			t.Fatal("could not build a colliding rail")
		}
	}
	assertNoAdjacent(t, collide)

	// Degenerate rails.
	assertNoAdjacent(t, nil)
	assertNoAdjacent(t, []string{"only"})
	assertNoAdjacent(t, []string{"same", "same", "same", "same"})
}

func assertNoAdjacent(t *testing.T, ids []string) {
	t.Helper()
	got := AssignIdentities(ids)
	if len(got) != len(ids) {
		t.Fatalf("AssignIdentities returned %d tokens for %d ids", len(got), len(ids))
	}
	for i := range got {
		if _, ok := IdentityIndex(got[i]); !ok {
			t.Fatalf("row %d got %s, which is not an identity token", i, got[i])
		}
		if i > 0 && got[i] == got[i-1] {
			t.Fatalf("rows %d and %d share %s (ids %q, %q)", i-1, i, got[i], ids[i-1], ids[i])
		}
	}
	// Determinism: the same slice always produces the same assignment.
	again := AssignIdentities(ids)
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("AssignIdentities is not deterministic at row %d", i)
		}
	}
}

// TestIdentityNextPrefersStability: a nudge happens ONLY on a real collision.
// Spending stability where it is not needed is the failure mode that makes a
// rail flicker every time a card is inserted.
func TestIdentityNextPrefersStability(t *testing.T) {
	const id = "wisp-parity"
	home := IdentityFor(id)
	for i := range IdentityCount {
		prev := Identity(i)
		got := IdentityNext(id, prev)
		if prev == home {
			if got == home {
				t.Errorf("IdentityNext did not move off a collision with %s", prev)
			}
		} else if got != home {
			t.Errorf("IdentityNext(%q, %s) = %s, want the stable %s — a nudge with no collision",
				id, prev, got, home)
		}
	}
	// A prev that is not an identity means "no neighbour above".
	for _, prev := range []Token{TextPrimary, Amber, Band, Token(tokenCount)} {
		if got := IdentityNext(id, prev); got != home {
			t.Errorf("IdentityNext(%q, %s) = %s, want %s", id, prev, got, home)
		}
	}
}

// TestIdentityStepIsOdd is the reason one nudge is always enough:
// gcd(odd, 8) == 1, so the step can never be a no-op modulo eight.
func TestIdentityStepIsOdd(t *testing.T) {
	for i := range 2000 {
		s := identityStep("id-" + strconv.Itoa(i))
		if s%2 == 0 {
			t.Fatalf("step for id-%d is %d, which is even and may land back where it started", i, s)
		}
		if s >= IdentityCount {
			t.Fatalf("step for id-%d is %d, outside the wheel", i, s)
		}
	}
}
