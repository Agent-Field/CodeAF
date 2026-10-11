package serve_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestMachinesAreTheViewersAndHeldByNamesTheRest(t *testing.T) {
	f := workerSetupAs(t, "sam")
	job := f.add(t, "priya")
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "mbp", Name: "mbp (pid 1)", Handle: "sam", OS: "darwin", CodeafVersion: "dev-test",
	}, 201, nil)
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "mbp", Name: "mbp (pid 2)", Handle: "sam", OS: "darwin", CodeafVersion: "dev-test",
	}, 201, nil)
	var other api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "priya-desk", Name: "priya-desk", Handle: "priya", OS: "linux", CodeafVersion: "other",
	}, 201, &other)
	pulled := f.pull(t, other)
	if pulled.Job.ID != job.ID {
		t.Fatalf("pull %d, want %d", pulled.Job.ID, job.ID)
	}

	var me struct {
		Contract int           `json:"contract"`
		Handle   string        `json:"handle"`
		Machines []api.Machine `json:"machines"`
	}
	f.request(t, "GET", "/v1/me", "", nil, 200, &me)
	if me.Contract != api.Contract || api.Contract != 1 || me.Handle != "sam" || len(me.Machines) != 1 {
		t.Fatalf("me: contract %d handle %q machines %+v", me.Contract, me.Handle, me.Machines)
	}
	got := me.Machines[0]
	if got.Name != "mbp" || got.OS != "darwin" || got.CodeafVersion != "dev-test" || got.Status != api.MachineIdle || got.Job != 0 {
		t.Fatalf("own machine: %+v", got)
	}
	if !got.LastHeartbeat.Equal(time.Unix(0, f.clock.Load()).UTC()) {
		t.Fatalf("last heartbeat %s", got.LastHeartbeat)
	}

	var one struct{ api.Job }
	f.request(t, "GET", "/v1/jobs/"+strconv.Itoa(job.ID), "", nil, 200, &one)
	if one.HeldBy == nil || one.HeldBy.Machine != "priya-desk" || one.HeldBy.Handle != "priya" || one.HeldBy.Since.IsZero() || !one.HeldBy.HeartbeatAt.Equal(one.HeldBy.Since) {
		t.Fatalf("held_by: %+v", one.HeldBy)
	}
	since := one.HeldBy.Since

	var q api.Queue
	f.request(t, "GET", "/v1/queue", "", nil, 200, &q)
	if q.Capacity.MachinesOnline != 2 || q.Capacity.Busy != 1 {
		t.Fatalf("capacity: %+v", q.Capacity)
	}

	f.clock.Add(int64(lease.HeartbeatEvery))
	f.request(t, "POST", "/v1/workers/leases/"+pulled.Lease.ID+"/heartbeat", other.Credential, nil, 200, nil)
	f.request(t, "GET", "/v1/jobs/"+strconv.Itoa(job.ID), "", nil, 200, &one)
	if one.HeldBy == nil || !one.HeldBy.Since.Equal(since) || !one.HeldBy.HeartbeatAt.After(since) {
		t.Fatalf("renewal: %+v", one.HeldBy)
	}
}

func TestOfflineAfterThreeHeartbeatIntervals(t *testing.T) {
	f := workerSetupAs(t, "sam")
	job := f.add(t, "sam")
	var worker api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "mbp", Name: "mbp", Handle: "sam", OS: "linux", CodeafVersion: "dev"}, 201, &worker)
	f.pull(t, worker)

	f.clock.Add(int64(time.Duration(lease.OfflineMisses)*lease.HeartbeatEvery) - 1)
	me := readMe(t, f)
	if len(me) != 1 || me[0].Status != api.MachineBusy || me[0].Job != job.ID {
		t.Fatalf("inside the window: %+v", me)
	}

	f.clock.Add(1)
	me = readMe(t, f)
	if len(me) != 1 || me[0].Status != api.MachineOffline || me[0].Job != job.ID {
		t.Fatalf("three intervals quiet: %+v", me)
	}
	var q api.Queue
	f.request(t, "GET", "/v1/queue", "", nil, 200, &q)
	if q.Capacity.MachinesOnline != 0 || q.Capacity.Busy != 0 {
		t.Fatalf("offline still counted: %+v", q.Capacity)
	}
	var one struct{ api.Job }
	f.request(t, "GET", "/v1/jobs/"+strconv.Itoa(job.ID), "", nil, 200, &one)
	if one.HeldBy == nil || one.HeldBy.Machine != "mbp" {
		t.Fatalf("quiet machine dropped off the job: %+v", one.HeldBy)
	}
}

