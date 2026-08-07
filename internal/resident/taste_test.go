package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	tasteFirstCorrection  = "the user wants written reports kept under a page"
	tasteSecondCorrection = "the user wants reports kept to under one page"
	tasteOtherCorrection  = "the user prefers the dev server left running between jobs"
)

func TestTwoSimilarCorrectionsBirthOneTasteCandidate(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-birth")
	settleTasteJob(t, graph, "taste-birth", "birth-one", tasteFirstCorrection)
	settleTasteJob(t, graph, "taste-birth", "birth-two", tasteSecondCorrection)
	settleTasteJob(t, graph, "taste-birth", "birth-three", tasteOtherCorrection)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rules, err := graph.TasteRules("")
	if err != nil || len(rules) != 1 {
		t.Fatalf("taste rules = %+v err=%v, want exactly the repeated correction", rules, err)
	}
	if rules[0].Status != store.FactCandidate || rules[0].Kind != store.FactPreference {
		t.Fatalf("born rule = %+v, want a preference candidate", rules[0])
	}
	if subject, ok := store.TasteSubject(rules[0].Scope); !ok || subject != "user" {
		t.Fatalf("rule scope = %q, want a user-subject taste shelf", rules[0].Scope)
	}
	if strings.Contains(rules[0].Body, "dev server") {
		t.Fatalf("the lone dissimilar correction became a rule: %q", rules[0].Body)
	}

	// A second pass must not reopen the same shelf, and the dissimilar
	// correction still has nothing to repeat.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rules, err = graph.TasteRules(""); err != nil || len(rules) != 1 {
		t.Fatalf("taste rules after second pass = %+v err=%v", rules, err)
	}
}

