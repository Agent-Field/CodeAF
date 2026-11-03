package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// B1: malformed requests are refused at the real typed dispatch door, before
// the serving fake (which always answers unknown_method) can receive them.
func TestWorkstreamBoundsBeforeDelegation(t *testing.T) {
	cases := []struct{ method, params, code string }{
		{"workstream.get", `{}`, CodeBadParams},
		{"workstream.create", `{"name":""}`, CodeBadParams},
		{"workstream.create", `{"name":"x","client_id":"` + strings.Repeat("a", 129) + `"}`, CodeBadParams},
		{"workstream.create", `{"name":"x","why":"` + strings.Repeat("a", 4001) + `"}`, CodeBadParams},
		{"workstream.create", `{"name":"x","refs":[{"kind":"factory_job","id":"01","factory":"f"}]}`, CodeBadParams},
		{"workstream.create", `{"name":"x","refs":[` + strings.Repeat(`{"kind":"conversation","id":"c"},`, 200) + `{"kind":"conversation","id":"c"}]}`, CodeBadParams},
		{"workstream.update", `{"id":"x"}`, CodeBadParams},
		{"workstream.update", `{"id":"x","patch":{"autonomy":"auto"}}`, CodeBadParams},
		{"workstream.update", `{"id":"x","patch":{"wake":{"morning":false}}}`, CodeBadParams},
		{"workstream.mark_done", `{"id":"x","how_it_went":"` + strings.Repeat("a", 4001) + `"}`, CodeBadParams},
		{"workstream.plan_set", `{"id":"x","milestones":[],"waves":[]}`, CodeBadParams},
		{"workstream.plan_set", `{"id":"x","base_version":-1,"milestones":[],"waves":[]}`, CodeBadParams},
		{"workstream.plan_set", `{"id":"x","base_version":0,"milestones":[],"waves":[{"n":0,"auto_start":false,"items":[]}]}`, CodeBadParams},
		{"page.create", `{"workstream_id":"x","title":"x","owner":"other"}`, CodeBadParams},
		{"page.create", `{"workstream_id":"x","title":"x","doc":[]}`, CodeBadParams},
		{"page.update", `{"id":"x","title":""}`, CodeBadParams},
		{"page.update", `{"id":"x","title":"` + strings.Repeat("t", 201) + `"}`, CodeBadParams},
		{"page.save", `{"id":"x","base_version":1}`, CodeBadParams},
		{"page.save", `{"id":"x","base_version":1,"doc":{"text":"` + strings.Repeat("a", 1<<20) + `"}}`, CodeTooLarge},
		{"page.move", `{"id":"x","parent_id":""}`, CodeBadParams},
		{"page.history", `{"id":"x","limit":0}`, CodeBadParams},
		{"page.history", `{"id":"x","limit":201}`, CodeBadParams},
		{"page.restore", `{"id":"x","version":0,"base_version":1}`, CodeBadParams},
		{"record.list", `{"workstream_id":"x","state":"active"}`, CodeBadParams},
		{"record.propose", `{"workstream_id":"x","kind":"decision","text":""}`, CodeBadParams},
		{"record.propose", `{"workstream_id":"x","kind":"decision","text":"` + strings.Repeat("t", 2001) + `"}`, CodeBadParams},
		{"record.propose", `{"workstream_id":"x","kind":"question","text":"x","source":{"kind":"message","id":"m"}}`, CodeBadParams},
		{"record.supersede", `{"id":"x","text":"x","kind":"other"}`, CodeBadParams},
		{"goahead.grant", `{"workstream_id":"x","scope":"wave","wave":1,"ends_at":"2099-01-01T00:00:00Z"}`, CodeBadParams},
		{"goahead.grant", `{"workstream_id":"x","scope":"wave","wave":1,"cap_usd":10001,"ends_at":"2099-01-01T00:00:00Z"}`, CodeBadParams},
	}
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	for _, tc := range cases {
		t.Run(tc.method+tc.params[:min(len(tc.params), 55)], func(t *testing.T) {
			_, err := methods[tc.method].call(fake, json.RawMessage(tc.params))
			wire, ok := err.(*Error)
			if !ok || wire.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}

func TestWorkstreamBoundsPreserveExplicitZeroAndFalse(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	for method, params := range map[string]string{
		"workstream.plan_set": `{"id":"x","base_version":0,"milestones":[],"waves":[]}`,
		"workstream.update":   `{"id":"x","patch":{"why":"","budget_usd":0,"wake":{"morning":false,"related_issues":false}}}`,
		"page.create":         `{"workstream_id":"x","title":"x","read_by_jobs":false}`,
	} {
		_, err := methods[method].call(fake, json.RawMessage(params))
		if err != ErrWorkstreamsNotYet {
			t.Fatalf("%s did not reach the fake: %v", method, err)
		}
	}
}
