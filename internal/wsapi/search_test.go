package wsapi

import (
	"context"
	"testing"
)

type fakeDiscoverer struct {
	lexical, embed []SearchHit
	lexErr, embErr error
	progress       IndexView
}

func (f fakeDiscoverer) SearchLexical(context.Context, string, int) ([]SearchHit, error) {
	return f.lexical, f.lexErr
}

func (f fakeDiscoverer) SearchEmbed(context.Context, string, int) ([]SearchHit, error) {
	return f.embed, f.embErr
}

func (f fakeDiscoverer) IndexProgress(context.Context) (IndexView, error) {
	return f.progress, nil
}

func TestSearchEvidenceHybridDedupesBySource(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{
		lexical: []SearchHit{{Ref: "old", SessionID: "old", Passage: "lexical", ScoreKind: ScoreBM25}},
		embed:   []SearchHit{{Ref: "old", SessionID: "old", Passage: "vector", ScoreKind: ScoreEmbed}, {Ref: "other", SessionID: "other", Passage: "also", ScoreKind: ScoreEmbed}},
	})
	hits, err := svc.SearchEvidence(ctx, SearchQuery{Query: "receipt links", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("deduped hits %+v", hits)
	}
	if hits[0].ScoreKind != ScoreEmbed || hits[0].Passage != "vector" {
		t.Fatalf("embed should replace lexical: %+v", hits[0])
	}
	if hits[1].Ref != "other" {
		t.Fatalf("second hit %+v", hits[1])
	}
}

func TestSearchEvidenceNilDiscovererIsEmpty(t *testing.T) {
	svc := testService(t, nil)
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: "anything"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("nil discoverer %+v, %v", hits, err)
	}
	view, err := svc.IndexProgress(context.Background())
	if err != nil || !view.Delayed || view.Detail != delayedDetail {
		t.Fatalf("delayed %+v, %v", view, err)
	}
}

func TestSearchEvidenceDegradedExpansion(t *testing.T) {
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{
		lexical: []SearchHit{{Ref: "lex", SessionID: "lex", Passage: "bm25", ScoreKind: ScoreBM25}},
		embed:   []SearchHit{{Ref: "exp", SessionID: "exp", Passage: "expanded", Degraded: true}},
	})
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: "receipt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[1].ScoreKind != ScoreExpansion || !hits[1].Degraded {
		t.Fatalf("expansion %+v", hits)
	}
}