func TestCandidateTasteRuleAnnotatesTheDeliveryItRidesOn(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-ask")
	bornTasteCandidate(t, graph, reconciler, "taste-ask", "ask")

	node := deliveringNode(t, graph, "taste-ask", "ask-delivery")
	question, asked, err := AnnotateDelivery(graph, node, "the report, in prose")
	if err != nil || !asked {
		t.Fatalf("annotate delivery asked=%t err=%v", asked, err)
	}
	if question.Category != store.QuestionCategoryTaste || question.Urgency != store.QuestionNextNaturalMoment {
		t.Fatalf("annotation = %+v, want a queued taste question", question)
	}
	if question.Status != store.QuestionPending || question.OriginNodeID != "" {
		t.Fatalf("annotation blocked the delivery or anchored to it: %+v", question)
	}
	if len(question.Options) != 2 || question.Options[0].Label != tasteKeepLabel ||
		question.Options[1].Label != tasteMeantLabel || question.DefaultAnswer != "1" {
		t.Fatalf("annotation options = %+v", question.Options)
	}
	if _, _, ok := store.DecodeTasteOption(question.Options[1].Value); !ok {
		t.Fatalf("annotation option carries no shelf: %+v", question.Options[1])
	}

	// The delivery lands first and the question rides under it, surfaced by the
	// same natural moment every saved question uses.
	completeNode(t, graph, "ask-delivery", "the report is ready")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("ask-delivery")
	if err != nil || !found || node.Status != store.Done {
		t.Fatalf("delivery was held up: node=%+v found=%t err=%v", node, found, err)
	}
	messages, err := graph.Messages("taste-ask", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	delivery, annotation := -1, -1
	for index, message := range messages {
		if message.Body == "the report is ready" {
			delivery = index
		}
		if message.QuestionSeq == question.Seq {
			annotation = index
		}
	}
	if delivery < 0 || annotation != delivery+1 {
		t.Fatalf("annotation did not ride the delivery: delivery=%d annotation=%d %+v",
			delivery, annotation, messages)
	}

	// A shelf already being asked about is left alone on the next delivery.
	if _, asked, err := AnnotateDelivery(graph, deliveringNode(t, graph, "taste-ask", "ask-again"), "the report, in prose"); asked || err != nil {
		t.Fatalf("second annotation asked=%t err=%v, want the shelf left alone", asked, err)
	}
}

func TestMeantAnswerPromotesTasteRuleWithSettledMoment(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-promote")
	rule := bornTasteCandidate(t, graph, reconciler, "taste-promote", "promote")

	answerTaste(t, graph, "taste-promote", rule.Scope, tasteMeantLabel)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := graph.TasteRules(store.FactActive)
	if err != nil || len(active) != 1 || active[0].Scope != rule.Scope {
		t.Fatalf("active taste rules = %+v err=%v", active, err)
	}
	if standing, err := TasteStandingOf(graph, active[0]); err != nil || standing.For != 3 || standing.Against != 0 {
		t.Fatalf("standing = %+v err=%v, want two corrections and one agreement", standing, err)
	}
	moments := learningMessages(t, graph, "taste-promote")
	if len(moments) != 1 || !strings.HasPrefix(moments[0].Body, "⚖ settled: ") ||
		!strings.HasSuffix(moments[0].Body, " — I'll hold myself to it") {
		t.Fatalf("settled moment = %+v", moments)
	}
	if block := TasteBlock(graph); !strings.Contains(block, firstLine(rule.Body)) {
		t.Fatalf("gate taste block = %q, want the settled rule", block)
	}
}

func TestTwoKeepAnswersDemoteAnActiveTasteRule(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-demote")
	rule := bornTasteCandidate(t, graph, reconciler, "taste-demote", "demote")
	answerTaste(t, graph, "taste-demote", rule.Scope, tasteMeantLabel)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := graph.TasteRules(store.FactActive)
	if err != nil || len(active) != 1 {
		t.Fatalf("active taste rules = %+v err=%v", active, err)
	}

	// One refusal is an exception; the rule holds.
	answerTaste(t, graph, "taste-demote", rule.Scope, tasteKeepLabel)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if held, err := graph.TasteRules(store.FactActive); err != nil || len(held) != 1 {
		t.Fatalf("one refusal demoted the rule: %+v err=%v", held, err)
	}

	answerTaste(t, graph, "taste-demote", rule.Scope, tasteKeepLabel)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if held, err := graph.TasteRules(store.FactActive); err != nil || len(held) != 0 {
		t.Fatalf("active taste rules after two refusals = %+v err=%v", held, err)
	}
	candidates, err := graph.TasteRules(store.FactCandidate)
	if err != nil || len(candidates) != 1 || candidates[0].Scope != rule.Scope {
		t.Fatalf("demoted rule = %+v err=%v, want the shelf back in candidacy", candidates, err)
	}
	if TasteBlock(graph) != "" {
		t.Fatalf("gate still holds a demoted rule: %q", TasteBlock(graph))
	}
	moments := learningMessages(t, graph, "taste-demote")
	if len(moments) != 2 || !strings.HasPrefix(moments[1].Body, "· let go — ") ||
		!strings.HasSuffix(moments[1].Body, " — you'd rather I didn't") {
		t.Fatalf("demotion moment = %+v", moments)
	}
}

func TestTasteAnnotationExpiresOnItsOwnWindow(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-expiry")
	bornTasteCandidate(t, graph, reconciler, "taste-expiry", "expiry")

	question, asked, err := AnnotateDelivery(graph, deliveringNode(t, graph, "taste-expiry", "expiry-delivery"), "the report, in prose")
	if err != nil || !asked {
		t.Fatalf("annotate delivery asked=%t err=%v", asked, err)
	}
	completeNode(t, graph, "expiry-delivery", "done and delivered")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	reconciler.now = func() time.Time { return time.Now().Add(tasteAskWindow + time.Hour) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	expired, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || expired.Status != store.QuestionExpired {
		t.Fatalf("annotation = %+v found=%t err=%v, want a quiet expiry", expired, found, err)
	}
	if !strings.Contains(expired.Resolution, "relevance window") {
		t.Fatalf("expiry reason = %q", expired.Resolution)
	}
}

func tasteReconciler(t *testing.T, graph *store.Store, sessionID string) *Reconciler {
	t.Helper()
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), sessionID, "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	return reconciler
}

// bornTasteCandidate runs the aggregation seam once and returns the shelf it
// opened, so the tests downstream of birth do not restate it.
func bornTasteCandidate(t *testing.T, graph *store.Store, reconciler *Reconciler, sessionID, prefix string) store.Fact {
	t.Helper()
	settleTasteJob(t, graph, sessionID, prefix+"-one", tasteFirstCorrection)
	settleTasteJob(t, graph, sessionID, prefix+"-two", tasteSecondCorrection)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rules, err := graph.TasteRules(store.FactCandidate)
	if err != nil || len(rules) != 1 {
		t.Fatalf("born candidate = %+v err=%v", rules, err)
	}
	return rules[0]
}

func settleTasteJob(t *testing.T, graph *store.Store, sessionID, id, correction string) {
	t.Helper()
	deliveringNode(t, graph, sessionID, id)
	completeNode(t, graph, id, "delivered "+id)
	if _, err := graph.RecordFactFrom(store.FactWriterDistiller, id, "user",
		store.FactPreference, correction); err != nil {
		t.Fatal(err)
	}
}

func deliveringNode(t *testing.T, graph *store.Store, sessionID, id string) store.Node {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: "write the report", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: sessionID, Intent: "write me the report"}); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node(id)
	if err != nil || !found {
		t.Fatalf("node %s found=%t err=%v", id, found, err)
	}
	return node
}

