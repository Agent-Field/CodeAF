package store

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func metaStore(t *testing.T) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), "meta.db"))
}

func TestChannelDerivationSurvivalShrinkageAndPromptGate(t *testing.T) {
	graph := metaStore(t)
	stated, err := graph.RecordFactFrom(FactWriterHead, RootID, "user", FactPreference, "prefers compact output")
	if err != nil || stated.Channel != FactChannelStated {
		t.Fatalf("stated=%+v err=%v", stated, err)
	}
	distilled, err := graph.RecordFactFrom(FactWriterDistiller, RootID, "domain:test", FactLesson, "first observation")
	if err != nil || distilled.Channel != FactChannelDistilled {
		t.Fatalf("distilled=%+v err=%v", distilled, err)
	}
	trial, err := graph.RecordFactFrom(FactWriterTrial, RootID, "domain:test", FactLesson, "trial observation")
	if err != nil || trial.Channel != FactChannelTrial {
		t.Fatalf("trial=%+v err=%v", trial, err)
	}
	for index := 0; index < 12; index++ {
		fact, err := graph.RecordFact(RootID, "weak", FactQuirk, "bad inference "+string(rune('a'+index)))
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.QuarantineFact(fact.Seq, 0, FactOriginUser); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordFactFrom(FactWriterHead, RootID, "strong", FactPreference, "stable "+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}
	weak, err := graph.RecordFact(RootID, "weak", FactQuirk, "one weak occurrence")
	if err != nil {
		t.Fatal(err)
	}
	eligible, err := graph.PromptEligible(weak)
	if err != nil || eligible {
		t.Fatalf("one weak occurrence eligible=%t err=%v", eligible, err)
	}
	weak2, err := graph.RecordFact(RootID, "weak", FactQuirk, "one weak occurrence")
	if err != nil {
		t.Fatal(err)
	}
	eligible, err = graph.PromptEligible(weak2)
	if err != nil || !eligible {
		t.Fatalf("two weak occurrences eligible=%t err=%v", eligible, err)
	}
	stats, err := graph.ChannelSurvivalStats()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, stat := range stats {
		if stat.Kind == FactQuirk && stat.Channel == FactChannelInferred {
			found = true
			if stat.Reversed < 12 || stat.Credibility >= LowCredibilityThreshold {
				t.Fatalf("weak stat=%+v", stat)
			}
		}
	}
	if !found {
		t.Fatal("missing inferred quirk survival projection")
	}
	if got, want := ShrunkRate(.25, .75, 8), .50; math.Abs(got-want) > 1e-9 {
		t.Fatalf("shrunk=%v want=%v", got, want)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, ok, err := graph.FactBySeq(weak2.Seq)
	if err != nil || !ok {
		t.Fatalf("rebuilt weak fact ok=%t err=%v", ok, err)
	}
	if eligible, err := graph.PromptEligible(rebuilt); err != nil || !eligible {
		t.Fatalf("rebuilt eligibility=%t err=%v", eligible, err)
	}
}

func answerMetaQuestion(t *testing.T, graph *Store, category QuestionCategory, answer string) {
	t.Helper()
	options := []QuestionOption{{Label: "yes", Value: "yes"}, {Label: "no", Value: "no"}}
	question, err := graph.AskQuestion(AgentQuestion{SessionID: "meta-questions", Text: "Use the default?", Urgency: QuestionBlocking,
		Category: category, DefaultAnswer: "1", Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	message, err := graph.PostMessage(Message{SessionID: "meta-questions", Role: RoleUser, Body: answer, QuestionSeq: question.Seq})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveQuestion(question.Seq, QuestionAnswered, answer, message.Seq); err != nil {
		t.Fatal(err)
	}
}

func TestVOIGateExceptionsJournalAndReturnToAsking(t *testing.T) {
	graph := metaStore(t)
	for index := 0; index < VOIMinSamples; index++ {
		answerMetaQuestion(t, graph, QuestionCategoryCompileAssumption, "yes")
	}
	ask, stat, err := graph.ShouldAsk(QuestionCategoryCompileAssumption)
	if err != nil || ask || stat.AcceptanceRate != 1 {
		t.Fatalf("accepted gate ask=%t stat=%+v err=%v", ask, stat, err)
	}
	for _, category := range []QuestionCategory{QuestionCategoryCharterRatification, QuestionCategoryRailRaise} {
		ask, _, err = graph.ShouldAsk(category)
		if err != nil || !ask {
			t.Fatalf("exception %s ask=%t err=%v", category, ask, err)
		}
	}
	if err := graph.RecordAssumedWithDefault(QuestionCategoryCompileAssumption, "yes", "meta-questions", "Use it?"); err != nil {
		t.Fatal(err)
	}
	events, _ := graph.Events(0, 0)
	found := false
	for _, event := range events {
		if event.Kind == EventAssumedWithDefault {
			found = true
		}
	}
	if !found {
		t.Fatal("missing assumed_with_default event")
	}
	answerMetaQuestion(t, graph, QuestionCategoryCompileAssumption, "no")
	ask, stat, err = graph.ShouldAsk(QuestionCategoryCompileAssumption)
	if err != nil || !ask || stat.Different != 1 {
		t.Fatalf("lower acceptance gate ask=%t stat=%+v err=%v", ask, stat, err)
	}
}

// Scope — "the quick look now, or the proper job?" — is an ordinary VOI-gated
// ask, and the consent-bearing categories stay exempt no matter how consistently
// they are answered. The exemption is the whole safety property of the gate:
// learning that a person always says yes is not permission to stop asking them.
func TestScopeIsGatedWhileConsentCategoriesStayExempt(t *testing.T) {
	graph := metaStore(t)
	consenting := []QuestionCategory{
		QuestionCategoryCharterRatification, QuestionCategoryRailRaise, QuestionCategoryServiceConsent,
	}
	for _, category := range append([]QuestionCategory{QuestionCategoryScope}, consenting...) {
		for index := 0; index < VOIMinSamples; index++ {
			answerMetaQuestion(t, graph, category, "1")
		}
	}
	ask, stat, err := graph.ShouldAsk(QuestionCategoryScope)
	if err != nil || ask {
		t.Fatalf("scope never learned its default: ask=%t stat=%+v err=%v", ask, stat, err)
	}
	for _, category := range consenting {
		ask, _, err := graph.ShouldAsk(category)
		if err != nil || !ask {
			t.Fatalf("consent category %s stopped asking: ask=%t err=%v", category, ask, err)
		}
	}
}

func TestFiveTraitProjectionSupersedesSingletons(t *testing.T) {
	graph := metaStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "trait-job", Brief: "make a detailed release plan", Stage: 1}}}, Provenance{Origin: OriginUser, SessionID: "traits", Intent: "make a detailed release plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{SessionID: "traits", Kind: CommandSplice, Instruction: strings.Repeat("specific release requirement ", 12)}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{SessionID: "traits", Kind: CommandAmend, Target: "trait-job", Instruction: "correct the release target"}); err != nil {
		t.Fatal(err)
	}
	answerMetaQuestion(t, graph, QuestionCategoryCompileAssumption, "no")
	charter, err := NewCharter("trait-proposal", "watch release readiness", WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: "ready", Cadence: time.Hour}}, "ready", CharterAction{Template: "prepare"}, CharterRails{PerFiringBudgetUSD: .1, MaxFiringsPerDay: 1}, CharterProposed, Ratification{})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter.WithProposalShape("release-ready")); err != nil {
		t.Fatal(err)
	}
	if err := graph.DeclineCharterProposal(charter.ID, "not useful"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	measured, err := graph.MeasureTraits(now)
	if err != nil || len(measured) != 5 {
		t.Fatalf("traits=%+v err=%v", measured, err)
	}
	for _, trait := range measured {
		if trait.Measurement.N == 0 {
			t.Fatalf("trait %s has no evidence: %+v", trait.Name, trait.Measurement)
		}
	}
	first, err := graph.ProjectTraits(now)
	if err != nil || len(first) != 5 {
		t.Fatalf("first traits=%+v err=%v", first, err)
	}
	second, err := graph.ProjectTraits(now.Add(TraitRefreshInterval + time.Hour))
	if err != nil || len(second) != 5 {
		t.Fatalf("second traits=%+v err=%v", second, err)
	}
	all, err := graph.Facts(100)
	if err != nil {
		t.Fatal(err)
	}
	var active, history []Fact
	for _, fact := range all {
		if fact.Kind != FactTrait {
			continue
		}
		if fact.Status == FactActive {
			active = append(active, fact)
		}
		if fact.Status == FactSuperseded {
			history = append(history, fact)
		}
	}
	if len(active) != 5 {
		t.Fatalf("active trait singletons=%d %+v", len(active), active)
	}
	for _, fact := range active {
		if fact.Channel != FactChannelDistilled {
			t.Fatalf("trait fact=%+v", fact)
		}
	}
	if len(history) != 5 {
		t.Fatalf("superseded traits=%d err=%v", len(history), err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{TraitCorrectionStyle, TraitDefaultAcceptance, TraitProposalAppetite, TraitSpecGranularity, TraitExplorationTolerance} {
		if _, fact, ok, err := graph.Trait(name); err != nil || !ok || fact.Kind != FactTrait {
			t.Fatalf("rebuilt trait %s ok=%t fact=%+v err=%v", name, ok, fact, err)
		}
	}
}

func TestLearningProgressAllocation(t *testing.T) {
	metrics := AllocateLearningProgress([]ScopeSurprise{
		{Scope: "improving", Samples: 8, LearningProgress: .3},
		{Scope: "flat", Samples: 8, LearningProgress: 0},
		{Scope: "cold", Samples: 2, ColdStart: true},
		{Scope: "worsening", Samples: 8, LearningProgress: -.2},
	})
	by := map[string]float64{}
	for _, metric := range metrics {
		by[metric.Scope] = metric.Allocation
	}
	if by["improving"] <= by["cold"] || by["cold"] <= 0 || by["flat"] != 0 || by["worsening"] != 0 {
		t.Fatalf("allocations=%v", by)
	}
}

func TestACTRActivationDecayBoundsAndFallback(t *testing.T) {
	now := time.Now()
	got := BaseLevelActivation([]time.Time{now.Add(-24 * time.Hour), now.Add(-96 * time.Hour)}, now, .5)
	if want := math.Log(1.5); math.Abs(got-want) > 1e-9 {
		t.Fatalf("activation=%v want=%v", got, want)
	}
	graph := metaStore(t)
	fact, err := graph.RecordFact(RootID, "domain:actr", FactLesson, "revisited lesson")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "reader", Brief: "reuse lesson", Stage: 1}}}, Provenance{Origin: OriginUser, Intent: "reuse"}); err != nil {
		t.Fatal(err)
	}
	decay, n, err := graph.LearnedDecay(FactLesson)
	if err != nil || n != 0 || decay != ActivationDefaultDecay {
		t.Fatalf("fallback d=%v n=%d err=%v", decay, n, err)
	}
	for index := 0; index < ActivationMinRevisits; index++ {
		if err := graph.RecordFactInjection("reader", []int64{fact.Seq}); err != nil {
			t.Fatal(err)
		}
	}
	decay, n, err = graph.LearnedDecay(FactLesson)
	if err != nil || n < ActivationMinRevisits || decay < ActivationMinDecay || decay > ActivationMaxDecay {
		t.Fatalf("learned d=%v n=%d err=%v", decay, n, err)
	}
	activation, usedDecay, accesses, err := graph.FactActivation(fact.Seq, time.Now().Add(time.Hour))
	if err != nil || math.IsInf(activation, 0) || usedDecay < ActivationMinDecay || usedDecay > ActivationMaxDecay || accesses != ActivationMinRevisits+1 {
		t.Fatalf("fact activation=%v d=%v accesses=%d err=%v", activation, usedDecay, accesses, err)
	}
}

