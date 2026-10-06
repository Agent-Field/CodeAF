package run

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/search"
	"github.com/Agent-Field/codeaf/internal/session"
)

type webSeat struct{ calls int }

func (s *webSeat) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	s.calls++
	name, args := "web_search", `{"query":"release notes","count":2}`
	if s.calls > 1 {
		name, args = "web_fetch", `{"url":"https://example.com/notes"}`
	}
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: name, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: args}}}}}}}, nil
}

type workerWeb struct {
	queries, pages int
	fail           bool
}

func (*workerWeb) Name() string { return "worker-web-fixture" }
func (w *workerWeb) Search(ctx context.Context, query string, count int) ([]search.Result, error) {
	w.queries++
	if query != "release notes" || count != 2 {
		return nil, errors.New("wrong search arguments")
	}
	return []search.Result{{Title: "Release notes", URL: "https://example.com/notes", Snippet: "Read the release notes"}}, nil
}
func (w *workerWeb) Fetch(ctx context.Context, url string) (string, error) {
	w.pages++
	if w.fail {
		return "", errors.New("fixture page unavailable")
	}
	if url != "https://example.com/notes" {
		return "", errors.New("wrong fetch URL")
	}
	return "The release adds scoped initialization.", nil
}

// The actual run worker must execute native web actions under the single-call
// envelope and preserve their identities and observations in the trajectory.
func TestBashWorkerRunsAndRecordsNativeWebTools(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fetch failure"}[failed], func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			root := t.TempDir()
			t.Setenv("CODEAF_HOME", root)
			stub := filepath.Join(root, "stub-codeaf")
			if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_PLANDB_BIN", stub)
			store, err := plandb.Open(filepath.Join(root, "plan.db"), "web", "root", "Research", "Research release notes")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			web := &workerWeb{fail: failed}
			worker := NewBashWorker(store, root, "test/model", "", &webSeat{})
			worker.searchProvider, worker.searchFetcher = web, web
			ctx, cancel := context.WithTimeout(WithStepsPerTask(t.Context(), 2), 10*time.Second)
			defer cancel()
			// Two actions exhaust this test's allowance; task completion is proved by
			// the live acceptance run, not by a fabricated finish command here.
			report, runErr := worker.Run(ctx, *store.Task(store.RootID()))
			if runErr == nil || !strings.Contains(runErr.Error(), "stopped at its step cap after 2 steps") || report.Steps != 2 {
				t.Fatalf("worker did not stop at the declared allowance: report=%+v err=%v", report, runErr)
			}
			steps, err := Trajectory(root, store.RootID())
			if err != nil {
				t.Fatal(err)
			}
			if web.queries != 1 || web.pages != 1 || len(steps) != 2 {
				t.Fatalf("search=%d fetch=%d steps=%+v", web.queries, web.pages, steps)
			}
			for i, name := range []string{"web_search", "web_fetch"} {
				if steps[i].Tool != name || steps[i].NotRun || steps[i].ExitCode != nil {
					t.Fatalf("native action was not recorded honestly: %+v", steps[i])
				}
				if !json.Valid([]byte(steps[i].Command)) {
					t.Fatalf("arguments lost: %+v", steps[i])
				}
			}
			want := "The release adds scoped initialization."
			if failed {
				want = "Fetch failed (worker-web-fixture): fixture page unavailable"
			}
			if !strings.Contains(steps[1].Observation, want) {
				t.Fatalf("fetch observation=%q, want %q", steps[1].Observation, want)
			}
		})
	}
}

// Both doors use CrewFactory. Its workers must bind the named profile, rather
// than the process default, and observe settings changed after construction.
func TestCrewFactoryWiresLiveWebFromItsProfile(t *testing.T) {
	profile := t.TempDir()
	for _, key := range []string{"EXA_API_KEY", "FIRECRAWL_API_KEY", "JINA_API_KEY"} {
		t.Setenv(key, "")
	}
	write := func(pin, exaKey string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{config.KeySearchProvider: pin, config.KeyExaKey: exaKey})
		if err := os.WriteFile(config.BudgetConfigPath(profile), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("exa", "fixture-key")
	factory := CrewFactory(nil, t.TempDir(), profile, Seats{One: "test/model"}, "", func(string) session.Completer { return &webSeat{} })
	worker := factory(plandb.Task{}).(*BashWorker)
	if worker.searchProvider == nil || worker.searchFetcher == nil {
		t.Fatal("run worker lost the web pair")
	}
	if got := worker.searchProvider.Name(); got != "exa" {
		t.Fatalf("search=%q, want profile pin exa", got)
	}
	if got := worker.searchFetcher.Name(); got != "exa-fetch" {
		t.Fatalf("fetch=%q, want the keyed provider from this profile", got)
	}
	write("duckduckgo", "")
	if got := worker.searchProvider.Name(); got != "duckduckgo" {
		t.Fatalf("search=%q, want changed pin duckduckgo", got)
	}
	if got := worker.searchFetcher.Name(); got != "jina" {
		t.Fatalf("fetch=%q, want the keyless fetcher after the profile key was removed", got)
	}
}

// Cancelling a task must reach an in-flight native network action. A handshake
// makes cancellation happen after the backend starts, without a clock delay.
type waitingWorkerSearch struct{ entered, cancelled chan struct{} }

func (*waitingWorkerSearch) Name() string { return "waiting-worker-search" }
func (w *waitingWorkerSearch) Search(ctx context.Context, _ string, _ int) ([]search.Result, error) {
	close(w.entered)
	<-ctx.Done()
	close(w.cancelled)
	return nil, ctx.Err()
}

func TestBashWorkerCancellationReachesNativeWebBackend(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	stub := filepath.Join(root, "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)
	store, err := plandb.Open(filepath.Join(root, "plan.db"), "cancel-web", "root", "Research", "Research release notes")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := &waitingWorkerSearch{entered: make(chan struct{}), cancelled: make(chan struct{})}
	worker := NewBashWorker(store, root, "test/model", "", &webSeat{})
	worker.searchProvider = backend
	guard, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(guard)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := worker.Run(ctx, *store.Task(store.RootID())); done <- err }()
	select {
	case <-backend.entered:
	case <-guard.Done():
		t.Fatal("native search never started")
	}
	cancel()
	select {
	case <-backend.cancelled:
	case <-guard.Done():
		t.Fatal("task cancellation never reached native search")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled task returned success")
		}
	case <-guard.Done():
		t.Fatal("worker did not finish after its native search was cancelled")
	}
}
