package appwire

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
)

// orgAccessEngine is a factory engine that also carries organization requests,
// and keeps each one it is handed.
type orgAccessEngine struct {
	factoryEngine
	got []OrgRequest
}

func (e *orgAccessEngine) FactoryOrgCall(_ context.Context, _ string, req OrgRequest, _ any) error {
	e.got = append(e.got, req)
	return nil
}

// THE APP NEVER CHOOSES A PATH: every access method is one request whose path
// is built here from a checked id and whose body is the fields the route takes.
func TestOrgAccessMethodsBuildOneFixedRequestEach(t *testing.T) {
	const conn = `"connection":"c1"`
	grant := `"kind":"workstream","id":"ws_1","subject":{"kind":"person","id":"mia"},"role":"edit","expires_at":"2027-01-02T03:04:05Z","note":" for the launch "`
	cases := []struct {
		method, params string
		want           OrgRequest
	}{
		{"factory.org_policy", `{` + conn + `}`, OrgRequest{Method: http.MethodGet, Path: "/v1/org/policy"}},
		{"factory.org_policy_set", `{` + conn + `,"policy":{"rev":3,"roles":[]}}`,
			OrgRequest{Method: http.MethodPut, Path: "/v1/org/policy", Body: json.RawMessage(`{"rev":3,"roles":[]}`)}},
		{"factory.org_grants", `{` + conn + `,"kind":"chat","id":"ch_x"}`, OrgRequest{Method: http.MethodGet, Path: "/v1/resources/chat/ch_x/grants"}},
		{"factory.org_grant_add", `{` + conn + `,` + grant + `}`, OrgRequest{Method: http.MethodPost, Path: "/v1/resources/workstream/ws_1/grants",
			Body: map[string]any{"subject": OrgGrantSubject{Kind: "person", ID: "mia"}, "role": "edit", "expires_at": "2027-01-02T03:04:05Z", "note": "for the launch"}}},
		{"factory.org_grant_preview", `{` + conn + `,` + grant + `}`, OrgRequest{Method: http.MethodPost, Path: "/v1/resources/workstream/ws_1/grants/preview",
			Body: map[string]any{"subject": OrgGrantSubject{Kind: "person", ID: "mia"}, "role": "edit", "expires_at": "2027-01-02T03:04:05Z", "note": "for the launch"}}},
		{"factory.org_grant_remove", `{` + conn + `,"kind":"canvas","id":"cv_1","grant":"g_7"}`, OrgRequest{Method: http.MethodDelete, Path: "/v1/resources/canvas/cv_1/grants/g_7"}},
		{"factory.org_move", `{` + conn + `,"kind":"canvas","id":"cv_1","to":"ws_2"}`, OrgRequest{Method: http.MethodPost, Path: "/v1/resources/canvas/cv_1/move",
			Body: map[string]string{"to": "ws_2"}}},
	}
	for _, c := range cases {
		e := &orgAccessEngine{}
		if _, err := methods[c.method].call(e, json.RawMessage(c.params)); err != nil {
			t.Fatalf("%s: %v", c.method, err)
		}
		if len(e.got) != 1 || !reflect.DeepEqual(e.got[0], c.want) {
			t.Errorf("%s built %+v, want %+v", c.method, e.got, c.want)
		}
	}
}

// An id that could climb out of its segment, or a body that is not a policy, never reaches the server.
func TestOrgAccessMethodsRefuseWhatCouldChooseAPath(t *testing.T) {
	const conn = `"connection":"c1"`
	for _, c := range []struct{ method, params string }{
		{"factory.org_grants", `{` + conn + `,"kind":"chat/../x","id":"ch_x"}`},
		{"factory.org_grants", `{` + conn + `,"kind":"Chat","id":"ch_x"}`},
		{"factory.org_grants", `{` + conn + `,"kind":"chat","id":"../org/policy"}`},
		{"factory.org_grants", `{` + conn + `,"kind":"chat","id":"a?b"}`},
		{"factory.org_grant_remove", `{` + conn + `,"kind":"chat","id":"ch_x","grant":"g/1"}`},
		{"factory.org_grant_remove", `{` + conn + `,"kind":"chat","id":"ch_x","grant":""}`},
		{"factory.org_grant_add", `{` + conn + `,"kind":"chat","id":"ch_x","subject":{"kind":"person","id":""},"role":"view"}`},
		{"factory.org_grant_add", `{` + conn + `,"kind":"chat","id":"ch_x","subject":{"kind":"a/b","id":"mia"},"role":"view"}`},
		{"factory.org_grant_add", `{` + conn + `,"kind":"chat","id":"ch_x","subject":{"kind":"person","id":"mia"},"role":""}`},
		{"factory.org_move", `{` + conn + `,"kind":"canvas","id":"cv_1","to":""}`},
		{"factory.org_move", `{` + conn + `,"kind":"canvas","id":"cv_1","to":"ws/2"}`},
		{"factory.org_policy_set", `{` + conn + `,"policy":[1]}`},
		{"factory.org_policy_set", `{` + conn + `,"policy":"x"}`},
		{"factory.org_policy_set", `{` + conn + `}`},
		{"factory.org_policy", `{}`},
	} {
		e := &orgAccessEngine{}
		_, err := methods[c.method].call(e, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams || len(e.got) != 0 {
			t.Errorf("%s %s: err %v, requests %+v", c.method, c.params, err, e.got)
		}
	}
}

// The hello word is announced only by an engine that can carry organization requests.
func TestOrgAccessIsAnnouncedOnlyByAnEngineThatServesIt(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		if h := withFactoryOrgAccessFeature(Hello{}, e); slices.Contains(h.Features, FeatureOrgAccess) {
			t.Errorf("%T announced %v", e, h.Features)
		}
		if _, err := methods["factory.org_policy"].call(e, json.RawMessage(`{"connection":"c1"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
	}
	if h := withFactoryOrgAccessFeature(Hello{}, &orgAccessEngine{}); !slices.Contains(h.Features, FeatureOrgAccess) {
		t.Fatalf("announced %v", h.Features)
	}
}
