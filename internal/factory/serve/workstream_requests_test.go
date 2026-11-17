package serve

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Flow 7 (Workstreams final spec): work for another team is a request into
// their workstream. santosh asks Engineering, which abir looks after and
// santosh is not in; it becomes abir's task there.
func TestARequestIntoAnotherWorkstreamBecomesATaskForItsOwnerOfIncomingWork(t *testing.T) {
	o := newWSOrg(t)
	eng := o.create(t, "abir", api.WorkstreamNew{Name: "Engineering"})
	mine := o.create(t, "santosh", api.WorkstreamNew{Name: "Marketing"})
	from := o.task(t, "santosh", "/v1/workstreams/"+mine.ID, api.TaskCreate{Title: "Ship the launch post", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})

	// santosh is not in Engineering, and still finds it to ask.
	o.do(t, "santosh", "GET", "/v1/workstreams/"+eng.ID+"/tasks", -1, nil, 404, nil)
	var can api.RequestableWorkstreams
	o.do(t, "santosh", "GET", "/v1/workstreams/requestable", -1, nil, 200, &can)
	i := slices.IndexFunc(can.Workstreams, func(w api.RequestableWorkstream) bool { return w.ID == eng.ID })
	if i < 0 || can.Workstreams[i].In || can.Workstreams[i].Receiver == nil || can.Workstreams[i].Receiver.ID != "abir" {
		t.Fatalf("requestable %+v", can.Workstreams)
	}

	var sent api.RequestSent
	o.do(t, "santosh", "POST", "/v1/workstreams/"+eng.ID+"/requests", -1,
		factory.TaskRequest{Title: "Fix the share link on recipes", Request: "The link opens the home page.", FromWS: mine.ID, FromTask: from.ID}, 201, &sent)
	if sent.Owner.ID != "abir" || sent.WSName != "Engineering" || sent.Task != nil {
		t.Fatalf("sent %+v: santosh sees what he sent, never Engineering's task", sent)
	}

	got := o.tasks(t, "abir", "/v1/workstreams/"+eng.ID)
	if len(got) != 1 {
		t.Fatalf("Engineering holds %+v", got)
	}
	task := got[0]
	if task.Owner.ID != "abir" || task.DoneBy.ID != "abir" || task.State != factory.TaskUpNext || task.CameFrom.ID != "santosh" || task.CameFrom.WSName != "Marketing" {
		t.Fatalf("the request became %+v", task)
	}
	var thread api.WorkstreamTaskEvents
	o.do(t, "abir", "GET", "/v1/workstreams/"+eng.ID+"/tasks/"+task.ID+"/events", -1, nil, 200, &thread)
	if len(thread.Events) != 1 || thread.Events[0].Text != "Requested by Priya Nair from Marketing" {
		t.Fatalf("thread %+v", thread.Events)
	}
	// The task it came from says where it went.
	o.do(t, "santosh", "GET", "/v1/workstreams/"+mine.ID+"/tasks/"+from.ID+"/events", -1, nil, 200, &thread)
	if n := len(thread.Events); n == 0 || thread.Events[n-1].Text != "Sent a request to Engineering: Fix the share link on recipes" {
		t.Fatalf("from thread %+v", thread.Events)
	}

	// A request needs words; an archived or unknown workstream takes none.
	o.do(t, "santosh", "POST", "/v1/workstreams/"+eng.ID+"/requests", -1, factory.TaskRequest{Title: "  "}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/workstreams/ws_nope/requests", -1, factory.TaskRequest{Title: "x"}, 404, nil)
}
