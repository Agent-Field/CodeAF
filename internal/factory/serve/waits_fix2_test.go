package serve

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// W1, W4: closing an unworked issue blocks existing children at the scan.
func TestWaitsClosedIssueBlocks(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.set(t, child.ID, []int{parent.ID}, 200)
	f.edit(t, parent.ID, func(it *factory.Item) { it.State, it.DoneHow = factory.StateShipped, "closed" })
	f.scan(t)
	got := f.readJob(t, child.ID)
	if got.State != "backlog" || got.Blocked == nil || got.Blocked.Reason != "waits on api#1107, which closed without merging" || !reflect.DeepEqual(got.WaitingOn, []int{parent.ID}) || f.item(t, child.ID).WaitsReleased || f.launches != 0 {
		t.Fatalf("closed issue released child: %+v", got)
	}
}

// W2: a closed issue is refused before any document or revision changes.
func TestWaitsSetRefusesClosedIssue(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.edit(t, parent.ID, func(it *factory.Item) { it.State, it.DoneHow = factory.StateShipped, "closed" })
	before := f.item(t, child.ID)
	revision := waitsRevision(t, f, child.ID)
	answer := f.set(t, child.ID, []int{parent.ID}, 422)
	if !strings.Contains(answer, "closed without merging") || !reflect.DeepEqual(f.item(t, child.ID), before) || waitsRevision(t, f, child.ID) != revision {
		t.Fatalf("closed issue refusal = %s", answer)
	}
}

// W1, W2: a published open pull request is accepted and waits for its merge.
func TestWaitsPublishedParentWaitsForMerge(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.edit(t, parent.ID, func(it *factory.Item) {
		it.State = factory.StateShipped
		it.PullRequest = &factory.PullRequest{State: factory.PROpen}
	})
	f.set(t, child.ID, []int{parent.ID}, 200)
	f.scan(t)
	if it := f.item(t, child.ID); it.WaitsReleased || it.Blocked != nil || !reflect.DeepEqual(it.WaitingOn, []int{parent.ID}) {
		t.Fatalf("open pull request did not stay waiting: %+v", it)
	}
	f.merge(t, parent.ID)
	f.scan(t)
	if it := f.item(t, child.ID); !it.WaitsReleased || it.Blocked != nil || len(it.WaitingOn) != 0 {
		t.Fatalf("merged parent did not release: %+v", it)
	}
}

// W5, W6: real person doors retire waits before Drop and Bring back erase a run.
func TestWaitsStartDropRestoreBeforeScan(t *testing.T) {
	for _, door := range []string{"run", "start_now"} {
		for _, blocked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/blocked=%v", door, blocked), func(t *testing.T) {
				f := waitsSetup(t)
				parent, child := f.addJob(t, 1107), f.addJob(t, 0)
				f.set(t, child.ID, []int{parent.ID}, 200)
				if blocked {
					f.edit(t, parent.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test"} })
					f.scan(t)
				}
				f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/%s", child.ID, door), "operator-secret", nil, 200, nil)
				if it := f.item(t, child.ID); !it.WaitsReleased || it.Blocked != nil || len(it.WaitingOn) != 0 || f.launches != 1 {
					t.Fatalf("start did not retire waits immediately: %+v", it)
				}
				f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/drop", child.ID), "operator-secret", nil, 200, nil)
				f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/restore", child.ID), "operator-secret", nil, 200, nil)
				before := f.item(t, child.ID)
				if factory.JobState(before) != "backlog" || !before.WaitsReleased {
					t.Fatalf("restored job = %+v", before)
				}
				f.edit(t, parent.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test"} })
				f.scan(t)
				f.merge(t, parent.ID)
				f.scan(t)
				if got := f.item(t, child.ID); !reflect.DeepEqual(got, before) || f.launches != 1 {
					t.Fatalf("restored waits became active: %+v", got)
				}
				for _, event := range before.Activity {
					if strings.HasPrefix(event.What, "waits over:") {
						t.Fatalf("person start recorded a release: %+v", event)
					}
				}
			})
		}
	}
}

// W5: per-job launch doors also persist retirement before entering the runner.
func TestWaitsForJobLaunchRetiresBeforeLaunch(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.set(t, child.ID, []int{parent.ID}, 200)
	f.h.Close()
	f.seam.ForJob = func(int) factory.Seam {
		return factory.Seam{Launch: func(id int) error {
			it := f.item(t, id)
			if !it.WaitsReleased || len(it.WaitingOn) != 0 || it.Blocked != nil {
				t.Fatalf("runner entered before waits retired: %+v", it)
			}
			return lease.NewDoors(f.h.workers.desk).Launch(id)
		}}
	}
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.h.stopWaits()
	if err := f.h.seam.Job(child.ID).Launch(child.ID); err != nil {
		t.Fatal(err)
	}
}

func waitsTeamSetup(t *testing.T) (*waitsFixture, string, string) {
	t.Helper()
	f := &waitsFixture{peopleV2Fixture: peopleV2Setup(t, api.FactoryTeam)}
	f.h.Close()
	if err := f.st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
		t.Fatal(err)
	}
	g := &scriptGitHub{t: t, allow: map[string]map[string]bool{
		"writer": {"acme/api": true, "acme/web": true},
		"reader": {"acme/api": true},
	}}
	f.cfg.GitHubClientID, f.cfg.GitHubHTTPClient = "client", g.client()
	f.cfg.AccessToken = "floor-secret"
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.h.stopWaits()
	writer := signViewer(t, f.h, g, &f.now, "writer")
	reader := signViewer(t, f.h, g, &f.now, "reader")
	return f, writer, reader
}

