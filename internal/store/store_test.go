package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRebuildMatchesMixedIncrementalWorkload(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	intent := "  Preserve these verbatim words, including their space.  "
	if err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "goal", Brief: "Deliver the resident store", Stage: 0},
		{ID: "collect", Parent: "goal", Brief: "Collect evidence", Stage: 1},
		{ID: "review", Parent: "goal", Brief: "Review risks", Stage: 2,
			Needs: []Need{{NodeID: "collect", Kind: Blocks}}},
		{ID: "draft", Parent: "goal", Brief: "Draft the result", Stage: 2,
			Needs: []Need{{NodeID: "collect", Kind: FeedsInto}, {NodeID: "review", Kind: Suggests}}},
		{ID: "publish", Parent: "goal", Brief: "Publish", Stage: 3,
			Needs: []Need{{NodeID: "review", Kind: Blocks}, {NodeID: "draft", Kind: FeedsInto}}},
	}}, Provenance{Origin: OriginUser, SessionID: "session-17", Intent: intent}); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	goal := mustClaim(t, store, "goal", "coordinator")
	if err := store.Start(goal); err != nil {
		t.Fatalf("Start goal: %v", err)
	}
	if err := store.Complete(goal, "too soon"); !errors.Is(err, ErrOpenChild) {
		t.Fatalf("Complete goal with open children = %v, want ErrOpenChild", err)
	}

	collect := mustClaim(t, store, "collect", "researcher")
	if err := store.Start(collect); err != nil {
		t.Fatalf("Start collect: %v", err)
	}
	if err := store.Complete(collect, strings.Repeat("evidence ", 700)); err != nil {
		t.Fatalf("Complete collect: %v", err)
	}
	collected, ok, err := store.Node("collect")
	if err != nil || !ok {
		t.Fatalf("Node collect = (%v, %v)", ok, err)
	}
	if len(collected.Summary) > MaxDigestBytes {
		t.Fatalf("summary has %d bytes, want at most %d", len(collected.Summary), MaxDigestBytes)
	}

	review := mustClaim(t, store, "review", "reviewer")
	if err := store.Start(review); err != nil {
		t.Fatalf("Start review: %v", err)
	}
	if err := store.Fail(review, "the upstream source disappeared"); err != nil {
		t.Fatalf("Fail review: %v", err)
	}

	firstDraft := mustClaim(t, store, "draft", "writer-old")
	if err := store.Start(firstDraft); err != nil {
		t.Fatalf("Start first draft: %v", err)
	}
	if err := store.Release(firstDraft); err != nil {
		t.Fatalf("Release first draft: %v", err)
	}
	if err := store.Fail(firstDraft, "late failure"); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("stale Fail = %v, want ErrClaimLost", err)
	}
	secondDraft := mustClaim(t, store, "draft", "writer-new")
	if secondDraft.Token <= firstDraft.Token {
		t.Fatalf("replacement token = %d, want greater than stale token %d", secondDraft.Token, firstDraft.Token)
	}
	if err := store.Complete(secondDraft, "draft artifact at cas://draft"); err != nil {
		t.Fatalf("Complete replacement draft: %v", err)
	}

	ready, err := store.Ready(0)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if got := nodeIDs(ready); !reflect.DeepEqual(got, []string{"publish"}) {
		t.Fatalf("Ready after local failure = %v, want [publish]", got)
	}
	digests, err := store.DependencyDigests("publish", MaxDigestBytes)
	if err != nil {
		t.Fatalf("DependencyDigests: %v", err)
	}
	joined := strings.Join(digests, "\n")
	if !strings.Contains(joined, "review (failed): the upstream source disappeared") {
		t.Fatalf("failure digest was not handed downstream: %q", joined)
	}
	if !strings.Contains(joined, "draft: draft artifact") {
		t.Fatalf("success digest was not handed downstream: %q", joined)
	}

	publish := mustClaim(t, store, "publish", "publisher")
	if err := store.Complete(publish, "published"); err != nil {
		t.Fatalf("Complete publish: %v", err)
	}
	if err := store.Complete(goal, "all work landed"); err != nil {
		t.Fatalf("Complete goal: %v", err)
	}
	if err := store.Fold("goal", strings.Repeat("fold ", 1000), []string{"cas://draft", "workspace://run-17", "cas://draft"}); err != nil {
		t.Fatalf("Fold: %v", err)
	}

	goalNode, ok, err := store.Node("goal")
	if err != nil || !ok {
		t.Fatalf("Node goal = (%v, %v)", ok, err)
	}
	if goalNode.Provenance.Intent != intent {
		t.Fatalf("intent = %q, want verbatim %q", goalNode.Provenance.Intent, intent)
	}
	if !goalNode.Folded || !goalNode.FoldRoot || len(goalNode.FoldDigest) > MaxDigestBytes {
		t.Fatalf("fold root = folded:%v root:%v digest:%d bytes", goalNode.Folded, goalNode.FoldRoot, len(goalNode.FoldDigest))
	}
	if len(goalNode.FoldPointers) != 3 ||
		!reflect.DeepEqual(goalNode.FoldPointers[:2], []string{"cas://draft", "workspace://run-17"}) {
		t.Fatalf("fold pointers = %v", goalNode.FoldPointers)
	}
	active, err := store.ActiveNodes()
	if err != nil {
		t.Fatalf("ActiveNodes: %v", err)
	}
	if got := nodeIDs(active); !reflect.DeepEqual(got, []string{RootID, "goal"}) {
		t.Fatalf("active nodes after fold = %v, want [%s goal]", got, RootID)
	}

	before, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot before rebuild: %v", err)
	}
	eventsBefore, err := store.Events(0, 0)
	if err != nil {
		t.Fatalf("Events before rebuild: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE events SET kind = kind WHERE seq = 1`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("updating append-only events returned %v", err)
	}
	if err := store.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	after, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after rebuild: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rebuilt view differs from incremental view\nbefore: %#v\nafter:  %#v", before, after)
	}
	eventsAfter, err := store.Events(0, 0)
	if err != nil {
		t.Fatalf("Events after rebuild: %v", err)
	}
	if !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatal("Rebuild changed the source event journal")
	}
}

