package serve_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A RUN A CHANGE OF ASSIGNEE CUT HAS ENDED, AND ITS JOB OFFERS RUN AS NEW.
// The reset that hands a job on stopped writing its stop on the job as a
// failure (the job waits in Backlog for its new person, nobody's failure),
// and Run as new, which read that failure, went with it: the new person could
// no longer start the request anew from a run that had ended all the same,
// and the run_as_new door refused them.
func TestJobEventsAHandedOnRunOffersRunAsNew(t *testing.T) {
	for _, tt := range []struct {
		name, shape, verb, body, person string
	}{
		{"reassign a running job", "running", "change_assignee", `{"assignee":"mei"}`, "mei"},
		{"reassign a job at its plan gate", "gate", "change_assignee", `{"assignee":"mei"}`, "mei"},
		{"unassign a running job", "running", "unassign", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newStopJob(t, tt.shape)
			s.door(t, tt.verb, tt.body)
			stored, err := s.f.st.Get(s.job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Person() != tt.person || factory.JobState(stored) != api.StateBacklog || stored.Failure != nil {
				t.Fatalf("the job was not handed on: person %q, state %s, failure %+v", stored.Person(), factory.JobState(stored), stored.Failure)
			}
			if !factory.CanRunAsNew(stored) {
				t.Errorf("a run the hand-on cut does not offer Run as new: stream %+v", stored.Stream)
			}
			if can := api.CanFor(stored, "mei", time.Unix(0, s.f.clock.Load()).UTC()); !can.RunAsNew {
				t.Errorf("the job's new person is not offered Run as new: %+v", can)
			}
		})
	}
	// A job that never ran offers nothing to run anew.
	s := newWaitJob(t, "")
	stored, err := s.f.st.Get(s.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if factory.CanRunAsNew(stored) {
		t.Errorf("a job that never ran offers Run as new")
	}
}
