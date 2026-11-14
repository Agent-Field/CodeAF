package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// C2, C3: The issue identity remains int64 and an empty result has no cursor.
func TestRepoIssuesJSONKeepsIdentityAndEmptyShape(t *testing.T) {
	var got RepoIssues
	raw := `{"repo":"acme/api","issues":[{"id":9007199254740993,"number":7,"title":"Retry","body":"details","url":"https://github.com/acme/api/issues/7","state":"closed","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-11-01T00:00:00Z"}],"rows":1,"newest":"2026-11-01T00:00:00Z"}`
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0].ID != 9007199254740993 || got.Newest == nil || !got.Newest.Equal(got.Issues[0].UpdatedAt) {
		t.Fatalf("decoded %+v", got)
	}
	encoded, err := json.Marshal(RepoIssues{Repo: "acme/api", Issues: []RepoIssue{}, Rows: 0})
	if err != nil || string(encoded) != `{"repo":"acme/api","issues":[],"rows":0}` {
		t.Fatalf("empty result = %s, %v", encoded, err)
	}
	encoded, err = json.Marshal(got)
	if err != nil || !strings.Contains(string(encoded), `"id":9007199254740993`) || strings.Contains(string(encoded), "body_cut") {
		t.Fatalf("issue result = %s, %v", encoded, err)
	}
}
