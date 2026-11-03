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
			briefing := seed(16, func(it *factory.Item) { it.BriefWriting = &factory.BriefWriting{} })
			held := seed(14, func(it *factory.Item) {
				it.State = factory.StateNeedsYou
				it.Question = "Start?"
				it.QKind = factory.QKindAskFirst
			})
			closed := false
			calls, pages, details, conditional := 0, 0, 0, 0
			reopened := false
			at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				js := func(v any) { _ = json.NewEncoder(w).Encode(v) }
				row := func(n int, state string) map[string]any {
					return map[string]any{"number": n, "title": "issue", "state": state, "created_at": at, "updated_at": at, "user": map[string]any{"login": ""}}
				}
				switch {
				case r.URL.Path == "/user":
					calls++
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
					// The repository has 5,000 old closed issues, one recent unseen
					// closure, and 200 open issues. Closed
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
						// Each row has its own update time and closing one moves it,
						// so since answers what GitHub would.
						updated := at.Add(time.Duration(n-5000) * time.Second)
						if n <= 5000 {
							updated = at.Add(-60 * 24 * time.Hour)
						}
						if n == 5001 && closed {
							updated = at.Add(time.Hour)
						}
						if n == 5001 && reopened {
							updated = time.Now().Add(2 * time.Second)
						}
						if since, err := time.Parse(time.RFC3339, r.URL.Query().Get("since")); err == nil && updated.Before(since) {
							continue
						}
						item := row(n, state)
						item["updated_at"] = updated
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
			for _, it := range []factory.Item{running, worked, started, decided, briefing} {
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
			if len(items) != 207 {
				t.Errorf("imported closed history: %d rows, want 207", len(items))
			}
			if calls != 6 || pages != 3 || details != 2 {
				t.Errorf("first poll used calls=%d pages=%d direct_checks=%d; want 6, 3, 2", calls, pages, details)
			}
			t.Logf("first poll: calls=%d pages=%d direct_checks=%d (includes identity)", calls, pages, details)
			// The cursor moved, so the next list is a new question with one small answer.
			beforeCalls := calls
			if cursor, err = source.PollOnce(ctx, src, st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			if calls-beforeCalls != 1 {
				t.Errorf("poll after the cursor moved: %d calls, want 1", calls-beforeCalls)
			}
			beforeCalls = calls
			beforeConditional := conditional
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
			if len(items) != 207 {
				t.Fatalf("unseen closed issue became a job: %d rows", len(items))
			}
			var recent factory.Item
			for _, it := range items {
				if it.Num == 5001 {
					recent = it
				}
			}
			if recent.DoneHow != "closed" {
				t.Fatalf("issue closed after cursor remains in Backlog: state=%s changed=%s starter=%q taker=%q adapted=%v scout=%v answered=%d decided=%t", recent.State, recent.Changed, recent.Starter, recent.Taker, recent.Adapted, recent.Scout != nil, len(recent.AnsweredQuestions), recent.PipelineDecided)
			}
			closed, reopened = false, true
			if _, err = source.PollOnce(ctx, src, st, cursor, time.Now); err != nil {
				t.Fatal(err)
			}
			got, _ = st.Get(recent.ID)
			if got.DoneHow != "" || got.State != factory.StateNew || !got.DoneAt.IsZero() || got.Starter != "" || got.Stream != nil {
				t.Fatalf("reopened issue with no work did not return to Backlog: %+v", got)
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
	// Row 2 is missing on GitHub: unknown, so it stays, and it is no trouble
	// with the poll.
	poll(false)
	if meta, _ := st.SourceMeta(source.Name); meta.Trouble != "" {
		t.Fatalf("a missing issue read as poll trouble: %q", meta.Trouble)
	}
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

// A closed issue the factory did work on keeps that ending when it reopens:
// only a row with nothing on it goes back to Backlog.
func TestReopenedIssueWithWorkStaysDone(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	row := factory.Item{Product: "o", Repo: "r", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateShipped, DoneHow: "closed", Stream: &factory.Stream{Spent: 0.2}}
	saved, err := st.Create(row)
	if err != nil {
		t.Fatal(err)
	}
	open := factory.Item{Product: "o", Repo: "r", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t again", Changed: time.Now()}
	if err := source.Merge(st, source.Name, []factory.Item{open}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DoneHow != "closed" || got.State != factory.StateShipped {
		t.Fatalf("worked job reopened: state=%s how=%q", got.State, got.DoneHow)
	}
}

// A poll that read the open list before a person's start found the issue closed
// carries an older change time, and does not undo the close.
func TestAStaleOpenRowDoesNotReopenAClose(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	done := time.Now()
	saved, err := st.Create(factory.Item{Product: "o", Repo: "r", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateShipped, DoneHow: "closed", DoneAt: done})
	if err != nil {
		t.Fatal(err)
	}
	stale := factory.Item{Product: "o", Repo: "r", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", Changed: done.Add(-time.Minute)}
	if err := source.Merge(st, source.Name, []factory.Item{stale}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(saved.ID); got.DoneHow != "closed" {
		t.Fatalf("a stale list undid the close: state=%s how=%q", got.State, got.DoneHow)
	}
}

// An issue closed while nothing ran is found by the next process's first poll,
// even when an earlier process finished its sweep.
func TestAnIssueClosedWhileTheAppWasOffLeavesBacklog(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	row, err := st.Create(factory.Item{Product: "acme", Repo: "api", Num: 1, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	state := "open"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case r.URL.Path == "/repos/acme/api/issues":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/repos/acme/api/issues/1":
			fmt.Fprintf(w, `{"state":%q}`, state)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	api := forge.NewClient("closed-while-off").WithBaseURL(srv.URL)
	poll := func() {
		t.Helper()
		if _, err := source.PollOnce(ctx, source.New(api, nil, nil), st, "", time.Now); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	state = "closed"
	poll()
	if got, _ := st.Get(row.ID); got.DoneHow != "" {
		t.Fatal("a finished sweep read the row again inside one process")
	}
	source.ForgetSweeps()
	poll()
	if got, _ := st.Get(row.ID); got.DoneHow != "closed" {
		t.Fatalf("issue closed while the app was off stayed: state=%s", got.State)
	}
}

func closedTestRow(change func(*factory.Item)) factory.Item {
	it := factory.Item{Product: "o", Repo: "r", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateNew}
	if change != nil {
		change(&it)
	}
	return it
}

// Finishing a row must never lose a person's work: anything a person or a paid
// look put on it keeps it where it is, and a Done row with it stays Done.
func TestAPersonsWorkKeepsTheRowWhereItIs(t *testing.T) {
	for name, change := range map[string]func(*factory.Item){
		"edited steps": func(it *factory.Item) { it.Adapted = []string{"dropped test"} },
		"look closer":  func(it *factory.Item) { it.Scout = &factory.Scout{Plan: []string{"step one"}} },
		"answered":     func(it *factory.Item) { it.AnsweredQuestions = []factory.Question{{}} },
		"taken":        func(it *factory.Item) { it.Taker = "someone" },
	} {
		t.Run(name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			backlog, err := st.Create(closedTestRow(change))
			if err != nil {
				t.Fatal(err)
			}
			marker := closedTestRow(func(it *factory.Item) { it.DoneHow, it.Title, it.Changed = "closed", "", time.Now() })
			if err := source.Merge(st, source.Name, []factory.Item{marker}); err != nil {
				t.Fatal(err)
			}
			if got, _ := st.Get(backlog.ID); got.DoneHow != "" || got.State != factory.StateNew {
				t.Fatalf("close finished a row with work: state=%s how=%q", got.State, got.DoneHow)
			}
			st2, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			done, err := st2.Create(closedTestRow(func(it *factory.Item) {
				change(it)
				it.State, it.DoneHow, it.DoneAt = factory.StateShipped, "closed", time.Now().Add(-time.Hour)
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err := source.Merge(st2, source.Name, []factory.Item{closedTestRow(func(it *factory.Item) { it.Changed = time.Now() })}); err != nil {
				t.Fatal(err)
			}
			if got, _ := st2.Get(done.ID); got.DoneHow != "closed" {
				t.Fatalf("reopen moved a row with work back: state=%s", got.State)
			}
		})
	}
}

// A close read before a reopen does not finish the reopened issue.
func TestAStaleCloseDoesNotFinishAReopenedIssue(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row, err := st.Create(closedTestRow(func(it *factory.Item) {
		it.State, it.DoneHow, it.DoneAt = factory.StateShipped, "closed", now.Add(-time.Hour)
	}))
	if err != nil {
		t.Fatal(err)
	}
	open := closedTestRow(func(it *factory.Item) { it.Changed = now })
	if err := source.Merge(st, source.Name, []factory.Item{open}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(row.ID); got.DoneHow != "" {
		t.Fatalf("reopen did not return the row: state=%s", got.State)
	}
	stale := closedTestRow(func(it *factory.Item) { it.DoneHow, it.Title, it.Changed = "closed", "", now.Add(-time.Minute) })
	if err := source.Merge(st, source.Name, []factory.Item{stale}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(row.ID); got.DoneHow != "" {
		t.Fatalf("a stale close finished a reopened issue: state=%s how=%q", got.State, got.DoneHow)
	}
}

// A person's Refresh of an issue still closed on GitHub does not reopen it,
// however new the change it read.
func TestRefreshOfAClosedIssueKeepsItDone(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.Create(factory.Item{Product: "acme", Repo: "api", Num: 1, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateShipped, DoneHow: "closed", DoneAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case "/repos/acme/api/issues/1":
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "title": "t", "state": "closed", "updated_at": time.Now(), "user": map[string]any{"login": "a"}})
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	src := source.New(forge.NewClient("refresh-closed").WithBaseURL(srv.URL), nil, nil)
	fresh, err := src.Refetch(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Merge(st, source.Name, []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(row.ID); got.DoneHow != "closed" {
		t.Fatalf("refresh reopened an issue closed on GitHub: state=%s", got.State)
	}
}

// A close read before a newer open read does not finish the row, reopened or not.
func TestADelayedCloseDoesNotBeatANewerOpenRead(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row, err := st.Create(closedTestRow(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Merge(st, source.Name, []factory.Item{closedTestRow(func(it *factory.Item) { it.Changed = now })}); err != nil {
		t.Fatal(err)
	}
	stale := closedTestRow(func(it *factory.Item) { it.DoneHow, it.Title, it.Changed = "closed", "", now.Add(-time.Minute) })
	if err := source.Merge(st, source.Name, []factory.Item{stale}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(row.ID); got.DoneHow != "" {
		t.Fatalf("a delayed close beat a newer open read: state=%s", got.State)
	}
}

// A sweep's closed answer does not finish a row an open read reached while that
// answer was on its way.
func TestASweepAnswerDoesNotBeatAnOpenReadDuringIt(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	mine := func(change func(*factory.Item)) factory.Item {
		it := factory.Item{Product: "acme", Repo: "api", Num: 1, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateNew}
		if change != nil {
			change(&it)
		}
		return it
	}
	row, err := st.Create(mine(nil))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case "/repos/acme/api/issues":
			fmt.Fprint(w, `[]`)
		case "/repos/acme/api/issues/1":
			if err := source.Merge(st, source.Name, []factory.Item{mine(func(it *factory.Item) { it.Changed = time.Now() })}); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"state":"closed"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	src := source.New(forge.NewClient("sweep-race").WithBaseURL(srv.URL), nil, nil)
	if _, err := source.PollOnce(ctx, src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(row.ID); got.DoneHow != "" {
		t.Fatalf("a sweep answer beat a newer open read: state=%s", got.State)
	}
}

// An issue transferred away answers with its new number. That number here is a
// different issue, so the answer must not finish it.
func TestRefreshOfATransferredIssueDoesNotCloseAnother(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case "/repos/acme/api/issues/7":
			json.NewEncoder(w).Encode(map[string]any{"number": 8, "title": "moved", "state": "closed", "updated_at": time.Now(), "user": map[string]any{"login": "a"}})
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	src := source.New(forge.NewClient("transfer").WithBaseURL(srv.URL), nil, nil)
	fresh, err := src.Refetch(context.Background(), factory.Item{Product: "acme", Repo: "api", Num: 7, Kind: factory.KindIssue, Origin: factory.OriginForge})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.DoneHow != "" {
		t.Fatalf("a transferred issue's answer closes number %d in this repository", fresh.Num)
	}
}

