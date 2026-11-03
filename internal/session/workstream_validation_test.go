package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

// Contract (g) validates byte bounds and JSON shapes before any read or write.
func TestWorkstreamToolBoundsRefuseBeforeTheDoor(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"propose_decision", `{"text":""}`},
		{"propose_decision", `{"text":"` + strings.Repeat("é", 1001) + `"}`},
		{"propose_question", `{"text":"Words","source":{"kind":"url","id":"https://example.com","label":"` + strings.Repeat("x", 201) + `"}}`},
		{"propose_jobs", `{"jobs":[]}`},
		{"propose_jobs", `{"jobs":[{"title":"` + strings.Repeat("x", 201) + `"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft","repo":"two\nlines"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft","text":"` + strings.Repeat("x", 4001) + `"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":"1"}`},
		{"propose_goahead", `{"scope":"job","factory":"f","job":7,"cap_usd":"1","ends_on":"2026-11-04","why":"Words"}`},
		{"write_agent_page", `{"markdown":"Words","updated_from":"Discussion"}`},
		{"write_agent_page", `{"title":"Notes","markdown":"Words","updated_from":"two\nlines"}`},
		{"workstream_read", `[]`},
		{"workstream_read", `null`},
		{"workstream_read", `{"what":null}`},
		{"workstream_read", `{"what":"page"}`},
	}
	for _, tc := range cases {
		d := &workstreamFakeDoor{}
		ctx, mark := withToolRejection(context.Background())
		text, bad, err := workstreamToolForTest(t, d, tc.name).Execute(ctx, json.RawMessage(tc.args))
		if err != nil || !bad || !mark.RejectedBeforeRun() {
			t.Fatalf("%s: %q bad=%v rejected=%v err=%v", tc.name, text, bad, mark.RejectedBeforeRun(), err)
		}
		if d.otherCalls+d.reads+len(d.saves) != 0 {
			t.Fatalf("invalid %s reached the door: %+v", tc.name, d)
		}
	}
}

func TestWorkstreamArgumentRefusalsAreMarked(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		key  string
	}{
		{"workstream_read", `{"what":`, "valid JSON"},
		{"workstream_read", `{"what":"page"}`, "page_id"},
		{"workstream_read", `{"what":"pages","page_id":""}`, "page_id"},
		{"workstream_read", `{"what":"plan","kind":"decision"}`, "kind"},
		{"workstream_read", `"{\"what\":\"pages\"}"`, "JSON object"},
		{"propose_decision", `{"text":"Words","kept":false}`, "kept"},
		{"propose_question", `{"text":"Words","source":{"kind":"url","id":"https://example.com","kept":false}}`, "kept"},
		{"propose_decision", `{"text":"Words","source":{"kind":"url","id":"https://example.com","id":"https://example.com"}}`, "id"},
		{"propose_decision", `{"text":"Words","source":{"kind":"url","id":"https://example.com","factory":""}}`, "factory"},
		{"propose_decision", `{"text":"Words","source":null}`, "source"},
		{"propose_decision", `{"text":"Words","source":"{\"kind\":\"url\",\"id\":\"https://example.com\",\"kept\":true}"}`, "source"},
		{"propose_jobs", `{"jobs":[{"title":"Draft","kept":false}]}`, "kept"},
		{"propose_jobs", `{"jobs":[null]}`, "jobs[0]"},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":null}`, "wave"},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":2,"wave_title":""}`, "wave_title"},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":2,"milestone_id":""}`, "milestone_id"},
		{"propose_goahead", `{"scope":"wave","wave":2,"factory":"","cap_usd":12.5,"ends_on":"2026-11-04","why":"Review"}`, "factory"},
		{"write_agent_page", `{"page_id":"p1","title":"","markdown":"Words","updated_from":"Discussion"}`, "title"},
		{"write_agent_page", `{"page_id":"p1","parent_id":"","markdown":"Words","updated_from":"Discussion"}`, "parent_id"},
	} {
		d := &workstreamFakeDoor{}
		ctx, mark := withToolRejection(context.Background())
		text, bad, err := workstreamToolForTest(t, d, tc.name).Execute(ctx, json.RawMessage(tc.args))
		if err != nil || !bad || !mark.RejectedBeforeRun() || !strings.Contains(text, tc.key) {
			t.Fatalf("%s: %q bad=%v rejected=%v err=%v", tc.name, text, bad, mark.RejectedBeforeRun(), err)
		}
		if d.otherCalls+d.reads+len(d.saves) != 0 {
			t.Fatalf("invalid %s reached the door: %+v", tc.name, d)
		}
	}
}