func waitsTeamJob(t *testing.T, f *waitsFixture, repo, owner string, num int) factory.Item {
	t.Helper()
	it, err := f.st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Repo: repo, Assignee: owner, Num: num, Title: "test waits"})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func waitsRevision(t *testing.T, f *waitsFixture, id int) int {
	t.Helper()
	revision, err := f.st.Revision(id)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

// W2, W6: a hidden parent has the unknown-parent answer and makes no write.
func TestWaitsSetHidesUnreadableParent(t *testing.T) {
	f, _, reader := waitsTeamSetup(t)
	parent := waitsTeamJob(t, f, "acme/web", "writer", 1107)
	child := waitsTeamJob(t, f, "acme/api", "reader", 1108)
	before := f.item(t, child.ID)
	revision := waitsRevision(t, f, child.ID)
	path := fmt.Sprintf("/v1/jobs/%d/waits_on", child.ID)
	body := fmt.Sprintf(`{"waits_on":[%d]}`, parent.ID)
	for _, ended := range []bool{false, true} {
		if ended {
			f.edit(t, parent.ID, func(it *factory.Item) { it.State, it.DoneHow = factory.StateDismissed, "dropped" })
		}
		hidden := identityRequest(f.h, "PUT", path, body, reader, nil)
		if hidden.Code != 422 || !strings.Contains(hidden.Body.String(), fmt.Sprintf("job %d does not exist in this factory", parent.ID)) || strings.Contains(hidden.Body.String(), "web") || !reflect.DeepEqual(f.item(t, child.ID), before) {
			t.Fatalf("hidden parent = %d %s", hidden.Code, hidden.Body)
		}
		missing := identityRequest(f.h, "PUT", path, `{"waits_on":[99999]}`, reader, nil)
		var hiddenError, missingError struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(hidden.Body.Bytes(), &hiddenError); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(missing.Body.Bytes(), &missingError); err != nil {
			t.Fatal(err)
		}
		if missing.Code != 422 || strings.ReplaceAll(hiddenError.Error, fmt.Sprint(parent.ID), "99999") != missingError.Error || !reflect.DeepEqual(f.item(t, child.ID), before) || waitsRevision(t, f, child.ID) != revision {
			t.Fatalf("hidden and missing differ: %s / %s", hidden.Body, missing.Body)
		}
	}
}

// W1, W3, W4, W11: job, queue and activity reads carry no other repo's details.
func TestWaitsReadsHideUnreadableParent(t *testing.T) {
	f, writer, reader := waitsTeamSetup(t)
	parent := waitsTeamJob(t, f, "acme/web", "writer", 1107)
	child := waitsTeamJob(t, f, "acme/api", "writer", 1108)
	f.request(t, "PUT", fmt.Sprintf("/v1/jobs/%d/waits_on", child.ID), writer, map[string]any{"waits_on": []int{parent.ID}}, 200, nil)
	check := func(reason string) api.Job {
		t.Helper()
		var job api.Job
		for _, path := range []string{fmt.Sprintf("/v1/jobs/%d", child.ID), "/v1/queue"} {
			w := identityRequest(f.h, "GET", path, "", reader, nil)
			if w.Code != 200 || strings.Contains(w.Body.String(), "web") || strings.Contains(w.Body.String(), "Private check") {
				t.Fatalf("parent details in %s: %d %s", path, w.Code, w.Body)
			}
			if path == "/v1/queue" {
				var queue api.Queue
				if err := json.Unmarshal(w.Body.Bytes(), &queue); err != nil {
					t.Fatal(err)
				}
				if len(queue.Jobs) != 1 || queue.Jobs[0].ID != child.ID || !reflect.DeepEqual(queue.Jobs[0].Blocked, job.Blocked) {
					t.Fatalf("queue differs from child read: %+v", queue.Jobs)
				}
			} else if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
		}
		if reason != "" && (job.Blocked == nil || job.Blocked.Reason != reason) {
			t.Fatalf("blocked = %+v", job.Blocked)
		}
		return job
	}
	f.edit(t, parent.ID, func(it *factory.Item) {
		it.Failure = &factory.Failure{Step: "check"}
		it.Stages = []factory.Stage{{Name: "check", Label: "Private check"}}
	})
	f.scan(t)
	check(fmt.Sprintf("waits on job %d, which failed", parent.ID))
	f.edit(t, parent.ID, func(it *factory.Item) { it.State, it.DoneHow = factory.StateShipped, "closed" })
	f.scan(t)
	check(fmt.Sprintf("waits on job %d, which closed without merging", parent.ID))
	f.merge(t, parent.ID)
	f.scan(t)
	job := check("")
	it := f.item(t, child.ID)
	if job.Blocked != nil || !it.WaitsReleased || len(it.Activity) == 0 || it.Activity[len(it.Activity)-1].What != fmt.Sprintf("waits over: job %d merged", parent.ID) {
		t.Fatalf("release = %+v", it)
	}
}
