package serve_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// stopJob is a job on the Factory service, started by its person (the start
// door takes any earlier stop down, factory.ClearPersonStop) and pulled by a
// runner, which reports it into one of the shapes below.
type stopJob struct {
	f      *workerFixture
	worker api.WorkerAttached
	lease  string
	job    factory.Item
	path   string
}

func newStopJob(t *testing.T, shape string) *stopJob {
	t.Helper()
	f := workerSetupAs(t, "sam")
	worker := f.attach(t, "sam")
	it, err := f.st.Create(factory.Item{Title: "a job a road stops", Repo: "owner/repo", State: factory.StateQueued, Assignee: "sam",
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, On: true, Until: factory.UntilDone, Max: 1},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
			{Name: "write", Kind: factory.StageChat, On: true, Until: factory.UntilDone},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		factory.ClearPersonStop(cur)
		if shape == "budget" {
			cur.Cap, cur.Autonomy = 1, factory.AutonomyAuto
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &stopJob{f: f, worker: worker, job: it, path: fmt.Sprintf("/v1/jobs/%d", it.ID)}
	s.lease = "/v1/workers/leases/" + f.pull(t, worker).Lease.ID
	switch shape {
	case "running":
		s.report(t, api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: string(factory.PhaseRunning), Round: 1}}})
	case "gate":
		s.report(t, api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: string(factory.PhaseDone), Round: 1}}})
		s.report(t, api.WorkerReport{Question: &api.Question{Kind: factory.QKindApprove, FromStep: factory.ApproveName, Text: "plan is ready · continue, or send it back?"}})
	case "budget":
		s.report(t, api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: string(factory.PhaseRunning), Round: 1}}})
		s.report(t, api.WorkerReport{Cost: 1.5, Question: &api.Question{Kind: "cap", FromStep: "plan", Text: "budget of $1 reached · job total $1.50 · raise it to $5, or stop?"}})
	case "stuck":
		s.report(t, api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: string(factory.PhaseRunning), Round: 1}}})
		s.report(t, api.WorkerReport{Question: &api.Question{Kind: factory.QKindOutOfAttempts, FromStep: "plan", Text: "plan is out of attempts · try once more?"}})
	}
	return s
}

func (s *stopJob) report(t *testing.T, report api.WorkerReport) {
	t.Helper()
	s.f.clock.Add(int64(time.Second))
	s.f.request(t, "POST", s.lease+"/report", s.worker.Credential, report, 200, nil)
}

func (s *stopJob) door(t *testing.T, verb, body string) {
	t.Helper()
	s.f.clock.Add(int64(time.Second))
	var payload any
	if body != "" {
		payload = rawJSON(body)
	}
	s.f.request(t, "POST", s.path+"/"+verb, "", payload, 200, nil)
}

// runnerStops is the runner's own end of a run its loop stopped on a
// person's answer: the loop's stop write on its copy of the job (the same
// write as run.StopItem's), what the worker reports from it (run.Diff, in the
// wire's words) and its done, answered with the statuses given.
func (s *stopJob) runnerStops(t *testing.T, reported, done int) {
	t.Helper()
	mirror, err := s.f.st.Get(s.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The runner's copy still waits where the person answered it.
	mirror.State, mirror.Stream.Phases[mirror.Stream.Cur].State = factory.StateNeedsYou, factory.PhaseWaiting
	stopped := mirror
	stopped.Stream = &factory.Stream{}
	*stopped.Stream = *mirror.Stream
	stopped.Stream.Phases = append([]factory.Phase(nil), mirror.Stream.Phases...)
	s.f.clock.Add(int64(time.Second))
	if err := run.StopItem(&stopped, time.Unix(0, s.f.clock.Load()).UTC()); err != nil {
		t.Fatal(err)
	}
	report, ok := run.Diff(mirror, stopped)
	if !ok {
		t.Fatal("the runner's stop has nothing to report")
	}
	s.f.request(t, "POST", s.lease+"/report", s.worker.Credential, api.FromRunReport(report), reported, nil)
	s.f.request(t, "POST", s.lease+"/done", s.worker.Credential, api.WorkerDone{State: api.StateBacklog}, done, nil)
}

type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }

