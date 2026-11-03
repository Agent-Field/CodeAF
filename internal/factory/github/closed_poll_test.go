package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	source "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestClosedIssueRequestCounts(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if _, err = st.Create(factory.Item{Product: "acme", Repo: "api", Num: n, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Title: "issue"}); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case r.URL.Path == "/repos/acme/api/issues":
			if r.URL.Query().Get("state") == "all" && r.URL.Query().Get("since") == "" {
				t.Error("closed history listed")
			}
			tag := fmt.Sprintf(`"%s"`, r.URL.RawQuery)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", tag)
			var rows []map[string]any
			for n := 1; n <= 5200; n++ {
				state := "closed"
				if n > 5000 {
					state = "open"
				}
				if r.URL.Query().Get("state") == "open" && state != "open" {
					continue
				}
				updated := at.Add(-time.Hour)
				if state == "open" {
					updated = at.Add(time.Duration(n) * time.Second)
				}
				if since, err := time.Parse(time.RFC3339, r.URL.Query().Get("since")); err == nil && updated.Before(since) {
					continue
				}
				rows = append(rows, map[string]any{"id": n, "number": n, "title": "issue", "state": state, "updated_at": updated})
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			lo := min((page-1)*100, len(rows))
			hi := min(lo+100, len(rows))
			_ = json.NewEncoder(w).Encode(rows[lo:hi])
		case strings.HasPrefix(r.URL.Path, "/repos/acme/api/issues/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/api/issues/"))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": n, "number": n, "state": "closed"})
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	src := source.New(forge.NewClient("request-counts").WithBaseURL(srv.URL), nil, nil)
	cursor := ""
	for i, want := range []int{6, 3, 3} {
		before := calls
		cursor, err = source.PollOnce(context.Background(), src, st, cursor, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("poll %d: requests=%d (5,000 closed, 200 open)", i+1, calls-before)
		if calls-before != want {
			t.Errorf("poll %d: requests=%d, want %d", i+1, calls-before, want)
		}
	}
}
