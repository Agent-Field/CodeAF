package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A JOB NEVER SHOWS TWO AREAS: the job a verb answers with names the same
// declared area as the queue, so a desk that swaps in a verb's job keeps the
// row it counted.
func TestAVerbsJobAndTheQueueAgreeOnADeclaredArea(t *testing.T) {
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
	repos := []factory.Repo{{Name: "owner/repo", Areas: []string{"tui", "webhooks"}}}
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items, Repos: repos}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil },
	}
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)
	job, err := st.Create(factory.Item{Title: "hooks", Repo: "owner/repo", State: factory.StateQueued,
		Change: &factory.Change{Files: []factory.ChangedFile{
			{Path: "webhooks/x.go", Status: "modified"}, {Path: "webhooks/y.go", Status: "modified"}, {Path: "tui/z.go", Status: "modified"}}}})
	if err != nil {
		t.Fatal(err)
	}

	var queue struct {
		Jobs []struct {
			Area string `json:"area"`
		} `json:"jobs"`
	}
	if w := verbRequest(h, "GET", "/v1/queue", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &queue) != nil || len(queue.Jobs) != 1 {
		t.Fatalf("queue: %d %s", w.Code, w.Body)
	}
	var taken struct {
		Area string `json:"area"`
	}
	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", job.ID), "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &taken) != nil {
		t.Fatalf("take: %d %s", w.Code, w.Body)
	}
	if queue.Jobs[0].Area != "webhooks" || taken.Area != queue.Jobs[0].Area {
		t.Fatalf("queue area %q, take's job area %q; want both webhooks", queue.Jobs[0].Area, taken.Area)
	}
}