// EVERY ROAD THAT ENDS A RUN RECORDS WHAT factory.job THEN SAYS OF THE JOB.
// Each road below is the real door (or the runner's real report) on a job a
// person started, and each writes a failure coded stopped on the job or on
// the step it cut. A person's own stop, by any door or by an answer, leaves
// the job in Backlog and records nothing; a reassignment hands it on in
// Backlog and records nothing; a Close or Reject records only its finish;
// a step's own failure leaves it in Needs you and records it failed. The
// desktop app reads both the outcomes and factory.job, and wakes its agent
// for a failed job from either: they must not disagree.
func TestJobEventsStopRoadsAgreeWithFactoryJob(t *testing.T) {
	tests := []struct {
		name  string
		shape string
		road  func(*testing.T, *stopJob)
		state string
		want  []string
	}{
		{"Stop at the plan gate", "gate", func(t *testing.T, s *stopJob) { s.door(t, "stop", "") }, api.StateBacklog, nil},
		{"Stop in a running step", "running", func(t *testing.T, s *stopJob) { s.door(t, "stop", "") }, api.StateBacklog, nil},
		{"a step's own Stop", "running", func(t *testing.T, s *stopJob) { s.door(t, "steps/plan/stop", "") }, api.StateBacklog, nil},
		{"Close at the plan gate", "gate", func(t *testing.T, s *stopJob) { s.door(t, "close", "") }, api.StateDone, []string{"finished:closed_locally"}},
		{"Reject at the plan gate", "gate", func(t *testing.T, s *stopJob) { s.door(t, "reject", `{"words":"not this plan"}`) }, api.StateDone, []string{"finished:closed_locally"}},
		{"cancel out of attempts, nobody holds it", "stuck", func(t *testing.T, s *stopJob) {
			// The runner hands the waiting job back, so the answer is the service's own.
			s.f.request(t, "POST", s.lease+"/done", s.worker.Credential, api.WorkerDone{State: api.StateNeedsYou}, 200, nil)
			s.door(t, "answer", `{"choice":"cancel_job"}`)
		}, api.StateBacklog, nil},
		{"reassign a running job", "running", func(t *testing.T, s *stopJob) { s.door(t, "change_assignee", `{"assignee":"mei"}`) }, api.StateBacklog, nil},
		{"reassign a job at its plan gate", "gate", func(t *testing.T, s *stopJob) { s.door(t, "change_assignee", `{"assignee":"mei"}`) }, api.StateBacklog, nil},
		{"unassign a running job", "running", func(t *testing.T, s *stopJob) { s.door(t, "unassign", "") }, api.StateBacklog, nil},
		{"a step fails on its runner", "running", func(t *testing.T, s *stopJob) {
			s.report(t, api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: string(factory.PhaseFailed), Round: 1,
				Failure: &api.Failure{Code: factory.CodeToolError, Detail: "the worktree is gone", Next: factory.FailureStopped, At: time.Unix(0, s.f.clock.Load()).UTC()}}}})
			s.f.request(t, "POST", s.lease+"/done", s.worker.Credential, api.WorkerDone{State: api.StateBacklog}, 200, nil)
		}, api.StateNeedsYou, []string{"failed:" + factory.CodeToolError}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkStopRoad(t, tt.shape, tt.road, tt.state, tt.want) })
	}
}

