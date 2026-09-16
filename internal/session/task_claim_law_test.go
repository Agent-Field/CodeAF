package session

import "testing"

// The two doors that claim a landed node's working copy refuse each other
// (#1077's review round 1): a merge round and a settle are both work in the
// node's own working copy, and neither may start over the other.
func TestTheClaimDoorsRefuseEachOther(t *testing.T) {
	_, node := unverifiedNode(t, nil)

	if err := node.claimSettle("your accept"); err != nil {
		t.Fatalf("a settle could not claim a free node: %v", err)
	}
	if node.claimResolving() {
		t.Fatal("a round started over a settle in flight")
	}
	node.releaseSettle()
	if !node.claimResolving() {
		t.Fatal("a round could not claim a free node")
	}
	if err := node.claimSettle("a re-audit"); err == nil {
		t.Fatal("a settle started over a round in flight")
	}
	node.releaseResolving()
	if err := node.claimSettle("a re-audit"); err != nil {
		t.Fatalf("a settle could not claim after the round handed back: %v", err)
	}
}

// The notice names whichever claim is in flight, in the claim's own words —
// the settle's plain words, the round's two.
func TestTheNoticeNamesTheResolutionInFlight(t *testing.T) {
	_, node := unverifiedNode(t, nil)

	if err := node.claimSettle("your accept"); err != nil {
		t.Fatalf("a settle could not claim a free node: %v", err)
	}
	if got := node.notice().Settling; got != "your accept" {
		t.Fatalf("the notice names %q, want the settle's own words", got)
	}
	node.releaseSettle()
	if got := node.notice().Settling; got != "" {
		t.Fatalf("a released settle still names %q", got)
	}

	if !node.claimResolving() {
		t.Fatal("the round could not claim a free node")
	}
	if got := node.notice().Settling; got != "a merge round" {
		t.Fatalf("the notice names %q, want the round's two words", got)
	}
	node.releaseResolving()
	if got := node.notice().Settling; got != "" {
		t.Fatalf("a released round still names %q", got)
	}
}
