package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeedbackTrackingUsesResolutionRatherThanCurrentLine(t *testing.T) {
	for _, mode := range []string{"outdated", "resolved", "partial", "denied"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					if mode == "denied" {
						http.Error(w, "denied", 403)
						return
					}
					fmt.Fprintf(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread","isResolved":%t,"comments":{"nodes":[{"databaseId":2}],"pageInfo":{"hasNextPage":%t}}}],"pageInfo":{"hasNextPage":false}}}}}}`, mode == "resolved", mode == "partial")
					return
				}
				if strings.Contains(r.URL.Path, "/pulls/") {
					fmt.Fprint(w, `[{"id":2,"body":"Preserve this unresolved finding","line":null,"original_line":2720,"updated_at":"2026-10-22T22:21:15Z"}]`)
					return
				}
				fmt.Fprint(w, `[]`)
			}))
			defer srv.Close()
			out, err := testClient(srv.URL, "fixture").PullFeedbackState(context.Background(), "o", "r", 11)
			if mode == "denied" || mode == "partial" {
				if err == nil {
					t.Fatal("incomplete resolution claimed complete")
				}
				return
			}
			if err != nil || len(out.Items) != 1 {
				t.Fatalf("missing comment: %v", err)
			}
			if out.Items[0].ID != "inline:2" || out.Items[0].Resolved != (mode == "resolved") {
				t.Fatal("wrong stable identity or resolution")
			}
			if strings.Contains(out.Text, "Preserve this") != (mode != "resolved") {
				t.Fatal("outdated/resolved text incorrect")
			}
		})
	}
}
func TestFeedbackLineMovementDoesNotChangeSemanticRevision(t *testing.T) {
	line := 8
	a := feedbackComment{ID: 1, Body: "fix it", Line: &line, Updated: "today", inline: true}
	before := renderFeedback([]feedbackComment{a}, nil)
	a.Line = nil
	after := renderFeedback([]feedbackComment{a}, nil)
	if before.Items[0] != after.Items[0] {
		t.Fatal("line movement changed revision")
	}
}

func TestFeedbackTrackingPagesLongThreadsWithoutLosingOutdatedComments(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			if strings.Contains(r.URL.Path, "/pulls/") {
				fmt.Fprint(w, `[{"id":2,"body":"Unresolved on page two","line":null,"original_line":19}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.Query, "repository(") {
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread","isResolved":false,"comments":{"nodes":[{"databaseId":1}],"pageInfo":{"hasNextPage":true,"endCursor":"a"}}}],"pageInfo":{"hasNextPage":false}}}}}}`)
			return
		}
		pages++
		if req.Variables["cursor"] == nil {
			fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"databaseId":1}],"pageInfo":{"hasNextPage":true,"endCursor":"a"}}}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"databaseId":2}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	}))
	defer srv.Close()
	out, err := testClient(srv.URL, "fixture").PullFeedbackState(context.Background(), "o", "r", 11)
	if err != nil || pages != 2 || len(out.Items) != 1 || out.Items[0].Resolved || !strings.Contains(out.Text, "Unresolved on page two") {
		t.Fatalf("paged thread lost: pages=%d err=%v", pages, err)
	}
}
