package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WP-6d Tool, (a), (h): exact answers travel as normal tool refusals.
func TestWorkstreamSuggestionToolAnswers(t *testing.T) {
	const args = `{"page_id":"page","base_version":3,"why":"Reason","edits":[{"find":"old","replace":"new","why":"Clearer"}]}`
	for _, test := range []struct {
		err  error
		want string
	}{
		{nil, "Suggested 1 edit(s) on Notes (sg_1). The person keeps or drops each one; the page is unchanged until then."},
		{workstream.ErrNotPersonPage, "That page is yours; change it with write_agent_page. Suggest edits only on the person's pages."},
		{ErrWorkstreamVersion, "The page changed while you were writing. Read it again before writing."},
		{ErrWorkstreamDone, "This workstream is done; nothing was added or written."},
		{errors.New("This workstream holds too many suggestions; nothing was suggested."), "This workstream holds too many suggestions; nothing was suggested."},
		{&workstream.SuggestionTextError{Message: "Edit 1: that text is not on the page. Quote plain text from one paragraph, heading, list item or code block exactly as it reads, without markdown marks."}, "Edit 1: that text is not on the page. Quote plain text from one paragraph, heading, list item or code block exactly as it reads, without markdown marks."},
		{&workstream.SuggestionTextError{Message: "Edit 1: that text appears more than once. Quote more of it so it is unique."}, "Edit 1: that text appears more than once. Quote more of it so it is unique."},
		{&workstream.SuggestionTextError{Message: "Edit 1: replace is the same as find."}, "Edit 1: replace is the same as find."},
		{&workstream.SuggestionTextError{Message: "Edit 1: only a code block's text may contain a line break."}, "Edit 1: only a code block's text may contain a line break."},
		{&workstream.SuggestionTextError{Message: "Edits 1 and 2 touch the same text. Merge them into one edit."}, "Edits 1 and 2 touch the same text. Merge them into one edit."},
	} {
		door := &workstreamFakeDoor{suggestionError: test.err}
		ctx, mark := withToolRejection(context.Background())
		got, refused, err := workstreamToolForTest(t, door, "suggest_page_edit").Execute(ctx, json.RawMessage(args))
		if err != nil || refused != (test.err != nil) || got != test.want || mark.RejectedBeforeRun() || len(door.suggestionAsks) != 1 {
			t.Fatalf("answer: %q refused=%v %v", got, refused, err)
		}
	}
}

// WP-6d Tool: strict nested schema, nulls, required fields, integer and byte
// bounds are refused before any door. Boundary values are accepted unchanged.
func TestWorkstreamSuggestionSchemaBeforeDoor(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{"page_id": "page", "base_version": 1, "why": "Reason", "edits": []any{map[string]any{"find": "old", "replace": "new", "why": "Clearer"}}}
	}
	for _, mutate := range []func(map[string]any){
		func(p map[string]any) { p["extra"] = true },
		func(p map[string]any) { delete(p, "page_id") },
		func(p map[string]any) { p["page_id"] = "" },
		func(p map[string]any) { p["page_id"] = nil },
		func(p map[string]any) { delete(p, "base_version") },
		func(p map[string]any) { p["base_version"] = 0 },
		func(p map[string]any) { p["base_version"] = -1 },
		func(p map[string]any) { p["base_version"] = 1.5 },
		func(p map[string]any) { p["base_version"] = "1" },
		func(p map[string]any) { p["base_version"] = nil },
		func(p map[string]any) { p["why"] = "" },
		func(p map[string]any) { p["why"] = strings.Repeat("é", 101) },
		func(p map[string]any) { p["why"] = "two\nlines" },
		func(p map[string]any) { p["why"] = nil },
		func(p map[string]any) { p["edits"] = nil },
		func(p map[string]any) { p["edits"] = []any{} },
		func(p map[string]any) { p["edits"] = []any{nil} },
		func(p map[string]any) {
			p["edits"] = []any{map[string]any{"find": "old", "replace": "new", "why": "Why", "source": true}}
		},
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["find"] = "" },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["find"] = strings.Repeat("é", 2001) },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["replace"] = strings.Repeat("é", 2001) },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["replace"] = nil },
		func(p map[string]any) { delete(p["edits"].([]any)[0].(map[string]any), "replace") },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["why"] = "" },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["why"] = strings.Repeat("x", 201) },
		func(p map[string]any) { p["edits"].([]any)[0].(map[string]any)["why"] = "two\nlines" },
		func(p map[string]any) {
			one := p["edits"].([]any)[0]
			p["edits"] = make([]any, 51)
			for i := range p["edits"].([]any) {
				p["edits"].([]any)[i] = one
			}
		},
	} {
		params := valid()
		mutate(params)
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		door := &workstreamFakeDoor{}
		ctx, mark := withToolRejection(context.Background())
		text, refused, err := workstreamToolForTest(t, door, "suggest_page_edit").Execute(ctx, raw)
		if err != nil || !refused || !mark.RejectedBeforeRun() || door.otherCalls != 0 {
			t.Fatalf("invalid params reached door: %s; %q %v", raw, text, err)
		}
	}
	params := valid()
	params["why"] = strings.Repeat("x", 200)
	params["edits"] = make([]any, 50)
	for i := range params["edits"].([]any) {
		params["edits"].([]any)[i] = map[string]any{"find": strings.Repeat("é", 2000), "replace": "", "why": strings.Repeat("x", 200)}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	door := &workstreamFakeDoor{}
	workstreamCallForTest(t, door, "suggest_page_edit", string(raw), false)
	if len(door.suggestionAsks) != 1 || len(door.suggestionAsks[0].Edits) != 50 {
		t.Fatal("boundary refused")
	}
	var schema map[string]any
	if err := json.Unmarshal(workstreamToolForTest(t, door, "suggest_page_edit").Schema, &schema); err != nil {
		t.Fatal(err)
	}
	edits := schema["properties"].(map[string]any)["edits"].(map[string]any)
	if schema["additionalProperties"] != false || edits["items"].(map[string]any)["additionalProperties"] != false {
		t.Fatal("schema permits invented properties")
	}
}

// WP-6d Tool: suggestions belong to the same create family as proposals.
func TestWorkstreamSuggestionActionCategory(t *testing.T) {
	if category := ActionCategoryForTool("suggest_page_edit"); category != ActionCreate {
		t.Fatalf("category: %q", category)
	}
}
