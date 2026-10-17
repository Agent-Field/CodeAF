package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// factoryEngine is an engine that serves the factory wire and records what
// the routes asked of it.
type factoryEngine struct {
	fakeEngine
	calls []string
}

func (f *factoryEngine) note(s string) { f.calls = append(f.calls, s) }

func (f *factoryEngine) FactoryCapabilities(context.Context) (FactoryCapabilities, error) {
	return FactoryCapabilities{Remote: true}, nil
}
func (f *factoryEngine) FactoryList(context.Context) (FactoryList, error) {
	return FactoryList{}, nil
}
func (f *factoryEngine) FactoryAttach(_ context.Context, url, token string) (FactoryConnection, error) {
	f.note("attach " + url + " " + token)
	return FactoryConnection{ID: "factory_1", URL: url, State: FactoryOK}, nil
}
func (f *factoryEngine) FactoryDetach(_ context.Context, id string) error {
	f.note("detach " + id)
	return nil
}
func (f *factoryEngine) FactoryMe(_ context.Context, id string) (FactoryMe, error) {
	f.note("me " + id)
	return FactoryMe{Profile: api.Profile{Scopes: []string{}}}, nil
}
func (f *factoryEngine) FactoryQueue(_ context.Context, id string) (api.Queue, error) {
	return api.Queue{}, nil
}
func (f *factoryEngine) FactoryJob(_ context.Context, id string, job int) (api.Job, error) {
	f.note("job " + id)
	return api.Job{}, nil
}
func (f *factoryEngine) FactoryRepos(context.Context, string) (FactoryRepos, error) {
	return FactoryRepos{}, nil
}
func (f *factoryEngine) FactoryPipeline(_ context.Context, id, repo string) (FactoryPipeline, error) {
	f.note("pipeline " + repo)
	return FactoryPipeline{}, nil
}
func (f *factoryEngine) FactoryRunners(context.Context, string) (api.Runners, error) {
	return api.Runners{}, nil
}
func (f *factoryEngine) FactoryPeople(context.Context, string) (api.People, error) {
	return api.People{}, nil
}
func (f *factoryEngine) FactoryActivity(_ context.Context, _ string, since time.Time, limit int) (FactoryActivity, error) {
	f.note("activity " + string(rune('0'+limit/1000)) + "|" + since.Format(time.RFC3339))
	return FactoryActivity{}, nil
}
func (f *factoryEngine) FactorySetup(context.Context, string) (api.FactorySetup, error) {
	return api.FactorySetup{}, nil
}
func (f *factoryEngine) FactoryAccessCheck(_ context.Context, caller, link string) (FactoryAccess, error) {
	f.note("check " + caller)
	return FactoryAccess{State: "ready"}, nil
}
func (f *factoryEngine) FactoryAccessSignIn(context.Context, string, string) (FactoryAccess, error) {
	return FactoryAccess{State: "waiting"}, nil
}
func (f *factoryEngine) FactoryAccessCancel(context.Context, string, string) (FactoryAccess, error) {
	return FactoryAccess{State: "ready"}, nil
}
func (f *factoryEngine) FactorySignInStart(_ context.Context, caller, id string) (FactorySignIn, error) {
	f.note("start " + caller)
	return FactorySignIn{Flow: "f1"}, nil
}
func (f *factoryEngine) FactorySignInPoll(_ context.Context, caller, flow string) (FactorySignInState, error) {
	return FactorySignInState{State: SignInPending}, nil
}
func (f *factoryEngine) FactorySignInCancel(context.Context, string, string) error { return nil }

