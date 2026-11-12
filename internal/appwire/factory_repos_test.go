package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// repoEngine is a factory engine that is also a FactoryRepoAdmin and keeps what
// each call was handed.
type repoEngine struct {
	factoryEngine
	calls []FactoryRepoSetParams
	err   error
}

func (r *repoEngine) FactoryRepoSet(_ context.Context, p FactoryRepoSetParams) (FactoryRepoSetDone, error) {
	r.calls = append(r.calls, p)
	if r.err != nil {
		return FactoryRepoSetDone{}, r.err
	}
	return FactoryRepoSetDone{Factory: p.Factory, Repo: p.Repo, On: *p.On}, nil
}

// ABSENT WITHOUT THE INTERFACE, AND NOT IMPLIED BY factory.admin OR factory.links.
func TestFactoryRepoSetAbsentWithoutTheInterface(t *testing.T) {
	if len(factoryRepoMethods) != 1 || factoryRepoMethods["factory.repo_set"].call == nil {
		t.Fatalf("repo methods: %v", factoryRepoMethods)
	}
	raw := json.RawMessage(`{"factory":"f","repo":"a/b","on":true}`)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}, &linkEngine{}} {
		if _, err := methods["factory.repo_set"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryReposManageFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryReposManageFeature(Hello{}, &repoEngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.repos_manage" {
		t.Fatalf("a repo engine announced %v", got.Features)
	}
	if h := withFactoryLinksFeature(withFactoryAdminFeature(Hello{}, &repoEngine{}), &repoEngine{}); len(h.Features) != 0 {
		t.Fatalf("repo management implied another feature: %v", h.Features)
	}
	if FeatureFactoryReposManage == FeatureFactoryAdmin || FeatureFactoryReposManage == FeatureFactoryLinks {
		t.Fatal("the feature is not distinct")
	}
}

// PARAMS ARE EXACTLY {factory, repo, on}: a path, verb, body or folder is
// bad_params, and the engine is not asked.
func TestFactoryRepoSetParamsAreStrict(t *testing.T) {
	long := strings.Repeat("x", RepoNameMax+1)
	bad := []string{
		`{}`, `[]`, `null`, `{"factory":"f"}`, `{"factory":"f","repo":"a/b"}`, `{"factory":"f","on":true}`, `{"repo":"a/b","on":true}`,
		`{"factory":" ","repo":"a/b","on":true}`,
		`{"factory":"f","repo":"a/b","on":null}`, `{"factory":"f","repo":"a/b","on":"true"}`, `{"factory":"f","repo":"a/b","on":1}`,
		`{"factory":"f","repo":"a/b","on":true,"path":"/v1/repos"}`, `{"factory":"f","repo":"a/b","on":true,"verb":"DELETE"}`,
		`{"factory":"f","repo":"a/b","on":true,"rawpath":"/x"}`, `{"factory":"f","repo":"a/b","on":true,"body":{}}`,
		`{"factory":"f","repo":"a/b","on":true,"folder":"/home/x"}`, `{"factory":"f","repo":"a/b","on":true,"delete":true}`,
		`{"factory":"f","repo":"","on":true}`, `{"factory":"f","repo":"ab","on":true}`, `{"factory":"f","repo":"/b","on":true}`, `{"factory":"f","repo":"a/","on":true}`,
		`{"factory":"f","repo":"a/b/c","on":true}`, `{"factory":"f","repo":"a b/c","on":true}`, `{"factory":"f","repo":"a/b c","on":true}`,
		`{"factory":"f","repo":"../b","on":true}`, `{"factory":"f","repo":"a/..","on":true}`, `{"factory":"f","repo":"./b","on":true}`, `{"factory":"f","repo":"a/.","on":true}`,
		`{"factory":"f","repo":"a/%2e%2e","on":true}`, `{"factory":"f","repo":"a/b?x=1","on":true}`, `{"factory":"f","repo":"a/b#x","on":true}`,
		`{"factory":"f","repo":"a\\b/c","on":true}`, `{"factory":"f","repo":"a/b\n","on":true,"x":1}`, `{"factory":"f","repo":"a/é","on":true}`,
		`{"factory":"f","repo":"a/b‮","on":true}`, `{"factory":"f","repo":"a/b\u0000","on":true}`,
		`{"factory":"f","repo":"` + strings.Repeat("o", RepoOwnerMax+1) + `/b","on":true}`, `{"factory":"f","repo":"a/` + long + `","on":true}`,
		// Stop is only the yes to turning a repository off, and only a bool.
		`{"factory":"f","repo":"a/b","on":true,"stop":true}`, `{"factory":"f","repo":"a/b","on":false,"stop":"yes"}`,
	}
	eng := &repoEngine{}
	for _, raw := range bad {
		if _, err := methods["factory.repo_set"].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %v", eng.calls)
	}
}

func TestFactoryRepoSetGoodParamsArePassedOnceAndOffIsNotADefault(t *testing.T) {
	eng := &repoEngine{}
	out, err := methods["factory.repo_set"].call(eng, json.RawMessage(`{"factory":" f ","repo":" Acme/api-2.x_y ","on":false}`))
	if err != nil || len(eng.calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
	if p := eng.calls[0]; p.Factory != "f" || p.Repo != "Acme/api-2.x_y" || p.On == nil || *p.On {
		t.Fatalf("got %+v", p)
	}
	data, _ := json.Marshal(out)
	var keys map[string]any
	_ = json.Unmarshal(data, &keys)
	if len(keys) != 3 || keys["factory"] != "f" || keys["repo"] != "Acme/api-2.x_y" || keys["on"] != false {
		t.Fatalf("receipt = %s", data)
	}
	// Turning off with stop passes the person's yes on, once.
	if _, err := methods["factory.repo_set"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b","on":false,"stop":true}`)); err != nil || len(eng.calls) != 2 || !eng.calls[1].Stop {
		t.Fatalf("stop: %v after %d calls", err, len(eng.calls))
	}
	// The longest names are accepted.
	long := strings.Repeat("o", RepoOwnerMax) + "/" + strings.Repeat("n", RepoNameMax)
	if _, err := methods["factory.repo_set"].call(eng, json.RawMessage(`{"factory":"f","repo":"`+long+`","on":true}`)); err != nil {
		t.Fatal(err)
	}
}

// An engine's refusal is passed on whole, and the write is asked once.
func TestFactoryRepoSetPassesTheEnginesWordsAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeUncertain, CodeConflict, CodeForbidden, CodeSignedOut, CodeBusy, CodeUnreachable, CodeNotFound, CodeBadParams, CodeHasRunning} {
		eng := &repoEngine{err: Errorf(code, "words for %s", code)}
		_, err := methods["factory.repo_set"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b","on":true}`))
		if wireCode(t, err) != code || len(eng.calls) != 1 {
			t.Errorf("%s: %v after %d calls", code, err, len(eng.calls))
		}
	}
}

// THE RECEIPT HAS NO FIELD FOR ANYTHING BUT THE SWITCH, and the schema names
// the method, the capability and the feature.
func TestFactoryRepoSetSchemaIsTheSwitchAndNothingElse(t *testing.T) {
	if got := fieldNames(reflect.TypeFor[FactoryRepoSetDone]()); !reflect.DeepEqual(got, []string{"factory", "repo", "on"}) {
		t.Fatalf("receipt fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryRepoSetParams]()); !reflect.DeepEqual(got, []string{"factory", "repo", "on", "stop"}) {
		t.Fatalf("param fields: %v", got)
	}
	doc := Schema()["methods"].(map[string]any)
	entry, ok := doc["factory.repo_set"].(map[string]any)
	if !ok || entry["params"] == nil || entry["result"] == nil {
		t.Fatal("schema has no entry for factory.repo_set")
	}
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["repos_manage"] == nil {
		t.Fatal("capabilities do not carry repos_manage")
	}
}

func fieldNames(ty reflect.Type) []string {
	var out []string
	for i := 0; i < ty.NumField(); i++ {
		name, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}
