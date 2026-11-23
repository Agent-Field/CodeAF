package serve

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FS0/FS15 privacy: a machine, its checkouts, slots and heartbeats are the
// caller's own and nobody else's, the operator included. Three people give
// their machine the SAME id, so a filter on the id alone would leak.
func TestMachinesAreOnlyTheCallersOwnEvenWithTheSameMachineID(t *testing.T) {
	c := capacitySetup(t)
	pri := c.attach(t, c.priya, "priya", "mbp", 0)
	raj := c.attach(t, c.raj, "raj", "mbp", 3)
	own := c.attach(t, "operator-secret", "owner", "mbp", 0)
	job := c.add(t, "raj")
	var pull api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", raj.Credential, nil, 200, &pull)
	if pull.Job.ID != job.ID {
		t.Fatalf("raj pulled %d, want %d", pull.Job.ID, job.ID)
	}
	secrets := []string{pri.Credential, raj.Credential, own.Credential, "operator-secret", c.priya, c.raj, pull.Lease.ID}

	for _, who := range []struct{ name, token, handle string }{{"priya", c.priya, "priya"}, {"raj", c.raj, "raj"}, {"operator", "operator-secret", "owner"}} {
		var list api.Runners
		body := c.request(t, "GET", "/v1/runners", who.token, nil, 200, &list)
		if len(list.Runners) != 1 || list.Runners[0].Owner != who.handle || list.Runners[0].ID != "mbp" {
			t.Fatalf("%s sees runners %+v", who.name, list.Runners)
		}
		if (who.handle == "raj") != (len(list.Runners[0].Jobs) == 1) || (who.handle == "raj") != (list.Runners[0].Slots == 3) {
			t.Fatalf("%s: own row mixed with another's: %+v", who.name, list.Runners[0])
		}
		noSecrets(t, who.name+" /v1/runners", body, secrets[:2:2], who.token)

		var people api.People
		body = c.request(t, "GET", "/v1/people", who.token, nil, 200, &people)
		for _, p := range people.People {
			if p.Handle != who.handle && (len(p.Runners) != 0 || len(p.Machines) != 0) {
				t.Fatalf("%s sees @%s's machines: %+v", who.name, p.Handle, p)
			}
			if p.Handle == "raj" && p.AssignedNow != 1 {
				t.Fatalf("%s: raj's job count lost: %+v", who.name, p)
			}
		}
		noSecrets(t, who.name+" /v1/people", body, secrets, who.token)

		for _, other := range []string{"priya", "raj"} {
			var p api.Person
			body = c.request(t, "GET", "/v1/people/"+other, who.token, nil, 200, &p)
			if other != who.handle && (len(p.Runners) != 0 || len(p.Machines) != 0) {
				t.Fatalf("%s reads @%s's card with machines: %s", who.name, other, body)
			}
			if other == who.handle && len(p.Runners) != 1 {
				t.Fatalf("%s lost their own runner on their card: %s", who.name, body)
			}
			if other == "raj" && (p.AssignedNow != 1 || p.Status == "") {
				t.Fatalf("%s: raj's card lost counts or status: %+v", who.name, p)
			}
		}

		var one api.Job
		body = c.request(t, "GET", "/v1/jobs/"+strconv.Itoa(job.ID), who.token, nil, 200, &one)
		if one.HeldBy == nil || one.HeldBy.Handle != "raj" {
			t.Fatalf("%s: who holds the job is gone: %s", who.name, body)
		}
		if who.handle == "raj" {
			if one.HeldBy.Machine != "mbp" || one.Runner != "mbp" {
				t.Fatalf("raj lost their own machine on the job: %s", body)
			}
		} else if one.HeldBy.Machine != "" || !one.HeldBy.HeartbeatAt.IsZero() || one.Runner != "" || strings.Contains(string(body), `"mbp"`) {
			t.Fatalf("%s sees raj's machine on the job: %s", who.name, body)
		}
	}

	var me struct {
		Machines []api.Machine `json:"machines"`
		Runners  []api.Runner  `json:"runners"`
	}
	c.request(t, "GET", "/v1/me", c.priya, nil, 200, &me)
	if len(me.Runners) != 1 || me.Runners[0].Owner != "priya" || len(me.Runners[0].Jobs) != 0 {
		t.Fatalf("priya's /v1/me: %+v", me)
	}
}

func noSecrets(t *testing.T, where, body string, secrets []string, self string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && secret != self && strings.Contains(body, secret) {
			t.Fatalf("%s leaks a secret that is not the caller's", where)
		}
	}
}

