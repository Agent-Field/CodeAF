package github

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConversationReviewFeedbackReachesHealthyFollowPR(t *testing.T) {
	revision := "2026-10-21T17:26:45Z"
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pulls/") {
			fmt.Fprint(w, `[]`)
			return
		}
		if fail {
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			rows := make([]map[string]any, 100)
			for i := range rows {
				rows[i] = map[string]any{"id": i, "body": "Thanks!"}
			}
			json.NewEncoder(w).Encode(rows)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{"id": 101, "body": "Request changes: add a failing-table test.", "updated_at": revision, "html_url": "https://github.com/o/r/pull/70#issuecomment-101", "user": map[string]string{"login": "review-bot"}}, {"id": 102, "body": PublicationMarker + "\nA machine receipt"}})
	}))
	defer server.Close()
	c := testClient(server.URL, "tok")
	ctx := context.Background()
	feedback, err := c.PullFeedback(ctx, "o", "r", 70)
	if err != nil || !strings.Contains(feedback, "101") || !strings.Contains(feedback, revision) || strings.Contains(feedback, "Thanks") || strings.Contains(feedback, "by codeaf factory") {
		t.Fatalf("%s %v", feedback, err)
	}
	it := factory.Item{Kind: factory.KindIssue, Publish: "pr", State: factory.StateShipped, Cap: 10, PullRequest: &factory.PullRequest{Number: 70, State: factory.PROpen, HeadSHA: strings.Repeat("a", 40), ChecksRead: true, Checks: factory.PRChecks{Passed: 8}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}}
	factory.WatchPublishedPR(&it)
	factory.QueueCIFollowUp(&it, time.Now())
	it.PullRequest.Feedback = feedback
	if !factory.QueueCIFollowUp(&it, time.Now()) || it.FollowUp.Attempt != 1 {
		t.Fatal("healthy PR ignored conversation review")
	}
	data, _ := json.Marshal(it)
	var restored factory.Item
	json.Unmarshal(data, &restored)
	restored.State = factory.StateShipped
	factory.WatchPublishedPR(&restored)
	// Monitoring may settle (watching -> healthy); the handled feedback must not
	// queue another repair round.
	factory.QueueCIFollowUp(&restored, time.Now())
	if restored.FollowUp.State == "queued" || restored.State == factory.StateQueued || restored.FollowUp.ReviewKey != it.FollowUp.ReviewKey {
		t.Fatalf("same feedback repeated after restart: %+v", *restored.FollowUp)
	}
	revision = "2026-10-21T18:00:00Z"
	feedback, err = c.PullFeedback(ctx, "o", "r", 70)
	restored.PullRequest.Feedback = feedback
	if err != nil || !factory.QueueCIFollowUp(&restored, time.Now()) {
		t.Fatal("edited review ignored")
	}
	fail = true
	if partial, err := c.PullFeedback(ctx, "o", "r", 70); err == nil || partial != "" {
		t.Fatal("failed feedback read returned all-clear/partial success")
	}
	for _, ack := range []string{"Thanks!", "LGTM 👍", PublicationMarker + "\nA machine receipt"} {
		if conversationFeedback(ack) {
			t.Fatal("routine receipt admitted")
		}
	}
	for _, text := range []string{"LGTM. But fix the race.", "Thanks! Please add coverage.", "a substantive external bot review"} {
		if !conversationFeedback(text) {
			t.Fatal(text)
		}
	}
}
