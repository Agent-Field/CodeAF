package appwire

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// WP-F2 (a), (b), (d), (f): typed dispatch refuses malformed additive fields.
func TestWorkstreamV02ParamsBeforeDelegation(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	for _, fields := range []string{`"size":"XL"`, `"size":""`, `"budget_usd":0`, `"budget_usd":-1`, `"budget_usd":10000.01`, `"source":{"kind":"message","id":"m"}`} {
		params := `{"id":"w","base_version":0,"milestones":[],"waves":[{"n":1,"auto_start":false,"items":[{"kind":"draft","draft_id":"d","title":"Draft",` + fields + `}]}]}`
		_, err := methods["workstream.plan_set"].call(fake, json.RawMessage(params))
		workstreamV02BadParams(t, err)
	}
	for _, fields := range []string{`"size":"S"`, `"size":""`, `"budget_usd":1`, `"budget_usd":0`, `"source":{"kind":"chat","id":"c"}`} {
		params := `{"id":"w","base_version":0,"milestones":[],"waves":[{"n":1,"auto_start":false,"items":[{"kind":"job","factory":"f","job":1,` + fields + `}]}]}`
		_, err := methods["workstream.plan_set"].call(fake, json.RawMessage(params))
		workstreamV02BadParams(t, err)
	}
	proposal := `{"scope":"wave","wave":1,"cap_usd":1,"ends_at":"2026-11-04T00:00:00Z"}`
	for _, tc := range []struct{ method, params string }{
		{"record.propose", `{"workstream_id":"w","kind":"goahead","text":"Why"}`},
		{"record.propose", `{"workstream_id":"w","kind":"goahead","text":"Why","kept":true,"goahead":` + proposal + `}`},
		{"record.propose", `{"workstream_id":"w","kind":"decision","text":"Why","goahead":` + proposal + `}`},
		{"record.propose", `{"workstream_id":"w","kind":"goahead","text":"Why","goahead":{"scope":"wave","wave":1,"cap_usd":1}}`},
		{"record.supersede", `{"id":"r","kind":"goahead","text":"Why"}`},
		{"page.save", `{"id":"p","base_version":1,"doc":{},"source":{"kind":"message","id":"m"}}`},
		{"page.create", `{"workstream_id":"w","title":"Page","source":{"kind":"chat","id":"c","factory":"f"}}`},
		{"page.create", `{"workstream_id":"w","title":"Page","source":{"kind":"page","id":"p","chat_id":"c"}}`},
	} {
		_, err := methods[tc.method].call(fake, json.RawMessage(tc.params))
		workstreamV02BadParams(t, err)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1)} {
		id, title := "d", "Draft"
		p := WorkstreamPlanSetParams{ID: "w", Milestones: []WorkstreamMilestone{}, Waves: []WorkstreamWave{{N: 1, Items: []WorkstreamPlanItem{{Kind: "draft", DraftID: &id, Title: &title, BudgetUSD: &value}}}}}
		workstreamV02BadParams(t, p.Check())
	}
}

func workstreamV02BadParams(t *testing.T, err error) {
	t.Helper()
	wire, ok := err.(*Error)
	if !ok || wire.Code != CodeBadParams {
		t.Fatalf("want bad_params, got %v", err)
	}
}

// WP-F2 (a), (b), (d), (f): each valid additive field reaches the typed delegate.
func TestWorkstreamV02ParamsReachDelegation(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	source := `{"kind":"message","id":"m","chat_id":"c","label":"Scope"}`
	for method, params := range map[string]string{
		"workstream.plan_set": `{"id":"w","base_version":0,"milestones":[],"waves":[{"n":1,"auto_start":false,"items":[{"kind":"draft","draft_id":"d","title":"Draft","size":"M","budget_usd":10000,"source":` + source + `}]}]}`,
		"page.save":           `{"id":"p","doc":{},"base_version":1,"source":` + source + `}`,
		"page.create":         `{"workstream_id":"w","title":"Page","source":` + source + `}`,
		"record.list":         `{"workstream_id":"w","kind":"goahead","include_goaheads":false}`,
		"record.propose":      `{"workstream_id":"w","kind":"goahead","text":"Why","kept":false,"goahead":{"scope":"wave","wave":1,"cap_usd":1,"ends_at":"2026-11-04T00:00:00Z"}}`,
	} {
		_, err := methods[method].call(fake, json.RawMessage(params))
		if err != ErrWorkstreamsNotYet {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, kind := range []string{"chat", "message", "factory_job", "page", "url"} {
		p := WorkstreamSource{Kind: kind, ID: "7"}
		chat, factory := "chat", "factory"
		if kind == "message" {
			p.ChatID = &chat
		}
		if kind == "factory_job" {
			p.Factory = &factory
		}
		if err := p.Check(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		label := strings.Repeat("x", 201)
		p.Label = &label
		workstreamV02BadParams(t, p.Check())
	}
}