func completeNode(t *testing.T, graph *store.Store, id, summary string) {
	t.Helper()
	claim, won, err := graph.Claim(id, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s won=%t err=%v", id, won, err)
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}
}

// answerTaste posts and settles one verdict on a shelf the way the head does:
// the question is surfaced into the thread and resolved with the chosen label.
func answerTaste(t *testing.T, graph *store.Store, sessionID, scope, label string) {
	t.Helper()
	options := []store.QuestionOption{
		{Label: tasteKeepLabel, Value: store.TasteOptionValue(store.TasteAnswerKeep, scope)},
		{Label: tasteMeantLabel, Value: store.TasteOptionValue(store.TasteAnswerMeant, scope)},
	}
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: sessionID, Text: store.QuestionMessageBody("or keep it the way I just did it?", options),
		Urgency: store.QuestionNextNaturalMoment, Options: options,
		Category: store.QuestionCategoryTaste, DefaultAnswer: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveQuestion(question.Seq, store.QuestionAnswered, label); err != nil {
		t.Fatal(err)
	}
}

// TestTasteBlockSkipsTheRuleItCannotFitAndKeepsGoing pins how the gate's block
// spends its budget. The block renders oldest-first, and stopping at the first
// line too long to fit would let one verbose rule hide every rule behind it. A
// line that does not fit is skipped, not final: the rules after it still land,
// whatever their age.
func TestTasteBlockSkipsTheRuleItCannotFitAndKeepsGoing(t *testing.T) {
	graph := openStore(t)
	// The store caps a single fact under the block budget, so no rule is
	// unfittable on its own — the skip has to come from position: the settled
	// rule spends enough of the budget that the verbose one cannot join it,
	// and the newest rule is small enough to land after the skip.
	settled := "always give the numbers before the narrative " + strings.Repeat("with units ", 14)
	verbose := "explain the reasoning " + strings.Repeat("in full ", 46)
	roomy := "name the file " + strings.Repeat("and its path ", 18)
	for _, rule := range []struct{ subject, body string }{
		{"reports", settled},
		{"prose", verbose},
		{"paths", roomy},
	} {
		if _, err := graph.RecordTasteCandidate("", rule.subject, rule.body); err != nil {
			t.Fatalf("record %s: %v", rule.subject, err)
		}
	}
	candidates := tasteRulesByStatus(t, graph, store.FactCandidate)
	for index := len(candidates) - 1; index >= 0; index-- {
		if _, err := graph.PromoteTasteRule(candidates[index].Seq); err != nil {
			t.Fatalf("promote %s: %v", candidates[index].Scope, err)
		}
	}

	rules := tasteRulesByStatus(t, graph, store.FactActive)
	if len(rules) != 3 || !strings.HasPrefix(rules[0].Body, "name the file") ||
		!strings.HasPrefix(rules[1].Body, "explain the reasoning") {
		t.Fatalf("fixture wrong: want roomy, verbose, settled newest first, got %+v", rules)
	}

	block := TasteBlock(graph)
	if !strings.Contains(block, "always give the numbers") {
		t.Errorf("the settled rule was starved by the one that could not fit:\n%q", block)
	}
	if strings.Contains(block, "explain the reasoning") {
		t.Errorf("a rule that did not fit was written anyway:\n%q", block)
	}
	if !strings.Contains(block, "name the file") {
		t.Errorf("the rule after the skipped one never landed — the skip did not keep going:\n%q", block)
	}
	if len(block) > tasteBlockBytes {
		t.Errorf("taste block = %d bytes, over its %d budget", len(block), tasteBlockBytes)
	}
}

