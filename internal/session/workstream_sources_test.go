package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WP-12 (g): the new tool reaches only its proposal door and tells the person
// who keeps or drops it. Its strict schema refuses invented fields and nulls.
func TestWorkstreamProposeSourceToolAndStrictArguments(t *testing.T) {
	d := &workstreamFakeDoor{}
	got := workstreamCallForTest(t, d, "propose_source", `{"kind":"link","url":"https://example.com","why":"Read the guide."}`, false)
	if !strings.Contains(got, "source-1") || !strings.Contains(got, "person keeps or drops") || len(d.sourceProposals) != 1 || len(d.proposals) != 0 || len(d.saves) != 0 || len(d.pageWrites) != 0 {
		t.Fatalf("proposal: %q %+v", got, d)
	}
	for _, args := range []string{
		`{"kind":"link","url":"https://example.com","why":"Read it.","read_by_jobs":true}`,
		`{"kind":"link","url":"https://example.com","why":"Read it.","proposed":false}`,
		`{"kind":"file","path":"relative","why":"Read it."}`,
		`{"kind":"link","url":"file:///etc/passwd","why":"Read it."}`,
		`{"kind":"link","url":"https://example.com","why":""}`,
		`{"kind":"link","url":"https://example.com","why":"` + strings.Repeat("x", 501) + `"}`,
		`{"kind":"link","url":"https://example.com","why":"Read it.","title":null}`,
		`{"kind":"kept","ref":{"kind":"message","id":"m","chat_id":"c","extra":true},"why":"Read it."}`,
		`{"kind":"kept","ref":{"kind":"message","id":"m","id":"m","chat_id":"c"},"why":"Read it."}`,
		`{"kind":"link","url":"https://example.com","url":"https://example.com","why":"Read it."}`,
	} {
		d := &workstreamFakeDoor{}
		ctx, mark := withToolRejection(context.Background())
		text, refused, err := workstreamToolForTest(t, d, "propose_source").Execute(ctx, json.RawMessage(args))
		if err != nil || !refused || !mark.RejectedBeforeRun() || d.otherCalls != 0 {
			t.Fatalf("invalid proposal: %s %q %+v %v", args, text, d, err)
		}
	}
}

// WP-12 Fix 1 Major 3 (b), (g): a raw invalid path is refused before the
// proposal door, even when its JSON-repaired replacement would be a real file.
func TestWorkstreamMajor3InvalidUTF8BProposeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-�.txt")
	if err := os.WriteFile(path, []byte("Existing replacement path."), 0600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"kind": "file", "path": path, "why": "Read the source."})
	if err != nil {
		t.Fatal(err)
	}
	d := &workstreamFakeDoor{}
	ctx, mark := withToolRejection(context.Background())
	invalid := bytes.ReplaceAll(args, []byte("�"), []byte{0xff})
	if !bytes.Contains(invalid, []byte{0xff}) {
		t.Fatal("fixture must contain a literal invalid UTF-8 byte")
	}
	answer, refused, err := workstreamToolForTest(t, d, "propose_source").Execute(ctx, invalid)
	if err != nil || !refused || !mark.RejectedBeforeRun() || !strings.Contains(answer, "UTF-8") || d.otherCalls != 0 || len(d.sourceProposals) != 0 {
		t.Fatalf("invalid raw path reached proposal door: %q %+v %v", answer, d, err)
	}
	workstreamCallForTest(t, d, "propose_source", string(args), false)
	if len(d.sourceProposals) != 1 || *d.sourceProposals[0].Path != path {
		t.Fatalf("valid replacement character proposal: %+v", d.sourceProposals)
	}
}

// WP-12 (g): sources and source are valid reads; source_id is required only
// for the latter and never permits another part's selector fields.
func TestWorkstreamSourceReadArguments(t *testing.T) {
	for _, args := range []string{`{"what":"sources"}`, `{"what":"source","source_id":"source-1"}`} {
		d := &workstreamFakeDoor{read: json.RawMessage(`{"sources":[]}`)}
		workstreamCallForTest(t, d, "workstream_read", args, false)
		if d.otherCalls != 1 {
			t.Fatal("source read did not reach the door")
		}
	}
	for _, args := range []string{`{"what":"source"}`, `{"what":"source","source_id":""}`, `{"what":"sources","source_id":"source-1"}`, `{"what":"source","source_id":"source-1","page_id":"page-1"}`, `{"what":"source","source_id":"source-1","kind":"decision"}`} {
		d := &workstreamFakeDoor{}
		workstreamCallForTest(t, d, "workstream_read", args, true)
		if d.otherCalls != 0 {
			t.Fatal("invalid source selector reached the door")
		}
	}
}

// WP-12 (g): every source remains in the JSON inventory even when it exceeds
// the content-read cap; page and file content retain their existing bounds.
func TestWorkstreamSourceListKeepsEveryRowAsJSON(t *testing.T) {
	items := make([]map[string]any, 2000)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("source-%d", i), "workstream_id": "w", "kind": "link", "title": strings.Repeat("x", 100), "url": "https://example.com", "read_by_jobs": false, "proposed": false, "added_by": "you", "added_at": "2026-11-03T12:00:00Z"}
	}
	raw, err := json.Marshal(map[string]any{"sources": items})
	if err != nil {
		t.Fatal(err)
	}
	d := &workstreamFakeDoor{read: raw}
	got := workstreamCallForTest(t, d, "workstream_read", `{"what":"sources"}`, false)
	if len(got) <= workstreamReadMax || !json.Valid([]byte(got)) || got != string(raw) {
		t.Fatal("source inventory was cut or changed")
	}
}
