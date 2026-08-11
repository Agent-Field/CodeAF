package head

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestSettledWorkOpensTheLoopWithNothingRunning is #3. The belt's result and read
// tools exist for settled work — the belt's own comment says "settled work is
// precisely what has findings" — and they lived behind a gate that asked whether
// anything was still running. Overnight jobs land, the terminal goes quiet, and
// "what did the market analysis conclude?" found no live jobs, so the loop
// declined and the router answered a question about substance from a 1200-byte
// slice while the reader that would have opened the job in full never ran.
func TestSettledWorkOpensTheLoopWithNothingRunning(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "market-analysis", "Market analysis",
		"analyse the competitive landscape for the storage market")
	completeNodeWith(t, graph, "market-analysis",
		"Three incumbents hold 71% of the market and none of them price below $40/TB.")

	head := New(nil, graph)
	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("the fixture left work live, so the gate is untested: %+v", active)
	}

	// There is no trigger any more, so there is nothing left to be shut out BY.
	// What is asserted instead is the property the trigger was standing in for:
	// with a quiet board, a question about settled work still reaches a prompt
	// that says where the findings are, and the tools that read them are in it.
	prompt, err := head.turnPrompt(store.Message{
		SessionID: "quiet", Body: "what did the market analysis conclude?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, emptyBoardLine) {
		t.Fatalf("an empty board did not say that aimed reads still reach settled work:\n%s", prompt)
	}
	if !strings.Contains(prompt, "market analysis") {
		t.Fatalf("the message never reached the loop:\n%s", prompt)
	}

	// And a message about nothing on the graph carries no readings at all, so an
	// ordinary sentence pays nothing for machinery it did not use.
	for _, idle := range []string{"good morning", "thanks!"} {
		quiet, err := head.turnPrompt(store.Message{SessionID: "quiet", Body: idle})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(quiet, hintHeader) {
			t.Fatalf("%q produced deterministic readings out of a coincidence:\n%s", idle, quiet)
		}
	}
}

