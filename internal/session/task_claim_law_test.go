package session

import "testing"

// The two doors that claim a landed node's working copy refuse each other
// (#1077's review round 1): a merge round and a settle are both work in the
// node's own working copy, and neither may start over the other.
func TestTheClaimDoorsRefuseEachOther(t *testing.T) {
	_, node := unverifiedNode(t, nil)

	settle, err := node.claimSettle("your accept")
	if err != nil {
		t.Fatalf("a settle could not claim a free node: %v", err)
	}
	if _, ok := node.claimResolving(); ok {
		t.Fatal("a round started over a settle in flight")
	}
	node.releaseSettle(settle)

	if _, ok := node.claimResolving(); !ok {
		t.Fatal("a round could not claim a free node")
	}
	if _, err := node.claimSettle("a re-audit"); err == nil {
		t.Fatal("a settle started over a round in flight")
	}
}

// The notice names whichever claim is in flight, in the claim's own words —
// the settle's plain words, the round's two.
func TestTheNoticeNamesTheResolutionInFlight(t *testing.T) {
	_, node := unverifiedNode(t, nil)

	settle, err := node.claimSettle("your accept")
	if err != nil {
		t.Fatalf("a settle could not claim a free node: %v", err)
	}
	if got := node.notice().Settling; got != "your accept" {
		t.Fatalf("the notice names %q, want the settle's own words", got)
	}
	node.releaseSettle(settle)
	if got := node.notice().Settling; got != "" {
		t.Fatalf("a released settle still names %q", got)
	}

	round, ok := node.claimResolving()
	if !ok {
		t.Fatal("the round could not claim a free node")
	}
	if got := node.notice().Settling; got != "a merge round" {
		t.Fatalf("the notice names %q, want the round's two words", got)
	}
	node.releaseResolving(round)
	if got := node.notice().Settling; got != "" {
		t.Fatalf("a released round still names %q", got)
	}
}

// A RELEASE OWNS ONLY ITS OWN GENERATION (#1077's Opus review): two accepts
// say the same words, so a release that matched words would wipe a second
// window's claim mid-flight. The settle's own terminal clear (resettle) opens
// the door to a second claim, and the first settle's deferred release must
// leave that second claim standing.
func TestAReleaseOwnsOnlyItsOwnGeneration(t *testing.T) {
	_, node := unverifiedNode(t, nil)

	first, err := node.claimSettle("your accept")
	if err != nil {
		t.Fatalf("the first accept could not claim: %v", err)
	}
	node.graph.resettle(node, TaskUnverified) // the first settle lands and hands back
	second, err := node.claimSettle("your accept") // a second window's accept
	if err != nil {
		t.Fatalf("the second accept could not claim after the resettle: %v", err)
	}
	node.releaseSettle(first) // the first settle's deferred release, late
	if got := node.notice().Settling; got != "your accept" {
		t.Fatalf("the first settle's release wiped the second window's claim: %q", got)
	}
	node.releaseSettle(second)
	if got := node.notice().Settling; got != "" {
		t.Fatalf("the second settle's release did not hand its own claim back: %q", got)
	}

	// THE SAME LAW FOR THE ROUND: a round that released early and emitted
	// re-raises the card, and a second round pressed on that card owns the
	// flag.
	round1, ok := node.claimResolving()
	if !ok {
		t.Fatal("the first round could not claim")
	}
	node.releaseResolving(round1) // the failed-check road: release, then emit
	round2, ok := node.claimResolving()
	if !ok {
		t.Fatal("the second round could not claim after the first handed back")
	}
	node.releaseResolving(round1) // the first round's deferred release, late
	if got := node.notice().Settling; got != "a merge round" {
		t.Fatalf("the first round's release wiped the second round's guard: %q", got)
	}
	node.releaseResolving(round2)
	if got := node.notice().Settling; got != "" {
		t.Fatalf("the second round's release did not hand its own guard back: %q", got)
	}
}