// checkStopRoad drives one road on a job in shape and checks that factory.job
// and the outcomes it recorded say the same of the job.
func checkStopRoad(t *testing.T, shape string, road func(*testing.T, *stopJob), state string, want []string) {
	t.Helper()
	s := newStopJob(t, shape)
	before, err := live.ReadOutcomes(s.f.st.Root(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	road(t, s)
	var job api.Job
	s.f.request(t, "GET", s.path, "", nil, 200, &job)
	stored, err := s.f.st.Get(s.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The road really ended a run: the stop (or the failure) is on the
	// step it cut, so the test does not pass on a road that did nothing.
	cut := false
	for _, ph := range stored.Stream.Phases {
		if ph.Failure != nil {
			cut = true
		}
	}
	if !cut {
		t.Fatalf("the road cut no step: %+v", stored.Stream.Phases)
	}
	got, err := live.ReadOutcomes(s.f.st.Root(), &before.Cursor, 100)
	if err != nil || got.Reset {
		t.Fatalf("outcomes: %+v %v", got, err)
	}
	var kinds []string
	for _, e := range got.Events {
		kinds = append(kinds, e.Kind+":"+e.How+e.Code)
	}
	if job.State != state || strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Errorf("factory.job reads %s (failure %+v, person_stopped %v), outcomes %v; want %s and %v",
			job.State, job.Failure, stored.PersonStopped != nil && *stored.PersonStopped, kinds, state, want)
	}
	// What the desktop app keys a failed job by agrees with what was recorded.
	if job.State == api.StateNeedsYou && job.Failure != nil {
		if len(got.Events) != 1 || !got.Events[0].At.Equal(job.Failure.At) {
			t.Errorf("factory.job's failure at %s, outcomes %+v", job.Failure.At, got.Events)
		}
	}
	// A full read after a reset forms no occurrence the outcomes never had:
	// an app that followed them already holds a receipt for each one.
	zero := int64(0)
	all, err := live.ReadOutcomes(s.f.st.Root(), &zero, 500)
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]bool{}
	for _, e := range all.Events {
		recorded[outcomeKey(e)] = true
	}
	for _, key := range fullReadKeys(job) {
		if !recorded[key] {
			t.Errorf("a full read of factory.job keys %s, which no outcome recorded (%v)", key, recorded)
		}
	}
}

// outcomeKey is the occurrence key the desktop app forms from an outcome
// (workstream_watch_event).
func outcomeKey(e live.Outcome) string {
	at := e.At.UTC().Format(time.RFC3339Nano)
	if e.Kind == "needs_you" {
		return "needs_you:" + e.Need + ":" + at
	}
	return e.Kind + ":" + at
}

// fullReadKeys is the same occurrences formed from factory.job, by the rule
// PROTOCOL.md's job outcomes section gives a full read: a failure is a failed
// job only in needs_you, or on a done job when it is no stop; a wait is keyed
// only on a job with no failure.
func fullReadKeys(j api.Job) []string {
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
	var keys []string
	merged := j.PR != nil && j.PR.MergedAt != nil && !j.PR.MergedAt.IsZero()
	if merged {
		keys = append(keys, "merged:"+stamp(*j.PR.MergedAt))
	}
	if j.State == api.StateDone && !j.DoneAt.IsZero() && !merged && j.DoneHow != "merged" {
		keys = append(keys, "finished:"+stamp(j.DoneAt))
	}
	if f := j.Failure; f != nil && (j.State == api.StateNeedsYou || j.State == api.StateDone && f.Code != factory.CodeStopped) {
		keys = append(keys, "failed:"+stamp(f.At))
	}
	if n := j.NeedsYou; n != nil && j.State == api.StateNeedsYou && j.Failure == nil && !n.Since.IsZero() {
		keys = append(keys, "needs_you:"+n.Kind+":"+stamp(n.Since))
	}
	return keys
}

// A STOP THE RUNNER'S LOOP MADE IS THE PERSON'S STOP ON THE SERVED COPY TOO.
// A person's cancel to a step out of attempts, or no to the budget, reaches
// the runner that holds the job; its loop ends the run on its own copy, marks
// the stop as the person's there, and reports the cut step to the service,
// whose copy is the one factory.job reads and outcomes are recorded from. The
// mark never travelled: the served copy, cleared of any earlier stop by its
// Start, read as a failed job in Needs you while the runner's copy waited in
// Backlog, and the outcomes (nothing) disagreed with factory.job (failed).
func TestJobEventsRunnerMadeStopsAgreeWithFactoryJob(t *testing.T) {
	for _, tt := range []struct {
		name, shape, answer string
		reported, done      int
	}{
		{"cancel out of attempts", "stuck", `{"choice":"cancel_job"}`, 200, 200},
		// The job is still over its budget when the runner reports the stop,
		// so the service keeps the report and withdraws the lease (409), and
		// the runner's done finds no lease.
		{"no to the budget", "budget", `{"yes":false}`, 409, 409},
	} {
		t.Run(tt.name, func(t *testing.T) {
			checkStopRoad(t, tt.shape, func(t *testing.T, s *stopJob) {
				s.door(t, "answer", tt.answer)
				s.runnerStops(t, tt.reported, tt.done)
			}, api.StateBacklog, nil)
		})
	}
}
