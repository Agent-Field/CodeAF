package serve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Authorization and a long-poll request retain an earlier Worker snapshot.
// A lowering must govern that already-authorized request too, without ending
// its existing lease. No sleeps, goroutine scheduling or live APIs required.
func TestRunnerLoweringGovernsAnAlreadyAuthorizedPull(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
	var first, second api.RunnerAttached
	attach := api.RunnerAttach{Runner: "capacity-parent", Name: "capacity-parent", Owner: "owner", Kind: api.RunnerMac, Slots: 2}
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", attach, 201, &first)
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", attach, 201, &second)
	f.add(t, "owner")
	var active api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", first.Credential, nil, 200, &active)
	request := httptest.NewRequest(http.MethodGet, "/v1/runners/pull", nil)
	request.Header.Set("Authorization", "Bearer "+second.Credential)
	authorized, ok := f.h.authorizeWorker(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("second worker did not authorize")
	}
	f.request(t, "PUT", "/v1/runners/capacity-parent", "operator-secret", api.RunnerSettings{Slots: 1}, 200, nil)
	f.add(t, "owner")
	answer := httptest.NewRecorder()
	f.h.pullWorker(answer, authorized)
	if answer.Code != http.StatusNoContent {
		t.Fatalf("already-authorized pull ignored saved capacity: got %d, want204; another job must wait for the existing lease", answer.Code)
	}
	f.request(t, "POST", "/v1/runners/leases/"+active.Lease.ID+"/heartbeat", first.Credential, struct{}{}, 200, nil)
}

func TestLegacyRunnerCapacityHasOneGrantedValue(t *testing.T) {
	for _, tc := range []struct{ saved, worker, want int }{{0, 0, api.DefaultRunnerSlots}, {2, 0, 2}, {1, 8, 1}} {
		t.Run(fmt.Sprintf("saved_%d_worker_%d", tc.saved, tc.worker), func(t *testing.T) {
			f := peopleV2Setup(t, api.FactorySolo)
			f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
			var attached api.RunnerAttached
			f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "legacy", Name: "legacy", Owner: "owner", Kind: api.RunnerMac}, 201, &attached)
			f.h.workers.mu.Lock()
			for key, row := range f.h.workers.credentials {
				row.Slots, row.Worker.Slots = tc.saved, tc.worker
				f.h.workers.credentials[key] = row
			}
			err := f.h.workers.saveRegistry()
			f.h.workers.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if err := f.h.workers.loadRegistry(); err != nil {
				t.Fatal(err)
			}
			var self api.RunnerSelf
			f.request(t, "GET", "/v1/runner/self", attached.Credential, nil, 200, &self)
			if self.Slots != tc.want {
				t.Fatalf("self slots %d, want granted %d", self.Slots, tc.want)
			}
			var list api.Runners
			f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
			if len(list.Runners) != 1 || list.Runners[0].Slots != tc.want {
				t.Fatalf("roster disagrees: %+v", list.Runners)
			}
		})
	}
}
