package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestPracticeTickCreatesSelfOriginLowestPriorityJobWithoutThreadPost(t *testing.T) {
	graph := residentTestStore(t, "practice-tick.db")
	seedScopedSurprise(t, graph, "repo:/work/parser", "build parser and run go test")

	now := time.Now().Add(21 * time.Minute)
	reconciler := New(graph, nil, nil).
		WithDistiller(func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			return []Learned{{Scope: "tool:go", Kind: store.FactSkill,
				Body:  "parser-check runs the verified parser fixture suite",
				Skill: &SkillCandidate{Artifact: "/tmp/parser-check"}}}, nil
		}).
		WithPracticeLoop(2, 20*time.Minute)
	reconciler.now = func() time.Time { return now }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	questions, err := graph.Questions(store.QuestionPracticing, 10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("practicing questions = %+v, err=%v", questions, err)
	}
	if !strings.HasPrefix(questions[0].Body, "I didn't know") {
		t.Fatalf("question body = %q", questions[0].Body)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	var practice store.Node
	for _, node := range nodes {
		if node.Group == store.PracticeGroup {
			practice = node
			break
		}
	}
	if practice.ID == "" || practice.Provenance.Origin != store.OriginSelf ||
		practice.Provenance.SessionID != "" || practice.Status != store.Pending {
		t.Fatalf("practice job = %+v", practice)
	}
	messages, err := graph.Messages("user-session", 0, 100)
	if err != nil || len(messages) != 0 {
		t.Fatalf("practice posted to user thread: %+v, err=%v", messages, err)
	}
	charters, err := graph.Charters()
	if err != nil {
		t.Fatal(err)
	}
	var practiceCharter store.Charter
	for _, charter := range charters {
		if store.IsPracticeCharter(charter) {
			practiceCharter = charter
			break
		}
	}
	rails := practiceCharter.Rails()
	if practiceCharter.ID == "" || practiceCharter.Status != store.CharterActive ||
		practiceCharter.Ratification.Origin != store.OriginSelf || rails.ExpiresAt == nil ||
		rails.MaxFiringsPerDay != 2 || rails.PerFiringBudgetUSD != 1 {
		t.Fatalf("practice charter = %+v rails=%+v", practiceCharter, rails)
	}
	var woken, checked, fired, started bool
	events, _ := graph.Events(0, 0)
	for _, event := range events {
		switch event.Kind {
		case store.EventCharterWoken:
			woken = true
		case store.EventSentinelChecked:
			checked = true
		case store.EventCharterFired:
			fired = true
		case store.EventQuestionPracticeStarted:
			started = true
		}
	}
	if !woken || !checked || !fired || !started {
		t.Fatalf("practice journal woken=%t checked=%t fired=%t started=%t", woken, checked, fired, started)
	}

	claim, won, err := graph.Claim(practice.ID, "practice-worker")
	if err != nil || !won {
		t.Fatalf("claim practice: won=%t err=%v", won, err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: practice.ID,
		PromptTokens: 40, CompletionTokens: 40}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSurprise(store.NodeSurprise{NodeID: practice.ID,
		ActualTokens: 80, ExpectedTokens: 100, Surprise: 0.2}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "go test passed and prediction error fell"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved, err := graph.Questions(store.QuestionResolved, 10)
	if err != nil || len(resolved) != 1 {
		t.Fatalf("resolved questions = %+v, err=%v", resolved, err)
	}
	skills, err := graph.SkillFacts(store.FactCandidate, 10)
	if err != nil || len(skills) != 1 || skills[0].NodeID != practice.ID {
		t.Fatalf("practice-fed skill candidate = %+v, err=%v", skills, err)
	}
}

func TestPracticeScorerRejectsEmailLikeObservationalScope(t *testing.T) {
	graph := residentTestStore(t, "practice-email.db")
	seedScopedSurprise(t, graph, "tool:email", "check the email inbox for replies")
	reconciler := New(graph, nil, nil)
	if err := reconciler.scanSurpriseQuestions(); err != nil {
		t.Fatal(err)
	}
	questions, err := graph.Questions(store.QuestionOpen, 10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("email question = %+v, err=%v", questions, err)
	}
	candidates, err := reconciler.practiceCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("email-like scope was practiceable: %+v", candidates)
	}
}

func TestDistillerRecordsQuestionOnlyWhenUserFailureRevealsGap(t *testing.T) {
	graph := residentTestStore(t, "practice-distill.db")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "failed-user", Brief: "run parser tests", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "run parser tests"}); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("failed-user")
	reconciler := New(graph, nil, nil).WithDistiller(func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
		return []Learned{{Scope: "repo:/work/parser", Kind: store.FactQuestion,
			Body: "I didn't know which parser fixture reproduces the failure"}}, nil
	})
	reconciler.distillJob(context.Background(), node, true)
	questions, err := graph.Questions(store.QuestionOpen, 10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("failed-job questions = %+v, err=%v", questions, err)
	}

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "successful-self", Brief: "run parser tests", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginSelf, Intent: "run parser tests"}); err != nil {
		t.Fatal(err)
	}
	self, _, _ := graph.Node("successful-self")
	reconciler.distillJob(context.Background(), self, true)
	all, err := graph.Questions("", 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("self-origin failure minted a question: %+v, err=%v", all, err)
	}
}

