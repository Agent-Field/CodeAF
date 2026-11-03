package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestBringBackKeepsLifetimeMoney(t *testing.T) {
	for _, stream := range []*factory.Stream{nil, {Spent: .138}} {
		f := newV2Jobs(t)
		it := f.create(t, factory.Item{Title: "Keep money", State: factory.StateDismissed, DoneHow: "dropped", ChatSpent: .03, Stream: stream})
		f.post(t, it, "restore", "", 200)
		saved, err := f.st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := .03
		if stream != nil {
			want = .168
		}
		if got := factory.JobLifetimeSpent(saved); math.Abs(got-want) > 1e-9 {
			t.Fatalf("bring back lifetime = %g, want %g", got, want)
		}
		if saved.Stream != nil || saved.ChatSpent != 0 {
			t.Fatal("work was not cleared")
		}
	}
}

func TestRunAsNewStartsWithoutLifetimeMoney(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.NewJob = func(repo, title, text string) (int, error) {
		it, err := f.st.Create(factory.Item{Repo: repo, Title: title, Body: text, State: factory.StateNew})
		return it.ID, err
	}
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{Title: "New copy", State: factory.StateShipped, DoneHow: "answered", SpentBefore: .138, Stream: &factory.Stream{Spent: .03, Ended: now}})
	w := f.post(t, it, "run-as-new", "", 201)
	var out struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	saved, err := f.st.Get(out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.FromJob != it.ID || saved.Num != 0 || factory.LedgerName(saved) == factory.LedgerName(it) {
		t.Fatalf("Run as new must have a distinct ledger identity: source=%q copy=%q from=%d", factory.LedgerName(it), factory.LedgerName(saved), saved.FromJob)
	}
	if got := factory.JobLifetimeSpent(saved); got != 0 {
		t.Fatalf("new job lifetime = %g", got)
	}
	if got := f.read(t, it).LifetimeCost; math.Abs(got-.168) > 1e-9 {
		t.Fatalf("source lifetime = %g", got)
	}
}

func TestLateOldRunUsageKeepsLifetimeWithoutChargingNewRun(t *testing.T) {
	f, it, worker, _, body := retainedNoChangeSetup(t)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var out api.WorkerRecovered
	f.request(t, "POST", path, worker.Credential, body, 200, &out)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateNew
		return run.QueueItem(it, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	body.AdditionalCost = .25
	for range 2 {
		f.request(t, "POST", path, worker.Credential, body, 200, &out)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.JobLifetimeSpent(saved); got != 1 {
		t.Fatalf("late lifetime = %g, want 1", got)
	}
	if got := factory.JobSpent(saved); got != 0 {
		t.Fatalf("old run charged new run %g, want 0", got)
	}
}

func TestSpendingPlacesTheWholeLifetimeAtTheLatestRunStart(t *testing.T) {
	f := newV2Jobs(t)
	f.create(t, factory.Item{Title: "Restarted", State: factory.StateNew, SpentBefore: .138, Created: now.Add(-48 * time.Hour), Stream: &factory.Stream{Started: now, Spent: .03}})
	day := now.Format(time.DateOnly)
	w := verbRequest(f.h, "GET", "/v1/spend/repos?from="+day+"&to="+day, "")
	if w.Code != 200 {
		t.Fatalf("Spending status=%d: %s", w.Code, w.Body)
	}
	var out api.SpendByRepo
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// The range covers only run two. Placement still counts the job's whole
	// lifetime; it cannot claim to be the exact usage bought inside that range.
	if math.Abs(out.TotalUSD-.168) > 1e-9 {
		t.Fatalf("Spending lifetime=%g, want .168", out.TotalUSD)
	}
	yesterday := now.Add(-24 * time.Hour).Format(time.DateOnly)
	w = verbRequest(f.h, "GET", "/v1/spend/repos?from="+yesterday+"&to="+yesterday, "")
	if w.Code != 200 {
		t.Fatalf("Spending status=%d: %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.TotalUSD != 0 {
		t.Fatalf("outside placement=%g", out.TotalUSD)
	}
}

func TestStopUndoKeepsMoneyReportedAfterStop(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Title: "Undo", State: factory.StateRunning, Stream: &factory.Stream{Spent: .138, Started: now.Add(-time.Minute)}})
	f.post(t, it, "stop", "", 200)
	if err := f.st.Annotate(it.ID, func(it *factory.Item) error { factory.AddChatSpend(it, .03); return nil }); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "undo", "", 200)
	if got := f.read(t, it).LifetimeCost; math.Abs(got-.168) > 1e-9 {
		t.Fatalf("Undo lost late money: %g, want .168", got)
	}
}

