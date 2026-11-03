package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func workstreamSourcesFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage-pricing.md")
	if err := os.WriteFile(path, []byte("Recorded usage.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func workstreamSourceWireCall(t *testing.T, connection *workstreamWireConnection, method string, params any) map[string]any {
	t.Helper()
	return workstreamWireResult(t, connection.call(t, method, params)).(map[string]any)
}

// WP-12 (a), (d), (f): all target forms round-trip through actual typed wire
// dispatch, with exact kind keys, derived titles, order and duplicate receipts.
func TestAppWorkstreamSourcesEveryKindOnWire(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Sources wire", workstreamJobRef("7"))
	pages, err := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	record, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "decision", Text: "Use recorded totals.\nDetails.", Kept: workstreamPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	path := workstreamSourcesFile(t)
	folder := t.TempDir()
	cases := []struct {
		fields map[string]any
		title  string
	}{
		{map[string]any{"kind": "file", "path": path}, "usage-pricing.md"},
		{map[string]any{"kind": "repo", "repo": "ACME/Billing"}, "ACME/Billing"},
		{map[string]any{"kind": "repo", "repo": folder}, filepath.Base(folder)},
		{map[string]any{"kind": "issue", "repo": "acme/billing", "number": 412}, "acme/billing#412"},
		{map[string]any{"kind": "link", "url": "HTTPS://Docs.Example.com/x/?q=1#part"}, "docs.example.com/x"},
		{map[string]any{"kind": "kept", "ref": map[string]any{"kind": "record", "id": record.Record.ID}}, "Use recorded totals."},
		{map[string]any{"kind": "kept", "ref": map[string]any{"kind": "page", "id": pages.Pages[0].ID}}, "Overview"},
		{map[string]any{"kind": "kept", "ref": map[string]any{"kind": "message", "id": "m", "chat_id": "chat"}}, "Kept message"},
		{map[string]any{"kind": "kept", "ref": map[string]any{"kind": "factory_job", "id": "7", "factory": "f"}}, "Job 7"},
	}
	membersBefore, err := b.collections.Members(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	want := []any{}
	for i, tc := range cases {
		params := map[string]any{"workstream_id": w.ID}
		for key, value := range tc.fields {
			params[key] = value
		}
		before := len(workstreamEvents(rec))
		result := workstreamSourceWireCall(t, connection, "source.add", params)
		item := result["source"].(map[string]any)
		keys := []string{"id", "workstream_id", "kind", "title", "read_by_jobs", "proposed", "added_by", "added_at"}
		for key := range tc.fields {
			if key != "kind" {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		if result["already"] != false || item["title"] != tc.title || item["added_by"] != "you" || item["read_by_jobs"] != false || item["proposed"] != false || !reflect.DeepEqual(workstreamWireKeys(item), keys) {
			t.Fatalf("kind %d: %+v", i, result)
		}
		for key, value := range tc.fields {
			if key == "url" {
				value = "https://docs.example.com/x/?q=1#part"
			}
			if !reflect.DeepEqual(item[key], workstreamWireDecode(t, workstreamWireEncode(t, value))) {
				t.Fatalf("kind %d field %s: %v", i, key, item)
			}
		}
		if i == 1 {
			params["repo"] = "acme/billing"
		}
		if i == 3 {
			params["repo"] = "ACME/Billing"
		}
		if i == 4 {
			params["url"] = "https://docs.example.com/x/?q=1#part"
		}
		again := workstreamSourceWireCall(t, connection, "source.add", params)
		if again["already"] != true || !reflect.DeepEqual(again["source"], item) || len(workstreamEvents(rec)) != before+1 {
			t.Fatalf("duplicate kind %d: %+v", i, again)
		}
		want = append(want, item)
	}
	listed := workstreamSourceWireCall(t, connection, "source.list", map[string]any{"workstream_id": w.ID})["sources"]
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("source list order or fields: %v", listed)
	}
	membersAfter, err := b.collections.Members(context.Background(), w.ID)
	if err != nil || !reflect.DeepEqual(membersAfter, membersBefore) {
		t.Fatalf("sources changed members: %+v %v", membersAfter, err)
	}
	for _, index := range []int{0, 5, 6} {
		item := want[index].(map[string]any)
		removed := workstreamSourceWireCall(t, connection, "source.remove", map[string]any{"id": item["id"]})["source"]
		if !reflect.DeepEqual(item, removed) {
			t.Fatalf("remove changed receipt: %v", removed)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "Recorded usage.\n" {
		t.Fatalf("removed source changed file: %q %v", data, err)
	}
	if _, err := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: pages.Pages[0].ID}); err != nil {
		t.Fatal(err)
	}
	if records, err := b.WorkstreamRecordList(appwire.WorkstreamRecordListParams{WorkstreamID: w.ID}); err != nil || len(records.Records) != 1 {
		t.Fatalf("source removal changed records: %v %v", records, err)
	}
}

// WP-12 (b): invalid paths, URLs, kind fields, refs and updates return
// bad_params/not_found without rows, timestamps or changed events.
func TestAppWorkstreamSourcesRefusalsOnWire(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Invalid source wire")
	path := workstreamSourcesFile(t)
	bad := []map[string]any{}
	for _, p := range []string{"relative", filepath.Dir(path) + "/../file", filepath.Dir(path) + "/./usage-pricing.md", filepath.Dir(path) + "//usage-pricing.md", path + "/", path + "\x00", path + "\n", filepath.Join(t.TempDir(), "missing"), "/" + strings.Repeat("x", 4096)} {
		bad = append(bad, map[string]any{"kind": "file", "path": p})
	}
	for _, repo := range []string{"../x", "owner/..", "-owner/x", "owner/name/extra", "relative", path} {
		bad = append(bad, map[string]any{"kind": "repo", "repo": repo})
	}
	for _, number := range []int{0, -1, 2147483648} {
		bad = append(bad, map[string]any{"kind": "issue", "repo": "owner/name", "number": number})
	}
	bad = append(bad, map[string]any{"kind": "issue", "repo": filepath.Dir(path), "number": 1})
	for _, url := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://h/x", "//h/x", "https:///x", "https://user:pw@h/x", "https://h/x y", "https://h/x\n", "https://h/" + strings.Repeat("x", 2039)} {
		bad = append(bad, map[string]any{"kind": "link", "url": url})
	}
	bad = append(bad, map[string]any{"kind": "file", "path": path, "url": "https://example.com"})
	for _, title := range []string{"", strings.Repeat("x", 201), "two\nlines", " untrimmed "} {
		bad = append(bad, map[string]any{"kind": "file", "path": path, "title": title})
	}
	bad = append(bad, map[string]any{"kind": "kept", "ref": map[string]any{"kind": "factory_job", "id": "7", "factory": "f"}}, map[string]any{"kind": "kept", "ref": map[string]any{"kind": "message", "id": "m", "chat_id": "c", "factory": ""}})
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	before := len(workstreamEvents(rec))
	for i, params := range bad {
		params["workstream_id"] = w.ID
		frame := connection.call(t, "source.add", params)
		if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" {
			t.Fatalf("invalid %d: %v", i, frame)
		}
	}
	for _, kind := range []string{"record", "page"} {
		frame := connection.call(t, "source.add", map[string]any{"workstream_id": w.ID, "kind": "kept", "ref": map[string]any{"kind": kind, "id": "missing"}})
		if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "not_found" {
			t.Fatalf("unknown kept: %v", frame)
		}
	}
	for _, patch := range []map[string]any{{"id": "missing"}, {"id": "missing", "keep": false}, {"id": "missing", "title": ""}} {
		frame := connection.call(t, "source.update", patch)
		if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" {
			t.Fatalf("invalid update: %v", frame)
		}
	}
	listed := workstreamSourceWireCall(t, connection, "source.list", map[string]any{"workstream_id": w.ID})["sources"].([]any)
	after, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || len(listed) != 0 || len(workstreamEvents(rec)) != before || !after.Workstream.UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatalf("refusal mutated state: %v %v", listed, err)
	}
}

