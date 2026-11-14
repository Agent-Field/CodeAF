package serve_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// A WAIT ON A PERSON IS ONE WAIT FROM THE MOMENT IT BEGAN, whether or not the
// road that began it gave it a clock of its own. A start held for approval,
// a Stop undone at a gate, a take-over and an AgentField run's approval wait
// with no ask clock and no step clock; needs_you.since fell back to the job's
// Changed, which every stamped write moves. So a rename, a priority, a budget
// or a note recorded another needs_you each, which the desktop app keys as a
// new occurrence (needs_you:<need>:<at>) and wakes its agent for again, and
// factory.job's needs_you.since moved with every one.
func TestJobEventsRecordAClocklessWaitOnce(t *testing.T) {
	for _, tt := range []struct {
		name  string
		shape string
		need  string
		wait  func(*testing.T, *stopJob)
	}{
		{"a start held for its approval", "", factory.NeedsApproveStart, func(t *testing.T, s *stopJob) {
			// The shape intake's ask_first writes: no ask, no run.
			if err := s.f.st.Update(s.job.ID, func(cur *factory.Item) error {
				cur.State, cur.QKind = factory.StateNeedsYou, factory.QKindAskFirst
				cur.AskedFirstReason = "touches protected paths"
				cur.Question = "Approval needed: start · touches protected paths"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{"a Stop undone at the plan gate", "gate", factory.NeedsApprovePlan, func(t *testing.T, s *stopJob) {
			s.door(t, "stop", "")
			s.door(t, "undo", "")
		}},
		{"a take-over", "running", factory.NeedsQuestion, func(t *testing.T, s *stopJob) {
			s.door(t, "control", `{"mode":"take_over"}`)
		}},
		{"an AgentField run's approval", "running", factory.NeedsQuestion, func(t *testing.T, s *stopJob) {
			s.report(t, api.WorkerReport{AgentField: &factory.AgentFieldRun{Target: "planner.plan", ExecutionID: "exec-1",
				Waiting: &factory.AgentFieldWait{Execution: "exec-1", RequestID: "req-1", Question: "Approve the plan?"}}})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newWaitJob(t, tt.shape)
			start, err := live.ReadOutcomes(s.f.st.Root(), nil, 100)
			if err != nil {
				t.Fatal(err)
			}
			tt.wait(t, s)
			var first api.Job
			s.f.request(t, "GET", s.path, "", nil, 200, &first)
			if first.State != api.StateNeedsYou || first.NeedsYou == nil || first.NeedsYou.Kind != tt.need {
				t.Fatalf("the job does not wait on a person: %s %+v", first.State, first.NeedsYou)
			}
			// Writes that have nothing to do with the wait, each stamped.
			for i, write := range []func(){
				func() { s.update(t, func(cur *factory.Item) { cur.Title = "a job renamed while it waits" }) },
				func() { s.door(t, "priority", `{"priority":"high"}`) },
				func() { s.door(t, "raise_budget", `{"budget":20}`) },
				func() {
					s.update(t, func(cur *factory.Item) { cur.Notes = append(cur.Notes, "a note settled in Talk") })
				},
			} {
				write()
				var job api.Job
				s.f.request(t, "GET", s.path, "", nil, 200, &job)
				if job.NeedsYou == nil || job.NeedsYou.Kind != tt.need || !job.NeedsYou.Since.Equal(first.NeedsYou.Since) {
					t.Errorf("write %d moved the wait: needs_you %+v, first since %s", i+1, job.NeedsYou, first.NeedsYou.Since)
				}
			}
			got, err := live.ReadOutcomes(s.f.st.Root(), &start.Cursor, 100)
			if err != nil || got.Reset {
				t.Fatalf("outcomes: %+v %v", got, err)
			}
			var waits []live.Outcome
			for _, e := range got.Events {
				if e.Kind == "needs_you" {
					waits = append(waits, e)
				}
			}
			if len(waits) != 1 || waits[0].Need != tt.need || !waits[0].At.Equal(first.NeedsYou.Since) {
				t.Fatalf("one wait recorded %d needs_you outcomes %+v; factory.job waits since %s", len(waits), waits, first.NeedsYou.Since)
			}
		})
	}
}

// newWaitJob is a job on the Factory service in shape (a newStopJob), or an
// unstarted job when shape is "".
func newWaitJob(t *testing.T, shape string) *stopJob {
	t.Helper()
	if shape != "" {
		return newStopJob(t, shape)
	}
	f := workerSetupAs(t, "sam")
	it := f.add(t, "sam")
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNew
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return &stopJob{f: f, job: it, path: fmt.Sprintf("/v1/jobs/%d", it.ID)}
}

// update is a stamped store write on the job, a second after the last.
func (s *stopJob) update(t *testing.T, change func(*factory.Item)) {
	t.Helper()
	s.f.clock.Add(int64(time.Second))
	// The store stamps Changed from the wall clock: make sure it moves.
	time.Sleep(2 * time.Millisecond)
	if err := s.f.st.Update(s.job.ID, func(cur *factory.Item) error {
		change(cur)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
