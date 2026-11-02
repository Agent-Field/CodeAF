package serve_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// EVERY JOB ON THE QUEUE SAYS WHICH AREA IT TOUCHED, so a desk can count
// jobs per area across the queue: the top-level directory most of its
// changed files sit under, a repository's declared area when it names them,
// and nothing for a job that touched no file. The job's own read says the
// same.
func TestQueueJobsSayTheAreaTheyTouched(t *testing.T) {
	change := func(paths ...string) *factory.Change {
		c := &factory.Change{}
		for _, p := range paths {
			c.Files = append(c.Files, factory.ChangedFile{Path: p, Status: "modified"})
		}
		return c
	}
	snap := factory.Snapshot{
		Now:   now,
		Repos: []factory.Repo{{Name: "owner/declared", Areas: []string{"tui", "webhooks"}}},
		Items: []factory.Item{
			{ID: 1, Repo: "owner/plain", Num: 1, Kind: factory.KindIssue, Title: "cli", State: factory.StateRunning, Changed: now,
				Change: change("cli/main.go", "cli/flags.go", "docs/cli.md")},
			{ID: 2, Repo: "owner/declared", Num: 2, Kind: factory.KindIssue, Title: "hooks", State: factory.StateRunning, Changed: now,
				Change: change("webhooks/x.go", "tui/y.go", "webhooks/z.go")},
			{ID: 4, Repo: "owner/plain", Num: 4, Kind: factory.KindIssue, Title: "untouched", State: factory.StateNew, Changed: now},
		},
	}
	h := serve.New(factory.Seam{Load: func() (factory.Snapshot, error) { return snap, nil }}, config)
	t.Cleanup(h.Close)

	w := verbRequest(h, "GET", "/v1/queue", "")
	var q struct {
		Jobs []map[string]json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || w.Code != 200 {
		t.Fatalf("queue: %d %s %v", w.Code, w.Body, err)
	}
	want := map[string]string{"1": `"cli/"`, "2": `"webhooks"`}
	if len(q.Jobs) != 3 {
		t.Fatalf("queue jobs = %d: %s", len(q.Jobs), w.Body)
	}
	for _, job := range q.Jobs {
		id := string(job["document_id"])
		if got := string(job["area"]); got != want[id] {
			t.Errorf("job %s area = %s, want %s", id, got, want[id])
		}
	}
	if !strings.Contains(w.Body.String(), `"area":"cli/"`) {
		t.Fatalf("queue JSON does not carry \"area\":\"cli/\": %s", w.Body)
	}

	for id, area := range map[string]string{"1": `"area":"cli/"`, "2": `"area":"webhooks"`} {
		read := verbRequest(h, "GET", "/v1/jobs/"+id, "")
		if read.Code != 200 || !strings.Contains(read.Body.String(), area) {
			t.Fatalf("job %s read: %d, want %s in %s", id, read.Code, area, read.Body)
		}
	}
}
