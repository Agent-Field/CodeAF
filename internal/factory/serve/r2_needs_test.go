package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestCanAndNotJobPerson(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	owned, err := st.Create(factory.Item{
		Title: "caps", Repo: "owner/repo", State: factory.StateNeedsYou, Assignee: "sam", Changed: when,
		Question: "budget of $5 reached · $5 more, or stop?", QKind: "cap", Cap: 5,
		Stream: &factory.Stream{Spent: 5, Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseWaiting}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	open, err := st.Create(factory.Item{
		Title: "ask", Repo: "owner/repo", State: factory.StateNeedsYou, Changed: when,
		Question: "which file?", QKind: "step",
	})
	if err != nil {
		t.Fatal(err)
	}
	login := "mei"
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		Launch:  func(int) error { return nil },
		Dismiss: func(int) error { t.Fatal("someone else cancelled"); return nil },
		Answer:  func(int, bool, string) error { t.Fatal("someone else answered the cap"); return nil },
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: login}, nil
		},
	}
	h := serve.New(seam, config)
	t.Cleanup(h.Close)

	var job struct {
		Can api.Can `json:"can"`
	}
	w := verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", owned.ID), "")
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 200 {
		t.Fatalf("job = %d %s %v", w.Code, w.Body, err)
	}
	if job.Can.Approve || job.Can.RunAgain || job.Can.CancelJob || job.Can.RetryPublish || job.Can.Answer || job.Can.AssignToMe || !job.Can.Reply {
		t.Fatalf("mei can = %+v", job.Can)
	}
	for _, verb := range []string{"run", "drop", "approve"} {
		w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", owned.ID, verb), "")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"reason":"not_job_person"`) || !strings.Contains(w.Body.String(), `"job_person":"@sam"`) {
			t.Fatalf("%s = %d %s", verb, w.Code, w.Body)
		}
	}
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", owned.ID), `{"choice":"continue"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "only @sam can answer this") || !strings.Contains(w.Body.String(), `"job_person":"@sam"`) {
		t.Fatalf("answer = %d %s", w.Code, w.Body)
	}
	w = verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", open.ID), "")
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || !job.Can.Answer || !job.Can.AssignToMe || !job.Can.Reply || !job.Can.Approve {
		t.Fatalf("open can = %+v %v", job.Can, err)
	}

	login = "sam"
	w = verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", owned.ID), "")
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || !job.Can.Approve || !job.Can.RunAgain || !job.Can.CancelJob || !job.Can.Answer || job.Can.AssignToMe {
		t.Fatalf("sam can = %+v %v", job.Can, err)
	}
}

func TestConflictNamesWhoWhenAndWhere(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 11, 11, 58, 0, 0, time.UTC)
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	held, err := st.Create(factory.Item{
		Title: "theirs", Repo: "owner/repo", State: factory.StateQueued, Assignee: "mei",
		Changed: when, WorktreeMachine: "priya-mbp",
	})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := st.Create(factory.Item{
		Title: "proof", Repo: "owner/repo", State: factory.StateLanded, Assignee: "sam",
		Changed: when, HeadCommit: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "sam"}, nil
		},
	}
	cfg := config
	cfg.Store = st
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)

	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/take", held.ID), "")
	checkJSON(t, w, http.StatusConflict)
	var taken struct {
		Error   string    `json:"error"`
		Taker   string    `json:"taker"`
		At      time.Time `json:"at"`
		Machine string    `json:"machine"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &taken); err != nil {
		t.Fatal(err)
	}
	if taken.Error != "already taken by @mei" || taken.Taker != "@mei" || taken.Machine != "priya-mbp" || !taken.At.Equal(when) {
		t.Fatalf("take conflict = %+v", taken)
	}

	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/approve", moved.ID), `{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	checkJSON(t, w, http.StatusConflict)
	var changed struct {
		Reason        string    `json:"reason"`
		By            string    `json:"by"`
		At            time.Time `json:"at"`
		Commit        string    `json:"commit"`
		CurrentCommit string    `json:"current_commit"`
		HeadCommit    string    `json:"head_commit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.Reason != "changed_since_opened" || changed.By != "@sam" || !changed.At.Equal(when) || changed.Commit != head || changed.CurrentCommit != head || changed.HeadCommit != head {
		t.Fatalf("commit conflict = %+v", changed)
	}
}

func TestAReportedStepFailureIsARecord(t *testing.T) {
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
	seam := factory.Seam{Load: func() (factory.Snapshot, error) {
		items, err := st.List()
		return factory.Snapshot{Items: items}, err
	}}
	h := serve.New(seam, cfg)
	t.Cleanup(h.Close)
	job, err := st.Create(factory.Item{
		Title: "fix", Repo: "owner/repo", State: factory.StateQueued, Assignee: "sam",
		Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true, Until: factory.UntilDone, Max: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := verbRequest(h, "POST", "/v1/workers/attach", `{"name":"laptop","handle":"sam","machine":"box"}`)
	checkJSON(t, w, http.StatusCreated)
	var attached api.WorkerAttached
	if err := json.Unmarshal(w.Body.Bytes(), &attached); err != nil {
		t.Fatal(err)
	}
	pull := authed(h, "GET", "/v1/workers/pull", attached.Credential, "")
	checkJSON(t, pull, 200)
	var held struct {
		Lease api.WorkerLease `json:"lease"`
		Job   api.Job         `json:"job"`
	}
	if err := json.Unmarshal(pull.Body.Bytes(), &held); err != nil || held.Lease.ID == "" || held.Job.ID != job.ID {
		t.Fatalf("pull = %s %v", pull.Body, err)
	}
	when := now
	body := api.WorkerReport{Steps: []api.StepReport{{
		Name:  "proof",
		Round: 2,
		Rounds: []api.RoundLine{
			{Round: 1, Summary: "2 findings", At: when},
			{Round: 2, Summary: "still red", At: when},
		},
		Failure: &api.Failure{Code: factory.CodeToolError, Detail: "still red", Attempts: 2, Machine: "box", Next: factory.FailureStopped, At: when},
	}}}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	reported := authed(h, "POST", "/v1/workers/leases/"+held.Lease.ID+"/report", attached.Credential, string(raw))
	checkJSON(t, reported, 200)
	it, err := st.Get(job.ID)
	if err != nil || it.Failure == nil || it.Failure.Attempts != 2 || it.Failure.Machine != "box" || it.Failure.Next != factory.FailureStopped {
		t.Fatalf("job failure = %+v %v", it.Failure, err)
	}
	ph := it.Stream.Phases[0]
	if len(ph.Rounds) != 2 || ph.Rounds[1].Summary != "still red" || ph.Failure == nil {
		t.Fatalf("step = %+v", ph)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.StepFailed || events[0].Job != job.ID {
		t.Fatalf("record = %+v %v", events, err)
	}
}
