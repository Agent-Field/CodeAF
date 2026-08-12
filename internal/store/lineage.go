package store

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
