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

func (s *webSeat) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
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
			_, _ = worker.Run(ctx, *store.Task(store.RootID()))
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
	write := func(pin string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{config.KeySearchProvider: pin})
		if err := os.WriteFile(config.BudgetConfigPath(profile), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("exa")
	factory := CrewFactory(nil, t.TempDir(), profile, Seats{One: "test/model"}, "", func(string) session.Completer { return &webSeat{} })
	worker := factory(plandb.Task{}).(*BashWorker)
	if worker.searchProvider == nil || worker.searchFetcher == nil {
		t.Fatal("run worker lost the web pair")
	}
	if got := worker.searchProvider.Name(); got != "exa" {
		t.Fatalf("search=%q, want profile pin exa", got)
	}
	write("duckduckgo")
	if got := worker.searchProvider.Name(); got != "duckduckgo" {
		t.Fatalf("search=%q, want changed pin duckduckgo", got)
	}
}
