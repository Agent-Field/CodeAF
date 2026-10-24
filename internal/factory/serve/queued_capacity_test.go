package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestQueuedCapacityIsScopedToTheOwnerAndWorktreeComputer(t *testing.T) {
	for _, tc := range []struct {
		name, owner, runner string
		machines            []machineCapacity
		full                bool
	}{
		{"full", "sam", "", []machineCapacity{{owner: "sam", machine: "mac", used: 2, slots: 2}}, true},
		{"spare slot", "sam", "", []machineCapacity{{owner: "sam", machine: "mac", used: 1, slots: 2}}, false},
		{"another owner", "sam", "", []machineCapacity{{owner: "lee", machine: "mac", used: 2, slots: 2}}, false},
		{"unknown owner", "", "", []machineCapacity{{owner: "sam", machine: "mac", used: 2, slots: 2}}, false},
		{"offline", "sam", "", nil, false},
		{"affinity offline", "sam", "old", []machineCapacity{{owner: "sam", machine: "new", used: 2, slots: 2}}, false},
		{"affinity full", "sam", "old", []machineCapacity{{owner: "sam", machine: "old", used: 2, slots: 2}, {owner: "sam", machine: "new", slots: 3}}, true},
		{"second computer free", "sam", "", []machineCapacity{{owner: "sam", machine: "old", used: 2, slots: 2}, {owner: "sam", machine: "new", slots: 3}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := api.Job{State: api.StateQueued, AssignedTo: tc.owner, Runner: tc.runner}
			presentQueuedCapacity(&job, tc.machines)
			api.PresentJob(&job)
			if (job.StatePresentation.Kind == "waiting_for_capacity") != tc.full {
				t.Fatalf("presentation: %+v", job.StatePresentation)
			}
			if !strings.Contains(job.StatePresentation.Detail, api.ConcurrencySettingsHint) {
				t.Fatalf("queued guidance lost Settings: %+v", job.StatePresentation)
			}
		})
	}
}

func TestQueuedCapacityReadsLiveUsageOncePerComputerAndRefreshesAfterAChange(t *testing.T) {
	c := capacitySetup(t)
	first := c.attach(t, c.priya, "priya", "mbp", 2)
	second := c.attach(t, c.priya, "priya", "mbp", 2)
	for range 2 {
		c.add(t, "priya")
	}
	waiting := c.add(t, "priya")
	for _, worker := range []api.RunnerAttached{first, second} {
		c.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, nil)
	}
	check := func(want string) {
		t.Helper()
		var job api.Job
		c.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", waiting.ID), c.priya, nil, 200, &job)
		if job.StatePresentation.Kind != want {
			t.Fatalf("job presentation: %+v", job.StatePresentation)
		}
		if want == "waiting_for_capacity" && !strings.Contains(job.StatePresentation.Detail, "2/2 execution slots") {
			t.Fatalf("duplicate slot registrations inflated capacity: %+v", job.StatePresentation)
		}
		var queue api.Queue
		c.request(t, "GET", "/v1/queue", c.priya, nil, 200, &queue)
		for _, row := range queue.Jobs {
			if row.ID == waiting.ID && row.StatePresentation != job.StatePresentation {
				t.Fatal("queue and job disagree")
			}
		}
	}
	check("waiting_for_capacity")
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: 3}, 200, nil)
	check("waiting_for_runner")
	// An old registration is never evidence of full online capacity.
	c.now = c.now.Add(2 * c.h.runnerOfflineAfter())
	check("waiting_for_runner")
}
