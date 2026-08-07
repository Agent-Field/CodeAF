package resident

import (
	"context"
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
	question, asked, err := AnnotateDelivery(graph, node)
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
	if _, asked, err := AnnotateDelivery(graph, deliveringNode(t, graph, "taste-ask", "ask-again")); asked || err != nil {
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

	question, asked, err := AnnotateDelivery(graph, deliveringNode(t, graph, "taste-expiry", "expiry-delivery"))
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
// spends its budget. The rules arrive newest first, so stopping at the first
// one too long to fit let one verbose rule hide every older rule behind it —
// and the older rules are the longest-settled ones, the very rules the gate
// exists to be held to. A line that does not fit is skipped, not final.
func TestTasteBlockSkipsTheRuleItCannotFitAndKeepsGoing(t *testing.T) {
	graph := openStore(t)
	// Oldest first, so that promoting them in this order leaves the block
	// reading them back newest first: roomy, then one that cannot fit beside
	// it, then the short settled rule that still can.
	settled := "always give the numbers before the narrative"
	verbose := "explain the reasoning " + strings.Repeat("in full ", 46)
	roomy := "name the file " + strings.Repeat("and its path ", 30)
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
	if !strings.Contains(block, settled) {
		t.Errorf("the settled rule was starved by the one that could not fit:\n%q", block)
	}
	if strings.Contains(block, verbose) {
		t.Errorf("a rule that did not fit was written anyway:\n%q", block)
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
