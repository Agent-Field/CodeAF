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
