package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// COMMENTS ON A PASSAGE: every method is one request on its own route, the
// body it was given and nothing more, the anchor passed on exactly as the desk
// selected it, and the server's thread passed back whole. The hello says so.
func TestOrgPageCommentsForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	if !strings.Contains(wire.hello, `"org_workstream_page_comments"`) {
		t.Fatalf("hello lacks org_workstream_page_comments: %s", wire.hello)
	}
	thread := map[string]any{"id": "pc_1", "ws": "ws_1", "page_id": "pg_1", "block": []any{2}, "block_text": "We ship the café build on Friday.",
		"offset": 12, "quote": "café build", "comments": []any{map[string]any{"id": "pc_1", "by": map[string]any{"kind": "person", "id": "santosh", "name": "santosh"}, "text": "Which build?", "at": "2026-11-17T10:00:00Z"}}}
	o.answer = func(method, uri string) (int, any) {
		switch {
		case strings.HasSuffix(uri, "/for_you"), strings.Contains(uri, "/comments") && method == "GET":
			return 200, map[string]any{}
		case strings.HasSuffix(uri, "/pages/pg_1/comments"), strings.HasSuffix(uri, "/reply"):
			return 201, thread
		case strings.Contains(uri, "/comments/"):
			return 200, thread
		}
		return 0, nil
	}
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id, "ws": "ws_1"}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	last := func() (string, string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		sent := ""
		if o.lastBody != nil {
			raw, _ := json.Marshal(o.lastBody)
			sent = string(raw)
		}
		return o.asks[len(o.asks)-1], sent
	}
	for _, tc := range []struct {
		method string
		params map[string]any
		ask    string
		body   string
	}{
		{"factory.org_workstream_page_comments", map[string]any{"page": "pg_1"}, "GET /v1/workstreams/ws_1/pages/pg_1/comments", ``},
		{"factory.org_workstream_page_comments", map[string]any{"page": "pg_1", "resolved": true}, "GET /v1/workstreams/ws_1/pages/pg_1/comments?resolved=1", ``},
		{"factory.org_workstream_page_comment", map[string]any{"page": "pg_1", "block": []any{2}, "block_text": "We ship the café build on Friday.", "offset": 12, "quote": "café build", "text": " Which build? "},
			"POST /v1/workstreams/ws_1/pages/pg_1/comments", `{"block":[2],"block_text":"We ship the café build on Friday.","offset":12,"quote":"café build","text":"Which build?"}`},
		{"factory.org_workstream_page_comment_reply", map[string]any{"thread": "pc_1", "text": "The second one."}, "POST /v1/workstreams/ws_1/comments/pc_1/reply", `{"text":"The second one."}`},
		{"factory.org_workstream_page_comment_resolve", map[string]any{"thread": "pc_1"}, "POST /v1/workstreams/ws_1/comments/pc_1/resolve", `{"reopen":false}`},
		{"factory.org_workstream_page_comment_resolve", map[string]any{"thread": "pc_1", "reopen": true}, "POST /v1/workstreams/ws_1/comments/pc_1/resolve", `{"reopen":true}`},
		{"factory.org_workstream_page_comment_seen", map[string]any{"thread": "pc_1"}, "POST /v1/workstreams/ws_1/comments/pc_1/seen", `{}`},
		{"factory.org_workstream_page_comments_for_you", nil, "GET /v1/workstreams/ws_1/comments/for_you", ``},
	} {
		o.since()
		var out json.RawMessage
		wire.must(tc.method, c(tc.params), &out)
		ask, body := last()
		if ask != tc.ask || body != tc.body {
			t.Fatalf("%s sent %q body %s; want %q %s", tc.method, ask, body, tc.ask, tc.body)
		}
	}

	var list appwire.OrgPageComments
	wire.must("factory.org_workstream_page_comments", c(map[string]any{"page": "pg_1"}), &list)
	if list.Threads == nil {
		t.Fatal("an absent list came back null")
	}
	var one map[string]any
	wire.must("factory.org_workstream_page_comment_seen", c(map[string]any{"thread": "pc_1"}), &one)
	if one["quote"] != "café build" || one["block_text"] == nil || one["comments"] == nil {
		t.Fatalf("the thread lost its shape: %v", one)
	}

	// Refused before anything is sent.
	o.since()
	for _, bad := range []map[string]any{
		{"page": "pg_1", "block": []any{2}, "block_text": "x", "offset": 0, "quote": "x", "text": "  "},
		{"page": "pg_1", "block": []any{}, "block_text": "x", "offset": 0, "quote": "x", "text": "hi"},
		{"page": "pg_1", "block": []any{0}, "block_text": "x", "offset": 0, "quote": " ", "text": "hi"},
		{"page": "../x", "block": []any{0}, "block_text": "x", "offset": 0, "quote": "x", "text": "hi"},
	} {
		wire.mustFail("factory.org_workstream_page_comment", c(bad), appwire.CodeBadParams)
	}
	wire.mustFail("factory.org_workstream_page_comment_reply", c(map[string]any{"thread": "pc_1", "text": ""}), appwire.CodeBadParams)
	wire.mustFail("factory.org_workstream_page_comment_seen", c(map[string]any{"thread": ""}), appwire.CodeBadParams)
	if asks := o.since(); len(asks) != 0 {
		t.Fatalf("a refused call still reached the server: %v", asks)
	}
}