func TestRunnerPreemptsPracticeWhenUserWorkArrives(t *testing.T) {
	graph := residentTestStore(t, "practice-preempt.db")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "practice-running", Brief: "run go test practice", Stage: 1, Group: store.PracticeGroup,
	}}}, store.Provenance{Origin: store.OriginSelf, Intent: "practice parser tests"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		if node.Group == store.PracticeGroup {
			close(started)
			<-ctx.Done()
			return ExecResult{}, ctx.Err()
		}
		return ExecResult{Summary: "user work done"}, nil
	}, "practice-preemption", 1)
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("initial practice dispatch = %d, %v", dispatched, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("practice execution did not start")
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "urgent-user", Brief: "fix the user build", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "fix the user build"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	practice, _, _ := graph.Node("practice-running")
	if practice.Status != store.Pending {
		t.Fatalf("preempted practice status = %s, want pending", practice.Status)
	}
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("user dispatch = %d, %v", dispatched, err)
	}
	runner.Wait()
	user, _, _ := graph.Node("urgent-user")
	if user.Status != store.Done {
		t.Fatalf("user work status = %s", user.Status)
	}
}

func residentTestStore(t *testing.T, name string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

func seedScopedSurprise(t *testing.T, graph *store.Store, scope, intent string) {
	t.Helper()
	for job := 1; job <= 2; job++ {
		surprise := 1.0
		if job == 2 {
			surprise = 0.6
		}
		rootID := fmt.Sprintf("history-%d", job)
		nodes := []store.NodeSpec{{ID: rootID, Brief: intent, Stage: 2}}
		for leaf := 1; leaf <= 3; leaf++ {
			nodes = append(nodes, store.NodeSpec{ID: fmt.Sprintf("%s-leaf-%d", rootID, leaf),
				Parent: rootID, Brief: intent, Stage: 1})
		}
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, store.Provenance{
			Origin: store.OriginUser, SessionID: "user-session", Intent: intent,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordFact(rootID, scope, store.FactLesson, intent); err != nil {
			t.Fatal(err)
		}
		for leaf := 1; leaf <= 3; leaf++ {
			id := fmt.Sprintf("%s-leaf-%d", rootID, leaf)
			completeResidentNode(t, graph, id, surprise)
		}
		completeResidentNode(t, graph, rootID, surprise)
	}
}

func completeResidentNode(t *testing.T, graph *store.Store, id string, surprise float64) {
	t.Helper()
	claim, won, err := graph.Claim(id, "practice-history")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", id, won, err)
	}
	if err := graph.RecordSurprise(store.NodeSurprise{NodeID: id,
		ActualTokens: int(100 * (1 + surprise)), ExpectedTokens: 100, Surprise: surprise}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "go test passed"); err != nil {
		t.Fatal(err)
	}
}
