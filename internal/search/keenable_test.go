package search

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestKeenableSearchRequestShape(t *testing.T) {
	var got map[string]any
	var title, method, contentType string
	server := serve(t, &keenableSearchURL, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		title = r.Header.Get("X-Keenable-Title")
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"query":"go","results":[
			{"title":"Go  1.25\nRelease Notes","url":"https://go.dev/doc/go1.25","snippet":"A rewritten\n garbage collector.","description":""},
			{"title":"No link","url":"  ","snippet":"dropped"},
			{"title":"Blog","url":"https://go.dev/blog/go1.25","snippet":"","description":"Falls back to the description.","published_at":"2025-08-12"}
		]}`))
	})

	results, err := (keenableSearch{}).Bind(Options{HTTPClient: server.Client()}).Search(context.Background(), "  go 1.25  ", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if method != http.MethodPost || contentType != "application/json" {
		t.Errorf("request = %s %q, want POST application/json", method, contentType)
	}
	if title != "codeaf" {
		t.Errorf("X-Keenable-Title = %q, want codeaf", title)
	}
	if got["query"] != "go 1.25" || got["max_results"] != float64(3) || got["disable_cards"] != true || got["snippet_max_length"] != float64(keenableSnippetChars) {
		t.Errorf("request body = %v", got)
	}
	want := []Result{
		{Title: "Go 1.25 Release Notes", URL: "https://go.dev/doc/go1.25", Snippet: "A rewritten garbage collector."},
		{Title: "Blog", URL: "https://go.dev/blog/go1.25", Snippet: "Falls back to the description.", Published: "2025-08-12"},
	}
	if len(results) != len(want) {
		t.Fatalf("results = %+v, want %+v", results, want)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
}

func TestKeenableSearchDefaultAndClampedLimit(t *testing.T) {
	var limits []float64
	server := serve(t, &keenableSearchURL, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		limits = append(limits, body["max_results"].(float64))
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	plug := (keenableSearch{}).Bind(Options{HTTPClient: server.Client()})
	for _, limit := range []int{0, 1000} {
		if _, err := plug.Search(context.Background(), "go", limit); err != nil {
			t.Fatalf("Search(%d): %v", limit, err)
		}
	}
	if len(limits) != 2 || limits[0] != defaultLimit || limits[1] != maxLimit {
		t.Errorf("max_results sent = %v, want [%d %d]", limits, defaultLimit, maxLimit)
	}
}

func TestKeenableSearchErrorStatus(t *testing.T) {
	server := serve(t, &keenableSearchURL, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Too many requests"}`))
	})
	_, err := (keenableSearch{}).Bind(Options{HTTPClient: server.Client()}).Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "keenable: 429") || !strings.Contains(err.Error(), "Too many requests") {
		t.Fatalf("err = %v, want a 429 naming keenable and the body", err)
	}
}

func TestKeenableSearchRejectsEmptyQuery(t *testing.T) {
	if _, err := (keenableSearch{}).Search(context.Background(), "   ", 5); err == nil || !strings.Contains(err.Error(), "empty query") {
		t.Fatalf("err = %v, want empty query", err)
	}
}

func TestKeenableSearchMalformedBody(t *testing.T) {
	server := serve(t, &keenableSearchURL, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})
	_, err := (keenableSearch{}).Bind(Options{HTTPClient: server.Client()}).Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "keenable: decode response") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}
