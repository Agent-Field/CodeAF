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
	v := []float32{0, 0, 0, 0}
	// The TUI string is closer to an abandoned mailer (mail a file) than to
	// the signed-in original, and still closer to that original than to an
	// A7 correction. Unique-by-session admits the original; ranking still
	// needs the original nearer than the correction family.
	mark(v, 0, t, "emailed", "purchase", "confirmation", "pdf", "receipt", "dinner slip", "restaurant paper")
	mark(v, 1, t, "emailed", "mail customers", "raw download", "abandon")
	mark(v, 2, t, "sign in", "signed-in", "authenticated", "billed", "logged-in", "hyperlink", "login", "logged", "fetch", "purchase", "pdf", "emailed", "receipt")
	mark(v, 3, t, "other one", "the other", "bare locator", "certificate", "wildcard", "terraform")
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
			fmt.Sprintf("We keep emailed receipt links and a purchase confirmation PDF. Who is allowed to fetch them? (billing ask %d)", i)))
		recs = append(recs, passage(fmt.Sprintf("a6-%02d", i),
			fmt.Sprintf("Plan: mail customers the raw download address for billed files. We abandon mailing the bare locator. The abandoned mailer stays rejected. (%d)", i)))
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
	assertFamily(t, svc, "emailed purchase confirmation PDF", "src-", 14, 20)
	assertFamily(t, svc, "emailed receipt links", "src-", 14, 20)
	assertFamily(t, svc, "No the other one signed-in session not bare locator", "a7-", 12, 20)
	assertFamily(t, svc, "billed-file hyperlinks require signed-in session buried in certificate work", "glob-", 10, 20)
}

