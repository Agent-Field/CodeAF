package wsdiscover

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestPreferSessionsKeepsAMinorityAfterADuplicateCluster(t *testing.T) {
	var rows []Passage
	for i := 0; i < 80; i++ {
		id := fmt.Sprintf("a6-%02d", i)
		rows = append(rows,
			Passage{SessionID: id, Text: "mail customers the raw download", Ordinal: 1},
			Passage{SessionID: id, Text: "we abandon mailing the bare locator", Ordinal: 2},
		)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("src-%02d", i)
		rows = append(rows, Passage{SessionID: id, Text: "signed-in billed-file hyperlink", Ordinal: 1})
	}
	// Passage-level top-160 is the bdab707 composition: 80 two-turn mailers.
	if got := countPrefix(rows[:160], "src-"); got != 0 {
		t.Fatalf("fixture should hide originals in the first 160 passages, got %d", got)
	}
	got := preferSessions(rows, 160, sessionExtra)
	if src := countPrefix(got, "src-"); src != 20 {
		t.Fatalf("unique sessions should admit all 20 originals, got %d in %d hits", src, len(got))
	}
	if a6 := countPrefix(got, "a6-"); a6 < 80 {
		t.Fatalf("still need the mailer family in the pool, got %d", a6)
	}
	abandoned := 0
	for _, p := range got {
		if strings.Contains(p.Text, "abandon") {
			abandoned++
		}
	}
	if abandoned < 80 {
		t.Fatalf("extra turns must keep abandon wording for ranking, got %d", abandoned)
	}
}

func TestSearchLexicalMinoritySessionsSurviveRepeatedPassages(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	var recs []Record
	for i := 0; i < 80; i++ {
		id := fmt.Sprintf("a6-%02d", i)
		recs = append(recs,
			rec(id, 1, 1, "purchase confirmation PDF mailed to customers as a raw download address"),
			rec(id, 1, 2, "we abandon mailing the bare locator for that purchase confirmation PDF"),
		)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("src-%02d", i)
		recs = append(recs, rec(id, 1, 1, "Access to purchase-document links requires an authenticated session. Signed-in billed-file only."))
	}
	if err := s.Ingest(ctx, recs, &FakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "emailed purchase confirmation PDF", 160)
	if err != nil {
		t.Fatal(err)
	}
	if src := countPrefixPassages(hits, "src-"); src != 20 {
		t.Fatalf("lexical pool missing originals: %d/20 in %d hits ids=%v", src, len(hits), sessionIDs(hits))
	}
}

func TestSearchSimilarMinoritySessionsSurviveACloserCluster(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	emb := scriptedEmbedder{vec: clusterVec}
	var recs []Record
	for i := 0; i < 80; i++ {
		id := fmt.Sprintf("a6-%02d", i)
		recs = append(recs,
			rec(id, 1, 1, "Plan: mail customers the raw download address for billed files so nobody has to log in."),
			rec(id, 1, 2, "Understood — we abandon mailing the raw download for billed files. Do not revive it."),
		)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("src-%02d", i)
		recs = append(recs, rec(id, 1, 1, "Customers must sign in before a billed-file hyperlink will work. Authenticated session only."))
	}
	if err := s.Ingest(ctx, recs, emb); err != nil {
		t.Fatal(err)
	}
	q, _, _, dim, err := emb.Embed(ctx, []string{"emailed purchase confirmation PDF"})
	if err != nil || len(q) != 1 {
		t.Fatalf("query embed %v %v", q, err)
	}
	all, err := s.queryPassages(ctx, `SELECT `+passageColumns+` FROM passages WHERE vector IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	raw := scoreSimilar(all, q[0], "scripted", "test", dim)
	if len(raw) < 160 {
		t.Fatalf("want ≥160 scored passages, got %d", len(raw))
	}
	if src := countPrefix(raw[:160], "src-"); src != 0 {
		t.Fatalf("bdab707 passage cap should hide originals in top-160, got %d", src)
	}
	hits, err := s.SearchSimilar(ctx, q[0], "scripted", "test", dim, 160)
	if err != nil {
		t.Fatal(err)
	}
	if src := countPrefixPassages(hits, "src-"); src != 20 {
		t.Fatalf("embed pool missing originals: %d/20 in %d hits ids=%v", src, len(hits), sessionIDs(hits))
	}
}

type scriptedEmbedder struct {
	vec func(string) []float32
}

func (s scriptedEmbedder) Available(context.Context) (string, bool, error) {
	return "scripted", true, nil
}

func (s scriptedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, string, string, int, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = s.vec(text)
	}
	return out, "scripted", "test", 4, nil
}

func clusterVec(text string) []float32 {
	t := strings.ToLower(text)
	v := []float32{0, 0, 0, 0}
	mark4(v, 0, t, "emailed", "purchase", "confirmation", "pdf", "receipt")
	mark4(v, 1, t, "emailed", "mail customers", "raw download", "abandon")
	mark4(v, 2, t, "sign in", "signed-in", "authenticated", "billed", "hyperlink", "purchase", "pdf", "emailed", "receipt")
	return unit4(v)
}

func mark4(v []float32, dim int, text string, needles ...string) {
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

func countPrefix(rows []Passage, prefix string) int {
	n := 0
	seen := map[string]struct{}{}
	for _, p := range rows {
		if !strings.HasPrefix(p.SessionID, prefix) {
			continue
		}
		if _, ok := seen[p.SessionID]; ok {
			continue
		}
		seen[p.SessionID] = struct{}{}
		n++
	}
	return n
}

func countPrefixPassages(rows []Passage, prefix string) int {
	return countPrefix(rows, prefix)
}

func sessionIDs(rows []Passage) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, p := range rows {
		if _, ok := seen[p.SessionID]; ok {
			continue
		}
		seen[p.SessionID] = struct{}{}
		out = append(out, p.SessionID)
	}
	return out
}
