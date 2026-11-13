package knowledge_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func synthesisFixture(t *testing.T, understand func(map[string]any) map[string]any) *knowledge.Vault {
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
				subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " guide", "scope": n["scope"], "folder": []any{"Guides", n["scope"]}, "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		return understand(p)
	})
	return v
}

func synthesisDraft(p map[string]any) map[string]any {
	subject := p["subject"].(map[string]any)
	return map[string]any{"sections": []any{map[string]any{"heading": "Operations", "paragraphs": []any{map[string]any{"text": subject["scope"].(string) + " durable operating guide.", "claims": subject["claims"]}}}}}
}

func TestKnowledgeInitialSynthesisOverlapsWithTwoWorkerLimit(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var active, peak atomic.Int32
	v := synthesisFixture(t, func(p map[string]any) map[string]any {
		n := active.Add(1)
		defer active.Add(-1)
		for current := peak.Load(); n > current; current = peak.Load() {
			if peak.CompareAndSwap(current, n) {
				break
			}
		}
		started <- p["subject"].(map[string]any)["scope"].(string)
		<-release
		return synthesisDraft(p)
	})
	captureRefinementRound(t, v, "baseline", []string{"Cedar", "Maple", "Birch"})
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
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("initial subject synthesis did not overlap")
		}
	}
	during, err := v.Snapshot()
	if err != nil || len(during.Pages) != 0 {
		t.Fatalf("unfinished synthesis published pages: %+v %v", during.Pages, err)
	}
	releaseOnce.Do(func() { close(release) })
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 {
		t.Fatalf("expected two concurrent initial synthesis jobs, got %d", peak.Load())
	}
	after, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guides := 0
	for _, page := range after.Pages {
		if page.Kind == "subject" {
			guides++
			if !strings.Contains(knowledge.PageMarkdown(page), page.Scope+" durable operating guide.") || len(page.Evidence) != 1 {
				t.Fatalf("initial synthesis lost prose or sources: %+v", page)
			}
			if len(page.Folder) != 2 || page.Folder[0] != "Guides" || page.Folder[1] != page.Scope {
				t.Fatalf("parallel synthesis changed folder placement: %v", page.Folder)
			}
		}
	}
	if guides != 3 {
		t.Fatalf("initial synthesis lost a queued subject: got %d guides", guides)
	}
}

func TestKnowledgeInitialSynthesisFailureRetainsSourcesForRetry(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	v := synthesisFixture(t, func(p map[string]any) map[string]any {
		if fail.Load() && p["subject"].(map[string]any)["scope"] == "Cedar" {
			return map[string]any{"sections": 17}
		}
		return synthesisDraft(p)
	})
	captureRefinementRound(t, v, "baseline", []string{"Cedar", "Maple", "Birch"})
	if err := v.Flush(context.Background(), "fixture", true); err == nil {
		t.Fatal("invalid initial draft unexpectedly published")
	}
	failed, err := v.Snapshot()
	if err != nil || len(failed.Pages) != 0 {
		t.Fatalf("failed initial synthesis published partial pages: %+v %v", failed.Pages, err)
	}
	fail.Store(false)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("initial source progress did not resume: %v", err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guides := 0
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			guides++
			if len(page.Evidence) != 1 {
				t.Fatalf("resumed %s lost its original evidence", page.Title)
			}
		}
	}
	if guides != 3 {
		t.Fatalf("initial retry lost a subject: got %d guides", guides)
	}
}

func TestKnowledgeRepeatedConceptAssignmentsRetainAllNewEvidence(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["admission_policy"] != nil {
			checks := []any{}
			for _, raw := range p["answers"].([]any) {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "useful": true})
			}
			return map[string]any{"checks": checks}
		}
		if p["integration_policy"] != nil {
			return fixtureIntegrateAll(p)
		}
		if r := fixtureMergedAnswer(p); r != nil {
			return r
		}

		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			assignments := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				assignment := map[string]any{"title": "Operations", "scope": "Cedar", "notes": []any{n["id"]}}
				if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
					assignment["existing_subject_id"] = existing[0].(map[string]any)["id"]
				}
				assignments = append(assignments, assignment)
			}
			return map[string]any{"subjects": assignments}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		if p["remaining_steps"] != nil {
			return refinementDraft(p)
		}
		return synthesisDraft(p)
	})
	for i, text := range []string{"Cedar queues jobs.", "Cedar retries failed jobs.", "Cedar tracks job completion."} {
		if err := v.Capture(knowledge.Source{ID: text, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			if len(page.Evidence) != 3 {
				t.Fatalf("repeated assignment lost concept evidence: got %d citations", len(page.Evidence))
			}
			return
		}
	}
	t.Fatal("existing concept disappeared")
}