// A VALID REQUEST FOR EACH METHOD, so the tests below can walk the table.
var factoryGood = map[string]string{
	"factory.capabilities":   `{}`,
	"factory.list":           `{}`,
	"factory.attach":         `{"url":"https://f.example"}`,
	"factory.detach":         `{"factory":"f"}`,
	"factory.me":             `{"factory":"f"}`,
	"factory.queue":          `{"factory":"f"}`,
	"factory.job":            `{"factory":"f","job":1}`,
	"factory.repos":          `{"factory":"f"}`,
	"factory.pipeline":       `{"factory":"f","repo":"o/r"}`,
	"factory.runners":        `{"factory":"f"}`,
	"factory.people":         `{"factory":"f"}`,
	"factory.activity":       `{"factory":"f"}`,
	"factory.setup":          `{"factory":"f"}`,
	"factory.access_check":   `{"link":"codeaf://factory/join?url=x","caller":"w1"}`,
	"factory.access_sign_in": `{"link":"codeaf://factory/join?url=x","caller":"w1"}`,
	"factory.access_cancel":  `{"link":"codeaf://factory/join?url=x","caller":"w1"}`,
	"factory.sign_in_start":  `{"factory":"f","caller":"w1"}`,
	"factory.sign_in_poll":   `{"flow":"f1","caller":"w1"}`,
	"factory.sign_in_cancel": `{"flow":"f1","caller":"w1"}`,
}

func TestEveryFactoryMethodHasAValidRequestHere(t *testing.T) {
	for name := range factoryMethods {
		if _, ok := factoryGood[name]; !ok {
			t.Errorf("%s has no request in factoryGood", name)
		}
	}
	for name := range factoryGood {
		if _, ok := methods[name]; !ok {
			t.Errorf("%s is in factoryGood and not registered", name)
		}
	}
}

// AN ENGINE THAT IS NOT A FACTORY REFUSES EVERY factory.* AS unknown_method,
// and does not announce the feature.
func TestFactoryAbsentWithoutCapability(t *testing.T) {
	for name, params := range factoryGood {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(params))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%s on a plain engine: %v", name, err)
		}
	}
	if got := withFactoryFeature(Hello{}, &fakeEngine{}); len(got.Features) != 0 {
		t.Fatalf("a plain engine announced %v", got.Features)
	}
	if got := withFactoryFeature(Hello{}, &factoryEngine{}); len(got.Features) != 1 || got.Features[0] != FeatureFactory {
		t.Fatalf("a factory engine announced %v", got.Features)
	}
}

func TestFactoryRejectsMissingAndMalformedParams(t *testing.T) {
	eng := &factoryEngine{}
	for _, c := range []struct{ name, params string }{
		{"factory.attach", `{}`}, {"factory.attach", `{"url":"  "}`},
		{"factory.detach", `{}`}, {"factory.me", `{"factory":""}`},
		{"factory.queue", `{}`}, {"factory.job", `{"factory":"f"}`}, {"factory.job", `{"factory":"f","job":0}`},
		{"factory.job", `{"factory":"f","job":"1"}`},
		{"factory.pipeline", `{"factory":"f","repo":"norepo"}`}, {"factory.pipeline", `{"factory":"f","repo":"a/b/c"}`},
		{"factory.pipeline", `{"factory":"f","repo":"../x"}`}, {"factory.pipeline", `{"factory":"f","repo":"a/.."}`},
		{"factory.pipeline", `{"factory":"f","repo":"a/b?x=1"}`},
		{"factory.activity", `{"factory":"f","limit":-1}`}, {"factory.activity", `{"factory":"f","limit":1001}`},
		{"factory.activity", `{"factory":"f","since":"yesterday"}`},
		{"factory.access_check", `{"link":"x"}`}, {"factory.access_check", `{"caller":"w1"}`},
		{"factory.access_sign_in", `{"link":"x","caller":"  "}`},
		{"factory.sign_in_start", `{"factory":"f"}`}, {"factory.sign_in_start", `{"caller":"w1"}`},
		{"factory.sign_in_poll", `{"flow":"f1"}`}, {"factory.sign_in_poll", `{"caller":"w1"}`},
		{"factory.sign_in_cancel", `{"caller":"w1"}`},
	} {
		_, err := methods[c.name].call(eng, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a refused request reached the engine: %v", eng.calls)
	}
	for name, params := range factoryGood {
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Errorf("%s %s: %v", name, params, err)
		}
	}
}

