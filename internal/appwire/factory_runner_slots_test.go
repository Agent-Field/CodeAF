package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type runnerEngine struct {
	factoryEngine
	calls []FactoryRunnerSlotsParams
	err   error
}

func (r *runnerEngine) FactoryRunnerSlots(_ context.Context, p FactoryRunnerSlotsParams) (FactoryRunnerSlotsDone, error) {
	r.calls = append(r.calls, p)
	if r.err != nil {
		return FactoryRunnerSlotsDone{}, r.err
	}
	return FactoryRunnerSlotsDone{Factory: p.Factory, Runner: p.Runner, Slots: *p.Slots}, nil
}

func TestFactoryRunnerSlotsAbsentWithoutTheInterface(t *testing.T) {
	if len(factoryRunnerMethods) != 1 || factoryRunnerMethods["factory.runner_slots"].call == nil {
		t.Fatalf("runner methods: %v", factoryRunnerMethods)
	}
	raw := json.RawMessage(`{"factory":"f","runner":"mbp","slots":2}`)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}, &linkEngine{}, &repoEngine{}} {
		if _, err := methods["factory.runner_slots"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryRunnerManageFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryRunnerManageFeature(Hello{}, &runnerEngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.runner_manage" {
		t.Fatalf("a runner engine announced %v", got.Features)
	}
	// Nothing else is implied by it, and it implies nothing else.
	if h := withFactoryReposManageFeature(withFactoryLinksFeature(withFactoryAdminFeature(Hello{}, &runnerEngine{}), &runnerEngine{}), &runnerEngine{}); len(h.Features) != 0 {
		t.Fatalf("runner management implied another feature: %v", h.Features)
	}
	if got := withFactoryRunnerManageFeature(Hello{}, &repoEngine{}); len(got.Features) != 0 {
		t.Fatalf("repo management implied runner management: %v", got.Features)
	}
}

// PARAMS ARE EXACTLY {factory, runner, slots}: a route, verb, body, token or
// checkout list is bad_params, and the engine is not asked.
func TestFactoryRunnerSlotsParamsAreStrictAndTheRangeIsTheServers(t *testing.T) {
	long := strings.Repeat("x", RunnerIDMax+1)
	over := api.MaxRunnerSlots + 1
	bad := []string{
		`{}`, `[]`, `null`, `{"factory":"f"}`, `{"factory":"f","runner":"m"}`, `{"factory":"f","slots":2}`, `{"runner":"m","slots":2}`,
		`{"factory":" ","runner":"m","slots":2}`,
		`{"factory":"f","runner":"m","slots":null}`, `{"factory":"f","runner":"m","slots":"2"}`, `{"factory":"f","runner":"m","slots":2.5}`,
		`{"factory":"f","runner":"m","slots":2.0}`, `{"factory":"f","runner":"m","slots":true}`, `{"factory":"f","runner":"m","slots":[2]}`,
		`{"factory":"f","runner":"m","slots":0}`, `{"factory":"f","runner":"m","slots":-1}`,
		`{"factory":"f","runner":"m","slots":` + itoa(over) + `}`, `{"factory":"f","runner":"m","slots":99999999999999999999}`,
		`{"factory":"f","runner":"m","slots":2,"path":"/v1/runners/m"}`, `{"factory":"f","runner":"m","slots":2,"verb":"DELETE"}`,
		`{"factory":"f","runner":"m","slots":2,"token":"tok_x"}`, `{"factory":"f","runner":"m","slots":2,"checkouts":[]}`,
		`{"factory":"f","runner":"m","slots":2,"pause":true}`, `{"factory":"f","runner":"m","slots":2,"body":{}}`,
		`{"factory":"f","runner":"","slots":2}`, `{"factory":"f","runner":".","slots":2}`, `{"factory":"f","runner":"..","slots":2}`,
		`{"factory":"f","runner":"a/b","slots":2}`, `{"factory":"f","runner":"a\\b","slots":2}`, `{"factory":"f","runner":" m","slots":2}`,
		`{"factory":"f","runner":"m ","slots":2}`, `{"factory":"f","runner":"m\n","slots":2}`, `{"factory":"f","runner":"m\u0000","slots":2}`,
		`{"factory":"f","runner":"m‮","slots":2}`, `{"factory":"f","runner":"m�","slots":2}`, `{"factory":"f","runner":"` + long + `","slots":2}`,
	}
	eng := &runnerEngine{}
	for _, raw := range bad {
		if _, err := methods["factory.runner_slots"].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %v", eng.calls)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestFactoryRunnerSlotsGoodParamsArePassedOnceAndTheBoundsAreInclusive(t *testing.T) {
	eng := &runnerEngine{}
	out, err := methods["factory.runner_slots"].call(eng, json.RawMessage(`{"factory":" f ","runner":"Sam's Mac.local","slots":`+itoa(api.MaxRunnerSlots)+`}`))
	if err != nil || len(eng.calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
	if p := eng.calls[0]; p.Factory != "f" || p.Runner != "Sam's Mac.local" || p.Slots == nil || *p.Slots != api.MaxRunnerSlots {
		t.Fatalf("got %+v", p)
	}
	data, _ := json.Marshal(out)
	var keys map[string]any
	_ = json.Unmarshal(data, &keys)
	if len(keys) != 3 || keys["factory"] != "f" || keys["runner"] != "Sam's Mac.local" || keys["slots"] != float64(api.MaxRunnerSlots) {
		t.Fatalf("receipt = %s", data)
	}
	for _, raw := range []string{
		`{"factory":"f","runner":"m","slots":1}`,
		`{"factory":"f","runner":"` + strings.Repeat("é", RunnerIDMax/2) + `","slots":2}`,
	} {
		if _, err := methods["factory.runner_slots"].call(eng, json.RawMessage(raw)); err != nil {
			t.Errorf("refused %s: %v", raw, err)
		}
	}
}

// An engine's refusal is passed on whole, and the write is asked once.
func TestFactoryRunnerSlotsPassesTheEnginesWordsAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeUncertain, CodeConflict, CodeForbidden, CodeSignedOut, CodeBusy, CodeUnreachable, CodeNotFound, CodeBadParams} {
		eng := &runnerEngine{err: Errorf(code, "words for %s", code)}
		_, err := methods["factory.runner_slots"].call(eng, json.RawMessage(`{"factory":"f","runner":"m","slots":2}`))
		if wireCode(t, err) != code || len(eng.calls) != 1 {
			t.Errorf("%s: %v after %d calls", code, err, len(eng.calls))
		}
	}
}

func TestFactoryRunnerSlotsSchemaIsTheCapacityAndNothingElse(t *testing.T) {
	if got := fieldNames(reflect.TypeFor[FactoryRunnerSlotsDone]()); !reflect.DeepEqual(got, []string{"factory", "runner", "slots"}) {
		t.Fatalf("receipt fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryRunnerSlotsParams]()); !reflect.DeepEqual(got, []string{"factory", "runner", "slots"}) {
		t.Fatalf("param fields: %v", got)
	}
	doc := Schema()["methods"].(map[string]any)
	entry, ok := doc["factory.runner_slots"].(map[string]any)
	if !ok || entry["params"] == nil || entry["result"] == nil {
		t.Fatal("schema has no entry for factory.runner_slots")
	}
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["runner_manage"] == nil {
		t.Fatal("capabilities do not carry runner_manage")
	}
}
