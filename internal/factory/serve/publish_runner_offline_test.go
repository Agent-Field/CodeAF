package serve

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// Expiry uses the server's deterministic clock. The HTTP question resumes
// publishing with its original consent, rather than launching the pipeline.
func TestPublishRunnerOfflineQuestionResumesTheSameApproval(t *testing.T) {
	for _, mode := range []string{api.FactoryTeam, api.FactorySolo} {
		t.Run(mode, func(t *testing.T) {
			f := peopleV2Setup(t, mode)
			f.h.Close()
			desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
			if err != nil {
				t.Fatal(err)
			}
			doors := lease.NewDoors(desk)
			f.seam.Answer, f.seam.Launch, f.seam.Stop = doors.Answer, doors.Launch, doors.Stop
			f.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
			f.h = New(f.seam, f.cfg)
			defer f.h.Close()
			it := f.add(t, "owner")
			head := strings.Repeat("1", 40)
			if err := f.st.Update(it.ID, func(it *factory.Item) error {
				it.State, it.HeadCommit = factory.StateLanded, head
				it.WorktreeMachine = "assignee-laptop"
				it.Proof = []factory.Claim{{Text: "works", OK: true}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.cfg.Record.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "owner", Commit: head}); err != nil {
				t.Fatal(err)
			}
			if _, err := doors.SignOff(it.ID, false); err != nil {
				t.Fatal(err)
			}
			var worker api.RunnerAttached
			f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "assignee-laptop", Name: "laptop", Owner: "owner"}, 201, &worker)
			var first api.RunnerPull
			f.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, &first)
			if len(first.Signals) != 1 || first.Signals[0].Verb != lease.SignalPublish {
				t.Fatalf("publish signal = %+v", first.Signals)
			}
			f.now = first.Lease.ExpiresAt
			path := fmt.Sprintf("/v1/jobs/%d", it.ID)
			raw := f.request(t, "GET", path, "operator-secret", nil, 200, nil)
			if !strings.Contains(raw, `"kind":"runner_offline"`) || !strings.Contains(raw, "Runner went offline mid-step") {
				t.Fatalf("offline job = %s", raw)
			}
			f.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 204, nil)
			f.request(t, "POST", "/v1/runners/leases/"+first.Lease.ID+"/done", worker.Credential, api.RunnerDone{State: "done"}, 409, nil)
			f.request(t, "POST", path+"/answer", "operator-secret", map[string]string{"choice": factory.ChoiceTryOnceMore}, 200, nil)
			var retry api.RunnerPull
			f.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, &retry)
			if retry.Lease.ID == first.Lease.ID || len(retry.Signals) != 1 || retry.Signals[0].Words != first.Signals[0].Words || retry.Job.HeadCommit != head {
				t.Fatalf("resumed publish = %+v", retry)
			}
		})
	}
}
