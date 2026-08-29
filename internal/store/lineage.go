package store

import (
	"fmt"
	"strings"
)

// A belief's LINEAGE is every wording of it the notebook has ever held: the one
// standing now, and every one a later wording replaced. The active view is what
// retrieval reads and is deliberately narrow — a superseded line is not evidence
// and must never come back as advice — but there is one question the active view
// cannot answer, and it is the question "have we learned this already".
//
// It matters because supersession is how the notebook stays small, and a writer
// that only checks what is ACTIVE reads a five-deep chain of the same lesson as
// one belief and writes the sixth. That is exactly what happened: one lesson
// about what a delivered message must contain was learned five separate times,
// each row superseding the last, each derived from the same failure. The chain
// is the evidence that the lesson was never the problem — and only a reader that
// can see the chain can notice.

// FactLineage returns the beliefs recorded in one scope, newest first,
// INCLUDING the ones a later wording superseded. It is the read a writer makes
// before deciding whether it has anything new to say; nothing that retrieves
// facts for use may read it, for the reason the active view exists.
func (s *Store) FactLineage(scope string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 200
	}
	return s.factsWhere(
		`scope = ? AND status IN (?, ?) ORDER BY seq DESC LIMIT ?`,
		scope, FactActive, FactSuperseded, limit)
}

// LineageNodes returns every node of one job's lineage, oldest first: the node
// itself and everything spliced beneath its id, including the repair rounds
// that continue it under "<id>-x<n>" and their children.
//
// It is an id-range read rather than a graph walk for exactly the reason
// DeliveryGateLineage is: THE LINEAGE IS AN ID NAMESPACE, and a reader that
// rebuilt it from parents and edges would own a second copy of the "-x" law and
// would get a different answer the first time the two drifted. A repair round
// is not a child of the node it repairs — it is spliced beside it, under the
// root, so that its result is announced like any other deliverable — so a
// parent walk finds none of them.
//
// The namespace test is the node itself plus SplitNamespace and nothing else: a
// bare prefix range would also swallow "jobless" for "job", which would let one
// job's record bound another's.
func (s *Store) LineageNodes(baseID string) ([]Node, error) {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		return nil, nil
	}
	namespace := baseID + SplitNamespace
	ceiling, ok := idPrefixCeiling(namespace)
	if !ok {
		return nil, fmt.Errorf("read lineage %q: %w: prefix has no ordered ceiling", baseID, ErrInvalid)
	}
	// The ordering is queryNodes' own — created_seq, created_order, id — which
	// is the graph's admission order and therefore the order the job actually
	// happened in. Spelling a second ORDER BY here appends one clause to
	// another and produces SQL that does not parse.
	return s.queryNodes(`WHERE id = ? OR (id >= ? AND id < ?)`, []any{baseID, namespace, ceiling})
}
