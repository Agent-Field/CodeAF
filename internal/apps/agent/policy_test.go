package agent

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestUsableToolsComeOnlyFromCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		access     spec.Access
		agentTools []string
		runs       bool
		connectors map[string]spec.Connector
		tools      []string
		want       []string
	}{
		{name: "persona cannot grant tools", agentTools: []string{"records.query", "apps.keep", "gmail.send"}},
		{name: "readable without persona list", access: spec.AccessRead, want: []string{"records.get", "records.query"}},
		{name: "writable despite persona list", access: spec.AccessReadWrite, agentTools: []string{"records.query"}, want: []string{"records.create", "records.delete", "records.get", "records.query", "records.update"}},
		{name: "declared run", runs: true, want: []string{"runs.start"}},
		{name: "connector capabilities", connectors: map[string]spec.Connector{"gmail": {Can: []string{"send", "read", "send"}}}, want: []string{"gmail.read", "gmail.send"}},
		{name: "declared external tool", tools: []string{"calendar.create", "apps.keep", "builder.write", "records.delete", "runs.start", "shell"}, want: []string{"calendar.create"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &spec.Bundle{
				App:   &spec.App{Records: map[string]*spec.RecordType{"contact": {}}, Capabilities: spec.Capabilities{Records: map[string]spec.Access{"contact": tc.access}, Connectors: tc.connectors, Tools: tc.tools}},
				Agent: &spec.AgentDoc{Tools: tc.agentTools},
			}
			if tc.runs {
				b.Runs = map[string]*spec.Run{"follow-up": {}}
			}
			if got := usableTools(b); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tools = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreateAndDeleteIgnoreThePersonaToolList(t *testing.T) {
	tests := []struct {
		name, reply, summary string
	}{
		{"add contact", `{"tool":"records.create","args":{"type":"contact","set":{"name":"Morgan Vale"}}}`, "Created Morgan Vale"},
		{"delete contact", `{"tool":"records.delete","args":{"type":"contact","id":"ct_1"}}`, "Deleted 1 contact"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.reply, `{"say":"Done."}`)
			r.svc.all.Store.(fakeStore).bundle.Agent.Tools = []string{"records.query", "apps.keep"}
			events := r.ask(t, tc.name)
			if events[0].Summary != tc.summary || len(r.records.scopes) != 1 || !r.records.scopes[0].Caller.Agent {
				t.Fatalf("events = %+v, writes = %+v", events, r.records.scopes)
			}
		})
	}
}

func TestWritableTypeDoesNotGrantWritesToReadableType(t *testing.T) {
	r := newRig(t,
		`{"tool":"records.create","args":{"type":"contact","set":{"name":"Morgan Vale"}}}`,
		`{"say":"I cannot edit contacts."}`)
	r.svc.all.Store.(fakeStore).bundle.App.Capabilities.Records["contact"] = spec.AccessRead
	r.ask(t, "add contact")
	if len(r.records.creates) != 0 {
		t.Fatal("read-only contact was written")
	}
	msgs := r.model.calls[1].Messages
	if !strings.Contains(msgs[len(msgs)-1].Text, "cannot write records of type contact") {
		t.Fatalf("feedback = %+v", msgs)
	}
}

func TestEmptyNameLookupRetriesOnceBeforeTheModelCanAskForAnID(t *testing.T) {
	tests := []struct {
		name, query string
		firstRows   bool
		fail        bool
		wantQueries []string
	}{
		{"exact title", `name:"Ada Lovelace"`, false, false, []string{`name:"Ada Lovelace"`, `name~"Ada Lovelace"`}},
		{"drop filters", `name:Ada has:email`, false, false, []string{`name:Ada has:email`, `name~"Ada"`}},
		{"contains is bounded", `name~Ada`, false, false, []string{`name~Ada`}},
		{"wildcard is bounded", `name:Ada*`, false, false, []string{`name:Ada*`}},
		{"no name", `email:ada@example.com`, false, false, []string{`email:ada@example.com`}},
		{"excluded name", `name:-Ada`, false, false, []string{`name:-Ada`}},
		{"found already", `name:Ada`, true, false, []string{`name:Ada`}},
		{"error is not empty", `name:Ada`, false, true, []string{`name:Ada`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.records.queryFn = func(p bridge.QueryParams) (bridge.QueryResult, error) {
				if tc.fail {
					return bridge.QueryResult{}, errors.New("query failed")
				}
				if tc.firstRows {
					return bridge.QueryResult{Rows: []bridge.Record{{Type: "contact", Fields: map[string]any{"name": "Ada"}}}, Total: 1}, nil
				}
				return bridge.QueryResult{}, nil
			}
			tb := newToolbox(r.svc.all, scope, r.svc.all.Store.(fakeStore).bundle)
			out, err := tb.call(context.Background(), "records.query", map[string]any{"type": "contact", "query": tc.query})
			if (err != nil) != tc.fail {
				t.Fatalf("call error = %v", err)
			}
			var got []string
			for _, p := range r.records.queries {
				got = append(got, p.Query)
			}
			if !slices.Equal(got, tc.wantQueries) {
				t.Fatalf("queries = %v, want %v", got, tc.wantQueries)
			}
			if len(got) == 2 && !strings.Contains(out.note, "retried") {
				t.Fatalf("retry not reported to model: %+v", out)
			}
		})
	}
}

func TestNameRetryFindsCaseInsensitiveSubstringThroughTheRealStore(t *testing.T) {
	r := newRig(t,
		`Let me find Ada. {"tool":"records.query","args":{"type":"contact","query":"name:ADA"}}`,
		`{"say":"Found Ada Lovelace."}`)
	r.svc.all.Records = records.New(svc.Config{Root: r.root}, r.svc.all)
	if err := r.svc.all.Records.Seed(context.Background(), "crm", map[string][]bridge.Record{
		"contact": {{ID: "ct_ada", Fields: map[string]any{"name": "Ada Lovelace", "phone": "+1 555 0100"}}},
	}); err != nil {
		t.Fatal(err)
	}
	events := r.ask(t, "find Ada")
	if events[0].Summary != "Looked up 1 contact" {
		t.Fatalf("events = %+v", events)
	}
	msgs := r.model.calls[1].Messages
	feedback := msgs[len(msgs)-1].Text
	for _, want := range []string{"Ada Lovelace", "case-insensitive", `name~"ADA"`} {
		if !strings.Contains(feedback, want) {
			t.Errorf("feedback lacks %q: %s", want, feedback)
		}
	}
	if strings.Contains(feedback, "555") {
		t.Fatal("retry exposed masked phone")
	}
}

func TestMixedRepliesExecuteOnlyTheFirstActionAndNeverStreamProtocol(t *testing.T) {
	r := newRig(t,
		`Looking now. {"tool":"records.query","args":{"type":"contact"}} {"tool":"records.delete","args":{"type":"contact","id":"ct_1"}}`,
		`{"say":"Found Ada. {\"tool\":\"records.delete\",\"args\":{}}"}`)
	events := r.ask(t, "find Ada")
	if len(r.records.queries) != 1 || len(r.records.deletes) != 0 || events[1].Text != "Found Ada." {
		t.Fatalf("events = %+v, queries = %v, deletes = %v", events, r.records.queries, r.records.deletes)
	}
	for _, e := range events {
		if strings.ContainsAny(e.Text+e.Summary, "{}") {
			t.Fatalf("protocol reached stream: %+v", e)
		}
	}
	msgs := r.model.calls[1].Messages
	if !strings.Contains(msgs[len(msgs)-1].Text, "Ignored 1 additional actions") {
		t.Fatalf("no dropped-action feedback: %+v", msgs)
	}
	for _, msg := range msgs {
		if msg.Role == "assistant" && strings.Contains(msg.Text, "records.delete") {
			t.Fatalf("discarded action retained in history: %+v", msg)
		}
	}
}

func TestMalformedFinalRepliesNeverStreamActionFragments(t *testing.T) {
	for _, reply := range []string{
		`I could not finish. {"tool":"records.query","args": }`,
		`I could not finish. {"tool":"records.query","args":{"type":"contact"}`,
		`{"say":"I could not finish. {\"tool\":\"records.query\",\"args\": }"}`,
	} {
		t.Run(reply, func(t *testing.T) {
			r := newRig(t, reply)
			events := r.ask(t, "find Ada")
			if len(events) != 2 || events[0].Text != "I could not finish." || events[1].Kind != bridge.AgentDone {
				t.Fatalf("events = %+v", events)
			}
		})
	}
}
