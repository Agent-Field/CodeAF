package directory_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

// A beat says a turn is running only to a relay that advertises working beats;
// an older relay never sees the field, which it would refuse.
func TestHTTPBeatWorkingOnlyWhereAdvertised(t *testing.T) {
	clock := directorytest.NewFakeClock()
	next := directory.Handler(fakeAuth, newNamespaces(clock).open)
	var supported atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !supported.Load() && bytes.Contains(raw, []byte(`"working"`)) {
			t.Error("an older relay received the working flag")
			http.Error(w, `{"err":"bad_request"}`, http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		reply := httptest.NewRecorder()
		next.ServeHTTP(reply, r)
		for key, values := range reply.Header() {
			if supported.Load() || !strings.EqualFold(key, directory.WorkingBeatsHeader) {
				w.Header()[key] = values
			}
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(reply.Body.Bytes())
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	v, err := client.Create(t.Context(), cellX, directory.CellInit{Head: head1})
	if err != nil {
		t.Fatal(err)
	}
	beat := directory.Beat{Fence: v.Cell.Lease.Fence, Working: true}
	got, err := client.Heartbeat(t.Context(), cellX, beat)
	if err != nil || got.Cell.Lease.Working {
		t.Fatalf("an older relay's lease = %+v, %v", got.Cell.Lease, err)
	}
	supported.Store(true)
	if _, err := client.Cell(t.Context(), cellX); err != nil { // a read learns the capability
		t.Fatal(err)
	}
	if got, err = client.Heartbeat(t.Context(), cellX, beat); err != nil || !got.Cell.Lease.Working {
		t.Fatalf("a relay that advertises working beats kept %+v, %v", got.Cell.Lease, err)
	}
}

// Pairing routes share the service address but do not advertise directory
// capabilities, so their replies cannot withdraw a learned working flag.
func TestHTTPWorkingCapabilitySurvivesLinkLimits(t *testing.T) {
	clock := directorytest.NewFakeClock()
	mux := http.NewServeMux()
	mux.Handle("/v1/dir/", directory.Handler(fakeAuth, newNamespaces(clock).open))
	mux.HandleFunc("/v1/link/limits", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	server := httptest.NewServer(mux)
	defer server.Close()
	client := directory.NewHTTP(server.URL, fakeSign("id_one", devA), server.Client())
	v, err := client.Create(t.Context(), cellX, directory.CellInit{Head: head1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.NewLinkHTTP(server.URL, server.Client()).Limits(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := client.Heartbeat(t.Context(), cellX, directory.Beat{Fence: v.Cell.Lease.Fence, Working: true})
	if err != nil || !got.Cell.Lease.Working {
		t.Fatalf("after link limits working = %v, %v; want true", got.Cell.Lease.Working, err)
	}
}
