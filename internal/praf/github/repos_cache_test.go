package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

func TestReposAllPages304SkipsGraphQL(t *testing.T)     { checkReposConditionalCounts(t, false) }
func TestReposChangedPageRefreshesGraphQL(t *testing.T) { checkReposConditionalCounts(t, true) }

func checkReposConditionalCounts(t *testing.T, change bool) {
	t.Helper()
	changed := false
	f, srv := newCountingForge(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			var ask struct {
				Variables map[string]string `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&ask); err != nil {
				t.Error(err)
			}
			data := map[string]any{}
			for i := 0; i < len(ask.Variables)/2; i++ {
				data["r"+strconv.Itoa(i)] = map[string]any{"issues": map[string]int{"totalCount": 7}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		if r.URL.Path != "/user/repos" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		page := r.URL.Query().Get("page")
		tag := `"repos-1"`
		if page == "2" && changed {
			tag = `"repos-2"`
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		rows := []map[string]any{}
		start, end := 0, 100
		if page == "2" {
			start, end = 100, 101
		}
		for i := start; i < end; i++ {
			name := fmt.Sprintf("r%d", i)
			rows = append(rows, map[string]any{"full_name": "acme/" + name, "name": name, "owner": map[string]string{"login": "acme"}, "open_issues_count": 9})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	c := idleClient(t, srv)
	repos, err := c.Repos(context.Background())
	if err != nil || len(repos) != 101 {
		t.Fatalf("first read: len=%d %v", len(repos), err)
	}
	changed = change
	repos, err = c.Repos(context.Background())
	if err != nil || len(repos) != 101 || repos[100].OpenIssues == nil || *repos[100].OpenIssues != 7 {
		t.Fatalf("conditional read: len=%d %v", len(repos), err)
	}
	want := 2
	if change {
		want = 4
	}
	if got := f.calls("POST /graphql"); got != want {
		t.Fatalf("changed=%v: GraphQL calls=%d want %d", change, got, want)
	}
	if got := f.calls("GET /user/repos"); got != 4 {
		t.Fatalf("REST calls=%d want 4", got)
	}
}