// The other half of the same reachability: an aimed board read reaches the
// settled job, so the model can learn the id that result and read both require.
func TestAnAimedBoardReadReachesSettledWork(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "market-analysis", "Market analysis", "analyse the storage market")
	completeNodeWith(t, graph, "market-analysis", "Three incumbents hold 71% of the market.")

	head := New(nil, graph)
	aimed, err := head.boardRows("quiet", "market analysis", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(aimed) == 0 || aimed[0].node.ID != "market-analysis" {
		t.Fatalf("an aimed read could not find the settled job: %+v", aimed)
	}
	// The unqueried board is still about what is moving; a settled job on it
	// would turn an enumeration of the workforce into a log.
	open, err := head.boardRows("quiet", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range open {
		if row.node.ID == "market-analysis" {
			t.Fatal("settled work leaked onto the unqueried board")
		}
	}
}

// TestFoldedJobStaysFindableAndReadable is the live transcript that proved the
// gap ran deeper than the report. An architecture review settled, distilled and
// FOLDED; the user asked about it a day later in a fresh session; the head said
// nothing in the graph or notebook matched — honestly, because the relevance
// search skipped every folded node, fold roots included. That is the state a
// job a person asks about the next day is always in.
//
// A fold root durably carries the digest and the pointers to what it wrote, so
// all three reads have to land: the search finds it, the result quotes the
// digest rather than the stale summary, and read opens the file it recorded.
func TestFoldedJobStaysFindableAndReadable(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	assessment := filepath.Join(workspace, "architecture-review.md")
	const verdict = "The contributor's plan authenticates every write path; no injection surface remains."
	if err := os.WriteFile(assessment, []byte(verdict+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spliceJobTree(t, graph, store.OriginUser, "",
		spec("plan-check", "", "Architecture plan validity check",
			"check whether the contributor's architecture plan is secure"),
		spec("plan-check-a", "plan-check", "Read the plan", "read the proposed plan"))
	completeNodeWith(t, graph, "plan-check-a", "The plan adds three write paths.")
	completeNodeWith(t, graph, "plan-check", "An early note, written before the review finished.")

	const digest = "Reviewed the contributor's architecture plan: it is secure, with every write path authenticated."
	if err := graph.Fold("plan-check", digest, []string{assessment}); err != nil {
		t.Fatalf("fold: %v", err)
	}
	node, found, err := graph.Node("plan-check")
	if err != nil || !found || !node.Folded || !node.FoldRoot {
		t.Fatalf("the fixture is not a folded fold root: %+v found=%t err=%v", node, found, err)
	}

	// One: relevance finds it. This is what returned nothing at all.
	reference := redirectReference("is the contributor's architecture plan secure?")
	targets, err := graph.SearchSurgeryTargets(reference, false)
	if err != nil {
		t.Fatal(err)
	}
	anchored := false
	for _, target := range targets {
		anchored = anchored || (target.Node.ID == "plan-check" && target.Score >= RedirectAnchorScore)
	}
	if !anchored {
		t.Fatalf("a folded job is invisible to the search that finds settled work: %+v", targets)
	}

	// Two: everything built on that search now reaches it.
	head := New(nil, graph)
	deep, opened := head.renderDeep("is the contributor's architecture plan secure?", "")
	if !opened["plan-check"] || !strings.Contains(deep, digest) {
		t.Fatalf("the deep slice never opened the folded job:\n%s", deep)
	}
	// And it quotes the fold, not the summary the fold superseded.
	if strings.Contains(deep, "An early note") {
		t.Fatalf("the deep slice quoted the stale pre-fold summary:\n%s", deep)
	}
	// Three: read opens the file the fold pointed at, which is where the answer is.
	run := &beltRun{head: head, user: store.Message{SessionID: "next-day", Body: "is it secure?"}}
	whole, failed := run.result(map[string]any{"id": "plan-check"})
	if failed || !strings.Contains(whole, digest) {
		t.Fatalf("result on a folded job failed=%t: %s", failed, whole)
	}
	opened2, failed := run.read(map[string]any{"job": "plan-check", "file": "architecture-review.md"})
	if failed || !strings.Contains(opened2, verdict) {
		t.Fatalf("read could not open the folded job's recorded file failed=%t: %s", failed, opened2)
	}
	if run.acted {
		t.Fatal("reading a folded job recorded an action")
	}
}

// The fold's members stay behind its root, which is what folding is for. Only
// the root is search-eligible, and only for a read: no verb gains a route to
// settled work it never had.
func TestFoldedMembersStayBehindTheirRootAndNoVerbReachesEither(t *testing.T) {
	graph := openHeadStore(t)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("packed", "", "Ledger reconciliation", "reconcile the quarterly ledger"),
		spec("packed-a", "packed", "Ledger reconciliation part one", "reconcile the quarterly ledger opening balances"))
	completeNodeWith(t, graph, "packed-a", "Opening balances agree.")
	completeNodeWith(t, graph, "packed", "The ledger reconciles.")
	if err := graph.Fold("packed", "The quarterly ledger reconciles to the cent.", nil); err != nil {
		t.Fatal(err)
	}

	targets, err := graph.SearchSurgeryTargets("ledger reconciliation quarterly", false)
	if err != nil {
		t.Fatal(err)
	}
	sawRoot := false
	for _, target := range targets {
		if target.Node.ID == "packed-a" {
			t.Fatal("a folded member was offered beside the root that speaks for it")
		}
		sawRoot = sawRoot || target.Node.ID == "packed"
	}
	if !sawRoot {
		t.Fatalf("the fold root fell out of the read: %+v", targets)
	}

	// Every verb supplies the statuses it may legally touch, and settled work is
	// in none of them — which is why the eligibility rides on their absence.
	for _, allowed := range [][]store.Status{
		{store.Pending, store.Claimed, store.Running},
		{store.Failed, store.Cancelled},
	} {
		acting, err := graph.SearchSurgeryTargets("ledger reconciliation quarterly", false, allowed...)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range acting {
			if target.Node.ID == "packed" {
				t.Fatalf("a verb reached a folded job through the read's widening: %v", allowed)
			}
		}
	}
}

// TestFoldedPartsAnswerWhatEachPartConcluded pins the other half of the folded
// filter. resultChildren skipped every folded child, so "what did each part
// conclude" about a tidied-away job returned nothing — status instead of
// substance, from the read written to end exactly that.
func TestFoldedPartsAnswerWhatEachPartConcluded(t *testing.T) {
	graph := openHeadStore(t)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("survey", "", "Vendor survey", "survey the vendors"),
		spec("survey-a", "survey", "Pricing", "compare vendor pricing"))
	completeNodeWith(t, graph, "survey-a", "Vendor B is 30% cheaper at volume.")
	if err := graph.Fold("survey-a", "Vendor B is 30% cheaper at volume.", nil); err != nil {
		t.Fatal(err)
	}
	completeNodeWith(t, graph, "survey", "Two vendors are viable.")

	children := New(nil, graph).resultChildren("survey")
	joined := strings.Join(children, "\n")
	if !strings.Contains(joined, "survey-a") || !strings.Contains(joined, "30% cheaper") {
		t.Fatalf("a folded part vanished from its parent's account of itself:\n%s", joined)
	}
}

// A settled job asked about in a fresh session must reach the router's prompt as
// substance rather than a status word, end to end.
func TestSettledQuestionCarriesSubstanceEndToEnd(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	prompt := routerPrompt(t, graph, "next-day", "what happened with the finance thing")
	if !strings.Contains(prompt, financeFinding) {
		t.Fatalf("the finance finding never reached the prompt:\n%s", prompt)
	}
}