// A job's stored machine ids (failure, desk, publish wait, take conflict) have
// no owner on them, and the assignee proves nothing about them: a reassigned
// job keeps the first worker's id. They are named only when the caller's own
// live hold or a machine only the caller registered proves them.
func TestHistoricalMachineIDsNeedProofOfOwnership(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "mbp", 0)
	c.attach(t, c.raj, "raj", "mbp", 0)
	c.attach(t, c.raj, "raj", "raj-only", 0)
	when := c.now
	mk := func(assignee, machine string) factory.Item {
		it, err := c.st.Create(factory.Item{
			Title: "history", Repo: "acme/api", State: factory.StateQueued, Assignee: assignee, WorktreeMachine: machine,
			Publishing: true, Changed: when,
			Failure: &factory.Failure{Step: "test", Code: factory.CodeToolError, Detail: "red", Attempts: 1, Machine: machine, Next: factory.FailureStopped, At: when},
		})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	ambiguous := mk("priya", "mbp")       // assignee is priya, id is also raj's
	rajsOwn := mk("raj", "raj-only")      // only raj registered it
	handedOver := mk("priya", "raj-only") // priya is assignee, machine was raj's

	read := func(token string, it factory.Item) (api.Job, string) {
		var j api.Job
		body := c.request(t, "GET", "/v1/jobs/"+strconv.Itoa(it.ID), token, nil, 200, &j)
		return j, body
	}
	named := func(j api.Job, body, id string) bool {
		return j.Runner == id || (j.Failure != nil && (j.Failure.Machine == id || j.Failure.Runner == id)) ||
			(j.Desk != nil && j.Desk.WorktreeMachine == id) || strings.Contains(body, `"`+id+`"`) || strings.Contains(body, "waits for "+id)
	}
	for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"raj", c.raj}, {"operator", "operator-secret"}} {
		for _, it := range []factory.Item{ambiguous} {
			j, body := read(who.token, it)
			if named(j, body, "mbp") {
				t.Fatalf("%s: an id two people registered is named: %s", who.name, body)
			}
		}
		j, body := read(who.token, rajsOwn)
		if want := who.name == "raj"; named(j, body, "raj-only") != want {
			t.Fatalf("%s: raj's own machine named=%v: %s", who.name, !want, body)
		}
		j, body = read(who.token, handedOver)
		if want := who.name == "raj"; named(j, body, "raj-only") != want {
			t.Fatalf("%s: assignee priya must not prove raj's machine; named=%v: %s", who.name, !want, body)
		}
	}

	// The queue list carries the same job wire.
	for _, token := range []string{c.priya, "operator-secret"} {
		var list struct{ Jobs []api.Job }
		body := c.request(t, "GET", "/v1/queue", token, nil, 200, &list)
		if strings.Contains(body, "raj-only") {
			t.Fatalf("job list names raj's machine: %s", body)
		}
	}

	// Taking a job whose recorded machine is not the taker's names nothing.
	for _, it := range []factory.Item{rajsOwn, ambiguous} {
		other := c.priya
		if it.ID == ambiguous.ID {
			other = c.raj
		}
		body := c.request(t, "POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/take", other, nil, 409, nil)
		if strings.Contains(body, "raj-only") || (it.ID == ambiguous.ID && strings.Contains(body, `"mbp"`)) {
			t.Fatalf("take conflict names a machine: %s", body)
		}
	}
}

// A solo factory has one person: nothing is withheld from them, and the
// worker credentials still never travel in a job or runner body.
func TestSoloKeepsItsMachineAndLeaksNoWorkerSecret(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	var worker api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "owner", Kind: api.RunnerMac, Checkouts: []api.Checkout{{Repo: "acme/api", Path: "/code/api"}}, TakesUnassigned: map[string]bool{"*": true}}, 201, &worker)
	it, err := f.st.Create(factory.Item{Title: "solo", Repo: "acme/api", State: factory.StateQueued, WorktreeMachine: "mbp", Changed: f.now,
		Failure: &factory.Failure{Step: "test", Code: factory.CodeToolError, Detail: "red", Attempts: 1, Machine: "mbp", Next: factory.FailureStopped, At: f.now}})
	if err != nil {
		t.Fatal(err)
	}
	var j api.Job
	body := f.request(t, "GET", "/v1/jobs/"+strconv.Itoa(it.ID), "operator-secret", nil, 200, &j)
	if j.Failure == nil || j.Failure.Machine != "mbp" || j.Desk == nil || j.Desk.WorktreeMachine != "mbp" {
		t.Fatalf("solo lost its own machine: %s", body)
	}
	for _, path := range []string{"/v1/jobs/" + strconv.Itoa(it.ID), "/v1/queue", "/v1/runners"} {
		got := f.request(t, "GET", path, "operator-secret", nil, 200, nil)
		noSecrets(t, "solo "+path, got, []string{worker.Credential}, "operator-secret")
	}
}
