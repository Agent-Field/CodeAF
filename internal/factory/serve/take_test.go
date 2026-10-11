package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Take names the connected person on an unassigned job and refuses a job the
// person rule already gave to someone else. The wire carries who holds it as
// assigned_to and desk.taker; the refusal is a sentence at 409.
func TestTakeAssignsMeAndRefusesAJobSomeoneElseHolds(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store = st
	cfg.Record = rec
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "me"}, nil
		},
	}
	h := serve.New(seam, cfg)
	free, err := st.Create(factory.Item{Title: "open work", Repo: "owner/repo", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	held, err := st.Create(factory.Item{Title: "their work", Repo: "owner/repo", State: factory.StateQueued, Assignee: "other"})
	if err != nil {
		t.Fatal(err)
	}

	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", free.ID), "")
	checkJSON(t, w, 200)
	got := decodeTaken(t, w.Body.Bytes())
	if got.AssignedTo != "me" || got.Desk.Taker != "me" || got.State != "queued" {
		t.Fatalf("take = %+v", got)
	}
	again := verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", free.ID), "")
	checkJSON(t, again, 200)
	if read := decodeTaken(t, again.Body.Bytes()); read.AssignedTo != "me" || read.Desk.Taker != "me" {
		t.Fatalf("job after take = %+v", read)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.Took || events[0].Job != free.ID || events[0].Handle != "me" {
		t.Fatalf("record = %+v, %v", events, err)
	}

	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", held.ID), "")
	checkJSON(t, w, 409)
	var refused struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Error != "already taken by @other" || !strings.Contains(refused.Error, " ") {
		t.Fatalf("refusal = %q", refused.Error)
	}
	events, err = rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("refusal was recorded: %+v, %v", events, err)
	}
	still := verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", held.ID), "")
	checkJSON(t, still, 200)
	if read := decodeTaken(t, still.Body.Bytes()); read.AssignedTo != "other" || read.Desk.Taker != "" {
		t.Fatalf("held job changed: %+v", read)
	}
}

func TestTakeRecordsTheMachine(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store = st
	cfg.Record = rec
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "me"}, nil
		},
	}
	h := serve.New(seam, cfg)
	w := verbRequest(h, "POST", "/v1/workers/attach", `{"machine":"priya-mbp","name":"laptop","handle":"me"}`)
	checkJSON(t, w, 201)
	job, err := st.Create(factory.Item{Title: "open work", Repo: "owner/repo", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", job.ID), "")
	checkJSON(t, w, 200)
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.Took || events[0].Detail != "priya-mbp" {
		t.Fatalf("record = %+v, %v", events, err)
	}
}

type takenJob struct {
	AssignedTo string `json:"assigned_to"`
	State      string `json:"state"`
	Desk       struct {
		Taker string `json:"taker"`
	} `json:"desk"`
}

func decodeTaken(t *testing.T, data []byte) takenJob {
	t.Helper()
	var got takenJob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
