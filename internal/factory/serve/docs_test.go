package serve

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// docOrg is the org scene with one page on the Platform workstream (shared
// with Growth) and one on Design's, which santosh and abir cannot see.
func docOrg(t *testing.T) (o *wsOrg, page, hidden string) {
	t.Helper()
	o = newWSOrg(t)
	webhook, _, _, brand := o.orgScene(t)
	var got api.Workstream
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+webhook.ID, webhook.Rev, map[string]any{"pages": []api.WorkstreamPage{{Title: "Runbook", Kind: "runbook"}}}, 200, &got)
	page = "ws/" + webhook.ID + "/page/" + got.Pages[0].ID
	o.do(t, "mia", "PATCH", "/v1/workstreams/"+brand.ID, brand.Rev, map[string]any{"pages": []api.WorkstreamPage{{Title: "Voice", Kind: "page"}}}, 200, &got)
	hidden = "ws/" + brand.ID + "/page/" + got.Pages[0].ID
	return o, page, hidden
}

// ws is the workstream id of a page document.
func ws(doc string) string { return strings.Split(doc, "/")[1] }

// setShares keeps a workstream's shares and privacy as given, straight in the
// store: the roles a share route will grant, without waiting on its wire.
func (o *wsOrg) setShares(t *testing.T, id string, shares []factory.Share, private bool) {
	t.Helper()
	if _, err := o.st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		w := all[id]
		w.Shares, w.Private = shares, private
		all[id] = w
		return []string{id}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// A PAGE'S DOCUMENT IS ITS WORKSTREAM'S: who sees the workstream reads it,
// who writes the workstream changes it, a page that is not one of its pages
// or a workstream one cannot see is absent, a canvas document is absent until
// the server keeps canvases, and an archived workstream's pages are
// read-only. The server stamps by itself and keeps the log in order.
func TestAPageDocumentIsItsWorkstreams(t *testing.T) {
	o, page, hidden := docOrg(t)
	var up api.DocUpdated
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{Update: b64("hello"), Seed: true}, 200, &up)
	if up.Seq != 1 || up.By != "abir" || up.Doc != page {
		t.Fatalf("stamped %+v", up)
	}
	o.do(t, "santosh", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{Update: b64("seed again"), Seed: true}, 409, nil)
	o.do(t, "santosh", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 1, Update: b64(" world")}, 200, &up)
	if up.Seq != 2 || up.By != "santosh" {
		t.Fatalf("stamped %+v", up)
	}
	var st api.DocState
	o.do(t, "mia", "GET", "/v1/docs/"+page+"?since=1", -1, nil, 200, &st)
	if st.Seq != 2 || len(st.Updates) != 1 || st.Updates[0].By != "santosh" || st.Updates[0].Update != b64(" world") || !st.CanEdit || st.Snapshot != nil {
		t.Fatalf("mia reads %+v", st)
	}
	// The operator sees every workstream but writes none it is not in.
	o.do(t, "owner", "GET", "/v1/docs/"+page, -1, nil, 200, &st)
	if st.CanEdit || len(st.Updates) != 2 {
		t.Fatalf("operator reads %+v", st)
	}
	o.do(t, "owner", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 2, Update: b64("x")}, 403, nil)
	o.do(t, "owner", "PUT", "/v1/docs/"+page+"/snapshot", -1, api.DocSnapshotPut{Seq: 2, State: b64("S")}, 403, nil)
	// Absent, the same as nothing: a hidden workstream, a page it does not
	// have, a canvas, a malformed id.
	for _, path := range []string{hidden, page + "x", "canvas/cv_1/frame/f_1", "ws/ws_1/frame/p", "ws/../page/p", page + "/elsewhere"} {
		o.do(t, "santosh", "GET", "/v1/docs/"+path, -1, nil, 404, nil)
		o.do(t, "santosh", "POST", "/v1/docs/"+path+"/updates", -1, api.DocUpdatePost{Update: b64("x")}, 404, nil)
	}
	// A base past the head is a copy of a document that was reset.
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 9, Update: b64("x")}, 409, nil)
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{Update: "not base64!"}, 422, nil)

	var kept api.DocSnapshotKept
	o.do(t, "abir", "PUT", "/v1/docs/"+page+"/snapshot", -1, api.DocSnapshotPut{Seq: 2, State: b64("hello world")}, 200, &kept)
	if !kept.Kept {
		t.Fatalf("snapshot %+v", kept)
	}
	o.do(t, "santosh", "PUT", "/v1/docs/"+page+"/snapshot", -1, api.DocSnapshotPut{Seq: 1, State: b64("hello")}, 200, &kept)
	if kept.Kept {
		t.Fatal("an older snapshot replaced a newer one")
	}
	o.do(t, "mia", "GET", "/v1/docs/"+page+"?since=1", -1, nil, 200, &st)
	if st.Snapshot == nil || st.Snapshot.Seq != 2 || st.Snapshot.State != b64("hello world") || st.Snapshot.By != "abir" || len(st.Updates) != 0 {
		t.Fatalf("behind the snapshot reads %+v", st)
	}

	// A view share reads and follows, never writes; a private workstream's
	// page is the operator's to see exist, never to read.
	o.setShares(t, ws(page), []factory.Share{{Kind: factory.ShareToPerson, ID: "mia", Role: factory.RoleView}, {Kind: factory.ShareToTeam, ID: "tm_platform", Role: factory.RoleEdit}}, false)
	o.do(t, "mia", "GET", "/v1/docs/"+page, -1, nil, 200, &st)
	if st.CanEdit {
		t.Fatal("a view share can edit")
	}
	o.do(t, "mia", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 2, Update: b64("x")}, 403, nil)
	o.do(t, "mia", "POST", "/v1/docs/"+page+"/awareness", -1, api.DocAwarenessPost{Client: 1, State: ptr(b64("c"))}, 204, nil)
	o.setShares(t, ws(page), nil, true)
	o.do(t, "owner", "GET", "/v1/docs/"+page, -1, nil, 404, nil)
	o.do(t, "mia", "GET", "/v1/docs/"+page, -1, nil, 404, nil)
	o.setShares(t, ws(page), []factory.Share{{Kind: factory.ShareToTeam, ID: "tm_platform", Role: factory.RoleEdit}}, false)

	// Archived: still read, never written.
	o.do(t, "abir", "DELETE", "/v1/workstreams/"+ws(page), -1, nil, 204, nil)
	o.do(t, "abir", "GET", "/v1/docs/"+page, -1, nil, 200, &st)
	if st.CanEdit {
		t.Fatal("an archived page is still editable")
	}
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 2, Update: b64("x")}, 403, nil)
}

