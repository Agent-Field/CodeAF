package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// actorEngine is a factory engine that is also a FactoryActor and keeps what
// each action was handed.
type actorEngine struct {
	factoryEngine
	acts []string
	got  []any
}

func (a *actorEngine) did(name string, p any) (FactoryJobActed, error) {
	a.acts = append(a.acts, name)
	a.got = append(a.got, p)
	return FactoryJobActed{Action: name, Job: 1}, nil
}

func (a *actorEngine) FactoryJobStart(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("start", p)
}
func (a *actorEngine) FactoryJobStop(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("stop", p)
}
func (a *actorEngine) FactoryJobDismiss(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("dismiss", p)
}
func (a *actorEngine) FactoryJobDrop(_ context.Context, p FactoryJobDropParams) (FactoryJobActed, error) {
	return a.did("drop", p)
}
func (a *actorEngine) FactoryJobUnassign(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("unassign", p)
}
func (a *actorEngine) FactoryJobAssignToMe(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("assign_to_me", p)
}
func (a *actorEngine) FactoryJobChangeAssignee(_ context.Context, p FactoryJobAssigneeParams) (FactoryJobActed, error) {
	return a.did("change_assignee", p)
}
func (a *actorEngine) FactoryJobSetPriority(_ context.Context, p FactoryJobPriorityParams) (FactoryJobActed, error) {
	return a.did("set_priority", p)
}
func (a *actorEngine) FactoryJobSetBudget(_ context.Context, p FactoryJobBudgetParams) (FactoryJobActed, error) {
	return a.did("set_budget", p)
}
func (a *actorEngine) FactoryJobApprove(_ context.Context, p FactoryJobApproveParams) (FactoryJobActed, error) {
	return a.did("approve", p)
}
func (a *actorEngine) FactoryJobUndo(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("undo", p)
}
func (a *actorEngine) FactoryJobRequestChanges(_ context.Context, p FactoryJobChangesParams) (FactoryJobActed, error) {
	return a.did("request_changes", p)
}
func (a *actorEngine) FactoryJobRetryPublish(_ context.Context, p FactoryJobActionParams) (FactoryJobActed, error) {
	return a.did("retry_publish", p)
}
func (a *actorEngine) FactoryJobMerge(_ context.Context, p FactoryJobMergeParams) (FactoryJobActed, error) {
	return a.did("merge", p)
}
func (a *actorEngine) FactoryJobSetModel(_ context.Context, p FactoryJobSetModelParams) (FactoryJobActed, error) {
	return a.did("set_model", p)
}
func (a *actorEngine) FactoryJobCreate(_ context.Context, p FactoryJobCreateParams) (FactoryJobActed, error) {
	return a.did("create", p)
}
func (a *actorEngine) FactoryJobStepStop(_ context.Context, p FactoryJobStepParams) (FactoryJobActed, error) {
	return a.did("step_stop", p)
}
func (a *actorEngine) FactoryJobStepRerun(_ context.Context, p FactoryJobStepParams) (FactoryJobActed, error) {
	return a.did("step_rerun", p)
}

const guard = `"expect":{"version":3}`

