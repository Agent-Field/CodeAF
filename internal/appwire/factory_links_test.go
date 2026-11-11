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

// linkEngine is a factory engine that is also a FactoryLinkAdmin and keeps what
// each call was handed.
type linkEngine struct {
	factoryEngine
	calls []string
	got   any
	err   error
}

func (l *linkEngine) did(name string, p any) error {
	l.calls = append(l.calls, name)
	l.got = p
	return l.err
}

func (l *linkEngine) FactoryLinkList(_ context.Context, p FactoryLinkParams) (FactoryLinkList, error) {
	return FactoryLinkList{Factory: p.Factory}, l.did("list", p)
}
func (l *linkEngine) FactoryLinkCreate(_ context.Context, p FactoryLinkCreateParams) (FactoryLinkMade, error) {
	return FactoryLinkMade{Factory: p.Factory, Outcome: LinkCopied}, l.did("create", p)
}
func (l *linkEngine) FactoryLinkCopy(_ context.Context, p FactoryLinkCopyParams) (FactoryLinkMade, error) {
	return FactoryLinkMade{Factory: p.Factory, Outcome: LinkCopied}, l.did("copy", p)
}
func (l *linkEngine) FactoryLinkRevoke(_ context.Context, p FactoryLinkRevokeParams) (FactoryLinkRevoked, error) {
	return FactoryLinkRevoked{Factory: p.Factory, ID: p.ID, Revoked: true}, l.did("revoke", p)
}

// ABSENT WITHOUT THE INTERFACE, AND NOT IMPLIED BY factory.admin.
func TestFactoryLinksAbsentWithoutTheInterface(t *testing.T) {
	if len(factoryLinkMethods) != 4 {
		t.Fatalf("link methods: %v", factoryLinkMethods)
	}
	for name := range factoryLinkMethods {
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}} {
			_, err := methods[name].call(e, json.RawMessage(`{"factory":"f"}`))
			if wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}} {
		if got := withFactoryLinksFeature(Hello{}, e); len(got.Features) != 0 {
			t.Fatalf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryLinksFeature(Hello{}, &linkEngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.links" || FeatureFactoryLinks == FeatureFactoryAdmin {
		t.Fatalf("a link engine announced %v", got.Features)
	}
	if h := withFactoryAdminFeature(Hello{}, &linkEngine{}); len(h.Features) != 0 {
		t.Fatalf("links implied admin: %v", h.Features)
	}
}

// PARAMS ARE STRICT: a url, token, path or body is bad_params, and the engine
// is not asked.
func TestFactoryLinksParamsAreStrict(t *testing.T) {
	cases := map[string][]string{
		"factory.links": {`{}`, `{"factory":" "}`, `{"factory":"f","url":"x"}`, `{"factory":"f","all":true}`, `[]`, `null`},
		"factory.link_create": {`{}`, `{"factory":"f","url":"codeaf://x"}`, `{"factory":"f","token":"t"}`, `{"factory":"f","path":"/v1/x"}`,
			`{"factory":"f","body":{}}`, `{"factory":"f","max_uses":0}`, `{"factory":"f","max_uses":51}`, `{"factory":"f","max_uses":-1}`,
			`{"factory":"f","max_uses":"3"}`, `{"factory":"f","login":"two words"}`, `{"factory":"f","login":"a","max_uses":2}`,
			`{"factory":"f","name":"` + strings.Repeat("x", 81) + `"}`, `{"factory":"f","name":"a\nb"}`, `{"factory":"f","name":"a‮b"}`,
			`{"factory":"f","expires_at":"0001-01-01T00:00:00Z"}`, `{"factory":"f","expires_at":"soon"}`, `{"factory":"f","copied":true}`},
		"factory.link_copy": {`{"factory":"f"}`, `{"factory":"f","ref":""}`, `{"factory":"f","ref":"short"}`, `{"factory":"f","ref":"../../../../../../x"}`,
			`{"factory":"f","ref":"AAAAAAAAAAAAAAAAAAAAAA","url":"x"}`, `{"ref":"AAAAAAAAAAAAAAAAAAAAAA"}`},
		"factory.link_revoke": {`{"factory":"f"}`, `{"factory":"f","id":""}`, `{"factory":"f","id":"../x"}`, `{"factory":"f","id":"a/b"}`,
			`{"factory":"f","id":"%2e"}`, `{"factory":"f","id":"a?b"}`, `{"factory":"f","id":"a b"}`, `{"factory":"f","id":"é"}`,
			`{"factory":"f","id":"` + strings.Repeat("a", 65) + `"}`, `{"factory":"f","id":"a","all":true}`},
	}
	for name, bad := range cases {
		eng := &linkEngine{}
		for _, raw := range bad {
			if _, err := methods[name].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
				t.Errorf("%s accepted %s: %v", name, raw, err)
			}
		}
		if len(eng.calls) != 0 {
			t.Fatalf("%s reached the engine with a bad request: %v", name, eng.calls)
		}
	}
}

