package knowledge_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func refinementFixture(t *testing.T, learner func(map[string]any) map[string]any) *knowledge.Vault {
	t.Helper()
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				scope := strings.Fields(s["text"].(string))[0]
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": scope, "topic": "Operations", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " guide", "scope": n["scope"], "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["remaining_steps"] != nil {
			return learner(p)
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	return v
}

func captureRefinementRound(t *testing.T, v *knowledge.Vault, round string, scopes []string) {
	t.Helper()
	for _, scope := range scopes {
		if err := v.Capture(knowledge.Source{ID: scope + round, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: scope + " " + round + " guidance."}); err != nil {
			t.Fatal(err)
		}
	}
}

func refinementDraft(p map[string]any) map[string]any {
	scope := p["subject"].(map[string]any)["scope"].(string)
	refs := []any{}
	for _, raw := range p["claims"].([]any) {
		refs = append(refs, raw.(map[string]any)["id"])
	}
	return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Current operations", "paragraphs": []any{map[string]any{"text": scope + " baseline and added guidance.", "claims": refs}}}}}
}

func TestKnowledgeRefinementOverlapsTwoJobsAndPublishesTogether(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	v := refinementFixture(t, func(p map[string]any) map[string]any {
		calls.Add(1)
		started <- p["subject"].(map[string]any)["scope"].(string)
		<-release
		return refinementDraft(p)
	})
	scopes := []string{"Cedar", "Maple", "Birch"}
	captureRefinementRound(t, v, "baseline", scopes)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	captureRefinementRound(t, v, "added", scopes)
	done := make(chan error, 1)
	go func() { done <- v.Flush(context.Background(), "fixture", true) }()
	joined := false
	defer func() {
		releaseOnce.Do(func() { close(release) })
		if !joined {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	}()
	selected := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case scope := <-started:
			selected[scope] = true
		case <-time.After(3 * time.Second):
			t.Fatal("independent dirty subjects did not start together")
		}
	}
	if len(selected) != 2 {
		t.Fatalf("same subject assigned twice: %v", selected)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected at most two blocked jobs, got %d", got)
	}
	during, err := v.Snapshot()
	if err != nil || during.Revision != before.Revision {
		t.Fatalf("unfinished parallel work changed the published wiki: %v %v", during.Revision, err)
	}
	// Let the coordinator publish before examining the next pass.
	releaseOnce.Do(func() { close(release) })
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search("baseline and added", 10)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := map[string]bool{}
	for _, hit := range hits {
		if hit.Page.Kind == "subject" && strings.Contains(knowledge.PageMarkdown(hit.Page), "baseline and added guidance.") {
			refreshed[hit.Page.Scope] = true
			if len(hit.Page.Evidence) != 2 {
				t.Fatalf("refreshed %s lost evidence: %+v", hit.Page.Scope, hit.Page.Evidence)
			}
		}
	}
	if len(refreshed) != len(selected) {
		t.Fatalf("published work differs from completed subjects: got %v want %v", refreshed, selected)
	}
	for scope := range selected {
		if !refreshed[scope] {
			t.Fatalf("completed %s was not published: %v", scope, refreshed)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("remaining subject stranded or completed subjects retried: got %d jobs", got)
	}
}

func TestKnowledgeRefinementFailureRetainsPublicationAndRetries(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	fail.Store(true)
	v := refinementFixture(t, func(p map[string]any) map[string]any {
		calls.Add(1)
		if fail.Load() && p["subject"].(map[string]any)["scope"] == "Cedar" {
			return map[string]any{"action": 17}
		}
		return refinementDraft(p)
	})
	scopes := []string{"Cedar", "Maple"}
	captureRefinementRound(t, v, "baseline", scopes)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	captureRefinementRound(t, v, "added", scopes)
	if err := v.Flush(context.Background(), "fixture", true); err == nil {
		t.Fatal("invalid parallel draft unexpectedly published")
	}
	after, err := v.Snapshot()
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("failed refinement replaced the previous publication: %v %v", after.Revision, err)
	}
	fail.Store(false)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("retained evidence could not resume: %v", err)
	}
	hits, err := v.Search("baseline and added", 10)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	for _, hit := range hits {
		if hit.Page.Kind == "subject" && strings.Contains(knowledge.PageMarkdown(hit.Page), "baseline and added guidance.") {
			refreshed++
		}
	}
	if refreshed != 2 {
		t.Fatalf("retry stranded a dirty subject: got %d refreshed subjects", refreshed)
	}
	completedCalls := calls.Load()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != completedCalls {
		t.Fatal("unchanged successful refinement repeated provider work")
	}
}

func TestKnowledgeCanceledRefinementRetainsPublicationAndResumes(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	v := refinementFixture(t, func(p map[string]any) map[string]any {
		started <- struct{}{}
		<-release
		return refinementDraft(p)
	})
	scopes := []string{"Cedar", "Maple"}
	captureRefinementRound(t, v, "baseline", scopes)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	captureRefinementRound(t, v, "added", scopes)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- v.Flush(ctx, "fixture", true) }()
	joined := false
	defer func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		if !joined {
			<-done
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("dirty subject jobs did not start before cancellation")
		}
	}
	cancel()
	releaseOnce.Do(func() { close(release) })
	select {
	case err = <-done:
		joined = true
	case <-time.After(3 * time.Second):
		t.Fatal("canceled refinement did not finish")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled build returned %v", err)
	}
	after, err := v.Snapshot()
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("cancellation replaced the published wiki: %v %v", after.Revision, err)
	}
	// Retry can use any completed provider receipts; it must not lose dirty work.
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search("baseline and added", 10)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	for _, hit := range hits {
		if hit.Page.Kind == "subject" && strings.Contains(knowledge.PageMarkdown(hit.Page), "baseline and added guidance.") {
			refreshed++
		}
	}
	if refreshed != 2 {
		t.Fatalf("cancellation lost pending subject work: got %d refreshed subjects", refreshed)
	}
}