func TestFactoryRoutesPassTrimmedTypedArguments(t *testing.T) {
	eng := &factoryEngine{}
	call := func(name, params string) {
		t.Helper()
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	call("factory.attach", `{"url":" https://f.example ","token":" tok "}`)
	call("factory.pipeline", `{"factory":"f","repo":" o/r "}`)
	call("factory.activity", `{"factory":"f","since":"2026-10-01T00:00:00Z"}`)
	call("factory.sign_in_start", `{"factory":"f","caller":" w1 "}`)
	want := []string{"attach https://f.example tok", "pipeline o/r", "activity 0|2026-10-01T00:00:00Z", "start w1"}
	if !reflect.DeepEqual(eng.calls, want) {
		t.Fatalf("calls = %q, want %q", eng.calls, want)
	}
}

// THERE IS NO ESCAPE HATCH: no factory method takes a path, a method, a header
// or an untyped body, and none answers one. Every type a factory method names,
// however deep, is a struct, a scalar or a list of them.
func TestFactoryWireHasNoUntypedDoor(t *testing.T) {
	banned := map[string]bool{"path": true, "url_path": true, "method": true, "headers": true, "header": true, "body": true, "raw": true, "request": true, "query": true}
	var walk func(t *testing.T, where string, ty reflect.Type, seen map[reflect.Type]bool)
	walk = func(t *testing.T, where string, ty reflect.Type, seen map[reflect.Type]bool) {
		if seen[ty] {
			return
		}
		seen[ty] = true
		switch {
		case ty == reflect.TypeFor[json.RawMessage]() || ty.Kind() == reflect.Interface:
			t.Errorf("%s reaches an untyped value (%s)", where, ty)
		case ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice || ty.Kind() == reflect.Array:
			walk(t, where, ty.Elem(), seen)
		case ty.Kind() == reflect.Map:
			if ty.Elem().Kind() == reflect.Interface {
				t.Errorf("%s reaches map[..]any (%s)", where, ty)
			}
			walk(t, where, ty.Elem(), seen)
		case ty.Kind() == reflect.Struct && ty != reflect.TypeFor[time.Time]():
			for i := 0; i < ty.NumField(); i++ {
				f := ty.Field(i)
				if !f.IsExported() {
					continue
				}
				// Only this package's own request and answer types are checked
				// by name; the factory's structs are its own contract.
				if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); banned[name] && ty.PkgPath() == reflect.TypeFor[Empty]().PkgPath() {
					t.Errorf("%s: field %s.%s is %q", where, ty.Name(), f.Name, name)
				}
				walk(t, where, f.Type, seen)
			}
		}
	}
	for name, m := range factoryMethods {
		if !strings.HasPrefix(name, "factory.") {
			t.Errorf("%s is not under factory.", name)
		}
		walk(t, name+" params", m.params, map[reflect.Type]bool{})
		walk(t, name+" result", m.result, map[reflect.Type]bool{})
	}
}

// THE SCHEMA CARRIES NO CREDENTIAL: no type a factory method answers has a
// field for a token, a grant, a device code, a cookie or a session secret.
func TestFactoryResultsHaveNoSecretField(t *testing.T) {
	secret := []string{"token", "device_code", "cookie", "secret", "password", "bearer", "authorization", "grant"}
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
	for name, m := range factoryMethods {
		walk(name, m.result)
	}
}

func TestFactorySchemaNamesEveryMethod(t *testing.T) {
	doc := Schema()["methods"].(map[string]any)
	for name := range factoryMethods {
		entry, ok := doc[name].(map[string]any)
		if !ok || entry["params"] == nil || entry["result"] == nil {
			t.Errorf("schema has no entry for %s", name)
		}
	}
	// The access answer is the one that was made secret-free on purpose.
	raw, _ := SchemaJSON()
	var out struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FactoryAccess", "FactorySignIn", "FactorySignInState", "FactoryConnection"} {
		def, ok := out.Defs[name]
		if !ok {
			t.Errorf("no definition %s", name)
			continue
		}
		for key := range def.Properties {
			if strings.Contains(key, "token") || key == "device_code" || key == "link" {
				t.Errorf("%s has property %q", name, key)
			}
		}
	}
	if got := out.Defs["FactoryAccess"].Properties["state"]; got == nil {
		t.Fatal("FactoryAccess has no state")
	}
}