// WP-12 Fix 1 Major 3 (b): literal invalid UTF-8 must be refused before JSON
// can repair source strings, with no source, workstream, agent or event added.
func TestAppWorkstreamMajor3InvalidUTF8BOnWire(t *testing.T) {
	b, rec, calls, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "UTF-8 source wire")
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	seed := workstreamSourceWireCall(t, connection, "source.add", map[string]any{"workstream_id": w.ID, "kind": "link", "url": "https://example.com", "title": "Original title"})["source"].(map[string]any)
	path := filepath.Join(t.TempDir(), "source-�.txt")
	if err := os.WriteFile(path, []byte("Replacement-character file exists."), 0600); err != nil {
		t.Fatal(err)
	}
	listParams := map[string]any{"workstream_id": w.ID}
	before := workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"]
	rowBefore, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil {
		t.Fatal(err)
	}
	events := len(workstreamEvents(rec))
	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"path", map[string]any{"kind": "file", "path": path}},
		{"url", map[string]any{"kind": "link", "url": "https://example.com/�"}},
		{"title", map[string]any{"kind": "link", "url": "https://example.com/title", "title": "Title �"}},
	} {
		for _, method := range []string{"source.add", "workstream.create"} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				params := map[string]any{"name": "Invalid UTF-8 " + tc.name, "sources": []any{tc.fields}}
				if method == "source.add" {
					params = map[string]any{"workstream_id": w.ID}
					for key, value := range tc.fields {
						params[key] = value
					}
				}
				raw := workstreamWireEncode(t, map[string]any{"id": connection.Next + 1, "method": method, "params": params})
				raw = bytes.ReplaceAll(raw, []byte("�"), []byte{0xff})
				if !bytes.Contains(raw, []byte{0xff}) {
					t.Fatal("fixture must contain a literal invalid UTF-8 byte")
				}
				frame := connection.callRaw(t, raw)
				if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" {
					t.Fatalf("invalid raw UTF-8 accepted: %v", frame)
				}
			})
		}
	}
	raw := workstreamWireEncode(t, map[string]any{"id": connection.Next + 1, "method": "source.update", "params": map[string]any{"id": seed["id"], "title": "Changed �"}})
	frame := connection.callRaw(t, bytes.ReplaceAll(raw, []byte("�"), []byte{0xff}))
	if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" {
		t.Fatalf("invalid update accepted: %v", frame)
	}
	after := workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"]
	rowAfter, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || !reflect.DeepEqual(after, before) || !rowAfter.Workstream.UpdatedAt.Equal(rowBefore.Workstream.UpdatedAt) || *calls != 1 || len(workstreamEvents(rec)) != events {
		t.Fatalf("invalid UTF-8 changed state: sources=%v agents=%d err=%v", after, *calls, err)
	}
	if list, err := b.WorkstreamList(appwire.WorkstreamListParams{}); err != nil || len(list.Workstreams) != 1 {
		t.Fatalf("invalid UTF-8 created workstreams: %+v %v", list, err)
	}
}

