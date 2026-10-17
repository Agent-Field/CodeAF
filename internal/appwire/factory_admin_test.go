package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// adminEngine is a factory engine that is also a FactoryAdmin and keeps what
// each call was handed.
type adminEngine struct {
	factoryEngine
	acts []string
	got  []FactoryAdminParams
	err  error
}

func (a *adminEngine) did(act string, p FactoryAdminParams) (FactoryAdminDone, error) {
	a.acts = append(a.acts, act)
	a.got = append(a.got, p)
	if a.err != nil {
		return FactoryAdminDone{}, a.err
	}
	return FactoryAdminDone{Factory: p.Factory, Action: act, SignedOut: true}, nil
}

func (a *adminEngine) FactorySignOut(_ context.Context, p FactoryAdminParams) (FactoryAdminDone, error) {
	return a.did(AdminActSignOut, p)
}

func (a *adminEngine) FactoryRevokeSessions(_ context.Context, p FactoryAdminParams) (FactoryAdminDone, error) {
	return a.did(AdminActRevokeSessions, p)
}

// AN ENGINE WITHOUT FactoryAdmin ANSWERS unknown_method, EVEN ONE THAT SERVES
// factory.*, and does not announce the feature.
func TestFactoryAdminAbsentWithoutAdmin(t *testing.T) {
	if len(factoryAdminMethods) != 2 {
		t.Fatalf("admin methods: %v", factoryAdminMethods)
	}
	for name := range factoryAdminMethods {
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}} {
			_, err := methods[name].call(e, json.RawMessage(`{"factory":"f"}`))
			if wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}} {
		if got := withFactoryAdminFeature(Hello{}, e); len(got.Features) != 0 {
			t.Fatalf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryAdminFeature(Hello{}, &adminEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryAdmin || FeatureFactoryAdmin != "factory.admin" {
		t.Fatalf("an admin engine announced %v", got.Features)
	}
	// The other optional halves are not implied by this one.
	if h := withFactoryActorFeature(Hello{}, &adminEngine{}); len(h.Features) != 0 {
		t.Fatalf("admin implied job actions: %v", h.Features)
	}
}

// THE ONLY PARAMETER IS THE CONNECTION: a session id, a path or a body a
// caller tries to add is refused, never ignored, and the engine is not asked.
func TestFactoryAdminRefusesAnythingButTheConnection(t *testing.T) {
	for name := range factoryAdminMethods {
		eng := &adminEngine{}
		for _, raw := range []string{
			`{}`, `{"factory":""}`, `{"factory":"  "}`, `{"factory":"f","session":"session_abc"}`,
			`{"factory":"f","session_id":"session_abc"}`, `{"factory":"f","path":"/v1/anything"}`,
			`{"factory":"f","body":{}}`, `{"factory":"f","all":true}`, `[]`, `"f"`, `null`,
		} {
			if _, err := methods[name].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
				t.Errorf("%s accepted %s: %v", name, raw, err)
			}
		}
		if len(eng.acts) != 0 {
			t.Fatalf("%s reached the engine with a bad request: %v", name, eng.acts)
		}
		out, err := methods[name].call(eng, json.RawMessage(`{"factory":" factory_1 "}`))
		if err != nil || len(eng.got) != 1 || eng.got[0].Factory != "factory_1" {
			t.Fatalf("%s: %v %+v", name, err, eng.got)
		}
		done := out.(*FactoryAdminDone)
		if done.Factory != "factory_1" || !strings.HasSuffix(name, done.Action) {
			t.Fatalf("%s answered %+v", name, done)
		}
	}
}

// An engine's refusal is passed on whole, and a write is asked once.
func TestFactoryAdminPassesTheEnginesWordsAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeUncertain, CodeConflict, CodeForbidden, CodeSignedOut, CodeBusy, CodeUnreachable} {
		for name := range factoryAdminMethods {
			eng := &adminEngine{err: Errorf(code, "words for %s", code)}
			_, err := methods[name].call(eng, json.RawMessage(`{"factory":"f"}`))
			if wireCode(t, err) != code || len(eng.acts) != 1 {
				t.Errorf("%s/%s: %v after %d calls", name, code, err, len(eng.acts))
			}
		}
	}
}

// NO TYPE THESE METHODS TAKE OR ANSWER HAS A FIELD FOR A CREDENTIAL OR A
// SESSION ID: the factory's handle on a sign-in never crosses this wire.
func TestFactoryAdminSchemaCarriesNoCredentialOrSessionId(t *testing.T) {
	banned := []string{"token", "device_code", "cookie", "secret", "password", "bearer", "authorization", "grant", "session", "link", "key"}
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
			for _, b := range banned {
				if strings.Contains(strings.ToLower(name), b) && name != "signed_out" {
					t.Errorf("%s: %s.%s (%q) looks like a credential or a session id", where, ty.Name(), f.Name, name)
				}
			}
			walk(where, f.Type)
		}
	}
	for name, m := range factoryAdminMethods {
		walk(name+" params", m.params)
		walk(name+" result", m.result)
	}
	if !seen[reflect.TypeFor[FactoryAdminParams]()] || !seen[reflect.TypeFor[FactoryAdminDone]()] {
		t.Fatal("the walk did not reach the admin types")
	}
	if n := reflect.TypeFor[FactoryAdminParams]().NumField(); n != 1 {
		t.Fatalf("the admin request grew to %d fields", n)
	}
}

func TestFactoryAdminSchemaNamesBothMethodsAndTheActions(t *testing.T) {
	doc := Schema()["methods"].(map[string]any)
	for name := range factoryAdminMethods {
		entry, ok := doc[name].(map[string]any)
		if !ok || entry["params"] == nil || entry["result"] == nil {
			t.Errorf("schema has no entry for %s", name)
		}
	}
	defs := Schema()["$defs"].(map[string]any)
	done := defs["FactoryAdminDone"].(map[string]any)["properties"].(map[string]any)
	if !reflect.DeepEqual(done["action"].(map[string]any)["enum"], []string{"sign_out", "revoke_sessions"}) {
		t.Fatalf("actions in schema: %v", done["action"])
	}
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["admin"] == nil {
		t.Fatal("capabilities do not carry admin")
	}
}