// A SENDER IS SLOWED, NOT DROPPED: past the burst an update is refused with
// 429 and Retry-After, and nothing is stamped for it.
func TestADocumentSenderIsSlowedDown(t *testing.T) {
	o, page, _ := docOrg(t)
	n := 0
	for ; n < 1000; n++ {
		r := httptest.NewRequest("POST", "/v1/docs/"+page+"/updates", strings.NewReader(`{"base_seq":0,"update":"`+b64("k")+`"}`))
		r.Header.Set("Authorization", "Bearer "+o.tokens["abir"])
		w := httptest.NewRecorder()
		o.h.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			if w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), api.DocReasonSlowDown) {
				t.Fatalf("429 = %v %s", w.Header(), w.Body)
			}
			break
		}
		if w.Code != 200 {
			t.Fatalf("update %d: %d %s", n, w.Code, w.Body)
		}
	}
	var st api.DocState
	o.do(t, "abir", "GET", "/v1/docs/"+page, -1, nil, 200, &st)
	if n == 1000 || st.Seq != int64(n) {
		t.Fatalf("slowed after %d, stamped %d", n, st.Seq)
	}
	// Someone else on the same page is not slowed by abir.
	o.do(t, "santosh", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: st.Seq, Update: b64("s")}, 200, nil)
}

