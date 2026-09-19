package wsdiscover

import (
	"context"
	"testing"
)

func TestFtsMatchQueryORsTermsAndDropsShortNoise(t *testing.T) {
	got := ftsMatchQuery(`No the other one signed-in session not bare locator`)
	want := `other OR one OR signed OR session OR not OR bare OR locator`
	if got != want {
		t.Fatalf("fts query %q, want %q", got, want)
	}
}

func TestLexicalOrMatchesAPartialTurn(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{
		rec("a7", 1, 1, "Use the emailed-bare-locator approach for billed files."),
		rec("a7", 1, 2, "No, the other one."),
		rec("a4", 1, 1, "Customers must sign in before a billed-file hyperlink will work."),
	}, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "No the other one signed-in session not bare locator", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range hits {
		if hit.SessionID == "a7" && hit.Ordinal == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("OR lexical missed the short correction: %+v", hits)
	}
}

func TestLexicalRanksTheDenserMatchFirst(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Ingest(ctx, []Record{
		rec("long", 1, 1, "A kitchen warranty paper slip mentions the other drawer once among espresso parts."),
		rec("short", 1, 1, "No, the other one."),
	}, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "the other one", 5)
	if err != nil || len(hits) < 2 {
		t.Fatalf("lexical %+v %v", hits, err)
	}
	if hits[0].SessionID != "short" {
		t.Fatalf("BM25 should prefer the short correction, got %+v", hits)
	}
}
