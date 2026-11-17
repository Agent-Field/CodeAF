package serve_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// This fixture captures the published body without writing to a real forge.
type recordedPostSource struct{ got []factory.Action }

func (s *recordedPostSource) Name() string { return "fixture" }
func (s *recordedPostSource) Read(context.Context, string) ([]factory.Item, string, error) {
	return nil, "", nil
}
func (s *recordedPostSource) Write(_ context.Context, action factory.Action) (factory.Receipt, error) {
	s.got = append(s.got, action)
	return factory.Receipt{URL: "https://github.com/owner/repo/pull/42"}, nil
}

// This crosses the approval HTTP door, the real runner's proof gate and the
// post executor, so a receipt cannot pass while the PR cites different consent.
func TestApproveEventBecomesPostStageTrailer(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "owner/repo", Title: "fix", State: factory.StateLanded, Proof: []factory.Claim{{Text: "works", OK: true}}, Stream: &factory.Stream{Spent: 0.42}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runner := run.New(run.Options{Store: st})
	seam := factory.Seam{
		Load:    func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		GitHub:  func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil },
		SignOff: runner.SignOff,
	}
	h := serve.New(seam, serve.Config{Record: rec})
	request := httptest.NewRequest("POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/approve", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	var receipt struct{ Event string }
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || response.Code != 200 || receipt.Event == "" {
		t.Fatalf("approve=%d %s error=%v", response.Code, response.Body, err)
	}
	saved, err := st.Get(it.ID)
	if err != nil || saved.State != factory.StateShipped {
		t.Fatalf("SignOff=%+v error=%v", saved, err)
	}
	// Reopening proves the publisher needs no in-memory approval from the handler.
	reopened, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := &recordedPostSource{}
	ex := run.NewPostExecutorWith(run.PostOptions{Record: reopened, Source: func(string) factory.Source { return src }})
	result, err := ex.Run(context.Background(), run.Job{Item: saved, Stage: factory.Stage{Ask: "pr"}})
	if err != nil || !result.Done || len(src.got) != 1 {
		t.Fatalf("post=%+v writes=%+v error=%v", result, src.got, err)
	}
	want := "Factory-Approval: " + receipt.Event + " @octocat"
	if !strings.HasSuffix(src.got[0].Body, want) || strings.Count(src.got[0].Body, "Factory-Approval:") != 1 {
		t.Fatalf("PR body=%q", src.got[0].Body)
	}
	events, err := reopened.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 2 || events[0].ID != receipt.Event || events[1].Kind != record.Posted || events[1].Cost != 0.42 {
		t.Fatalf("history=%+v error=%v", events, err)
	}
}