func TestUnsettledFactAndTrialProvenanceSurviveRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "trial.db"))
	first, err := graph.RecordFact("", "domain:search", FactLesson,
		"breadth-first search worked for shallow dependency trees")
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.RecordFact("", "domain:search", FactLesson,
		"depth-first search worked for deeply nested dependency trees")
	if err != nil {
		t.Fatal(err)
	}
	pair := UnsettledPair{Approaches: []UnsettledApproach{
		{Approach: "breadth-first search", Scope: "shallow dependency trees", Evidence: []int64{first.Seq}},
		{Approach: "depth-first search", Scope: "deeply nested dependency trees", Evidence: []int64{second.Seq}},
	}}
	unsettled, err := graph.RecordUnsettledFact("", "domain:search", pair)
	if err != nil {
		t.Fatal(err)
	}
	if unsettled.Kind != FactUnsettled || !reflect.DeepEqual(unsettled.Unsettled, &pair) {
		t.Fatalf("recorded unsettled fact = %+v", unsettled)
	}

	provenance := Provenance{
		Origin: OriginUser, SessionID: "trial-session", Intent: "choose a search strategy", TrialOf: unsettled.Seq,
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "trial", Brief: "compare both search strategies", Stage: 1},
		{ID: "trial-result", Parent: "trial", Brief: "apply the observed winner", Stage: 2},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"trial", "trial-result"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok || node.Provenance.TrialOf != unsettled.Seq {
			t.Fatalf("node %s provenance = %+v ok=%t err=%v", id, node.Provenance, ok, err)
		}
	}
	stats, err := graph.TrialStats()
	if err != nil || stats.Fired != 1 || stats.Pending != 1 || stats.Settled != 0 || stats.Inconclusive != 0 {
		t.Fatalf("pending trial stats = %+v err=%v", stats, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, ok, err := graph.Fact(unsettled.Seq)
	if err != nil || !ok || rebuilt.Status != FactActive || !reflect.DeepEqual(rebuilt.Unsettled, &pair) {
		t.Fatalf("rebuilt unsettled fact = %+v ok=%t err=%v", rebuilt, ok, err)
	}
	node, ok, err := graph.Node("trial")
	if err != nil || !ok || node.Provenance.TrialOf != unsettled.Seq {
		t.Fatalf("rebuilt trial provenance = %+v ok=%t err=%v", node.Provenance, ok, err)
	}
	rebuiltStats, err := graph.TrialStats()
	if err != nil || !reflect.DeepEqual(rebuiltStats, stats) {
		t.Fatalf("rebuilt trial stats = %+v, want %+v (err=%v)", rebuiltStats, stats, err)
	}
}

func TestFoldSpillsOversizedDigestToCASAndRebuildKeepsPointer(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "cas-fold.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "oversized", Brief: "Preserve the full fold", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "preserve the oversized fold"}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, "oversized", "worker")
	if err := graph.Complete(claim, "complete"); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("full fold territory with evidence\n", 400)
	workspacePointer := "/workspace/result.md"
	if err := graph.Fold("oversized", full, []string{workspacePointer}); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("oversized")
	if err != nil || !ok {
		t.Fatalf("read fold: ok=%v err=%v", ok, err)
	}
	if len(node.FoldDigest) > MaxDigestBytes || len(node.FoldPointers) != 2 || node.FoldPointers[0] != workspacePointer {
		t.Fatalf("fold = digest:%d pointers:%v", len(node.FoldDigest), node.FoldPointers)
	}
	spilled, err := os.ReadFile(node.FoldPointers[1])
	if err != nil {
		t.Fatalf("read CAS spill %q: %v", node.FoldPointers[1], err)
	}
	if string(spilled) != strings.TrimSpace(full) {
		t.Fatalf("CAS spill changed full digest: got %d bytes, want %d", len(spilled), len(strings.TrimSpace(full)))
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, ok, err := graph.Node("oversized")
	if err != nil || !ok || !reflect.DeepEqual(rebuilt.FoldPointers, node.FoldPointers) {
		t.Fatalf("rebuilt CAS pointers = (%v, %v, %v), want %v", rebuilt.FoldPointers, ok, err, node.FoldPointers)
	}
}

func TestConcurrentClaimsHaveExactlyOneWinnerPerNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	first := openTestStore(t, path)
	second, err := Open(path)
	if err != nil {
		t.Fatalf("Open second handle: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const nodeCount = 12
	nodes := make([]NodeSpec, 0, nodeCount)
	for index := 0; index < nodeCount; index++ {
		id := fmt.Sprintf("job-%02d", index)
		parent := "job-00"
		if index == 0 {
			parent = ""
		}
		nodes = append(nodes, NodeSpec{ID: id, Parent: parent, Brief: "Do " + id, Stage: 1})
	}
	if err := first.Splice(RootID, Subtree{Nodes: nodes}, Provenance{Origin: OriginSelf, Intent: "claim every ready job"}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	ready, err := first.Ready(0)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != nodeCount {
		t.Fatalf("ready nodes = %d, want %d", len(ready), nodeCount)
	}

	const workers = 24
	start := make(chan struct{})
	winners := make(chan Claim, workers*nodeCount)
	errorsSeen := make(chan error, workers*nodeCount)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			handle := first
			if worker%2 == 1 {
				handle = second
			}
			for offset := 0; offset < len(ready); offset++ {
				node := ready[(worker+offset)%len(ready)]
				claim, won, err := handle.Claim(node.ID, fmt.Sprintf("worker-%02d", worker))
				if err != nil {
					errorsSeen <- err
					continue
				}
				if won {
					winners <- claim
				}
			}
		}(worker)
	}
	close(start)
	wait.Wait()
	close(winners)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("Claim returned an error under contention: %v", err)
	}

	counts := make(map[string]int, nodeCount)
	for claim := range winners {
		counts[claim.ID]++
	}
	for _, node := range ready {
		if counts[node.ID] != 1 {
			t.Errorf("node %q had %d claim winners, want exactly 1", node.ID, counts[node.ID])
		}
		stored, ok, err := first.Node(node.ID)
		if err != nil || !ok {
			t.Errorf("Node %q = (%v, %v)", node.ID, ok, err)
			continue
		}
		if stored.Status != Claimed || stored.Attempt != 1 || stored.ClaimToken != 1 {
			t.Errorf("node %q = status %s, attempt %d, token %d", node.ID, stored.Status, stored.Attempt, stored.ClaimToken)
		}
	}
	readyAfter, err := second.Ready(0)
	if err != nil {
		t.Fatalf("Ready after claims: %v", err)
	}
	if len(readyAfter) != 0 {
		t.Fatalf("Ready after claims = %v, want none", nodeIDs(readyAfter))
	}
	events, err := first.Events(0, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	claimedEvents := 0
	for _, event := range events {
		if event.Kind == EventNodeClaimed {
			claimedEvents++
		}
	}
	if claimedEvents != nodeCount {
		t.Fatalf("claim events = %d, want %d (no lost or phantom updates)", claimedEvents, nodeCount)
	}
}

func TestReopenResumesMaterializedViewWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := first.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "run", Brief: "Crash-shaped run", Stage: 0},
		{ID: "ingest", Parent: "run", Brief: "Ingest inputs", Stage: 1},
		{ID: "report", Parent: "run", Brief: "Write report", Stage: 2,
			Needs: []Need{{NodeID: "ingest", Kind: FeedsInto}}},
	}}, Provenance{Origin: OriginTrigger, Intent: "resume after an abrupt process exit"}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	run := mustClaim(t, first, "run", "coordinator")
	if err := first.Start(run); err != nil {
		t.Fatalf("Start run: %v", err)
	}
	ingest := mustClaim(t, first, "ingest", "before-crash")
	if err := first.Complete(ingest, "durably ingested"); err != nil {
		t.Fatalf("Complete ingest: %v", err)
	}
	before, err := first.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot before close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	after, err := reopened.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after reopen: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("view changed across reopen\nbefore: %#v\nafter:  %#v", before, after)
	}
	ready, err := reopened.Ready(0)
	if err != nil {
		t.Fatalf("Ready after reopen: %v", err)
	}
	if got := nodeIDs(ready); !reflect.DeepEqual(got, []string{"report"}) {
		t.Fatalf("Ready after reopen = %v, want [report]", got)
	}
	report := mustClaim(t, reopened, "report", "after-crash")
	if err := reopened.Fail(report, "reporting service unavailable"); err != nil {
		t.Fatalf("continue after reopen: %v", err)
	}
}

