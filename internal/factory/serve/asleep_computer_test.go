package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (f *peopleV2Fixture) attachMBP(t *testing.T, slots int) api.RunnerAttached {
	t.Helper()
	var worker api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "owner", Kind: api.RunnerMac, Slots: slots, TakesUnassigned: map[string]bool{"*": true}}, 201, &worker)
	return worker
}

func (f *peopleV2Fixture) saysAsleep(t *testing.T, worker api.RunnerAttached, asleep bool) {
	t.Helper()
	f.request(t, "POST", "/v1/runners/presence", worker.Credential, api.RunnerPresence{Asleep: asleep}, 200, nil)
}

// A process that attaches is running, so its computer is awake, whatever the
// registry saved before the app last stopped. The saved word is what kept a
// person's job waiting with nothing running.
func TestAComputerAttachingNowIsAwake(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	before := f.attachMBP(t, 2)
	f.saysAsleep(t, before, true)
	it := f.add(t, "owner")
	f.request(t, "GET", "/v1/runners/pull", before.Credential, nil, 204, nil)

	after := f.attachMBP(t, 2)

	var pulled api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", after.Credential, nil, 200, &pulled)
	if pulled.Lease.Job != it.ID {
		t.Fatalf("the job did not start after the app came back: %+v", pulled.Lease)
	}
	var list api.Runners
	f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	if len(list.Runners) != 1 || list.Runners[0].State == api.RunnerAsleep {
		t.Fatalf("the computer still reads asleep: %+v", list.Runners)
	}
}

// The runner asks the factory what it believes, so a wake that never arrived
// is noticed by the runner itself.
func TestARunnerReadsWhetherItIsMarkedAsleep(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	worker := f.attachMBP(t, 2)
	var self api.RunnerSelf
	f.request(t, "GET", "/v1/runner/self", worker.Credential, nil, 200, &self)
	if self.Asleep {
		t.Fatal("an awake runner reads asleep")
	}
	f.saysAsleep(t, worker, true)
	f.request(t, "GET", "/v1/runner/self", worker.Credential, nil, 200, &self)
	if !self.Asleep {
		t.Fatal("the runner cannot see that it is marked asleep")
	}
}

// Clearing the flag starts what is queued within the owner's number, and a
// job in the backlog stays there.
func TestWakingStartsOnlyQueuedJobsWithinJobsAtOnce(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	first := f.attachMBP(t, 1)
	second := f.attachMBP(t, 1)
	f.saysAsleep(t, first, true)
	f.add(t, "owner")
	f.add(t, "owner")
	backlog, err := f.st.Create(factory.Item{Title: "later", Repo: "acme/api", State: factory.StateNew, Assignee: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	f.saysAsleep(t, first, false)
	f.request(t, "GET", "/v1/runners/pull", first.Credential, nil, 200, nil)
	f.request(t, "GET", "/v1/runners/pull", second.Credential, nil, 204, nil)
	if cur, _ := f.st.Get(backlog.ID); cur.State != factory.StateNew {
		t.Fatalf("a backlog job moved when the computer woke: %s", cur.State)
	}
}

// Every word on the job comes from one reason: the computer is asleep, so no
// slot and no Jobs at once setting is what it waits for.
func TestAQueuedJobOnAnAsleepComputerSaysSo(t *testing.T) {
	for _, overLimit := range []bool{false, true} {
		t.Run(fmt.Sprintf("started_now_%v", overLimit), func(t *testing.T) {
			f := peopleV2Setup(t, api.FactorySolo)
			worker := f.attachMBP(t, 2)
			it := f.add(t, "owner")
			if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.OverLimit = overLimit; return nil }); err != nil {
				t.Fatal(err)
			}
			read := func() api.StatePresentation {
				var job api.Job
				f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "operator-secret", nil, 200, &job)
				return job.StatePresentation
			}
			f.saysAsleep(t, worker, true)
			p := read()
			if p.Kind != api.WaitingForComputer || p.Title != "Waiting for mbp" || !strings.Contains(p.Detail, "asleep") {
				t.Fatalf("reads %+v", p)
			}
			if words := p.Title + p.Detail; strings.Contains(words, "Starting now") || strings.Contains(words, "Jobs at once") || strings.Contains(words, "slot") {
				t.Fatalf("blames the wrong thing: %+v", p)
			}
			if QueuedWait(f.st, it, f.now) == "" {
				t.Fatal("the manager is not told the same reason")
			}
			f.saysAsleep(t, worker, false)
			if p := read(); p.Kind == api.WaitingForComputer {
				t.Fatalf("an awake computer is still waited for: %+v", p)
			}
			if got := QueuedWait(f.st, it, f.now); got != "" {
				t.Fatalf("the manager still hears a reason: %q", got)
			}
		})
	}
}

// In a team, an unassigned job can go to anyone, and another person's
// computer is never named.
func TestAnAsleepComputerClaimsOnlyItsOwnersJobs(t *testing.T) {
	c := capacitySetup(t)
	worker := c.attach(t, c.priya, "priya", "mbp", 2)
	c.request(t, "POST", "/v1/runners/presence", worker.Credential, api.RunnerPresence{Asleep: true}, 200, nil)
	read := func(it factory.Item, token string) api.StatePresentation {
		t.Helper()
		var job api.Job
		c.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), token, nil, 200, &job)
		return job.StatePresentation
	}
	if p := read(c.add(t, ""), "operator-secret"); p.Kind == api.WaitingForComputer {
		t.Fatalf("an unassigned job claims one computer: %+v", p)
	}
	mine := c.add(t, "priya")
	if p := read(mine, c.priya); p.Title != "Waiting for mbp" {
		t.Fatalf("its person reads %+v", p)
	}
	if p := read(mine, "operator-secret"); p.Kind != api.WaitingForComputer || strings.Contains(p.Title+p.Detail, "mbp") || strings.Contains(p.Detail, "its work is on") {
		t.Fatalf("another person reads %+v", p)
	}
}
