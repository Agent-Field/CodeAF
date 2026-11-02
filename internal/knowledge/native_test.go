package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeCompilesWithoutPython(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if err := v.Flush(context.Background(), "", true); err != nil {
		t.Fatalf("native compilation: %v", err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.Pending != 0 || len(state.Pages) != 0 {
		t.Fatalf("empty build: %+v", state)
	}
	if _, err := os.Stat(v.Directory() + "/knowledge.sqlite"); err != nil {
		t.Fatal(err)
	}
}

// The transport stands in for the external provider, while all compilation and
// publication run through the same Vault interface used by the application.
type knowledgeTransport func(*http.Request) (*http.Response, error)

func (f knowledgeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKnowledgeBuildPublishesSearchableCitationsWithoutPython(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "source", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != knowledge.Model {
			t.Fatalf("wrong model: %v", body["model"])
		}
		calls++
		result := `{"subjects":[]}`
		if calls == 1 {
			result = `{"claims":[{"local_id":"c","text":"Cedar queues jobs.","topic":"Queues","scope":"Cedar","kind":"fact","evidence":[{"source":"source","quote":"Cedar queues jobs."}]}],"notes":[],"relations":[],"aliases":[]}`
		}
		response, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": result}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(response)), Header: make(http.Header)}, nil
	})
	t.Setenv("PATH", t.TempDir())
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search("queues", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || len(hits[0].Page.Evidence) != 1 || hits[0].Page.Evidence[0].Quote != source.Text {
		t.Fatalf("missing cited knowledge: %+v", hits)
	}
	if calls != 2 {
		t.Fatalf("wanted extraction and discovery, got %d calls", calls)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("unchanged build repeated model calls")
	}
}

func TestKnowledgeHumanChangesSurviveNativeRebuilds(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Messages []knowledge.Source `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &payload); err != nil {
			t.Fatal(err)
		}
		result := map[string]any{"subjects": []any{}}
		if len(payload.Messages) > 0 {
			s := payload.Messages[0]
			result = map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s.Text, "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": s.ID, "quote": s.Text}}}}}
		}
		content, _ := json.Marshal(result)
		raw, _ := json.Marshal(map[string]any{"model": body.Model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
	})
	t.Setenv("PATH", t.TempDir())
	capture := func(id, text string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	capture("s1", "Cedar queues jobs.")
	hits, err := v.Search("queues", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("initial page: %+v %v", hits, err)
	}
	page := hits[0].Page
	if err = v.EditPage(page.ID, page.Revision, "My corrected queue instructions."); err != nil {
		t.Fatal(err)
	}
	capture("s2", "Cedar retries queued work.")
	updated, err := v.Read(page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HumanEdited || knowledge.PageMarkdown(updated) != "My corrected queue instructions." {
		t.Fatalf("human text overwritten: %+v", updated)
	}
	hits, err = v.Search("corrected", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("edit search mismatch: %+v %v", hits, err)
	}
	if err = v.DeletePage(page.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	capture("s3", "Cedar drains queued jobs.")
	if _, err = v.Read(page.ID); !os.IsNotExist(err) {
		t.Fatalf("deleted page reappeared: %v", err)
	}
	hits, err = v.Search("Cedar", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("deleted content leaked into search: %+v %v", hits, err)
	}
}

func TestKnowledgeCompilationKeepsExplicitGenerationControls(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "none" || body["max_tokens"] != float64(8000) || body["temperature"] != float64(0) {
			t.Fatalf("knowledge generation controls changed: reasoning=%v max_tokens=%v temperature=%v", body["reasoning"], body["max_tokens"], body["temperature"])
		}
		format, ok := body["response_format"].(map[string]any)
		if !ok || format["type"] != "json_object" {
			t.Fatalf("structured response mode missing: %+v", body["response_format"])
		}
		raw, _ := json.Marshal(map[string]any{"model": body["model"], "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"claims":[]}`}}}})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeCompilationKeepsRoleAffinityAcrossReopen(t *testing.T) {
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		return map[string]any{"subjects": []any{}}
	})
	transport := http.DefaultTransport
	keys := map[string]string{}
	profiles := []string{t.TempDir(), t.TempDir()}
	profileIndex := 0
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body struct{ Messages []struct{ Content string } }
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		key := r.Header.Get("X-Session-Id")
		if key == "" {
			t.Error("background compilation did not establish provider cache affinity")
		}
		role := body.Messages[0].Content
		group := profiles[profileIndex] + role
		if prior := keys[group]; prior != "" && prior != key {
			t.Error("role affinity changed across compilation/reopen")
		}
		for other, prior := range keys {
			if other != group && prior == key {
				t.Error("different profiles or roles share provider affinity")
			}
		}
		keys[group] = key
		return transport.RoundTrip(r)
	})
	for i, profile := range profiles {
		profileIndex = i
		v := knowledge.Open(profile)
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"baseline", "added"} {
			v = knowledge.Open(profile)
			if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar " + id + " operations."}); err != nil {
				t.Fatal(err)
			}
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(keys) != 4 {
		t.Fatalf("expected two roles in each of two profiles: got %d", len(keys))
	}
}
