package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A WORKSTREAM'S FILES: every method is one request on its own route, with
// the file's rev as If-Match, the body it was given and nothing more, and the
// server's answer in its shape. The hello says so. The methods are carried by
// the one org carrier (appfactory_org.go); this half adds no carrier of its
// own, and this test is what proves the files ride it.
func TestOrgWorkstreamFilesForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	if !strings.Contains(wire.hello, `"org_workstream_files"`) {
		t.Fatalf("hello lacks org_workstream_files: %s", wire.hello)
	}
	file := map[string]any{"id": "fl_1", "ws": "ws_1", "rev": 2, "kind": "document", "name": "Brief", "page_id": "pg_1",
		"made_by": map[string]any{"kind": "afi", "name": "Afi", "for": "abir"}, "changed_at": "2026-11-16T10:00:00Z"}
	o.answer = func(method, uri string) (int, any) {
		switch {
		case strings.HasSuffix(uri, "/files/move"):
			return 200, map[string]any{"files": []any{file}}
		case method == "GET" && strings.Contains(uri, "/files"):
			return 200, map[string]any{"files": []any{file}, "from": map[string]any{"id": "ws_0", "name": "Pantry"}}
		case strings.Contains(uri, "/files"):
			return 200, file
		}
		return 0, nil
	}
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id, "id": "ws_1"}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	last := func() (string, string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.asks[len(o.asks)-1], o.ifMatch[len(o.ifMatch)-1]
	}
	for _, tc := range []struct {
		method  string
		params  map[string]any
		ask     string
		ifMatch string
		body    string
	}{
		{"factory.org_workstream_files", nil, "GET /v1/workstreams/ws_1/files", "", ``},
		{"factory.org_workstream_files", map[string]any{"query": "no photo", "deleted": true}, "GET /v1/workstreams/ws_1/files?deleted=1&q=no+photo", "", ``},
		{"factory.org_workstream_file_create", map[string]any{"kind": "folder", "name": " Research "}, "POST /v1/workstreams/ws_1/files", "", `{"kind":"folder","name":"Research"}`},
		{"factory.org_workstream_file_create", map[string]any{"kind": "upload", "name": "a.txt", "data": "aGk=", "mime": "text/plain"}, "POST /v1/workstreams/ws_1/files", "", `{"data":"aGk=","kind":"upload","mime":"text/plain","name":"a.txt"}`},
		{"factory.org_workstream_file_update", map[string]any{"file": "fl_1", "rev": 2, "name": "Brief 2", "edited": "person"}, "PATCH /v1/workstreams/ws_1/files/fl_1", "2", `{"edited":"person","name":"Brief 2"}`},
		{"factory.org_workstream_file_move", map[string]any{"files": []string{"fl_1"}, "folder": "fl_9"}, "POST /v1/workstreams/ws_1/files/move", "", `{"files":["fl_1"],"folder":"fl_9"}`},
		{"factory.org_workstream_file_delete", map[string]any{"file": "fl_1"}, "DELETE /v1/workstreams/ws_1/files/fl_1", "", ``},
		{"factory.org_workstream_file_restore", map[string]any{"file": "fl_1"}, "POST /v1/workstreams/ws_1/files/fl_1/restore", "", ``},
	} {
		var out json.RawMessage
		wire.must(tc.method, c(tc.params), &out)
		ask, ifMatch := last()
		if ask != tc.ask || ifMatch != tc.ifMatch {
			t.Fatalf("%s sent %q If-Match %q; want %q %q", tc.method, ask, ifMatch, tc.ask, tc.ifMatch)
		}
		o.mu.Lock()
		sent := ""
		if o.lastBody != nil {
			raw, _ := json.Marshal(o.lastBody)
			sent = string(raw)
		}
		o.mu.Unlock()
		if sent != tc.body {
			t.Fatalf("%s sent body %s; want %s", tc.method, sent, tc.body)
		}
	}

	var list appwire.OrgWorkstreamFiles
	wire.must("factory.org_workstream_files", c(nil), &list)
	if len(list.Files) != 1 || list.Files[0].PageID != "pg_1" || list.Files[0].MadeBy.For != "abir" || list.From == nil || list.From.Files == nil {
		t.Fatalf("files came back as %+v", list)
	}

	o.since()
	for _, bad := range []struct {
		method string
		params map[string]any
	}{
		{"factory.org_workstream_file_update", map[string]any{"file": "fl_1"}},
		{"factory.org_workstream_file_update", map[string]any{"file": "fl_1", "rev": 1, "edited": "robot"}},
		{"factory.org_workstream_file_delete", map[string]any{"file": "../x"}},
		{"factory.org_workstream_file_move", map[string]any{"files": []string{}}},
		{"factory.org_workstream_file_create", map[string]any{"name": "x"}},
	} {
		wire.mustFail(bad.method, c(bad.params), appwire.CodeBadParams)
	}
	if asks := o.since(); len(asks) != 0 {
		t.Fatalf("a refused call still reached the server: %v", asks)
	}
}
