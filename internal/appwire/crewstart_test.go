package appwire

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// plainCrews is a Crewer that is not a starter: any call it did not expect to
// receive panics on the embedded nil.
type plainCrews struct {
	fakeEngine
	Crewer
	created int
}

func (p *plainCrews) CrewCreate(CrewCreate) (Crew, error) { p.created++; return Crew{ID: "plain"}, nil }

type startCrews struct {
	plainCrews
	got  CrewCreate
	root RootStartAsk
}

func (s *startCrews) CrewStart(ask CrewCreate) (CrewStartResult, error) {
	s.got = ask
	return CrewStartResult{Crew: Crew{ID: "c1"}, Start: CrewStarted{RequestID: ask.RequestID, Manager: "m1", Goal: CrewGoal{State: GoalStarting}}}, nil
}

func (s *startCrews) RootStart(ask RootStartAsk) (RootStartResult, error) {
	s.root = ask
	return RootStartResult{Crew: Crew{ID: "root"}, Manager: "g1", RootCreated: true, ManagerCreated: true}, nil
}

func wireCodeOf(t *testing.T, err error) string {
	t.Helper()
	var wire *Error
	if !errors.As(err, &wire) {
		t.Fatalf("want a wire error, got %v", err)
	}
	return wire.Code
}

func TestCrewsCreateStartIsOptInAndGatedBeforeTheEngine(t *testing.T) {
	create := methods["crews.create"]
	start := `{"name":"Docs","start_manager":true,"manager_starts_members":true,"request_id":"r1","goal":"go","client_id":"c","said":"go!"}`
	plain := &plainCrews{}
	if _, err := create.call(plain, json.RawMessage(start)); wireCodeOf(t, err) != CodeUnknownMethod || plain.created != 0 {
		t.Fatalf("a non-starter ran the create: %v created %d", err, plain.created)
	}
	// An old create is untouched, and start fields without the opt-in are refused.
	if _, err := create.call(plain, json.RawMessage(`{"name":"Old","agents":["a"],"goal":"kept"}`)); err != nil || plain.created != 1 {
		t.Fatalf("old create: %v %d", err, plain.created)
	}
	if _, err := create.call(plain, json.RawMessage(`{"name":"Old","agents":["a"],"request_id":"r"}`)); wireCodeOf(t, err) != CodeBadParams {
		t.Fatalf("stray start field: %v", err)
	}
	eng := &startCrews{}
	out, err := create.call(eng, json.RawMessage(start))
	if err != nil {
		t.Fatal(err)
	}
	res := out.(*crewResult)
	if res.Crew.ID != "c1" || res.Start == nil || res.Start.Manager != "m1" || eng.got.Said == nil || *eng.got.Said != "go!" || eng.got.ClientID != "c" {
		t.Fatalf("%+v %+v", res, eng.got)
	}
	raw, _ := json.Marshal(res)
	var shape map[string]json.RawMessage
	_ = json.Unmarshal(raw, &shape)
	if _, ok := shape["start"]; !ok {
		t.Fatalf("start missing from %s", raw)
	}
	old, _ := json.Marshal(&crewResult{Crew: Crew{ID: "x"}})
	if strings.Contains(string(old), `"start"`) {
		t.Fatalf("an old reply grew a start: %s", old)
	}
}

func TestCrewsRootStartIsOptionalAndAdvertised(t *testing.T) {
	root := methods["crews.root.start"]
	if _, err := root.call(&plainCrews{}, json.RawMessage(`{}`)); wireCodeOf(t, err) != CodeUnknownMethod {
		t.Fatalf("%v", err)
	}
	eng := &startCrews{}
	out, err := root.call(eng, json.RawMessage(`{"workspace":"/w","model":"m"}`))
	if got := out.(*RootStartResult); err != nil || got.Manager != "g1" || !got.RootCreated || eng.root.Workspace != "/w" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := root.call(eng, json.RawMessage(`{"model":"`+string(make([]byte, 300))+`"}`)); err == nil {
		t.Fatal("an over-long model was accepted")
	}
	if hello := withMessageReadsFeature(Hello{}, eng); !slices.Contains(hello.Features, FeatureCrewStart) {
		t.Fatalf("not advertised: %v", hello.Features)
	}
	if hello := withMessageReadsFeature(Hello{}, &plainCrews{}); slices.Contains(hello.Features, FeatureCrewStart) {
		t.Fatal("advertised by a non-starter")
	}
}