func TestInvalidSpliceLeavesNoPartialEventOrView(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "atomic.db"))
	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("journal_mode = %q (%v), want wal", journalMode, err)
	}
	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil || busyTimeout != 10000 {
		t.Fatalf("busy_timeout = %d (%v), want 10000", busyTimeout, err)
	}
	before, err := store.Events(0, 0)
	if err != nil {
		t.Fatalf("Events before: %v", err)
	}
	err = store.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "valid", Brief: "would be valid", Stage: 1},
		{ID: "invalid", Parent: "valid", Brief: "has a missing input", Stage: 2,
			Needs: []Need{{NodeID: "missing", Kind: Blocks}}},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "admit all or none"})
	if err == nil {
		t.Fatal("invalid splice succeeded")
	}
	after, err := store.Events(0, 0)
	if err != nil {
		t.Fatalf("Events after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("invalid splice appended %d events", len(after)-len(before))
	}
	nodes, err := store.Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if got := nodeIDs(nodes); !reflect.DeepEqual(got, []string{RootID}) {
		t.Fatalf("invalid splice left nodes %v", got)
	}
}

func TestFoldWithoutPointersRebuildsExactly(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "empty-fold.db"))
	if err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "leaf", Brief: "A completed leaf", Stage: 1},
	}}, Provenance{Origin: OriginSelf, Intent: "fold without artifacts"}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	leaf := mustClaim(t, store, "leaf", "worker")
	if err := store.Complete(leaf, "nothing to point at"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := store.Fold("leaf", "compact", nil); err != nil {
		t.Fatalf("Fold: %v", err)
	}
	before, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := store.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	after, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after rebuild: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("empty-pointer fold changed during rebuild\nbefore: %#v\nafter:  %#v", before, after)
	}

}
func TestSkillFactStatusTransitionsSurviveRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "skills.db"))
	if _, err := graph.RecordFact("", "tool:git", FactSkill, "asserted skill"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("RecordFact accepted an unevaluated active skill: %v", err)
	}

	first, err := graph.RecordSkillCandidate("", "tool:git",
		"git-audit checks a repository before delivery", "/workspace/first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.RecordSkillCandidate("", "tool:git",
		"git-audit checks a repository before delivery", "/workspace/second")
	if err != nil {
		t.Fatal(err)
	}
	if hits, err := graph.SearchFacts(FactQuery{Cues: []string{"tool:git"}, Terms: "git audit"}); err != nil || len(hits) != 0 {
		t.Fatalf("candidate leaked into retrieval: hits=%+v err=%v", hits, err)
	}

	installed := "/home/test/.aforge/skills/git-audit"
	if err := graph.ActivateSkill(first.Seq, installed); err != nil {
		t.Fatal(err)
	}
	const failure = "check.sh exited 7: fixture rejected"
	if err := graph.SupersedeFactWithReason(second.Seq, first.Seq, failure); err != nil {
		t.Fatal(err)
	}
	before, err := graph.SkillFacts("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("skill facts before rebuild = %+v", before)
	}
	bySeq := map[int64]Fact{before[0].Seq: before[0], before[1].Seq: before[1]}
	if got := bySeq[first.Seq]; got.Status != FactActive || got.Artifact != installed {
		t.Fatalf("activated skill = %+v", got)
	}
	if got := bySeq[second.Seq]; got.Status != FactSuperseded || got.StatusNote != failure {
		t.Fatalf("failed candidate = %+v", got)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, err := graph.SkillFacts("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("skill statuses changed during rebuild\nbefore: %#v\nafter:  %#v", before, after)
	}
	if hits, err := graph.SearchFacts(FactQuery{Cues: []string{"tool:git"}, Terms: "git audit"}); err != nil || len(hits) != 1 || hits[0].Seq != first.Seq {
		t.Fatalf("rebuilt active skill retrieval = %+v err=%v", hits, err)
	}
}

