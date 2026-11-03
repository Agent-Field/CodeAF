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
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestClosedIssuesLeaveBacklogOnPoll(t *testing.T) {
	for _, prior := range []string{"", `{"acme/api":"2026-10-01T00:00:00Z"}`} {
		t.Run("cursor="+prior, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetRepos([]string{"acme/api"}); err != nil {
				t.Fatal(err)
			}
			seed := func(n int, change func(*factory.Item)) factory.Item {
				it := factory.Item{Product: "acme", Repo: "api", Num: n, Origin: factory.OriginForge, Kind: factory.KindIssue, State: factory.StateNew, Title: "issue"}
				if change != nil {
					change(&it)
				}
				made, err := st.Create(it)
				if err != nil {
					t.Fatal(err)
				}
				return made
			}
			old := seed(10, nil)
			running := seed(11, func(it *factory.Item) { it.State = factory.StateRunning })
			worked := seed(12, func(it *factory.Item) { it.Stream = &factory.Stream{} })
			started := seed(13, func(it *factory.Item) { it.Starter = "owner" })
			held := seed(14, func(it *factory.Item) {
				it.State = factory.StateNeedsYou
				it.Question = "Start?"
				it.QKind = factory.QKindAskFirst
			})
			closed := false
			calls, pages, details, conditional := 0, 0, 0, 0
			at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				js := func(v any) { _ = json.NewEncoder(w).Encode(v) }
				row := func(n int, state string) map[string]any {
					return map[string]any{"number": n, "title": "issue", "state": state, "created_at": at, "updated_at": at, "user": map[string]any{"login": ""}}
				}
				switch {
				case r.URL.Path == "/user":
					js(map[string]any{"login": "owner"})
				case r.URL.Path == "/repos/acme/api/issues":
					calls++
					pages++
					tag := fmt.Sprintf(`"%t"`, closed)
					if r.Header.Get("If-None-Match") != "" {
						conditional++
					}
					if r.Header.Get("If-None-Match") == tag {
						w.WriteHeader(304)
						return
					}
					w.Header().Set("ETag", tag)
					var rows []map[string]any
					// The repository has 5,000 closed issues and 200 open ones. Closed
					// history must never be walked to find the few rows already local.
					for n := 1; n <= 5200; n++ {
						state := "closed"
						if n > 5000 {
							state = "open"
						}
						if n == 5001 && closed {
							state = "closed"
						}
						if r.URL.Query().Get("state") == "open" && state != "open" {
							continue
						}
						if r.URL.Query().Get("since") != "" && n <= 5000 {
							continue
						}
						rows = append(rows, row(n, state))
					}
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					if page < 1 {
						page = 1
					}
					lo := min((page-1)*100, len(rows))
					hi := min(lo+100, len(rows))
					js(rows[lo:hi])
				case strings.HasPrefix(r.URL.Path, "/repos/acme/api/issues/"):
					calls++
					details++
					n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/api/issues/"))
					js(row(n, "closed"))
				default:
					t.Errorf("unexpected request %s", r.URL)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			api := forge.NewClient("closed-poll-" + t.Name()).WithBaseURL(srv.URL)
			src := source.New(api, nil, nil)
			cursor, err := source.PollOnce(ctx, src, st, prior, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := st.Get(old.ID)
			if factory.JobState(got) != "done" || got.DoneHow != "closed" || got.DoneAt.IsZero() {
				t.Fatalf("closed issue #10 remains %s (DoneHow=%q)", factory.JobState(got), got.DoneHow)
			}
			if triage.Wants(got) {
				t.Error("closed job still eligible for paid triage")
			}
			for _, it := range []factory.Item{running, worked, started} {
				got, _ := st.Get(it.ID)
				if got.State != it.State || got.DoneHow != "" {
					t.Errorf("poll finished protected job #%d", it.Num)
				}
			}
			got, _ = st.Get(held.ID)
			if got.DoneHow != "closed" || got.Question != "" {
				t.Error("untouched needs-you row was not finished")
			}
			history, _ := record.Open(st, record.Options{})
			events, err := history.Since(ctx, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			finishes := 0
			for _, e := range events {
				if e.Kind == record.SourceClosed {
					finishes++
				}
			}
			if finishes != 2 {
				t.Errorf("Activity has %d source closes, want 2", finishes)
			}
			items, _ := st.List()
			if len(items) != 205 {
				t.Errorf("imported closed history: %d rows, want 205", len(items))
			}
			t.Logf("first poll: calls=%d pages=%d direct_checks=%d", calls, pages, details)
			beforeCalls, beforeConditional := calls, conditional
			cursor, err = source.PollOnce(ctx, src, st, cursor, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			if calls-beforeCalls != 1 || conditional-beforeConditional != 1 {
				t.Errorf("quiet poll: calls=%d conditional=%d; want 1 each", calls-beforeCalls, conditional-beforeConditional)
			}
			closed = true
			if _, err = source.PollOnce(ctx, src, st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			items, _ = st.List()
			var recent factory.Item
			for _, it := range items {
				if it.Num == 5001 {
					recent = it
				}
			}
			if recent.DoneHow != "closed" {
				t.Fatal("issue closed after cursor remains in Backlog")
			}
			closed = false
			if _, err = source.PollOnce(ctx, src, st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			got, _ = st.Get(recent.ID)
			if got.DoneHow != "closed" {
				t.Fatal("reopen silently restarted a finished job")
			}
			// Sweep completion survives a fresh Source and a process-style restart.
			beforeCalls = calls
			if _, err = source.PollOnce(ctx, source.New(api, nil, nil), st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			if calls-beforeCalls != 1 {
				t.Errorf("completed sweep repeated: %d calls", calls-beforeCalls)
			}
		})
	}
}
