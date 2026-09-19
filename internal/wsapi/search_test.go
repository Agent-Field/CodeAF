package wsapi

import (
	"context"
	"strings"
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

func TestSearchEvidenceDifferentWordingOriginalsBeatParaphraseEchoes(t *testing.T) {
	// J09/J18 A4: production queries have no who/may/can/should/allowed.
	// Hits that restate the ask are not the original passages. Gold is the
	// authenticated-access family, not paraphrase ids and not abandoned plans.
	queries := []string{"emailed purchase confirmation PDF", "emailed receipt links"}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			var lexical, embed []SearchHit
			for i := 0; i < 20; i++ {
				id := "para-" + itoa(i)
				passage := query + " billing ask"
				lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreBM25})
				embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
			}
			for i := 0; i < 20; i++ {
				id := "src-" + itoa(i)
				passage := "Customers must sign in before a billed-file hyperlink will work. Authenticated session only."
				embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
			}
			for i := 0; i < 20; i++ {
				id := "a6-" + itoa(i)
				passage := "Plan: mail customers the raw download address. We abandon mailing the bare locator. The abandoned mailer stays rejected."
				embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
			}
			svc := testService(t, nil)
			svc.SetDiscoverer(fakeDiscoverer{lexical: lexical, embed: embed})
			hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: query, Limit: 20})
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
				t.Fatalf("A4 originals in top-20: %d (want ≥14); abandoned=%d para=%d ids=%v", gold, abandoned, para, idsOf(hits))
			}
		})
	}
}

func TestSearchEvidenceAccessPolicyBeatsCafeOCRHardNegatives(t *testing.T) {
	// f1423514 filled A4 top-20 with Cafe dinner slip OCR: those chats
	// mention billed-file only to refuse it, and original-neighbor ranking
	// treated the refusal as the standing decision. Gold is the access policy.
	// Forty cafe distractors plus forty sources is the live family size, not
	// a three-chat toy. Cafe/restaurant are not banned words.
	query := "emailed purchase confirmation PDF"
	var embed []SearchHit
	for i := 0; i < 40; i++ {
		id := "cafe-" + itoa(i)
		passage := "OCR cafe dinner slip " + itoa(i) + " into the outing spreadsheet. Restaurant paper, not a billed-file hyperlink policy."
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	for i := 0; i < 40; i++ {
		id := "src-" + itoa(i)
		passage := "Access to purchase-document links requires an authenticated session. Sending the bare locator in email is rejected."
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	for i := 0; i < 20; i++ {
		id := "a6-" + itoa(i)
		passage := "Plan: mail customers the raw download address. We abandon mailing the bare locator. The abandoned mailer stays rejected."
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{embed: embed})
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: query, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	gold, cafe, abandoned := 0, 0, 0
	for _, hit := range hits {
		switch {
		case strings.HasPrefix(hit.SessionID, "src-"):
			gold++
		case strings.HasPrefix(hit.SessionID, "cafe-"):
			cafe++
		case strings.HasPrefix(hit.SessionID, "a6-"):
			abandoned++
		}
	}
	if gold < 14 || cafe > 0 || abandoned > 0 {
		t.Fatalf("A4 originals in top-20: %d (want ≥14); cafe=%d abandoned=%d ids=%v", gold, cafe, abandoned, idsOf(hits))
	}
}

func TestAccessPolicyDecisionDoesNotDenylistCafe(t *testing.T) {
	policy := []string{"Customers must sign in before a billed-file hyperlink will work. OCR cafe dinner slip is filed separately."}
	if !accessPolicyDecision(policy) {
		t.Fatal("mentioning a cafe dinner slip must not strip an access-policy decision")
	}
	refusal := []string{"OCR cafe dinner slip 1 into the outing spreadsheet. Restaurant paper, not a billed-file hyperlink policy."}
	if accessPolicyDecision(refusal) {
		t.Fatal("a billed-file refusal mention is not the access-policy decision")
	}
}

func TestSearchEvidenceShortCorrectionCoversTheAskAcrossTurns(t *testing.T) {
	query := "No the other one signed-in session not bare locator"
	var lexical, embed []SearchHit
	for i := 0; i < 20; i++ {
		id := "a7-" + itoa(i)
		lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: "No, the other one.", ScoreKind: ScoreBM25})
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: "Switching to the signed-in session requirement. The bare locator is not adopted.", ScoreKind: ScoreEmbed})
	}
	for i := 0; i < 20; i++ {
		id := "src-" + itoa(i)
		passage := "Customers must sign in. A billed-file hyperlink needs a signed-in session. The bare locator stays refused."
		lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreBM25})
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{lexical: lexical, embed: embed})
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: query, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	gold := 0
	for _, hit := range hits {
		if strings.HasPrefix(hit.SessionID, "a7-") {
			gold++
		}
	}
	if gold < 12 {
		t.Fatalf("A7 corrections in top-20: %d (want ≥12): %+v", gold, idsOf(hits))
	}
}

func TestSearchEvidenceMinorityTopicBeatsTheTopicalMajority(t *testing.T) {
	query := "billed-file hyperlinks require signed-in session buried in certificate work"
	var lexical, embed []SearchHit
	for i := 0; i < 20; i++ {
		id := "glob-" + itoa(i)
		passage := "Main work is rotating wildcard certificate and terraform state locks. Side note: billed-file hyperlinks still require a signed-in session."
		lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreBM25})
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	for i := 0; i < 20; i++ {
		id := "src-" + itoa(i)
		passage := "Customers must sign in before a billed-file hyperlink will work. Signed-in session only."
		lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreBM25})
		embed = append(embed, SearchHit{Ref: id, SessionID: id, Passage: passage, ScoreKind: ScoreEmbed})
	}
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{lexical: lexical, embed: embed})
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: query, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	gold := 0
	for _, hit := range hits {
		if strings.HasPrefix(hit.SessionID, "glob-") {
			gold++
		}
	}
	if gold < 10 {
		t.Fatalf("global minority in top-20: %d (want ≥10): %+v", gold, idsOf(hits))
	}
}

func TestSearchEvidenceTruncatesToLimit(t *testing.T) {
	var lexical []SearchHit
	for i := 0; i < 50; i++ {
		id := "n-" + itoa(i)
		lexical = append(lexical, SearchHit{Ref: id, SessionID: id, Passage: "receipt links " + id, ScoreKind: ScoreBM25})
	}
	svc := testService(t, nil)
	svc.SetDiscoverer(fakeDiscoverer{lexical: lexical})
	hits, err := svc.SearchEvidence(context.Background(), SearchQuery{Query: "receipt links", Limit: 8})
	if err != nil || len(hits) != 8 {
		t.Fatalf("truncated %+v %v", hits, err)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func idsOf(hits []SearchHit) []string {
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = hit.SessionID
	}
	return out
}