func TestFactoryAccessHasNoLinkOrGrantFields(t *testing.T) {
	ty := reflect.TypeFor[FactoryAccess]()
	for i := 0; i < ty.NumField(); i++ {
		tag, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
		if tag == "link" || tag == "access_token" {
			t.Errorf("FactoryAccess has %q", tag)
		}
	}
}

func TestFactorySetupSchemaCarriesServerStates(t *testing.T) {
	defs := Schema()["$defs"].(map[string]any)
	setup := defs["FactorySetup"].(map[string]any)
	state := setup["properties"].(map[string]any)["state"].(map[string]any)
	want := []string{"github", "repos", "reading", "checkouts", "done"}
	if !reflect.DeepEqual(state["enum"], want) {
		t.Fatalf("setup states in schema: got %v, want %v", state["enum"], want)
	}
}

// A SCRIPT OR OPERATOR KEY HAS A HANDLE AND NO SESSION: it must not be
// reported as a person signed in through GitHub.
func TestFactoryMeSignedInComesOnlyFromTheSession(t *testing.T) {
	key := NewFactoryMe(api.Me{Profile: api.Profile{Handle: "ops-key"}, Operator: true, Access: "host"})
	if key.GitHubSignedIn || key.Handle != "ops-key" || !key.Operator {
		t.Fatalf("an access key read as signed in: %+v", key)
	}
	person := NewFactoryMe(api.Me{Profile: api.Profile{Handle: "alice", Scopes: []string{"read:user"}}, SessionID: "session-secret-1", GitHubSignedIn: true})
	// An older server omits the field: the same body decodes to false.
	var old api.Me
	if err := json.Unmarshal([]byte(`{"handle":"alice","name":"Alice","avatar_url":"u","operator":true,"session_id":"session_x"}`), &old); err != nil || NewFactoryMe(old).GitHubSignedIn {
		t.Fatalf("an omitted attestation was promoted: %+v %v", old, err)
	}
	if !person.GitHubSignedIn || len(person.Scopes) != 1 {
		t.Fatalf("a verified session read as not signed in: %+v", person)
	}
}

// THE REPLY CARRIES NONE OF THE MACHINE, RUNNER OR SESSION FIELDS, AND SCOPES
// IS ALWAYS AN ARRAY.
func TestFactoryMeReplyHasNoPrivateFields(t *testing.T) {
	me := api.Me{
		Factory: api.Factory{Name: "acme"}, Contract: api.Contract, Mode: api.FactoryTeam,
		Profile:   api.Profile{Handle: "alice"},
		SessionID: "session-secret-1", GitHubSignedIn: true,
		Runners: []api.Runner{{}}, Machines: []api.Machine{{}},
		TakesUnassigned: map[string]bool{"checkout-/home/alice/src": true},
		PollingEvery:    "30s", ServedBy: "runner-host-9",
	}
	raw, err := json.Marshal(NewFactoryMe(me))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"factory", "codeaf_version", "contract", "handle", "name", "avatar_url", "scopes", "mode", "operator", "access", "github_signed_in", "can_sign_out"} {
		if _, ok := got[key]; !ok {
			t.Errorf("reply lacks %q: %s", key, raw)
		}
	}
	if len(got) != 12 {
		t.Errorf("reply has %d keys, want 12: %s", len(got), raw)
	}
	if string(got["scopes"]) != "[]" {
		t.Errorf("scopes = %s, want []", got["scopes"])
	}
	for _, leak := range []string{"session", "runner", "machine", "takes_unassigned", "served_by", "polling", "/home/", "secret"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("reply leaks %q: %s", leak, raw)
		}
	}
}

// factory.me READS ONCE AND WRITES NOTHING.
func TestFactoryMeRouteReadsOnce(t *testing.T) {
	e := &factoryEngine{}
	out, err := methods["factory.me"].call(e, json.RawMessage(factoryGood["factory.me"]))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*FactoryMe); !ok {
		t.Fatalf("factory.me answered %T", out)
	}
	if !reflect.DeepEqual(e.calls, []string{"me f"}) {
		t.Fatalf("calls = %v", e.calls)
	}
}