// docStream follows a document over a real connection and hands each event
// to the channel it returns.
type docEvent struct {
	kind, id string
	data     json.RawMessage
	got      time.Time
}

func docStream(t *testing.T, ctx context.Context, url, token string) <-chan docEvent {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("stream %s: %d", url, resp.StatusCode)
	}
	out := make(chan docEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		var e docEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if e.kind != "" {
					e.got = time.Now()
					out <- e
				}
				e = docEvent{}
			case strings.HasPrefix(line, "id: "):
				e.id = line[4:]
			case strings.HasPrefix(line, "event: "):
				e.kind = line[7:]
			case strings.HasPrefix(line, "data: "):
				e.data = json.RawMessage(line[6:])
			}
		}
	}()
	return out
}

func nextDocEvent(t *testing.T, events <-chan docEvent, kind string) docEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("stream ended waiting for %s", kind)
			}
			if e.kind == kind {
				return e
			}
		case <-timeout:
			t.Fatalf("no %s in 5s", kind)
		}
	}
}

// TWO PEOPLE ON ONE PAGE, OVER A REAL CONNECTION: a stream opened at a seq
// replays what came after it, then hears the other person's update as it is
// stamped and their caret as it moves, and hears the page go when it is
// dropped.
func TestTwoPeopleFollowOnePage(t *testing.T) {
	o, page, _ := docOrg(t)
	srv := httptest.NewServer(o.h)
	defer srv.Close()
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{Update: b64("one"), Seed: true}, 200, nil)
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 1, Update: b64("two")}, 200, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := docStream(t, ctx, srv.URL+"/v1/docs/"+page+"/events?since=1", o.tokens["santosh"])
	replay := nextDocEvent(t, events, api.EventDocUpdate)
	var u api.DocUpdateEvent
	_ = json.Unmarshal(replay.data, &u)
	if replay.id != "2" || u.Seq != 2 || u.Update != b64("two") || u.Doc != page {
		t.Fatalf("replay = %+v %s", u, replay.id)
	}
	sent := time.Now()
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/updates", -1, api.DocUpdatePost{BaseSeq: 2, Update: b64("three")}, 200, nil)
	live := nextDocEvent(t, events, api.EventDocUpdate)
	_ = json.Unmarshal(live.data, &u)
	if u.Seq != 3 || u.By != "abir" || u.Update != b64("three") {
		t.Fatalf("live = %+v", u)
	}
	if lag := live.got.Sub(sent); lag > 500*time.Millisecond {
		t.Fatalf("update reached the other person after %v", lag)
	}
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/awareness", -1, api.DocAwarenessPost{Client: 42, State: ptr(b64(`{"user":{"name":"abir"}}`))}, 204, nil)
	caret := nextDocEvent(t, events, api.EventDocAwareness)
	var a api.DocAwarenessEvent
	_ = json.Unmarshal(caret.data, &a)
	if a.By != "abir" || a.Client != 42 || a.State == nil {
		t.Fatalf("caret = %+v", a)
	}
	o.do(t, "abir", "POST", "/v1/docs/"+page+"/awareness", -1, api.DocAwarenessPost{Client: 42}, 204, nil)
	_ = json.Unmarshal(nextDocEvent(t, events, api.EventDocAwareness).data, &a)
	if a.State != nil {
		t.Fatalf("left = %+v", a)
	}
	if err := o.h.dropDoc(page); err != nil {
		t.Fatal(err)
	}
	nextDocEvent(t, events, api.EventDocRemoved)
	var st api.DocState
	o.do(t, "santosh", "GET", "/v1/docs/"+page, -1, nil, 200, &st)
	if st.Seq != 0 || len(st.Updates) != 0 {
		t.Fatalf("after drop = %+v", st)
	}
	// Someone who cannot see the workstream cannot follow its page.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/docs/"+page+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+o.tokens["mia"]+"x")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode == 200 {
		t.Fatalf("a stranger followed: %v %v", resp, err)
	}
}