func TestSQLiteSearchEvidenceOriginalsEnterWhenAbandonedMailersAreTheMajority(t *testing.T) {
	// bdab707 gathered 160 passages. Eighty two-turn abandoned mailers
	// fill that page; demoting "abandon" cannot recover A4 if src- never
	// entered SearchLexical/SearchEmbed. Gold is still the signed-in original.
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
			fmt.Sprintf("We keep emailed receipt links and a purchase confirmation PDF. Who is allowed to fetch them? (billing ask %d)", i)))
	}
	for i := 1; i <= 80; i++ {
		id := fmt.Sprintf("a6-%02d", i)
		recs = append(recs, wsdiscover.Record{
			SessionID: id, SourceRef: "chat:" + id, Speaker: "user", Generation: 1, Ordinal: 1,
			Text: fmt.Sprintf("Plan: mail customers the raw download address for billed files so nobody has to log in. (%d)", i),
		})
		recs = append(recs, wsdiscover.Record{
			SessionID: id, SourceRef: "chat:" + id, Speaker: "assistant", Generation: 1, Ordinal: 2,
			Text: fmt.Sprintf("Understood — we abandon mailing the raw download for billed files. The abandoned mailer stays rejected. (%d)", i),
		})
	}
	if err := adapter.store.Ingest(ctx, recs, client); err != nil {
		t.Fatal(err)
	}
	query := "emailed purchase confirmation PDF"
	lex, err := adapter.SearchLexical(ctx, query, 160)
	if err != nil {
		t.Fatal(err)
	}
	embHits, err := adapter.SearchEmbed(ctx, query, 160)
	if err != nil {
		t.Fatal(err)
	}
	if countHitPrefix(lex, "src-")+countHitPrefix(embHits, "src-") < 14 {
		t.Fatalf("originals must enter SearchLexical/SearchEmbed: lex=%v emb=%v", idsOfHits(lex), idsOfHits(embHits))
	}
	hits, err := svc.SearchEvidence(ctx, wsapi.SearchQuery{Query: query, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	gold, abandoned, para := 0, 0, 0
	for _, hit := range hits {
		switch {
		case strings.HasPrefix(hit.SessionID, "src-"):
			gold++
		case strings.HasPrefix(hit.SessionID, "a6-"):
			abandoned++
		case strings.HasPrefix(hit.SessionID, "para-"):
			para++
		}
	}
	if gold < 14 || abandoned > 0 || para > 0 {
		t.Fatalf("A4 originals in top-20: %d (want ≥14); abandoned=%d para=%d ids=%v", gold, abandoned, para, idsOfHits(hits))
	}
}

func TestSQLiteSearchEvidenceAccessPolicyBeatsCafeOCRHardNegatives(t *testing.T) {
	// Live 10k on f1423514 ranked 40 Cafe dinner slip OCR chats above the
	// signed-in originals for "emailed purchase confirmation PDF". The
	// production path must beat that family at the same size, without a
	// cafe/restaurant denylist. Isolated CODEAF_HOME only.
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
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("src-%02d", i)
		recs = append(recs, passage(id, fmt.Sprintf(
			"Access to purchase-document links requires an authenticated session. Sending the bare locator in email is rejected. (policy thread %d)", i)))
		recs = append(recs, wsdiscover.Record{
			SessionID: id, SourceRef: "chat:" + id, Speaker: "assistant", Generation: 1, Ordinal: 2,
			Text: fmt.Sprintf("Recorded: signed-in fetch only; mailing the raw address stays refused. Evidence stays in this Security thread (%d).", i),
		})
	}
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("cafe-%02d", i)
		recs = append(recs, passage(id, fmt.Sprintf(
			"OCR cafe dinner slip %d into the outing spreadsheet. Restaurant paper, not a billed-file hyperlink policy.", i)))
		recs = append(recs, wsdiscover.Record{
			SessionID: id, SourceRef: "chat:" + id, Speaker: "assistant", Generation: 1, Ordinal: 2,
			Text: "Logged the outing total. No access-control change.",
		})
	}
	for i := 1; i <= 20; i++ {
		recs = append(recs, passage(fmt.Sprintf("a6-%02d", i),
			fmt.Sprintf("Plan: mail customers the raw download address for billed files. We abandon mailing the bare locator. The abandoned mailer stays rejected. (%d)", i)))
		recs = append(recs, passage(fmt.Sprintf("para-%02d", i),
			fmt.Sprintf("We keep emailed receipt links and a purchase confirmation PDF. Who is allowed to fetch them? (billing ask %d)", i)))
	}
	if err := adapter.store.Ingest(ctx, recs, client); err != nil {
		t.Fatal(err)
	}
	query := "emailed purchase confirmation PDF"
	lex, err := adapter.SearchLexical(ctx, query, 160)
	if err != nil {
		t.Fatal(err)
	}
	embHits, err := adapter.SearchEmbed(ctx, query, 160)
	if err != nil {
		t.Fatal(err)
	}
	if countHitPrefix(embHits, "cafe-") < 14 {
		t.Fatalf("cafe OCR must enter the pool so ranking can lose to it: lex=%v emb=%v", idsOfHits(lex), idsOfHits(embHits))
	}
	if countHitPrefix(lex, "src-")+countHitPrefix(embHits, "src-") < 14 {
		t.Fatalf("originals must enter SearchLexical/SearchEmbed: lex=%v emb=%v", idsOfHits(lex), idsOfHits(embHits))
	}
	hits, err := svc.SearchEvidence(ctx, wsapi.SearchQuery{Query: query, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	gold, cafe, abandoned, para := 0, 0, 0, 0
	for _, hit := range hits {
		switch {
		case strings.HasPrefix(hit.SessionID, "src-"):
			gold++
		case strings.HasPrefix(hit.SessionID, "cafe-"):
			cafe++
		case strings.HasPrefix(hit.SessionID, "a6-"):
			abandoned++
		case strings.HasPrefix(hit.SessionID, "para-"):
			para++
		}
	}
	if gold < 14 || cafe > 0 || abandoned > 0 || para > 0 {
		t.Fatalf("A4 originals in top-20: %d (want ≥14); cafe=%d abandoned=%d para=%d ids=%v", gold, cafe, abandoned, para, idsOfHits(hits))
	}
}

func countHitPrefix(hits []wsapi.SearchHit, prefix string) int {
	n := 0
	seen := map[string]struct{}{}
	for _, hit := range hits {
		if !strings.HasPrefix(hit.SessionID, prefix) {
			continue
		}
		if _, ok := seen[hit.SessionID]; ok {
			continue
		}
		seen[hit.SessionID] = struct{}{}
		n++
	}
	return n
}

func idsOfHits(hits []wsapi.SearchHit) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if _, ok := seen[hit.SessionID]; ok {
			continue
		}
		seen[hit.SessionID] = struct{}{}
		out = append(out, hit.SessionID)
	}
	return out
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
		passage("para-01", "We keep emailed receipt links and a purchase confirmation PDF. Who is allowed to fetch them?"),
	}, client); err != nil {
		t.Fatal(err)
	}
	inner := graphOnlySearch{}
	store := wrapSearchWithEvidence(inner, folders)
	for _, query := range []string{"emailed purchase confirmation PDF", "emailed receipt links"} {
		hits, err := store.SearchConversations(query, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 {
			t.Fatalf("%q: hybrid search place returned nothing", query)
		}
		found := false
		for _, hit := range hits {
			if hit.SessionID == "src-01" {
				found = true
			}
			if hit.SessionID == "graph-only" {
				t.Fatalf("%q: fell back to graph.db while discovery had hits", query)
			}
		}
		if !found {
			t.Fatalf("%q: search place missed the original: %+v", query, hits)
		}
	}
}

type graphOnlySearch struct{}

func (graphOnlySearch) SearchConversations(string, int) ([]store.ConversationHit, error) {
	return []store.ConversationHit{{
		MessageHit: store.MessageHit{SessionID: "graph-only", Body: "graph lexical only"},
	}}, nil
}