func TestFactoryLinksGoodParamsAreNormalisedAndPassedOn(t *testing.T) {
	eng := &linkEngine{}
	if _, err := methods["factory.link_create"].call(eng, json.RawMessage(`{"factory":" f ","name":"  Crew  ","login":"@Priya","max_uses":1,"expires_at":"2026-10-20T00:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	p := eng.got.(FactoryLinkCreateParams)
	if p.Factory != "f" || p.Name != "Crew" || p.Login != "priya" || p.MaxUses == nil || *p.MaxUses != 1 || p.ExpiresAt == nil || p.ExpiresAt.Day() != 20 {
		t.Fatalf("got %+v", p)
	}
	// Nothing typed: nothing is invented, so the factory's defaults apply.
	if _, err := methods["factory.link_create"].call(eng, json.RawMessage(`{"factory":"f"}`)); err != nil {
		t.Fatal(err)
	}
	if p := eng.got.(FactoryLinkCreateParams); p.MaxUses != nil || p.ExpiresAt != nil || p.Name != "" || p.Login != "" {
		t.Fatalf("defaults were invented: %+v", p)
	}
	if _, err := methods["factory.link_revoke"].call(eng, json.RawMessage(`{"factory":"f","id":"tok_0a-Z"}`)); err != nil || eng.got.(FactoryLinkRevokeParams).ID != "tok_0a-Z" {
		t.Fatalf("%v %+v", err, eng.got)
	}
	if _, err := methods["factory.link_copy"].call(eng, json.RawMessage(`{"factory":"f","ref":"AAAAAAAAAAAAAAAAAAAAAA"}`)); err != nil {
		t.Fatal(err)
	}
}

// An engine's refusal is passed on whole, and a write is asked once.
func TestFactoryLinksPassTheEnginesWordsAndAskOnce(t *testing.T) {
	good := map[string]string{
		"factory.links": `{"factory":"f"}`, "factory.link_create": `{"factory":"f"}`,
		"factory.link_copy": `{"factory":"f","ref":"AAAAAAAAAAAAAAAAAAAAAA"}`, "factory.link_revoke": `{"factory":"f","id":"tok_a"}`,
	}
	for _, code := range []string{CodeUncertain, CodeConflict, CodeForbidden, CodeSignedOut, CodeBusy, CodeUnreachable, CodeNotFound} {
		for name, raw := range good {
			eng := &linkEngine{err: Errorf(code, "words for %s", code)}
			_, err := methods[name].call(eng, json.RawMessage(raw))
			if wireCode(t, err) != code || len(eng.calls) != 1 {
				t.Errorf("%s/%s: %v after %d calls", name, code, err, len(eng.calls))
			}
		}
	}
}

// NO TYPE THESE METHODS TAKE OR ANSWER HAS A FIELD FOR THE INVITATION'S ADDRESS
// OR SECRET, OR ANYTHING NAMED FOR ONE. `ref` is the engine's own opaque handle.
func TestFactoryLinksSchemaCarriesNoAddressOrSecret(t *testing.T) {
	banned := []string{"token", "device_code", "cookie", "secret", "password", "bearer", "authorization", "grant", "url", "link", "key", "session", "address", "href", "code"}
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
				if strings.Contains(strings.ToLower(name), b) {
					t.Errorf("%s: %s.%s (%q) looks like an address, secret or credential", where, ty.Name(), f.Name, name)
				}
			}
			walk(where, f.Type)
		}
	}
	for name, m := range factoryLinkMethods {
		walk(name+" params", m.params)
		walk(name+" result", m.result)
	}
	for _, ty := range []reflect.Type{reflect.TypeFor[FactoryLinkList](), reflect.TypeFor[FactoryLinkMade](), reflect.TypeFor[FactoryLink](), reflect.TypeFor[FactoryLinkRevoked]()} {
		if !seen[ty] {
			t.Fatalf("the walk did not reach %s", ty.Name())
		}
	}
	made := reflect.TypeFor[FactoryLinkMade]()
	if f, ok := made.FieldByName("Ref"); !ok || f.Tag.Get("json") != "ref,omitempty" {
		t.Fatal("ref is not the optional opaque handle")
	}
}

func TestFactoryLinksSchemaNamesEveryMethodAndTheOutcomes(t *testing.T) {
	doc := Schema()["methods"].(map[string]any)
	for name := range factoryLinkMethods {
		entry, ok := doc[name].(map[string]any)
		if !ok || entry["params"] == nil || entry["result"] == nil {
			t.Errorf("schema has no entry for %s", name)
		}
	}
	defs := Schema()["$defs"].(map[string]any)
	made := defs["FactoryLinkMade"].(map[string]any)["properties"].(map[string]any)
	if !reflect.DeepEqual(made["outcome"].(map[string]any)["enum"], []string{"copied", "not_copied", "unconfirmed"}) {
		t.Fatalf("outcomes: %v", made["outcome"])
	}
	link := defs["FactoryLink"].(map[string]any)["properties"].(map[string]any)
	if !reflect.DeepEqual(link["status"].(map[string]any)["enum"], []string{"open", "used_up", "expired", "revoked"}) {
		t.Fatalf("statuses: %v", link["status"])
	}
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["links"] == nil || caps["copy_link"] == nil {
		t.Fatal("capabilities do not carry links and copy_link")
	}
}

// A LINK ROW'S JSON IS THE PUBLIC FACTS ONLY, and counted is absent when unknown.
func TestFactoryLinkJSONShape(t *testing.T) {
	raw, _ := json.Marshal(FactoryLink{ID: "tok_a", Status: "open", UsedBy: []string{}})
	if strings.Contains(string(raw), "counted") || !strings.Contains(string(raw), `"used_by":[]`) {
		t.Fatalf("%s", raw)
	}
	yes := true
	raw, _ = json.Marshal(FactoryLink{ID: "tok_a", Status: "open", Counted: &yes})
	if !strings.Contains(string(raw), `"counted":true`) {
		t.Fatalf("%s", raw)
	}
}

// THE FACTORY'S OWN STATEMENT OF WHAT IT CAN DO, AND AN OLDER FACTORY'S SILENCE.
func TestFactoryMeCarriesTheAvailabilityBooleansAndSilenceIsFalse(t *testing.T) {
	var old api.Me
	if err := json.Unmarshal([]byte(`{"mode":"team","operator":true,"handle":"alice","session_id":"session_x","github_signed_in":true}`), &old); err != nil {
		t.Fatal(err)
	}
	if got := NewFactoryMe(old); got.GitHubSignInAvailable || got.InviteAvailable {
		t.Fatalf("an older factory's silence read as true: %+v", got)
	}
	// Neither a role nor an id promotes them.
	if got := NewFactoryMe(api.Me{Operator: true, Mode: api.FactoryTeam, SessionID: "session_x", GitHubSignedIn: true}); got.InviteAvailable || got.GitHubSignInAvailable {
		t.Fatalf("inferred: %+v", got)
	}
	got := NewFactoryMe(api.Me{GitHubSignInAvailable: true, InviteAvailable: true})
	if !got.GitHubSignInAvailable || !got.InviteAvailable {
		t.Fatalf("%+v", got)
	}
	// Invitations need the sign-in, so one cannot be reported without it.
	if got := NewFactoryMe(api.Me{InviteAvailable: true}); got.InviteAvailable {
		t.Fatalf("invites without sign-in: %+v", got)
	}
}
