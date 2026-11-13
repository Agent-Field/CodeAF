package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A PERSON'S READ, POST /v1/read {jobs}: the chosen jobs are handed to the
// read in the order asked, and its answer comes back whole; a factory with no
// read refuses the verb.
func TestAClickReadHandsTheChosenJobsToTheRead(t *testing.T) {
	seam := factory.Seam{Load: func() (factory.Snapshot, error) {
		return factory.Snapshot{Items: []factory.Item{
			{ID: 5, Repo: "acme/api", Kind: factory.KindIssue, Title: "a", State: factory.StateNew},
			{ID: 6, Repo: "acme/api", Kind: factory.KindIssue, Title: "b", State: factory.StateNew},
		}, Repos: []factory.Repo{{Name: "acme/api"}}}, nil
	}}
	var handed []int
	h := New(seam, Config{Address: "http://factory", Version: "test", Read: func(_ context.Context, ids []int) api.ReadResult {
		handed = ids
		return api.ReadResult{Read: []int{6}, Skipped: []int{}, Failed: []int{5}}
	}})
	t.Cleanup(h.Close)
	post := func(h *Handler, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/read", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := post(h, `{"jobs":[6,5]}`)
	var got api.ReadResult
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	if !reflect.DeepEqual(handed, []int{6, 5}) || !reflect.DeepEqual(got.Read, []int{6}) || !reflect.DeepEqual(got.Failed, []int{5}) {
		t.Fatalf("handed %v answered %+v", handed, got)
	}
	if w := post(h, `{"jobs":[]}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no jobs: status %d", w.Code)
	}
	none := New(seam, Config{Address: "http://factory", Version: "test"})
	t.Cleanup(none.Close)
	if w := post(none, `{"jobs":[5]}`); w.Code < 400 {
		t.Fatalf("a factory with no read answered %d", w.Code)
	}
}
