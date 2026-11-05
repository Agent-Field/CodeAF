package serve

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

type waitsFixture struct {
	*peopleV2Fixture
	launches int
}

func waitsSetup(t *testing.T) *waitsFixture {
	t.Helper()
	f := &waitsFixture{peopleV2Fixture: peopleV2Setup(t, api.FactorySolo)}
	// Scans are called directly with the fixture's clock, never after a sleep.
	f.h.stopWaits()
	if err := f.st.SetRepos([]string{"owner/api"}); err != nil {
		t.Fatal(err)
	}
	f.h.Close()
	f.seam.Launch = func(id int) error { f.launches++; return lease.NewDoors(f.h.workers.desk).Launch(id) }
	f.seam.Stop = func(id int) error { return lease.NewDoors(f.h.workers.desk).Stop(id) }
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.h.stopWaits()
	return f
}

func (f *waitsFixture) addJob(t *testing.T, num int) factory.Item {
	t.Helper()
	it, err := f.st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Repo: "owner/api", Num: num, Title: "test waits", Cap: 1, Stages: []factory.Stage{{Name: "test", Label: "Test", Kind: factory.StageCheck, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (f *waitsFixture) edit(t *testing.T, id int, change func(*factory.Item)) {
	t.Helper()
	if err := f.st.Update(id, func(it *factory.Item) error { change(it); return nil }); err != nil {
		t.Fatal(err)
	}
}

func (f *waitsFixture) item(t *testing.T, id int) factory.Item {
	t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (f *waitsFixture) readJob(t *testing.T, id int) api.Job {
	t.Helper()
	var j api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", id), "operator-secret", nil, 200, &j)
	return j
}

func (f *waitsFixture) set(t *testing.T, id int, ids []int, status int) string {
	t.Helper()
	return f.request(t, "PUT", fmt.Sprintf("/v1/jobs/%d/waits_on", id), "operator-secret", struct {
		WaitsOn []int `json:"waits_on"`
	}{ids}, status, nil)
}

func (f *waitsFixture) scan(t *testing.T) {
	t.Helper()
	if err := f.h.scanWaits(); err != nil {
		t.Fatal(err)
	}
}

func (f *waitsFixture) merge(t *testing.T, id int) {
	t.Helper()
	f.edit(t, id, func(it *factory.Item) {
		it.State = factory.StateShipped
		it.PullRequest = &factory.PullRequest{State: factory.PRMerged}
	})
}

// W1, W11: only unmerged parents remain in waiting_on, and release never launches.
func TestWaitsOrdering(t *testing.T) {
	f := waitsSetup(t)
	a, b, child := f.addJob(t, 1107), f.addJob(t, 1108), f.addJob(t, 0)
	f.set(t, child.ID, []int{a.ID, b.ID}, 200)
	if it := f.item(t, child.ID); !reflect.DeepEqual(it.WaitingOn, []int{a.ID, b.ID}) {
		t.Fatalf("set waiting = %+v", it)
	}
	f.scan(t)
	j := f.readJob(t, child.ID)
	if j.State != api.StateBacklog || !reflect.DeepEqual(j.WaitingOn, []int{a.ID, b.ID}) || j.Blocked != nil || j.Held {
		t.Fatalf("waiting = %+v", j)
	}
	rev, err := f.st.Revision(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.scan(t)
	if after, _ := f.st.Revision(child.ID); after != rev {
		t.Fatalf("unchanged scan wrote revision %d", after)
	}
	f.merge(t, a.ID)
	if j := f.readJob(t, child.ID); !reflect.DeepEqual(j.WaitingOn, []int{a.ID, b.ID}) {
		t.Fatalf("read changed waits before scan = %+v", j)
	}
	f.scan(t)
	j = f.readJob(t, child.ID)
	it := f.item(t, child.ID)
	if !reflect.DeepEqual(j.WaitingOn, []int{b.ID}) || !reflect.DeepEqual(it.WaitingOn, []int{b.ID}) || it.WaitsReleased {
		t.Fatalf("one merged = %+v", j)
	}
	for _, endpoint := range []string{"/v1/queue", "/v1/queue?q=is:backlog"} {
		var queue api.Queue
		f.request(t, "GET", endpoint, "operator-secret", nil, 200, &queue)
		found := false
		for _, job := range queue.Jobs {
			if job.ID == child.ID {
				found = true
				if !reflect.DeepEqual(job.WaitingOn, it.WaitingOn) {
					t.Fatalf("%s waiting = %+v", endpoint, job)
				}
			}
		}
		if !found {
			t.Fatalf("%s omitted the waiting child", endpoint)
		}
	}
	f.merge(t, b.ID)
	f.scan(t)
	j, it = f.readJob(t, child.ID), f.item(t, child.ID)
	if j.State != api.StateBacklog || len(j.WaitingOn) != 0 || len(it.WaitingOn) != 0 || j.Blocked != nil || !it.WaitsReleased || f.launches != 0 {
		t.Fatalf("released = %+v, launches = %d", j, f.launches)
	}
	if len(it.Activity) == 0 || !strings.Contains(it.Activity[len(it.Activity)-1].What, "waits over: api#1107 merged") || !strings.Contains(it.Activity[len(it.Activity)-1].What, "api#1108 merged") {
		t.Fatalf("activity = %+v", it.Activity)
	}
	f.scan(t)
	if after := f.item(t, child.ID); !reflect.DeepEqual(after, it) {
		t.Fatalf("release repeated: %+v", after)
	}
}

// W1, W2, W6: PUT persists unsatisfied parents in order and clears both lists.
func TestWaitsSetPersistsUnsatisfiedParents(t *testing.T) {
	f := waitsSetup(t)
	a, b, c, child := f.addJob(t, 1107), f.addJob(t, 1108), f.addJob(t, 1109), f.addJob(t, 0)
	f.merge(t, b.ID)
	waits, waiting := []int{c.ID, b.ID, a.ID}, []int{c.ID, a.ID}
	var answer api.Job
	f.request(t, "PUT", fmt.Sprintf("/v1/jobs/%d/waits_on", child.ID), "operator-secret", map[string]any{"waits_on": waits}, 200, &answer)
	it := f.item(t, child.ID)
	if !reflect.DeepEqual(it.WaitsOn, waits) || !reflect.DeepEqual(it.WaitingOn, waiting) || !reflect.DeepEqual(answer.WaitingOn, waiting) || it.WaitsReleased {
		t.Fatalf("set waits = %+v; answer = %+v", it, answer)
	}
	f.edit(t, a.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test"} })
	f.scan(t)
	if it := f.item(t, child.ID); it.Blocked == nil {
		t.Fatal("failed parent did not block")
	}
	f.set(t, child.ID, []int{}, 200)
	if it := f.item(t, child.ID); len(it.WaitsOn) != 0 || len(it.WaitingOn) != 0 || it.Blocked != nil {
		t.Fatalf("cleared waits = %+v", it)
	}
}

// W1, W3: a partial merge updates waiting_on without changing an existing block's since.
func TestWaitsPartialMergeKeepsBlockSince(t *testing.T) {
	f := waitsSetup(t)
	a, b, child := f.addJob(t, 1107), f.addJob(t, 1108), f.addJob(t, 0)
	f.set(t, child.ID, []int{a.ID, b.ID}, 200)
	f.edit(t, b.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test", Code: "failed"} })
	f.scan(t)
	before := f.item(t, child.ID)
	if before.Blocked == nil {
		t.Fatal("failed parent did not block")
	}
	f.now = f.now.Add(time.Minute)
	f.merge(t, a.ID)
	f.scan(t)
	after := f.item(t, child.ID)
	if !reflect.DeepEqual(after.WaitingOn, []int{b.ID}) || !reflect.DeepEqual(after.Blocked, before.Blocked) || after.WaitsReleased {
		t.Fatalf("partial merge = %+v", after)
	}
	rev, err := f.st.Revision(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.scan(t)
	if next, _ := f.st.Revision(child.ID); next != rev {
		t.Fatalf("unchanged block and waits wrote revision %d", next)
	}
}

// W2: every invalid parent list leaves both the document and revision unchanged.
func TestWaitsSetRefused(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 0), f.addJob(t, 0)
	f.set(t, child.ID, []int{parent.ID}, 200)
	closed, dropped, local := f.addJob(t, 0), f.addJob(t, 0), f.addJob(t, 0)
	f.edit(t, closed.ID, func(it *factory.Item) { it.PullRequest = &factory.PullRequest{State: factory.PRClosed} })
	f.edit(t, dropped.ID, func(it *factory.Item) { it.DoneHow = "dropped" })
	f.edit(t, local.ID, func(it *factory.Item) { it.DoneHow = "closed_locally" })
	for _, ids := range [][]int{{9999}, make([]int, 21), {parent.ID, parent.ID}, {child.ID}, {closed.ID}, {dropped.ID}, {local.ID}, {0}, {-1}} {
		before := f.item(t, child.ID)
		rev, err := f.st.Revision(child.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw := f.set(t, child.ID, ids, 422)
		var refusal struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &refusal); err != nil || refusal.Error == "" {
			t.Fatalf("refusal = %s, %v", raw, err)
		}
		if !reflect.DeepEqual(f.item(t, child.ID), before) {
			t.Fatalf("refused %v changed job", ids)
		}
		if after, _ := f.st.Revision(child.ID); after != rev {
			t.Fatalf("refused %v changed revision", ids)
		}
	}
	f.merge(t, parent.ID)
	f.set(t, child.ID, []int{parent.ID}, 200)
	if it := f.item(t, child.ID); len(it.WaitingOn) != 0 || it.WaitsReleased {
		t.Fatalf("merged parent was not satisfied at PUT = %+v", it)
	}
	f.scan(t)
	if !f.item(t, child.ID).WaitsReleased || f.launches != 0 {
		t.Fatal("merged parent did not release on the first scan")
	}
}

// W3, W11: a failed parent blocks without changing Backlog or the person's Start.
func TestWaitsBlockedParentAndRepair(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.set(t, child.ID, []int{parent.ID}, 200)
	before := f.readJob(t, child.ID)
	f.edit(t, parent.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test", Code: "failed"} })
	f.scan(t)
	j := f.readJob(t, child.ID)
	if j.State != api.StateBacklog || j.NeedsYou != nil || j.Can.Run != before.Can.Run || j.Can.StartNow != before.Can.StartNow || j.Held != before.Held || j.Blocked == nil || j.Blocked.By != "waits_on" || j.Blocked.Job != parent.ID || !strings.Contains(j.Blocked.Reason, "api#1107, which failed at Test") || !j.Blocked.Since.Equal(f.now) {
		t.Fatalf("blocked = %+v", j)
	}
	blocked := f.item(t, child.ID)
	f.now = f.now.Add(time.Minute)
	f.scan(t)
	if !reflect.DeepEqual(f.item(t, child.ID), blocked) {
		t.Fatal("unchanged block was written again")
	}
	f.edit(t, parent.ID, func(it *factory.Item) { it.Failure = nil })
	f.scan(t)
	j = f.readJob(t, child.ID)
	if j.Blocked != nil || !reflect.DeepEqual(j.WaitingOn, []int{parent.ID}) {
		t.Fatalf("repaired = %+v", j)
	}
	f.merge(t, parent.ID)
	f.scan(t)
	if !f.item(t, child.ID).WaitsReleased {
		t.Fatal("repaired parent did not release")
	}
}

// W4: ended-unmerged reasons are derived, and bringing a dropped parent back clears its block.
func TestWaitsClosedParents(t *testing.T) {
	for _, tc := range []struct {
		name, why string
		change    func(*factory.Item)
	}{
		{"closed PR", "closed without merging", func(it *factory.Item) { it.PullRequest = &factory.PullRequest{State: factory.PRClosed} }},
		{"dropped", "was dropped", func(it *factory.Item) { it.DoneHow, it.State = "dropped", factory.StateDismissed }},
		{"closed locally", "was closed locally", func(it *factory.Item) { it.DoneHow = "closed_locally" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := waitsSetup(t)
			parent, child := f.addJob(t, 0), f.addJob(t, 0)
			f.set(t, child.ID, []int{parent.ID}, 200)
			f.edit(t, parent.ID, tc.change)
			f.scan(t)
			j := f.readJob(t, child.ID)
			if j.State != api.StateBacklog || j.Blocked == nil || !strings.Contains(j.Blocked.Reason, tc.why) {
				t.Fatalf("blocked = %+v", j)
			}
			f.edit(t, parent.ID, func(it *factory.Item) { it.DoneHow, it.State, it.PullRequest = "", factory.StateNew, nil })
			f.scan(t)
			if j := f.readJob(t, child.ID); j.Blocked != nil || len(j.WaitingOn) != 1 {
				t.Fatalf("restored = %+v", j)
			}
		})
	}
}

// W5: the real run and Stop doors permanently retire waiting and blocked waits.
func TestWaitsPersonStartOverrides(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		for _, stopBeforeScan := range []bool{false, true} {
			f := waitsSetup(t)
			parent, child := f.addJob(t, 0), f.addJob(t, 0)
			f.set(t, child.ID, []int{parent.ID}, 200)
			if blocked {
				f.edit(t, parent.ID, func(it *factory.Item) { it.Failure = &factory.Failure{Step: "test"} })
				f.scan(t)
			}
			f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/run", child.ID), "operator-secret", nil, 200, nil)
			if f.launches != 1 {
				t.Fatalf("launches = %d", f.launches)
			}
			if !stopBeforeScan {
				f.scan(t)
			}
			f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/stop", child.ID), "operator-secret", nil, 200, nil)
			f.scan(t)
			it := f.item(t, child.ID)
			if !it.WaitsReleased || it.Blocked != nil || len(it.WaitingOn) != 0 {
				t.Fatalf("started waits = %+v", it)
			}
			for _, event := range it.Activity {
				if strings.HasPrefix(event.What, "waits over:") {
					t.Fatalf("person's start recorded a release: %+v", event)
				}
			}
			f.merge(t, parent.ID)
			f.scan(t)
			if after := f.item(t, child.ID); !reflect.DeepEqual(after, it) || f.launches != 1 {
				t.Fatalf("inert job changed: %+v; launches = %d", after, f.launches)
			}
		}
	}
}

// W6: replacement, clearing and cycle checks operate before any write.
func TestWaitsChangesAndCycles(t *testing.T) {
	f := waitsSetup(t)
	a, b, c := f.addJob(t, 0), f.addJob(t, 0), f.addJob(t, 0)
	f.set(t, a.ID, []int{b.ID}, 200)
	f.set(t, a.ID, []int{c.ID}, 200)
	if it := f.item(t, a.ID); !reflect.DeepEqual(it.WaitsOn, []int{c.ID}) || !reflect.DeepEqual(it.WaitingOn, []int{c.ID}) {
		t.Fatal("waits were not replaced")
	}
	f.set(t, a.ID, []int{}, 200)
	if it := f.item(t, a.ID); len(it.WaitsOn) != 0 || len(it.WaitingOn) != 0 || it.Blocked != nil {
		t.Fatal("waits were not cleared")
	}
	f.set(t, a.ID, []int{b.ID}, 200)
	f.set(t, b.ID, []int{a.ID}, 422)
	f.set(t, b.ID, []int{c.ID}, 200)
	before := f.item(t, c.ID)
	f.set(t, c.ID, []int{a.ID}, 422)
	if !reflect.DeepEqual(f.item(t, c.ID), before) {
		t.Fatal("cycle refusal changed job")
	}
	for _, change := range []func(*factory.Item){
		func(it *factory.Item) { it.Stream = &factory.Stream{} },
		func(it *factory.Item) { it.Stream = nil; it.WaitsReleased = true },
	} {
		f.edit(t, c.ID, change)
		before := f.item(t, c.ID)
		f.set(t, c.ID, []int{b.ID}, 422)
		if !reflect.DeepEqual(f.item(t, c.ID), before) {
			t.Fatal("inert-job refusal changed job")
		}
	}
}

// W6: expect is checked and ownership is enforced, as at the run door.
func TestWaitsExpectAndOwner(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 0), f.addJob(t, 0)
	j := f.readJob(t, child.ID)
	f.edit(t, child.ID, func(it *factory.Item) { it.Title = "changed" })
	before := f.item(t, child.ID)
	f.request(t, "PUT", fmt.Sprintf("/v1/jobs/%d/waits_on", child.ID), "operator-secret", map[string]any{"waits_on": []int{parent.ID}, "expect": map[string]any{"version": j.Version}}, 409, nil)
	if !reflect.DeepEqual(f.item(t, child.ID), before) {
		t.Fatal("stale change wrote the job")
	}
	f.h.config.Mode = api.FactoryTeam
	f.edit(t, child.ID, func(it *factory.Item) { it.Assignee = "other" })
	before = f.item(t, child.ID)
	f.set(t, child.ID, []int{parent.ID}, 403)
	if !reflect.DeepEqual(f.item(t, child.ID), before) {
		t.Fatal("owner refusal wrote the job")
	}
}

// W7: restarting the handler loses no waits, and startup itself checks merged parents.
func TestWaitsRestart(t *testing.T) {
	f := waitsSetup(t)
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.set(t, child.ID, []int{parent.ID}, 200)
	f.scan(t)
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.h.stopWaits()
	if it := f.item(t, child.ID); it.WaitsReleased || !reflect.DeepEqual(it.WaitsOn, []int{parent.ID}) || !reflect.DeepEqual(it.WaitingOn, []int{parent.ID}) {
		t.Fatalf("restart = %+v", it)
	}
	f.merge(t, parent.ID)
	f.scan(t)
	if !f.item(t, child.ID).WaitsReleased || f.launches != 0 {
		t.Fatal("new handler did not release")
	}
	other := f.addJob(t, 0)
	f.set(t, other.ID, []int{parent.ID}, 200)
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.h.stopWaits()
	if !f.item(t, other.ID).WaitsReleased || f.launches != 0 {
		t.Fatal("startup did not release merged parents")
	}
}

// W9: the factory advertises waits independently of its API contract number.
func TestWaitsAboutFeature(t *testing.T) {
	f := waitsSetup(t)
	var about struct {
		Features []string `json:"features"`
	}
	f.request(t, "GET", "/v1/about", "", nil, 200, &about)
	if !slices.Contains(about.Features, "waits_on") {
		t.Fatalf("features = %v", about.Features)
	}
}