func TestWorkstreamDoorErrorsAreNotArgumentRejections(t *testing.T) {
	for _, problem := range []error{ErrWorkstreamVersion, ErrWorkstreamNotAgentPage, ErrWorkstreamDone, errors.New("Invalid arguments: from the page store")} {
		d := &workstreamFakeDoor{pageError: problem}
		ctx, mark := withToolRejection(context.Background())
		text, bad, err := workstreamToolForTest(t, d, "write_agent_page").Execute(ctx, json.RawMessage(`{"page_id":"p1","markdown":"Words","updated_from":"Discussion"}`))
		if err != nil || !bad || mark.RejectedBeforeRun() || len(d.pageWrites) != 1 {
			t.Fatalf("%v: %q bad=%v rejected=%v err=%v", problem, text, bad, mark.RejectedBeforeRun(), err)
		}
	}
}

func TestWorkstreamSchemasAreWellFormedAndCarryNoReference(t *testing.T) {
	for _, tool := range (&Agent{config: Config{Workstream: &workstreamFakeDoor{}}}).workstreamTools() {
		if !json.Valid(tool.Schema) {
			t.Fatalf("%s has a malformed schema", tool.Name)
		}
		for _, mark := range []string{`"$ref"`, `"$defs"`, `"definitions"`} {
			if strings.Contains(string(tool.Schema), mark) {
				t.Fatalf("%s schema carries %s", tool.Name, mark)
			}
		}
	}
}

func TestWorkstreamWholeNumbersUseTheSharedArgumentGrammar(t *testing.T) {
	for _, number := range []string{"2.0", "2e0"} {
		d := &workstreamFakeDoor{plan: WorkstreamPlanState{Waves: []workstream.Wave{{N: 2}}}}
		workstreamCallForTest(t, d, "propose_jobs", `{"wave":`+number+`,"jobs":[{"title":"Draft"}]}`, false)
		if len(d.saves) != 1 || len(d.saves[0].Waves[0].Items) != 1 {
			t.Fatalf("whole-valued wave %s did not append one draft: %+v", number, d)
		}
	}
	for _, tc := range []struct {
		number string
		want   string
	}{
		{"2.5", `Invalid arguments: wave takes a whole number: send {"wave":2}, not 2.5`},
		{`"2"`, `Invalid arguments: wave takes a whole number: send {"wave":2}, not "2"`},
	} {
		d := &workstreamFakeDoor{}
		ctx, mark := withToolRejection(context.Background())
		text, bad, err := workstreamToolForTest(t, d, "propose_jobs").Execute(ctx, json.RawMessage(`{"wave":`+tc.number+`,"jobs":[{"title":"Draft"}]}`))
		if err != nil || !bad || !mark.RejectedBeforeRun() || text != tc.want {
			t.Fatalf("wave %s: %q bad=%v rejected=%v err=%v", tc.number, text, bad, mark.RejectedBeforeRun(), err)
		}
		if d.otherCalls+d.reads+len(d.saves) != 0 {
			t.Fatal("an invalid wave reached the door")
		}
	}
}

func TestWorkstreamGoAheadDecodesFractionalDollarsAndWholeTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		scope  string
	}{
		{"job", `"factory":"f","job":7.0`, "job f#7"},
		{"wave", `"wave":2e0`, "wave 2"},
	} {
		d := &workstreamFakeDoor{plan: WorkstreamPlanState{Waves: []workstream.Wave{{N: 2}}}}
		ctx, mark := withToolRejection(context.Background())
		args := `{"scope":"` + tc.name + `",` + tc.target + `,"cap_usd":12.50,"ends_on":"2026-11-04","why":"Review"}`
		text, bad, err := workstreamToolForTest(t, d, "propose_goahead").Execute(ctx, json.RawMessage(args))
		if err != nil || bad || mark.RejectedBeforeRun() || len(d.proposals) != 1 {
			t.Fatalf("%s: %q bad=%v rejected=%v err=%v", tc.name, text, bad, mark.RejectedBeforeRun(), err)
		}
		proposal := d.proposals[0]
		want := WorkstreamGoAheadProposalPrefix + tc.scope + ", up to $12.50, until 2026-11-04. Review"
		if proposal.GoAhead == nil || proposal.GoAhead.CapUSD != 12.5 || proposal.Text != want || proposal.Kind != "decision" {
			t.Fatalf("fractional dollars changed: %+v", proposal)
		}
	}
}