// WP-12 Fix 1 Major 3 (b): source validation preserves create's existing
// replacement decoding for name and why, and accepts literal valid U+FFFD.
func TestAppWorkstreamMajor3InvalidUTF8BLeavesCreateTextDecoding(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	raw := fmt.Appendf(nil, `{"id":%d,"method":"workstream.create","params":{"name":"Name `, connection.Next+1)
	raw = append(raw, 0xff)
	raw = append(raw, []byte(`","why":"Why `)...)
	raw = append(raw, 0xff)
	raw = append(raw, []byte(`","sources":[{"kind":"link","url":"https://example.com","title":"Valid �"}]}}`)...)
	created := workstreamWireResult(t, connection.callRaw(t, raw)).(map[string]any)["workstream"].(map[string]any)
	if created["name"] != "Name �" || created["why"] != "Why �" {
		t.Fatalf("create text decoding changed: %v", created)
	}
	sources := workstreamSourceWireCall(t, connection, "source.list", map[string]any{"workstream_id": created["id"]})["sources"].([]any)
	if len(sources) != 1 || sources[0].(map[string]any)["title"] != "Valid �" {
		t.Fatalf("valid replacement character refused: %v", sources)
	}
}

// WP-12 Fix 1 Minor 1 (a), (b): wire adds refuse overlong normalized URLs
// without writes/events, while a returned URL at the bound can be re-added.
func TestAppWorkstreamMinor1LinkBoundABOnWire(t *testing.T) {
	b, rec, _, _ := workstreamBackend(t)
	w := workstreamCreateForTest(t, b, "Normalized URL wire")
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	events := len(workstreamEvents(rec))
	params := map[string]any{"workstream_id": w.ID, "kind": "link", "url": "https://h/" + strings.Repeat("é", 1019)}
	frame := connection.call(t, "source.add", params)
	if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" {
		t.Fatalf("expanded URL accepted: %v", frame)
	}
	listParams := map[string]any{"workstream_id": w.ID}
	list := workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"].([]any)
	after, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || len(list) != 0 || len(workstreamEvents(rec)) != events || !after.Workstream.UpdatedAt.Equal(w.UpdatedAt) {
		t.Fatalf("invalid URL changed state: %v %v", list, err)
	}
	params["url"] = "https://h/" + strings.Repeat("é", 339) + "abcd"
	created := workstreamSourceWireCall(t, connection, "source.add", params)
	item := created["source"].(map[string]any)
	want := "https://h/" + strings.Repeat("%C3%A9", 339) + "abcd"
	if created["already"] != false || item["url"] != want || len(item["url"].(string)) != 2048 {
		t.Fatalf("fitting URL: %v", created)
	}
	params["url"] = item["url"]
	again := workstreamSourceWireCall(t, connection, "source.add", params)
	list = workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"].([]any)
	if again["already"] != true || !reflect.DeepEqual(again["source"], item) || len(list) != 1 || len(workstreamEvents(rec)) != events+1 {
		t.Fatalf("returned URL retry: %v sources=%v", again, list)
	}
}