func TestPlaybookFactsAndSupersessionSurviveRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "playbooks.db"))
	first, err := graph.RecordFact("", "repo:parser", FactPlaybook,
		"Run go test for parser changes")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := graph.ReplaceFact(first.Seq, "", "repo:parser", FactPlaybook,
		"Run make check for parser changes; the wrapper configures generated fixtures")
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	retired, found, err := graph.FactBySeq(first.Seq)
	if err != nil || !found || retired.Kind != FactPlaybook || retired.Status != FactSuperseded ||
		retired.EvidenceSeq != replacement.Seq {
		t.Fatalf("rebuilt retired playbook = %+v found=%t err=%v", retired, found, err)
	}
	active, found, err := graph.FactBySeq(replacement.Seq)
	if err != nil || !found || active.Kind != FactPlaybook || active.Status != FactActive {
		t.Fatalf("rebuilt active playbook = %+v found=%t err=%v", active, found, err)
	}
	hits, err := graph.SearchFacts(FactQuery{
		Cues: []string{"repo:parser"}, Terms: "generated fixtures",
		Kind: FactPlaybook, PreferUseful: true, Limit: 5,
	})
	if err != nil || len(hits) != 1 || hits[0].Seq != replacement.Seq {
		t.Fatalf("rebuilt playbook retrieval = %+v err=%v", hits, err)
	}
	hasPlaybooks, err := graph.HasActiveFactKind(FactPlaybook)
	if err != nil || !hasPlaybooks {
		t.Fatalf("HasActiveFactKind(playbook) = %t, %v", hasPlaybooks, err)
	}
	eventsAfter, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatal("rebuilding playbooks changed the event journal")
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustClaim(t *testing.T, store *Store, id, owner string) Claim {
	t.Helper()
	claim, won, err := store.Claim(id, owner)
	if err != nil {
		t.Fatalf("Claim %q: %v", id, err)
	}
	if !won {
		t.Fatalf("Claim %q lost unexpectedly", id)
	}
	return claim
}

