package resident

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestLearningMomentsAnchorOneFactAndBatchPerJob(t *testing.T) {
	tests := []struct {
		name    string
		learned []Learned
		want    string
	}{
		{
			name: "one fact",
			learned: []Learned{{Scope: "repo:aforge", Kind: store.FactLesson,
				Body: "card receipts belong to their originating job\nwith supporting detail"}},
			want: "· learned — card receipts belong to their originating job",
		},
		{
			name: "batched",
			learned: []Learned{
				{Scope: "repo:aforge", Kind: store.FactPlain, Body: "first durable thing"},
				{Scope: "repo:aforge", Kind: store.FactLesson, Body: "second durable thing"},
				{Scope: "repo:aforge", Kind: store.FactPlaybook, Body: "third durable thing"},
			},
			want: "· learned 3 things ▸\n  · learned — first durable thing\n  · learned — second durable thing\n  · learned — third durable thing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			settle, reconciler := learningJobFixture(t, graph, "learning", func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
				return test.learned, nil
			})
			settle()
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			moments := learningMessages(t, graph, "learning")
			if len(moments) != 1 || moments[0].NodeID != "learning-job" || moments[0].Body != test.want {
				t.Fatalf("learning moments = %+v, want one anchored %q", moments, test.want)
			}
		})
	}
}

func TestLearningMomentShowsSettledExperiment(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "evidence-a", Brief: "evidence A", Stage: 1},
		{ID: "evidence-b", Parent: "evidence-a", Brief: "evidence B", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "trial", Intent: "seed evidence"}); err != nil {
		t.Fatal(err)
	}
	first, err := graph.RecordFact("evidence-a", "repo:aforge", store.FactLesson, "buffered reads worked")
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.RecordFact("evidence-b", "repo:aforge", store.FactLesson, "direct reads worked")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := graph.RecordUnsettledFact("evidence-a", "repo:aforge", store.UnsettledPair{Approaches: []store.UnsettledApproach{
		{Approach: "buffered reads", Scope: "repo:aforge", Evidence: []int64{first.Seq}},
		{Approach: "direct reads", Scope: "repo:aforge", Evidence: []int64{second.Seq}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "trial-job", Brief: "compare buffered and direct reads", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "trial", Intent: "settle the read strategy", TrialOf: pair.Seq}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil).WithDistiller(func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
		return []Learned{{Scope: "repo:aforge", Kind: store.FactPlaybook,
			Body: "buffered reads win for this parser", Replaces: pair.Seq}}, nil
	})
	if err := reconciler.SessionOpened(context.Background(), "trial", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("trial-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "comparison complete"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	moments := learningMessages(t, graph, "trial")
	if len(moments) != 1 || moments[0].Body != "⚖ settled: buffered reads win for this parser · 1 trial" {
		t.Fatalf("trial moments = %+v", moments)
	}
}

func TestSkillPromotionPostsForgedMomentAndReflectionDigest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "skills", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"skill-job-one", "skill-job-two"} {
		artifact := writeSkillArtifact(t, "repo-audit", "#!/bin/sh\nexit 0\n")
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: "teach a reusable audit", Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, SessionID: "skills", Intent: "audit this repository"}); err != nil {
			t.Fatal(err)
		}
		claim, won, err := graph.Claim(id, "worker")
		if err != nil || !won {
			t.Fatalf("claim %s won=%t err=%v", id, won, err)
		}
		if err := graph.Complete(claim, "audit complete"); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordSkillCandidate(id, "repo:audit", testSkillDoc, artifact); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("skills", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var forged, reflected *store.Message
	for index := range messages {
		message := &messages[index]
		if strings.HasPrefix(message.Body, "⚒ forged:") {
			forged = message
		}
		if strings.HasPrefix(message.Body, "· reflected —") {
			reflected = message
		}
	}
	if forged == nil || forged.NodeID != "skill-job-two" ||
		forged.Body != "⚒ forged: repo-audit — proved twice, now available" {
		t.Fatalf("forged moment = %+v", forged)
	}
	if reflected == nil || !strings.Contains(reflected.Body, "1 skill promoted") {
		t.Fatalf("reflection digest = %+v", reflected)
	}
	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(active) != 1 || filepath.Base(active[0].Artifact) != "repo-audit" {
		t.Fatalf("active skills = %+v err=%v", active, err)
	}
}

func TestLearningVisibilitySuppressesDetachedAndEmptyPaths(t *testing.T) {
	t.Run("detached defers to brief", func(t *testing.T) {
		graph := openStore(t)
		settle, reconciler := learningJobFixture(t, graph, "detached", func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			return []Learned{{Scope: "repo:aforge", Kind: store.FactLesson, Body: "learned while away"}}, nil
		})
		if err := reconciler.SessionClosed("detached", "tui"); err != nil {
			t.Fatal(err)
		}
		settle()
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if moments := learningMessages(t, graph, "detached"); len(moments) != 0 {
			t.Fatalf("detached learning moments = %+v", moments)
		}
	})

	t.Run("another attached thread preserves the originating anchor", func(t *testing.T) {
		graph := openStore(t)
		settle, reconciler := learningJobFixture(t, graph, "originating", func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			return []Learned{{Scope: "repo:aforge", Kind: store.FactLesson, Body: "learning stays with its job"}}, nil
		})
		if _, err := graph.TouchSeen("tui", "new-thread", store.SeenAttached); err != nil {
			t.Fatal(err)
		}
		settle()
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		moments := learningMessages(t, graph, "originating")
		if len(moments) != 1 || moments[0].NodeID != "learning-job" {
			t.Fatalf("originating learning moments = %+v", moments)
		}
	})

	t.Run("no learning adds no message bytes", func(t *testing.T) {
		graph := openStore(t)
		settle, reconciler := learningJobFixture(t, graph, "empty-learning", func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			return nil, nil
		})
		settle()
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		messages, err := graph.Messages("empty-learning", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Body != "job complete" {
			t.Fatalf("no-learning path wrote extra bytes: %+v", messages)
		}
	})
}