// WP-12 (b), (e): create refuses every bad source before opening an agent;
// good sources are separate from why and duplicate/client retries add none.
func TestAppWorkstreamCreateSourcesPreflight(t *testing.T) {
	b, rec, calls, _ := workstreamBackend(t)
	path := workstreamSourcesFile(t)
	good := appwire.WorkstreamSourceAdd{Kind: "file", Path: &path, ReadByJobs: workstreamPtr(true)}
	for _, bad := range []appwire.WorkstreamSourceAdd{{Kind: "file", Path: workstreamPtr("relative")}, {Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "record", ID: "missing"}}} {
		_, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Free name", Sources: []appwire.WorkstreamSourceAdd{good, bad}})
		workstreamCheckError(t, err, appwire.CodeBadParams)
	}
	if *calls != 0 || len(workstreamEvents(rec)) != 0 {
		t.Fatal("refused source created an agent or event")
	}
	if list, err := b.WorkstreamList(appwire.WorkstreamListParams{}); err != nil || len(list.Workstreams) != 0 {
		t.Fatalf("refused create left rows: %+v %v", list, err)
	}
	why := "Exactly sent.\n"
	p := appwire.WorkstreamCreateParams{Name: "Free name", Why: &why, ClientID: workstreamPtr("source-retry"), Sources: []appwire.WorkstreamSourceAdd{{Kind: "repo", Repo: workstreamPtr("acme/billing")}, good, good}}
	created, err := b.WorkstreamCreate(p)
	if err != nil || created.Workstream.Why != why || *calls != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}
	p.Sources = []appwire.WorkstreamSourceAdd{{Kind: "link", URL: workstreamPtr("https://example.com/new")}}
	if again, err := b.WorkstreamCreate(p); err != nil || again.Workstream.ID != created.Workstream.ID || *calls != 1 {
		t.Fatalf("retry: %+v %v", again, err)
	}
	list, err := b.WorkstreamSourceList(appwire.WorkstreamSourceListParams{WorkstreamID: created.Workstream.ID})
	if err != nil || len(list.Sources) != 2 || list.Sources[0].ReadByJobs || !list.Sources[1].ReadByJobs || list.Sources[1].AddedBy != "you" || list.Sources[1].Proposed {
		t.Fatalf("created sources: %+v %v", list, err)
	}
	tooMany := make([]appwire.WorkstreamSourceAdd, workstream.CreateSourcesMax+1)
	for i := range tooMany {
		tooMany[i] = good
	}
	_, err = b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Over limit", Sources: tooMany})
	workstreamCheckError(t, err, appwire.CodeBadParams)
}

