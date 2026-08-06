package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFactInjectionOutcomesAreDistinctAndRebuildSafe(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "exercise outcome attribution", Stage: 0},
		{ID: "good", Parent: "job", Brief: "finish cleanly", Stage: 1},
		{ID: "failed", Parent: "job", Brief: "fail", Stage: 1},
		{ID: "gated", Parent: "job", Brief: "miss the delivery gate", Stage: 1},
		{ID: "over", Parent: "job", Brief: "run out of room", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "test fact outcomes"}); err != nil {
		t.Fatal(err)
	}
	suspect, err := graph.RecordFact("", "repo:test", FactLesson, "always take the risky shortcut")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := graph.RecordFact("", "repo:test", FactLesson, "verify the result")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordFactInjection("good", []int64{suspect.Seq, clean.Seq, suspect.Seq}); err != nil {
		t.Fatal(err)
	}
	// A second context on the same node does not turn one ride into two.
	if err := graph.RecordFactInjection("good", []int64{suspect.Seq}); err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []string{"failed", "gated", "over"} {
		if err := graph.RecordFactInjection(nodeID, []int64{suspect.Seq}); err != nil {
			t.Fatal(err)
		}
	}

	good := mustClaim(t, graph, "good", "worker")
	if err := graph.Complete(good, "done"); err != nil {
		t.Fatal(err)
	}
	failed := mustClaim(t, graph, "failed", "worker")
	if err := graph.Fail(failed, "broken"); err != nil {
		t.Fatal(err)
	}
	gated := mustClaim(t, graph, "gated", "worker")
	if err := graph.RecordDeliveryGate("gated", DeliveryGate{Pass: false, Gap: "missing proof"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(gated, "delivered without proof"); err != nil {
		t.Fatal(err)
	}
	over := mustClaim(t, graph, "over", "worker")
	if err := graph.Complete(over, "partial"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice("job", Subtree{Nodes: []NodeSpec{{
		ID: "over-x1-finish", Brief: "finish the remainder", Stage: 2,
	}}}, Provenance{Origin: OriginSelf, Intent: "re-expand over"}); err != nil {
		t.Fatal(err)
	}

	assertOutcomes := func() {
		t.Helper()
		outcomes, err := graph.FactOutcomes()
		if err != nil {
			t.Fatal(err)
		}
		if got := outcomes[suspect.Seq]; got.Rides != 4 || got.Bad != 3 || got.LatestBadSeq == 0 {
			t.Fatalf("suspect outcomes = %+v, want 4 rides and 3 bad", got)
		}
		if got := outcomes[clean.Seq]; got.Rides != 1 || got.Bad != 0 || got.LatestBadSeq != 0 {
			t.Fatalf("clean outcomes = %+v, want one clean ride", got)
		}
	}
	assertOutcomes()

	eventsBefore, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var foundBatch bool
	for _, event := range eventsBefore {
		if event.Kind != EventFactInjected || event.NodeID != "good" {
			continue
		}
		var payload factInjectionPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(payload.FactSeqs, []int64{suspect.Seq, clean.Seq}) {
			foundBatch = true
		}
	}
	if !foundBatch {
		t.Fatal("batched fact injection event was not recorded")
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertOutcomes()
	eventsAfter, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatal("rebuild changed fact injection events")
	}
}

func TestFactQuarantineExcludesRetrievalAndCanBeRehabilitated(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	fact, err := graph.RecordFact("", "tool:git", FactQuirk, "git always destroys worktrees")
	if err != nil {
		t.Fatal(err)
	}
	query := FactQuery{Cues: []string{"tool:git"}, Terms: "destroys worktrees", Limit: 5}
	if found, err := graph.SearchFacts(query); err != nil || len(found) != 1 {
		t.Fatalf("initial search = %+v err=%v", found, err)
	}
	if err := graph.QuarantineFact(fact.Seq, fact.Seq, FactOriginUser); err != nil {
		t.Fatal(err)
	}
	assertFactAbsentFromRetrieval(t, graph, query)

	quarantined, found, err := graph.FactBySeq(fact.Seq)
	if err != nil || !found {
		t.Fatalf("quarantined fact = %+v found=%t err=%v", quarantined, found, err)
	}
	if quarantined.Status != FactQuarantined || quarantined.EvidenceSeq != fact.Seq ||
		quarantined.StatusOrigin != FactOriginUser || quarantined.StatusSeq == 0 {
		t.Fatalf("quarantine metadata = %+v", quarantined)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertFactAbsentFromRetrieval(t, graph, query)
	rebuilt, found, err := graph.FactBySeq(fact.Seq)
	if err != nil || !found || rebuilt.Status != FactQuarantined || rebuilt.EvidenceSeq != fact.Seq {
		t.Fatalf("rebuilt quarantine = %+v found=%t err=%v", rebuilt, found, err)
	}

	if err := graph.RestoreFact(fact.Seq, FactOriginCLI); err != nil {
		t.Fatal(err)
	}
	if found, err := graph.SearchFacts(query); err != nil || len(found) != 1 || found[0].Seq != fact.Seq {
		t.Fatalf("restored search = %+v err=%v", found, err)
	}
	if err := graph.QuarantineFact(fact.Seq, 0, FactOriginCLI); err != nil {
		t.Fatal(err)
	}
	replacement, err := graph.RecordFact("", fact.Scope, fact.Kind, fact.Body)
	if err != nil {
		t.Fatal(err)
	}
	old, found, err := graph.FactBySeq(fact.Seq)
	if err != nil || !found || old.Status != FactSuperseded || old.EvidenceSeq != replacement.Seq {
		t.Fatalf("rehabilitated old fact = %+v found=%t err=%v", old, found, err)
	}
	if found, err := graph.SearchFacts(query); err != nil || len(found) != 1 || found[0].Seq != replacement.Seq {
		t.Fatalf("rehabilitated search = %+v err=%v", found, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	old, found, err = graph.FactBySeq(fact.Seq)
	if err != nil || !found || old.Status != FactSuperseded {
		t.Fatalf("rebuilt rehabilitation = %+v found=%t err=%v", old, found, err)
	}
}

func TestOpenMigratesPreQuarantineFactViewByJournalReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordFact("", "repo:migrate", FactPlain, "journal survives schema migration")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		DROP TABLE facts_fts;
		ALTER TABLE facts RENAME TO facts_current;
		CREATE TABLE facts (
			seq INTEGER PRIMARY KEY REFERENCES events(seq),
			ts TEXT NOT NULL,
			node_id TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'user',
			kind TEXT NOT NULL DEFAULT 'fact' CHECK (kind IN ('preference', 'quirk', 'lesson', 'fact')),
			body TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
			uses INTEGER NOT NULL DEFAULT 0,
			last_used TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO facts (seq, ts, node_id, scope, kind, body, status, uses, last_used)
		SELECT seq, ts, node_id, scope, kind, body, status, uses, last_used FROM facts_current;
		DROP TABLE facts_current;
	`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	migrated, found, err := reopened.FactBySeq(fact.Seq)
	if err != nil || !found || migrated.Status != FactActive || migrated.StatusSeq != fact.Seq {
		t.Fatalf("migrated fact = %+v found=%t err=%v", migrated, found, err)
	}
	if err := reopened.QuarantineFact(fact.Seq, 0, FactOriginCLI); err != nil {
		t.Fatalf("new quarantine status rejected after migration: %v", err)
	}
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	migrated, found, err = reopened.FactBySeq(fact.Seq)
	if err != nil || !found || migrated.Status != FactQuarantined {
		t.Fatalf("rebuilt migrated fact = %+v found=%t err=%v", migrated, found, err)
	}
}

func assertFactAbsentFromRetrieval(t *testing.T, graph *Store, query FactQuery) {
	t.Helper()
	if found, err := graph.SearchFacts(query); err != nil || len(found) != 0 {
		t.Fatalf("search returned quarantined facts: %+v err=%v", found, err)
	}
	if active, err := graph.ActiveFacts("tool:git", 5); err != nil || len(active) != 0 {
		t.Fatalf("active facts returned quarantine: %+v err=%v", active, err)
	}
	if recent, err := graph.RecentFacts(5); err != nil || len(recent) != 0 {
		t.Fatalf("recent facts returned quarantine: %+v err=%v", recent, err)
	}
}