func tasteRulesByStatus(t *testing.T, graph *store.Store, status string) []store.Fact {
	t.Helper()
	rules, err := graph.TasteRules(status)
	if err != nil {
		t.Fatalf("taste rules (%s): %v", status, err)
	}
	return rules
}

// Settled taste leads the gate's prompt, so the order it renders in decides
// whether the gate can ever be handed a warm prefix. Newest-first meant every
// newly settled rule PREPENDED and rewrote the block from its first byte;
// oldest-first makes a new rule an append.
func TestTasteBlockAppendsNewlySettledRules(t *testing.T) {
	graph := openStore(t)
	settle := func(subject, body string) {
		t.Helper()
		candidate, err := graph.RecordTasteCandidate("", subject, body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := graph.PromoteTasteRule(candidate.Seq); err != nil {
			t.Fatal(err)
		}
	}
	settle("user", "keep written comparisons under a page")
	settle("user", "name the files a job wrote beside the answer")
	before := TasteBlock(graph)
	if !strings.HasPrefix(before, "- keep written comparisons under a page") {
		t.Fatalf("taste block is not oldest-first:\n%s", before)
	}

	settle("user", "put the number in the first sentence of a report")
	after := TasteBlock(graph)
	if after == before {
		t.Fatal("the newly settled rule never reached the gate's block")
	}
	if !strings.HasPrefix(after, before) {
		t.Fatalf("a newly settled rule rewrote the block instead of appending:\nbefore:\n%s\n\nafter:\n%s", before, after)
	}
}

// The ask allows free text on purpose — "keep it this way, or 'shorter, no
// headings'?" invites the third answer — and typing it used to match no option,
// record nothing, and bring the identical question back after the next
// delivery.
func TestFreeTextTasteAnswerBecomesACorrectionInsteadOfNothing(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-free")
	rule := bornTasteCandidate(t, graph, reconciler, "taste-free", "free")
	subject, ok := store.TasteSubject(rule.Scope)
	if !ok {
		t.Fatalf("shelf %q has no subject", rule.Scope)
	}

	const typed = "shorter, and no headings at all"
	answerTaste(t, graph, "taste-free", rule.Scope, typed)

	answers, err := graph.TasteAnswers()
	if err != nil {
		t.Fatal(err)
	}
	free := 0
	for _, answer := range answers {
		if answer.Free == typed && answer.Scope == rule.Scope && answer.Answer == "" {
			free++
		}
	}
	if free != 1 {
		t.Fatalf("free-text answer was discarded: %+v", answers)
	}
	// It is neither a yes nor a no about the rule it was provoked by.
	standing, err := TasteStandingOf(graph, rule)
	if err != nil || standing.Against != 0 {
		t.Fatalf("typed answer counted against the shelf: %+v err=%v", standing, err)
	}

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	corrections, err := graph.CorrectionFacts(50)
	if err != nil {
		t.Fatal(err)
	}
	filed := 0
	for _, correction := range corrections {
		if correction.Body == typed && correction.Scope == subject {
			filed++
		}
	}
	if filed != 1 {
		t.Fatalf("typed answer never reached the correction shelf %q: %+v", subject, corrections)
	}
	// The pass runs every tick and the answer is durable; filing it twice would
	// supersede its own copy forever and churn the notebook.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	corrections, err = graph.CorrectionFacts(50)
	if err != nil {
		t.Fatal(err)
	}
	filed = 0
	for _, correction := range corrections {
		if correction.Body == typed {
			filed++
		}
	}
	if filed != 1 {
		t.Fatalf("typed answer was filed again on the next tick: %+v", corrections)
	}
}

// A one-word correction used to score 801 against every correction that
// happened to contain the word, so a single bogus match birthed a candidate
// rule the gate could then be held to.
func TestSentenceSimilarityRefusesTheContainmentShortcut(t *testing.T) {
	short := tasteTokens("shorter")
	long := tasteTokens("make the incident postmortem shorter")
	if score := sentenceTokenSimilarity(short, long); score >= TasteSimilarityFloor {
		t.Fatalf("a one-word correction matched an unrelated sentence at %d", score)
	}
	if score := sentenceTokenSimilarity(long, short); score >= TasteSimilarityFloor {
		t.Fatalf("similarity is not symmetric: %d", score)
	}
	// Two genuine statements of the same correction still meet.
	first := tasteTokens(tasteFirstCorrection)
	second := tasteTokens(tasteSecondCorrection)
	if score := sentenceTokenSimilarity(first, second); score < TasteSimilarityFloor {
		t.Fatalf("the same correction said twice scored %d", score)
	}
	// The containment shortcut is kept for shelf names, which is what it was
	// written for: repo:parser inside repo:parser-tests is an alias candidate.
	if score := normalizedTokenSimilarity([]string{"parser"}, []string{"parser", "test"}); score < 800 {
		t.Fatalf("shelf-name containment scored %d", score)
	}
	// And the documented floor now decides something: every non-zero score used
	// to be at least 60 whatever the constant said.
	if score := sentenceTokenSimilarity(
		tasteTokens("keep the report short"),
		tasteTokens("keep the report short and plain")); score < TasteSimilarityFloor {
		t.Fatalf("a near-identical pair scored %d", score)
	}
}

// The one quiet question a delivery may carry has to be chosen against the
// delivery. The node is not completed at that moment, so its summary is empty
// and the request was all there was — which is how the shelf's evidence filled
// up with answers to mismatched questions.
func TestDeliveryAnnotationChoosesAgainstWhatWasDelivered(t *testing.T) {
	graph := openStore(t)
	reconciler := tasteReconciler(t, graph, "taste-seen")
	for index, correction := range []string{
		"the user wants curl invocations kept to one line",
		"the user wants curl invocations written on one line",
	} {
		id := fmt.Sprintf("seen-%d", index)
		deliveringNode(t, graph, "taste-seen", id)
		completeNode(t, graph, id, "delivered "+id)
		if _, err := graph.RecordFactFrom(store.FactWriterDistiller, id, "tool:curl",
			store.FactPreference, correction); err != nil {
			t.Fatal(err)
		}
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rules, err := graph.TasteRules(store.FactCandidate)
	if err != nil || len(rules) != 1 {
		t.Fatalf("born candidate = %+v err=%v", rules, err)
	}

	// The request ("write me the report") says nothing about the shelf's
	// subject. Only the delivery does.
	node := deliveringNode(t, graph, "taste-seen", "seen-delivery")
	if _, asked, err := AnnotateDelivery(graph, node, "here is the draft"); asked || err != nil {
		t.Fatalf("annotation fired without a relevant delivery: asked=%t err=%v", asked, err)
	}
	seen := deliveringNode(t, graph, "taste-seen", "seen-delivery-two")
	if _, asked, err := AnnotateDelivery(graph, seen, "fetched it with curl and wrote it up"); !asked || err != nil {
		t.Fatalf("annotation missed the delivery it rides: asked=%t err=%v", asked, err)
	}
}