// WP-12 Fix 1 Major 2 (e): identical wire retries bypass source existence
// checks and create no agent, sources or events; new clients keep the checks.
func TestAppWorkstreamMajor2CreateRetryEOnWire(t *testing.T) {
	b, rec, calls, _ := workstreamBackend(t)
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	path := workstreamSourcesFile(t)
	p := map[string]any{"name": "Retry with removed source", "client_id": "retry-removed", "sources": []any{map[string]any{"kind": "file", "path": path}}}
	created := workstreamSourceWireCall(t, connection, "workstream.create", p)["workstream"].(map[string]any)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	listParams := map[string]any{"workstream_id": created["id"]}
	before := workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"]
	events := len(workstreamEvents(rec))
	again := workstreamSourceWireCall(t, connection, "workstream.create", p)["workstream"]
	after := workstreamSourceWireCall(t, connection, "source.list", listParams)["sources"]
	if !reflect.DeepEqual(again, created) || !reflect.DeepEqual(after, before) || len(after.([]any)) != 1 || *calls != 1 || len(workstreamEvents(rec)) != events {
		t.Fatalf("retry changed state: workstream=%v sources=%v calls=%d", again, after, *calls)
	}
	p["client_id"], p["name"] = "new-client", "New client name"
	frame := connection.call(t, "workstream.create", p)
	if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" || *calls != 1 || len(workstreamEvents(rec)) != events {
		t.Fatalf("new client bypassed validation: %v", frame)
	}
	if list, err := b.WorkstreamList(appwire.WorkstreamListParams{}); err != nil || len(list.Workstreams) != 1 {
		t.Fatalf("new client wrote a workstream: %+v %v", list, err)
	}
	delete(p, "sources")
	workstreamSourceWireCall(t, connection, "workstream.create", p)
}

// WP-12 (c), (h): toggles and keeping a proposal use the real wire path;
// done rejects all mutations and reopen restores them.
func TestAppWorkstreamSourceUpdatesAndDoneOnWire(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	proposal, err := door.ProposeSource(ctx, sessionWorkstreamSourceProposalForTest())
	if err != nil {
		t.Fatal(err)
	}
	connection := workstreamWireConnect(t, &workstreamWireBackend{appBackend: b})
	before := len(workstreamEvents(rec))
	frame := connection.call(t, "source.update", map[string]any{"id": proposal.ID, "read_by_jobs": true})
	if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "bad_params" || len(workstreamEvents(rec)) != before {
		t.Fatalf("proposal jobs: %v", frame)
	}
	kept := workstreamSourceWireCall(t, connection, "source.update", map[string]any{"id": proposal.ID, "keep": true, "read_by_jobs": true})["source"].(map[string]any)
	if kept["proposed"] != false || kept["read_by_jobs"] != true || kept["why"] != "Read the guide." || kept["added_by"] != "agent" {
		t.Fatalf("keep: %v", kept)
	}
	for _, flag := range []bool{false, true} {
		workstreamSourceWireCall(t, connection, "source.update", map[string]any{"id": proposal.ID, "read_by_jobs": flag})
		list := workstreamSourceWireCall(t, connection, "source.list", map[string]any{"workstream_id": w.ID})["sources"].([]any)
		if len(list) != 1 || list[0].(map[string]any)["read_by_jobs"] != flag {
			t.Fatalf("toggle: %v", list)
		}
	}
	before = len(workstreamEvents(rec))
	workstreamSourceWireCall(t, connection, "source.update", map[string]any{"id": proposal.ID, "keep": true})
	if len(workstreamEvents(rec)) != before {
		t.Fatal("no-op update sent an event")
	}
	workstreamSourceWireCall(t, connection, "workstream.mark_done", map[string]any{"id": w.ID})
	before = len(workstreamEvents(rec))
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"source.add", map[string]any{"workstream_id": w.ID, "kind": "link", "url": "https://example.com/new"}},
		{"source.update", map[string]any{"id": proposal.ID, "title": "Changed"}},
		{"source.remove", map[string]any{"id": proposal.ID}},
	} {
		frame := connection.call(t, tc.method, tc.params)
		if frame["error"] == nil || frame["error"].(map[string]any)["code"] != "conflict" {
			t.Fatalf("done: %v", frame)
		}
	}
	if len(workstreamEvents(rec)) != before {
		t.Fatal("done write sent an event")
	}
	if _, err := door.ProposeSource(ctx, sessionWorkstreamSourceProposalForTest()); err == nil {
		t.Fatal("done proposal succeeded")
	}
	workstreamSourceWireCall(t, connection, "source.list", map[string]any{"workstream_id": w.ID})
	workstreamSourceWireCall(t, connection, "workstream.reopen", map[string]any{"id": w.ID})
	workstreamSourceWireCall(t, connection, "source.update", map[string]any{"id": proposal.ID, "title": "Reopened"})
	workstreamSourceWireCall(t, connection, "source.remove", map[string]any{"id": proposal.ID})
	workstreamSourceWireCall(t, connection, "source.add", map[string]any{"workstream_id": w.ID, "kind": "link", "url": "https://example.com/new"})
}

