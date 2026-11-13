package main

import (
	"encoding/base64"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// docHeard is one relayed document event and when this engine relayed it.
type docHeard struct {
	name string
	data any
	at   time.Time
}

// hearDocs makes p's engine relay its documents to a channel.
func hearDocs(p *person) chan docHeard {
	got := make(chan docHeard, 256)
	h := p.w.backend.factoryHub()
	h.mu.Lock()
	h.docEvent = func(name string, data any) { got <- docHeard{name, data, time.Now()} }
	h.orgRetry = 5 * time.Millisecond
	h.mu.Unlock()
	return got
}

func nextDocHeard(t *testing.T, got chan docHeard, name string) docHeard {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-got:
			if e.name == name {
				return e
			}
		case <-deadline:
			t.Fatalf("no %s arrived", name)
		}
	}
}

// TWO PEOPLE, TWO ENGINES, ONE REAL ORGANIZATION SERVER: alice types into a
// page and bob's engine relays every update in order, with the server's
// stamp, well inside half a second; alice's caret reaches bob and leaves with
// her; and bob's follow ends cleanly when he stops it. The latency figures are
// logged so the measurement is visible in -v output.
func TestTwoEnginesCoEditOnePageThroughARealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	if !strings.Contains(alice.w.hello, `"org_docs"`) {
		t.Fatalf("hello lacks org_docs: %s", alice.w.hello)
	}
	var ws appwire.OrgWorkstream
	alice.w.must("factory.org_workstream_create", map[string]any{"connection": alice.conn.ID, "name": "Webhooks", "pages": []map[string]any{{"title": "Runbook", "kind": "runbook"}}}, &ws)
	if _, err := f.st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		w := all[ws.ID]
		w.Shares = []factory.Share{{Kind: factory.ShareToPerson, ID: "bob", Role: factory.RoleEdit}}
		all[ws.ID] = w
		return []string{ws.ID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	doc := "ws/" + ws.ID + "/page/" + ws.Pages[0].ID
	heard := hearDocs(bob)
	hearDocs(alice)

	// The first text goes in as a seed; a second seed is refused as conflict.
	var up appwire.OrgDocUpdated
	alice.w.must("factory.org_doc_send", map[string]any{"connection": alice.conn.ID, "doc": doc, "update": b64s("seed"), "seed": true}, &up)
	if up.Seq != 1 || up.By != "alice" {
		t.Fatalf("seed = %+v", up)
	}
	if _, code, _ := bob.w.call("factory.org_doc_send", map[string]any{"connection": bob.conn.ID, "doc": doc, "update": b64s("again"), "seed": true}); code != appwire.CodeConflict {
		t.Fatalf("a second seed = %q", code)
	}
	if line := bob.w.lastLine(); !strings.Contains(line, `"reason":"seeded"`) {
		t.Fatalf("a second seed's refusal lost its reason: %s", line)
	}
	bob.w.mustFail("factory.org_doc_send", map[string]any{"connection": bob.conn.ID, "doc": "ws/../page/x", "update": b64s("x")}, appwire.CodeBadParams)

	var st appwire.OrgDocState
	bob.w.must("factory.org_doc_read", map[string]any{"connection": bob.conn.ID, "doc": doc}, &st)
	if st.Seq != 1 || len(st.Updates) != 1 || !st.CanEdit || st.Awareness == nil {
		t.Fatalf("bob reads %+v", st)
	}
	bob.w.must("factory.org_doc_follow", map[string]any{"connection": bob.conn.ID, "doc": doc, "since": st.Seq}, nil)

	// The first live update proves the stream is open; then measure.
	alice.w.must("factory.org_doc_send", map[string]any{"connection": alice.conn.ID, "doc": doc, "base_seq": 1, "update": b64s("k")}, nil)
	if e := nextDocHeard(t, heard, appwire.EventFactoryOrgDocUpdate).data.(appwire.FactoryOrgDocUpdate); e.Seq != 2 || e.By != "alice" || e.Connection != bob.conn.ID || e.Doc != doc {
		t.Fatalf("first live = %+v", e)
	}
	var lags []time.Duration
	for i := 3; i < 53; i++ {
		sent := time.Now()
		alice.w.must("factory.org_doc_send", map[string]any{"connection": alice.conn.ID, "doc": doc, "base_seq": i - 1, "update": b64s("k")}, nil)
		e := nextDocHeard(t, heard, appwire.EventFactoryOrgDocUpdate)
		if u := e.data.(appwire.FactoryOrgDocUpdate); u.Seq != int64(i) || u.Update != b64s("k") {
			t.Fatalf("update %d arrived as %+v", i, u)
		}
		lags = append(lags, e.at.Sub(sent))
	}
	sort.Slice(lags, func(i, j int) bool { return lags[i] < lags[j] })
	t.Logf("alice's update to bob's engine over %d sends: p50 %v, p95 %v, max %v", len(lags), lags[len(lags)/2], lags[len(lags)*95/100], lags[len(lags)-1])
	if lags[len(lags)-1] > 500*time.Millisecond {
		t.Fatalf("an update took %v to reach the other engine", lags[len(lags)-1])
	}

	state := b64s(`{"user":{"name":"alice"}}`)
	alice.w.must("factory.org_doc_aware", map[string]any{"connection": alice.conn.ID, "doc": doc, "client": 7, "state": state}, nil)
	if a := nextDocHeard(t, heard, appwire.EventFactoryOrgDocAwareness).data.(appwire.FactoryOrgDocAwareness); a.By != "alice" || a.Client != 7 || a.State == nil || *a.State != state {
		t.Fatalf("caret = %+v", a)
	}
	alice.w.must("factory.org_doc_aware", map[string]any{"connection": alice.conn.ID, "doc": doc, "client": 7, "state": nil}, nil)
	if a := nextDocHeard(t, heard, appwire.EventFactoryOrgDocAwareness).data.(appwire.FactoryOrgDocAwareness); a.State != nil {
		t.Fatalf("leave = %+v", a)
	}

	var kept appwire.OrgDocSnapshotKept
	bob.w.must("factory.org_doc_snapshot", map[string]any{"connection": bob.conn.ID, "doc": doc, "seq": 52, "state": b64s("all of it")}, &kept)
	if !kept.Kept {
		t.Fatalf("snapshot = %+v", kept)
	}
	alice.w.must("factory.org_doc_read", map[string]any{"connection": alice.conn.ID, "doc": doc, "since": 10}, &st)
	if st.Snapshot == nil || st.Snapshot.Seq != 52 || st.Snapshot.By != "bob" || len(st.Updates) != 0 {
		t.Fatalf("behind the snapshot = %+v", st)
	}

	bob.w.must("factory.org_doc_unfollow", map[string]any{"connection": bob.conn.ID, "doc": doc}, nil)
	h := bob.w.backend.factoryHub()
	h.mu.Lock()
	left := len(h.docFollow)
	h.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d follows left after unfollow", left)
	}
}

func b64s(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// lastLine is the newest line the wire carried that answered a call.
func (w *factoryWire) lastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.lines) - 1; i >= 0; i-- {
		if strings.Contains(w.lines[i], `"id":`) {
			return w.lines[i]
		}
	}
	return ""
}