// A VALID REQUEST FOR EACH ACTION.
var factoryActionGood = map[string]string{
	"factory.job_start":           `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_stop":            `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_dismiss":         `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_drop":            `{"factory":"f","job":1,"words":"dup",` + guard + `}`,
	"factory.job_unassign":        `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_assign_to_me":    `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_change_assignee": `{"factory":"f","job":1,"assignee":"@bob",` + guard + `}`,
	"factory.job_set_priority":    `{"factory":"f","job":1,"priority":"high",` + guard + `}`,
	"factory.job_set_budget":      `{"factory":"f","job":1,"budget":10,` + guard + `}`,
	"factory.job_approve":         `{"factory":"f","job":1,"commit":"abc1234",` + guard + `}`,
	"factory.job_undo":            `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_request_changes": `{"factory":"f","job":1,"words":"prove it",` + guard + `}`,
	"factory.job_retry_publish":   `{"factory":"f","job":1,` + guard + `}`,
	"factory.job_create":          `{"factory":"f","repo":"o/r","title":"Fix it"}`,
	"factory.job_step_stop":       `{"factory":"f","job":1,"step":"build",` + guard + `}`,
	"factory.job_step_rerun":      `{"factory":"f","job":1,"step":"build",` + guard + `}`,
	"factory.job_merge":           `{"factory":"f","job":1,"commit":"abc1234",` + guard + `}`,
	"factory.job_set_model":       `{"factory":"f","job":1,"model":"deepseek/deepseek-v4-flash",` + guard + `}`,
}

func TestEveryFactoryActionHasAValidRequestHere(t *testing.T) {
	for name := range factoryActionMethods {
		if _, ok := factoryActionGood[name]; !ok {
			t.Errorf("%s has no request in factoryActionGood", name)
		}
	}
	for name := range factoryActionGood {
		if _, ok := methods[name]; !ok {
			t.Errorf("%s is in factoryActionGood and not registered", name)
		}
	}
}

// AN ENGINE WITHOUT FactoryActor ANSWERS unknown_method, EVEN ONE THAT SERVES
// factory.*, and does not announce the feature.
func TestFactoryActionsAbsentWithoutActor(t *testing.T) {
	for name, params := range factoryActionGood {
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
			_, err := methods[name].call(e, json.RawMessage(params))
			if wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
	}
	if got := withFactoryActorFeature(Hello{}, &factoryEngine{}); len(got.Features) != 0 {
		t.Fatalf("a reader announced %v", got.Features)
	}
	got := withFactoryActorFeature(Hello{}, &actorEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryJobActions {
		t.Fatalf("an actor announced %v", got.Features)
	}
	// factory.remote is still announced by the reader half alone.
	if got := withFactoryFeature(Hello{}, &actorEngine{}); got.Features[0] != FeatureFactory {
		t.Fatalf("features %v", got.Features)
	}
}

func TestFactoryActionsRefuseBadRequestsBeforeTheEngine(t *testing.T) {
	eng := &actorEngine{}
	bad := []struct{ name, params string }{
		// No version read: the write would run on a job the person never saw.
		{"factory.job_stop", `{"factory":"f","job":1}`},
		{"factory.job_stop", `{"factory":"f","job":1,"expect":{}}`},
		{"factory.job_stop", `{"factory":"f","job":1,"expect":{"state":"running"}}`},
		{"factory.job_stop", `{"factory":"f","job":1,"expect":{"version":0}}`},
		{"factory.job_stop", `{"factory":"","job":1,` + guard + `}`},
		{"factory.job_stop", `{"factory":"f","job":0,` + guard + `}`},
		{"factory.job_stop", `{"factory":"f","job":"1",` + guard + `}`},
		{"factory.job_change_assignee", `{"factory":"f","job":1,"assignee":"",` + guard + `}`},
		{"factory.job_change_assignee", `{"factory":"f","job":1,"assignee":"bob/../x",` + guard + `}`},
		{"factory.job_change_assignee", `{"factory":"f","job":1,"assignee":"-bob",` + guard + `}`},
		{"factory.job_set_priority", `{"factory":"f","job":1,"priority":"asap",` + guard + `}`},
		{"factory.job_set_budget", `{"factory":"f","job":1,"budget":7,` + guard + `}`},
		{"factory.job_set_budget", `{"factory":"f","job":1,"budget":0,` + guard + `}`},
		{"factory.job_approve", `{"factory":"f","job":1,"commit":"HEAD~1",` + guard + `}`},
		{"factory.job_approve", `{"factory":"f","job":1,"commit":"abc",` + guard + `}`},
		{"factory.job_request_changes", `{"factory":"f","job":1,"words":"  ",` + guard + `}`},
		{"factory.job_step_stop", `{"factory":"f","job":1,"step":"",` + guard + `}`},
		{"factory.job_step_rerun", `{"factory":"f","job":1,"step":"a/../b",` + guard + `}`},
		{"factory.job_step_rerun", `{"factory":"f","job":1,"step":"a?b",` + guard + `}`},
		// A stop takes no words; only a rerun says what to do differently.
		{"factory.job_step_stop", `{"factory":"f","job":1,"step":"build","words":"why",` + guard + `}`},
		{"factory.job_set_model", `{"factory":"f","job":1,"model":"two words",` + guard + `}`},
		{"factory.job_create", `{"factory":"f","repo":"norepo","title":"x"}`},
		{"factory.job_create", `{"factory":"f","repo":"o/r","title":" "}`},
		{"factory.job_create", `{"factory":"f","repo":"o/r","title":"x","start":"queued"}`},
		{"factory.job_create", `{"factory":"f","repo":"o/r","title":"x","start":"now"}`},
		{"factory.job_create", `{"factory":"f","repo":"o/r","title":"x","assignee":"a b"}`},
		// An unknown key is not ignored: a client that thinks it can send an
		// issue flag, a path or a list of jobs is told it cannot.
		{"factory.job_create", `{"factory":"f","repo":"o/r","title":"x","issue":true}`},
		{"factory.job_approve", `{"factory":"f","job":1,"jobs":[1,2],` + guard + `}`},
		{"factory.job_stop", `{"factory":"f","job":1,"path":"/v1/jobs/2/stop",` + guard + `}`},
	}
	for _, c := range bad {
		_, err := methods[c.name].call(eng, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	if len(eng.acts) != 0 {
		t.Fatalf("a refused request reached the engine: %v", eng.acts)
	}
	for name, params := range factoryActionGood {
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Errorf("%s %s: %v", name, params, err)
		}
	}
	if len(eng.acts) != len(factoryActionGood) {
		t.Fatalf("engine saw %v", eng.acts)
	}
}

func TestFactoryActionsPassTrimmedTypedArguments(t *testing.T) {
	eng := &actorEngine{}
	call := func(name, params string) {
		t.Helper()
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	call("factory.job_change_assignee", `{"factory":" f ","job":4,"assignee":" @Bob ","expect":{"version":9,"state":"running"}}`)
	call("factory.job_approve", `{"factory":"f","job":4,"commit":" ABC1234 ","words":" ok ","edited":true,"expect":{"version":9}}`)
	call("factory.job_create", `{"factory":"f","repo":" o/r ","title":" Fix ","assignee":" @alice "}`)
	call("factory.job_create", `{"factory":"f","repo":"o/r","title":"Fix","assignee":""}`)
	a := eng.got[0].(FactoryJobAssigneeParams)
	if a.Factory != "f" || a.Assignee != "Bob" || *a.Expect.Version != 9 || a.Expect.State != "running" {
		t.Fatalf("assignee params %+v", a)
	}
	p := eng.got[1].(FactoryJobApproveParams)
	if p.Commit != "abc1234" || p.Words != "ok" || !p.Edited {
		t.Fatalf("approve params %+v", p)
	}
	c := eng.got[2].(FactoryJobCreateParams)
	if c.Repo != "o/r" || c.Title != "Fix" || c.Assignee == nil || *c.Assignee != "alice" || c.Start != "" {
		t.Fatalf("create params %+v", c)
	}
	if n := eng.got[3].(FactoryJobCreateParams); n.Assignee == nil || *n.Assignee != "" {
		t.Fatalf("an empty assignee must stay set: %+v", n)
	}
}

// NO UNTYPED DOOR, NO BULK: no param type has a path, a method, a body, an
// untyped value, or a list of jobs, however deep.
func TestFactoryActionWireHasNoUntypedDoorOrBulk(t *testing.T) {
	banned := map[string]bool{"path": true, "url": true, "method": true, "headers": true, "body": true, "raw": true, "query": true, "jobs": true, "ids": true, "verb": true, "route": true}
	for name, m := range factoryActionMethods {
		var walk func(ty reflect.Type, seen map[reflect.Type]bool, where string)
		walk = func(ty reflect.Type, seen map[reflect.Type]bool, where string) {
			if seen[ty] {
				return
			}
			seen[ty] = true
			switch {
			case ty == reflect.TypeFor[json.RawMessage]() || ty.Kind() == reflect.Interface:
				t.Errorf("%s %s reaches an untyped value (%s)", name, where, ty)
			case ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Array:
				walk(ty.Elem(), seen, where)
			case ty.Kind() == reflect.Slice:
				t.Errorf("%s %s carries a list (%s): an action acts on one job", name, where, ty)
			case ty.Kind() == reflect.Map:
				t.Errorf("%s %s carries a map (%s)", name, where, ty)
			case ty.Kind() == reflect.Struct && ty != reflect.TypeFor[time.Time]():
				for i := 0; i < ty.NumField(); i++ {
					f := ty.Field(i)
					tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
					if banned[tag] {
						t.Errorf("%s %s: field %s.%s is %q", name, where, ty.Name(), f.Name, tag)
					}
					walk(f.Type, seen, where)
				}
			}
		}
		walk(m.params, map[reflect.Type]bool{}, "params")
	}
}

// NO CREDENTIAL FIELD in anything a job action answers or refuses with.
func TestFactoryActionResultsHaveNoSecretField(t *testing.T) {
	secret := []string{"token", "device_code", "cookie", "secret", "password", "bearer", "authorization", "grant", "session", "invite", "link"}
	seen := map[reflect.Type]bool{}
	var walk func(where string, ty reflect.Type)
	walk = func(where string, ty reflect.Type) {
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice || ty.Kind() == reflect.Map || ty.Kind() == reflect.Array {
			ty = ty.Elem()
		}
		if ty.Kind() != reflect.Struct || seen[ty] || ty == reflect.TypeFor[time.Time]() {
			return
		}
		seen[ty] = true
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			for _, s := range secret {
				if strings.Contains(strings.ToLower(name), s) {
					t.Errorf("%s: %s.%s (%q) looks like a credential", where, ty.Name(), f.Name, name)
				}
			}
			walk(where, f.Type)
		}
	}
	for name, m := range factoryActionMethods {
		walk(name, m.result)
	}
	walk("refusal", reflect.TypeFor[FactoryRefusal]())
}

func TestFactoryActionSchemaIsAccurate(t *testing.T) {
	doc := Schema()
	ms := doc["methods"].(map[string]any)
	for name := range factoryActionMethods {
		entry, ok := ms[name].(map[string]any)
		if !ok || entry["params"] == nil || entry["result"] == nil {
			t.Errorf("schema has no entry for %s", name)
		}
	}
	raw, _ := SchemaJSON()
	var out struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	var priority, start []string
	for key, def := range out.Defs {
		if strings.Contains(key, "FactoryJobPriorityParams") {
			priority = def.Properties["priority"].Enum
		}
		if strings.Contains(key, "FactoryJobCreateParams") {
			start = def.Properties["start"].Enum
		}
	}
	if !reflect.DeepEqual(priority, []string{"urgent", "high", "medium", "low"}) || !reflect.DeepEqual(start, []string{"backlog", "now"}) {
		t.Fatalf("enums: priority %v start %v", priority, start)
	}
	// FactoryCapabilities.writes is NOT claimed by this slice.
	if _, ok := reflect.TypeFor[FactoryCapabilities]().FieldByName("Writes"); !ok {
		t.Fatal("capabilities lost writes")
	}
}
