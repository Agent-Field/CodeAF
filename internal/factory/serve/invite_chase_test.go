package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// INVITED PEOPLE WHO NEVER JOIN (owner decisions §13): a task done by someone
// invited waits for them; three days on, the task's owner gets "Hand to
// someone else?", once, and "Hand it to me" makes it theirs.
func TestATaskWaitingThreeDaysOnAnInviteAsksItsOwner(t *testing.T) {
	o := newInviteOrg(t)
	f := o.f
	made, _ := o.invite(t, o.priya, api.InviteNew{Email: "ana@agentfield.example", Name: "Ana", Workstreams: []string{o.product.ID}}, 201)
	inv := made.Invite
	var task factory.WorkstreamTask
	f.request(t, "POST", "/v1/workstreams/"+o.product.ID+"/tasks", o.priya, factory.TaskCreate{Title: "Design the badge",
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: inv.Ref, Name: "Ana"}}, 201, &task)

	read := func() factory.WorkstreamTask {
		t.Helper()
		var got factory.WorkstreamTask
		f.request(t, "GET", "/v1/workstreams/"+o.product.ID+"/tasks/"+task.ID, o.priya, nil, 200, &got)
		return got
	}
	f.h.chaseInvites(f.now.Add(factory.InviteChaseAfter - time.Hour))
	if got := read(); len(got.Asks) != 0 {
		t.Fatalf("asked before three days: %+v", got.Asks)
	}
	f.h.chaseInvites(f.now.Add(factory.InviteChaseAfter))
	f.h.chaseInvites(f.now.Add(factory.InviteChaseAfter + time.Hour))
	got := read()
	if got.State != factory.TaskNeeds || len(got.Asks) != 1 {
		t.Fatalf("after three days the owner is not asked once: %+v", got)
	}
	ask := got.Asks[0]
	if ask.Title != "Hand to someone else?" || ask.Decider == nil || ask.Decider.ID != got.Owner.ID {
		t.Fatalf("the chase Ask: %+v (owner %+v)", ask, got.Owner)
	}
	f.request(t, "POST", "/v1/workstreams/"+o.product.ID+"/tasks/"+task.ID+"/answer", o.priya,
		api.TaskAnswer{Ask: ask.ID, Option: factory.InviteChaseMe}, 200, nil)
	got = read()
	if got.DoneBy.Kind != factory.TaskByPerson || factory.InvitedPerson(got.DoneBy.ID) || got.DoneBy.ID != got.Owner.ID || len(got.Asks) != 0 {
		t.Fatalf("Hand it to me left it with the invite: %+v", got)
	}
}
