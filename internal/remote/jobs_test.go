package remote

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// jobsFakeAgent is a fakeAgent that also owns the job door, so the server's
// optional-interface check finds it.
type jobsFakeAgent struct {
	*fakeAgent
	rows []session.JobNotice
}

func (j jobsFakeAgent) JobNotices() []session.JobNotice { return j.rows }

func TestJobsListCrossesTheWire(t *testing.T) {
	rows := []session.JobNotice{
		{ID: 4, Name: "build", Command: "make build", Kind: session.JobKindCommand},
		{ID: 2, Command: "sleep 30", Kind: session.JobKindCommand},
	}

	// The engine end: the server answers the call from the agent's own door.
	l := dialAgent(t, engineOn(&fakeAgent{}))
	l.hello(Hello{Version: Version, Workspace: "api"})
	if got := decode[[]session.JobNotice](t, l.ok(1, MethodJobsList, nil).Payload); len(got) != 0 {
		t.Fatalf("an agent without the door answered %+v", got)
	}
	_ = l.end()
	withDoor := engineOn(&fakeAgent{})
	withDoor.Agent = jobsFakeAgent{fakeAgent: &fakeAgent{}, rows: rows}
	l = dialAgent(t, withDoor)
	l.hello(Hello{Version: Version, Workspace: "api"})
	if got := decode[[]session.JobNotice](t, l.ok(1, MethodJobsList, nil).Payload); !reflect.DeepEqual(got, rows) {
		t.Fatalf("server answered %+v, want %+v", got, rows)
	}
	_ = l.end()

	// The surface end: the client decodes the same rows, as one read-only call.
	client, e := newEngine(t)
	e.answers[MethodJobsList] = rows
	got, err := client.Agent().JobNotices()
	if err != nil || !reflect.DeepEqual(got, rows) {
		t.Fatalf("JobNotices = %+v, %v; want %+v", got, err, rows)
	}
	if classify(MethodJobsList) != classify(MethodPlanTasks) {
		t.Fatalf("Jobs.List is not in the read-only call class")
	}
}