func nodeIDs(nodes []Node) []string {
	ids := make([]string, len(nodes))
	for index, node := range nodes {
		ids[index] = node.ID
	}
	return ids
}

// The TUI treats this watermark as proof that nothing happened, so it must
// move for every kind of write and stand still across a rebuild.
func TestLatestEventSeqIsTheWatermarkEveryWriteMoves(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))

	// A store is never truly empty: opening it writes the permanent spine.
	spine, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatalf("LatestEventSeq: %v", err)
	}
	if spine <= 0 {
		t.Fatalf("a freshly opened store reports watermark %d, want the spine event", spine)
	}
	if again, err := graph.LatestEventSeq(); err != nil || again != spine {
		t.Fatalf("LatestEventSeq is not stable: %d, %v", again, err)
	}

	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "leaf", Brief: "one leaf", Stage: 0},
	}}, Provenance{Origin: OriginUser, SessionID: "watermark", Intent: "watch the seq"}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	afterSplice, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatalf("LatestEventSeq: %v", err)
	}
	if afterSplice <= spine {
		t.Fatalf("splice left the watermark at %d (was %d)", afterSplice, spine)
	}

	// A projection outside the graph tables moves it too: that is what lets
	// one watermark stand for every read the thread lens performs.
	if _, err := graph.PostMessage(Message{
		SessionID: "watermark", Role: RoleUser, Body: "hello",
	}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	afterMessage, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatalf("LatestEventSeq: %v", err)
	}
	if afterMessage <= afterSplice {
		t.Fatalf("message left the watermark at %d (was %d)", afterMessage, afterSplice)
	}

	// Rebuild replays the journal into the projections; the journal itself is
	// append-only, so a rebuilt store is still the same watermark.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	afterRebuild, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatalf("LatestEventSeq: %v", err)
	}
	if afterRebuild != afterMessage {
		t.Fatalf("rebuild moved the watermark to %d (was %d)", afterRebuild, afterMessage)
	}
}

