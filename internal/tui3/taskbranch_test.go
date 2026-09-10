package tui3

// WHERE THE WORK STARTS FROM, said before it runs (docs/CHAT-V3.md, Decision
// 26): a node over a repository gets a worktree cut from HEAD, so the edits a
// person has not committed are not in it — which is a surprise worth spending on
// the proposal instead of on the merge.

import (
	"strings"
	"testing"
	"time"
)

// The card names the branch point when there is one to name.
func TestAProposalOverARepositoryNamesItsBranchPoint(t *testing.T) {
	a, _, _ := taskApp(t)
	a.branch = "work"
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)})

	text := taskText(a)
	if !strings.Contains(text, taskBranchPointWord) {
		t.Fatalf("the proposal does not say where the work starts from:\n%s", text)
	}
	// It is one line and it is said once — not on the head, not again under the
	// expansion.
	if got := strings.Count(text, taskBranchPointWord); got != 1 {
		t.Fatalf("the branch point is on %d rows, want one:\n%s", got, text)
	}
}

// AND SAYS NOTHING WHEN THERE IS NOT. A workspace that is no repository runs the
// work in the person's own directory, where nothing is hidden from it and there
// is no branch point to name; an unknown renders as NOTHING rather than as a
// sentence that is not true here.
func TestAProposalWithNoRepositoryUnderItNamesNoBranchPoint(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)})

	if text := taskText(a); strings.Contains(text, "HEAD") {
		t.Fatalf("a proposal outside a repository talked about HEAD:\n%s", text)
	}
}

// A card that has been answered keeps its head and its verdict and drops
// everything else, the branch point with it: a question that has been answered
// is a fact, and the fact is what was decided.
func TestASettledProposalDropsTheBranchPoint(t *testing.T) {
	a, agent, _ := taskApp(t)
	a.branch = "work"
	agent.pending = []uint64{7}
	askTask(t, a, 7, 4*time.Second)
	drive(t, a, key("enter"))

	if text := taskText(a); strings.Contains(text, taskBranchPointWord) {
		t.Fatalf("a settled card is still naming its branch point:\n%s", text)
	}
}
