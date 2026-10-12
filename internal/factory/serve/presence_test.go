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
	if me.Contract != api.Contract || api.Contract != 2 || me.Handle != "sam" || len(me.Machines) != 1 {
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

func TestOfflineAfterSixtySeconds(t *testing.T) {
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
	if len(me) != 1 || me[0].Status != api.MachineOffline || me[0].Job != 0 {
		t.Fatalf("three intervals quiet: %+v", me)
	}
	var q api.Queue
	f.request(t, "GET", "/v1/queue", "", nil, 200, &q)
	if q.Capacity.MachinesOnline != 0 || q.Capacity.Busy != 0 {
		t.Fatalf("offline still counted: %+v", q.Capacity)
	}
	var one struct{ api.Job }
	f.request(t, "GET", "/v1/jobs/"+strconv.Itoa(job.ID), "", nil, 200, &one)
	if one.HeldBy != nil || one.State != api.StateNeedsYou {
		t.Fatalf("offline job did not ask: %+v", one.Job)
	}
}

func TestTeamRunnersNeverTakeUnassignedEvenWithSavedConsent(t *testing.T) {
	f := workerSetupAs(t, "sam")
	job := f.add(t, "")
	var sam api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "mbp", Name: "mbp", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}}, 201, &sam)
	f.request(t, "PUT", "/v1/me/takes_unassigned", "", map[string]bool{"owner/repo": true}, 200, nil)
	f.request(t, "GET", "/v1/workers/pull", sam.Credential, nil, 204, nil)
	if err := f.st.Update(job.ID, func(it *factory.Item) error { it.Assignee = "sam"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := f.pull(t, sam); got.Job.ID != job.ID {
		t.Fatal("owner's job was refused")
	}
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
