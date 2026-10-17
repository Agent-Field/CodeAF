package serve_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestPersonCardMachinesFollowPresenceWithoutListingAnotherHandle(t *testing.T) {
	f := workerSetupAs(t, "sam")
	job := f.add(t, "sam")
	var worker api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "sam-desk", Name: "sam-desk", Handle: "sam", OS: "linux"}, 201, &worker)
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "other-desk", Name: "other-desk", Handle: "other"}, 201, nil)
	read := func() api.Person {
		t.Helper()
		var p api.Person
		f.request(t, "GET", "/v1/people/sam", "", nil, 200, &p)
		if p.Role != "member" || len(p.Machines) != 1 || p.Machines[0].Name != "sam-desk" {
			t.Fatalf("person = %+v", p)
		}
		return p
	}
	if p := read(); p.Machines[0].Status != api.MachineIdle {
		t.Fatalf("idle = %+v", p.Machines)
	}
	f.pull(t, worker)
	if p := read(); p.Machines[0].Status != api.MachineBusy || p.Machines[0].Job != job.ID {
		t.Fatalf("busy = %+v", p.Machines)
	}
	f.clock.Add(int64(time.Duration(lease.OfflineMisses) * lease.HeartbeatEvery))
	if p := read(); p.Machines[0].Status != api.MachineOffline {
		t.Fatalf("offline = %+v", p.Machines)
	}
}
