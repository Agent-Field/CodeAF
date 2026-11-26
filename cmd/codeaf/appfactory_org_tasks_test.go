package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A WORKSTREAM'S TASKS: every method is one request on its own route, with the
// rev it was given as If-Match, the body it was given and nothing more (a
// change's null that clears reaches the server as a null), and the server's
// answer passed back whole. The hello says so.
func TestOrgWorkstreamTasksForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	if !strings.Contains(wire.hello, `"org_workstream_tasks"`) || !strings.Contains(wire.hello, `"org_workstream_requests"`) {
		t.Fatalf("hello lacks org_workstream_tasks or org_workstream_requests: %s", wire.hello)
	}
	if !strings.Contains(wire.hello, `"org_workstream_task_start"`) {
		t.Fatalf("hello lacks org_workstream_task_start: %s", wire.hello)
	}
	task := map[string]any{"id": "tk_1", "ws": "ws_1", "rev": 2, "title": "Draft the March release notes", "state": "needs",
		"owner": map[string]any{"kind": "person", "id": "santosh", "name": "santosh"}, "done_by": map[string]any{"kind": "afi"},
		"ask": map[string]any{"id": "a1", "title": "Publish the notes to the website?", "options": []any{}}}
	event := map[string]any{"id": "te_1", "task": "tk_1", "kind": "comment", "text": "hi"}
	o.answer = func(method, uri string) (int, any) {
		switch {
		case strings.HasSuffix(uri, "/events"):
			return 200, map[string]any{}
		case strings.HasSuffix(uri, "/comment"):
			return 201, event
		case strings.HasSuffix(uri, "/requestable"):
			return 200, map[string]any{"workstreams": []any{}}
		case strings.HasSuffix(uri, "/requests"):
			return 201, map[string]any{"id": "tk_2", "ws": "ws_1", "ws_name": "Engineering", "title": "Fix the share link"}
		case strings.HasSuffix(uri, "/start_all"), strings.HasSuffix(uri, "/tasks") && method == "GET":
			return 200, map[string]any{"tasks": []any{task}}
		case strings.Contains(uri, "/tasks"):
			return 200, task
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
	last := func() (string, string, string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		sent := ""
		if o.lastBody != nil {
			raw, _ := json.Marshal(o.lastBody)
			sent = string(raw)
		}
		return o.asks[len(o.asks)-1], o.ifMatch[len(o.ifMatch)-1], sent
	}
	for _, tc := range []struct {
		method  string
		params  map[string]any
		ask     string
		ifMatch string
		body    string
	}{
		{"factory.org_workstream_tasks", nil, "GET /v1/workstreams/ws_1/tasks", "", ``},
		{"factory.org_workstream_task_delete", map[string]any{"id": "tk_1", "rev": 2}, "DELETE /v1/workstreams/ws_1/tasks/tk_1", "2", ``},
		{"factory.org_workstream_task_restore", map[string]any{"id": "tk_1"}, "POST /v1/workstreams/ws_1/tasks/tk_1/restore", "", ``},
		{"factory.org_workstream_task", map[string]any{"id": "job:7"}, "GET /v1/workstreams/ws_1/tasks/job:7", "", ``},
		{"factory.org_workstream_task_create", map[string]any{"title": " Draft notes ", "done_by": map[string]any{"kind": "afi"}},
			"POST /v1/workstreams/ws_1/tasks", "", `{"done_by":{"kind":"afi"},"title":"Draft notes"}`},
		// words_by, the desk's word that Afi wrote the words, reaches the
		// server as sent (it only ever lowers trust there).
		{"factory.org_workstream_task_create", map[string]any{"title": "Look it up", "done_by": map[string]any{"kind": "afi"}, "words_by": "afi"},
			"POST /v1/workstreams/ws_1/tasks", "", `{"done_by":{"kind":"afi"},"title":"Look it up","words_by":"afi"}`},
		{"factory.org_workstream_task_update", map[string]any{"id": "tk_1", "rev": 2, "request": "New words", "words_by": "afi"},
			"PATCH /v1/workstreams/ws_1/tasks/tk_1", "2", `{"request":"New words","words_by":"afi"}`},
		{"factory.org_workstream_task_update", map[string]any{"id": "tk_1", "rev": 2, "state": "done", "group": nil},
			"PATCH /v1/workstreams/ws_1/tasks/tk_1", "2", `{"group":null,"state":"done"}`},
		{"factory.org_workstream_task_update", map[string]any{"id": "job:7", "rev": 0, "owner": map[string]any{"kind": "everyone"}},
			"PATCH /v1/workstreams/ws_1/tasks/job:7", "0", `{"owner":{"kind":"everyone"}}`},
		{"factory.org_workstream_task_answer", map[string]any{"id": "tk_1", "option": "publish"}, "POST /v1/workstreams/ws_1/tasks/tk_1/answer", "", `{"option":"publish","text":""}`},
		{"factory.org_workstream_task_comment", map[string]any{"id": "tk_1", "text": "Mention the import fix."}, "POST /v1/workstreams/ws_1/tasks/tk_1/comment", "", `{"text":"Mention the import fix."}`},
		{"factory.org_workstream_task_comment", map[string]any{"id": "tk_1", "offer": map[string]any{"event": "te_2", "state": "taken"}},
			"POST /v1/workstreams/ws_1/tasks/tk_1/comment", "", `{"offer":{"event":"te_2","state":"taken"}}`},
		{"factory.org_workstream_task_events", map[string]any{"id": "tk_1"}, "GET /v1/workstreams/ws_1/tasks/tk_1/events", "", ``},
		{"factory.org_workstream_task_events", map[string]any{"id": "tk_1", "seen": true}, "GET /v1/workstreams/ws_1/tasks/tk_1/events?seen=1", "", ``},
		{"factory.org_workstreams_requestable", nil, "GET /v1/workstreams/requestable", "", ``},
		{"factory.org_workstream_request", map[string]any{"title": " Fix the share link ", "from_ws": "ws_2", "from_task": "tk_9"},
			"POST /v1/workstreams/ws_1/requests", "", `{"from_task":"tk_9","from_ws":"ws_2","request":"","title":"Fix the share link"}`},
		{"factory.org_workstream_request", map[string]any{"title": "Fix it", "words_by": "afi"},
			"POST /v1/workstreams/ws_1/requests", "", `{"from_task":"","from_ws":"","request":"","title":"Fix it","words_by":"afi"}`},
		{"factory.org_workstream_tasks_start_all", map[string]any{"group": "grp_batch-release-2.4"}, "POST /v1/workstreams/ws_1/tasks/start_all", "", `{"group":"grp_batch-release-2.4"}`},
		{"factory.org_workstream_tasks_start_all", map[string]any{"group": "grp_batch-release-2.4", "ids": []string{}}, "POST /v1/workstreams/ws_1/tasks/start_all", "", `{"group":"grp_batch-release-2.4","ids":[]}`},
		{"factory.org_workstream_tasks_start_all", map[string]any{"group": "grp_batch-release-2.4", "ids": []string{"tk_1"}}, "POST /v1/workstreams/ws_1/tasks/start_all", "", `{"group":"grp_batch-release-2.4","ids":["tk_1"]}`},
	} {
		o.since()
		var out json.RawMessage
		wire.must(tc.method, c(tc.params), &out)
		ask, ifMatch, body := last()
		if ask != tc.ask || ifMatch != tc.ifMatch || body != tc.body {
			t.Fatalf("%s sent %q If-Match %q body %s; want %q %q %s", tc.method, ask, ifMatch, body, tc.ask, tc.ifMatch, tc.body)
		}
	}

	var list appwire.OrgTasks
	wire.must("factory.org_workstream_tasks", c(nil), &list)
	var one map[string]any
	if err := json.Unmarshal(list.Tasks[0], &one); err != nil || one["ask"] == nil || one["owner"].(map[string]any)["id"] != "santosh" {
		t.Fatalf("the task lost its shape: %v %v", one, err)
	}
	var events appwire.OrgTaskEvents
	wire.must("factory.org_workstream_task_events", c(map[string]any{"id": "tk_1"}), &events)
	if events.Events == nil {
		t.Fatal("an absent thread came back null")
	}

	// Refused before anything is sent.
	o.since()
	for _, bad := range []struct {
		method string
		params map[string]any
	}{
		{"factory.org_workstream_task_update", map[string]any{"id": "tk_1", "rev": 2}},
		{"factory.org_workstream_task_update", map[string]any{"id": "../x", "rev": 1, "state": "done"}},
		{"factory.org_workstream_task_create", map[string]any{"title": "", "done_by": map[string]any{"kind": "afi"}}},
		{"factory.org_workstream_task_create", map[string]any{"title": "No doer"}},
		{"factory.org_workstream_task_answer", map[string]any{"id": "tk_1"}},
		{"factory.org_workstream_task_comment", map[string]any{"id": "tk_1", "text": "  "}},
		{"factory.org_workstream_tasks_start_all", map[string]any{"group": ""}},
	} {
		wire.mustFail(bad.method, c(bad.params), appwire.CodeBadParams)
	}
	if asks := o.since(); len(asks) != 0 {
		t.Fatalf("a refused call still reached the server: %v", asks)
	}
}
