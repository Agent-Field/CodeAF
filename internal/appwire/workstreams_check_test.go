package appwire

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

func TestStabEM4PlanBoundsBeforeDelegation(t *testing.T) {
	draftID, title := "draft", "Draft"
	draft := WorkstreamPlanItem{Kind: "draft", DraftID: &draftID, Title: &title}
	for _, tc := range []struct {
		name string
		edit func(*WorkstreamPlanSetParams)
		code string
	}{
		{"draft text", func(p *WorkstreamPlanSetParams) {
			text := strings.Repeat("x", workstream.PlanDraftTextMax+1)
			p.Waves[0].Items[0].Text = &text
		}, CodeTooLarge},
		{"milestones", func(p *WorkstreamPlanSetParams) {
			p.Milestones = make([]WorkstreamMilestone, workstream.PlanMilestonesMax+1)
			for i := range p.Milestones {
				p.Milestones[i] = WorkstreamMilestone{ID: fmt.Sprintf("m%d", i), Title: "Milestone"}
			}
		}, CodeTooLarge},
		{"waves", func(p *WorkstreamPlanSetParams) {
			p.Waves = make([]WorkstreamWave, workstream.PlanWavesMax+1)
			for i := range p.Waves {
				p.Waves[i] = WorkstreamWave{N: i + 1, Items: []WorkstreamPlanItem{}}
			}
		}, CodeTooLarge},
		{"items", func(p *WorkstreamPlanSetParams) {
			p.Waves[0].Items = make([]WorkstreamPlanItem, workstream.PlanItemsMax+1)
			for i := range p.Waves[0].Items {
				p.Waves[0].Items[i] = draft
			}
		}, CodeTooLarge},
		{"control characters", func(p *WorkstreamPlanSetParams) {
			text := "before\x00after"
			p.Waves[0].Items[0].Text = &text
		}, CodeBadParams},
		{"control characters past the text bound", func(p *WorkstreamPlanSetParams) {
			text := strings.Repeat("x", workstream.PlanDraftTextMax) + "\x00"
			p.Waves[0].Items[0].Text = &text
		}, CodeBadParams},
		{"bad draft id", func(p *WorkstreamPlanSetParams) {
			id := " invalid "
			p.Waves[0].Items[0].DraftID = &id
		}, CodeBadParams},
		{"at text bound", func(p *WorkstreamPlanSetParams) {
			text := strings.Repeat("x", workstream.PlanDraftTextMax)
			p.Waves[0].Items[0].Text = &text
		}, CodeUnknownMethod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := WorkstreamPlanSetParams{ID: "w", Milestones: []WorkstreamMilestone{}, Waves: []WorkstreamWave{{N: 1, Items: []WorkstreamPlanItem{draft}}}}
			tc.edit(&params)
			raw, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
			_, err = methods["workstream.plan_set"].call(fake, raw)
			wire, ok := err.(*Error)
			if !ok || wire.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}

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
		// WP-F1 (a), (b): version reads are positive and restore versions are paired.
		{"page.get", `{"id":"x","version":0}`, CodeBadParams},
		{"page.get", `{"id":"x","version":-1}`, CodeBadParams},
		{"page.restore", `{"id":"x","version":1}`, CodeBadParams},
		{"page.restore", `{"id":"x","base_version":1}`, CodeBadParams},
		{"page.restore", `{"id":"x","version":1,"base_version":-1}`, CodeBadParams},
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
		// WP-F1 (a), (b): additive params, including id-only restore, reach dispatch.
		"page.get":     `{"id":"x","version":1}`,
		"page.list":    `{"workstream_id":"x","include_deleted":true}`,
		"page.restore": `{"id":"x"}`,
	} {
		_, err := methods[method].call(fake, json.RawMessage(params))
		if err != ErrWorkstreamsNotYet {
			t.Fatalf("%s did not reach the fake: %v", method, err)
		}
	}
}

