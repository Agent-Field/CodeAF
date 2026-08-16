package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// §13.3, pinned at the seam that decides what a gap costs.
//
// A gate read twelve verbatim technical profiles — `**Milvus** — …` through
// `**FAISS** — …`, in the order the person listed them — and returned "the
// deliverable does not contain the 12 profiles together in one final message …
// it contains a series of separate messages, each describing or pointing to
// individual profiles written to files". Not one clause of that verdict
// describes the text it was judging; every clause of it paraphrases a lesson
// distilled from an EARLIER verdict and injected above the material.
//
// The prompt now fences the deliverable so the judge can tell the material from
// the lessons by position. This test pins the half that does not depend on a
// model reading the fence: a deliverable that contains, verbatim, the things the
// review says are missing does not buy a repair round over them.
func TestADeliverableContainingWhatWasAskedForDoesNotFailForLackingIt(t *testing.T) {
	const quote = "one-paragraph technical profile of each of Milvus, Qdrant, Weaviate, Chroma and FAISS"
	deliverable := strings.Join([]string{
		"**Milvus** — a shared-storage architecture with fully disaggregated compute.",
		"**Qdrant** — a Rust engine with payload-aware HNSW filtering.",
		"**Weaviate** — GraphQL, REST and gRPC APIs over one object store.",
		"**Chroma** — an embedded store that keeps collections on local disk.",
		"**FAISS** — a library rather than a server: IVF, HNSW and PQ indexes.",
	}, "\n\n")

	if closed := revision.AdmitGapPresent(quote, deliverable); closed == "" {
		t.Fatalf("a gap naming five things the deliverable spells out verbatim was admitted; "+
			"it would buy a repair round against a correct answer\nquote: %s", quote)
	}
	// And the direction that must still convict. One of the five never arrives,
	// so the review is right and the ordinary path has to judge it.
	missing := strings.Replace(deliverable, "**Chroma**", "**Pinecone**", 1)
	if closed := revision.AdmitGapPresent(quote, missing); closed != "" {
		t.Fatalf("a gap about a genuinely absent item was refused with %q", closed)
	}
	// Prose is not an enumeration, and a rule that fired on prose would be a
	// rule that swallowed every real gap phrased in the person's own words.
	if closed := revision.AdmitGapPresent("write me a 500 word essay on latency",
		"I will research latency and write the essay next."); closed != "" {
		t.Fatalf("a prose gap was refused with %q", closed)
	}
}

// The fence itself: the material is delimited, and a deliverable that happens to
// contain the marker cannot close the fence early and push its own tail out into
// the part of the prompt the judge reads as instructions.
func TestTheDeliverableIsFencedAndCannotEscapeItsFence(t *testing.T) {
	fenced := revision.FenceDeliverable("the answer")
	lines := strings.Split(fenced, "\n")
	if len(lines) != 3 || lines[1] != "the answer" {
		t.Fatalf("the deliverable is not a fenced line of its own:\n%s", fenced)
	}
	opening, closing := lines[0], lines[2]
	if opening == "" || closing == "" || opening == closing {
		t.Fatalf("the fence has no two distinct markers:\n%s", fenced)
	}
	escaping := revision.FenceDeliverable("part one\n" + opening + "\npart two")
	if strings.Count(escaping, opening) != 1 {
		t.Fatalf("a deliverable carrying the opening marker reopened the fence:\n%s", escaping)
	}
	if !strings.Contains(escaping, "part two") {
		t.Fatalf("neutralising the marker dropped the deliverable's own text:\n%s", escaping)
	}
}

// §13.3's cost, at the other end: where a gate repair does its work.
//
// A repair of a top-level job is minted as a top-level node, because only a
// root's completion is announced. That is right and it used to mean the repair
// opened a brand new, empty directory — so the round bought to assemble twelve
// finished profiles reported "the workspace is empty" with all twelve sitting in
// the job's own folder next door. The lineage is in the id; the workspace has to
// read it.
func TestARepairWorksInTheWorkspaceOfTheJobItRepairs(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-12", Brief: "twelve profiles"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "profile twelve databases"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-12-x1", Brief: "assemble the twelve"},
		{ID: "task-12-x1-n1", Parent: "task-12-x1", Brief: "a leg of the repair"},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "profile twelve databases"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"task-12-x1", "task-12-x1-n1"} {
		node, found, err := graph.Node(id)
		if err != nil || !found {
			t.Fatalf("node %s: found=%t err=%v", id, found, err)
		}
		if got := jobIDOf(graph, node); got != "task-12" {
			t.Fatalf("%s works in %q, want the workspace of task-12 — the finished parts are there", id, got)
		}
	}
}
