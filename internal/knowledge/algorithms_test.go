package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func withKnowledgeModel(t *testing.T, respond func(map[string]any) map[string]any) {
	t.Helper()
	var mu sync.Mutex
	withConcurrentKnowledgeModel(t, func(payload map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return respond(payload)
	})
}

func withConcurrentKnowledgeModel(t *testing.T, respond func(map[string]any) map[string]any, consolidate ...func(map[string]any) map[string]any) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(request *http.Request) (*http.Response, error) {
		var body struct {
			Model    string
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &payload); err != nil {
			return nil, err
		}
		var result map[string]any
		if catalog, ok := payload["consolidation_catalog"].([]any); ok {
			groups := []any{}
			for _, raw := range catalog {
				draft := raw.(map[string]any)
				groups = append(groups, map[string]any{"members": []any{draft["id"]}, "title": draft["title"], "scope": draft["scope"], "purpose": draft["purpose"], "folder": draft["folder"]})
			}
			result = map[string]any{"groups": groups}
			if len(consolidate) > 0 {
				result = consolidate[0](payload)
			}
		} else {
			result = respond(payload)
		}
		content, _ := json.Marshal(result)
		raw, _ := json.Marshal(map[string]any{"model": body.Model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
	})
}
func TestKnowledgeRejectsUnsupportedQuotesAndQualifiesAssistantReports(t *testing.T) {
	for _, test := range []struct {
		name, role, quote string
		pages             int
		qualified         bool
	}{
		{"foreign quote", "user", "Cedar is deployed.", 0, false},
		{"assistant report", "assistant", "Cedar is proposed.", 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			if err := v.Capture(knowledge.Source{ID: "source", Session: "chat", Role: test.role, TS: "2026-10-25T12:00:00Z", Text: "Cedar is proposed."}); err != nil {
				t.Fatal(err)
			}
			withKnowledgeModel(t, func(payload map[string]any) map[string]any {
				if payload["messages"] == nil {
					return map[string]any{"subjects": []any{}}
				}
				return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar is proposed.", "scope": "Cedar", "topic": "Status", "kind": "fact", "evidence": []any{map[string]any{"source": "source", "quote": test.quote}}}}}
			})
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			snapshot, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Pages) != test.pages {
				t.Fatalf("wrong support admission: %+v", snapshot.Pages)
			}
			if test.qualified && !strings.Contains(knowledge.PageMarkdown(snapshot.Pages[0]), "The agent reported") {
				t.Fatal("assistant assertion was promoted to an unqualified fact")
			}
		})
	}
}
func TestKnowledgeCorrectionRequiresNewerHumanEvidence(t *testing.T) {
	for _, role := range []string{"user", "assistant"} {
		t.Run(role, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			oldID := ""
			withKnowledgeModel(t, func(payload map[string]any) map[string]any {
				if payload["messages"] == nil {
					return map[string]any{"subjects": []any{}}
				}
				source := payload["messages"].([]any)[0].(map[string]any)
				result := map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": source["text"], "key": "retry count", "scope": "Cedar", "topic": "Retries", "kind": "fact", "evidence": []any{map[string]any{"source": source["id"], "quote": source["text"]}}}}}
				if oldID != "" {
					result["relations"] = []any{map[string]any{"from": "new:c", "to": oldID, "kind": "supersedes", "reason": "The retry count changed."}}
				}
				return result
			})
			first := knowledge.Source{ID: "old", Session: "chat", Role: "user", TS: "2026-10-24T12:00:00Z", Text: "Cedar retries twice."}
			if err := v.Capture(first); err != nil {
				t.Fatal(err)
			}
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			snapshot, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			oldID = snapshot.Pages[0].Claims[0]
			if err := v.Capture(knowledge.Source{ID: "new", Session: "chat", Role: role, TS: "2026-10-25T12:00:00Z", Text: "Cedar retries three times."}); err != nil {
				t.Fatal(err)
			}
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			hits, err := v.Search("twice", 5)
			if err != nil {
				t.Fatal(err)
			}
			if role == "user" && len(hits) != 0 {
				t.Fatal("superseded claim remained current")
			}
			if role == "assistant" && len(hits) == 0 {
				t.Fatal("assistant report retired human evidence")
			}
		})
	}
}
func TestKnowledgeWithholdsExplanationRejectedByReview(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["messages"] != nil {
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar is proposed.", "scope": "Cedar", "topic": "Status", "kind": "proposal", "evidence": []any{map[string]any{"source": "s", "quote": "Cedar is proposed."}}}}}
		}
		if catalog, ok := payload["catalog"].([]any); ok {
			note := catalog[0].(map[string]any)
			return map[string]any{"subjects": []any{map[string]any{"title": "Cedar design", "scope": "Cedar", "notes": []any{note["id"]}, "aliases": []any{}}}}
		}
		if payload["subject"] != nil {
			claim := payload["claims"].([]any)[0].(map[string]any)
			return map[string]any{"sections": []any{map[string]any{"heading": "Status", "paragraphs": []any{map[string]any{"text": "Cedar is deployed.", "claims": []any{claim["id"]}}}}}, "questions": []any{}, "conflicts": []any{}}
		}
		return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": false, "useful": true, "reason": "A proposal is not deployment."}}}
	})
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar is proposed."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search("deployed", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("unsupported prose published: %+v %v", hits, err)
	}
	hits, err = v.Search("proposed", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("original qualified evidence lost: %+v %v", hits, err)
	}
}
func TestKnowledgeMalformedModelResultRetainsPendingSource(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(map[string]any) map[string]any { return map[string]any{"claims": "not an array"} })
	if err := v.Flush(context.Background(), "fixture", true); err == nil {
		t.Fatal("malformed extraction consumed saved chat")
	}
	state, err := v.Status()
	if err != nil || state.Pending != 1 || state.LastError == "" {
		t.Fatalf("missing retained work: %+v %v", state, err)
	}
}