// WP-12 conformance: four heterogeneous source rows retain exact optional keys
// at each illustrated index, including the agent's reason only on its proposal.
func (fixture *workstreamWireFixture) sources(t *testing.T) {
	t.Helper()
	b := fixture.Backend
	fixture.record(t)
	adds := []appwire.WorkstreamSourceAddParams{
		{WorkstreamID: fixture.Stream.ID, Kind: "file", Path: workstreamPtr(fixture.IDs["/Users/ada/billing/usage-pricing.md"]), ReadByJobs: workstreamPtr(true)},
		{WorkstreamID: fixture.Stream.ID, Kind: "issue", Repo: workstreamPtr("acme/billing"), Number: workstreamPtr(412), ReadByJobs: workstreamPtr(true)},
		{WorkstreamID: fixture.Stream.ID, Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "record", ID: fixture.IDs["rec_7a1e"]}},
	}
	for i, ask := range adds {
		made, err := b.WorkstreamSourceAdd(ask)
		if err != nil {
			t.Fatal(err)
		}
		fixture.IDs[fmt.Sprintf("src_2b7%d", i+1)] = made.Source.ID
	}
	s, err := b.workstreamStore(false)
	if err != nil {
		t.Fatal(err)
	}
	item, _, change, err := s.ProposeSource(context.Background(), fixture.Stream.ID, workstream.SourceAdd{Kind: "link", URL: workstreamPtr("https://docs.stripe.com/billing/subscriptions/usage-based")}, "The metering rules the plan follows.")
	if err != nil {
		t.Fatal(err)
	}
	b.workstreamChanged(change)
	fixture.IDs["src_2b74"] = item.ID
}

func workstreamSeedSources(t *testing.T, b *appBackend, id string, total int) {
	t.Helper()
	store, err := b.collectionStore(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		statement, err := tx.Prepare(`INSERT INTO workstream_sources(id,workstream_id,kind,target,title,url,added_by,added_at) VALUES (?,?,'link',?,'Example',?,'you',100)`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for i := 0; i < total; i++ {
			url := fmt.Sprintf("https://example.com/source/%d", i)
			if _, err := statement.Exec(fmt.Sprintf("fixture-source-%d", i), id, url, url); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func workstreamWireSourceError(t *testing.T, fixture *workstreamWireFixture, errorCase workstreamWireErrorCase, params map[string]any) bool {
	t.Helper()
	if !strings.HasPrefix(errorCase.Name, "source_") {
		return false
	}
	switch {
	case strings.HasSuffix(errorCase.Name, "_bad"):
		if errorCase.Method == "source.list" || errorCase.Method == "source.add" {
			delete(params, "workstream_id")
		} else {
			delete(params, "id")
		}
	case strings.HasSuffix(errorCase.Name, "_missing"):
		if errorCase.Method == "source.list" || errorCase.Method == "source.add" {
			params["workstream_id"] = "missing-workstream"
		} else {
			params["id"] = "missing-source"
		}
	case strings.HasSuffix(errorCase.Name, "_done"):
		if _, err := fixture.Backend.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: fixture.Stream.ID}); err != nil {
			t.Fatal(err)
		}
	case strings.HasSuffix(errorCase.Name, "_limit"):
		workstreamSeedSources(t, fixture.Backend, fixture.Stream.ID, workstream.SourcesMax)
	default:
		return false
	}
	return true
}
