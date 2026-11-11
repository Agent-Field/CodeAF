package remote

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
)

// programPagerFake is an engine whose programs' older actions can be paged.
type programPagerFake struct {
	*appTasksFake
	asked AppProgramActionsArgs
	page  session.AppProgramActionsPage
}

func (f *programPagerFake) AppProgramActions(id, before string) (session.AppProgramActionsPage, bool, error) {
	f.asked = AppProgramActionsArgs{ID: id, Before: before}
	return f.page, id == "7", nil
}

// AN OLDER PAGE OF A PROGRAM'S ACTIONS CROSSES --host AS THE READ IT IS: its
// read state, its actions, its cursor, and an earlier run's cursor as the
// field that says so — and an engine that cannot page says it cannot, never
// an empty page.
func TestAProgramsOlderActionsCrossTheWire(t *testing.T) {
	client, engine := newEngine(t)
	page := session.AppProgramActionsPage{Read: session.AppProgramReadComplete, Actions: []delegate.Shown{{Text: "read a.go"}}, EarlierActions: 3, Before: "c.3"}
	engine.answers[MethodAppProgramActions] = appProgramActionsResult{page, true}
	got, found, err := client.Agent().AppProgramActions("7", "c.4")
	if err != nil || !found || !reflect.DeepEqual(got, page) {
		t.Fatalf("page %+v %v %v", got, found, err)
	}
	var args AppProgramActionsArgs
	if err := json.Unmarshal(engine.calls(MethodAppProgramActions)[0].Payload, &args); err != nil || args != (AppProgramActionsArgs{ID: "7", Before: "c.4"}) {
		t.Fatalf("args %+v %v", args, err)
	}
	engine.answers[MethodAppProgramActions] = appProgramActionsResult{session.AppProgramActionsPage{Gone: true}, true}
	if got, _, _ := client.Agent().AppProgramActions("7", "c.4"); !got.Gone {
		t.Fatalf("an earlier run's cursor lost its fact: %+v", got)
	}

	fake := &programPagerFake{appTasksFake: &appTasksFake{rows: []session.AppTaskView{{ID: "7"}}}, page: page}
	payload, _ := json.Marshal(AppProgramActionsArgs{ID: "7", Before: "c.4"})
	raw, err := dispatchAppTask(fake, Frame{Method: MethodAppProgramActions, Payload: payload})
	var result appProgramActionsResult
	if err != nil || json.Unmarshal(raw, &result) != nil || !result.Found || !reflect.DeepEqual(result.Page, page) || fake.asked.Before != "c.4" {
		t.Fatalf("dispatch %s %v", raw, err)
	}
	if classify(MethodAppProgramActions) != classGetter || !watcherMay(MethodAppProgramActions) {
		t.Fatal("paging a program's actions is not a read")
	}
	if _, err := dispatchAppTask(&appTasksFake{rows: []session.AppTaskView{{ID: "7"}}}, Frame{Method: MethodAppProgramActions, Payload: payload}); err == nil {
		t.Fatal("an engine that cannot page answered a page")
	}
}