// A producer keeps its answer in the message and its working in a file — that
// is the leaf contract — and the split only works if the consumer is told
// where the file is. Artifacts are not a column: the executor's path→node map
// lives in the worker's memory and dies with it, so the paths are recovered
// from the one durable record of them, the producer's own summary. They come
// back beside the digest rather than inside it because the file list sits at
// the end of a summary, which is exactly where the byte bound bites first.
func TestDependencyInputsKeepTheProducersFilesOutOfTheByteBound(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "deps.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "merge", Brief: "merge the findings", Stage: 2,
			Needs: []Need{{NodeID: "panelist", Kind: FeedsInto}}},
		{ID: "panelist", Parent: "merge", Brief: "read the diff", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "review the branch"}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, "panelist", "worker")
	summary := "Found four defects, the worst a use-after-free in parser.c.\n\nFiles:\n" +
		"/workspace/job/01-panelist-findings.md\n/workspace/job/evidence.txt"
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}

	inputs, err := graph.DependencyInputs("merge", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs = %+v, want the one settled dependency", inputs)
	}
	if inputs[0].NodeID != "panelist" {
		t.Fatalf("producer = %q; the consumer is told this is prior work it must not redo", inputs[0].NodeID)
	}
	if len(inputs[0].Digest) > 40 {
		t.Fatalf("digest = %q, over the byte bound", inputs[0].Digest)
	}
	want := []string{"/workspace/job/01-panelist-findings.md", "/workspace/job/evidence.txt"}
	if !reflect.DeepEqual(inputs[0].Artifacts, want) {
		t.Fatalf("artifacts = %v, want %v — the bound clipped the file list out of the digest", inputs[0].Artifacts, want)
	}
	// The older shape is unchanged for everyone still reading it.
	digests, err := graph.DependencyDigests("merge", 40)
	if err != nil || len(digests) != 1 || digests[0] != inputs[0].Digest {
		t.Fatalf("digests = %v err=%v", digests, err)
	}
}

