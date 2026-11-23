package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestStaleExpectNamesWhoMovedTheJob(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 11, 11, 58, 0, 0, time.UTC)
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return when }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store, cfg.Record = st, rec
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items, Now: when}, err
		},
		Launch: func(int) error { t.Fatal("stale run reached the door"); return nil },
		Steer:  func(int, string) error { t.Fatal("stale steer reached the door"); return nil },
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "sam"}, nil
		},
	}
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)
	running, err := st.Create(factory.Item{Title: "moving", Repo: "owner/repo", State: factory.StateRunning, Changed: when})
	if err != nil {
		t.Fatal(err)
	}
	held, err := st.Create(factory.Item{Title: "theirs", Repo: "owner/repo", State: factory.StateQueued, Assignee: "mei", Changed: when})
	if err != nil {
		t.Fatal(err)
	}
	done, err := st.Create(factory.Item{Title: "shipped", Repo: "owner/repo", State: factory.StateShipped, Changed: when})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Approved, Job: done.ID, Handle: "raj"}); err != nil {
		t.Fatal(err)
	}

	moved := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/steer", running.ID), `{"words":"go on","expect":{"state":"needs_you"}}`)
	checkConflict(t, moved, "stale", "job moved on while you looked", "running")

	taken := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", held.ID), `{"expect":{"state":"running"}}`)
	checkConflict(t, taken, "stale", "already taken by @mei", "queued")
	var body struct {
		By string `json:"by"`
	}
	if err := json.Unmarshal(taken.Body.Bytes(), &body); err != nil || body.By != "@mei" {
		t.Fatalf("taken by = %s %v", taken.Body, err)
	}

	approved := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/approve", done.ID), `{"expect":{"state":"needs_you"}}`)
	checkConflict(t, approved, "stale", "already approved by @raj at 11:58", "done")
	var stamp struct {
		By string    `json:"by"`
		At time.Time `json:"at"`
	}
	if err := json.Unmarshal(approved.Body.Bytes(), &stamp); err != nil || stamp.By != "@raj" || !stamp.At.Equal(when) {
		t.Fatalf("approved stamp = %s %v", approved.Body, err)
	}

	staleVer := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/run", running.ID), `{"expect":{"version":9}}`)
	checkConflict(t, staleVer, "stale", "job moved on while you looked", "running")
}

