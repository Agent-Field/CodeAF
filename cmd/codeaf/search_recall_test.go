package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
)

// topicEmbedWire is a test embedder that puts same-topic different-wording
// passages near each other. Production never binds it.
type topicEmbedWire struct{ calls int }

func (s *topicEmbedWire) Embed(_ context.Context, request provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	s.calls++
	data := make([]provider.Embedding, len(request.Input))
	for i, text := range request.Input {
		data[i] = provider.Embedding{Index: i, Embedding: topicVec(text)}
	}
	model := request.Model
	if model == "" {
		model = "openai/text-embedding-3-small"
	}
	return &provider.EmbeddingResponse{Model: model, Data: data}, nil
}

func topicVec(text string) []float32 {
	t := strings.ToLower(text)
	v := []float32{0, 0, 0, 0, 0}
	mark(v, 0, t, "billed", "sign", "login", "logged", "locator", "session", "fetch", "hyperlink", "authentic")
	mark(v, 1, t, "emailed", "purchase", "confirmation", "pdf")
	mark(v, 2, t, "other one", "the other")
	mark(v, 3, t, "certificate", "wildcard", "terraform")
	return unit4(v)
}

func mark(v []float32, dim int, text string, needles ...string) {
	for _, n := range needles {
		if strings.Contains(text, n) {
			v[dim] = 1
			return
		}
	}
}

func unit4(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		v[len(v)-1] = 1
		return v
	}
	s := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= s
	}
	return v
}

func TestSQLiteSearchEvidenceFindsOriginalsDespiteDifferentWording(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	wire := &topicEmbedWire{}
	client := embed.New(wire, "openai/text-embedding-3-small", nil)
	svc, adapter := openV3FolderServiceWith(client)
	if svc == nil || adapter == nil {
		t.Fatal("Open must bind both stores")
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	var recs []wsdiscover.Record
	for i := 1; i <= 20; i++ {
		recs = append(recs, passage(fmt.Sprintf("src-%02d", i),
			fmt.Sprintf("Customers must sign in before a billed-file hyperlink will work. Authenticated session only. (policy thread %d)", i)))
		recs = append(recs, passage(fmt.Sprintf("para-%02d", i),
			fmt.Sprintf("We email people a download for their purchase confirmation PDF. Who is allowed to fetch it? (billing ask %d)", i)))
		recs = append(recs, passage(fmt.Sprintf("a7-%02d", i),
			fmt.Sprintf("No, the other one. Switching to the signed-in session requirement. The bare locator is not adopted. (%d)", i)))
		recs = append(recs, passage(fmt.Sprintf("glob-%02d", i),
			fmt.Sprintf("Main work is rotating wildcard certificate %d and terraform state locks. Side note: billed-file hyperlinks still require a signed-in session.", i)))
		recs = append(recs, passage(fmt.Sprintf("neg-%02d", i),
			fmt.Sprintf("I bought espresso machine %d and need the kitchen warranty paper slip.", i)))
	}
	if err := adapter.store.Ingest(ctx, recs, client); err != nil {
		t.Fatal(err)
	}
	assertFamily(t, svc, "emailed purchase confirmation PDF who may fetch it", "src-", 14, 20)
	assertFamily(t, svc, "No the other one signed-in session not bare locator", "a7-", 12, 20)
	assertFamily(t, svc, "billed-file hyperlinks require signed-in session buried in certificate work", "glob-", 10, 20)
}

func assertFamily(t *testing.T, svc *wsapi.Service, query, prefix string, want, limit int) {
	t.Helper()
	hits, err := svc.SearchEvidence(context.Background(), wsapi.SearchQuery{Query: query, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	gold := 0
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.SessionID)
		if strings.HasPrefix(hit.SessionID, prefix) {
			gold++
		}
	}
	if gold < want {
		t.Fatalf("%q gold %s in top-%d: %d (want ≥%d) ids=%v", query, prefix, limit, gold, want, ids)
	}
}

func TestWrapSearchWithEvidenceUsesHybridHits(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	wire := &topicEmbedWire{}
	client := embed.New(wire, "openai/text-embedding-3-small", nil)
	folders := openV3FoldersWith(client)
	if folders == nil {
		t.Fatal("folders")
	}
	wrapped := folders.(*sessionFolders)
	t.Cleanup(func() { _ = wrapped.Close() })
	ctx := context.Background()
	if err := wrapped.disc.store.Ingest(ctx, []wsdiscover.Record{
		passage("src-01", "Customers must sign in before a billed-file hyperlink will work. Authenticated session only."),
		passage("para-01", "We email people a download for their purchase confirmation PDF. Who is allowed to fetch it?"),
	}, client); err != nil {
		t.Fatal(err)
	}
	inner := graphOnlySearch{}
	store := wrapSearchWithEvidence(inner, folders)
	hits, err := store.SearchConversations("emailed purchase confirmation PDF who may fetch it", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid search place returned nothing")
	}
	found := false
	for _, hit := range hits {
		if hit.SessionID == "src-01" {
			found = true
		}
		if hit.SessionID == "graph-only" {
			t.Fatal("fell back to graph.db while discovery had hits")
		}
	}
	if !found {
		t.Fatalf("search place missed the original: %+v", hits)
	}
}

type graphOnlySearch struct{}

func (graphOnlySearch) SearchConversations(string, int) ([]store.ConversationHit, error) {
	return []store.ConversationHit{{
		MessageHit: store.MessageHit{SessionID: "graph-only", Body: "graph lexical only"},
	}}, nil
}