func TestParameterRegistryOneNotchRailsAndRebuild(t *testing.T) {
	graph := metaStore(t)
	evidence := ParameterEvidence{Reversals: 0, Total: 6, Rate: 0}
	change, changed, err := graph.TuneParameter(ParameterSkillPromotionOccurrences, -1, evidence, "eased skill promotion (0/6 reversals)")
	if err != nil || !changed || change.Old-change.New != 1 {
		t.Fatalf("change=%+v changed=%t err=%v", change, changed, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if got := graph.Parameter(ParameterSkillPromotionOccurrences); got != change.New {
		t.Fatalf("replayed=%v want=%v", got, change.New)
	}
	if _, changed, err = graph.TuneParameter(ParameterSkillPromotionOccurrences, -1, evidence, "at floor"); err != nil || changed {
		t.Fatalf("floor changed=%t err=%v", changed, err)
	}
	for index := 0; index < 10; index++ {
		_, _, _ = graph.TuneParameter(ParameterSkillPromotionOccurrences, 1, evidence, "tightened")
	}
	if got := graph.Parameter(ParameterSkillPromotionOccurrences); got != tunableRegistry[ParameterSkillPromotionOccurrences].Ceiling {
		t.Fatalf("ceiling=%v", got)
	}
}

// Traits are measured on every retrospective and are structurally barred from
// ordinary retrieval, which is right — a number about the user has no business
// in front of a worker who asked about a parser. Without an explicit way to ask
// for them by name, though, a whole learning loop terminated in a table nobody
// read.
func TestMeasuredTraitBlockIsTheCarveOutForBarredTraits(t *testing.T) {
	graph := metaStore(t)
	now := time.Now()
	if _, err := graph.RecordTrait(TraitCorrectionStyle, TraitMeasurement{
		Value: CorrectionStyleValue{Style: "immediate", MeanLatencySeconds: 42}, N: 9, Updated: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordTrait(TraitProposalAppetite, TraitMeasurement{
		Value: ProposalAppetiteValue{Acceptance: 0.75}, N: 4, Updated: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Ordinary retrieval still cannot see them, and must not start to.
	facts, err := graph.SearchFacts(FactQuery{Cues: []string{"trait:correction-style"}, Limit: 5})
	if err != nil || len(facts) != 0 {
		t.Fatalf("traits leaked into ordinary retrieval: %+v err=%v", facts, err)
	}

	block := graph.MeasuredTraitBlock(320)
	for _, want := range []string{"corrects immediate", "accepts 75% of standing proposals"} {
		if !strings.Contains(block, want) {
			t.Fatalf("trait block omitted %q:\n%s", want, block)
		}
	}
	if len(block) > 320 {
		t.Fatalf("trait block is %d bytes: %s", len(block), block)
	}
	if tight := graph.MeasuredTraitBlock(40); tight != "" {
		t.Fatalf("trait block ignored a budget it cannot fit: %q", tight)
	}
	if empty := metaStore(t).MeasuredTraitBlock(320); empty != "" {
		t.Fatalf("unmeasured store rendered a trait block: %q", empty)
	}
}
