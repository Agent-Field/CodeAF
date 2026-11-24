package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// "Who can do what" (Workstreams final spec), the rows that are not everyone's:
// stopping or moving a task is its owner's or whoever looks after the
// workstream, never merely anyone who edits it. Webhook reliability is
// Platform's (abir and santosh look after it); Growth (mia) edits it by share.
func TestOnlyTheOwnerOrWhoeverLooksAfterItStopsOrMovesATask(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, pricing, _ := o.orgScene(t)
	path := "/v1/workstreams/" + webhook.ID
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	mine := o.task(t, "santosh", path, api.TaskCreate{Title: "Retry the dead letters", DoneBy: afi})
	if mine.Owner.ID != "santosh" {
		t.Fatalf("santosh's task is owned by %+v", mine.Owner)
	}

	// mia edits Webhook reliability and Pricing page, and still may not stop
	// or move santosh's task; she comments or hands it on instead.
	var refused struct {
		Error string `json:"error"`
	}
	o.do(t, "mia", "PATCH", path+"/tasks/"+mine.ID, mine.Rev, map[string]any{"state": "stopped"}, 403, &refused)
	if refused.Error != factory.ErrTaskNotStopper.Error() {
		t.Fatalf("mia's stop was refused with %q", refused.Error)
	}
	o.do(t, "mia", "PATCH", path+"/tasks/"+mine.ID, mine.Rev, map[string]any{"move_to": pricing.ID}, 403, nil)
	o.do(t, "mia", "POST", path+"/tasks/"+mine.ID+"/comment", -1, map[string]any{"text": "Can this wait a day?"}, 201, nil)

	// abir looks after it (Platform owns it), so abir may stop someone else's task.
	var stopped api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+mine.ID, mine.Rev, map[string]any{"state": "stopped"}, 200, &stopped)
	if stopped.State != factory.TaskStopped {
		t.Fatalf("abir's stop left %q", stopped.State)
	}

	// The owner always may: mia moves her own task to Pricing page.
	hers := o.task(t, "mia", path, api.TaskCreate{Title: "Explain retries on the pricing page", DoneBy: afi})
	o.do(t, "mia", "PATCH", path+"/tasks/"+hers.ID, hers.Rev, map[string]any{"move_to": pricing.ID}, 200, nil)
	if got := o.tasks(t, "mia", "/v1/workstreams/"+pricing.ID); len(got) != 1 || got[0].ID != hers.ID {
		t.Fatalf("Pricing page holds %+v", got)
	}
}