func TestWorkstreamControlCharactersBeforeDelegation(t *testing.T) {
	cases := []struct {
		name, method, params string
		prose                bool
	}{
		{"name", "workstream.create", `{"name":$text}`, false},
		{"rename", "workstream.update", `{"id":"x","patch":{"name":$text}}`, false},
		{"client_id", "workstream.create", `{"name":"x","client_id":$text}`, false},
		{"page title", "page.create", `{"workstream_id":"x","title":$text}`, false},
		{"updated page title", "page.update", `{"id":"x","title":$text}`, false},
		{"milestone title", "workstream.plan_set", `{"id":"x","base_version":0,"milestones":[{"id":"","title":$text}],"waves":[]}`, false},
		{"wave title", "workstream.plan_set", `{"id":"x","base_version":0,"milestones":[],"waves":[{"n":1,"title":$text,"auto_start":false,"items":[]}]}`, false},
		{"draft title", "workstream.plan_set", `{"id":"x","base_version":0,"milestones":[],"waves":[{"n":1,"auto_start":false,"items":[{"kind":"draft","draft_id":"d","title":$text}]}]}`, false},
		{"source label", "record.propose", `{"workstream_id":"x","kind":"decision","text":"x","source":{"kind":"chat","id":"c","label":$text}}`, false},
		{"replacement source label", "record.supersede", `{"id":"x","text":"x","source":{"kind":"chat","id":"c","label":$text}}`, false},
		{"why", "workstream.create", `{"name":"x","why":$text}`, true},
		{"updated why", "workstream.update", `{"id":"x","patch":{"why":$text}}`, true},
		{"how_it_went", "workstream.mark_done", `{"id":"x","how_it_went":$text}`, true},
		{"record text", "record.propose", `{"workstream_id":"x","kind":"question","text":$text}`, true},
		{"replacement text", "record.supersede", `{"id":"x","text":$text}`, true},
		{"draft text", "workstream.plan_set", `{"id":"x","base_version":0,"milestones":[],"waves":[{"n":1,"auto_start":false,"items":[{"kind":"draft","draft_id":"d","title":"Draft","text":$text}]}]}`, true},
	}
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	for _, tc := range cases {
		for control := rune(0); control <= 0x9f; control++ {
			if control > 0x1f && control < 0x7f {
				continue
			}
			t.Run(fmt.Sprintf("%s/U+%04X", tc.name, control), func(t *testing.T) {
				value, err := json.Marshal("before" + string(control) + "after")
				if err != nil {
					t.Fatal(err)
				}
				params := strings.ReplaceAll(tc.params, "$text", string(value))
				_, err = methods[tc.method].call(fake, json.RawMessage(params))
				if tc.prose && (control == '\n' || control == '\t') {
					if err != ErrWorkstreamsNotYet {
						t.Fatalf("allowed prose did not reach the fake: %v", err)
					}
					return
				}
				wire, ok := err.(*Error)
				if !ok || wire.Code != CodeBadParams {
					t.Fatalf("got %v, want %s before delegation", err, CodeBadParams)
				}
			})
		}
	}
}

func TestWorkstreamTextFieldsPreserveAllowedContent(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	for _, tc := range []struct{ method, params string }{
		{"workstream.create", `{"name":"Launch","client_id":"","why":""}`},
		{"workstream.create", `{"name":"Launch","client_id":" retry ","why":"first\n\tsecond"}`},
		{"page.create", `{"workstream_id":"x","title":" Résumé 界 "}`},
		{"record.propose", `{"workstream_id":"x","kind":"question","text":"first\n\tsecond","source":{"kind":"chat","id":"c","label":" Label "}}`},
		{"workstream.plan_set", `{"id":"x","base_version":0,"milestones":[],"waves":[{"n":1,"title":"","auto_start":false,"items":[{"kind":"draft","draft_id":"d","title":" Draft ","text":""}]}]}`},
	} {
		t.Run(tc.method+tc.params, func(t *testing.T) {
			_, err := methods[tc.method].call(fake, json.RawMessage(tc.params))
			if err != ErrWorkstreamsNotYet {
				t.Fatalf("allowed content did not reach the fake: %v", err)
			}
		})
	}
}