// The scale wall nobody saw: one shared pot handed out in edge order meant the
// first verbose finding could take the whole budget and every sibling after it
// vanished on a bare `continue`. The synthesis leaf of a fifty-leaf audit then
// wrote a confident report over whatever happened to be first.
func TestDependencyInputsShareTheBudgetAndSayWhatTheyClipped(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "fanin.db"))
	nodes := []NodeSpec{{ID: "merge", Brief: "merge the findings", Stage: 2}}
	for index := 0; index < 6; index++ {
		id := fmt.Sprintf("panelist-%d", index)
		nodes = append(nodes, NodeSpec{ID: id, Parent: "merge", Brief: "read a slice", Stage: 1})
		nodes[0].Needs = append(nodes[0].Needs, Need{NodeID: id, Kind: FeedsInto})
	}
	if err := graph.Splice(RootID, Subtree{Nodes: nodes}, Provenance{
		Origin: OriginUser, Intent: "audit the service",
	}); err != nil {
		t.Fatal(err)
	}
	// The first panelist is a firehose; the rest are terse. Under the old rule
	// the firehose ate everything.
	for index := 0; index < 6; index++ {
		claim := mustClaim(t, graph, fmt.Sprintf("panelist-%d", index), "worker")
		summary := fmt.Sprintf("finding %d", index)
		if index == 0 {
			summary = strings.Repeat("a use-after-free in parser.c. ", 400)
		}
		if err := graph.Complete(claim, summary); err != nil {
			t.Fatal(err)
		}
	}

	inputs, err := graph.DependencyInputs("merge", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 6 {
		t.Fatalf("inputs = %d, want every panelist carried", len(inputs))
	}
	total := 0
	for index, input := range inputs {
		total += len(input.Digest)
		if input.NodeID != fmt.Sprintf("panelist-%d", index) {
			t.Fatalf("input %d is %q", index, input.NodeID)
		}
		if strings.TrimSpace(input.Digest) == "" {
			t.Fatalf("panelist %d arrived empty", index)
		}
	}
	if total > 4096 {
		t.Fatalf("digests total %d bytes, over the pot", total)
	}
	if !strings.Contains(inputs[0].Digest, "clipped to fit") {
		t.Fatal("the clipped digest does not say it was clipped")
	}
	for _, input := range inputs[1:] {
		if strings.Contains(input.Digest, "clipped to fit") {
			t.Fatalf("a terse finding was marked clipped: %q", input.Digest)
		}
	}

	// A fan-in too wide for anyone to get a readable share takes fewer, fuller
	// inputs — and names the ones it could not carry rather than dropping them
	// into silence.
	narrow, err := graph.DependencyInputs("merge", 1200)
	if err != nil {
		t.Fatal(err)
	}
	last := narrow[len(narrow)-1]
	if last.NodeID != "" || !strings.Contains(last.Digest, "did not fit here") {
		t.Fatalf("a starved fan-in dropped its tail silently: %+v", narrow)
	}
	if !strings.Contains(last.Digest, "panelist-5") {
		t.Fatalf("the overflow notice does not name what was left out: %q", last.Digest)
	}
}