func TestLateSettlementAfterRoundTableGrowthKeepsOldRunLifetime(t *testing.T) {
	f := workerSetupAs(t, "sam")
	it := f.add(t, "sam")
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	cli := client.Open(f.server.URL, "").Worker(worker.Credential)
	anchor, err := client.PrepareWorkerReport(api.WorkerReport{}, "old-run-settlement")
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.ReportPrepared(context.Background(), pulled.Lease.ID, anchor); err != nil {
		t.Fatal(err)
	}
	for i, spent := range []float64{.25, .375} {
		report := api.WorkerReport{Steps: []api.StepReport{{Name: "proof", Round: 1, State: api.StepRunning, RoundCosts: map[int]float64{1: spent}}}}
		prepared, err := client.PrepareWorkerReport(report, fmt.Sprintf("table-growth-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := cli.ReportPrepared(context.Background(), pulled.Lease.ID, prepared); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Stream.Spent != .375 || saved.Stream.Phases[0].RoundCosts[1] != .375 {
		t.Fatalf("round table growth not admitted: %+v", saved.Stream)
	}
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateNew
		if err := run.QueueItem(it, now.Add(time.Minute)); err != nil {
			return err
		}
		return run.ApplyReport(it, factory.RunReport{Cost: .0625}, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	body := api.WorkerRecovery{OriginalLease: pulled.Lease.ID, Body: anchor.Body, Headers: anchor.Headers, AdditionalCost: .5}
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	for attempt := range 2 {
		revision, err := f.st.Revision(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		var out api.WorkerRecovered
		f.request(t, "POST", path, worker.Credential, body, 200, &out)
		saved, err := f.st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.SpentBefore != .5 || factory.JobSpent(saved) != .0625 || out.Job.LifetimeCost != .5625 || !out.AlreadyApplied {
			t.Fatalf("settlement %d: earlier=%g run=%g lifetime=%g replay=%v, want .5/.0625/.5625/true", attempt, saved.SpentBefore, factory.JobSpent(saved), out.Job.LifetimeCost, out.AlreadyApplied)
		}
		if attempt == 1 {
			after, err := f.st.Revision(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after != revision {
				t.Fatalf("repeated settlement rewrote the job: revision %d -> %d", revision, after)
			}
		}
	}
}

func TestArchivedLifetimeSettlementSurvivesClearedOrReplacedMachine(t *testing.T) {
	for _, mode := range []string{"drop and bring back", "restart on another machine"} {
		t.Run(mode, func(t *testing.T) {
			f := workerSetupAs(t, "sam")
			it := f.add(t, "sam")
			worker := f.attach(t, "sam")
			pulled := f.pull(t, worker)
			cli := client.Open(f.server.URL, "").Worker(worker.Credential)
			started, err := client.PrepareWorkerReport(api.WorkerReport{Steps: []api.StepReport{{Name: "proof", Round: 1, State: api.StepRunning}}}, "started-lifetime")
			if err != nil {
				t.Fatal(err)
			}
			if err := cli.ReportPrepared(context.Background(), pulled.Lease.ID, started); err != nil {
				t.Fatal(err)
			}
			anchor, err := client.PrepareWorkerReport(api.WorkerReport{}, "archived-lifetime")
			if err != nil {
				t.Fatal(err)
			}
			if err := cli.ReportPrepared(context.Background(), pulled.Lease.ID, anchor); err != nil {
				t.Fatal(err)
			}
			if mode == "drop and bring back" {
				f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/drop", it.ID), "", nil, 200, nil)
				f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/restore", it.ID), "", nil, 200, nil)
			} else {
				desk, err := lease.Open(f.st, lease.Options{Now: func() time.Time { return now }})
				if err != nil {
					t.Fatal(err)
				}
				if err := lease.NewDoors(desk).Stop(it.ID); err != nil {
					t.Fatal(err)
				}
				if err := f.st.Update(it.ID, func(it *factory.Item) error {
					it.State = factory.StateNew
					if err := run.QueueItem(it, now.Add(time.Minute)); err != nil {
						return err
					}
					it.WorktreeMachine = ""
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			var replacement api.WorkerAttached
			var newPull api.WorkerPull
			if mode == "restart on another machine" {
				f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Name: "replacement-machine", Machine: "replacement-machine", Handle: "sam"}, 201, &replacement)
				newPull = f.pull(t, replacement)
				if err := f.st.Update(it.ID, func(it *factory.Item) error {
					return run.ApplyReport(it, factory.RunReport{Cost: .02}, now.Add(time.Minute))
				}); err != nil {
					t.Fatal(err)
				}
			}
			body := api.WorkerRecovery{OriginalLease: pulled.Lease.ID, Body: anchor.Body, Headers: anchor.Headers, AdditionalCost: .05}
			path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
			var first api.WorkerRecovered
			f.request(t, "POST", path, worker.Credential, body, 200, &first)
			wantRun := 0.0
			if mode == "restart on another machine" {
				wantRun = .02
			}
			if !first.AlreadyApplied || math.Abs(first.Job.LifetimeCost-(wantRun+.05)) > 1e-9 || first.Job.Cost != wantRun {
				t.Fatalf("late spend: lifetime=%g run=%g replay=%v, want %g/%g/true", first.Job.LifetimeCost, first.Job.Cost, first.AlreadyApplied, wantRun+.05, wantRun)
			}
			var stranger api.WorkerAttached
			f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Name: "stranger-machine", Machine: "stranger-machine", Handle: "sam"}, 201, &stranger)
			f.request(t, "POST", path, stranger.Credential, body, 409, nil)
			wrongLease := body
			wrongLease.OriginalLease = "lease_unrelated"
			f.request(t, "POST", path, worker.Credential, wrongLease, 422, nil)
			revision, err := f.st.Revision(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, cost := range []float64{.05, .03} {
				body.AdditionalCost = cost
				var replay api.WorkerRecovered
				f.request(t, "POST", path, worker.Credential, body, 200, &replay)
				if replay.Job.LifetimeCost != first.Job.LifetimeCost || replay.Job.Cost != wantRun {
					t.Fatalf("replay changed spending: %+v", replay.Job)
				}
			}
			if mode == "restart on another machine" {
				f.request(t, "POST", "/v1/workers/leases/"+newPull.Lease.ID+"/heartbeat", replacement.Credential, nil, 200, nil)
			}
			after, err := f.st.Revision(it.ID)
			if err != nil || after != revision {
				t.Fatalf("replay rewrote job: %d -> %d: %v", revision, after, err)
			}
		})
	}
}
