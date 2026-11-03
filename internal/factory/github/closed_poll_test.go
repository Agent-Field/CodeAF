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
	t.Run("bounded sweep", testClosedSweepBound)
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
			decided := seed(15, func(it *factory.Item) {
				it.State = factory.StateNeedsYou
				it.PipelineDecided = true
				it.QKind = factory.QKindAskFirst
			})
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
					tag := fmt.Sprintf(`"%t-%s"`, closed, r.URL.RawQuery)
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
					for n := 1; n <= 5201; n++ {
						state := "closed"
						if n > 5000 && n <= 5200 {
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
						item := row(n, state)
						if n <= 5000 {
							item["updated_at"] = at.Add(-60 * 24 * time.Hour)
						}
						rows = append(rows, item)
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
			for _, it := range []factory.Item{running, worked, started, decided} {
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
			if len(items) != 206 {
				t.Errorf("imported closed history: %d rows, want 206", len(items))
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
			t.Logf("quiet poll: calls=%d conditional=%d", calls-beforeCalls, conditional-beforeConditional)
			closed = true
			if _, err = source.PollOnce(ctx, src, st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			items, _ = st.List()
			if len(items) != 206 {
				t.Fatalf("unseen closed issue became a job: %d rows", len(items))
			}
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

func testClosedSweepBound(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 45; n++ {
		_, err := st.Create(factory.Item{Product: "acme", Repo: "api", Num: n, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Title: "old"})
		if err != nil {
			t.Fatal(err)
		}
	}
	direct := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case r.URL.Path == "/repos/acme/api/issues":
			fmt.Fprint(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/api/issues/"):
			direct++
			if strings.HasSuffix(r.URL.Path, "/2") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// A person's concurrent start wins over the older poll snapshot.
			if strings.HasSuffix(r.URL.Path, "/1") {
				if err := st.Update(1, func(it *factory.Item) error { it.State = factory.StateRunning; return nil }); err != nil {
					t.Error(err)
				}
			}
			fmt.Fprint(w, `{"state":"closed"}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	api := forge.NewClient("bounded-close").WithBaseURL(srv.URL)
	poll := func(wantError bool) {
		t.Helper()
		_, err := source.PollOnce(ctx, source.New(api, nil, nil), st, "", time.Now)
		if (err != nil) != wantError {
			t.Fatalf("poll error = %v, want error %t", err, wantError)
		}
	}
	poll(true)
	if direct != 40 {
		t.Fatalf("first sweep used %d direct checks, want 40", direct)
	}
	poll(false)
	if direct != 45 {
		t.Fatalf("resumed sweep used %d total checks, want 45", direct)
	}
	poll(false)
	if direct != 45 {
		t.Fatal("completed sweep repeated")
	}
	got, _ := st.Get(1)
	if got.State != factory.StateRunning || got.DoneHow != "" {
		t.Fatal("poll stopped a concurrent start")
	}
	missing, _ := st.Get(2)
	if missing.DoneHow != "" {
		t.Fatal("missing issue was guessed closed")
	}
	later, _ := st.Get(3)
	if later.DoneHow != "closed" {
		t.Fatal("missing issue blocked a later closure")
	}
	t.Log("45 old rows: 40 direct checks, then 5, then 0; concurrent start preserved")
}