func checkConflict(t *testing.T, w *httptest.ResponseRecorder, reason, sentence, state string) {
	t.Helper()
	checkJSON(t, w, http.StatusConflict)
	var body struct {
		Error    string `json:"error"`
		Reason   string `json:"reason"`
		NowState string `json:"now_state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != sentence || body.Reason != reason || body.NowState != state {
		t.Fatalf("conflict = %+v, want %s %s %s", body, reason, sentence, state)
	}
}

func TestUnprovenApproveWritesNothing(t *testing.T) {
	cfg := recordConfig(t)
	s, calls := spyVerbs(t, nil)
	s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
	w := verbRequest(serve.New(s, cfg), "POST", "/v1/jobs/9/approve", "")
	checkJSON(t, w, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "claim not shown") || !strings.Contains(w.Body.String(), `"reason":"conflict"`) || !strings.Contains(w.Body.String(), `"now_state":"needs_you"`) || len(*calls) != 0 {
		t.Fatalf("unproven approve = %s calls %v", w.Body, *calls)
	}
	events, err := cfg.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 0 {
		t.Fatalf("receipt after a refused claim: %+v %v", events, err)
	}
}

func TestAnswerRefusalLeavesNoApproval(t *testing.T) {
	cfg := recordConfig(t)
	s := factory.FixtureSeam(now)
	s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
	s.Answer = func(int, bool, string) error { return fmt.Errorf("#1 is not waiting on you") }
	w := verbRequest(serve.New(s, cfg), "POST", "/v1/jobs/1/answer", `{"yes":true}`)
	checkJSON(t, w, http.StatusConflict)
	events, err := cfg.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 0 {
		t.Fatalf("receipt after a refused answer: %+v %v", events, err)
	}
}

func TestReleaseClearsTheTakeAndRefusesSomeoneElse(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store, cfg.Record = st, rec
	login := "me"
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: login}, nil
		},
	}
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)
	free, err := st.Create(factory.Item{Title: "open", Repo: "owner/repo", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := st.Create(factory.Item{Title: "mine", Repo: "owner/repo", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/release", free.ID), "")
	checkConflict(t, w, "conflict", "job is not taken", "queued")

	if w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", mine.ID), ""); w.Code != 200 {
		t.Fatalf("take: %d %s", w.Code, w.Body)
	}
	login = "other"
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/release", mine.ID), "")
	checkJSON(t, w, http.StatusForbidden)
	if !strings.Contains(w.Body.String(), "only @me can release this job") || !strings.Contains(w.Body.String(), `"reason":"not_allowed"`) {
		t.Fatalf("release by someone else: %s", w.Body)
	}
	login = "me"
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/release", mine.ID), "")
	checkJSON(t, w, 200)
	read, err := st.Get(mine.ID)
	if err != nil || read.Taker != "" {
		t.Fatalf("taker after release: %+v %v", read, err)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 2 || events[1].Kind != record.Released || events[1].Handle != "me" {
		t.Fatalf("audit: %+v %v", events, err)
	}
}

func TestDropRecordsAndStopsTheHolder(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store, cfg.Record, cfg.PullWait = st, rec, time.Millisecond
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		Dismiss: func(id int) error {
			return st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateDismissed
				return nil
			})
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "sam"}, nil
		},
	}
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)
	it, err := st.Create(factory.Item{Title: "running", Repo: "owner/repo", State: factory.StateQueued, Assignee: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	attached := verbRequest(h, "POST", "/v1/workers/attach", `{"name":"laptop","handle":"sam"}`)
	checkJSON(t, attached, http.StatusCreated)
	var worker api.WorkerAttached
	if err := json.Unmarshal(attached.Body.Bytes(), &worker); err != nil {
		t.Fatal(err)
	}
	pull := authed(h, "GET", "/v1/workers/pull", worker.Credential, "")
	checkJSON(t, pull, 200)
	var held struct {
		Lease api.WorkerLease `json:"lease"`
	}
	if err := json.Unmarshal(pull.Body.Bytes(), &held); err != nil || held.Lease.ID == "" {
		t.Fatalf("pull: %s %v", pull.Body, err)
	}
	dropped := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/drop", it.ID), `{}`)
	checkJSON(t, dropped, 200)
	read, err := st.Get(it.ID)
	if err != nil || read.State != factory.StateDismissed {
		t.Fatalf("after drop: %+v %v", read, err)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 2 || events[1].Kind != record.Dropped || events[1].Detail != "" || events[0].Kind != record.RunnerAdded {
		t.Fatalf("audit: %+v %v", events, err)
	}
	beat := authed(h, "POST", "/v1/workers/leases/"+held.Lease.ID+"/heartbeat", worker.Credential, "")
	checkJSON(t, beat, 409)
	if !strings.Contains(beat.Body.String(), "lease is no longer live") {
		t.Fatalf("dropped runner remained live: %s", beat.Body)
	}
}

func TestBadInputCarriesAReason(t *testing.T) {
	s, _ := spyVerbs(t, nil)
	w := verbRequest(serve.New(s, config), "POST", "/v1/jobs/1/answer", `{}`)
	checkJSON(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), `"reason":"bad_input"`) {
		t.Fatalf("bad input: %s", w.Body)
	}
	missing := verbRequest(serve.New(factory.FixtureSeam(now), config), "GET", "/v1/jobs/999", "")
	checkJSON(t, missing, http.StatusNotFound)
	if !strings.Contains(missing.Body.String(), `"reason":"not_found"`) {
		t.Fatalf("missing: %s", missing.Body)
	}
}

func authed(h http.Handler, method, path, credential, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