func TestRetrospectiveDigestNonzeroOnlyAndSilentWhenEmpty(t *testing.T) {
	t.Run("nonzero categories", func(t *testing.T) {
		graph := openStore(t)
		for index := 1; index <= reflectionMinJobs; index++ {
			settleRetrospectiveJob(t, graph, index)
		}
		reconciler := New(graph, nil, nil).WithReflector(func(_ context.Context, _ []JobSketch) ([]Learned, error) {
			return []Learned{{Scope: "repo:aforge", Kind: store.FactLesson, Body: "the series proved one stable lesson"}}, nil
		}).WithCharterProposals()
		if err := reconciler.SessionOpened(context.Background(), "reflection", "tui", time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		messages, err := graph.Messages("reflection", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		var digest string
		for _, message := range messages {
			if strings.HasPrefix(message.Body, "· reflected —") {
				digest = message.Body
			}
		}
		if digest == "" || !strings.Contains(digest, "1 proposal made") ||
			!strings.Contains(digest, "1 belief learned") || strings.Contains(digest, "territory") ||
			strings.Contains(digest, "skill") {
			t.Fatalf("retrospective digest = %q", digest)
		}
	})

	t.Run("empty reflection", func(t *testing.T) {
		graph := openStore(t)
		for index := 1; index <= reflectionMinJobs; index++ {
			settleRetrospectiveJob(t, graph, index)
		}
		reconciler := New(graph, nil, nil).WithReflector(func(_ context.Context, _ []JobSketch) ([]Learned, error) {
			return nil, nil
		})
		if err := reconciler.SessionOpened(context.Background(), "quiet-reflection", "tui", time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		messages, err := graph.Messages("quiet-reflection", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if strings.HasPrefix(message.Body, "· reflected —") {
				t.Fatalf("empty retrospective posted %q", message.Body)
			}
		}
	})
}

func TestRetractionMomentContent(t *testing.T) {
	if got := letGoMoment("obsolete belief\nold detail").headline; got != "· let go — obsolete belief" {
		t.Fatalf("let-go moment = %q", got)
	}
}

func learningJobFixture(t *testing.T, graph *store.Store, sessionID string, distill DistillFunc) (func(), *Reconciler) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "learning-job", Brief: "finish a learning job", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: sessionID, Intent: "learn from this job"}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil).WithDistiller(distill)
	if err := reconciler.SessionOpened(context.Background(), sessionID, "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settle := func() {
		claim, won, err := graph.Claim("learning-job", "worker")
		if err != nil || !won {
			t.Fatalf("claim won=%t err=%v", won, err)
		}
		if err := graph.Complete(claim, "job complete"); err != nil {
			t.Fatal(err)
		}
	}
	return settle, reconciler
}

func learningMessages(t *testing.T, graph *store.Store, sessionID string) []store.Message {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var moments []store.Message
	for _, message := range messages {
		if isLearningMomentBody(message.Body) {
			moments = append(moments, message)
		}
	}
	return moments
}

func isLearningMomentBody(body string) bool {
	return strings.HasPrefix(body, "· learned") || strings.HasPrefix(body, "⚒ forged:") ||
		strings.HasPrefix(body, "⚖ settled:") || strings.HasPrefix(body, "· let go —")
}

// Overnight work learns things with nobody watching. Dropping the moment
// outright — which is what wiping the map unconditionally did — meant the whole
// `aforge wake` path taught the notebook and told the user nothing.
func TestLearningMomentsWaitForSomebodyToReadThem(t *testing.T) {
	graph := openStore(t)
	settle, reconciler := learningJobFixture(t, graph, "away",
		func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			return []Learned{{Scope: "repo:aforge", Kind: store.FactLesson,
				Body: "overnight work still teaches something"}}, nil
		})
	if err := reconciler.SessionClosed("away", "tui"); err != nil {
		t.Fatal(err)
	}
	settle()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if moments := learningMessages(t, graph, "away"); len(moments) != 0 {
		t.Fatalf("moment posted with no surface attached: %+v", moments)
	}
	// Nothing new happens; the user simply comes back.
	if err := reconciler.SessionOpened(context.Background(), "away", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	moments := learningMessages(t, graph, "away")
	if len(moments) != 1 || !strings.Contains(moments[0].Body, "overnight work still teaches something") {
		t.Fatalf("held moment never arrived: %+v", moments)
	}
	// And it arrives exactly once.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if moments := learningMessages(t, graph, "away"); len(moments) != 1 {
		t.Fatalf("held moment arrived twice: %+v", moments)
	}
}
