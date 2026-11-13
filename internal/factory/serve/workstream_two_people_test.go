package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// E1'S ACCEPTANCE, OVER THE WIRE: santosh and abir, each signed in through
// the device flow, talk to one organization server on a real loopback
// listener with plain HTTP, the way two desks do.
//
//   - abir, shared for view, is refused 403 on PATCH and on archive.
//   - santosh's private workstream is a name-only row for the operator and
//     404 for its content.
//   - a decision sent with a forged by is kept as the caller's.
func TestTwoPeopleOverTheWireMeetE1sAcceptance(t *testing.T) {
	o := newWSOrg(t)
	srv := httptest.NewServer(o.h)
	t.Cleanup(srv.Close)
	call := func(who, method, path string, rev int, body any, out any) int {
		t.Helper()
		var buf io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			buf = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, srv.URL+path, buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+o.tokens[who])
		if rev >= 0 {
			req.Header.Set("If-Match", strconv.Itoa(rev))
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil && res.StatusCode < 300 {
			if err := json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode
	}

	var ws api.Workstream
	if got := call("santosh", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Webhook reliability"}, &ws); got != 201 {
		t.Fatalf("create = %d", got)
	}
	path := "/v1/workstreams/" + ws.ID
	view := []factory.Share{{Kind: "person", ID: "abir", Role: "view"}}
	var shared api.WorkstreamShared
	if got := call("santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &view, Rev: &ws.Rev}, &shared); got != 200 {
		t.Fatalf("share = %d", got)
	}
	var seen api.Workstream
	if got := call("abir", "GET", path, -1, nil, &seen); got != 200 || seen.MyRole != "view" {
		t.Fatalf("abir reads = %d as %q", got, seen.MyRole)
	}
	if got := call("abir", "PATCH", path, seen.Rev, map[string]any{"purpose": "mine now"}, nil); got != 403 {
		t.Fatalf("abir's PATCH with view = %d, want 403", got)
	}
	if got := call("abir", "DELETE", path, -1, nil, nil); got != 403 {
		t.Fatalf("abir's archive with view = %d, want 403", got)
	}

	var private api.Workstream
	if got := call("santosh", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Salary bands", Private: true, Purpose: "secret"}, &private); got != 201 {
		t.Fatalf("private create = %d", got)
	}
	var list api.WorkstreamList
	call("owner", "GET", "/v1/workstreams", -1, nil, &list)
	var row *api.WorkstreamSummary
	for i := range list.Workstreams {
		if list.Workstreams[i].ID == private.ID {
			row = &list.Workstreams[i]
		}
	}
	if row == nil || row.Name != "Salary bands" || row.MyRole != "" || !row.Private {
		t.Fatalf("the operator's row = %+v", row)
	}
	if got := call("owner", "GET", "/v1/workstreams/"+private.ID, -1, nil, nil); got != 404 {
		t.Fatalf("the operator reads a private workstream: %d, want 404", got)
	}

	edit := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	call("santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &edit}, &shared)
	forged := []api.WorkstreamDecision{{Text: "Retry thrice", By: "santosh", At: "2001-01-01T00:00:00Z"}}
	var after api.Workstream
	if got := call("abir", "PATCH", path, shared.Workstream.Rev, map[string]any{"decisions": forged}, &after); got != 200 {
		t.Fatalf("abir's decision = %d", got)
	}
	if d := after.Decisions[0]; d.By != "abir" || d.At == forged[0].At {
		t.Fatalf("a forged decision was kept as %+v", d)
	}
}
