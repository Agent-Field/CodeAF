package remote

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type appTasksFake struct {
	rows    []session.AppTaskView
	quick   []session.AppQuickTaskView
	intent  session.AppTaskIntent
	failure error
}

func (f *appTasksFake) AppTasks() ([]session.AppTaskView, error) { return f.rows, f.failure }
func (f *appTasksFake) MoveTurnToTask() error                    { return f.failure }
func (f *appTasksFake) AppTask(id string) (session.AppTaskView, bool, error) {
	return f.rows[0], id == f.rows[0].ID, f.failure
}
func (f *appTasksFake) AppTaskAction(intent session.AppTaskIntent) error {
	f.intent = intent
	return f.failure
}
func (f *appTasksFake) AppQuickTasks() ([]session.AppQuickTaskView, error) { return f.quick, f.failure }
func (f *appTasksFake) AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error) {
	f.intent = intent
	return nil, []string{"changed.txt"}, f.failure
}

func TestAppTaskWireCarriesHashesReceiptsAndRefusals(t *testing.T) {
	client, engine := newEngine(t)
	step := 9
	state := "your_call"
	version := uint64(3)
	row := session.AppTaskView{ID: "7", Title: "Edit", State: state, Notes: []session.AppTaskNote{{Text: "words", ReadAtStep: &step}}, Cap: 2, Version: version}
	quick := session.AppQuickTaskView{ID: "8", State: "done", Files: []session.AppQuickFile{{Path: "one.txt", BeforeHash: "before", AfterHash: "after"}}}
	engine.answers[MethodAppTasks] = []session.AppTaskView{row}
	engine.answers[MethodAppTask] = appTaskPageResult{row, true}
	engine.answers[MethodAppQuickTasks] = []session.AppQuickTaskView{quick}
	engine.answers[MethodAppQuickTaskAction] = appQuickActionResult{Changed: []string{"changed.txt"}}
	if got, err := client.Agent().AppTasks(); err != nil || !reflect.DeepEqual(got, []session.AppTaskView{row}) {
		t.Fatalf("tasks %+v %v", got, err)
	}
	if got, found, err := client.Agent().AppTask("7"); err != nil || !found || !reflect.DeepEqual(got, row) {
		t.Fatalf("page %+v %v %v", got, found, err)
	}
	if got, err := client.Agent().AppQuickTasks(); err != nil || !reflect.DeepEqual(got, []session.AppQuickTaskView{quick}) {
		t.Fatalf("quick %+v %v", got, err)
	}
	intent := session.AppTaskIntent{ID: "7", Action: "decide", Verb: "tell", Text: "words", State: &state, Version: &version}
	if err := client.Agent().AppTaskAction(intent); err != nil {
		t.Fatal(err)
	}
	undone, changed, err := client.Agent().AppQuickTaskAction(session.AppTaskIntent{ID: "8", Action: "undo"})
	if err != nil || len(undone) != 0 || !reflect.DeepEqual(changed, []string{"changed.txt"}) {
		t.Fatalf("undo %v %v %v", undone, changed, err)
	}
	var got session.AppTaskIntent
	if err := json.Unmarshal(engine.calls(MethodAppTaskAction)[0].Payload, &got); err != nil || !reflect.DeepEqual(got, intent) {
		t.Fatalf("intent %+v %v", got, err)
	}
	engine.fails[MethodAppTasks] = "engine: cannot read tasks"
	if _, err := client.Agent().AppTasks(); err == nil || !strings.Contains(err.Error(), "cannot read tasks") {
		t.Fatalf("read lost refusal %v", err)
	}
}

func TestAppTaskEngineDispatchAndDriverClassification(t *testing.T) {
	fake := &appTasksFake{rows: []session.AppTaskView{{ID: "7", State: "proposed", Cap: 2}}, quick: []session.AppQuickTaskView{{ID: "8", State: "done"}}}
	for _, method := range []string{MethodAppTasks, MethodAppTask, MethodAppTaskAction, MethodAppQuickTasks, MethodAppQuickTaskAction} {
		payload, _ := json.Marshal(session.AppTaskIntent{ID: "7", Action: "hold"})
		if method == MethodAppTask {
			payload, _ = json.Marshal(PlanTaskArgs{ID: "7"})
		}
		if _, err := dispatchAppTask(fake, Frame{Method: method, Payload: payload}); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		mutating := method == MethodAppTaskAction || method == MethodAppQuickTaskAction
		if mutating && classify(method) != classAct {
			t.Fatalf("%s can queue behind the turn it must release", method)
		}
		if mutating && watcherMay(method) {
			t.Fatalf("reader can mutate with %s", method)
		}
		if !mutating && (classify(method) != classGetter || !watcherMay(method)) {
			t.Fatalf("%s is not a read", method)
		}
	}
	fake.failure = errors.New("stale: task changed")
	if _, err := dispatchAppTask(fake, Frame{Method: MethodAppTasks}); err == nil {
		t.Fatal("dispatch lost refusal")
	}
	if _, err := dispatchAppTask(struct{}{}, Frame{Method: MethodAppTasks}); err == nil {
		t.Fatal("missing engine door became an empty list")
	}
}
