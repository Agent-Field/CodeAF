package store

import (
	"path/filepath"
	"testing"
)

func TestTasteCategoryJoinsTheEmpiricalAskGate(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.RecordAssumedWithDefault(QuestionCategoryTaste,
		"keep it this way", "voi", "or keep it the way I just did it?"); err != nil {
		t.Fatalf("skipped taste ask was refused: %v", err)
	}
	ask, stat, err := graph.ShouldAsk(QuestionCategoryTaste)
	if err != nil || !ask || stat.N != 0 {
		t.Fatalf("first taste ask = %t stat=%+v err=%v, want the ask allowed", ask, stat, err)
	}

	// A user who keeps every delivery teaches the gate to stop asking. Taste is
	// an ordinary VOI category with no consent exception, so it may be gated
	// away entirely — which is the whole point of asking it quietly.
	for range VOIMinSamples {
		answerTasteDefault(t, graph)
	}
	ask, stat, err = graph.ShouldAsk(QuestionCategoryTaste)
	if err != nil {
		t.Fatal(err)
	}
	if stat.N != VOIMinSamples || stat.Accepted != VOIMinSamples || ask {
		t.Fatalf("measured taste ask = %t stat=%+v, want the gate closed", ask, stat)
	}
}

func TestTasteShelfLifecycleReplaysThroughRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const rule = "the user wants written reports kept under a page"
	if _, err := graph.RecordFactFrom(FactWriterDistiller, "", "user", FactPreference, rule); err != nil {
		t.Fatal(err)
	}
	candidate, err := graph.RecordTasteCandidate("", "user", rule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordTasteCandidate("", "user", rule); err == nil {
		t.Fatal("a shelf was opened twice")
	}
	// A candidate is out of retrieval by construction: it has not earned the
	// right to reach a worker as settled knowledge.
	retrieved, err := graph.SearchFactsUncounted(FactQuery{Cues: []string{candidate.Scope}, Limit: 5})
	if err != nil || len(retrieved) != 0 {
		t.Fatalf("candidate rule was retrievable: %+v err=%v", retrieved, err)
	}

	active, err := graph.PromoteTasteRule(candidate.Seq)
	if err != nil || active.Status != FactActive || active.Scope != candidate.Scope {
		t.Fatalf("promoted rule = %+v err=%v", active, err)
	}
	if _, err := graph.PromoteTasteRule(active.Seq); err == nil {
		t.Fatal("an active rule was promoted again")
	}
	demoted, err := graph.DemoteTasteRule(active.Seq)
	if err != nil || demoted.Status != FactCandidate || demoted.Scope != candidate.Scope {
		t.Fatalf("demoted rule = %+v err=%v", demoted, err)
	}
	before, err := graph.TasteRules("")
	if err != nil || len(before) != 1 || before[0].Seq != demoted.Seq {
		t.Fatalf("shelf before rebuild = %+v err=%v", before, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, err := graph.TasteRules("")
	if err != nil || len(after) != 1 {
		t.Fatalf("shelf after rebuild = %+v err=%v", after, err)
	}
	if after[0].Seq != before[0].Seq || after[0].Status != before[0].Status ||
		after[0].Scope != before[0].Scope || after[0].Body != before[0].Body {
		t.Fatalf("rebuild changed the shelf:\n got %+v\nwant %+v", after[0], before[0])
	}
	if promoted, err := graph.TasteRules(FactActive); err != nil || len(promoted) != 0 {
		t.Fatalf("rebuild restood a demoted rule: %+v err=%v", promoted, err)
	}
	// The superseded standings stay in the journal; only the current line reads
	// back as the shelf.
	history, found, err := graph.FactBySeq(candidate.Seq)
	if err != nil || !found {
		t.Fatalf("candidate history found=%t err=%v", found, err)
	}
	if history.Status != FactSuperseded {
		t.Fatalf("candidate history after rebuild = %+v, want superseded", history)
	}
}

func TestTasteScopeIsTheShelfIdentity(t *testing.T) {
	const body = "the user wants written reports kept under a page"
	scope := TasteScope("repo:/src/aforge", body)
	subject, ok := TasteSubject(scope)
	if !ok || subject != "repo:/src/aforge" {
		t.Fatalf("subject of %q = (%q, %t)", scope, subject, ok)
	}
	if TasteScope("user", body) != TasteScope("user", body) || TasteScope("user", "  ") != "" {
		t.Fatalf("taste scope is not stable: %q", TasteScope("user", body))
	}
	if _, ok := TasteSubject("user"); ok {
		t.Fatal("an ordinary shelf decoded as a taste shelf")
	}
	value := TasteOptionValue(TasteAnswerMeant, scope)
	answer, decoded, ok := DecodeTasteOption(value)
	if !ok || answer != TasteAnswerMeant || decoded != scope {
		t.Fatalf("decode %q = (%q, %q, %t)", value, answer, decoded, ok)
	}
	if _, _, ok := DecodeTasteOption("redirect:cancel:node:x"); ok {
		t.Fatal("another option family decoded as taste")
	}
}

// answerTasteDefault settles one taste question on its offered default, which
// is what the ask gate measures.
func answerTasteDefault(t *testing.T, graph *Store) {
	t.Helper()
	options := []QuestionOption{
		{Label: "keep it this way", Value: TasteOptionValue(TasteAnswerKeep, "taste:user:short-reports")},
		{Label: "this is what I meant", Value: TasteOptionValue(TasteAnswerMeant, "taste:user:short-reports")},
	}
	question, err := graph.AskQuestion(AgentQuestion{
		SessionID: "voi", Text: "or keep it the way I just did it?",
		Urgency: QuestionNextNaturalMoment, Options: options,
		Category: QuestionCategoryTaste, DefaultAnswer: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveQuestion(question.Seq, QuestionAnswered, "keep it this way"); err != nil {
		t.Fatal(err)
	}
}