func TestSavedTakesUnassignedReplacesAttachConsent(t *testing.T) {
	f := workerSetupAs(t, "sam")
	first := f.add(t, "")
	var sam, priya api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "mbp", Name: "mbp", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true},
	}, 201, &sam)
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "desk", Name: "desk", Handle: "priya", TakesUnassigned: map[string]bool{lease.EveryRepo: true},
	}, 201, &priya)

	f.request(t, "PUT", "/v1/me/takes_unassigned", "", map[string]bool{"other/repo": true}, 200, nil)
	f.request(t, "GET", "/v1/workers/pull", sam.Credential, nil, 204, nil)
	if pulled := f.pull(t, priya); pulled.Job.ID != first.ID {
		t.Fatalf("priya's own consent was replaced: %+v", pulled.Job.ID)
	}

	second := f.add(t, "")
	f.request(t, "PUT", "/v1/me/takes_unassigned", "", map[string]bool{"owner/repo": true}, 200, nil)
	var me struct {
		TakesUnassigned map[string]bool `json:"takes_unassigned"`
		Machines        []api.Machine   `json:"machines"`
	}
	f.request(t, "GET", "/v1/me", "", nil, 200, &me)
	if len(me.TakesUnassigned) != 1 || !me.TakesUnassigned["owner/repo"] {
		t.Fatalf("saved toggle: %+v", me.TakesUnassigned)
	}
	for _, row := range me.Machines {
		if row.Name == "desk" {
			t.Fatalf("priya's machine listed for sam: %+v", me.Machines)
		}
	}
	if pulled := f.pull(t, sam); pulled.Job.ID != second.ID {
		t.Fatalf("saved repo was not honoured: %d", pulled.Job.ID)
	}

	third := f.add(t, "")
	f.request(t, "PUT", "/v1/me/takes_unassigned", "", map[string]bool{}, 200, nil)
	var sam2 api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{
		Machine: "mbp-2", Name: "mbp-2", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true},
	}, 201, &sam2)
	f.request(t, "GET", "/v1/workers/pull", sam2.Credential, nil, 204, nil)
	saved, set, err := f.st.Takes(" @Sam ")
	if err != nil || !set || len(saved) != 0 {
		t.Fatalf("cleared toggle: %v set=%v %v", saved, set, err)
	}
	again, err := store.Open(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	if _, set, err = again.Takes("sam"); err != nil || !set {
		t.Fatalf("toggle did not persist: set=%v %v", set, err)
	}
	if _, set, err = again.Takes("priya"); err != nil || set {
		t.Fatalf("priya gained sam's toggle: set=%v %v", set, err)
	}
	if third.ID == 0 {
		t.Fatal("unassigned job was not created")
	}

	f.request(t, "GET", "/v1/me/takes_unassigned", "", nil, 405, nil)
	f.request(t, "PUT", "/v1/me/takes_unassigned", "", map[string]bool{" ": true}, 400, nil)
}

func TestTakesUnassignedNeedsAStore(t *testing.T) {
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	req, err := http.NewRequest(http.MethodPut, s.URL+"/v1/me/takes_unassigned", strings.NewReader(`{"owner/repo":true}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func readMe(t *testing.T, f *workerFixture) []api.Machine {
	t.Helper()
	var me struct {
		Machines []api.Machine `json:"machines"`
	}
	f.request(t, "GET", "/v1/me", "", nil, 200, &me)
	return me.Machines
}
