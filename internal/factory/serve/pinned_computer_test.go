package serve

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A queued job whose work is on a computer that is down names that computer
// and how long it has been down, to its person only; Start now says why it
// cannot start instead of doing nothing. A live computer, or none known, keeps
// the generic words.
func TestQueuedJobOnADownComputerSaysWhichAndStartNowSaysWhy(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "mbp", 2)
	it := c.add(t, "priya")
	if err := c.st.Update(it.ID, func(cur *factory.Item) error { cur.WorktreeMachine = "mbp"; return nil }); err != nil {
		t.Fatal(err)
	}
	read := func(token string) api.StatePresentation {
		t.Helper()
		var job api.Job
		c.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), token, nil, 200, &job)
		return job.StatePresentation
	}
	if p := read(c.priya); p.Kind == api.WaitingForComputer {
		t.Fatalf("a live computer is not waited for: %+v", p)
	}
	c.now = c.now.Add(2*c.h.runnerOfflineAfter() + 3*time.Hour)
	p := read(c.priya)
	if p.Kind != api.WaitingForComputer || p.Title != "Waiting for mbp" || !strings.Contains(p.Detail, "Waiting for mbp (offline for 3 h)") {
		t.Fatalf("the owner reads %+v", p)
	}
	if strings.Contains(p.Detail, "Jobs at once") {
		t.Fatalf("no setting can start it elsewhere: %+v", p)
	}
	if other := read("operator-secret"); strings.Contains(other.Title+other.Detail, "mbp") || other.Kind != api.WaitingForComputer {
		t.Fatalf("another person's computer is named to the operator: %+v", other)
	}
}

// Start now on a queued job whose computer is down refuses with why, before it
// writes anything: a slot above Jobs at once cannot start work that is on a
// computer that is off.
func TestStartNowOnAJobWhoseComputerIsDownSaysWhy(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	f.seam.Launch = func(int) error { return nil }
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "owner", Kind: api.RunnerMac, Slots: 2, TakesUnassigned: map[string]bool{"*": true}}, 201, nil)
	it := f.add(t, "owner")
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.WorktreeMachine = "mbp"; return nil }); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2*f.h.runnerOfflineAfter() + 40*time.Minute)
	before, _ := f.st.Get(it.ID)
	var refused struct {
		Error string `json:"error"`
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "operator-secret", nil, 409, &refused)
	if !strings.Contains(refused.Error, "its work is on mbp, which is offline for 4") {
		t.Fatalf("Start now refusal: %q", refused.Error)
	}
	if after, _ := f.st.Get(it.ID); !after.Changed.Equal(before.Changed) || after.OverLimit || after.Marked != before.Marked {
		t.Fatal("a refused start wrote to the job")
	}
}

func TestPinnedDownClaimsNothingUnknownOrLive(t *testing.T) {
	roster := []machineRow{{Handle: "owner-a", Machine: api.Machine{Name: "mac", Status: api.MachineIdle}}, {Handle: "owner-b", Machine: api.Machine{Name: "old", Status: api.MachineOffline}}}
	if _, ok := pinnedDown("", "owner-a", roster); ok {
		t.Fatal("no pin")
	}
	if _, ok := pinnedDown("mac", "owner-a", roster); ok {
		t.Fatal("a live computer")
	}
	if _, ok := pinnedDown("gone", "owner-a", roster); ok {
		t.Fatal("a computer the roster does not know")
	}
	if _, ok := pinnedDown("old", "owner-a", roster); ok {
		t.Fatal("another person's computer of the same name")
	}
	if row, ok := pinnedDown("old", "owner-b", roster); !ok || row.Name != "old" {
		t.Fatal("the person's own offline computer")
	}
}
